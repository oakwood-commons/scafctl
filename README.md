# scafctl

[![Go Report Card](https://goreportcard.com/badge/github.com/oakwood-commons/scafctl)](https://goreportcard.com/report/github.com/oakwood-commons/scafctl)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/oakwood-commons/scafctl)](https://github.com/oakwood-commons/scafctl/releases)
[![CI](https://github.com/oakwood-commons/scafctl/actions/workflows/test.yml/badge.svg)](https://github.com/oakwood-commons/scafctl/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/oakwood-commons/scafctl/graph/badge.svg)](https://codecov.io/gh/oakwood-commons/scafctl)
[![Documentation](https://img.shields.io/badge/docs-github.io-blue)](https://oakwood-commons.github.io/scafctl/)

> **Alpha** — scafctl is under active development. APIs and CLI commands may
> change between releases. Breaking changes are documented in release notes.
> Questions? Open an issue or start a
> [Discussion](https://github.com/oakwood-commons/scafctl/discussions).

Define, discover, and deliver configuration as code using CEL-powered solutions.

scafctl is a CLI tool that lets you declaratively gather data from any source (APIs, files, environment, Git, and more), transform it with [CEL](https://cel.dev/) expressions, and execute side-effect workflows — all defined in a single **Solution** file.

### Core Concepts

- **Solution** — A YAML file that declares what data to gather and what work to do. Solutions are versionable, composable, and shareable via OCI registries.
- **Resolver** — A named unit that gathers or computes a value using one or more providers. Resolvers can depend on each other and execute in parallel when possible.
- **Action** — A side-effect operation (run a command, call an API, write a file) organized into a dependency graph with support for parallelism, retries, conditions, and forEach loops.
- **Provider** — A pluggable backend that does the actual work (e.g. `http`, `exec`, `file`, `cel`). scafctl ships with 16 built-in providers and supports external plugins.

## Installation

### Homebrew (macOS / Linux)

```bash
brew install oakwood-commons/tap/scafctl
```

### From Release Binaries

Download the latest binary for your platform from the
[GitHub Releases](https://github.com/oakwood-commons/scafctl/releases) page.

#### macOS / Linux

```bash
# Download (replace VERSION and OS/ARCH as needed)
curl -LO https://github.com/oakwood-commons/scafctl/releases/latest/download/scafctl_VERSION_OS_ARCH.tar.gz
tar xzf scafctl_*.tar.gz
sudo mv scafctl /usr/local/bin/
```

> **macOS note:** You may need to remove the quarantine attribute before running:
> ```bash
> xattr -dr 'com.apple.quarantine' /usr/local/bin/scafctl
> ```

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
- **Providers**: 16 built-in providers (HTTP, exec, file, directory, git, CEL, and more)
- **Catalog**: Publish, version, and share reusable solutions via OCI registries
- **Secrets**: Encrypted secrets management with OS keyring integration
- **Plugins**: Extend scafctl with custom providers via a plugin system
- **Snapshots**: Capture and diff resolver output over time
- **Linting**: Validate solution files for correctness before execution
- **Logging**: Quiet by default with user-controlled verbosity, format, and file output

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
```

Run: `scafctl run solution -f deploy.yaml`

## Built-in Providers

scafctl ships with 16 providers. Use `scafctl explain provider <name>` to see full schema and examples.

| Provider | Description |
| ---------- | ------------- |
| `cel` | Evaluate CEL expressions |
| `debug` | Log debug information during execution |
| `directory` | List, create, remove, and copy directories |
| `env` | Read environment variables |
| `exec` | Execute shell commands |
| `file` | Read and write files |
| `git` | Query Git repository metadata |
| `go-template` | Render Go templates |
| `http` | Make HTTP requests |
| `identity` | Pass input through unchanged |
| `parameter` | Access CLI-provided parameters |
| `secret` | Read encrypted secrets |
| `sleep` | Pause execution for a duration |
| `solution` | Compose sub-solutions recursively |
| `static` | Return static values |
| `validation` | Validate values against rules |

See the [Provider Reference](docs/tutorials/provider-reference.md) and [Provider Development](docs/tutorials/provider-development.md) tutorials.

## Documentation

### Tutorials

- [Getting Started](docs/tutorials/getting-started.md) — First steps with scafctl
- [Resolvers](docs/tutorials/resolver-tutorial.md) — Gathering and transforming data
- [Actions](docs/tutorials/actions-tutorial.md) — Executing operations
- [Authentication](docs/tutorials/auth-tutorial.md) — Entra ID and service principals
- [Catalog](docs/tutorials/catalog-tutorial.md) — Publishing and sharing solutions
- [CEL Expressions](docs/tutorials/cel-tutorial.md) — Dynamic evaluation with CEL
- [Go Templates](docs/tutorials/go-templates-tutorial.md) — Templating with Go templates
- [Configuration](docs/tutorials/config-tutorial.md) — Managing application settings
- [Logging](docs/tutorials/logging-tutorial.md) — Controlling log output, formats, and destinations
- [Caching](docs/tutorials/cache-tutorial.md) — Provider result caching
- [Directory Provider](docs/tutorials/directory-provider-tutorial.md) — Listing, scanning, and managing directories
- [Snapshots](docs/tutorials/snapshots-tutorial.md) — Capturing and diffing output
- [Provider Development](docs/tutorials/provider-development.md) — Building providers (builtin and plugin)
- [Auth Handler Development](docs/tutorials/auth-handler-development.md) — Building auth handlers (builtin and plugin)
- [Extension Concepts](docs/tutorials/extension-concepts.md) — Provider vs Auth Handler vs Plugin terminology
- [Plugin Development](docs/tutorials/plugin-development.md) — Plugin overview and discovery

### Design Documents

Architecture and design decisions are documented in [docs/design/](docs/design/).

### Examples

- [Resolver examples](examples/resolvers/)
- [Action examples](examples/actions/)
- [Catalog examples](examples/catalog/)
- [Solution examples](examples/solutions/)

## Authentication

scafctl supports secure authentication for accessing protected APIs. Currently supported:

- **Microsoft Entra ID** (Azure AD) - Device code flow and Service Principal

### Quick Auth Setup

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

## Catalog

The catalog system lets you publish, version, and share solutions using OCI-compatible registries:

```bash
# Build a solution into an OCI artifact
scafctl build solution -f solution.yaml

# Push to a registry
scafctl catalog push my-solution@1.0.0 --registry ghcr.io/myorg

# Pull and inspect
scafctl catalog pull my-solution@1.0.0
scafctl catalog inspect my-solution@1.0.0

# List local catalog entries
scafctl catalog list
```

See the [Catalog Tutorial](docs/tutorials/catalog-tutorial.md) and [Catalog Design](docs/design/catalog.md).

## Configuration

scafctl stores its configuration in XDG-compliant paths. Use the `config` command to manage settings:

```bash
# Initialize default config
scafctl config init

# View current config
scafctl config show

# Set / get / unset values
scafctl config set defaultOutput json
scafctl config get defaultOutput
scafctl config unset defaultOutput

# Manage catalog registries
scafctl config add-catalog my-registry --url ghcr.io/myorg
scafctl config remove-catalog my-registry
```

See the [Configuration Tutorial](docs/tutorials/config-tutorial.md).

## Logging

By default, scafctl produces **no structured log output** — only styled user messages (errors, warnings, success). This keeps the CLI clean for human users and pipe-friendly for scripts.

### Quick Reference

```bash
# Default: no logs, just styled errors/warnings
scafctl run solution -f solution.yaml

# Enable debug logging (human-readable console format)
scafctl run solution -f solution.yaml --debug
scafctl run solution -f solution.yaml --log-level debug

# JSON structured logs (for log aggregation / piping)
scafctl run solution -f solution.yaml --log-level info --log-format json

# Write logs to a file instead of stderr
scafctl run solution -f solution.yaml --debug --log-file /tmp/scafctl.log

# Environment variables (useful for CI/CD)
export SCAFCTL_DEBUG=1             # Enable debug logging
export SCAFCTL_LOG_LEVEL=info      # Set specific level
export SCAFCTL_LOG_FORMAT=json     # Set format
export SCAFCTL_LOG_PATH=/tmp/scafctl.log  # Log to file
```

### Log Levels

| Level | Description |
|-------|-------------|
| `none` | No log output (default) |
| `error` | Errors only |
| `warn` | Warnings and errors |
| `info` | Informational messages |
| `debug` | Verbose debugging (V-level 1) |
| `trace` | Very verbose (V-level 2) |
| `1`-`3` | Numeric V-levels for fine-grained control |

### Precedence

Flag (`--log-level`) > `--debug` > environment variable > config file > default (`none`)

See the [Logging Tutorial](docs/tutorials/logging-tutorial.md) for detailed examples.

## Secrets

scafctl provides encrypted secrets management backed by the OS keyring or the `SCAFCTL_SECRET_KEY` environment variable:

```bash
# Store a secret
scafctl secrets set my-api-key

# Retrieve / check / list
scafctl secrets get my-api-key
scafctl secrets exists my-api-key
scafctl secrets list

# Include internal secrets (e.g. auth tokens)
scafctl secrets list --all
scafctl secrets get scafctl.auth.entra.metadata --all

# Rotate encryption key
scafctl secrets rotate

# Import / export for migration
scafctl secrets export --file secrets.enc
scafctl secrets import --file secrets.enc
```

Use the `secret` provider to access secrets in solutions:

```yaml
spec:
  resolvers:
    api_key:
      type: string
      resolve:
        with:
          - provider: secret
            inputs:
              name: my-api-key
```

## Plugins

scafctl supports external plugins that extend the provider and auth handler systems. Plugins are standalone executables that communicate via gRPC.

See the [Plugin Design](docs/design/plugins.md), [Provider Development Guide](docs/tutorials/provider-development.md#delivering-as-a-plugin), and [Auth Handler Development Guide](docs/tutorials/auth-handler-development.md#delivering-as-a-plugin).

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

    test:
      provider: exec
      dependsOn: [build]
      inputs:
        command: "go test ./..."

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

  finally:
    notify:
      provider: http
      inputs:
        url: "https://slack.webhook/..."
```

## CLI Commands

scafctl is organized into command groups. Run `scafctl <command> --help` for details on any command.

```bash
# Run a solution (resolvers + actions)
scafctl run solution -f config.yaml
scafctl run solution -f config.yaml -o json      # JSON output
scafctl run solution -f config.yaml --dry-run     # Dry run
scafctl run solution -f config.yaml --progress    # Progress output

# Run resolvers only (debugging and inspection)
scafctl run resolver -f config.yaml
scafctl run resolver db config -f config.yaml     # Specific resolvers
scafctl run resolver --verbose -f config.yaml     # Verbose mode

# Render solution to structured output
scafctl render solution -f config.yaml -o json
scafctl render solution -f config.yaml -o yaml

# Build and bundle solutions
scafctl build solution -f solution.yaml

# Catalog operations
scafctl catalog list
scafctl catalog push my-solution@1.0.0
scafctl catalog pull my-solution@1.0.0
scafctl catalog inspect my-solution@1.0.0

# Bundle verification and diffing
scafctl bundle verify my-solution@1.0.0
scafctl bundle diff my-solution@1.0.0 my-solution@2.0.0
scafctl bundle extract my-solution@1.0.0

# Inspect providers and solutions
scafctl explain provider http
scafctl explain solution -f solution.yaml
scafctl get provider
scafctl get resolver -f solution.yaml

# Lint and validate
scafctl lint -f solution.yaml

# Snapshots
scafctl snapshot save -f solution.yaml --name baseline
scafctl snapshot show --name baseline
scafctl snapshot diff --from baseline --to current

# Resolver dependency graph
scafctl run resolver -f solution.yaml --graph

# Configuration and secrets
scafctl config show
scafctl secrets list
scafctl secrets list --all  # include auth tokens

# Authentication
scafctl auth login entra
scafctl auth status

# Dependency vendoring
scafctl vendor update -f solution.yaml

# Cache management
scafctl cache info
scafctl cache clear

# Version
scafctl version
```

## Contributing

Contributions are welcome! Please see:

- [CONTRIBUTING.md](CONTRIBUTING.md) — Development setup, coding standards, and contribution workflow
- [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) — Community guidelines
- [SECURITY.md](.github/SECURITY.md) — Reporting security vulnerabilities

Have a question? Start a [GitHub Discussion](https://github.com/oakwood-commons/scafctl/discussions).

## License

This project is licensed under the Apache License 2.0 — see the [LICENSE](LICENSE) file for details.

