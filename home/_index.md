---
title: "scafctl"
type: docs
---

# scafctl

**Define, discover, and deliver configuration as code using CEL-powered solutions.**

scafctl is a CLI tool that lets you declaratively gather data from any source — APIs, files, environment variables, Git, and more — transform it with [CEL](https://cel.dev/) expressions, and execute side-effect workflows, all defined in a single **Solution** file.

[![Go Report Card](https://goreportcard.com/badge/github.com/oakwood-commons/scafctl)](https://goreportcard.com/report/github.com/oakwood-commons/scafctl)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://github.com/oakwood-commons/scafctl/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/oakwood-commons/scafctl)](https://github.com/oakwood-commons/scafctl/releases)

---

## Quick Install

```bash
brew install oakwood-commons/tap/scafctl
```

Or download a binary from [GitHub Releases](https://github.com/oakwood-commons/scafctl/releases).

---

## 30-Second Example

Create a file `hello.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: hello
  version: 1.0.0
spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'Hello, ' + 'world!'"
```

Run it:

```bash
scafctl run resolver -f hello.yaml -o yaml
# greeting: Hello, world!
```

---

## Core Concepts

| Concept | Description |
|---------|-------------|
| **Solution** | A YAML file declaring what data to gather and what work to do. Versionable, composable, and shareable via OCI registries. |
| **Resolver** | A named unit that gathers or computes a value using one or more providers. Resolvers can depend on each other and execute in parallel. |
| **Action** | A side-effect operation (run a command, call an API, write a file) organized into a dependency graph with parallelism, retries, conditions, and `forEach` loops. |
| **Provider** | A pluggable backend that does the actual work. scafctl ships with 18 built-in providers and supports external plugins. |

---

## Built-in Providers

scafctl ships with 18 providers out of the box:

`cel` · `debug` · `directory` · `env` · `exec` · `file` · `git` · `github` · `go-template` · `hcl` · `http` · `identity` · `metadata` · `parameter` · `secret` · `sleep` · `static` · `validation`

```bash
# List all providers
scafctl get provider

# Inspect a specific provider's schema and examples
scafctl explain provider http
```

---

## Key Features

- **CEL Integration** — Use [Common Expression Language](https://cel.dev/) for dynamic evaluation, filtering, and transformation
- **Dependency Graph** — Resolvers and actions model their dependencies; scafctl executes in parallel where possible
- **Catalog** — Publish, version, and share reusable solutions via OCI-compatible registries
- **Secrets** — Encrypted secrets management backed by the OS keyring
- **Plugins** — Extend scafctl with custom providers via a gRPC plugin system
- **Snapshots** — Capture and diff resolver output over time
- **Dry Run** — Preview what actions would do without executing them
- **Linting** — Validate solution files before execution
- **Auth** — Microsoft Entra ID (Azure AD) device code and service principal flows

---

## Documentation

### Get Started
- [Getting Started](docs/tutorials/getting-started/) — Install and run your first solution
- [Resolvers](docs/tutorials/resolver-tutorial/) — Gather and transform data
- [Actions](docs/tutorials/actions-tutorial/) — Execute operations

### Go Deeper
- [CEL Expressions](docs/tutorials/cel-tutorial/) — Dynamic evaluation
- [Catalog](docs/tutorials/catalog-tutorial/) — Share solutions via OCI
- [Provider Reference](docs/tutorials/provider-reference/) — All 18 providers

---

## Example: CI/CD Pipeline

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: pipeline
spec:
  resolvers:
    servers:
      type: array
      resolve:
        with:
          - provider: static
            inputs:
              value: ["prod-1", "prod-2"]

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
          url: "https://hooks.slack.com/..."
          method: POST
```

---

## Links

- [GitHub Repository](https://github.com/oakwood-commons/scafctl)
- [Releases](https://github.com/oakwood-commons/scafctl/releases)
- [Discussions](https://github.com/oakwood-commons/scafctl/discussions)
- [Contributing](https://github.com/oakwood-commons/scafctl/blob/main/CONTRIBUTING.md)
