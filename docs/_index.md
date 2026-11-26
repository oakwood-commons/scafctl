---
title: "scafctl Documentation"
type: docs
---

# scafctl

**Declaratively discover data, shape configuration, and run portable workflows for humans and AI agents.**

`scafctl` is a cross-platform CLI and workflow engine that turns project setup, configuration, and automation into testable, reusable **Solutions**. Gather live data from APIs, files, Git, and environments, transform and merge configuration safely with [CEL](https://cel.dev/), execute parallel action graphs identically across Linux, macOS, Windows, and CI, distribute versioned bundles through OCI registries, and let AI coding assistants inspect schemas, lint, and preview changes deterministically via `scafctl mcp serve`.

## Quick Install

```bash
brew install oakwood-commons/tap/scafctl
```

Or download a binary from [GitHub Releases](https://github.com/oakwood-commons/scafctl/releases).

## 30-Second Example

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

```shell
scafctl run resolver -f hello.yaml -o yaml
# greeting: Hello, world!
```

## Documentation

- **[Getting Started](tutorials/getting-started/)**: Install `scafctl` and run your first solution
- **[Tutorials](tutorials/)**: Step-by-step guides for resolvers, actions, CEL, catalogs, MCP, and more
- **[Authoring & Publishing Catalog Solutions](tutorials/publishing-solutions-tutorial/)**: Design, test, sign, and publish reusable solutions
- **[MCP Server](tutorials/mcp-server-tutorial/)**: Expose live schemas and dry-run validation to AI coding agents
- **[State Tutorial](tutorials/state-tutorial/)**: Persist resolver values across executions
- **[Design](design/)**: Architecture, design docs, and contributor guides

## Examples

Runnable examples are located in the [`/examples`](https://github.com/oakwood-commons/scafctl/tree/main/examples) directory:

- [`examples/actions/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/actions): Action workflow examples
- [`examples/config/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/config): Configuration examples
- [`examples/exec/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/exec): Shell execution examples
- [`examples/mcp/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/mcp): MCP server integration examples
- [`examples/providers/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/providers): Provider examples
- [`examples/resolvers/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/resolvers): Resolver examples
- [`examples/snapshots/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/snapshots): Snapshot examples
- [`examples/solutions/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/solutions): Complete solution examples
- [`examples/plugins/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/plugins): Plugin examples
- [`examples/serve/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/serve): REST API server examples

Browse examples from the CLI:

```shell
scafctl get examples
scafctl get examples resolvers/hello-world.yaml
```
