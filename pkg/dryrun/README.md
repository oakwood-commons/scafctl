# dryrun — Structured WhatIf Report Generation

Package `dryrun` generates structured WhatIf reports for scafctl solutions, showing what a solution execution would do without performing any side effects.

## Features

- **WhatIf messages** — Each action includes a provider-generated description of what it would do with real resolved inputs (e.g., `Would execute command "./deploy.sh" via bash in /app`)
- **Real resolver data** — Resolvers execute normally (they're side-effect-free), providing real data for accurate WhatIf messages
- **Action plan** — Builds an execution-order action plan with phase assignments, WhatIf descriptions, deferred inputs, and dependencies
- **Verbose mode** — Optionally includes MaterializedInputs for detailed inspection
- **Warnings** — Collects issues like graph build failures
- **JSON/YAML-friendly** — All types carry `json` and `yaml` struct tags for serialization

## API

### Types

```go
// Report is the full structured WhatIf dry-run output.
type Report struct {
    DryRun       bool
    Solution     string
    Version      string
    HasWorkflow  bool
    ActionPlan   []WhatIfAction
    TotalActions int
    TotalPhases  int
    Warnings     []string
}

// WhatIfAction describes a single action in the WhatIf plan.
type WhatIfAction struct {
    Name               string
    Provider           string
    Description        string
    WhatIf             string            // Provider-generated description
    Phase              int
    Section            string            // "actions" or "finally"
    Dependencies       []string
    When               string
    MaterializedInputs map[string]any    // Only when Verbose is true
    DeferredInputs     map[string]string
}

// Options controls the dry-run generation.
type Options struct {
    Registry     *provider.Registry
    ResolverData map[string]any
    Verbose      bool
}
```

### Functions

#### `Generate(ctx context.Context, sol *solution.Solution, opts Options) (*Report, error)`

Builds a structured WhatIf report from a solution and pre-executed resolver data.

**Important:** Callers must execute resolvers normally *before* calling `Generate` and pass the results via `Options.ResolverData`. Resolvers are side-effect-free, so they should run with real execution (not mocked). The function uses the real resolver data to generate accurate WhatIf messages by calling each provider's `DescribeWhatIf` method.

```go
// 1. Execute resolvers normally (they're side-effect-free)
resolverData := executeResolvers(ctx, sol, params, registry)

// 2. Generate the WhatIf report
report, err := dryrun.Generate(ctx, sol, dryrun.Options{
    Registry:     registry,
    ResolverData: resolverData,
    Verbose:      false, // set true to include MaterializedInputs
})
if err != nil {
    return err
}

// 3. Use the report
fmt.Printf("Solution: %s (v%s)\n", report.Solution, report.Version)
fmt.Printf("Actions: %d across %d phases\n", report.TotalActions, report.TotalPhases)
for _, action := range report.ActionPlan {
    fmt.Printf("  What if: [%s] %s\n", action.Name, action.WhatIf)
}
```

## Report Generation Flow

```text
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│  Execute     │     │  dryrun      │     │   Report     │
│  Resolvers   │────>│  .Generate() │────>│   with       │
│  (real data) │     │              │     │   WhatIf     │
└──────────────┘     └──────┬───────┘     └──────────────┘
                            │
                     ┌──────┴───────┐
                     │ Build action │
                     │ graph, call  │
                     │ DescribeWhat │
                     │ If() per     │
                     │ action       │
                     └──────────────┘
```

## CLI Usage

```bash
# WhatIf table output (default)
scafctl run solution -f ./my-solution.yaml --dry-run

# JSON output
scafctl run solution -f ./my-solution.yaml --dry-run -o json

# Verbose mode (includes MaterializedInputs)
scafctl run solution -f ./my-solution.yaml --dry-run --verbose
```

## Testing

```bash
go test ./pkg/dryrun/... -v
go test ./pkg/dryrun/... -bench=.
```
