# scafctl

[![Go Report Card](https://goreportcard.com/badge/github.com/oakwood-commons/scafctl)](https://goreportcard.com/report/github.com/oakwood-commons/scafctl)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/oakwood-commons/scafctl)](https://github.com/oakwood-commons/scafctl/releases)
[![CI](https://github.com/oakwood-commons/scafctl/actions/workflows/pr-checks.yml/badge.svg)](https://github.com/oakwood-commons/scafctl/actions/workflows/pr-checks.yml)

> **Alpha** — scafctl is under active development. APIs and CLI commands may
> change between releases. Breaking changes are documented in release notes.
> Questions? Open an issue or start a
> [Discussion](https://github.com/oakwood-commons/scafctl/discussions).

A configuration discovery and scaffolding tool built in Go.

## Installation

### From Release Binaries (Recommended)

Download the latest binary for your platform from the
[GitHub Releases](https://github.com/oakwood-commons/scafctl/releases) page.

#### macOS / Linux

```bash
# Download (replace VERSION and OS/ARCH as needed)
curl -LO https://github.com/oakwood-commons/scafctl/releases/latest/download/scafctl_VERSION_OS_ARCH.tar.gz
tar xzf scafctl_*.tar.gz
sudo mv scafctl /usr/local/bin/
```

#### Windows

Download the `.zip` archive for your architecture from
[GitHub Releases](https://github.com/oakwood-commons/scafctl/releases),
extract it, and add the directory to your `PATH`.

#### From Source

```bash
go install github.com/oakwood-commons/scafctl/cmd/scafctl@latest
```

### Shell Completion

Generate completions for your shell and add them to your profile:

```bash
# Bash
scafctl completion bash > /usr/local/etc/bash_completion.d/scafctl

# Zsh
scafctl completion zsh > "${fpath[1]}/_scafctl"

# Fish
scafctl completion fish > ~/.config/fish/completions/scafctl.fish

# PowerShell
scafctl completion powershell | Out-String | Invoke-Expression
```

Restart your shell (or `source` the file) for completions to take effect.
Zsh users must have `compinit` loaded before the completion file is sourced.

## Features

- **Resolvers**: Gather and transform configuration data from multiple sources
- **Actions**: Execute side-effect operations as a declarative action graph
- **CEL Integration**: Use Common Expression Language for dynamic evaluation
- **Providers**: Extensible provider system (HTTP, exec, file, git, etc.)

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
        provider: exec
        forEach:
          in:
            expr: "_.targets"
        inputs:
          command:
            expr: "'deploy.sh ' + __item"
          shell: true
```

Run: `scafctl run solution -f deploy.yaml`

## Documentation

- [Resolver Tutorial](docs/tutorials/resolver-tutorial.md) - Getting started with resolvers
- [Actions Tutorial](docs/tutorials/actions-tutorial.md) - Getting started with actions
- [Authentication Tutorial](docs/tutorials/auth-tutorial.md) - Setting up and using authentication
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

See the [Authentication Tutorial](docs/tutorials/auth-tutorial.md) for more details.

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
      provider: exec
      inputs:
        command: "go build ./..."
        shell: true

    test:
      provider: exec
      dependsOn: [build]
      inputs:
        command: "go test ./..."
        shell: true

    deploy:
      provider: exec
      dependsOn: [test]
      forEach:
        in:
          expr: "_.servers"
        concurrency: 2
      inputs:
        command:
          expr: "'deploy.sh ' + __item"
        shell: true

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

## Contributing

Contributions are welcome! Please see:

- [CONTRIBUTING.md](CONTRIBUTING.md) — Development setup, coding standards, and contribution workflow
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) — Community guidelines
- [SECURITY.md](.github/SECURITY.md) — Reporting security vulnerabilities

Have a question? Start a [GitHub Discussion](https://github.com/oakwood-commons/scafctl/discussions).

## License

This project is licensed under the Apache License 2.0 — see the [LICENSE](LICENSE) file for details.

