# Resolvers Design

## Overview
Resolvers gather, normalize, validate, and transform input data into a coherent configuration context used by templates and actions. They bridge solution manifests with dynamic sources (filesystem, environment, HTTP, catalogs, providers) and evaluate expressions via CEL extensions to produce concrete values.

This document defines the resolver model, lifecycle, configuration evaluation semantics, data flow, error handling, logging, and extensibility.

## Goals
- Establish a clear, predictable resolver chain and lifecycle.
- Integrate CEL and template evaluation for dynamic configuration.
- Support composable, testable resolvers with strong validation.
- Enable extensibility for new data sources and transformation rules.

## Non-Goals
- Replace guide content; this complements existing guides with deeper design rationale.
- Specify provider implementations; those are documented separately.

## Terminology
- Resolver: A unit that produces values by reading inputs and applying rules.
- Resolver Chain: Ordered sequence of resolver steps that produce the final context.
- Context: The resolved data map (and refs) available to templates and actions.
- Solution: Manifest describing resolvers, templates, actions, and metadata.

## Schema
The resolver schema is defined in [docs/schemas/resolver-schema.md](docs/schemas/resolver-schema.md). Solution manifests reference resolvers using this schema for validation and tooling support.

Typical fields:
- `id`: Unique resolver identifier for chain ordering and referencing.
- `type`: Resolver type (e.g., filesystem, environment, HTTP, catalog, computed).
- `config`: Parameters, often with CEL expressions and template fragments.
- `dependsOn`: Optional dependencies to enforce ordering and enable partial recomputation.
- `outputs`: Named values emitted into the context.

## Lifecycle & Phases
Resolvers participate in the broader scaffolding lifecycle and precede action execution:
1. Discover: Identify sources (files, env, URLs, catalogs) declared in the solution.
2. Normalize: Coerce input shapes into predictable, typed structures.
3. Validate: Enforce schema constraints; reject malformed or incompatible data.
4. Enrich: Augment with derived values (e.g., defaults, lookups via providers).
5. Transform: Evaluate CEL expressions and templates to produce concrete outputs.
6. Publish: Write outputs into the shared context for downstream templates/actions.

See [docs/guides/02-resolver-pipeline.md](docs/guides/02-resolver-pipeline.md) for an end-to-end walkthrough of the resolver chain and [docs/guides/05-expression-language.md](docs/guides/05-expression-language.md) for expression details.

## Configuration Evaluation
Resolvers frequently require dynamic evaluation:
- CEL Expressions: Implemented via [pkg/celexp/celexp.go](pkg/celexp/celexp.go) with custom extensions in `pkg/celexp/ext/` (arrays, strings, filepath, debug, etc.). Overloads follow `strings.ReplaceAll(funcName, ".", "_")` conventions with explicit type checks.
- Conversion Utilities: [pkg/celexp/conversion](pkg/celexp/conversion) provides safe conversions between CEL and Go types.
- Templates: [pkg/gotmpl/gotmpl.go](pkg/gotmpl/gotmpl.go) renders text templates for values that need structured generation.

Evaluation occurs during Transform. The chain ensures inputs are validated before evaluation and that outputs are concrete and type-safe.

## Data Flow & Outputs
- Inputs: Solution manifest definitions, external sources (filesystem/env/HTTP/catalogs), and prior resolver outputs.
- Context Map: Resolvers publish named outputs into a shared map accessible to templates and actions.
- Refs: Structured references (see [pkg/celexp/refs.go](pkg/celexp/refs.go)) enable stable access paths and inter-step value linking.
- Copying & Merge: Prefer `maps.Copy()` for merging maps to maintain clarity and avoid mutation bugs.

## Ordering & Orchestration
- Chain Order: Resolvers can be executed in a defined order. Simple chains run sequentially; complex ones may use `dependsOn` semantics.
- DAG Integration: While actions are orchestrated via the DAG runner ([pkg/dag/dag.go](pkg/dag/dag.go)), resolver ordering can also be represented as a graph to detect cycles and enable parallel evaluation when safe.
- Cycle Detection: Resolver graphs must be acyclic; cycles are rejected.

## Dependency Discovery via AST
The resolver chain constructs a DAG by combining explicit `dependsOn` declarations with implicit dependencies discovered via Abstract Syntax Tree (AST) analysis:

- **AST Parsing**: CEL expressions and template fragments within resolver `config` fields are parsed into ASTs to identify variable and output references (e.g., if a config contains `${other_resolver.value}` or a CEL reference to another resolver's output).
- **Implicit Dependencies**: The AST walker traverses the tree to extract all referenced resolver IDs or output names, automatically establishing dependency edges.
- **Explicit Declaration**: Manifest authors can also declare `dependsOn` fields to enforce ordering beyond what the AST discovers, supporting non-obvious dependencies (e.g., ensuring a resolver runs after another for side-effect ordering).
- **Merged Graph**: Implicit and explicit dependencies are merged; cycles are detected and rejected. The resulting DAG ensures topological ordering and enables safe parallelization.
- **Validation**: AST analysis helps catch references to undefined resolvers or outputs early, improving error messages and tooling support.

## Internal System Namespace (`_._`)
The context includes a reserved internal namespace `_._` that holds system metadata and diagnostics. It is accessible to resolvers, templates, and actions via CEL expressions (e.g., `_._.execution.version`, `_._.auth.azure.claims`).

**Always Present** (deterministic, populated every execution):
- `execution`: Binary metadata, arguments, working directory, environment, user, start/current timestamps, and results summary.
- `auth`: Authentication providers keyed by provider name, each with active status, decoded JWT claims (payload only, no token or signature), and relevant headers (useful for conditional logic based on logged-in user or service account).
- `solution`: Solution manifest metadata (name, version, maintainers, tags).

**Conditional** (populated when diagnostics/profiling are enabled via `--diagnostics` or `--debug` flags):
- `pprof`: CPU/memory profiling data if profiling is active.
- `resolvers`: Execution summary including all resolver IDs, failed resolvers, bottleneck analysis, and DAG diagram (mermaid format).
- `references`: Detailed resolver-to-resolver dependency graph and variable references (from AST analysis).

**Diagnostic Levels**: `_._` population follows the same diagnostic/debug levels as logging:
- **Baseline** (always): execution, auth, solution.
- **Level 1** (`--debug` or default diagnostics): add resolver execution summary.
- **Level 2** (`--debug -v` or higher): add references and detailed bottleneck info.
- **Level 3** (`--debug -vv` or profiling enabled): add pprof data and mermaid diagrams.

**Determinism Guarantee**: All keys in `_._` are deterministic and stable across runs with identical inputs. Ordering within lists (e.g., resolvers) follows declaration order in the solution manifest.

**Access Pattern**: Downstream resolvers and actions can reference `_._` to:
- Gate execution on binary version (e.g., `_._.execution.version >= "1.4.0"`).
- Conditionally use auth claims (e.g., `has(_._.auth.azure) && _._.auth.azure.active ? _._.auth.azure.claims.roles : []`).
- Check provider availability (e.g., `has(_._.auth.github)`).
- Detect and report bottlenecks or failed resolvers for observability.

**Security Note**: `_._.auth` contains decoded JWT claims (payload only). The original token string and signature are not stored. Claims may contain sensitive identity information; avoid logging or exposing this namespace in non-secure contexts.

**Example Output**: Running `scafctl run --debug` with a local solution.yaml produces resolved context including `_._`:

```yaml
# Resolved Context:
_:
  myConfig:
    name: "sample-app"
    version: "1.0.0"
    port: 8080
  
  environment:
    name: "production"
    region: "us-east-1"
  
  _:
    execution:
      binary:
        name: "scafctl.exe"
        path: "C:\\Users\\abaker9\\bin\\scafctl.exe"
        version: "1.4.2"
        commit: "a3b5c7d"
        buildTime: "2025-12-20T14:23:11Z"
      args:
        - "run"
        - "--debug"
      workingDirectory: "C:\\Users\\abaker9\\projects\\my-solution"
      environment:
        PATH: "C:\\Windows\\system32;C:\\Users\\abaker9\\bin"
        SCAFCTL_CONFIG: "C:\\Users\\abaker9\\.scafctl\\config.yaml"
      user:
        name: "abaker9"
        uid: 1001
      startTime: "2025-12-25T10:30:15Z"
      currentTime: "2025-12-25T10:30:18Z"
      duration: "3.2s"
    
    auth:
      azure:
        active: true
        claims:
          aud: "api://scafctl"
          iss: "https://login.microsoftonline.com/tenant-id/v2.0"
          sub: "user-object-id"
          name: "Alex Baker"
          email: "abaker9@example.com"
          roles:
            - "Solution.Admin"
            - "Catalog.Publisher"
          exp: 1735218615
        headers:
          Authorization: "Bearer <token-redacted>"
      github:
        active: false
    
    solution:
      name: "my-solution"
      version: "2.1.0"
      maintainers:
        - name: "Alex Baker"
          email: "abaker9@example.com"
      tags:
        - "infrastructure"
        - "terraform"
    
    resolvers:
      all:
        - id: "myConfig"
          type: "filesystem"
          status: "success"
          duration: "125ms"
        - id: "environment"
          type: "computed"
          status: "success"
          duration: "5ms"
      failed: []
      bottlenecks:
        - id: "myConfig"
          duration: "125ms"
          reason: "HTTP fetch with retry"
      diagram: |
        graph TD
          A[myConfig] --> B[environment]
          B --> C[actions]
      references:
        myConfig:
          produces:
            - "_.myConfig.name"
            - "_.myConfig.version"
            - "_.myConfig.port"
          consumedBy:
            - "environment"
        environment:
          dependsOn:
            - "myConfig"
          produces:
            - "_.environment.name"
            - "_.environment.region"
```

Without `--debug`, only the always-present fields (`execution`, `auth`, `solution`) appear in `_._`.

## Providers & External Sources
Resolvers may query providers (e.g., HTTP clients, catalogs, VCS) to enrich or validate inputs:
- HTTP Client: See [pkg/httpc/client.go](pkg/httpc/client.go) for retries, caching, and timeouts.
- Catalog & Providers: Architectural patterns are documented in [docs/design/providers.md](docs/design/providers.md) and [docs/design/catalog.md](docs/design/catalog.md).

## Error Handling
- Validation Errors: Fail fast during Validate; provide actionable messages.
- Evaluation Errors: Return explicit errors (`fmt.Errorf("context: %w", err)`) instead of panicking.
- Aggregation: Collect resolver errors for user reporting; halt on critical failures.

## Logging
Use the project’s structured logging conventions:
- Logger: `logr` with `zapr`. Create via `logger.Get(verbosity)` and attach to context using `logger.WithLogger(ctx, lgr)`.
- Contextual Keys: Include keys from [pkg/logger/logger.go](pkg/logger/logger.go) like `RootCommandKey`, `CommitKey`, `VersionKey` where relevant.
- Verbosity: Negative levels (e.g., `-1`) for debug detail.

## Metrics & Profiling
- Metrics: Emit counters/timers via [pkg/metrics/metrics.go](pkg/metrics/metrics.go) where applicable.
- Profiling: Enable CPU/memory profiling via hidden flags; manage lifecycle with [pkg/profiler](pkg/profiler/profiler.go). Shutdown errors are logged and non-fatal.

## Security Considerations
- Input Sanitization: Treat all external inputs as untrusted; validate aggressively.
- Template Safety: Avoid executing arbitrary code; limit evaluation to supported CEL functions and templates.
- Network Hygiene: Use timeouts, retries, and caching via `httpc`.

## Testing Strategy
- Unit Tests: Co-locate `*_test.go` with resolver implementations; use `testify/assert` and `testify/require`.
- CEL Integration: Mirror patterns from `pkg/celexp` tests to verify expression behavior.
- Benchmarks: Use `BenchmarkResolver_...` naming for performance-sensitive resolvers.

## Extensibility Guidelines
- New Resolver Types: Define clear `type` identifiers and update the resolver schema as needed; document configs and outputs.
- CEL Functions: Add `celexp.ExtFunction` implementations with examples and explicit type errors using `types.NewErr`.
- Providers: Extend via functional options and interfaces for testability and mockability.

## Example (Conceptual)
A resolver that loads configuration from a URL and merges defaults:
1. Discover: Read URL from solution config; resolve env var fallbacks.
2. Fetch: Use `httpc` with retries and caching to download JSON.
3. Normalize: Parse into a typed structure; coerce missing fields to defaults.
4. Validate: Enforce schema constraints (required fields, enums).
5. Transform: Apply CEL expressions to compute derived values; render small templates as needed.
6. Publish: Emit `outputs` (e.g., `service.name`, `service.port`) into the context map.

## Future Enhancements
- Incremental Caching: Cache resolver outputs keyed by input checksums to skip redundant work.
- Typed Context: Stronger typing and validation for published outputs to improve safety and tooling.
- Interactive Inputs: Bubbletea-based forms to collect missing inputs interactively.

## References
- Guides: [docs/guides/02-resolver-pipeline.md](docs/guides/02-resolver-pipeline.md), [docs/guides/05-expression-language.md](docs/guides/05-expression-language.md), [docs/guides/07-templates.md](docs/guides/07-templates.md)
- Schema: [docs/schemas/resolver-schema.md](docs/schemas/resolver-schema.md)
- Solution: [pkg/solution/solution.go](pkg/solution/solution.go)
- CEL: [pkg/celexp/celexp.go](pkg/celexp/celexp.go), [pkg/celexp/refs.go](pkg/celexp/refs.go)
- Templates: [pkg/gotmpl/gotmpl.go](pkg/gotmpl/gotmpl.go)
- HTTP: [pkg/httpc/client.go](pkg/httpc/client.go)