# Action Orchestration Guide

## Overview

**Actions** are explicit, opt-in side effects. They execute only when you request them, and they orchestrate through dependency graphs.

Core principles:
- Nothing executes unless explicitly invoked
- Dependencies form a DAG (Directed Acyclic Graph)
- Execution order determined by `dependsOn` declarations
- Optional `when:` conditions make actions conditional
- Optional `foreach:` enables iteration

## Basic Action Structure

```yaml
actions:
  build:
    description: Build the application
    provider: shell
    inputs:
      cmd:
        - "echo Building {{ _.projectName }}"
        as: __env                 # Foreach alias must start with "__"
```
        endpoint: https://deploy.example.com/{{ __env }}
## Providers

Actions use **providers** to execute operations. Common providers:

| Provider | Purpose | Example |
|----------|---------|---------|
| `shell` (sh) | Execute shell commands | `go build`, `docker build` |
| `api` | Make HTTP requests | Webhooks, REST APIs |
  - `__item` - Default name when no alias is provided
  - Custom alias (e.g., `__env`) - Available when `as:` sets a name starting with `__`
| `git` | Git operations | Clone, commit, push |
| `go-template` | Render Go templates | Generate files |
| `cel` | Execute CEL expressions | Compute values |

## Inputs and Outputs

Actions receive inputs and may produce outputs:

```yaml
actions:
  fetch-data:
    description: Fetch data from API
    provider: api
    inputs:
      endpoint: https://api.example.com/config
      method: GET
      headers:
        Authorization: Bearer {{ _.apiToken }}
    outputs:
      config: .data.config  # Extract from response
```

The provider processes inputs, executes the operation, and returns outputs.

## Conditional Execution: `when:`

Make actions conditional with CEL expressions:

```yaml
actions:
  deploy:
    description: Deploy to production
    provider: api
    when: _.environment == "prod"
    inputs:
      endpoint: https://deploy.example.com/trigger
      method: POST
      body: '{"service": "{{ _.serviceName }}"}'
```

If `when:` evaluates to false, the action is **skipped**.

Use cases:
- Skip build in dev environment
- Only deploy on stable branches
- Only create resources if enabled

## Action Dependencies: `dependsOn:`

Express dependencies between actions to form execution order:

```yaml
actions:
  lint:
    description: Run linters
    provider: shell
    inputs:
      cmd: [golangci-lint, run]

  test:
    description: Run tests
    provider: shell
    dependsOn: [lint]  # Must run after lint
    inputs:
      cmd: [go, test, ./...]

  build:
    description: Build binary
    provider: shell
    dependsOn: [test]  # Must run after test
    inputs:
      cmd: [go, build, -o, bin/app]

  containerize:
    description: Build container image
    provider: shell
    dependsOn: [build]  # Must run after build
    inputs:
      cmd: [docker, build, -t, myapp:latest, .]
```

Execution order: `lint` → `test` → `build` → `containerize`

### DAG Execution

Multiple actions can run in parallel if they don't depend on each other:

```yaml
actions:
  lint:
    provider: shell
    inputs:
      cmd: [golangci-lint, run]

  format-check:
    provider: shell
    inputs:
      cmd: [go, fmt, ./...]

  test:
    provider: shell
    dependsOn: [lint, format-check]  # Wait for both
    inputs:
      cmd: [go, test, ./...]
```

Execution:
1. `lint` and `format-check` run **in parallel**
2. When **both complete**, `test` runs

This is more efficient than sequential execution.

### Conditional Dependencies

Use `when:` to conditionally depend on actions:

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd: [go, build]

  test:
    provider: shell
    when: _.runTests == true
    inputs:
      cmd: [go, test]

  deploy:
    provider: api
    when: _.runTests == false  # Only if tests NOT running
    dependsOn: [build]  # Only depends on build
    inputs:
      endpoint: https://deploy.example.com/trigger
      method: POST
```

## Iteration: `foreach:`

Run an action multiple times over a collection:

```yaml
actions:
  deploy-service:
    description: Deploy to each environment
    provider: api
    foreach:
      over: _.environments      # Array resolver
      as: __env                 # Foreach alias must start with "__"
    inputs:
      endpoint: https://deploy.example.com/{{ __env }}
      method: POST
      body: '{"service": "{{ _.serviceName }}"}'
```

With `environments: ["dev", "staging", "prod"]`, this deploys 3 times.

### Foreach Context

In foreach iterations:
- `__item` - Default name when `as:` is omitted
- Custom alias (e.g., `__env`) - Set via `as:` and must start with "__"
- `_` - All resolvers (unchanged)

```yaml
actions:
  notify:
    provider: api
    foreach:
      over: _.recipients
      as: __recipient
    inputs:
      endpoint: https://notify.example.com/send
      method: POST
      body: |
        {
          "to": "{{ __recipient.email }}",
          "message": "Deployed {{ _.version }} to {{ __recipient.region }}"
        }
```

### Foreach with Conditional

Combine `when:` with `foreach:`:

```yaml
actions:
  deploy-all:
    provider: api
    when: _.deployEnabled == true
    foreach:
      over: _.environments
      as: __env
    inputs:
      endpoint: https://deploy.example.com/{{ __env }}
      method: POST
```

If `when:` is false, the entire foreach is skipped.

### Conditional Items in Foreach

Use conditional in the input to skip some items:

```yaml
actions:
  deploy:
    provider: shell
    foreach:
      over: _.regions
      as: __region
    inputs:
      cmd:
        # Only deploy non-dev regions in production
        - |
          if [[ "{{ _.environment }}" == "prod" && "{{ __region }}" == "dev" ]]; then
            echo "Skipping dev region in prod"
          else
            deploy-to-{{ __region }}
          fi
```

## Combined: Dependencies + Foreach + When

```yaml
actions:
  test:
    description: Run tests
    provider: shell
    inputs:
      cmd: [go, test, ./...]

  build-image:
    description: Build container image
    provider: shell
    dependsOn: [test]
    inputs:
      cmd: [docker, build, -t, myapp:latest, .]

  push-registry:
    description: Push to container registry
    provider: api
    dependsOn: [build-image]
    when: _.pushImages == true
    foreach:
      over: _.registries
      as: __registry
    inputs:
      endpoint: https://{{ __registry }}/push
      method: POST
      body: '{"image": "myapp:latest"}'

  notify-team:
    description: Notify team of deployment
    provider: api
    dependsOn: [push-registry]
    when: _.environment == "prod"
    inputs:
      endpoint: https://slack.example.com/notify
      method: POST
      body: '{"message": "Deployed {{ _.version }}"}'
```

Flow:
1. `test` runs
2. On success, `build-image` runs
3. On success, if `pushImages == true`, `push-registry` runs **foreach registry**
4. On completion, if `environment == "prod"`, `notify-team` runs

## Common Patterns

### Pattern 1: Linear Pipeline

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd: [go, build]

  test:
    provider: shell
    dependsOn: [build]
    inputs:
      cmd: [go, test]

  package:
    provider: shell
    dependsOn: [test]
    inputs:
      cmd: [tar, -czf, app.tar.gz, bin/]
```

### Pattern 2: Fan-Out / Fan-In

```yaml
actions:
  unit-test:
    provider: shell
    inputs:
      cmd: [go, test, ./...]

  integration-test:
    provider: shell
    inputs:
      cmd: [go, test, -tags=integration, ./...]

  lint:
    provider: shell
    inputs:
      cmd: [golangci-lint, run]

  build:
    provider: shell
    dependsOn: [unit-test, integration-test, lint]  # Wait for all
    inputs:
      cmd: [go, build]
```

Execution:
1. `unit-test`, `integration-test`, `lint` run **in parallel**
2. When **all complete**, `build` runs

### Pattern 3: Multi-Environment Deployment

```yaml
actions:
  test:
    provider: shell
    inputs:
      cmd: [go, test, ./...]

  build:
    provider: shell
    dependsOn: [test]
    inputs:
      cmd: [docker, build, -t, myapp:latest, .]

  deploy:
    description: Deploy to each environment
    provider: api
    dependsOn: [build]
    foreach:
      over: _.deploymentTargets
      as: __target
    inputs:
      endpoint: https://deploy.example.com/environments/{{ __target.name }}
      method: POST
      body: |
        {
          "image": "myapp:latest",
          "replicas": {{ __target.replicas }},
          "resources": {{ __target.resources | toJson }}
        }
```

### Pattern 4: Conditional Workflows

```yaml
actions:
  validate:
    provider: shell
    inputs:
      cmd: [go, vet, ./...]

  build-dev:
    provider: shell
    dependsOn: [validate]
    when: _.environment == "dev"
    inputs:
      cmd: [go, build, -gcflags, all=-N]  # Debug build

  build-prod:
    provider: shell
    dependsOn: [validate]
    when: _.environment == "prod"
    inputs:
      cmd: [go, build, -ldflags, -s]  # Optimized build

  push:
    description: Push built image
    provider: api
    dependsOn: [build-dev, build-prod]
    when: _.pushEnabled == true
    inputs:
      endpoint: https://registry.example.com/push
      method: POST
      body: '{"image": "{{ _.imageName }}"}'
```

Execution:
- `validate` runs
- If dev: `build-dev` runs
- If prod: `build-prod` runs
- Only one build runs (mutually exclusive)
- If `pushEnabled == true`, `push` runs after whichever build completed

### Pattern 5: Notification on Completion

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd: [go, build]

  deploy:
    provider: api
    dependsOn: [build]
    inputs:
      endpoint: https://deploy.example.com/trigger
      method: POST

  notify-success:
    provider: api
    dependsOn: [deploy]
    when: _.environment == "prod"
    inputs:
      endpoint: https://slack.example.com/webhook
      method: POST
      body: '{"text": "✅ Production deployment successful"}'

  notify-failure:
    provider: api
    when: deployment.status == "failed"
    inputs:
      endpoint: https://slack.example.com/webhook
      method: POST
      body: '{"text": "❌ Deployment failed"}'
```

## Advanced Concepts

### Outputs from Previous Actions

Actions can use outputs from previous actions (if providers support it):

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd: [go, build]
    outputs:
      binary_path: ./bin/app

  upload:
    provider: api
    dependsOn: [build]
    inputs:
      endpoint: https://storage.example.com/upload
      method: POST
      file: "{{ _.build.binary_path }}"  # Reference previous action output
```

### Error Handling

Actions stop at first failure:

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd: [go, build]

  test:
    provider: shell
    dependsOn: [build]
    inputs:
      cmd: [go, test]  # If build fails, test never runs
```

Use conditional actions to handle errors gracefully:

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd: [go, build]

  cleanup-on-failure:
    provider: shell
    when: build.status == "failed"
    inputs:
      cmd: [rm, -rf, bin/]
```

## Best Practices

1. **Express dependencies explicitly** - Use `dependsOn` rather than action order
2. **Use `when:` for conditionals** - Makes intent clear
3. **Leverage parallelism** - Independent actions run in parallel
4. **Fail fast** - Actions stop on first error
5. **Name actions clearly** - `deploy-to-production` better than `deploy2`
6. **Document side effects** - Use `description:` to explain what action does
7. **Use foreach for collections** - Cleaner than hardcoding multiple actions
8. **Make actions idempotent** - Safe to re-run without duplicate side effects
9. **Avoid deep dependencies** - Too many levels make workflows hard to understand
10. **Test conditions** - Verify `when:` conditions work as expected

## Troubleshooting

### Issue: Action not executing

Check:
1. Is `when:` condition false?
2. Are dependencies failing?
3. Is the action explicitly selected?

```bash
# See what would execute (no side effects)
scafctl run solution:myapp --dry-run

# Force re-evaluation without caches
scafctl run solution:myapp --dry-run --no-cache

# Run specific action
scafctl run solution:myapp --action my-action
```

### Issue: Wrong execution order

Check `dependsOn:` declarations:

```yaml
# WRONG: No dependency, might run in parallel
actions:
  step1:
    provider: shell
    inputs:
      cmd: [step1]

  step2:
    provider: shell
    inputs:
      cmd: [step2]

# CORRECT: Explicit dependency
  step2:
    provider: shell
    dependsOn: [step1]  # Must run after step1
    inputs:
      cmd: [step2]
```

### Issue: Foreach not iterating

Check:
1. Is `over:` value an array?
2. Is resolver being resolved?

```yaml
resolvers:
  environments:
    resolve:
      from:
        - provider: cli
          key: envs
        - provider: static
          value: ["dev", "prod"]

actions:
  deploy:
    foreach:
      over: _.environments  # Must be array
      as: __env
    inputs:
      cmd:
        - deploy-to-{{ __env }}
```

## Examples in Repository

See `examples/go-taskfile/solution.yaml` for:
- Linear action pipelines
- DAG execution with parallelism
- Conditional actions
- Dependencies

See `examples/scafctl-build/solution.yaml` for:
- Replacing a Taskfile with scafctl actions
- Building Go binaries via `scafctl run --action build`
- Injecting repo paths, ldflags, and toolchain overrides through resolvers

## Next Steps

- **Master expressions** → [Expression Language](./05-expression-language.md)
- **Learn about providers** → [Providers Guide](./06-providers.md)
- **Reference schemas** → [Action Schema](../schemas/action-schema.md)

---

Actions are where scafctl creates real-world impact. Master orchestration to build powerful, reliable workflows!
