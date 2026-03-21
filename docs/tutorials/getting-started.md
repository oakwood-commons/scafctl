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

{{< tabs "getting-started-cmd-1" >}}
{{% tab "Bash" %}}
```bash
# Build from source
go build -ldflags "-s -w" -o scafctl ./cmd/scafctl/scafctl.go

# Move to PATH
mv scafctl /usr/local/bin/
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Build from source
go build -ldflags "-s -w" -o scafctl ./cmd/scafctl/scafctl.go

# Move to PATH (macOS/Linux)
Move-Item -Force ./scafctl /usr/local/bin/scafctl
```
{{% /tab %}}
{{< /tabs >}}

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

{{< tabs "getting-started-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl run solution -f hello.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run solution -f hello.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
Hello, World!
```

This solution has two parts:

- **Resolver** (`greeting`) — gathers a value using the `static` provider
- **Action** (`say-hello`) — runs a shell command that references the resolver via `_.greeting`

> [!NOTE]
> **Want to go deeper with resolvers?** The [Resolver Tutorial](resolver-tutorial.md) covers parameters, dependencies, transforms, validation, and more.

### 2. Action Dependencies

Actions can depend on other actions and access their results. scafctl automatically infers dependencies from `__actions` references in CEL expressions and Go templates. Create a new file called `action-deps.yaml`:

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
        provider: exec
        inputs:
          command:
            expr: "'echo Got status: ' + string(__actions['fetch-data'].results.statusCode)"
```

Because `process` references `__actions['fetch-data']`, scafctl automatically determines that `process` depends on `fetch-data` and schedules it to run after `fetch-data` completes. You can still use `dependsOn` to declare dependencies that aren't expressed via `__actions` references (e.g., ordering actions that don't consume each other's results).

Run it:

{{< tabs "getting-started-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl run solution -f action-deps.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run solution -f action-deps.yaml
```
{{% /tab %}}
{{< /tabs >}}

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
| `hcl` | Parse Terraform/OpenTofu HCL files |
| `validation` | Data validation |
| `secret` | Encrypted secrets |

See [Provider Reference](provider-reference.md) for full documentation.

## Common Commands

{{< tabs "getting-started-cmd-4" >}}
{{% tab "Bash" %}}
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

# Show resolver dependency graph
scafctl run resolver --graph -f solution.yaml

# List available providers
scafctl get provider

# Get details about a specific provider
scafctl get provider http

# Pass parameters
scafctl run solution -f solution.yaml -r key1=value1 -r key2=value2

# Interactive output exploration
scafctl run resolver -f solution.yaml -i

# JSON/YAML output
scafctl render solution -f solution.yaml -o json

# Scaffold a new solution
scafctl new solution --name my-app --description "My application scaffold" --output my-app.yaml

# Browse and download examples
scafctl examples list
scafctl examples get resolvers/hello-world.yaml -o hello.yaml

# Evaluate a CEL expression
scafctl eval cel --expression '"hello".upperAscii()'

# Validate a solution file
scafctl lint -f solution.yaml

# List lint rules
scafctl lint rules

# Explain a lint rule
scafctl lint explain <rule-id>
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
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

# Show resolver dependency graph
scafctl run resolver --graph -f solution.yaml

# List available providers
scafctl get provider

# Get details about a specific provider
scafctl get provider http

# Pass parameters
scafctl run solution -f solution.yaml -r key1=value1 -r key2=value2

# Interactive output exploration
scafctl run resolver -f solution.yaml -i

# JSON/YAML output
scafctl render solution -f solution.yaml -o json

# Scaffold a new solution
scafctl new solution --name my-app --description "My application scaffold" --output my-app.yaml

# Browse and download examples
scafctl examples list
scafctl examples get resolvers/hello-world.yaml -o hello.yaml

# Evaluate a CEL expression
scafctl eval cel --expression '"hello".upperAscii()'

# Validate a solution file
scafctl lint -f solution.yaml

# List lint rules
scafctl lint rules

# Explain a lint rule
scafctl lint explain <rule-id>
```
{{% /tab %}}
{{< /tabs >}}

## Next Steps

- [Solution Scaffolding Tutorial](scaffolding-tutorial.md) — Create new solutions quickly
- [Resolver Tutorial](resolver-tutorial.md) — Deep dive into resolvers
- [Actions Tutorial](actions-tutorial.md) — Learn about workflows
- [Eval Tutorial](eval-tutorial.md) — Test CEL expressions and Go templates
- [Linting Tutorial](linting-tutorial.md) — Validate solutions and explore lint rules
- [Catalog Tutorial](catalog-tutorial.md) — Store and run solutions by name
- [Provider Reference](provider-reference.md) — All providers documented
- [MCP Server Tutorial](mcp-server-tutorial.md) — AI-assisted development
- [Examples](https://github.com/oakwood-commons/scafctl/tree/main/examples) — Working examples
