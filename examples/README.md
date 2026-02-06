# scafctl Examples

This directory contains runnable examples demonstrating scafctl features. Each example is self-contained and can be run directly.

## Quick Start

```bash
# Run any example
scafctl run solution -f examples/<path-to-example>.yaml

# Run with parameters
scafctl run solution -f examples/resolvers/parameters.yaml -r name=Alice

# Run with verbose output
scafctl run solution -f examples/actions/hello-world.yaml -v
```

---

## Directory Structure

| Directory | Description |
|-----------|-------------|
| [actions/](actions/) | Workflow automation with actions |
| [resolvers/](resolvers/) | Dynamic value resolution patterns |
| [solutions/](solutions/) | Complete end-to-end solution examples |
| [config/](config/) | Configuration file examples |
| [plugins/](plugins/) | Custom plugin examples |

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
| [complex-workflow.yaml](actions/complex-workflow.yaml) | Full CI/CD-style workflow | `scafctl run solution -f examples/actions/complex-workflow.yaml` |

---

## Resolver Examples

Resolvers demonstrate dynamic value computation, validation, and transformation.

| Example | Description | Run |
|---------|-------------|-----|
| [hello-world.yaml](resolvers/hello-world.yaml) | Simplest resolver | `scafctl run solution -f examples/resolvers/hello-world.yaml` |
| [parameters.yaml](resolvers/parameters.yaml) | CLI parameters with defaults | `scafctl run solution -f examples/resolvers/parameters.yaml -r name=Alice` |
| [dependencies.yaml](resolvers/dependencies.yaml) | Resolver dependencies & phases | `scafctl run solution -f examples/resolvers/dependencies.yaml` |
| [env-config.yaml](resolvers/env-config.yaml) | Environment-based configuration | `scafctl run solution -f examples/resolvers/env-config.yaml -r env=production` |
| [validation.yaml](resolvers/validation.yaml) | Input validation patterns | `scafctl run solution -f examples/resolvers/validation.yaml` |
| [transform-pipeline.yaml](resolvers/transform-pipeline.yaml) | Data transformation pipeline | `scafctl run solution -f examples/resolvers/transform-pipeline.yaml` |
| [feature-flags.yaml](resolvers/feature-flags.yaml) | Feature flag patterns | `scafctl run solution -f examples/resolvers/feature-flags.yaml` |
| [secrets.yaml](resolvers/secrets.yaml) | Secret management (requires secret store) | `scafctl run solution -f examples/resolvers/secrets.yaml` |
| [identity.yaml](resolvers/identity.yaml) | Authentication identity info (requires auth) | `scafctl run solution -f examples/resolvers/identity.yaml` |

---

## Solution Examples

Complete solutions demonstrating real-world use cases.

| Example | Description |
|---------|-------------|
| [comprehensive/](solutions/comprehensive/) | Full-featured solution with all capabilities |
| [terraform/](solutions/terraform/) | Terraform project scaffolding |
| [taskfile/](solutions/taskfile/) | Taskfile.yaml generation |
| [email-notifier/](solutions/email-notifier/) | Email notification workflow |

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

---

## Tips

### View Output as JSON
```bash
scafctl run solution -f examples/resolvers/hello-world.yaml -o json
```

### Dry Run Mode
```bash
scafctl run solution -f examples/actions/hello-world.yaml --dry-run
```

### Debug Logging
```bash
scafctl run solution -f examples/resolvers/dependencies.yaml -v
```

### Pass Multiple Parameters
```bash
scafctl run solution -f examples/resolvers/parameters.yaml \
  -r name=Bob \
  -r count=5 \
  -r uppercase=true
```
