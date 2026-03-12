# soldiff — Solution Structural Comparison

Package `soldiff` provides structural comparison of two scafctl solutions, producing a detailed diff of metadata, resolver, action, and testing changes.

## Features

- **Structural diff** — Compares `metadata`, `spec.resolvers`, `spec.workflow.actions`, `spec.workflow.finally`, and `spec.testing.cases` sections
- **Change classification** — Each difference is typed as `added`, `removed`, or `changed`
- **Deterministic output** — Changes are sorted by field path for stable, reproducible results
- **File-based comparison** — `CompareFiles` loads and compares solutions from disk
- **In-memory comparison** — `Compare` works directly with `*solution.Solution` values
- **Summary counts** — `Result.Summary` provides aggregate totals by change type

## API

### Types

```go
// Change represents a single structural difference.
type Change struct {
    Field    string // Dot-separated path, e.g. "spec.resolvers.appName"
    Type     string // "added", "removed", or "changed"
    OldValue any    // Previous value (nil for additions)
    NewValue any    // New value (nil for removals)
}

// Result contains the full diff output.
type Result struct {
    PathA   string   // Path to the first solution file
    PathB   string   // Path to the second solution file
    Changes []Change // All structural differences
    Summary Summary  // Aggregate counts
}

// Summary counts changes by type.
type Summary struct {
    Total   int
    Added   int
    Removed int
    Changed int
}
```

### Functions

#### `Compare(solA, solB *solution.Solution) *Result`

Compares two in-memory solutions structurally. Returns a `Result` with all changes sorted by field path.

```go
result := soldiff.Compare(solutionA, solutionB)
for _, c := range result.Changes {
    fmt.Printf("%s %s: %v → %v\n", c.Type, c.Field, c.OldValue, c.NewValue)
}
```

#### `CompareFiles(ctx context.Context, pathA, pathB string) (*Result, error)`

Loads two solution YAML files from disk and compares them. Populates `Result.PathA` and `Result.PathB`.

```go
result, err := soldiff.CompareFiles(ctx, "v1/solution.yaml", "v2/solution.yaml")
if err != nil {
    return err
}
fmt.Printf("Total changes: %d (added: %d, removed: %d, changed: %d)\n",
    result.Summary.Total, result.Summary.Added, result.Summary.Removed, result.Summary.Changed)
```

## What Gets Compared

| Section | Fields Compared |
|---------|----------------|
| `metadata` | `name`, `description`, `version` |
| `spec.resolvers.<name>` | Existence (added/removed), `type`, `description`, primary provider |
| `spec.workflow.actions.<name>` | Existence, `provider`, `description` |
| `spec.workflow.finally.<name>` | Existence |
| `spec.workflow` | Added/removed as a whole section |
| `spec.testing.cases.<name>` | Existence |
| `spec.testing` | Added/removed as a whole section |

## CLI Usage

The `soldiff` package powers the `solution diff` CLI command:

```bash
# Compare two solution files
scafctl solution diff v1/solution.yaml v2/solution.yaml

# Output as JSON
scafctl solution diff v1/solution.yaml v2/solution.yaml -o json
```

## Testing

```bash
go test ./pkg/soldiff/...
```

The test suite includes unit tests for all comparison scenarios (additions, removals, modifications, identical solutions, nil edges) and benchmarks for large solution diffs.
