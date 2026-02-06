# scafctl

A configuration discovery and scaffolding tool built in Go.

## Features

- **Resolvers**: Gather and transform configuration data from multiple sources
- **Actions**: Execute side-effect operations as a declarative action graph
- **CEL Integration**: Use Common Expression Language for dynamic evaluation
- **Providers**: Extensible provider system (HTTP, shell, file, git, etc.)

## Quick Start

### Resolvers: Compute Data

Resolvers gather and transform configuration data:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-config
  version: 1.0.0

spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: env
            inputs:
              name: ENVIRONMENT
              default: development
```

Run: `scafctl run solution -f config.yaml`

### Actions: Execute Work

Actions perform side-effect operations based on resolver data:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: deploy-workflow
  version: 1.0.0

spec:
  resolvers:
    targets:
      type: array
      resolve:
        with:
          - provider: static
            inputs:
              value: ["server1", "server2"]

  workflow:
    actions:
      deploy:
        provider: shell
        forEach:
          in:
            expr: "_.targets"
        inputs:
          command:
            expr: "'deploy.sh ' + __item"
```

Run: `scafctl run solution -f deploy.yaml`

## Documentation

- [Resolver Tutorial](docs/resolver-tutorial.md) - Getting started with resolvers
- [Actions Tutorial](docs/actions-tutorial.md) - Getting started with actions
- [Authentication Tutorial](docs/auth-tutorial.md) - Setting up and using authentication
- [Examples: Resolvers](examples/resolvers/) - Resolver examples
- [Examples: Actions](examples/actions/) - Action examples

## Authentication

scafctl supports secure authentication for accessing protected APIs. Currently supported:

- **Microsoft Entra ID** (Azure AD) - Device code flow and Service Principal

### Quick Start

```bash
# Interactive: Authenticate with Entra ID (device code)
scafctl auth login entra

# CI/CD: Authenticate with service principal
export AZURE_CLIENT_ID="..."
export AZURE_TENANT_ID="..."
export AZURE_CLIENT_SECRET="..."
scafctl auth login entra --flow service-principal

# Check authentication status
scafctl auth status

# Use authenticated HTTP requests
scafctl run solution -f my-solution.yaml
```

### Authenticated HTTP Requests

Use the `authProvider` and `scope` properties in HTTP providers:

```yaml
spec:
  resolvers:
    me:
      type: object
      resolve:
        with:
          - provider: http
            inputs:
              url: "https://graph.microsoft.com/v1.0/me"
              method: GET
              authProvider: entra
              scope: "https://graph.microsoft.com/.default"
```

See the [Authentication Tutorial](docs/auth-tutorial.md) for more details.

## Actions Overview

The Actions system enables executing operations as a declarative dependency graph:

### Key Features

- **Dependencies**: Actions can depend on other actions
- **Parallel Execution**: Independent actions run in parallel
- **ForEach**: Iterate over arrays with concurrency control
- **Conditions**: Skip actions based on conditions
- **Error Handling**: Continue or fail on errors
- **Retry**: Automatic retry with backoff strategies
- **Timeouts**: Per-action timeout limits
- **Finally**: Cleanup actions that always run

### Example: CI/CD Pipeline

```yaml
workflow:
  actions:
    build:
      provider: shell
      inputs:
        command: "go build ./..."

    test:
      provider: shell
      dependsOn: [build]
      inputs:
        command: "go test ./..."

    deploy:
      provider: shell
      dependsOn: [test]
      forEach:
        in:
          expr: "_.servers"
        concurrency: 2
      inputs:
        command:
          expr: "'deploy.sh ' + __item"

  finally:
    notify:
      provider: http
      inputs:
        url: "https://slack.webhook/..."
```

## CLI Commands

```bash
# Run a solution (resolvers + actions)
scafctl run solution -f config.yaml

# Run with progress output
scafctl run solution -f config.yaml --progress

# Run with JSON output for scripts/pipelines
scafctl run solution -f config.yaml -o json

# Dry run (show what would execute)
scafctl run solution -f config.yaml --dry-run

# Run resolvers only (skip actions)
scafctl run solution -f config.yaml --skip-actions

# Render solution to artifact
scafctl render solution -f config.yaml -o json
scafctl render solution -f config.yaml -o yaml
```

