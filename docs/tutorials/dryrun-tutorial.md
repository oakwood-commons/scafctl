---
title: "Dry-Run Tutorial"
weight: 86
---

# Dry-Run Tutorial

This tutorial covers using `--dry-run` to preview what a solution execution would do without performing any side effects.

## Overview

Dry-run mode resolves all values and builds an action execution plan, but never executes actions. This gives you a complete picture of what *would* happen:

```
┌──────────────┐     ┌──────────────┐     ┌──────────────┐
│   Solution   │     │  Dry-Run     │     │   Report     │
│   (.yaml)    │ ──► │  Generator   │ ──► │  (resolvers, │
│              │     │              │     │   actions,   │
│              │     │  No side     │     │   phases)    │
│              │     │  effects!    │     │              │
└──────────────┘     └──────────────┘     └──────────────┘
```

## When to Use Dry-Run

| Use Case | Scenario |
|----------|----------|
| **Pre-flight check** | Verify all resolvers produce expected values before executing actions |
| **Action plan review** | See the execution order, phases, and dependencies before running |
| **CI validation** | Assert resolver outputs in a pipeline without side effects |
| **Debugging** | Understand why a solution behaves unexpectedly by inspecting resolved values |
| **Documentation** | Generate a report of what a solution does for review or audit |

## CLI Usage

### Dry-Run a Full Solution

```bash
scafctl run solution -f solution.yaml --dry-run
```

This resolves all values and shows the action plan without executing any actions.

### Dry-Run with JSON Output

```bash
scafctl run solution -f solution.yaml --dry-run -o json
```

Returns a structured JSON report with resolver values, action plan, phases, and warnings.

### Dry-Run Resolvers Only

```bash
scafctl run resolver -f solution.yaml --dry-run
```

Resolves values without building an action plan — useful when you only care about resolver output.

## Example Walkthrough

### Step 1: Inspect the Example Solution

```bash
cat examples/dryrun/basic-dryrun.yaml
```

This solution has four resolvers (`greeting`, `target`, `port`, `endpoint`) and two actions (`greet`, `deploy`).

### Step 2: Run Dry-Run

```bash
scafctl run solution -f examples/dryrun/basic-dryrun.yaml --dry-run
```

The output shows:
- **Resolvers** — Each resolver's value (e.g., `greeting = "Hello, World!"`, `endpoint = "https://staging.example.com:8080"`)
- **Action Plan** — Execution order (`greet` in phase 1, `deploy` in phase 2 after `greet`)
- **No side effects** — The `echo` commands are never executed

### Step 3: JSON Output for Automation

```bash
scafctl run solution -f examples/dryrun/basic-dryrun.yaml --dry-run -o json | jq .
```

The JSON report contains:

```json
{
  "dryRun": true,
  "solution": "dryrun-demo",
  "version": "1.0.0",
  "hasResolvers": true,
  "hasWorkflow": true,
  "resolvers": {
    "greeting": { "value": "Hello, World!", "status": "resolved", "dryRun": true },
    "endpoint": { "value": "https://staging.example.com:8080", "status": "resolved", "dryRun": true }
  },
  "actionPlan": [
    { "name": "greet", "provider": "exec", "phase": 1 },
    { "name": "deploy", "provider": "exec", "phase": 2, "dependencies": ["greet"] }
  ],
  "totalActions": 2,
  "totalPhases": 2
}
```

### Step 4: Conditional Actions

```bash
scafctl run solution -f examples/dryrun/conditional-dryrun.yaml --dry-run -o json
```

This example shows how dry-run reports conditional (`when`) actions and `finally` blocks. The action plan includes the `when` expression so you can see which conditions will be evaluated at runtime.

## Dry-Run Report Structure

| Field | Description |
|-------|-------------|
| `dryRun` | Always `true` |
| `solution` | Solution name from metadata |
| `version` | Solution version |
| `hasResolvers` | Whether the solution defines resolvers |
| `hasWorkflow` | Whether the solution defines a workflow |
| `parameters` | Parameters that were (or would be) passed |
| `resolvers` | Map of resolver name → `{value, status, dryRun}` |
| `actionPlan` | Ordered list of planned actions with phase, provider, inputs, dependencies |
| `totalActions` | Total number of actions |
| `totalPhases` | Total execution phases |
| `mockBehaviors` | What each provider does in dry-run mode |
| `warnings` | Issues like unresolved values or graph build failures |

## Using with Snapshots

Dry-run shows what *would* happen; snapshots capture what *did* happen. Combine them:

```bash
# Preview what will happen
scafctl run solution -f solution.yaml --dry-run -o json > plan.json

# Execute and capture the result
scafctl run resolver -f solution.yaml --snapshot --snapshot-file=actual.json

# Compare plan vs actual if needed
```

## CI Pipeline Integration

Use dry-run in CI to validate solutions without side effects:

```bash
# Verify all resolvers resolve successfully
output=$(scafctl run solution -f solution.yaml --dry-run -o json)
warnings=$(echo "$output" | jq '.warnings | length')

if [ "$warnings" -gt 0 ]; then
  echo "Dry-run reported warnings:"
  echo "$output" | jq '.warnings[]'
  exit 1
fi
```

## See Also

- [Snapshots Tutorial]({{< relref "snapshots-tutorial" >}}) — Capture and compare runtime execution state
- [Solution Diff Tutorial]({{< relref "soldiff-tutorial" >}}) — Structural comparison of solution files
- [examples/dryrun/](https://github.com/oakwood-commons/scafctl/tree/main/examples/dryrun) — Example solution files for dry-run
