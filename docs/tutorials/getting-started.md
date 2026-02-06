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

### 2. Using Parameters

Pass values from the command line:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: greet-user
  version: 1.0.0

spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: name
          - provider: static
            inputs:
              value: "World"  # Default if no parameter

  workflow:
    actions:
      greet:
        provider: exec
        inputs:
          command:
            expr: "'echo Hello, ' + _.name + '!'"
```

```bash
scafctl run solution -f greet.yaml -r name=Alice
# Output: Hello, Alice!

scafctl run solution -f greet.yaml
# Output: Hello, World!
```

### 3. Resolver Dependencies

Resolvers can depend on each other using the `_` namespace:

```yaml
spec:
  resolvers:
    firstName:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: first

    lastName:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: last

    fullName:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "_.firstName + ' ' + _.lastName"
```

### 4. Action Dependencies

Actions can depend on other actions and access their results:

```yaml
spec:
  workflow:
    actions:
      fetch-data:
        provider: http
        inputs:
          url: https://api.example.com/config

      process:
        dependsOn: [fetch-data]
        provider: exec
        inputs:
          command:
            expr: "'echo Got status: ' + string(__actions['fetch-data'].results.statusCode)"
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
# Run a solution
scafctl run solution -f solution.yaml

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

- [Resolver Tutorial](resolver-tutorial.md) - Deep dive into resolvers
- [Actions Tutorial](actions-tutorial.md) - Learn about workflows
- [Provider Reference](provider-reference.md) - All providers documented
- [Examples](https://github.com/oakwood-commons/scafctl/tree/main/examples) - Working examples
