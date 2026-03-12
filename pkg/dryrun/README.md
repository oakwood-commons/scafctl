# dryrun — Structured Dry-Run Report Generation

Package `dryrun` generates structured dry-run reports for scafctl solutions, showing what a solution execution would do without performing any side effects.

## Features

- **Unified report** — Combines resolver previews and action planning in a single structured output
- **Resolver status** — Shows each resolver's dry-run value and resolution status
- **Action plan** — Builds an execution-order action plan with phase assignments, materialized inputs, deferred inputs, and dependencies
- **Mock behaviors** — Reports what each provider would do in dry-run mode
- **Warnings** — Collects issues like unresolved resolvers or graph build failures
- **JSON/YAML-friendly** — All types carry `json` and `yaml` struct tags for serialization

## API

### Types

```go
// Report is the full structured dry-run output.
type Report struct {
    DryRun        bool
    Solution      string
    Version       string
    HasResolvers  bool
    HasWorkflow   bool
    Parameters    map[string]any
    Resolvers     map[string]Resolver
    ActionPlan    []Action
    TotalActions  int
    TotalPhases   int
    MockBehaviors []MockBehavior
    Warnings      []string
}

// Resolver describes a single resolver's dry-run result.
type Resolver struct {
    Value  any
    Status string // "resolved" or "not-resolved"
    DryRun bool
}

// Action describes a single action in the dry-run plan.
type Action struct {
    Name               string
    Provider           string
    Description        string
    MaterializedInputs map[string]any
    DeferredInputs     map[string]string
    Dependencies       []string
    When               string
    Section            string // "actions" or "finally"
    Phase              int
    MockBehavior       string
}

// MockBehavior describes what a provider does in dry-run mode.
type MockBehavior struct {
    Provider     string
    MockBehavior string
}

// Options controls the dry-run generation.
type Options struct {
    Params       map[string]any
    Registry     *provider.Registry
    ResolverData map[string]any
}
```

### Functions

#### `Generate(ctx context.Context, sol *solution.Solution, opts Options) (*Report, error)`

Builds a structured dry-run report from a solution and pre-executed resolver data.

**Important:** Callers must execute resolvers with dry-run mode enabled *before* calling `Generate` and pass the results via `Options.ResolverData`. The function does not execute resolvers itself — it assembles the report from provided data.

```go
// 1. Execute resolvers in dry-run mode (done by caller)
resolverData := map[string]any{
    "greeting": "Hello!",
    "port":     8080,
}

// 2. Generate the report
report, err := dryrun.Generate(ctx, sol, dryrun.Options{
    Params:       params,
    Registry:     registry,
    ResolverData: resolverData,
})
if err != nil {
    return err
}

// 3. Use the report
fmt.Printf("Solution: %s (v%s)\n", report.Solution, report.Version)
fmt.Printf("Actions: %d across %d phases\n", report.TotalActions, report.TotalPhases)
for _, action := range report.ActionPlan {
    fmt.Printf("  [%d] %s (%s)\n", action.Phase, action.Name, action.Provider)
}
```

## Report Generation Flow

```text
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  Execute     │     │  dryrun      │     │   Report     │
│  resolvers   │ ──► │  .Generate() │ ──► │   (JSON/     │
│  (dry-run)   │     │              │     │    YAML)     │
└──────────────┘     └──────────────┘     └──────────────┘
        │                    │
        │                    ├─ Resolver entries (value, status)
        │                    ├─ Action plan (phases, deps, inputs)
        │                    ├─ Mock behaviors
        │                    └─ Warnings
        │
        └─ ResolverData map[string]any
```

## CLI Usage

The `dryrun` package powers the `--dry-run` flag on solution and resolver commands:

```bash
# Dry-run a full solution (resolvers + action plan)
scafctl run solution -f solution.yaml --dry-run

# Dry-run with JSON output
scafctl run solution -f solution.yaml --dry-run -o json

# Dry-run resolvers only
scafctl run resolver -f solution.yaml --dry-run
```

## Testing

```bash
go test ./pkg/dryrun/...
```

The test suite includes unit tests for report generation (resolvers, actions, mock behaviors, edge cases) and benchmarks.
