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

```shell
scafctl run resolver -f demo.yaml
```

Output:

```
KEY          VALUE
───────────────────────
app_name     my-app
environment  production
region       us-west-2
```

### Step 3: Get JSON Output

```shell
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

> [!NOTE]
> **Tip**: Unlike `run solution`, this command never executes actions — it's safe for inspection.

---

## Run Specific Resolvers

Pass resolver names as positional arguments to execute only those resolvers (plus their transitive dependencies).

### Step 1: Run a Single Resolver

```shell
scafctl run resolver environment -f demo.yaml -o json
```

Output:

```json
{
  "environment": "production"
}
```

### Step 2: Run Multiple Resolvers

```shell
scafctl run resolver environment region -f demo.yaml -o json
```

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

```shell
scafctl run resolver endpoint -f dep-demo.yaml -o json
```

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

```shell
scafctl run resolver nonexistent -f demo.yaml
```

Output:

```
 ❌ unknown resolver(s): nonexistent (available: app_name, environment, region)
```

---

## Execution Metadata

By default, `run resolver` outputs only resolver values. Pass `--show-execution` to add a `__execution` key alongside the resolver values. The `__execution` key is a structured object with named sections, designed to be extensible for future additions.

```shell
scafctl run resolver -f dep-demo.yaml -o json --show-execution
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

```shell
scafctl run resolver endpoint -f dep-demo.yaml -o json
```

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

```shell
scafctl run resolver -f plan-aware.yaml -o json
```

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

Resolvers execute through three ordered phases: **resolve -> transform -> validate**. You can skip phases to inspect intermediate values, or rely on the non-fatal validation behavior described below to see resolved values even when validation fails.

> **Validation is non-fatal in `run resolver`.** Because `run resolver` is an inspection and troubleshooting command, validation failures never withhold the produced values. The resolved values are still printed, validation diagnostics are written to stderr, and the command exits `0`. To make validation failures exit non-zero (for example, in CI), pass `--on-validation-error error`. To gate on validation as the primary intent, use the dedicated [`scafctl validate resolver`](#validate-resolver) command, which presets fatal validation.
>
> **Dependents keep running too.** Because a resolver that fails only a validation rule still produces a usable value, its dependent resolvers continue to execute and read that value -- they are not skipped. (Resolve and transform failures still exit non-zero and their dependents are skipped because no value was produced.)
>
> **Partial values survive a resolve/transform failure.** When a resolver hard-fails in the resolve or transform phase, `run resolver` still emits the values of every resolver that *did* resolve successfully on stdout, plus a failure summary on stderr. In structured output (`-o json`/`-o yaml`) the stdout document additionally carries a `__status: failed` / `__diagnostics` envelope naming the failed resolver(s); table output shows only the successful values on stdout (the failure detail stays on stderr). Unlike a validation-only failure, a resolve/transform failure always exits non-zero (exit `1`) regardless of `--on-validation-error` -- but the successful values are no longer discarded.

Create a file called `phases-demo.yaml`. This example resolves a port number, transforms it by adding `8000`, then validates the result is within the valid port range. The input value of `60000` is intentionally too high -- after the transform, the result (`68000`) exceeds the valid range:

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

Running the resolver reports a validation failure because the transformed value (`68000`) exceeds the valid port range -- but the value is still shown:

```shell
scafctl run resolver -f phases-demo.yaml -o json
```

Output (values on stdout, exit `0`):

```json
{
  "port": 68000
}
```

Diagnostics on stderr:

```
Warning: 1 resolver(s) failed validation:
  - port: Port must be between 1024 and 65535
(resolved values are still emitted on stdout; pass --on-validation-error error to exit non-zero)
```

You can see the resolved value (`68000`) directly, and the diagnostic explains why it is invalid -- no flags required.

To make validation failures exit non-zero, add `--on-validation-error error`:

```shell
scafctl run resolver -f phases-demo.yaml -o json --on-validation-error error
```

The values are still printed, the diagnostics still appear on stderr, but the command now exits `2` (validation failed).

### Skip Validation

Use `--on-validation-error ignore` to bypass the validation phase entirely (no diagnostics, no validation work):

```shell
scafctl run resolver --on-validation-error ignore -f phases-demo.yaml -o json
```

Output:

```json
{
  "port": 68000
}
```

Now you can see the problem: the transform added `8000` to `60000`, producing `68000` which exceeds the valid port range. Unlike the default non-fatal behavior, `--on-validation-error ignore` produces no validation diagnostics at all.

### Skip Transform (and Validation)

Use `--skip-transform` to see the raw resolved value before any transformations:

```shell
scafctl run resolver --skip-transform -f phases-demo.yaml -o json
```

Output:

```json
{
  "port": 60000
}
```

This reveals the provider returned `60000` — confirming the root cause is the input value, not the transform logic.

> [!WARNING]
> **Note**: `--skip-transform` implies skipping validation because validating a pre-transform value is misleading — validation rules are written against the expected final shape.

---

## Validate Resolver

When your primary intent is to **gate on validation** rather than inspect values, use the dedicated `scafctl validate resolver` command. It behaves like `run resolver` but presets fatal validation: validation failures exit `2` (validation failed). After the resolver phases pass, it additionally runs `lint` as part of the gate -- lint errors (including JSON Schema violations) fail, lint warnings are surfaced, and `--strict` makes lint warnings fatal too. This makes `validate resolver` a first-class gate verb for scripts and CI pipelines.

```shell
scafctl validate resolver -f phases-demo.yaml -o json
```

The resolved values are still printed and diagnostics still appear on stderr, but the command exits `2` when any resolver fails validation or when lint reports an error (or a warning under `--strict`). Use `run resolver` for day-to-day troubleshooting (exit `0`) and `validate resolver` when a non-zero exit on validation or lint failure is the goal. To validate a whole solution as a gate without focusing on resolvers, use [`scafctl validate solution`](../../design/cli/#validation-gate).

---

## Dependency Graph

The `--graph` flag renders the resolver dependency graph **without executing any providers**. This is useful for understanding the structure and execution order of your resolvers.

### ASCII Graph (default)

```shell
scafctl run resolver --graph -f dep-demo.yaml
```

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

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
scafctl run resolver --graph --graph-format=dot -f dep-demo.yaml | dot -Tpng > graph.png
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
scafctl run resolver --graph --graph-format=dot -f dep-demo.yaml | dot -Tpng -o graph.png
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

### Mermaid Format

Generate a Mermaid diagram for embedding in Markdown or documentation:

```shell
scafctl run resolver --graph --graph-format=mermaid -f dep-demo.yaml
```

### JSON Format

Get the graph as structured JSON for programmatic analysis:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml | jq .
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml | ConvertFrom-Json
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

> [!NOTE]
> **Bash users:** The above command uses [jq](https://jqlang.github.io/jq/), a command-line JSON processor. Install it separately if not already available.

The JSON output includes:
- **nodes**: List of resolvers with name, type, phase, provider, and conditional flag
- **edges**: Dependency relationships (`from` → `to` with dependency type)
- **phases**: Phase groups showing which resolvers execute together
- **stats**: Graph metrics including total resolvers, phases, edges, max depth, and **critical path**

### Critical Path

The graph stats include a **critical path** — the longest chain of dependencies that determines the minimum sequential execution depth. This helps identify bottlenecks:

```shell
scafctl run resolver --graph --graph-format=json -f dep-demo.yaml -e '_.stats.criticalPath'
```

### Graph in Execution Metadata

When running resolvers normally, the dependency graph and a provider usage summary are automatically embedded in `__execution`:

```shell
scafctl run resolver -f dep-demo.yaml -o json --show-execution -e '_.__execution.dependencyGraph'
scafctl run resolver -f dep-demo.yaml -o json --show-execution -e '_.__execution.providerSummary'
```

The **providerSummary** shows per-provider statistics: resolver count, total duration, call count, success/failure counts, and average duration.

### Extracting Diagrams from Execution Metadata

The `dependencyGraph` in `__execution` includes a `diagrams` field with pre-rendered ASCII, DOT, and Mermaid representations. Use the `-e` flag (CEL expression) to extract a specific diagram:

> [!WARNING]
> **Prerequisite:** The DOT diagram commands below require [Graphviz](https://graphviz.org/) to be installed.

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
# Extract the Mermaid diagram
scafctl run resolver -f dep-demo.yaml -o json --show-execution -e '_.__execution.dependencyGraph.diagrams.mermaid'

# Extract the DOT diagram and render with Graphviz
scafctl run resolver -f dep-demo.yaml -o json --show-execution -e '_.__execution.dependencyGraph.diagrams.dot' | dot -Tpng > graph.png

# Extract the ASCII diagram
scafctl run resolver -f dep-demo.yaml -o json --show-execution -e '_.__execution.dependencyGraph.diagrams.ascii'
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
# Extract the Mermaid diagram
scafctl run resolver -f dep-demo.yaml -o json --show-execution -e '_.__execution.dependencyGraph.diagrams.mermaid'

# Extract the DOT diagram and render with Graphviz
scafctl run resolver -f dep-demo.yaml -o json --show-execution -e '_.__execution.dependencyGraph.diagrams.dot' | dot -Tpng -o graph.png

# Extract the ASCII diagram
scafctl run resolver -f dep-demo.yaml -o json --show-execution -e '_.__execution.dependencyGraph.diagrams.ascii'
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

You can also extract diagrams when running only specific resolvers:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
# Get the Mermaid diagram for just endpoint and unrelated (and their deps)
scafctl run resolver endpoint unrelated -f dep-demo.yaml -o json --show-execution \
  -e '_.__execution.dependencyGraph.diagrams.mermaid'
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
~~~powershell
scafctl run resolver endpoint unrelated -f dep-demo.yaml -o json --show-execution `
  -e '_.__execution.dependencyGraph.diagrams.mermaid'
~~~
{{< /shell-pane >}}
{{< /shell-toggle >}}

This is useful for generating focused dependency visualizations of a resolver subset without rendering the full solution graph.

> [!NOTE]
> `--graph` is mutually exclusive with `--snapshot`.

---

## Snapshots

The `--snapshot` flag executes resolvers and saves the complete execution state to a JSON file. This mirrors the snapshot functionality in `scafctl render solution --snapshot`.

### Basic Snapshot

```shell
scafctl run resolver --snapshot --snapshot-file=snapshot.json -f demo.yaml
```

This creates a snapshot file containing:
- **metadata**: Solution name, version, build version, timestamp, total duration, and execution status
- **resolvers**: Complete execution state of each resolver including resolved values, durations, and provider information
- **parameters**: Input parameters used during execution

### Redacting Sensitive Values

Use `--redact` to replace sensitive resolver values with `<redacted>`:

```shell
scafctl run resolver --snapshot --snapshot-file=snapshot.json --redact -f demo.yaml
```

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

```shell
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

## Working Directory Override

Use the `--cwd` (or `-C`) global flag to run commands as if you were in a different directory. This is useful when scripting or when your solution files live in a different location:

```shell
# Run resolvers from a solution in another directory
scafctl --cwd /path/to/project run resolver -f solution.yaml

# Short form (similar to git -C)
scafctl -C /path/to/project run resolver -f solution.yaml

# Combine with other flags
scafctl -C /path/to/project run resolver -f solution.yaml -o json
```

All relative paths (including `-f`, `--output-dir`, etc.) are resolved against the specified working directory instead of the current directory.

---

## Debugging Dependencies

A common debugging workflow is to inspect how resolver dependencies cascade.

### Step 1: Visualize the Graph

```shell
scafctl run resolver --graph -f dep-demo.yaml
```

### Step 2: Run Target Resolver

```shell
scafctl run resolver endpoint -f dep-demo.yaml -o json
```

### Step 3: Inspect a Single Resolver's Value

```shell
scafctl run resolver base_url -f dep-demo.yaml -o json
```

This lets you progressively narrow down which resolver is producing unexpected output.

---

## Working with Parameters

Parameters can be passed in two equivalent ways:

1. **Positional `key=value`** (recommended) — after resolver names or on their own
2. **Explicit `-r` flag** — repeatable flag for each parameter

Both forms can be mixed freely.

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
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
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
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
{{< /shell-pane >}}
{{< /shell-toggle >}}

### Dynamic Help Text

When you specify a solution file with `--help`, the command shows the solution's
resolver parameters alongside the standard help text:

```shell
scafctl run resolver -f param-demo.yaml --help
```

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

```shell
scafctl run resolver -f param-demo.yaml environment=staging -o json
```

Output:

```json
{
  "config_url": "https://config.staging.example.com",
  "env": "staging"
}
```

### Default Values

The parameter provider supports an optional `default` input that provides a
fallback value when the parameter is not passed via `-r` flags. This is simpler
than using a multi-step fallback chain with `continueOnError: true` and a `static`
provider.

Create a file called `param-default-demo.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: param-default-demo
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
              default: development
```

Running without `-r` uses the default:

```shell
scafctl run resolver -f param-default-demo.yaml -o json
```

Output:

```json
{
  "env": "development"
}
```

Running with `-r` overrides the default:

```shell
scafctl run resolver -f param-default-demo.yaml environment=staging -o json
```

Output:

```json
{
  "env": "staging"
}
```

> **Note:** The `default` input must be a literal value. The resolver executor
> resolves all inputs before calling the provider, so a ValueRef default (e.g.
> `default: {rslvr: fallback}`) would be evaluated even when the parameter
> exists. Use the `onError: continue` fallback chain pattern when you need a
> dynamic default.

### Parameter Key Validation

Parameter keys are validated against the solution's parameter-type resolver names.
Unknown keys are rejected early with a helpful error message that suggests the
closest valid key when a typo is detected:

```shell
# Typo: "envronment" instead of "environment"
scafctl run resolver -f param-demo.yaml envronment=staging
# Error: solution does not accept input "envronment" — did you mean "environment"?

# Completely unknown key
scafctl run resolver -f param-demo.yaml unknown=value
# Error: solution does not accept input "unknown" (valid inputs: environment)
```

### Controlling Parameter Types

By default the parameter provider infers a type from the raw CLI value:
`true`/`false` become booleans, numeric strings become numbers, JSON
objects/arrays are parsed, and `file://` values are loaded. `http://`/`https://`
values are kept as literal strings -- they are not fetched. Inference applies
only to CLI values; an authored `default` keeps its YAML type under `auto` (a
quoted `"false"` stays the string `"false"`, a bare `false` stays a bool), so
quoting a default is the way to keep it a string. When automatic inference
produces the wrong type -- for example a numeric ID with leading zeros that
should stay a string, or a value that must be parsed a specific way -- set the
`type` input to take explicit control.

> [!NOTE]
> When the **enclosing resolver declares a scalar `type`** (`string`, `int`,
> `float`, `bool`), that declared type is authoritative under `auto`: the raw
> CLI value is coerced **directly** to it instead of being inferred and then
> re-coerced. So a `type: string` resolver keeps `-r version=2.0` as `"2.0"`
> (not `"2"`) and `-r billingId=00042` as `"00042"` -- you don't need a separate
> `type: string` on the parameter source. A `type: any` (or untyped) resolver
> keeps normal inference, and an explicit `type` on the parameter source still
> wins.

The `type` input accepts these values:

| Type     | Behavior                                                                 |
| -------- | ------------------------------------------------------------------------ |
| `auto`   | Default. Infers bool, number, JSON, and `file://` for CLI values; an authored default keeps its YAML type. When the enclosing resolver declares a scalar type, the CLI value is coerced directly to it. |
| `string` | Coerces the value to a string (strips surrounding quotes).               |
| `raw`    | Returns the value exactly as received, with no coercion or quote strip.  |
| `int`    | Parses the value as an integer; whole-number float syntax like `2.0` is accepted, but a fractional value like `2.5` errors. |
| `float`  | Parses the value as a floating-point number; errors on failure.          |
| `bool`   | Parses `true`/`false` (case-insensitive); errors on any other value.     |
| `json`   | Parses the value as JSON; errors on invalid JSON.                        |
| `csv`    | Splits the value on commas into a trimmed list of strings.               |
| `fetch`  | Performs an SSRF-guarded HTTP GET and returns the response body.         |

> [!NOTE]
> `auto` never triggers network I/O or list-splitting. A `http://`/`https://`
> value stays the literal string (use `type: fetch` to retrieve the content, or
> the `http` provider for anything beyond a plain GET), and a comma-separated
> value like `a,b,c` stays the string `"a,b,c"` (use `type: csv` to get a list).

An explicit `type` applies to both CLI-supplied values and the `default` input.
When a typed value cannot be parsed (for example `type: int` on `abc`), the
resolver fails with a clear error rather than silently returning the wrong type.

Create a file called `param-string-demo.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: param-string-demo
  version: 1.0.0
spec:
  resolvers:
    # Inferred: "00042" would become the number 42
    billingId:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: billingId
    # Verbatim: stays the string "00042"
    billingIdString:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: billingId
              type: string
    # Opt-in list: "a,b,c" becomes ["a", "b", "c"]
    regions:
      resolve:
        with:
          - provider: parameter
            inputs:
              key: regions
              type: csv
              default: us-east-1
```

```shell
scafctl run resolver -f param-string-demo.yaml billingId=00042 regions=us-east-1,us-west-2 -o json
```

Output -- inference drops the leading zeros, `type: string` preserves them, and
`type: csv` splits the list:

```json
{
  "billingId": 42,
  "billingIdString": "00042",
  "regions": ["us-east-1", "us-west-2"]
}
```

> **Note:** Use `type: string` to keep numeric-looking values usable with CEL
> string functions like `matches()`; `auto` would coerce `8080` to an integer,
> which has no `matches()` method. Use `type: raw` when you want the value
> untouched, including preserving a non-string `default` (such as a YAML integer)
> exactly as written.

---


## Common Workflows

### CI/CD Validation

Check that all resolvers succeed without running actions:

{{< shell-toggle >}}
{{< shell-pane "bash" >}}
```bash
# Basic validation
scafctl run resolver -f demo.yaml -o quiet
echo "Exit code: $?"

# Strict mode: also require official auth handlers to be declared in bundle.plugins
scafctl run resolver -f demo.yaml -o quiet --strict
```
{{< /shell-pane >}}
{{< shell-pane "powershell" >}}
```powershell
# Basic validation
scafctl run resolver -f demo.yaml -o quiet
Write-Output "Exit code: $LASTEXITCODE"

# Strict mode: also require official auth handlers to be declared in bundle.plugins
scafctl run resolver -f demo.yaml -o quiet --strict
```
{{< /shell-pane >}}
{{< /shell-toggle >}}

### Partial Execution for Debugging

Isolate a failing resolver and its dependencies:

```shell
scafctl run resolver endpoint -f dep-demo.yaml -o json
```

### Inspect Raw Resolved Values

Skip transforms to see what providers actually return:

```shell
scafctl run resolver --skip-transform -f dep-demo.yaml -o json
```

### Preview Execution Plan

Use `--graph` to see the resolver execution plan without running anything:

```shell
scafctl run resolver --graph -f dep-demo.yaml
```

### Comparing Resolver Outputs

```shell
# Get current values
scafctl run resolver -f param-demo.yaml -r environment=production -o json > current.json

# Change parameters and compare
scafctl run resolver -f param-demo.yaml -r environment=staging -o json > staging.json
diff current.json staging.json
```

### Show Execution Metrics

```shell
scafctl run resolver -f demo.yaml --show-metrics -o json
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

```shell
# All resolvers
scafctl run resolver -f solution.yaml

# Named resolvers (with dependencies)
scafctl run resolver db config -f solution.yaml

# Named resolvers from a catalog solution (-s/--solution)
scafctl run resolver db config -s my-app@1.0.0

# Resolvers consumed by a specific action (-a/--action)
scafctl run resolver -a deploy -f solution.yaml

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

# Strict mode (also require official auth handlers to be declared)
scafctl run resolver -f solution.yaml --strict

# With parameters
scafctl run resolver -f solution.yaml -r key=value

# Interactive exploration
scafctl run resolver -f solution.yaml -i

# Aliases: res, resolvers
scafctl run res -f solution.yaml
```

## Next Steps

- [Run Provider Tutorial](run-provider-tutorial.md) — Test providers in isolation
- [Actions Tutorial](actions-tutorial.md) — Learn about workflows
- [Resolver Tutorial](resolver-tutorial.md) — Deep dive into resolvers
- [Provider Reference](provider-reference.md) — Complete provider documentation
