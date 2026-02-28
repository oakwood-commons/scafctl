# scafctl Examples

This directory contains runnable examples demonstrating scafctl features. Each example is self-contained and can be run directly.

## Quick Start

```bash
# Run any example (full solution with actions)
scafctl run solution -f examples/<path-to-example>.yaml

# Run resolvers only (debugging/inspection)
scafctl run resolver -f examples/resolver-demo.yaml

# Run specific resolvers with verbose output
scafctl run resolver environment region -f examples/resolver-demo.yaml --verbose

# Run with parameters
scafctl run resolver -f examples/resolvers/parameters.yaml -r name=Alice

# Run with debug logging (see what scafctl is doing)
scafctl run solution -f examples/actions/hello-world.yaml --debug

# Run with JSON logs for troubleshooting
scafctl run solution -f examples/actions/hello-world.yaml --log-level info --log-format json
```

### Browse Examples from the CLI

You can also discover and download examples using the built-in `examples` command:

```bash
# List all available examples
scafctl examples list

# Filter by category
scafctl examples list --category resolvers
scafctl examples list --category actions
scafctl examples list --category solutions

# Download an example
scafctl examples get resolvers/hello-world.yaml -o hello.yaml

# List in JSON format
scafctl examples list -o json
```

---

## Directory Structure

| Directory | Description |
|-----------|-------------|
| [actions/](actions/) | Workflow automation with actions |
| [exec/](exec/) | Exec provider shell execution patterns |
| [providers/](providers/) | Provider input file examples for `scafctl run provider` |
| [resolvers/](resolvers/) | Dynamic value resolution patterns |
| [solutions/](solutions/) | Complete end-to-end solution examples |
| [snapshots/](snapshots/) | Execution snapshot capture and comparison |
| [catalog/](catalog/) | Catalog bundling and registry examples |
| [config/](config/) | Configuration file examples |
| [eval/](eval/) | CEL and Go template evaluation data and templates |
| [plugins/](plugins/) | go-plugin source code examples (see note below) |
| [mcp/](mcp/) | MCP server configurations for AI clients |

> **Terminology Note**: The `plugins/` directory contains go-plugin source code for developing custom providers. When distributing via the catalog, these are pushed as **provider** or **auth-handler** artifacts:
> ```bash
> # Build and push a provider to the catalog
> scafctl build provider ./my-provider --version 1.0.0
> scafctl catalog push my-provider@1.0.0 --catalog ghcr.io/myorg
> 
> # The artifact is stored at: ghcr.io/myorg/providers/my-provider:1.0.0
> ```

---

## Actions Examples

Action workflows demonstrate executing tasks with dependencies, parallelism, and error handling.

| Example | Description | Run |
|---------|-------------|-----|
| [hello-world.yaml](actions/hello-world.yaml) | Simplest action workflow | `scafctl run solution -f examples/actions/hello-world.yaml` |
| [sequential-chain.yaml](actions/sequential-chain.yaml) | Actions running in sequence | `scafctl run solution -f examples/actions/sequential-chain.yaml` |
| [parallel-with-deps.yaml](actions/parallel-with-deps.yaml) | Parallel execution with dependencies | `scafctl run solution -f examples/actions/parallel-with-deps.yaml` |
| [conditional-execution.yaml](actions/conditional-execution.yaml) | Skip actions based on conditions | `scafctl run solution -f examples/actions/conditional-execution.yaml` |
| [foreach-deploy.yaml](actions/foreach-deploy.yaml) | Iterate over arrays with forEach | `scafctl run solution -f examples/actions/foreach-deploy.yaml` |
| [retry-backoff.yaml](actions/retry-backoff.yaml) | Retry with exponential backoff | `scafctl run solution -f examples/actions/retry-backoff.yaml` |
| [error-handling.yaml](actions/error-handling.yaml) | Handle errors gracefully | `scafctl run solution -f examples/actions/error-handling.yaml` |
| [finally-cleanup.yaml](actions/finally-cleanup.yaml) | Cleanup actions that always run | `scafctl run solution -f examples/actions/finally-cleanup.yaml` |
| [template-render.yaml](actions/template-render.yaml) | Render files from templates | `scafctl run solution -f examples/actions/template-render.yaml` |
| [go-template-inline.yaml](actions/go-template-inline.yaml) | Inline Go templates with loops/conditionals | `scafctl run solution -f examples/actions/go-template-inline.yaml` |
| [complex-workflow.yaml](actions/complex-workflow.yaml) | Full CI/CD-style workflow | `scafctl run solution -f examples/actions/complex-workflow.yaml` |

---

## Eval Examples

Data files and templates for use with `scafctl eval` commands.

| Example | Description | Run |
|---------|-------------|-----|
| [cel-data.json](eval/cel-data.json) | Sample data for CEL expression evaluation | `scafctl eval cel '_.items.filter(i, i.active).map(i, i.name)' --data-file examples/eval/cel-data.json` |
| [deployment.tmpl](eval/deployment.tmpl) | Kubernetes deployment template | `scafctl eval template --template-file examples/eval/deployment.tmpl --data-file examples/eval/template-data.json` |
| [template-data.json](eval/template-data.json) | Data for Go template rendering | Used with `deployment.tmpl` above |

---

## Exec Provider Examples

Demonstrate the exec provider's embedded POSIX shell, shell types, and cross-platform patterns.

| Example | Description | Run |
|---------|-------------|-----|
| [simple.yaml](exec/simple.yaml) | Basic commands, arguments, working directory | `scafctl run solution -f examples/exec/simple.yaml` |
| [shell-features.yaml](exec/shell-features.yaml) | Pipes, variables, conditionals, loops | `scafctl run solution -f examples/exec/shell-features.yaml` |
| [shell-types.yaml](exec/shell-types.yaml) | Embedded, bash, and PowerShell shells | `scafctl run solution -f examples/exec/shell-types.yaml` |
| [environment-and-io.yaml](exec/environment-and-io.yaml) | Environment vars, stdin, timeouts | `scafctl run solution -f examples/exec/environment-and-io.yaml` |
| [cross-platform.yaml](exec/cross-platform.yaml) | Patterns that work on all platforms | `scafctl run solution -f examples/exec/cross-platform.yaml` |

---

## Resolver Examples

Resolvers demonstrate dynamic value computation, validation, and transformation.

| Example | Description | Run |
|---------|-------------|-----|
| [hello-world.yaml](resolvers/hello-world.yaml) | Simplest resolver | `scafctl run resolver -f examples/resolvers/hello-world.yaml` |
| [parameters.yaml](resolvers/parameters.yaml) | CLI parameters with defaults | `scafctl run resolver -f examples/resolvers/parameters.yaml -r name=Alice` |
| [dependencies.yaml](resolvers/dependencies.yaml) | Resolver dependencies & phases | `scafctl run resolver -f examples/resolvers/dependencies.yaml` |
| [env-config.yaml](resolvers/env-config.yaml) | Environment-based configuration | `scafctl run resolver -f examples/resolvers/env-config.yaml -r env=production` |
| [validation.yaml](resolvers/validation.yaml) | Input validation patterns | `scafctl run resolver -f examples/resolvers/validation.yaml` |
| [transform-pipeline.yaml](resolvers/transform-pipeline.yaml) | Data transformation pipeline | `scafctl run resolver -f examples/resolvers/transform-pipeline.yaml` |
| [feature-flags.yaml](resolvers/feature-flags.yaml) | Feature flag patterns | `scafctl run resolver -f examples/resolvers/feature-flags.yaml` |
| [secrets.yaml](resolvers/secrets.yaml) | Secret management (requires secret store) | `scafctl run resolver -f examples/resolvers/secrets.yaml` |
| [identity.yaml](resolvers/identity.yaml) | Authentication identity info (requires auth) | `scafctl run resolver -f examples/resolvers/identity.yaml` |
| [cel-extensions.yaml](resolvers/cel-extensions.yaml) | All custom CEL extension functions | `scafctl run resolver -f examples/resolvers/cel-extensions.yaml -o json` |
| [cel-transforms.yaml](resolvers/cel-transforms.yaml) | Data transformation patterns with CEL | `scafctl run resolver -f examples/resolvers/cel-transforms.yaml -o yaml` |
| [go-template-sprig.yaml](resolvers/go-template-sprig.yaml) | Sprig v3 functions in Go templates | `scafctl run resolver -f examples/resolvers/go-template-sprig.yaml -o json` |
| [go-template-extensions.yaml](resolvers/go-template-extensions.yaml) | Custom Go template extensions (toHcl, toYaml, fromYaml) | `scafctl run resolver -f examples/resolvers/go-template-extensions.yaml -o json` |

---

## Solution Examples

Complete solutions demonstrating real-world use cases.

| Example | Description |
|---------|-------------|
| [comprehensive/](solutions/comprehensive/) | Full-featured solution with all capabilities |
| [terraform/](solutions/terraform/) | Terraform project scaffolding |
| [taskfile/](solutions/taskfile/) | Taskfile.yaml generation |
| [email-notifier/](solutions/email-notifier/) | Email notification workflow |
| [k8s-clusters/](solutions/k8s-clusters/) | Read a Go template file, iterate 10 K8s clusters, render and write unique manifests |
| [bad-solution-yaml/](solutions/bad-solution-yaml/) | Invalid solution demonstrating error handling for conflicting ValueRef keys |
| [tested-solution/](solutions/tested-solution/) | Functional testing features: assertions, inheritance, tags, watch mode |
| [scaffold-demo/](solutions/scaffold-demo/) | Test scaffolding with `scafctl test init` — generates starter test suites |
| [github-auth/](solutions/github-auth/) | GitHub authentication — identity, API calls, and status checks |

---

## Snapshot Examples

Snapshots capture resolver execution state for debugging, testing, and comparison.

| Example | Description | Run |
|---------|-------------|-----|
| [basic-snapshot.yaml](snapshots/basic-snapshot.yaml) | Capture and inspect a snapshot | `scafctl render solution -f examples/snapshots/basic-snapshot.yaml --snapshot --snapshot-file=/tmp/snapshot.json` |
| [snapshot-diff.yaml](snapshots/snapshot-diff.yaml) | Compare snapshots across environments | See example header for commands |
| [redacted-snapshot.yaml](snapshots/redacted-snapshot.yaml) | Redact sensitive values in snapshots | `scafctl render solution -f examples/snapshots/redacted-snapshot.yaml --snapshot --snapshot-file=/tmp/redacted.json --redact` |

---

## Catalog Examples

Catalog build and distribution examples.

| Example | Description |
|---------|-------------|
| [bundling-example/](catalog/bundling-example/) | Solution bundling with local file dependencies |
| [remote-registry-workflow.md](catalog/remote-registry-workflow.md) | Guide for remote OCI registry workflows |

---

## Configuration Examples

Application configuration file examples.

| Example | Description |
|---------|-------------|
| [minimal-config.yaml](config/minimal-config.yaml) | Minimal configuration |
| [full-config.yaml](config/full-config.yaml) | Full configuration with all options |

---

## Demo Files

Standalone demo files in the root examples directory:

| File | Description |
|------|-------------|
| [resolver-demo.yaml](resolver-demo.yaml) | Interactive resolver demonstration |
| [resolver-stress-demo.yaml](resolver-stress-demo.yaml) | Performance testing with many resolvers |
| [resolver-validation-failures-demo.yaml](resolver-validation-failures-demo.yaml) | Demonstrates validation error handling |

---

## Plugin Examples

Custom plugin development examples.

| Example | Description |
|---------|-------------|
| [echo/](plugins/echo/) | Simple echo plugin implementation |
| [auto-fetch-solution.yaml](plugins/auto-fetch-solution.yaml) | Solution demonstrating automatic plugin fetching from catalogs |

---

## MCP Server Configurations

AI client configuration examples for connecting to scafctl's MCP server.

| File | Description |
|------|-------------|
| [mcp/README.md](mcp/README.md) | Overview and setup instructions |
| [mcp/vscode-mcp.json](mcp/vscode-mcp.json) | VS Code / GitHub Copilot configuration |
| [mcp/claude-desktop-config.json](mcp/claude-desktop-config.json) | Claude Desktop configuration |
| [mcp/cursor-mcp.json](mcp/cursor-mcp.json) | Cursor configuration |
| [mcp/windsurf-mcp.json](mcp/windsurf-mcp.json) | Windsurf configuration |

---

## Tips

### View Output as JSON
```bash
scafctl run resolver -f examples/resolvers/hello-world.yaml -o json
```

### Dry Run Mode
```bash
scafctl run solution -f examples/actions/hello-world.yaml --dry-run
```

### Debug Logging
```bash
scafctl run resolver -f examples/resolvers/dependencies.yaml -v
```

### Pass Multiple Parameters
```bash
scafctl run resolver -f examples/resolvers/parameters.yaml \
  -r name=Bob \
  -r count=5 \
  -r uppercase=true
```

### Interactive Mode
```bash
# Explore output in a TUI (navigate, search, filter)
scafctl run resolver -f examples/resolvers/dependencies.yaml -i
```

### Filter with CEL Expressions
```bash
# Extract specific values from output
scafctl run resolver -f examples/resolvers/dependencies.yaml -e '_.fullName'
```

### Snapshots
```bash
# Capture a snapshot during render
scafctl render solution -f examples/snapshots/basic-snapshot.yaml \
  --snapshot --snapshot-file=/tmp/snapshot.json

# View the snapshot
scafctl snapshot show /tmp/snapshot.json

# Compare two snapshots
scafctl snapshot diff /tmp/snap-a.json /tmp/snap-b.json
```

---

## Catalog Workflows

### Build and Run from Catalog
```bash
# Build a solution into the catalog
scafctl build solution examples/resolver-demo.yaml --version 1.0.0

# Run by name (no file path needed)
scafctl run resolver resolver-demo

# List catalog contents
scafctl catalog list
```

### Export and Import (Air-Gapped Transfer)
```bash
# Build and export a solution
scafctl build solution examples/resolver-demo.yaml --version 1.0.0
scafctl catalog save resolver-demo -o resolver-demo.tar

# Transfer the tar file to another machine, then import
scafctl catalog load --input resolver-demo.tar

# Run the imported solution
scafctl run resolver resolver-demo
```

### Version Management
```bash
# Build multiple versions
scafctl build solution examples/resolver-demo.yaml --version 1.0.0
scafctl build solution examples/resolver-demo.yaml --version 2.0.0

# Export specific version
scafctl catalog save resolver-demo@1.0.0 -o resolver-demo-v1.tar

# Delete old versions
scafctl catalog delete resolver-demo@1.0.0

# Clean up orphaned blobs
scafctl catalog prune
```
