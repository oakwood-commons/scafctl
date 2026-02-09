# Solution Provider

**Date:** February 9, 2026

---

## Design Philosophy

Three guiding principles:

1. **Fail loud by default, opt into silence.** Sub-solution failures should be Go errors unless the user explicitly asks for envelope-only reporting. This prevents the most common mistake (forgetting to check `status`).

2. **Reuse existing types everywhere.** The codebase already has `ValueRef`, `get.Interface`, `resolver.Context`, `action.ExecutionResult`, etc. The solution provider should compose these, not reinvent them.

3. **Separation of loading vs execution.** The provider should do one thing — execute a loaded solution. Loading is delegated entirely to `get.Interface`. This makes testing trivial and keeps the provider focused.

---

## Purpose

The `solution` provider enables **solution composition** — executing a sub-solution as an opaque unit of work and consuming its results. The parent solution never sees the sub-solution's internal resolvers, actions, or DAG; it receives only the final output.

This supports a composition model where reusable solutions are published to the catalog and consumed as building blocks. A team maintains `deploy-to-k8s@2.0.0` in the catalog, and any parent solution invokes it without knowing its internals.

---

## What This Provider Does

- Loads a sub-solution from a local file, catalog reference, or URL
- Executes the sub-solution's resolvers (and optionally its workflow) as a self-contained unit
- Returns the sub-solution's results to the parent as structured data
- Passes parameters from the parent into the sub-solution
- Detects circular references and enforces depth limits

## What This Provider Does Not Do

- Expose the sub-solution's internal resolvers or actions to the parent
- Interleave sub-solution execution with parent execution
- Modify the sub-solution in any way
- Manage sub-solution lifecycle or state beyond a single execution

---

## Capabilities

| Capability | Context | Behavior |
|------------|---------|----------|
| `from` | Resolver `resolve.with` | Runs resolvers only, returns resolver values |
| `action` | Workflow `actions` | Runs resolvers + workflow, returns full result |

---

## Solution Sources

The provider resolves sub-solutions from multiple locations, using the same resolution logic as `scafctl run solution` via `get.Getter`:

| Source Type | Example `source` Value | Resolution |
|-------------|----------------------|------------|
| **Local file** | `./infra/database.yaml`, `/absolute/path.yaml` | Loaded from the filesystem relative to the working directory |
| **Catalog** | `deploy-to-k8s`, `deploy-to-k8s@2.0.0` | Searches local catalog first, then configured remote catalogs |
| **URL** | `https://example.com/solution.yaml` | Fetched via HTTP(S) |

Resolution priority follows `get.Getter` behavior: catalog bare name → filesystem → URL.

> **Note**: When a parent solution referencing local file paths (e.g., `source: "./child.yaml"`) is built and pushed to a catalog, the referenced files are not included by default. See [catalog-build-bundling.md](catalog-build-bundling.md) for the design to support this.

### Catalog Version Resolution

Bare catalog names (e.g., `deploy-to-k8s`) resolve to the highest semver version at execution time. This is non-deterministic — if a new version is published between runs, the newer version is used silently.

For reproducible builds, use explicit versions: `deploy-to-k8s@2.0.0`. Digest pinning (`deploy-to-k8s@sha256:...`) provides the strongest guarantee by resolving via content hash.

> **Future Enhancement**: A catalog-level lock file mechanism (similar to `go.sum` or `package-lock.json`) could automatically record resolved versions on first use and replay them on subsequent runs. This would benefit all catalog consumers — CLI `run`, the solution provider, and any future catalog-aware tooling. This is tracked as a future catalog package enhancement, not a solution provider concern.

---

## Input Schema

1. **`source` is a `ValueRef`** — allowing dynamic resolution via CEL. This unlocks patterns like selecting a catalog solution based on a resolved environment value.

2. **`inputs` is explicitly `map[string]*spec.ValueRef`** — the same type used by `ProviderSource.Inputs` and `Action.Inputs`, giving us expr/tmpl/rslvr/literal support for free.

3. **`propagateErrors`** — controls error behavior, defaulting to `true` (Go error on failure). Set `false` for envelope-only reporting.

4. **`maxDepth`** — a hard recursion limit as defense in depth beyond circular detection.

~~~yaml
source:
  type: ValueRef
  required: true
  description: >
    Location of the sub-solution. Supports literal string, CEL expression,
    resolver reference, or Go template. Resolved value must be a string
    (file path, catalog reference, or URL).
  example:
    literal: "deploy-to-k8s@2.0.0"
    # or dynamically:
    # expr: "'deploy-to-' + _.target + '@2.0.0'"

inputs:
  type: map[string]*ValueRef
  required: false
  description: >
    Key-value map passed as resolver parameters to the sub-solution.
    Each value supports the full ValueRef surface (literal, expr, rslvr, tmpl).
    Injected via provider.WithParameters() — consumed by `parameter` provider
    resolvers in the sub-solution.

propagateErrors:
  type: boolean
  required: false
  default: true
  description: >
    When true (default), sub-solution failures cause the provider to return
    a Go error. When false, failures are reported only in the output envelope's
    `status` and `errors` fields.

maxDepth:
  type: integer
  required: false
  default: 10
  description: >
    Maximum nesting depth for recursive solution composition.
    Prevents runaway recursion even without direct cycles.

resolvers:
  type: array
  required: false
  items:
    type: string
  description: >
    List of resolver names to execute from the child solution.
    When omitted or empty, all resolvers in the child solution run.
    When specified, only the listed resolvers are executed, which reduces
    execution time when only a subset of the child's values is needed.
    An error is returned if any listed name does not exist in the child.

timeout:
  type: string
  required: false
  description: >
    Maximum duration for sub-solution execution, as a Go duration string
    (e.g. "30s", "5m", "1h"). When set, a context deadline is applied to
    the child's resolver and workflow execution. If the timeout expires,
    the child execution is cancelled and an error is returned (or reported
    in the envelope if propagateErrors is false).
  example: "30s"
~~~

### Dynamic `source`

Making `source` a `ValueRef` enables composition patterns like:

~~~yaml
infra-solution:
  type: any
  resolve:
    with:
      - provider: solution
        inputs:
          source:
            expr: "'infra-' + _.region + '@1.0.0'"
~~~

Since the existing `InputResolver` already handles `ValueRef` resolution for all input fields, this is free — no extra implementation work.

The `ExtractDependencies` function on the descriptor handles `source` as well, ensuring that if `source` contains `_.someResolver`, the DAG correctly orders it.

### Output Shaping

The provider returns the full output envelope. To extract or reshape specific values, use the resolver's **transform phase**:

~~~yaml
infra-config:
  type: any
  resolve:
    with:
      - provider: solution
        inputs:
          source: "infra-config@1.0.0"
  transform:
    with:
      - provider: cel
        inputs:
          expression: "__self.resolvers"
~~~

This keeps the provider focused on execution and delegates post-processing to the existing framework-level mechanism. The transform phase supports CEL, Go templates, and any other transform-capable provider.

---

## Output Envelope

### `from` Capability

~~~json
{
  "resolvers": {
    "resolver-name": "<value>"
  },
  "status": "success | failed",
  "errors": [
    {"resolver": "name", "message": "description"}
  ]
}
~~~

### `action` Capability

~~~json
{
  "resolvers": {
    "resolver-name": "<value>"
  },
  "workflow": {
    "finalStatus": "succeeded | failed | cancelled | partial-success",
    "failedActions": [],
    "skippedActions": []
  },
  "status": "success | failed",
  "errors": [],
  "success": true
}
~~~

The `workflow` field provides only aggregate status — `finalStatus`, `failedActions`, and `skippedActions`. Individual sub-action results are **not** included, maintaining the opacity boundary. If a sub-solution author wants to expose specific results to the parent, they should do so through the sub-solution's resolvers (visible via `resolvers` in the envelope).

If the parent truly needs sub-action details (escape hatch), a `verbose: true` input could include the full `workflow.actions` map. But the default is opaque.

---

## Error Handling

### Default: Propagate Errors

When `propagateErrors` is `true` (the default), the provider:

1. Executes the sub-solution.
2. If any resolver fails or the workflow has `finalStatus != "succeeded"`, returns a Go error with a descriptive message:
   ~~~
   solution "deploy-k8s@2.0.0": resolver "db-host" failed: connection refused
   ~~~
3. The parent's own `onError` mechanism handles it (fallback to next source, `continue`, or `fail`).

This integrates naturally with the existing resolver fallback chain. A resolver using the solution provider with `onError: continue` can fall back to another source if the sub-solution fails.

### Opt-in: Envelope-Only

When `propagateErrors: false`, the provider always returns `*Output{Data: envelope}` — never a Go error (except for circular references and context cancellation). The provider adds a warning via `Output.Warnings` when `status == "failed"`:

~~~
sub-solution "deploy-k8s@2.0.0" failed: 1 resolver error(s). Check _.status and _.errors fields.
~~~

This ensures failures appear in logs even if the parent doesn't inspect the envelope.

### Always a Go Error

Circular reference detection and max depth violations always return a Go error regardless of `propagateErrors`, since these are programming mistakes.

---

## Circular Reference & Depth Detection

Sub-solutions can themselves use the `solution` provider, enabling multi-level composition. Two mechanisms prevent runaway recursion:

### Ancestor Stack

A `[]string` ancestor stack in context, using the existing `With*/From*` pattern:

~~~go
// pkg/provider/builtin/solutionprovider/context.go

type ancestorStackKey struct{}

func WithAncestorStack(ctx context.Context, stack []string) context.Context
func AncestorStackFromContext(ctx context.Context) []string
func PushAncestor(ctx context.Context, name string) (context.Context, error)
~~~

`PushAncestor` checks for duplicates and returns an error with the full chain if found.

### Max Depth

In addition to cycle detection, a hard depth limit (default 10, configurable via `maxDepth` input) catches non-cyclic but pathologically deep composition (A → B → C → D → ... → Z).

The current depth is derived from `len(AncestorStackFromContext(ctx))`. If adding the new solution would exceed `maxDepth`, return an error.

### Canonical Names

| Source Type | Canonical Name |
|-------------|---------------|
| File path | Absolute path (e.g., `/home/user/infra.yaml`) |
| Catalog | `name@version` (e.g., `deploy-to-k8s@2.0.0`) |
| URL | Full URL (e.g., `https://example.com/solution.yaml`) |

Error example:

~~~text
solution: circular reference detected: parent-solution → infra-config@1.0.0 → parent-solution
~~~

---

## Context Isolation

Each sub-solution runs in an isolated context. This is critical for correctness — the sub-solution must not see the parent's resolver values or parameters.

### Propagated (parent → sub-solution)

| Value | Mechanism | Rationale |
|-------|-----------|-----------|
| Logger | `logger.WithLogger(ctx, scoped)` | Scoped with sub-solution name prefix |
| Writer | `writer.FromContext(ctx)` (pass through) | Sub-solution output goes to same terminal |
| Dry-run | `provider.WithDryRun(ctx, flag)` | Consistent behavior |
| Auth registry | `auth.WithRegistry(ctx, reg)` | Sub-solutions need the same auth |
| App config | `config.WithConfig(ctx, cfg)` | Sub-solutions read the same config |
| Ancestor stack | Custom context key | For recursion detection |

### Replaced (parent context stripped, sub-solution gets fresh values)

| Value | Mechanism | Rationale |
|-------|-----------|-----------|
| Resolver context | `provider.WithResolverContext(ctx, map{})` | Sub-solution starts fresh; parent's `_` is not visible |
| Parameters | `provider.WithParameters(ctx, inputs)` | `inputs` map replaces parent's `-r` params |
| Iteration context | Not propagated | Sub-solution is not part of parent's forEach |
| Action context | Not shared | Sub-solution builds its own `action.Context` |

### NOT Propagated

| Value | Rationale |
|-------|-----------|
| Parent `resolver.Context` | Would leak parent resolver names into sub-solution's `_` namespace |
| Parent `__actions` | Sub-solution's actions are independent |
| Execution mode | Set independently based on whether `from` or `action` capability is active |
| Metrics | Sub-solution records its own metrics; aggregated at output |

### Logger Scoping

The logger passed to the sub-solution is scoped with the solution name:

~~~go
subLogger := logger.FromContext(ctx).WithName("solution:" + canonicalName)
ctx = logger.WithLogger(ctx, subLogger)
~~~

This produces log output like:
~~~
solution:deploy-k8s@2.0.0  Resolving 5 resolvers in 3 phases
solution:deploy-k8s@2.0.0  Phase 1: executing 2 resolvers concurrently
~~~

---

## Execution Model

### `from` Capability (Resolver Context)

~~~
Execute(ctx, input) → *Output
  1. Resolve `source` → string path
  2. Resolve `inputs` → map[string]any params
  3. Check ancestor stack + depth
  4. Push ancestor
  5. Load solution via get.Interface.Get(ctx, path)
  6. Build isolated context:
     - Fresh resolver context (empty)
     - Parameters from `inputs`
     - Scoped logger
  7. Create resolver.Executor with shared registry
  8. Execute resolvers: executor.Execute(ctx, solution.Spec.Resolvers, params)
  9. Extract resolver.Context → build envelope
  10. If sub-solution failed AND propagateErrors → return Go error
  11. Return &Output{Data: envelope, Warnings: warnings}
~~~

### `action` Capability (Workflow Context)

Same as above through step 8, then:

~~~
  9. Extract resolverData from resolver.Context
  10. Build action.Graph from solution.Spec.Workflow
  11. Create action.Executor with shared registry, resolverData, and progress callback
  12. Execute workflow: actionExecutor.Execute(ctx, workflow)
  13. Build envelope with resolver values + workflow summary
  14. If sub-solution failed AND propagateErrors → return Go error
  15. Return &Output{Data: envelope, Warnings: warnings}
~~~

### Progress Reporting

The action executor supports `WithProgressCallback` for reporting execution progress. When the solution provider runs in `action` capability mode, it propagates progress callbacks from the sub-solution's action executor to the parent. Sub-solution progress events are prefixed with the solution's canonical name to distinguish them from parent-level progress:

~~~
[solution:deploy-k8s@2.0.0] Action "provision-cluster" succeeded (3/5)
[solution:deploy-k8s@2.0.0] Action "configure-networking" running (4/5)
~~~

This ensures the parent does not block silently during long-running sub-solution workflows.

### Timeout

The provider accepts an optional `timeout` input as a Go duration string (e.g. `"30s"`, `"5m"`). When specified, a `context.WithTimeout` deadline is applied to the child solution's resolver and workflow execution. If the timeout expires, child execution is cancelled and the error is either returned (default) or reported in the envelope (when `propagateErrors: false`).

The parent resolver/action `Timeout` field also applies to the entire provider execution via context deadline propagation, so the provider-level `timeout` acts as an inner bound within the outer deadline.

### Resolver Filtering

The provider accepts an optional `resolvers` input — a list of resolver names to execute from the child solution. When omitted or empty, all resolvers in the child solution run (default behaviour). When specified, only the listed resolvers are executed, which reduces execution time when only a subset of the child's values is needed.

An error is returned if any listed name does not exist in the child solution. This catches typos early rather than silently returning partial results.

~~~yaml
child-config:
  type: any
  resolve:
    with:
      - provider: solution
        inputs:
          source: "infra-config@1.0.0"
          resolvers:
            - database-url
            - cache-ttl
~~~

### Dry Run

In dry-run mode, the provider **still loads the solution** (to validate the source is valid) but does not execute it. This catches typos in catalog names or file paths before real execution. The cost is one file read or HTTP request, which is acceptable.

Returns a mock envelope:

~~~json
// from capability:
{
  "resolvers": {},
  "status": "success",
  "errors": [],
  "dryRun": true
}

// action capability:
{
  "resolvers": {},
  "workflow": {
    "finalStatus": "succeeded",
    "failedActions": [],
    "skippedActions": []
  },
  "status": "success",
  "errors": [],
  "dryRun": true,
  "success": true
}
~~~

---

## Usage Examples

### Resolver: Import Values from a Catalog Solution

~~~yaml
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "production"

    infra-config:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "infra-config@1.0.0"
              inputs:
                environment:
                  expr: "_.environment"
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.resolvers"
~~~

Result: `_.infra-config` = `{"db_host": "db.prod.internal", "db_port": 5432}`

### Resolver: Extract a Specific Value with CEL

~~~yaml
    db-connection:
      type: string
      resolve:
        with:
          - provider: solution
            inputs:
              source: "infra-config@1.0.0"
              inputs:
                environment:
                  expr: "_.environment"
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.resolvers.db_host + ':' + string(__self.resolvers.db_port)"
~~~

Result: `_.db-connection` = `"db.prod.internal:5432"`

### Resolver: Dynamic Source

~~~yaml
    infra:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source:
                expr: "'infra-' + _.region + '@1.0.0'"
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.resolvers"
~~~

### Resolver: Local File Reference

~~~yaml
    local-config:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./shared/common-config.yaml"
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.resolvers"
~~~

### Workflow: Compose Deployment Steps

~~~yaml
spec:
  resolvers:
    region:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "us-east-1"

  workflow:
    actions:
      setup:
        provider: exec
        inputs:
          shell: true
          command: "echo 'Preparing...'"

      deploy-infra:
        provider: solution
        dependsOn:
          - setup
        inputs:
          source: "deploy-k8s@2.0.0"
          inputs:
            region:
              expr: "_.region"
            cluster: "main-cluster"

      verify:
        provider: exec
        dependsOn:
          - deploy-infra
        inputs:
          shell: true
          command:
            expr: "'Deploy status: ' + string(__actions['deploy-infra'].results.workflow.finalStatus)"
~~~

### Error Handling in Parent (envelope mode)

~~~yaml
    infra:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "infra-config@1.0.0"
              propagateErrors: false

    app-config:
      type: any
      dependsOn:
        - infra
      when:
        expr: "_.infra.status == 'success'"
      resolve:
        with:
          - provider: static
            inputs:
              value:
                expr: "_.infra.resolvers.db_host"
~~~

### Error Handling in Parent (fallback chain)

~~~yaml
    infra:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "infra-config@1.0.0"
            onError: continue
          - provider: static
            inputs:
              value:
                db_host: "localhost"
                db_port: 5432
~~~

---

## Implementation

### Key Files

| File | Purpose |
|------|---------|
| `pkg/provider/builtin/solutionprovider/solution.go` | Provider struct, `Descriptor()`, `Execute()` |
| `pkg/provider/builtin/solutionprovider/solution_test.go` | Unit tests with mock loader |
| `pkg/provider/builtin/solutionprovider/context.go` | Ancestor stack + depth context helpers |
| `pkg/provider/builtin/solutionprovider/envelope.go` | Envelope construction for `from` and `action` |
| `pkg/cmd/scafctl/run/solution.go` | Registration wiring |
| `tests/integration/cli_test.go` | Integration tests |

### Provider Structure

~~~go
// pkg/provider/builtin/solutionprovider/solution.go

type SolutionProvider struct {
    loader   get.Interface
    registry *provider.Registry
}

type Option func(*SolutionProvider)

func WithLoader(l get.Interface) Option
func WithRegistry(r *provider.Registry) Option

func New(opts ...Option) *SolutionProvider
~~~

The provider uses `get.Interface` directly for solution loading. `get.Interface` is a small, focused interface and `get.MockGetter` already exists for testing. If decoupling from the `get` package is desired, a local interface can be defined — Go's structural typing means `get.Getter` satisfies it automatically:

~~~go
type Loader interface {
    Get(ctx context.Context, path string) (*solution.Solution, error)
}
~~~

The provider also receives a reference to the provider registry so sub-solutions have access to all built-in and plugin providers.

### Registration

The solution provider is **not** registered in `builtin.go`. It depends on `get.Interface` (which needs catalog config) and a reference to the `*provider.Registry` itself — both are available only at CLI wiring time.

Registration happens in the CLI run command:

~~~go
// In pkg/cmd/scafctl/run/solution.go, inside Run():

getter := get.NewGetter(...)
registry := builtin.MustDefaultRegistry()

solutionProv := solutionprovider.New(
    solutionprovider.WithLoader(getter),
    solutionprovider.WithRegistry(registry),
)
registry.Register(solutionProv)
~~~

### Custom Dependency Extraction

The provider implements `ExtractDependencies` on its descriptor to scan `source` and nested `inputs` for resolver references:

~~~go
desc.ExtractDependencies = func(inputs map[string]any) []string {
    var deps []string
    if source, ok := inputs["source"]; ok {
        deps = append(deps, extractRefsFromValue(source)...)
    }
    if subInputs, ok := inputs["inputs"].(map[string]any); ok {
        for _, v := range subInputs {
            deps = append(deps, extractRefsFromValue(v)...)
        }
    }
    return deps
}
~~~

### Schema Definition

Input schema:

~~~yaml
type: object
required: [source]
properties:
  source:
    description: "Sub-solution location (file path, catalog reference, or URL)"
  inputs:
    type: object
    description: "Parameters passed to the sub-solution's parameter provider"
    additionalProperties: true
  propagateErrors:
    type: boolean
    default: true
    description: "Whether sub-solution failures cause a Go error"
  maxDepth:
    type: integer
    default: 10
    minimum: 1
    maximum: 100
    description: "Maximum nesting depth for recursive composition"
  resolvers:
    type: array
    items:
      type: string
    description: "Resolver names to execute; when empty all resolvers run"
  timeout:
    type: string
    description: "Maximum duration for sub-solution execution (e.g. 30s, 5m)"
    example: "30s"
~~~

Output schemas per capability:

~~~yaml
from:
  type: object
  properties:
    resolvers:
      type: object
    status:
      type: string
    errors:
      type: array

action:
  type: object
  required: [success]
  properties:
    success:
      type: boolean
    resolvers:
      type: object
    workflow:
      type: object
    status:
      type: string
    errors:
      type: array
~~~

Note: `action` capability requires a `success` boolean field in the output schema (enforced by `ValidateDescriptor`). The provider sets this to `status == "success"`.

---

## Test Strategy

### Unit Tests (`solution_test.go`)

1. **Basic `from` — success:** Mock loader returns solution with 2 resolvers. Assert envelope has correct `resolvers` map and `status: success`.
2. **Basic `action` — success:** Mock loader returns solution with resolvers + workflow. Assert envelope includes `workflow.finalStatus` and `success: true`.
3. **`propagateErrors: true` (default):** Mock loader returns solution whose resolvers fail. Assert `Execute` returns a Go error.
4. **`propagateErrors: false`:** Same failing solution, but now `Execute` returns `*Output` with `status: failed` and `Output.Warnings` populated.
5. **Circular detection — direct:** A → A. Assert error message includes chain.
6. **Circular detection — indirect:** A → B → A. Push "A" onto ancestor stack in context, then execute with source "A". Assert error.
7. **Max depth exceeded:** Push 10 ancestors onto stack, execute with `maxDepth: 10`. Assert depth error.
8. **Dry-run:** Set `DryRun(ctx, true)`. Assert mock envelope returned, loader still called (validates source), but resolvers not executed.
9. **Context isolation:** Assert sub-solution does not see parent's resolver values or parameters.
10. **Dynamic source:** `source` with CEL expression resolving to a catalog name. Assert correct solution is loaded.

### Integration Tests (`cli_test.go`)

1. **End-to-end resolver composition:** Parent solution with a resolver using `provider: solution` pointing to a child solution file. Assert parent resolver gets child's resolver values.
2. **End-to-end workflow composition:** Parent workflow invokes child solution as an action. Assert `__actions` contains envelope with workflow status.
3. **Circular reference:** Two solution files referencing each other. Assert clear error message.
4. **Parameter passthrough:** Parent passes `inputs` to child. Child's `parameter` resolver receives the value. Assert child resolver resolves correctly.
5. **Transform phase integration:** Parent uses transform to extract specific values from the solution provider envelope.