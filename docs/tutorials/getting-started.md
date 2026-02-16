---
title: "Getting Started"
weight: 10
---

# Getting Started with scafctl

This guide will help you get up and running with scafctl in under 10 minutes.

## What is scafctl?

scafctl is a CLI tool for declarative configuration and workflow automation. It uses:

- **Resolvers** to gather and transform data
- **Actions** to perform side effects
- **Providers** as the execution primitives for both

Think of it as a way to define "what you want" (data + operations) in YAML, and let scafctl figure out "how to do it" (dependency order, parallelization, error handling).

## Installation

```bash
# Build from source
go build -ldflags "-s -w" -o scafctl ./cmd/scafctl/scafctl.go

# Move to PATH
mv scafctl /usr/local/bin/
```

## Quick Start

### 1. Your First Solution

Create a file called `hello.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: hello-world
  version: 1.0.0

spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Hello, World!"

  workflow:
    actions:
      say-hello:
        provider: exec
        inputs:
          command:
            expr: "'echo ' + _.greeting"
```

Run it:

```bash
scafctl run solution -f hello.yaml
```

Output:

```
Hello, World!
```

This solution has two parts:

- **Resolver** (`greeting`) — gathers a value using the `static` provider
- **Action** (`say-hello`) — runs a shell command that references the resolver via `_.greeting`

> **Want to go deeper with resolvers?** The [Resolver Tutorial](resolver-tutorial.md) covers parameters, dependencies, transforms, validation, and more.

### 2. Action Dependencies

Actions can depend on other actions and access their results. Create a new file called `action-deps.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: action-deps
  version: 1.0.0

spec:
  workflow:
    actions:
      fetch-data:
        provider: http
        inputs:
          url: https://httpbin.org/get

      process:
        dependsOn: [fetch-data]
        provider: exec
        inputs:
          command:
            expr: "'echo Got status: ' + string(__actions['fetch-data'].results.statusCode)"
```

The `process` action uses `dependsOn` to wait for `fetch-data` to complete, then accesses its results via the `__actions` namespace.

Run it:

```bash
scafctl run solution -f action-deps.yaml
```

Output:

```
Got status: 200
```

## Key Concepts

### Solutions

A **Solution** is the top-level unit. It contains:

- `metadata` - Name, version, description
- `spec.resolvers` - Data gathering/transformation
- `spec.workflow.actions` - Side effects to perform
- `spec.workflow.finally` - Cleanup actions (always run)

### Resolvers

Resolvers compute values. They:

- Execute before any actions
- Form a dependency graph (DAG)
- Run in parallel when possible
- Support fallback chains (try providers in order)

```yaml
resolvers:
  config:
    resolve:
      with:
        - provider: env         # Try environment first
          inputs:
            operation: get
            name: APP_CONFIG
        - provider: file        # Fallback to file
          inputs:
            operation: read
            path: ./config.json
        - provider: static      # Final fallback
          inputs:
            value: "{}"
```

### Actions

Actions perform work. They:

- Execute after all resolvers complete
- Form a dependency graph (DAG)
- Can access resolver data via `_`
- Can access prior action results via `__actions`

### Providers

Providers are the execution primitives. Common ones:

| Provider | Use Case |
|----------|----------|
| `static` | Return constant values |
| `parameter` | CLI parameters (`-r key=value`) |
| `env` | Environment variables |
| `file` | Read/write files |
| `http` | API calls |
| `exec` | Shell commands |
| `cel` | Expression evaluation |
| `validation` | Data validation |
| `secret` | Encrypted secrets |

See [Provider Reference](provider-reference.md) for full documentation.

## Common Commands

```bash
# Run a solution from file
scafctl run solution -f solution.yaml

# Run a solution from catalog (by name)
scafctl run solution my-solution

# Build a solution to local catalog
scafctl build solution solution.yaml --version 1.0.0

# List cataloged solutions
scafctl catalog list

# Dry-run (show what would happen)
scafctl run solution -f solution.yaml --dry-run

# Render without executing
scafctl render solution -f solution.yaml

# Show dependency graph
scafctl resolver graph -f solution.yaml

# List available providers
scafctl get provider

# Explain a provider
scafctl explain provider http

# Pass parameters
scafctl run solution -f solution.yaml -r key1=value1 -r key2=value2

# Interactive output exploration
scafctl render solution -f solution.yaml -i

# JSON/YAML output
scafctl render solution -f solution.yaml -o json
```

## Next Steps

- [Resolver Tutorial](resolver-tutorial.md) — Deep dive into resolvers
- [Actions Tutorial](actions-tutorial.md) — Learn about workflows
- [Catalog Tutorial](catalog-tutorial.md) — Store and run solutions by name
- [Provider Reference](provider-reference.md) — All providers documented
- [Examples](https://github.com/oakwood-commons/scafctl/tree/main/examples) — Working examples
