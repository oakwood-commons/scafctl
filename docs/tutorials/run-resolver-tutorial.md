---
title: "Run Resolver Tutorial"
weight: 25
---

# Run Resolver Tutorial

This tutorial covers the `scafctl run resolver` command — a debugging and inspection tool for executing resolvers without running actions. You'll learn how to run all resolvers, target specific resolvers, inspect execution metadata, skip phases, and visualize dependencies.

## Prerequisites

- scafctl installed and available in your PATH
- Familiarity with [resolvers](resolver-tutorial.md)
- A solution file with defined resolvers

## Table of Contents

1. [Run All Resolvers](#run-all-resolvers)
2. [Run Specific Resolvers](#run-specific-resolvers)
3. [Execution Metadata](#execution-metadata)
4. [Skipping Phases](#skipping-phases)
5. [Dependency Graph](#dependency-graph)
6. [Snapshots](#snapshots)
7. [Output Formats](#output-formats)
8. [Debugging Dependencies](#debugging-dependencies)
9. [Working with Parameters](#working-with-parameters)
10. [Common Workflows](#common-workflows)

---

> [!NOTE]
> **A note on `__execution` metadata**: By default, `run resolver` output contains only resolver values. Pass `--show-execution` to include an `__execution` key with execution metadata (timing, dependency graph, provider stats). For `run solution`, execution metadata is also opt-in via `--show-execution`. See [Execution Metadata](#execution-metadata) for details on this feature.

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

{{< tabs "run-resolver-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
KEY          VALUE
───────────────────────
app_name     my-app
environment  production
region       us-west-2
```

### Step 3: Get JSON Output

{{< tabs "run-resolver-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "app_name": "my-app",
  "environment": "production",
  "region": "us-west-2"
}
```

> [!NOTE]
> **Tip**: Unlike `run solution`, this command never executes actions — it's safe for inspection.

---

## Run Specific Resolvers

Pass resolver names as positional arguments to execute only those resolvers (plus their transitive dependencies).

### Step 1: Run a Single Resolver

{{< tabs "run-resolver-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver environment -f demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver environment -f demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "environment": "production"
}
```

### Step 2: Run Multiple Resolvers

{{< tabs "run-resolver-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver environment region -f demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver environment region -f demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "environment": "production",
  "region": "us-west-2"
}
```

### Step 3: Dependencies Are Included Automatically

Create a file called `dep-demo.yaml` with a solution that has dependencies:

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
              expression: "__self + '/v2/data'"
    unrelated:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: not-needed
```

Running just `endpoint` automatically includes `base_url` (its dependency):

{{< tabs "run-resolver-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver endpoint -f dep-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver endpoint -f dep-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "base_url": "https://api.example.com",
  "endpoint": "https://api.example.com/v2/data"
}
```

Note that `base_url` is included (it's a dependency of `endpoint`) but `unrelated` is not.

### Step 4: Handle Unknown Resolver Names

If you request a resolver that doesn't exist, you get a clear error:

{{< tabs "run-resolver-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver nonexistent -f demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver nonexistent -f demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
 ❌ unknown resolver(s): nonexistent (available: app_name, environment, region)
```

---

## Execution Metadata

By default, `run resolver` outputs only resolver values. Pass `--show-execution` to add a `__execution` key alongside the resolver values. The `__execution` key is a structured object with named sections, designed to be extensible for future additions.

{{< tabs "run-resolver-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f dep-demo.yaml -o json --show-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f dep-demo.yaml -o json --show-execution
```
{{% /tab %}}
{{< /tabs >}}

Current sections:

- **`resolvers`**: Per-resolver metadata including phase, duration, status, provider, dependencies, and phase metrics (resolve/transform/validate breakdown)
- **`summary`**: Aggregate stats — total duration, resolver count, phase count
- **`dependencyGraph`**: Full dependency graph with nodes, edges, phases, stats, and pre-rendered **diagrams** (ASCII, DOT, Mermaid)
- **`providerSummary`**: Per-provider usage statistics (resolver count, total duration, call count, success/failure counts, average duration)

Example JSON output:

```json
{
  "base_url": "https://api.example.com",
  "endpoint": "https://api.example.com/v2/data",
  "unrelated": "not-needed",
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

{{< tabs "run-resolver-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver endpoint -f dep-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver endpoint -f dep-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

The `__execution` section only includes metadata for the requested resolvers and their dependencies.

> [!NOTE]
> **Tip**: Pass `--show-execution` to include `__execution` metadata when you need timing, dependency graph, or provider stats. For `run solution`, execution metadata is also opt-in via `--show-execution`.

---

## Pre-Execution Plan (`__plan`)

Before any resolver runs, `scafctl` builds an execution plan from the dependency graph and injects it into the resolver context as `__plan`. This gives every resolver access to topology data -- phase assignment, effective dependencies, and dependency count -- for every other resolver, **before execution begins**.

This is useful for:

- Conditional execution based on topology (e.g., skip a resolver if another has no deps)
- Validating expected dependency structure (guard against misconfiguration)
- Including phase metadata in resolver output for observability

### Shape of `__plan`

`__plan` is a map keyed by resolver name. Each entry has three fields:

| Field | Type | Description |
|---|---|---|
| `phase` | int | Execution phase number (1-based) |
| `dependsOn` | list(string) | Effective dependency names |
| `dependencyCount` | int | Number of effective dependencies |

### Using `__plan` in a `when` Condition

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: plan-aware
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
          - provider: cel
            inputs:
              expression: "_.base_url + '/v2/data'"

    # Only resolve if endpoint actually has dependencies
    # (guards against misconfiguration where endpoint was made standalone)
    endpoint_info:
      type: string
      when:
        expr: "__plan['endpoint'].dependencyCount > 0"
      resolve:
        with:
          - provider: cel
            inputs:
              expression: >-
                'endpoint runs in phase ' + string(__plan['endpoint'].phase) +
                ' and depends on: ' + __plan['endpoint'].dependsOn[0]
~~~

Run it:

{{< tabs "run-resolver-tutorial-plan-1" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f plan-aware.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f plan-aware.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "base_url": "https://api.example.com",
  "endpoint": "https://api.example.com/v2/data",
  "endpoint_info": "endpoint runs in phase 2 and depends on: base_url"
}
```

`endpoint_info` resolved because `__plan['endpoint'].dependencyCount > 0` was `true`.

### `__plan` is Pre-Execution

`__plan` is computed from the static dependency graph **before any resolver runs** -- it reflects the declared structure, not runtime values. Use it for topology checks. For runtime values from other resolvers, use `_` (e.g., `_.base_url`).

> [!NOTE]
> See `examples/resolvers/plan-aware.yaml` for a complete working example.

---

## Skipping Phases

Resolvers execute through three ordered phases: **resolve → transform → validate**. You can skip phases to inspect intermediate values — particularly useful when validation blocks output and you need to see what was actually resolved.

Create a file called `phases-demo.yaml`. This example resolves a port number, transforms it by adding `8000`, then validates the result is within the valid port range. The input value of `60000` is intentionally too high — after the transform, the result (`68000`) exceeds the valid range:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: phases-demo
  version: 1.0.0
spec:
  resolvers:
    port:
      type: int
      resolve:
        with:
          - provider: static
            inputs:
              value: 60000
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self + 8000"
      validate:
        with:
          - provider: validation
            inputs:
              expression: "__self >= 1024 && __self <= 65535"
              message: "Port must be between 1024 and 65535"
```

Running the resolver fails because the transformed value (`68000`) exceeds the valid port range:

{{< tabs "run-resolver-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f phases-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f phases-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
❌ resolver execution failed: ... validation: Port must be between 1024 and 65535
```

The validation error blocks the output entirely — you can't see the resolved value.

### Skip Validation

Use `--skip-validation` to bypass the validation phase and see the actual value:

{{< tabs "run-resolver-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --skip-validation -f phases-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --skip-validation -f phases-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "port": 68000
}
```

Now you can see the problem: the transform added `8000` to `60000`, producing `68000` which exceeds the valid port range. Without `--skip-validation`, this value would be hidden behind the error.

### Skip Transform (and Validation)

Use `--skip-transform` to see the raw resolved value before any transformations:

{{< tabs "run-resolver-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --skip-transform -f phases-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --skip-transform -f phases-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "port": 60000
}
```

This reveals the provider returned `60000` — confirming the root cause is the input value, not the transform logic.

> [!WARNING]
> **Note**: `--skip-transform` implies `--skip-validation` because validating a pre-transform value is misleading — validation rules are written against the expected final shape.

---

## Dependency Graph

The `--graph` flag renders the resolver dependency graph **without executing any providers**. This is useful for understanding the structure and execution order of your resolvers.

### ASCII Graph (default)

{{< tabs "run-resolver-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --graph -f dep-demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --graph -f dep-demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
Resolver Dependency Graph:

Phase 1:
  - unrelated
  - base_url

Phase 2:
  - endpoint
    depends on:
      * base_url

Statistics:
  Total Resolvers: 3
  Total Phases: 2
  Max Parallelism: 2
  Avg Dependencies: 0.33
```

### Graphviz DOT Format

> [!WARNING]
> **Prerequisite:** Requires [Graphviz](https://graphviz.org/) (`dot` command) to be installed.

Generate DOT output and pipe it to Graphviz for a visual diagram:

{{< tabs "runres-graph-dot" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --graph --graph-format=dot -f dep-demo.yaml | dot -Tpng > graph.png
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --graph --graph-format=dot -f dep-demo.yaml | dot -Tpng -o graph.png
```
{{% /tab %}}
{{< /tabs >}}

### Mermaid Format

Generate a Mermaid diagram for embedding in Markdown or documentation:

{{< tabs "run-resolver-tutorial-cmd-13" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --graph --graph-format=mermaid -f dep-demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --graph --graph-format=mermaid -f dep-demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

### JSON Format

Get the graph as structured JSON for programmatic analysis:

{{< tabs "runres-graph-json" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml | jq .
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml | ConvertFrom-Json
```
{{% /tab %}}
{{< /tabs >}}

> [!NOTE]
> **Bash users:** The above command uses [jq](https://jqlang.github.io/jq/), a command-line JSON processor. Install it separately if not already available.

The JSON output includes:
- **nodes**: List of resolvers with name, type, phase, provider, and conditional flag
- **edges**: Dependency relationships (`from` → `to` with dependency type)
- **phases**: Phase groups showing which resolvers execute together
- **stats**: Graph metrics including total resolvers, phases, edges, max depth, and **critical path**

### Critical Path

The graph stats include a **critical path** — the longest chain of dependencies that determines the minimum sequential execution depth. This helps identify bottlenecks:

{{< tabs "run-resolver-tutorial-cmd-14" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml -e '_.stats.criticalPath'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml -e '_.stats.criticalPath'
```
{{% /tab %}}
{{< /tabs >}}

### Graph in Execution Metadata

When running resolvers normally, the dependency graph and a provider usage summary are automatically embedded in `__execution`:

{{< tabs "run-resolver-tutorial-cmd-15" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph'
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.providerSummary'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph'
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.providerSummary'
```
{{% /tab %}}
{{< /tabs >}}

The **providerSummary** shows per-provider statistics: resolver count, total duration, call count, success/failure counts, and average duration.

### Extracting Diagrams from Execution Metadata

The `dependencyGraph` in `__execution` includes a `diagrams` field with pre-rendered ASCII, DOT, and Mermaid representations. Use the `-e` flag (CEL expression) to extract a specific diagram:

> [!WARNING]
> **Prerequisite:** The DOT diagram commands below require [Graphviz](https://graphviz.org/) to be installed.

{{< tabs "runres-extract-diagrams" >}}
{{% tab "Bash" %}}
```bash
# Extract the Mermaid diagram
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.mermaid'

# Extract the DOT diagram and render with Graphviz
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.dot' | dot -Tpng > graph.png

# Extract the ASCII diagram
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.ascii'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Extract the Mermaid diagram
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.mermaid'

# Extract the DOT diagram and render with Graphviz
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.dot' | dot -Tpng -o graph.png

# Extract the ASCII diagram
scafctl run resolver -f dep-demo.yaml -o json -e '_.__execution.dependencyGraph.diagrams.ascii'
```
{{% /tab %}}
{{< /tabs >}}

You can also extract diagrams when running only specific resolvers:

{{< tabs "runres-extract-subset" >}}
{{% tab "Bash" %}}
```bash
# Get the Mermaid diagram for just endpoint and unrelated (and their deps)
scafctl run resolver endpoint unrelated -f dep-demo.yaml -o json \
  -e '_.__execution.dependencyGraph.diagrams.mermaid'
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver endpoint unrelated -f dep-demo.yaml -o json `
  -e '_.__execution.dependencyGraph.diagrams.mermaid'
```
{{% /tab %}}
{{< /tabs >}}

This is useful for generating focused dependency visualizations of a resolver subset without rendering the full solution graph.

> [!NOTE]
> `--graph` is mutually exclusive with `--snapshot`.

---

## Snapshots

The `--snapshot` flag executes resolvers and saves the complete execution state to a JSON file. This mirrors the snapshot functionality in `scafctl render solution --snapshot`.

### Basic Snapshot

{{< tabs "run-resolver-tutorial-cmd-16" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --snapshot --snapshot-file=snapshot.json -f demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --snapshot --snapshot-file=snapshot.json -f demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

This creates a snapshot file containing:
- **metadata**: Solution name, version, build version, timestamp, total duration, and execution status
- **resolvers**: Complete execution state of each resolver including resolved values, durations, and provider information
- **parameters**: Input parameters used during execution

### Redacting Sensitive Values

Use `--redact` to replace sensitive resolver values with `<redacted>`:

{{< tabs "run-resolver-tutorial-cmd-17" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --snapshot --snapshot-file=snapshot.json --redact -f demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --snapshot --snapshot-file=snapshot.json --redact -f demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

Resolvers marked with `sensitive: true` will have their values replaced in the snapshot.

### Snapshot Use Cases

- **Debugging**: Capture a full execution trace for offline analysis
- **Auditing**: Record resolver outputs for compliance
- **Testing**: Compare snapshots between runs to detect regressions
- **Support**: Redacted snapshots can be shared without exposing secrets

> [!NOTE]
> `--snapshot` requires `--snapshot-file` to be specified. It is mutually exclusive with `--graph`.

---

## Output Formats

The command supports all standard output formats:

{{< tabs "run-resolver-tutorial-cmd-18" >}}
{{% tab "Bash" %}}
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
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
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
{{% /tab %}}
{{< /tabs >}}

---

## Working Directory Override

Use the `--cwd` (or `-C`) global flag to run commands as if you were in a different directory. This is useful when scripting or when your solution files live in a different location:

{{< tabs "run-resolver-tutorial-cmd-19" >}}
{{% tab "Bash" %}}
```bash
# Run resolvers from a solution in another directory
scafctl --cwd /path/to/project run resolver -f solution.yaml

# Short form (similar to git -C)
scafctl -C /path/to/project run resolver -f solution.yaml

# Combine with other flags
scafctl -C /path/to/project run resolver -f solution.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Run resolvers from a solution in another directory
scafctl --cwd /path/to/project run resolver -f solution.yaml

# Short form (similar to git -C)
scafctl -C /path/to/project run resolver -f solution.yaml

# Combine with other flags
scafctl -C /path/to/project run resolver -f solution.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

All relative paths (including `-f`, `--output-dir`, etc.) are resolved against the specified working directory instead of the current directory.

---

## Debugging Dependencies

A common debugging workflow is to inspect how resolver dependencies cascade.

### Step 1: Visualize the Graph

{{< tabs "runres-debug-graph" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --graph -f dep-demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --graph -f dep-demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

### Step 2: Run Target Resolver

{{< tabs "run-resolver-tutorial-cmd-20" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver endpoint -f dep-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver endpoint -f dep-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

### Step 3: Inspect a Single Resolver's Value

{{< tabs "run-resolver-tutorial-cmd-21" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver base_url -f dep-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver base_url -f dep-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

This lets you progressively narrow down which resolver is producing unexpected output.

---

## Working with Parameters

Parameters can be passed in two equivalent ways:

1. **Positional `key=value`** (recommended) — after resolver names or on their own
2. **Explicit `-r` flag** — repeatable flag for each parameter

Both forms can be mixed freely.

{{< tabs "runres-params" >}}
{{% tab "Bash" %}}
```bash
# Positional key=value syntax (recommended)
scafctl run resolver -f parameterized.yaml env=staging

# Load parameters from file (positional)
scafctl run resolver -f parameterized.yaml @params.yaml

# Load parameters from stdin (pipe YAML or JSON)
echo '{"env": "prod"}' | scafctl run resolver -f parameterized.yaml @-

# Pipe parameters from another command
cat params.yaml | scafctl run resolver -f parameterized.yaml -r @-

# Pipe raw stdin into a single parameter
echo hello | scafctl run resolver -f parameterized.yaml message=@-

# Read a file's raw content into a parameter
scafctl run resolver -f parameterized.yaml body=@content.txt

# Multiple positional parameters
scafctl run resolver -f parameterized.yaml env=prod region=us-east1

# Mix resolver names and parameters — bare words are names, key=value are params
scafctl run resolver db auth -f parameterized.yaml env=prod

# Explicit -r flag (still supported)
scafctl run resolver -f parameterized.yaml -r env=staging

# Load parameters from file with -r
scafctl run resolver -f parameterized.yaml -r @params.yaml

# Mix both forms (-r values and positional values are combined)
scafctl run resolver -f parameterized.yaml -r env=prod region=us-east1
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Positional key=value syntax (recommended)
scafctl run resolver -f parameterized.yaml env=staging

# Load parameters from file — wrap @file in single quotes to avoid splatting operator
scafctl run resolver -f parameterized.yaml '@params.yaml'

# Load parameters from stdin (pipe YAML or JSON)
'{"env": "prod"}' | scafctl run resolver -f parameterized.yaml '@-'

# Pipe parameters from another command
Get-Content params.yaml | scafctl run resolver -f parameterized.yaml -r '@-'

# Pipe raw stdin into a single parameter
'hello' | scafctl run resolver -f parameterized.yaml 'message=@-'

# Read a file's raw content into a parameter
scafctl run resolver -f parameterized.yaml 'body=@content.txt'

# Multiple positional parameters
scafctl run resolver -f parameterized.yaml env=prod region=us-east1

# Mix resolver names and parameters
scafctl run resolver db auth -f parameterized.yaml env=prod

# Explicit -r flag (still supported)
scafctl run resolver -f parameterized.yaml -r env=staging

# Load parameters from file with -r
scafctl run resolver -f parameterized.yaml -r '@params.yaml'

# Mix both forms
scafctl run resolver -f parameterized.yaml -r env=prod region=us-east1
```
{{% /tab %}}
{{< /tabs >}}

### Dynamic Help Text

When you specify a solution file with `--help`, the command shows the solution's
resolver parameters alongside the standard help text:

{{< tabs "run-resolver-tutorial-cmd-22" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f param-demo.yaml --help
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f param-demo.yaml --help
```
{{% /tab %}}
{{< /tabs >}}

This appends a table showing which resolvers accept CLI parameters, their types,
and descriptions — making it easy to discover what inputs a solution expects
without reading the YAML file.

### Example with Parameter Provider

Create a file called `param-demo.yaml`:

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
              expression: "'https://config.' + __self + '.example.com'"
```

{{< tabs "run-resolver-tutorial-cmd-23" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f param-demo.yaml environment=staging -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f param-demo.yaml environment=staging -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "config_url": "https://config.staging.example.com",
  "env": "staging"
}
```

### Parameter Key Validation

Parameter keys are validated against the solution's parameter-type resolver names.
Unknown keys are rejected early with a helpful error message that suggests the
closest valid key when a typo is detected:

{{< tabs "run-resolver-tutorial-cmd-24" >}}
{{% tab "Bash" %}}
```bash
# Typo: "envronment" instead of "environment"
scafctl run resolver -f param-demo.yaml envronment=staging
# Error: solution does not accept input "envronment" — did you mean "environment"?

# Completely unknown key
scafctl run resolver -f param-demo.yaml unknown=value
# Error: solution does not accept input "unknown" (valid inputs: environment)
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Typo: "envronment" instead of "environment"
scafctl run resolver -f param-demo.yaml envronment=staging
# Error: solution does not accept input "envronment" — did you mean "environment"?

# Completely unknown key
scafctl run resolver -f param-demo.yaml unknown=value
# Error: solution does not accept input "unknown" (valid inputs: environment)
```
{{% /tab %}}
{{< /tabs >}}

---

## Common Workflows

### CI/CD Validation

Check that all resolvers succeed without running actions:

{{< tabs "run-resolver-tutorial-cmd-25" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f demo.yaml -o quiet
echo "Exit code: $?"
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f demo.yaml -o quiet
Write-Output "Exit code: $LASTEXITCODE"
```
{{% /tab %}}
{{< /tabs >}}

### Partial Execution for Debugging

Isolate a failing resolver and its dependencies:

{{< tabs "run-resolver-tutorial-cmd-26" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver endpoint -f dep-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver endpoint -f dep-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

### Inspect Raw Resolved Values

Skip transforms to see what providers actually return:

{{< tabs "run-resolver-tutorial-cmd-27" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --skip-transform -f dep-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --skip-transform -f dep-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

### Preview Execution Plan

Use `--graph` to see the resolver execution plan without running anything:

{{< tabs "run-resolver-tutorial-cmd-28" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver --graph -f dep-demo.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver --graph -f dep-demo.yaml
```
{{% /tab %}}
{{< /tabs >}}

### Comparing Resolver Outputs

{{< tabs "run-resolver-tutorial-cmd-29" >}}
{{% tab "Bash" %}}
```bash
# Get current values
scafctl run resolver -f param-demo.yaml -r environment=production -o json > current.json

# Change parameters and compare
scafctl run resolver -f param-demo.yaml -r environment=staging -o json > staging.json
diff current.json staging.json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Get current values
scafctl run resolver -f param-demo.yaml -r environment=production -o json > current.json

# Change parameters and compare
scafctl run resolver -f param-demo.yaml -r environment=staging -o json > staging.json
diff current.json staging.json
```
{{% /tab %}}
{{< /tabs >}}

### Show Execution Metrics

{{< tabs "run-resolver-tutorial-cmd-30" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f demo.yaml --show-metrics -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f demo.yaml --show-metrics -o json
```
{{% /tab %}}
{{< /tabs >}}

Metrics are displayed on stderr after execution completes.

---

## What's Next?

- [Resolver Tutorial](resolver-tutorial.md) — Learn resolver fundamentals
- [Actions Tutorial](actions-tutorial.md) — Execute actions after resolvers
- [CEL Tutorial](cel-tutorial.md) — Filter and transform resolver output
- [Snapshots Tutorial](snapshots-tutorial.md) — Deeper dive into snapshot analysis

---

## Quick Reference

{{< tabs "run-resolver-tutorial-cmd-31" >}}
{{% tab "Bash" %}}
```bash
# All resolvers
scafctl run resolver -f solution.yaml

# Named resolvers (with dependencies)
scafctl run resolver db config -f solution.yaml

# JSON output (resolver values only, no execution metadata)
scafctl run resolver -f solution.yaml -o json

# JSON output with __execution metadata (phases, timing, providers)
scafctl run resolver -f solution.yaml -o json

# Skip transform and validation phases
scafctl run resolver --skip-transform -f solution.yaml

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
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# All resolvers
scafctl run resolver -f solution.yaml

# Named resolvers (with dependencies)
scafctl run resolver db config -f solution.yaml

# JSON output (resolver values only, no execution metadata)
scafctl run resolver -f solution.yaml -o json

# JSON output with __execution metadata (phases, timing, providers)
scafctl run resolver -f solution.yaml -o json

# Skip transform and validation phases
scafctl run resolver --skip-transform -f solution.yaml

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
{{% /tab %}}
{{< /tabs >}}

## Next Steps

- [Run Provider Tutorial](run-provider-tutorial.md) — Test providers in isolation
- [Actions Tutorial](actions-tutorial.md) — Learn about workflows
- [Resolver Tutorial](resolver-tutorial.md) — Deep dive into resolvers
- [Provider Reference](provider-reference.md) — Complete provider documentation
