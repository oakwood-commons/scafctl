---
title: "Solution Diff Tutorial"
weight: 85
---

This tutorial covers comparing two solution files structurally to understand what changed between versions. The `soldiff` package detects additions, removals, and modifications across metadata, resolvers, actions, and testing sections.

## Overview

Solution diffing answers the question: **"What structurally changed between two versions of a solution?"** Unlike `git diff` which shows text changes, `soldiff` understands the solution schema and reports meaningful structural differences.

```text
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│ solution-v1  │     │   soldiff    │     │    Result    │
│   .yaml      │ ──► │  .Compare()  │ ──► │  (changes,   │
│              │     │              │     │   summary)   │
│ solution-v2  │ ──► │              │     │              │
│   .yaml      │     │              │     │              │
└──────────────┘     └──────────────┘     └──────────────┘
```

## When to Use Solution Diff

| Use Case | Scenario |
|----------|----------|
| **Code review** | Review structural impact of solution YAML changes before merging |
| **Configuration drift** | Detect when a deployed solution has drifted from the expected baseline |
| **Refactoring validation** | Confirm a refactor didn't accidentally add/remove resolvers or actions |
| **Version comparison** | Compare v1 and v2 of a solution to document what changed |

## CLI Usage

### Basic Comparison

```bash
scafctl solution diff solution-v1.yaml solution-v2.yaml
```

Output:
```
Solution Diff: solution-v1.yaml ↔ solution-v2.yaml

Changes (5):
  changed  metadata.version: "1.0.0" → "2.0.0"
  changed  metadata.description: "Baseline version" → "Updated version"
  changed  spec.resolvers.replicas.description: "Number of replicas" → "Number of replicas (scaled up)"
  added    spec.resolvers.health_check_url
  added    spec.workflow.actions.verify

Summary: 5 total | 2 added | 0 removed | 3 changed
```

### JSON Output

```bash
scafctl solution diff solution-v1.yaml solution-v2.yaml -o json
```

Returns structured JSON with `changes` array and `summary` object — useful for CI pipelines and programmatic processing.

## Example Walkthrough

### Step 1: Create Two Solution Versions

Use the provided examples:

```bash
# View baseline solution
cat examples/soldiff/solution-v1.yaml

# View updated solution
cat examples/soldiff/solution-v2.yaml
```

### Step 2: Compare Them

```bash
scafctl solution diff examples/soldiff/solution-v1.yaml examples/soldiff/solution-v2.yaml
```

The diff shows:
- `metadata.version` changed from `1.0.0` to `2.0.0`
- `metadata.description` changed
- `spec.resolvers.replicas.description` changed
- `spec.resolvers.health_check_url` was added (new resolver)
- `spec.workflow.actions.verify` was added (new action)

### Step 3: Use in CI

In a CI pipeline, use JSON output to assert no unexpected changes:

```bash
# Fail if any resolvers were removed
diff_output=$(scafctl solution diff baseline.yaml current.yaml -o json)
removed=$(echo "$diff_output" | jq '.summary.removed')
if [ "$removed" -gt 0 ]; then
  echo "ERROR: Resolvers were removed!"
  exit 1
fi
```

## What Gets Compared

The diff compares these structural elements:

| Section | What's Compared |
|---------|----------------|
| `metadata` | `name`, `description`, `version` |
| `spec.resolvers` | Resolver existence, `type`, `description`, primary provider |
| `spec.workflow.actions` | Action existence, `provider`, `description` |
| `spec.workflow.finally` | Action existence |
| `spec.workflow` | Added/removed as a whole |
| `spec.testing.cases` | Test case existence |
| `spec.testing` | Added/removed as a whole |

## Programmatic Usage

```go
import "github.com/oakwood-commons/scafctl/pkg/soldiff"

// Compare from files
result, err := soldiff.CompareFiles(ctx, "v1.yaml", "v2.yaml")
if err != nil {
    return err
}

// Or compare in-memory solutions
result := soldiff.Compare(solutionA, solutionB)

// Inspect results
fmt.Printf("Changes: %d\n", result.Summary.Total)
for _, c := range result.Changes {
    fmt.Printf("  %s %s\n", c.Type, c.Field)
}
```

## Combining with Snapshots

Solution diff compares *structure* (YAML schema), while snapshot diff compares *runtime output* (resolver values, timing). Use both for comprehensive change analysis:

```bash
# 1. Compare structure
scafctl solution diff v1.yaml v2.yaml

# 2. Compare runtime behavior
scafctl run resolver -f v1.yaml --snapshot --snapshot-file=before.json
scafctl run resolver -f v2.yaml --snapshot --snapshot-file=after.json
scafctl snapshot diff before.json after.json
```

## See Also

- [Snapshots Tutorial]({{< relref "snapshots-tutorial" >}}) — Runtime snapshot capture and comparison
- [Dry-Run Tutorial]({{< relref "dryrun-tutorial" >}}) — Preview execution without side effects
- [examples/soldiff/](https://github.com/oakwood-commons/scafctl/tree/main/examples/soldiff) — Example solution files for diffing
