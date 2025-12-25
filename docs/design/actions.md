# Actions Design

## Overview
Actions are the execution units that perform work during scaffolding and runtime operations in scafctl. They are orchestrated by the dependency graph engine (`pkg/dag`) and configured via solution manifests. Actions consume evaluated configuration (including CEL expressions and template outputs), interact with providers and the filesystem, and produce outputs that feed subsequent steps.

This document defines the conceptual model, lifecycle, execution semantics, error handling, and extensibility strategy for actions.

## Goals
- Provide a clear, consistent action model and lifecycle.
- Define how actions integrate with the resolver chain and DAG runner.
- Document schema, logging, metrics, and error handling expectations.
- Enable extensibility via providers, plugins, and CEL-powered configuration.

## Non-Goals
- Replace the existing guides; instead, complement them with deeper architecture details.
- Specify provider implementations in full; providers remain decoupled components documented separately.

## Terminology
- Action: A unit of work described in a solution manifest and executed by the runner.
- Step: A node in the DAG associated with an action; may depend on other steps.
- Runner: The DAG engine (`pkg/dag`) that schedules, executes, and records results.
- Solution: A manifest describing scaffolding configuration, resolvers, templates, and actions (`pkg/solution`).
- Provider: A pluggable module or external service client used by actions (see design/providers.md).

## Schema
The canonical schema for actions is defined in [docs/schemas/action-schema.md](docs/schemas/action-schema.md). Solution manifests reference actions using this schema, enabling validation and predictable execution.

Key fields typically include:
- `id`: Unique identifier used for DAG node naming and dependency mapping.
- `type`: Logical action type (e.g., file operations, provider calls, templating).
- `dependsOn`: Dependencies that form the DAG edges.
- `config`: Parameters for the action, often evaluated via CEL and templates.
- `outputs`: Named values produced by the action and made available to downstream steps.

## Lifecycle & Phases
Actions participate in the broader scaffolding lifecycle:
1. Resolve: Inputs are collected, normalized, and transformed based on solution rules.
2. Transform: CEL expressions and templates are evaluated to produce concrete configs.
3. Execute: Actions run according to DAG ordering, producing outputs and side effects.

See [docs/guides/02-resolver-pipeline.md](docs/guides/02-resolver-pipeline.md) and [docs/guides/04-action-orchestration.md](docs/guides/04-action-orchestration.md) for lifecycle context.

## Execution Model (DAG)
The runner (`pkg/dag`) executes actions as DAG nodes:
- Dependency Resolution: `dependsOn` establishes edges to enforce ordering and enable parallelism where safe.
- Cycle Detection: The runner rejects cyclic graphs to ensure finite execution.
- Scheduling: Independent nodes may run concurrently; dependent nodes wait for prerequisites.
- Results & Timing: `RunnerResults` track per-node start/end times, status, and errors for reporting.

## Dependency Discovery via AST
The action DAG is constructed by combining explicit `dependsOn` declarations with implicit dependencies discovered via Abstract Syntax Tree (AST) analysis:

- **AST Parsing**: CEL expressions and templates within action `config` fields are parsed into ASTs to identify variable references and output dependencies (e.g., if an action config references output from a prior action).
- **Implicit Dependencies**: The AST walker extracts all referenced action IDs and resolver outputs, automatically establishing dependency edges without requiring manual declaration.
- **Explicit Declaration**: Authors can declare `dependsOn` fields to enforce ordering beyond AST discovery, supporting side-effect ordering or non-expression dependencies.
- **Merged Graph**: Implicit and explicit dependencies are unified. Cycles are detected and rejected. The result is a topologically sorted DAG enabling safe parallelization.
- **Validation**: AST analysis catches references to undefined actions or missing outputs early, reducing runtime failures.

## Configuration Evaluation
Actions often reference dynamic configuration:
- CEL Expressions: Via `pkg/celexp`, actions can evaluate expressions with custom extensions (arrays, strings, filepath, debug, etc.).
- Conversion Utilities: `pkg/celexp/conversion` ensures type-safe bridging between CEL and Go.
- Templates: `pkg/gotmpl` integrates Go templates, enabling text generation and file content synthesis.

Evaluation precedes execution; the runner feeds actions with already-resolved, concrete values.

## Providers & Plugins
Actions use providers to interact with external systems (e.g., HTTP, VCS, registries). The provider architecture is documented in [docs/design/providers.md](docs/design/providers.md). Plugins may add new action types or provider capabilities without modifying core runner logic.

## Error Handling
- Fail Fast per Node: An action failure marks its node as failed; downstream dependent nodes are skipped or marked blocked.
- Aggregation: The runner aggregates errors for final reporting and exit codes.
- Wrapping: Use `fmt.Errorf("context: %w", err)` for error propagation in implementations.
- Non-Panic: Actions should return errors instead of panicking; initialization may panic only in `main` when appropriate.

## Logging
Actions use the `logr` interface with `zapr` per project conventions:
- Verbosity: `logger.Get(verbosity)` supports negative levels for debug (e.g., `-1`).
- Context: Use `logger.WithLogger(ctx, lgr)` and retrieve via `logger.FromContext(ctx)`.
- Structured: Include keys defined in `logger/logger.go` (e.g., `RootCommandKey`, `CommitKey`, `VersionKey`).

## Metrics & Profiling
- Metrics: Actions can emit counters/timers via `pkg/metrics` when applicable.
- Profiling: CPU/memory profiling can be enabled via hidden CLI flags and managed using `pkg/profiler`. Errors on profiler shutdown are logged but non-fatal.

## Outputs & Data Flow
- Named Outputs: Actions publish outputs into the execution context for use by downstream nodes.
- Map Copying: Prefer `maps.Copy()` for copying maps over manual loops.
- Type Safety: Use conversion helpers and explicit erroring in CEL integrations (`types.NewErr`).

## Observability & Reporting
- RunnerResults: Records statuses, durations, and errors for each node and overall execution.
- Terminal Output: Styled via `lipgloss` and `bubbletea` components (planned) for a readable summary.
- CLI Integration: See [docs/cli/commands/run.md](docs/cli/commands/run.md) and related command docs for user interaction.

## Security Considerations
- Input Validation: Validate action configs against schema prior to execution.
- Template Safety: Treat templates and expressions as untrusted input; avoid executing arbitrary code.
- External Calls: Configure timeouts, retries, and caching via `pkg/httpc` where applicable.

## Testing Strategy
- Unit Tests: Implement `*_test.go` near action code using `testify/assert` and `testify/require`.
- CEL Integration Tests: Follow patterns outlined in `pkg/celexp` tests to validate expression behavior.
- Benchmarks: Use `Benchmark...` naming conventions for performance tests where needed.

## Extensibility Guidelines
- New Action Types: Define clear `type` identifiers, document configs, and update schema where appropriate.
- CEL Functions: Implement `celexp.ExtFunction` objects with proper `EnvOptions`, examples, and type checks.
- Providers: Extend via functional options and interfaces for testability.

## Example (Conceptual)
An action that writes a templated file:
1. Config contains a path (possibly via `filepath` CEL functions) and content derived from `gotmpl`.
2. The runner evaluates CEL and templates during Transform.
3. The action executes, writes the file, and outputs the created path and checksum.
4. Downstream actions depend on this output to continue (e.g., commit, upload, or validate).

## Future Enhancements
- Typed Output Contracts: Stronger typing for action outputs to improve validation and editor tooling.
- Interactive Forms: Bubbletea-based TUI to collect user inputs for actions at runtime.
- Rich Caching: Execution-level caching keyed by config checksums to skip redundant work.

## References
- Guides: [docs/guides/04-action-orchestration.md](docs/guides/04-action-orchestration.md)
- Schema: [docs/schemas/action-schema.md](docs/schemas/action-schema.md)
- Providers: [docs/design/providers.md](docs/design/providers.md)
- DAG: [pkg/dag/dag.go](pkg/dag/dag.go)
- Solution: [pkg/solution/solution.go](pkg/solution/solution.go)
- CEL: [pkg/celexp/celexp.go](pkg/celexp/celexp.go)
- Templates: [pkg/gotmpl/gotmpl.go](pkg/gotmpl/gotmpl.go)