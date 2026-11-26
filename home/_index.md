---
title: "scafctl"
description: "Declaratively discover data, shape configuration, and run portable workflows for humans and AI agents."
type: docs
---

<div class="bp-hero">
  <div class="bp-hero__eyebrow"><span class="bp-hero__dot" aria-hidden="true"></span><span>CONFIGURATION FLOW // DECLARATIVE BY DESIGN</span></div>
  <img class="bp-hero__logo" src="images/scafctl-logo.svg" alt="scafctl" width="300" height="93">
  <h1 class="bp-hero__tagline">Declaratively discover data, shape configuration, and run portable workflows for humans and AI agents.</h1>
  <p class="bp-hero__desc"><code>scafctl</code> is a cross-platform CLI and workflow engine that turns project setup, configuration, and automation into testable, reusable <strong>Solutions</strong>. Gather live data from APIs, files, Git, and environments, transform and merge configuration safely with <a href="https://cel.dev/">CEL</a>, execute parallel action graphs across Linux, macOS, Windows, and CI, share versioned bundles via OCI registries, and let AI coding assistants inspect schemas and preview changes deterministically over MCP.</p>
  <div class="bp-hero__actions">
    <a class="bp-hero__btn bp-hero__btn--primary" href="docs/tutorials/getting-started/">Get Started</a>
    <a class="bp-hero__btn bp-hero__btn--secondary" href="docs/tutorials/">Explore Tutorials</a>
  </div>
  <div class="bp-hero__badges">
    <a href="https://goreportcard.com/report/github.com/oakwood-commons/scafctl"><img src="https://goreportcard.com/badge/github.com/oakwood-commons/scafctl" alt="Go Report Card"></a>
    <a href="https://github.com/oakwood-commons/scafctl/blob/main/LICENSE"><img src="https://img.shields.io/badge/License-Apache_2.0-blue.svg" alt="License"></a>
    <a href="https://github.com/oakwood-commons/scafctl/releases"><img src="https://img.shields.io/github/v/release/oakwood-commons/scafctl" alt="Release"></a>
  </div>
</div>

## 30-Second Walkthrough

{{% steps %}}
1. **Install `scafctl`**

   Install on macOS, Linux, or Windows (`x86_64` and `arm64` archives on [GitHub Releases](https://github.com/oakwood-commons/scafctl/releases/latest)):

   {{< install-tabs "walkthrough" >}}

2. **Define a Solution (`hello.yaml`)**

   Declare resolvers to gather and transform data, and optional workflow actions to execute side effects:

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

3. **Run and Inspect**

   Evaluate resolvers and emit structured YAML or JSON output:

   ```bash
   scafctl run resolver -f hello.yaml -o yaml
   # greeting: Hello, world!
   ```
{{% /steps %}}

---

## Core Concepts

| Concept | Description |
|---------|-------------|
| **Solution** | A YAML file declaring what data to gather and what work to do. Versionable, composable, and shareable via OCI registries. |
| **Resolver** | A named unit that gathers or computes a value using one or more providers. Resolvers can depend on each other and execute in parallel. |
| **Action** | A side-effect operation (run a command, call an API, write a file) organized into a dependency graph with parallelism, retries, conditions, and `forEach` loops. |
| **Provider** | A pluggable backend that does the actual work. scafctl ships with 11 built-in providers and 11 official plugin providers (22 total, auto-resolved from OCI catalogs). |

---

## Providers

scafctl ships with 11 built-in providers and 11 official plugin providers (22 total, auto-resolved on first use):

`cel` | `debug` | `directory` | `env` | `exec` | `file` | `git` | `github` | `go-template` | `hcl` | `http` | `identity` | `kubeconfig` | `message` | `metadata` | `parameter` | `secret` | `sleep` | `solution` | `state` | `static` | `validation`

```bash
# List all providers
scafctl get provider

# Inspect a specific provider's schema and examples
scafctl explain provider http
```

---

## Key Features

- **CEL Integration**: Use [Common Expression Language](https://cel.dev/) for dynamic evaluation, filtering, and transformation
- **Dependency Graph**: Resolvers and actions model their dependencies; `scafctl` executes independent steps in parallel
- **Cross-Platform Shell**: Run shell actions identically on Linux, macOS, and Windows using an embedded POSIX shell with zero WSL or Git Bash dependency
- **AI Agent Integration (`MCP`)**: Expose live schemas, CEL/template evaluation, linting, refactoring, and dry-run previews to AI coding tools via `scafctl mcp serve`
- **Functional Testing**: Verify resolvers, validation rules, and workflows deterministically with `scafctl test functional`
- **Catalog**: Publish, version, and share reusable solutions via OCI-compatible registries
- **Secrets**: Encrypted secrets management backed by the OS keyring
- **Plugins**: Extend `scafctl` with custom providers via a gRPC plugin system
- **Snapshots**: Capture and diff resolver output over time
- **Dry Run**: Preview what actions would do without executing them
- **Linting**: Validate solution files before execution
- **Auth**: Microsoft Entra ID (Azure AD), GitHub, and Google Cloud Platform authentication (interactive browser OAuth + PKCE, device code, service principals, and workload identity)

---

## Documentation

### Get Started
- [Getting Started](docs/tutorials/getting-started/): Install and run your first solution
- [Resolvers](docs/tutorials/resolver-tutorial/): Gather and transform data
- [Actions](docs/tutorials/actions-tutorial/): Execute operations

### Go Deeper
- [CEL Expressions](docs/tutorials/cel-tutorial/): Dynamic evaluation
- [Catalog](docs/tutorials/catalog-tutorial/): Share solutions via OCI
- [Publishing Catalog Solutions](docs/tutorials/publishing-solutions-tutorial/): Design, sign, and publish reusable solutions
- [MCP Server](docs/tutorials/mcp-server-tutorial/): Connect AI coding agents (`scafctl mcp serve`)
- [Provider Reference](docs/tutorials/provider-reference/): All 22 providers

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
