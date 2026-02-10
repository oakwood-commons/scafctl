# Solution Provider Implementation Plan

This document outlines the implementation plan for the `solution` provider, based on the [design specification](../design/solution-provider.md).

---

## Executive Summary

The `solution` provider enables solution composition — executing a sub-solution as an opaque unit of work and consuming its results. A parent solution loads a sub-solution (from file, catalog, or URL), executes it in isolation, and receives structured output without visibility into the sub-solution's internals.

**Core Principles:**
- Fail loud by default, opt into envelope-only reporting
- Reuse existing types (`ValueRef`, `get.Interface`, `resolver.Executor`, `action.Executor`)
- Separation of loading vs execution — provider executes, `get.Interface` loads

---

## Design Decisions

| Topic | Decision |
|-------|----------|
| Loading mechanism | Delegate to `get.Interface` (same resolution as `scafctl run solution`) |
| Provider capabilities | `from` (resolvers only) and `action` (resolvers + workflow) |
| Error propagation | `propagateErrors: true` (default) returns Go error; `false` returns envelope only |
| Circular detection | Ancestor stack in context with canonical names |
| Depth limit | `maxDepth` input (default 10) as defense in depth |
| Context isolation | Sub-solution gets fresh resolver context and parameters; inherits logger, writer, auth, config |
| Registration | NOT in `builtin.go` — registered at CLI wiring time (depends on `get.Interface` and `*provider.Registry`) |
| Dependency extraction | Custom `ExtractDependencies` scans `source` and `inputs` for resolver references |
| Dry-run | Loads solution (validates source) but does not execute; returns mock envelope |
| Resolver filtering | `resolvers` input selects which child resolvers to run; empty means all |
| Timeout | `timeout` input applies a context deadline to child execution (Go duration string) |

---

## Dependencies Map

Understanding what already exists and what this provider needs to interact with.

### Existing Packages (No Changes Required)

| Package | Type/Function | Usage |
|---------|---------------|-------|
| `pkg/solution/get` | `get.Interface`, `get.Getter` | Solution loading from file/catalog/URL |
| `pkg/solution` | `Solution`, `Spec` | Solution data model |
| `pkg/resolver` | `Executor`, `RegistryInterface` | Resolver execution |
| `pkg/action` | `Executor`, `Workflow`, `Context` | Action/workflow execution |
| `pkg/provider` | `Provider`, `Descriptor`, `Output`, `Registry` | Provider interface and registration |
| `pkg/provider` | `WithDryRun`, `DryRunFromContext` | Dry-run context |
| `pkg/provider` | `WithResolverContext`, `ResolverContextFromContext` | Resolver context isolation |
| `pkg/provider` | `WithParameters`, `ParametersFromContext` | Parameter injection |
| `pkg/provider` | `ExecutionModeFromContext` | Execution mode detection |
| `pkg/logger` | `FromContext`, `WithLogger` | Logger context |
| `pkg/terminal/writer` | `FromContext` | Writer pass-through |
| `pkg/config` | `WithConfig` | App config pass-through |
| `pkg/auth` | `WithRegistry` | Auth registry pass-through |
| `pkg/schema/schemahelper` | `ObjectSchema` | JSON schema construction |

### New Package

| Package | Purpose |
|---------|---------|
| `pkg/provider/builtin/solutionprovider` | Solution provider implementation |

---

## Implementation Phases

### Phase 1: Context Helpers (Ancestor Stack)

**Goal:** Implement the ancestor stack and depth detection context helpers.

**File:** `pkg/provider/builtin/solutionprovider/context.go`

```go
package solutionprovider

import "context"

type ancestorStackKey struct{}

// WithAncestorStack returns a new context with the given ancestor stack.
func WithAncestorStack(ctx context.Context, stack []string) context.Context

// AncestorStackFromContext retrieves the ancestor stack from context.
// Returns nil if no stack is set (root-level execution).
func AncestorStackFromContext(ctx context.Context) []string

// PushAncestor adds a canonical name to the ancestor stack and returns the
// updated context. Returns an error if the name already exists in the stack
// (circular reference detected).
func PushAncestor(ctx context.Context, name string) (context.Context, error)
```

**Canonical name resolution:**

| Source Type | Canonical Form | Example |
|-------------|----------------|---------|
| File path | Absolute path | `/home/user/infra.yaml` |
| Catalog reference | `name@version` | `deploy-to-k8s@2.0.0` |
| URL | Full URL | `https://example.com/solution.yaml` |

**Error format:**

```
solution: circular reference detected: parent-solution → infra-config@1.0.0 → parent-solution
```

**Depth check logic:**

```go
func checkDepth(ctx context.Context, maxDepth int) error {
    stack := AncestorStackFromContext(ctx)
    if len(stack) >= maxDepth {
        return fmt.Errorf("solution: max nesting depth %d exceeded: %s",
            maxDepth, strings.Join(stack, " → "))
    }
    return nil
}
```

**File:** `pkg/provider/builtin/solutionprovider/context_test.go`

Tests:
- [x] `PushAncestor` on empty context — creates stack with one entry
- [x] `PushAncestor` with existing stack — appends correctly
- [x] `PushAncestor` duplicate — returns error with chain
- [x] `PushAncestor` indirect cycle (A → B → A) — returns error
- [x] `AncestorStackFromContext` on empty context — returns nil
- [x] `checkDepth` under limit — no error
- [x] `checkDepth` at limit — returns error

---

### Phase 2: Envelope Construction

**Goal:** Implement helpers to build the output envelope for both capabilities.

**File:** `pkg/provider/builtin/solutionprovider/envelope.go`

```go
package solutionprovider

// ResolverError represents a single resolver failure in the output envelope.
type ResolverError struct {
    Resolver string `json:"resolver"`
    Message  string `json:"message"`
}

// Envelope is the output structure for the solution provider.
type Envelope struct {
    Resolvers map[string]any  `json:"resolvers"`
    Workflow  *WorkflowResult `json:"workflow,omitempty"`  // only for action capability
    Status    string          `json:"status"`              // "success" or "failed"
    Errors    []ResolverError `json:"errors"`
    Success   *bool           `json:"success,omitempty"`   // only for action capability
    DryRun    *bool           `json:"dryRun,omitempty"`    // only when dry-run mode
}

// WorkflowResult is the aggregate workflow status in the output envelope.
type WorkflowResult struct {
    FinalStatus    string   `json:"finalStatus"`
    FailedActions  []string `json:"failedActions"`
    SkippedActions []string `json:"skippedActions"`
}

// buildFromEnvelope constructs the output envelope for `from` capability.
func buildFromEnvelope(resolverData map[string]any, errors []ResolverError) *Envelope

// buildActionEnvelope constructs the output envelope for `action` capability.
func buildActionEnvelope(resolverData map[string]any, workflowResult *WorkflowResult, errors []ResolverError) *Envelope

// buildDryRunEnvelope constructs the mock envelope for dry-run mode.
// isAction controls whether workflow and success fields are included.
func buildDryRunEnvelope(isAction bool) *Envelope
```

**File:** `pkg/provider/builtin/solutionprovider/envelope_test.go`

Tests:
- [x] `buildFromEnvelope` — success case with resolver values
- [x] `buildFromEnvelope` — failed case with errors
- [x] `buildActionEnvelope` — success with workflow result
- [x] `buildActionEnvelope` — failed workflow sets `success: false`
- [x] `buildDryRunEnvelope` — from capability (no workflow)
- [x] `buildDryRunEnvelope` — action capability (with workflow)

---

### Phase 3: Provider Core

**Goal:** Implement the `SolutionProvider` struct, `Descriptor()`, and `Execute()`.

**File:** `pkg/provider/builtin/solutionprovider/solution.go`

#### Provider Struct and Constructor

```go
package solutionprovider

const ProviderName = "solution"

// Loader is the interface for loading solutions.
// get.Getter satisfies this via Go structural typing.
type Loader interface {
    Get(ctx context.Context, path string) (*solution.Solution, error)
}

// SolutionProvider executes sub-solutions and returns their results.
type SolutionProvider struct {
    loader   Loader
    registry *provider.Registry
}

type Option func(*SolutionProvider)

func WithLoader(l Loader) Option {
    return func(p *SolutionProvider) { p.loader = l }
}

func WithRegistry(r *provider.Registry) Option {
    return func(p *SolutionProvider) { p.registry = r }
}

func New(opts ...Option) *SolutionProvider
```

#### Input Struct (Decoded)

```go
type Input struct {
    Source          string         `json:"source" doc:"Sub-solution location (file path, catalog reference, or URL)" example:"deploy-to-k8s@2.0.0"`
    Inputs         map[string]any `json:"inputs,omitempty" doc:"Parameters passed to the sub-solution's parameter provider"`
    Resolvers      []string       `json:"resolvers,omitempty" doc:"Resolver names to execute from the child solution; when empty all resolvers run"`
    PropagateErrors *bool         `json:"propagateErrors,omitempty" doc:"Whether sub-solution failures cause a Go error"`
    MaxDepth       *int           `json:"maxDepth,omitempty" doc:"Maximum nesting depth for recursive composition" minimum:"1" maximum:"100"`
    Timeout        *string        `json:"timeout,omitempty" doc:"Maximum duration for sub-solution execution (e.g. 30s, 5m)" example:"30s"`
}

func (i *Input) shouldPropagateErrors() bool {
    if i.PropagateErrors == nil {
        return true // default
    }
    return *i.PropagateErrors
}

func (i *Input) maxDepthOrDefault() int {
    if i.MaxDepth == nil {
        return 10 // default
    }
    return *i.MaxDepth
}
```

#### Descriptor

The descriptor declares:
- **Name:** `"solution"`
- **Capabilities:** `CapabilityFrom`, `CapabilityAction`
- **Schema:** JSON schema for `Input` struct (using `schemahelper`)
- **OutputSchemas:** Per-capability output schemas
- **Decode:** Decodes `map[string]any` → `*Input`
- **ExtractDependencies:** Custom function scanning `source` and `inputs` for resolver references
- **MockBehavior:** `"envelope"` — returns static envelope structure

```go
func (p *SolutionProvider) Descriptor() *provider.Descriptor {
    // Build input schema
    schema := schemahelper.ObjectSchema(
        []string{"source"}, // required fields
        map[string]*jsonschema.Schema{
            "source": schemahelper.StringSchema("Sub-solution location"),
            "inputs": {
                Type:                 jsonschema.TypeObject,
                Description:          "Parameters passed to the sub-solution",
                AdditionalProperties: jsonschema.TrueSchema,
            },
            "resolvers":       schemahelper.ArraySchema("Resolver names to execute; when empty all run"),
            "propagateErrors": schemahelper.BoolSchema("Whether sub-solution failures cause a Go error"),
            "maxDepth":        schemahelper.IntSchema("Maximum nesting depth", 1, 100),
            "timeout":         schemahelper.StringSchema("Maximum duration for execution (e.g. 30s, 5m)"),
        },
    )

    return &provider.Descriptor{
        Name:        ProviderName,
        APIVersion:  "v1",
        Description: "Executes a sub-solution and returns its results",
        Schema:      schema,
        Capabilities: []provider.Capability{
            provider.CapabilityFrom,
            provider.CapabilityAction,
        },
        OutputSchemas:       buildOutputSchemas(),
        Decode:              decodeInput,
        ExtractDependencies: extractDependencies,
        MockBehavior:        "envelope",
    }
}
```

#### Execute Method

This is the core of the provider. The execution flow differs based on capability:

```go
func (p *SolutionProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    in := input.(*Input)
    lgr := logger.FromContext(ctx)

    // 1. Resolve canonical name for source
    canonicalName := canonicalize(in.Source)

    // 2. Check circular references
    if err := checkCircular(ctx, canonicalName); err != nil {
        return nil, err // always a Go error regardless of propagateErrors
    }

    // 3. Check depth limit
    if err := checkDepth(ctx, in.maxDepthOrDefault()); err != nil {
        return nil, err // always a Go error regardless of propagateErrors
    }

    // 4. Push ancestor
    ctx, err := PushAncestor(ctx, canonicalName)
    if err != nil {
        return nil, err
    }

    // 5. Load solution
    lgr.V(1).Info("loading sub-solution", "source", in.Source)
    sol, err := p.loader.Get(ctx, in.Source)
    if err != nil {
        return nil, fmt.Errorf("solution %q: failed to load: %w", in.Source, err)
    }

    // 6. Dry-run: validate source but don't execute
    if provider.DryRunFromContext(ctx) {
        isAction := provider.ExecutionModeFromContext(ctx) == provider.ExecutionModeAction
        envelope := buildDryRunEnvelope(isAction)
        return &provider.Output{Data: envelopeToMap(envelope)}, nil
    }

    // 7. Build isolated context
    subCtx := buildIsolatedContext(ctx, canonicalName, in.Inputs)

    // 8. Determine execution mode and run
    mode := provider.ExecutionModeFromContext(ctx)
    switch {
    case mode == provider.ExecutionModeAction && sol.Spec.HasWorkflow():
        return p.executeWithWorkflow(subCtx, sol, in)
    default:
        return p.executeResolversOnly(subCtx, sol, in)
    }
}
```

#### Resolver-Only Execution (`from` capability)

```go
func (p *SolutionProvider) executeResolversOnly(ctx context.Context, sol *solution.Solution, in *Input) (*provider.Output, error) {
    lgr := logger.FromContext(ctx)

    // Create resolver executor with shared registry
    executor := resolver.NewExecutor(
        resolver.WithRegistry(p.registry),
    )

    // Execute resolvers
    lgr.V(1).Info("executing sub-solution resolvers", "count", len(sol.Spec.Resolvers))
    rctx, err := executor.Execute(ctx, sol.Spec.Resolvers, in.Inputs)

    // Build envelope
    resolverData := extractResolverData(rctx)
    var resolverErrors []ResolverError
    if err != nil {
        resolverErrors = extractResolverErrors(err)
    }
    envelope := buildFromEnvelope(resolverData, resolverErrors)

    // Error propagation
    if err != nil && in.shouldPropagateErrors() {
        return nil, fmt.Errorf("solution %q: %w", in.Source, err)
    }

    output := &provider.Output{Data: envelopeToMap(envelope)}
    if envelope.Status == "failed" {
        output.Warnings = []string{
            fmt.Sprintf("sub-solution %q failed: %d resolver error(s). Check _.status and _.errors fields.",
                in.Source, len(resolverErrors)),
        }
    }
    return output, nil
}
```

#### Workflow Execution (`action` capability)

```go
func (p *SolutionProvider) executeWithWorkflow(ctx context.Context, sol *solution.Solution, in *Input) (*provider.Output, error) {
    lgr := logger.FromContext(ctx)

    // Phase 1: Execute resolvers
    resolverExecutor := resolver.NewExecutor(
        resolver.WithRegistry(p.registry),
    )
    rctx, resolverErr := resolverExecutor.Execute(ctx, sol.Spec.Resolvers, in.Inputs)

    resolverData := extractResolverData(rctx)
    var resolverErrors []ResolverError
    if resolverErr != nil {
        resolverErrors = extractResolverErrors(resolverErr)
        if in.shouldPropagateErrors() {
            return nil, fmt.Errorf("solution %q: resolver phase failed: %w", in.Source, resolverErr)
        }
    }

    // Phase 2: Execute workflow (only if resolvers succeeded)
    var workflowResult *WorkflowResult
    if resolverErr == nil && sol.Spec.HasWorkflow() {
        lgr.V(1).Info("executing sub-solution workflow")
        actionExecutor := action.NewExecutor(
            action.WithRegistry(p.registry),
            action.WithResolverData(resolverData),
        )
        execResult, actionErr := actionExecutor.Execute(ctx, sol.Spec.Workflow)
        workflowResult = buildWorkflowResult(execResult)

        if actionErr != nil && in.shouldPropagateErrors() {
            return nil, fmt.Errorf("solution %q: workflow failed: %w", in.Source, actionErr)
        }
    }

    // Build envelope
    envelope := buildActionEnvelope(resolverData, workflowResult, resolverErrors)

    output := &provider.Output{Data: envelopeToMap(envelope)}
    if envelope.Status == "failed" {
        output.Warnings = []string{
            fmt.Sprintf("sub-solution %q failed. Check _.status and _.errors fields.", in.Source),
        }
    }
    return output, nil
}
```

#### Context Isolation Helper

```go
func buildIsolatedContext(ctx context.Context, canonicalName string, params map[string]any) context.Context {
    // Scoped logger
    subLogger := logger.FromContext(ctx).WithName("solution:" + canonicalName)
    ctx = logger.WithLogger(ctx, subLogger)

    // Fresh resolver context (empty — sub-solution cannot see parent's _.)
    ctx = provider.WithResolverContext(ctx, map[string]any{})

    // Inject inputs as parameters (consumed by parameter provider in sub-solution)
    if params != nil {
        ctx = provider.WithParameters(ctx, params)
    }

    // Writer, auth, config, dry-run, ancestor stack are inherited from parent context
    // (no action needed — they pass through automatically)

    return ctx
}
```

#### Custom Dependency Extraction

```go
func extractDependencies(inputs map[string]any) []string {
    var deps []string

    // Scan source for resolver references
    if source, ok := inputs["source"]; ok {
        deps = append(deps, extractRefsFromValue(source)...)
    }

    // Scan each input value for resolver references
    if subInputs, ok := inputs["inputs"].(map[string]any); ok {
        for _, v := range subInputs {
            deps = append(deps, extractRefsFromValue(v)...)
        }
    }

    return deps
}

// extractRefsFromValue extracts resolver references from a ValueRef-like value.
// Handles literal strings, maps with "rslvr"/"expr"/"tmpl" keys, etc.
func extractRefsFromValue(v any) []string {
    // Implementation uses the same patterns as existing InputResolver
    // dependency extraction in the resolver package
}
```

**File:** `pkg/provider/builtin/solutionprovider/solution_test.go`

Tests (using mock loader):
- [x] Basic `from` success — 2 resolvers, assert envelope
- [x] Basic `action` success — resolvers + workflow, assert envelope with `success: true`
- [x] `propagateErrors: true` — resolver failure returns Go error
- [x] `propagateErrors: false` — resolver failure returns envelope with `status: failed` and `Output.Warnings`
- [x] Circular detection (direct) — A → A
- [x] Circular detection (indirect) — A → B → A
- [x] Max depth exceeded — 10 ancestors, `maxDepth: 10`
- [x] Dry-run — loader called (validates source), resolvers NOT executed, mock envelope returned
- [x] Context isolation — sub-solution does not see parent's resolver values
- [x] Dynamic source — CEL expression in source resolves to catalog name
- [x] Loader error — clear error message with source path
- [x] `propagateErrors` defaults to `true` when nil
- [x] `maxDepth` defaults to 10 when nil
- [x] Action capability with no workflow — executes resolvers only, returns `from`-like envelope

---

### Phase 4: CLI Registration

**Goal:** Wire the solution provider into the `run solution` command.

**File:** `pkg/cmd/scafctl/run/solution.go`

The solution provider requires `get.Interface` and `*provider.Registry`, both of which are already created during `run solution` setup. Registration needs to happen after the registry is created but before resolver/action execution.

```go
// In the Run() function of SolutionOptions, after registry creation:

import "pkg/provider/builtin/solutionprovider"

// Register solution provider (needs getter + registry, so cannot be in builtin.go)
solutionProv := solutionprovider.New(
    solutionprovider.WithLoader(opts.getter),
    solutionprovider.WithRegistry(registry),
)
if err := registry.Register(solutionProv); err != nil {
    return fmt.Errorf("registering solution provider: %w", err)
}
```

**Key consideration:** The solution provider holds a reference to the same registry it's registered in. This is intentional — sub-solutions need access to all providers (including other solution providers for nested composition). The circular reference is safe because:
- The registry is a lookup table, not an execution context
- Provider execution is always forward (parent → child), never backward
- Circular solution execution is caught by the ancestor stack

---

### Phase 5: Integration Tests

**Goal:** Add integration tests to the CLI test suite.

**File:** `tests/integration/cli_test.go`

**Test solution files needed:**

#### `tests/integration/testdata/solution-provider/child.yaml`

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: child-solution
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "hello from child"
    echo-param:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              name: message
```

#### `tests/integration/testdata/solution-provider/parent-resolver.yaml`

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-resolver
  version: 1.0.0
spec:
  resolvers:
    child-data:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./child.yaml"
              inputs:
                message: "passed from parent"
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.resolvers"
```

#### `tests/integration/testdata/solution-provider/parent-action.yaml`

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parent-action
  version: 1.0.0
spec:
  resolvers:
    msg:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "from parent"
  workflow:
    actions:
      run-child:
        provider: solution
        inputs:
          source: "./child.yaml"
          inputs:
            message:
              expr: "_.msg"
```

#### `tests/integration/testdata/solution-provider/circular-a.yaml`

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: circular-a
  version: 1.0.0
spec:
  resolvers:
    data:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./circular-b.yaml"
```

#### `tests/integration/testdata/solution-provider/circular-b.yaml`

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: circular-b
  version: 1.0.0
spec:
  resolvers:
    data:
      type: any
      resolve:
        with:
          - provider: solution
            inputs:
              source: "./circular-a.yaml"
```

**Integration test cases:**

| # | Test Name | Description | Assertion |
|---|-----------|-------------|-----------|
| 1 | Resolver composition | Parent loads child via `provider: solution` in resolver | `child-data.greeting == "hello from child"` |
| 2 | Parameter passthrough | Parent passes `inputs` to child | `child-data.echo-param == "passed from parent"` |
| 3 | Workflow composition | Parent invokes child as action | `__actions.run-child.results.status == "success"` |
| 4 | Circular reference | Two solutions reference each other | Error contains "circular reference detected" |
| 5 | Transform integration | Parent uses CEL transform on solution output | Extracted resolver values accessible |
| 6 | Dry-run mode | `--dry-run` flag with solution provider | No execution, mock envelope returned |

---

### Phase 6: Documentation and Provider Listing

**Goal:** Ensure the solution provider is discoverable and documented.

**Tasks:**

1. **Add to `builtin.ProviderNames()`** — Include `"solution"` in the list returned by `ProviderNames()` in `pkg/provider/builtin/builtin.go`. Add a comment explaining it's registered at CLI wiring time, not in `registerAllToRegistry()`.

2. **Provider examples** — Add example solution files to `examples/solutions/` demonstrating composition patterns:
   - `examples/solutions/composition/parent.yaml`
   - `examples/solutions/composition/child.yaml`

3. **CLI help** — Verify `scafctl explain provider solution` works after registration.

---

## File Summary

### New Files

| File | Phase | Purpose |
|------|-------|---------|
| `pkg/provider/builtin/solutionprovider/context.go` | 1 | Ancestor stack and depth context helpers |
| `pkg/provider/builtin/solutionprovider/context_test.go` | 1 | Context helper tests |
| `pkg/provider/builtin/solutionprovider/envelope.go` | 2 | Envelope construction for both capabilities |
| `pkg/provider/builtin/solutionprovider/envelope_test.go` | 2 | Envelope tests |
| `pkg/provider/builtin/solutionprovider/solution.go` | 3 | Provider struct, Descriptor(), Execute() |
| `pkg/provider/builtin/solutionprovider/solution_test.go` | 3 | Unit tests with mock loader |
| `tests/integration/testdata/solution-provider/child.yaml` | 5 | Child solution for integration tests |
| `tests/integration/testdata/solution-provider/parent-resolver.yaml` | 5 | Parent resolver composition test |
| `tests/integration/testdata/solution-provider/parent-action.yaml` | 5 | Parent action composition test |
| `tests/integration/testdata/solution-provider/circular-a.yaml` | 5 | Circular reference test (A) |
| `tests/integration/testdata/solution-provider/circular-b.yaml` | 5 | Circular reference test (B) |
| `examples/solutions/composition/parent.yaml` | 6 | Example: solution composition |
| `examples/solutions/composition/child.yaml` | 6 | Example: child solution |

### Modified Files

| File | Phase | Change |
|------|-------|--------|
| `pkg/cmd/scafctl/run/solution.go` | 4 | Register solution provider after registry creation |
| `pkg/provider/builtin/builtin.go` | 6 | Add `"solution"` to `ProviderNames()` with comment |
| `tests/integration/cli_test.go` | 5 | Add integration test cases |

---

## Implementation Order and Dependencies

```
Phase 1: Context Helpers
  └── No dependencies

Phase 2: Envelope Construction
  └── No dependencies

Phase 3: Provider Core
  ├── Depends on Phase 1 (ancestor stack)
  └── Depends on Phase 2 (envelope builders)

Phase 4: CLI Registration
  └── Depends on Phase 3 (provider exists)

Phase 5: Integration Tests
  ├── Depends on Phase 3 (provider exists)
  └── Depends on Phase 4 (provider registered)

Phase 6: Documentation
  └── Depends on Phase 4 (provider registered)
```

Phases 1 and 2 can be implemented in parallel. Phase 3 depends on both. Phases 4–6 are sequential.

---

## Risk Areas and Mitigations

### Registry Circular Reference

**Risk:** The solution provider holds a reference to the registry it's registered in.

**Mitigation:** This is safe because:
- The registry is a lookup table, not an ownership structure
- Provider execution is always parent → child (forward only)
- The ancestor stack in context prevents actual circular execution
- Go garbage collection handles the reference cycle

### Resolver Executor API Surface

**Risk:** `resolver.Executor` may not expose a clean way to extract resolver data after execution.

**Mitigation:** Study the existing `run solution` command in `pkg/cmd/scafctl/run/solution.go` — it already does exactly this flow (create executor → execute → extract data). Mirror that pattern.

### Action Executor API Surface

**Risk:** `action.Executor` may have a different interface for retrieving workflow results.

**Mitigation:** The action executor returns an `ExecutionResult`. Study its shape in `pkg/action/executor.go` to understand what's available for envelope construction.

### Context Keys and Isolation

**Risk:** Forgetting to strip a context value, leaking parent state to sub-solution.

**Mitigation:**
- `buildIsolatedContext` is the single point of context construction
- Unit tests explicitly assert sub-solution cannot access parent resolver values
- Integration tests verify end-to-end isolation

### Input Resolution for `source` and `inputs`

**Risk:** The `source` field is a `ValueRef` in the design, but provider inputs are already resolved by the executor before reaching `Execute()`. The existing `InputResolver` in the resolver/action executor handles `ValueRef` resolution.

**Mitigation:** The provider's `Decode` function receives already-resolved values. `source` will be a plain string by the time `Execute` is called. The `ExtractDependencies` function handles the pre-resolution dependency analysis against the raw input map (which may contain `ValueRef` structures).

---

## Verification Checklist

After implementation, verify:

- [ ] `go build ./...` succeeds
- [ ] `go test ./pkg/provider/builtin/solutionprovider/...` passes
- [ ] `go test ./tests/integration/...` passes (new test cases)
- [ ] `golangci-lint run` passes
- [ ] `scafctl run solution tests/integration/testdata/solution-provider/parent-resolver.yaml` produces correct output
- [ ] `scafctl run solution tests/integration/testdata/solution-provider/circular-a.yaml` produces clear circular reference error
- [ ] `scafctl run solution --dry-run tests/integration/testdata/solution-provider/parent-resolver.yaml` returns mock envelope
- [ ] `scafctl explain provider solution` shows provider documentation
