---
title: "scafctl Documentation"
type: docs
---

# scafctl

Define, discover, and deliver configuration as code using CEL-powered solutions.

scafctl is a CLI tool that lets you declaratively gather data from any source (APIs, files, environment, Git, and more), transform it with [CEL](https://cel.dev/) expressions, and execute side-effect workflows — all defined in a single **Solution** file.

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
spec:
  resolvers:
    greeting:
      provider: cel
      expression: "'Hello, ' + 'world!'"
```

```bash
scafctl run solution hello.yaml -o yaml
# greeting: "Hello, world!"
```

## Documentation

- **[Getting Started](tutorials/getting-started/)** — Install scafctl and run your first solution
- **[Tutorials](tutorials/)** — Step-by-step guides for resolvers, actions, CEL, catalogs, and more
- **[Design](design/)** — Architecture, design docs, and contributor guides

## Examples

Runnable examples are located in the [`/examples`](https://github.com/oakwood-commons/scafctl/tree/main/examples) directory:

- [`examples/actions/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/actions) — Action workflow examples
- [`examples/config/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/config) — Configuration examples
- [`examples/resolvers/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/resolvers) — Resolver examples
- [`examples/solutions/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/solutions) — Complete solution examples
- [`examples/plugins/`](https://github.com/oakwood-commons/scafctl/tree/main/examples/plugins) — Plugin examples
