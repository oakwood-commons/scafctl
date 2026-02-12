---
title: "Run Resolver Tutorial"
weight: 25
---

# Run Resolver Tutorial

This tutorial covers the `scafctl run resolver` command — a debugging and inspection tool for executing resolvers without running actions. You'll learn how to run all resolvers, target specific resolvers, inspect execution metadata, skip phases, and use dry-run for planning.

## Prerequisites

- scafctl installed and available in your PATH
- Familiarity with [resolvers](resolver-tutorial.md)
- A solution file with defined resolvers

## Table of Contents

1. [Run All Resolvers](#run-all-resolvers)
2. [Run Specific Resolvers](#run-specific-resolvers)
3. [Execution Metadata](#execution-metadata)
4. [Skipping Phases](#skipping-phases)
5. [Dry Run](#dry-run)
6. [Dependency Graph](#dependency-graph)
7. [Snapshots](#snapshots)
8. [Output Formats](#output-formats)
9. [Debugging Dependencies](#debugging-dependencies)
10. [Working with Parameters](#working-with-parameters)
11. [Common Workflows](#common-workflows)

---

## Run All Resolvers

The simplest usage runs all resolvers in a solution file.

### Step 1: Create a Solution File

Create a file called `demo.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: resolver-demo
  version: 1.0.0
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: production
    region:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: us-west-2
    app_name:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: my-app
```

### Step 2: Run All Resolvers

```bash
scafctl run resolver -f demo.yaml
```

This outputs all resolved values in table format (default).

### Step 3: Get JSON Output

```bash
scafctl run resolver -f demo.yaml -o json
```

Output:

```json
{
  "app_name": "my-app",
  "environment": "production",
  "region": "us-west-2"
}
```

> **Tip**: Unlike `run solution`, this command never executes actions — it's safe for inspection.

---

## Run Specific Resolvers

Pass resolver names as positional arguments to execute only those resolvers (plus their transitive dependencies).

### Step 1: Run a Single Resolver

```bash
scafctl run resolver environment -f demo.yaml -o json
```

Output:

```json
{
  "environment": "production"
}
```

### Step 2: Run Multiple Resolvers

```bash
scafctl run resolver environment region -f demo.yaml -o json
```

### Step 3: Dependencies Are Included Automatically

Given a solution with dependencies:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dep-demo
  version: 1.0.0
spec:
  resolvers:
    base_url:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: https://api.example.com
    endpoint:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value:
                rslvr: base_url
      transform:
        with:
          - provider: cel
            inputs:
              expression: "value + '/v2/data'"
    unrelated:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: not-needed
```

Running just `endpoint` automatically includes `base_url` (its dependency):

```bash
scafctl run resolver endpoint -f dep-demo.yaml -o json
```

Output includes `base_url` and `endpoint`, but not `unrelated`.

### Step 4: Handle Unknown Resolver Names

If you request a resolver that doesn't exist, you get a clear error:

```bash
scafctl run resolver nonexistent -f demo.yaml
```

Output:

```
Error: unknown resolver(s): nonexistent (available: environment, region, app_name)
```

---

## Execution Metadata

The `run resolver` command is a debugging tool, so it always includes a `__execution` key in the output alongside the resolver values. The `__execution` key is a structured object with named sections, designed to be extensible for future additions (e.g. timeline).

```bash
scafctl run resolver -f dep-demo.yaml -o json
```

Current sections:

- **`resolvers`**: Per-resolver metadata including phase, duration, status, provider, dependencies, and phase metrics (resolve/transform/validate breakdown)
- **`summary`**: Aggregate stats — total duration, resolver count, phase count
- **`dependencyGraph`**: Full dependency graph with nodes, edges, phases, stats, and pre-rendered **diagrams** (ASCII, DOT, Mermaid)
- **`providerSummary`**: Per-provider usage statistics (resolver count, total duration, call count, success/failure counts, average duration)

Example JSON output:

```json
{
  "base_url": "https://api.example.com",
  "endpoint": "https://api.example.com/v1/users",
  "unrelated": "other-value",
  "__execution": {
    "resolvers": {
      "base_url": {
        "phase": 1,
        "duration": "2ms",
        "status": "success",
        "provider": "static",
        "dependencies": [],
        "providerCallCount": 1,
        "valueSizeBytes": 26,
        "dependencyCount": 0
      },
      "endpoint": {
        "phase": 2,
        "duration": "3ms",
        "status": "success",
        "provider": "static",
        "dependencies": ["base_url"],
        "providerCallCount": 1,
        "valueSizeBytes": 38,
        "dependencyCount": 1,
        "phaseMetrics": [
          { "phase": "resolve", "duration": "2ms" },
          { "phase": "validate", "duration": "1ms" }
        ]
      }
    },
    "summary": {
      "totalDuration": "5ms",
      "resolverCount": 3,
      "phaseCount": 2
    }
  }
}
```

### Combine with Named Resolvers

```bash
scafctl run resolver endpoint -f dep-demo.yaml -o json
```

The `__execution` section only includes metadata for the requested resolvers and their dependencies.

> **Tip**: For `run solution`, execution metadata is opt-in via `--show-execution`.

---

## Skipping Phases

Resolvers execute through three ordered phases: **resolve → transform → validate**. You can skip phases to inspect intermediate values.

### Skip Validation

Skip only the validation phase:

```bash
scafctl run resolver --skip-validation -f solution.yaml -o json
```

This runs resolve and transform, but skips validation rules. Useful when you want to see the final transformed value without validation errors blocking output.

### Skip Transform (and Validation)

Skip both transform and validation phases, returning raw resolved values:

```bash
scafctl run resolver --skip-transform -f solution.yaml -o json
```

This runs only the resolve phase. Useful for inspecting what providers return before any transformations are applied.

> **Note**: `--skip-transform` implies `--skip-validation` because validating a pre-transform value is misleading — validation rules are written against the expected final shape.

---

## Dry Run

Use `--dry-run` to show the execution plan without running any providers:

```bash
scafctl run resolver --dry-run -f dep-demo.yaml -o json
```

This displays:
- DAG-based execution phases (which resolvers run in which order)
- Per-resolver dependencies, provider types, and configured phases
- Active and skipped phases based on flags

Example output:

```json
{
  "dryRun": true,
  "executionPlan": {
    "totalResolvers": 3,
    "totalPhases": 2,
    "activePhases": ["resolve", "transform", "validate"],
    "skippedPhases": [],
    "phases": [
      { "phase": 1, "resolvers": ["base_url", "unrelated"] },
      { "phase": 2, "resolvers": ["endpoint"] }
    ]
  },
  "resolvers": {
    "base_url": {
      "provider": "static",
      "dependencies": [],
      "configuredPhases": ["resolve"]
    },
    "endpoint": {
      "provider": "static",
      "dependencies": ["base_url"],
      "configuredPhases": ["resolve", "transform"]
    }
  }
}
```

Combine with skip flags to see what would be active:

```bash
scafctl run resolver --dry-run --skip-transform -f dep-demo.yaml -o json
```

---

## Dependency Graph

The `--graph` flag renders the resolver dependency graph **without executing any providers**. This is useful for understanding the structure and execution order of your resolvers.

### ASCII Graph (default)

```bash
scafctl run resolver --graph -f dep-demo.yaml
```

The ASCII output shows resolvers grouped by phase with dependency arrows.

### Graphviz DOT Format

Generate DOT output and pipe it to Graphviz for a visual diagram:

```bash
scafctl run resolver --graph --graph-format=dot -f dep-demo.yaml | dot -Tpng > graph.png
```

### Mermaid Format

Generate a Mermaid diagram for embedding in Markdown or documentation:

```bash
scafctl run resolver --graph --graph-format=mermaid -f dep-demo.yaml
```

### JSON Format

Get the graph as structured JSON for programmatic analysis:

```bash
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml | jq .
```

The JSON output includes:
- **nodes**: List of resolvers with name, type, phase, provider, and conditional flag
- **edges**: Dependency relationships (`from` → `to` with dependency type)
- **phases**: Phase groups showing which resolvers execute together
- **stats**: Graph metrics including total resolvers, phases, edges, max depth, and **critical path**

### Critical Path

The graph stats include a **critical path** — the longest chain of dependencies that determines the minimum sequential execution depth. This helps identify bottlenecks:

```bash
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml | jq '.stats.criticalPath'
```

### Graph in Execution Metadata

When running resolvers normally, the dependency graph and a provider usage summary are automatically embedded in `__execution`:

```bash
scafctl run resolver -f dep-demo.yaml -o json | jq '.__execution.dependencyGraph'
scafctl run resolver -f dep-demo.yaml -o json | jq '.__execution.providerSummary'
```

The **providerSummary** shows per-provider statistics: resolver count, total duration, call count, success/failure counts, and average duration.

### Extracting Diagrams from Execution Metadata

The `dependencyGraph` in `__execution` includes a `diagrams` field with pre-rendered ASCII, DOT, and Mermaid representations. Use the `-e` flag (CEL expression) to extract a specific diagram:

```bash
# Extract the Mermaid diagram
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.mermaid'

# Extract the DOT diagram and render with Graphviz
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.dot' | dot -Tpng > graph.png

# Extract the ASCII diagram
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.ascii'
```

You can also extract diagrams when running only specific resolvers:

```bash
# Get the Mermaid diagram for just log_level and health_check_endpoints (and their deps)
scafctl run resolver log_level health_check_endpoints -f solution.yaml -o json \
  -e '_.__execution.dependencyGraph.diagrams.mermaid'
```

This is useful for generating focused dependency visualizations of a resolver subset without rendering the full solution graph.

{{% hint info %}}
`--graph` is mutually exclusive with `--dry-run` and `--snapshot`.
{{% /hint %}}

---

## Snapshots

The `--snapshot` flag executes resolvers and saves the complete execution state to a JSON file. This mirrors the snapshot functionality in `scafctl render solution --snapshot`.

### Basic Snapshot

```bash
scafctl run resolver --snapshot --snapshot-file=snapshot.json -f demo.yaml
```

This creates a snapshot file containing:
- **metadata**: Solution name, version, build version, timestamp, total duration, and execution status
- **resolvers**: Complete execution state of each resolver including resolved values, durations, and provider information
- **parameters**: Input parameters used during execution

### Redacting Sensitive Values

Use `--redact` to replace sensitive resolver values with `<redacted>`:

```bash
scafctl run resolver --snapshot --snapshot-file=snapshot.json --redact -f demo.yaml
```

Resolvers marked with `sensitive: true` will have their values replaced in the snapshot.

### Snapshot Use Cases

- **Debugging**: Capture a full execution trace for offline analysis
- **Auditing**: Record resolver outputs for compliance
- **Testing**: Compare snapshots between runs to detect regressions
- **Support**: Redacted snapshots can be shared without exposing secrets

{{% hint info %}}
`--snapshot` requires `--snapshot-file` to be specified. It is mutually exclusive with `--dry-run` and `--graph`.
{{% /hint %}}

---

## Output Formats

The command supports all standard output formats:

```bash
# Table (default) — human-readable bordered table
scafctl run resolver -f demo.yaml

# JSON — for scripting and piping
scafctl run resolver -f demo.yaml -o json

# YAML — for configuration contexts
scafctl run resolver -f demo.yaml -o yaml

# Quiet — suppress output (useful for exit code checks)
scafctl run resolver -f demo.yaml -o quiet

# Interactive TUI — explore and search results
scafctl run resolver -f demo.yaml -i

# CEL expression filtering
scafctl run resolver -f demo.yaml -e '_.environment'
```

---

## Debugging Dependencies

A common debugging workflow is to inspect how resolver dependencies cascade.

### Step 1: Visualize the Graph

```bash
scafctl resolver graph -f dep-demo.yaml
```

### Step 2: Run Target Resolver

```bash
scafctl run resolver endpoint -f dep-demo.yaml -o json
```

### Step 3: Inspect a Single Resolver's Value

```bash
scafctl run resolver base_url -f dep-demo.yaml -o json
```

This lets you progressively narrow down which resolver is producing unexpected output.

---

## Working with Parameters

Pass parameters using `-r` flags, just like with `run solution`:

```bash
# Key-value parameter
scafctl run resolver -f parameterized.yaml -r env=staging

# Load parameters from file
scafctl run resolver -f parameterized.yaml -r @params.yaml

# Multiple parameters
scafctl run resolver -f parameterized.yaml -r env=prod -r region=us-east1
```

### Example with Parameter Provider

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: param-demo
  version: 1.0.0
spec:
  resolvers:
    env:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: environment
    config_url:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value:
                rslvr: env
      transform:
        with:
          - provider: cel
            inputs:
              expression: "'https://config.' + value + '.example.com'"
```

```bash
scafctl run resolver -f param-demo.yaml -r environment=staging -o json
```

---

## Common Workflows

### CI/CD Validation

Check that all resolvers succeed without running actions:

```bash
scafctl run resolver -f solution.yaml -o quiet
echo "Exit code: $?"
```

### Partial Execution for Debugging

Isolate a failing resolver and its dependencies:

```bash
scafctl run resolver failing-resolver -f solution.yaml -o json
```

### Inspect Raw Resolved Values

Skip transforms to see what providers actually return:

```bash
scafctl run resolver --skip-transform -f solution.yaml -o json
```

### Preview Execution Plan

See what would happen without running anything:

```bash
scafctl run resolver --dry-run -f solution.yaml -o json
```

### Comparing Resolver Outputs

```bash
# Get current values
scafctl run resolver -f solution.yaml -o json > current.json

# Change parameters and compare
scafctl run resolver -f solution.yaml -r env=staging -o json > staging.json
diff current.json staging.json
```

### Show Execution Metrics

```bash
scafctl run resolver -f solution.yaml --show-metrics -o json
```

Metrics are displayed on stderr after execution completes.

---

## What's Next?

- [Resolver Tutorial](resolver-tutorial.md) — Learn resolver fundamentals
- [Actions Tutorial](actions-tutorial.md) — Execute actions after resolvers
- [CEL Tutorial](cel-tutorial.md) — Filter and transform resolver output
- [Snapshots Tutorial](snapshots-tutorial.md) — Deeper dive into snapshot analysis

---

## Quick Reference

```bash
# All resolvers
scafctl run resolver -f solution.yaml

# Named resolvers (with dependencies)
scafctl run resolver db config -f solution.yaml

# JSON output (always includes __execution metadata)
scafctl run resolver -f solution.yaml -o json

# Skip transform and validation phases
scafctl run resolver --skip-transform -f solution.yaml

# Show execution plan without running
scafctl run resolver --dry-run -f solution.yaml

# Dependency graph (ASCII)
scafctl run resolver --graph -f solution.yaml

# Dependency graph (DOT/Mermaid/JSON)
scafctl run resolver --graph --graph-format=dot -f solution.yaml
scafctl run resolver --graph --graph-format=mermaid -f solution.yaml
scafctl run resolver --graph --graph-format=json -f solution.yaml

# Snapshot execution state
scafctl run resolver --snapshot --snapshot-file=out.json -f solution.yaml

# Snapshot with sensitive value redaction
scafctl run resolver --snapshot --snapshot-file=out.json --redact -f solution.yaml

# With parameters
scafctl run resolver -f solution.yaml -r key=value

# Interactive exploration
scafctl run resolver -f solution.yaml -i

# Aliases: res, resolvers
scafctl run res -f solution.yaml
```
