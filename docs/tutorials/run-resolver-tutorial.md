---
title: "Run Resolver Tutorial"
weight: 25
---

# Run Resolver Tutorial

This tutorial covers the `scafctl run resolver` command — a debugging and inspection tool for executing resolvers without running actions. You'll learn how to run all resolvers, target specific resolvers, inspect execution phases, and use verbose mode for troubleshooting.

## Prerequisites

- scafctl installed and available in your PATH
- Familiarity with [resolvers](resolver-tutorial.md)
- A solution file with defined resolvers

## Table of Contents

1. [Run All Resolvers](#run-all-resolvers)
2. [Run Specific Resolvers](#run-specific-resolvers)
3. [Verbose Mode](#verbose-mode)
4. [Output Formats](#output-formats)
5. [Debugging Dependencies](#debugging-dependencies)
6. [Working with Parameters](#working-with-parameters)
7. [Common Workflows](#common-workflows)

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

## Verbose Mode

Use `--verbose` to include per-resolver execution metadata in the output. This is the primary debugging tool.

```bash
scafctl run resolver --verbose -f dep-demo.yaml -o json
```

When `--verbose` is used, the output includes a `__execution` key alongside the resolver values. The `__execution` key is a structured object with named sections, designed to be extensible for future additions (e.g. diagrams, timeline).

Current sections:

- **`resolvers`**: Per-resolver metadata including phase, duration, status, provider, dependencies, and phase metrics (resolve/transform/validate breakdown)
- **`summary`**: Aggregate stats — total duration, resolver count, phase count

Example JSON output with `--verbose`:

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
scafctl run resolver endpoint --verbose -f dep-demo.yaml -o json
```

The `__execution` section only includes metadata for the requested resolvers and their dependencies.

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

### Step 2: Run Target Resolver with Verbose

```bash
scafctl run resolver endpoint --verbose -f dep-demo.yaml -o json
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
scafctl run resolver failing-resolver --verbose -f solution.yaml -o json
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

---

## Quick Reference

```bash
# All resolvers
scafctl run resolver -f solution.yaml

# Named resolvers (with dependencies)
scafctl run resolver db config -f solution.yaml

# Verbose debugging (includes __execution metadata in output)
scafctl run resolver --verbose -f solution.yaml

# JSON output
scafctl run resolver -f solution.yaml -o json

# With parameters
scafctl run resolver -f solution.yaml -r key=value

# Interactive exploration
scafctl run resolver -f solution.yaml -i

# Aliases: res, resolvers
scafctl run res -f solution.yaml
```
