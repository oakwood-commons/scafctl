# Action Schema

## Overview

**Actions** are explicit, opt-in side effects. They execute only when requested and orchestrate through dependency graphs.

Each action is independent but can express dependencies on other actions via `dependsOn`.

## Full Schema

```yaml
actions:
  actionName:
    description: What this action does

    provider: provider-name

    # Optional: Only run if condition is true
    when: _.condition == true

    # Optional: Run dependencies first
    dependsOn:
      - other-action
      - another-action

    # Optional: Iterate over array
    forEach:
      over: _.arrayResolver
      as: __itemName

    # Inputs passed to provider
    inputs:
      input1: value1
      input2: value2
      # Provider-specific fields

    # Optional: Extract outputs
    outputs:
      output1: .path.to.value
      output2: .another.path
```

## Core Fields

### `description`

**Type:** `string`
**Required:** Yes

Human-readable description of what the action does:

```yaml
description: Build the Go application binary
description: Deploy to production cluster
description: Send notification to Slack channel
```

### `provider`

**Type:** `string`
**Required:** Yes

Provider to execute. Common types:
- `shell` (sh) - Execute shell commands
- `api` - Make HTTP requests
- `filesystem` - File operations
- `git` - Git operations
- `go-template` - Render Go templates
- `cel` - Execute CEL expressions

```yaml
provider: shell
provider: api
provider: filesystem
```

## Conditional Execution

### `when:`

**Type:** CEL expression
**Required:** No

Skip action if condition is false:

```yaml
actions:
  deploy-prod:
    description: Deploy to production
    when: _.environment == "prod"
    provider: api
    inputs:
      endpoint: https://deploy.example.com
      method: POST
```

If `when:` is false, the action is **skipped entirely**.

Use cases:
- Skip build in certain environments
- Only deploy on approved branches
- Skip notifications if disabled

Context: Access `_` (all resolvers) in condition.

## Dependencies

### `dependsOn:`

**Type:** `array[string]`
**Required:** No

Actions that must run before this one:

```yaml
actions:
  lint:
    provider: shell
    inputs:
      cmd: [golangci-lint, run]

  test:
    description: Run tests
    dependsOn: [lint]
    provider: shell
    inputs:
      cmd: [go, test, ./...]

  build:
    description: Build application
    dependsOn: [test]
    provider: shell
    inputs:
      cmd: [go, build]
```

Execution order: `lint` → `test` → `build`

### DAG Execution

Independent actions run in parallel:

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
    dependsOn: [lint, format-check]  # Waits for both
    provider: shell
    inputs:
      cmd: [go, test]
```

Execution:
1. `lint` and `format-check` run **in parallel**
2. `test` runs when **both complete**

### Multiple Dependencies

Depend on multiple actions:

```yaml
deploy:
  dependsOn:
    - build
    - test
    - validate
  provider: api
```

Waits for all to complete before running.

## Iteration

### `forEach:`

**Type:** `object`
**Required:** No

Run action multiple times over a collection:

```yaml
forEach:
  over: _.environments      # Array resolver to iterate
  as: __env                 # Foreach alias must start with "__"
```

```yaml
actions:
  deploy:
    description: Deploy to each environment
    provider: api
    forEach:
      over: _.deployTargets
      as: __target
    inputs:
      endpoint: https://deploy.example.com/{{ __target.name }}
      method: POST
      body: '{"replicas": {{ __target.replicas }}}'
```

### Foreach Context

Inside foreach:
- `__item` - Default alias when `as:` is omitted
- Custom alias (e.g., `__region`) - Set via `as:` and must begin with "__"
- `_` - All resolvers (unchanged)

```yaml
forEach:
  over: _.regions
  as: __region

inputs:
  cmd:
    - "echo Deploying to {{ __region }}"
    - "deploy-to-{{ __region }}"
```

### Foreach with When

Combine `when:` with `forEach:`:

```yaml
actions:
  deploy:
    when: _.deployEnabled == true
    forEach:
      over: _.environments
      as: __env
    provider: shell
    inputs:
      cmd:
        - "deploy-to-{{ __env }}"
```

If `when:` is false, entire foreach is skipped.

## Inputs and Outputs

### `inputs:`

**Type:** `object`
**Required:** No (but most providers need it)

Data passed to the provider. Provider-specific fields.

#### Shell Provider

```yaml
inputs:
  cmd:
    - "command1"
    - "command2"
  env:
    - "VAR1=value1"
    - "VAR2=value2"
  cwd: ./working/directory
```

#### API Provider

```yaml
inputs:
  endpoint: https://api.example.com/endpoint
  method: POST
  headers:
    Authorization: Bearer token
    Content-Type: application/json
  body: |
    {
      "key": "{{ _.value }}"
    }
  query:
    param1: value1
    param2: value2
```

#### Filesystem Provider

```yaml
inputs:
  operation: write|read|delete|copy|mkdir
  path: ./config/app.yaml
  content: |
    configuration: {{ _.config }}
  source: ./original/file
  destination: ./copy/destination
```

#### Git Provider

```yaml
inputs:
  operation: clone|commit|push|pull|tag
  repository: https://github.com/org/repo.git
  destination: ./workspace
  branch: main
  message: Release {{ _.version }}
  author:
    name: "{{ _.authorName }}"
    email: "{{ _.authorEmail }}"
```

### `outputs:`

**Type:** `object`
**Required:** No

Extract values from provider response:

```yaml
outputs:
  config: .data.config
  status: .status
  url: .deployment.url
```

Uses JSONPath syntax to extract fields from provider output.

## Templating in Inputs

Use **Go templating** for text fields:

```yaml
inputs:
  cmd:
    - "echo Building {{ _.projectName }}"
    - "go build -o bin/{{ _.projectName }} ./cmd"

  path: ./config/{{ _.environment }}/app.yaml

  endpoint: https://{{ _.region }}.api.example.com/deploy

  message: "Deployed {{ _.serviceName }} v{{ _.version }}"
```

Use **CEL** for expressions:

```yaml
when: _.environment == "prod" && _.version != "0.0.0-dev"

dependsOn: [build, test]
```

## Complete Examples

### Example 1: Linear Pipeline

```yaml
actions:
  lint:
    description: Run linters
    provider: shell
    inputs:
      cmd: [golangci-lint, run, ./...]

  test:
    description: Run tests
    dependsOn: [lint]
    provider: shell
    inputs:
      cmd: [go, test, -v, ./...]

  build:
    description: Build binary
    dependsOn: [test]
    provider: shell
    inputs:
      cmd: [go, build, -o, bin/app, ./cmd]
```

### Example 2: Conditional Deployment

```yaml
actions:
  build:
    description: Build application
    provider: shell
    inputs:
      cmd: [go, build]

  deploy-dev:
    description: Deploy to development
    dependsOn: [build]
    when: _.environment == "dev"
    provider: api
    inputs:
      endpoint: https://deploy-dev.example.com/trigger
      method: POST

  deploy-prod:
    description: Deploy to production
    dependsOn: [build]
    when: _.environment == "prod"
    provider: api
    inputs:
      endpoint: https://deploy-prod.example.com/trigger
      method: POST
```

### Example 3: Multi-Region Deployment

```yaml
actions:
  build:
    description: Build and push image
    provider: shell
    inputs:
      cmd:
        - docker build -t myapp:{{ _.version }} .
        - docker push registry.example.com/myapp:{{ _.version }}

  deploy:
    description: Deploy to each region
    dependsOn: [build]
    forEach:
      over: _.regions
      as: __region
    provider: api
    inputs:
      endpoint: https://deploy.{{ __region }}.example.com/trigger
      method: POST
      body: |
        {
          "image": "myapp:{{ _.version }}",
          "replicas": {{ __region.replicas }}
        }
```

### Example 4: Notification

```yaml
actions:
  deploy:
    description: Deploy application
    provider: api
    inputs:
      endpoint: https://deploy.example.com/trigger
      method: POST

  notify-on-success:
    description: Notify team of success
    dependsOn: [deploy]
    when: _.notifyTeam == true
    provider: api
    inputs:
      endpoint: https://hooks.slack.com/services/WEBHOOK
      method: POST
      body: |
        {
          "text": "✅ Deployment successful",
          "blocks": [
            {
              "type": "section",
              "text": {
                "type": "mrkdwn",
                "text": "*{{ _.serviceName }}* v{{ _.version }} deployed to {{ _.environment }}"
              }
            }
          ]
        }
```

### Example 5: Generate Config

```yaml
actions:
  generate-dockerfile:
    description: Generate Dockerfile from template
    provider: go-template
    inputs:
      source: |
        FROM golang:{{ .GoVersion }}
        WORKDIR /app
        RUN apt-get update && apt-get install -y {{ .Dependencies }}
        COPY . .
        RUN go build -o bin/{{ .AppName }} ./cmd
      context:
        GoVersion: "{{ _.goVersion }}"
        AppName: "{{ _.projectName }}"
        Dependencies: "{{ _.systemDeps }}"

  write-dockerfile:
    description: Write Dockerfile to disk
    dependsOn: [generate-dockerfile]
    provider: filesystem
    inputs:
      operation: write
      path: ./Dockerfile
      content: "{{ _.generateDockerfile }}"

  build-image:
    description: Build Docker image
    dependsOn: [write-dockerfile]
    provider: shell
    inputs:
      cmd: [docker, build, -t, "{{ _.imageName }}", .]
```

## Field Reference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | Yes | What the action does |
| `provider` | string | Yes | Provider type (shell, api, etc.) |
| `when` | CEL expr | No | Skip if false |
| `dependsOn` | array | No | Actions that must run first |
| `forEach` | object | No | Iteration configuration |
| `forEach.over` | CEL expr | Yes (if forEach) | Array to iterate |
| `forEach.as` | string | Yes (if forEach) | Item variable name |
| `inputs` | object | No | Provider-specific inputs |
| `outputs` | object | No | Extract outputs from response |

## Best Practices

1. **Name actions clearly** - Shows what they do
2. **Use descriptions** - Document purpose and side effects
3. **Express dependencies explicitly** - Use `dependsOn` not ordering
4. **Leverage parallelism** - Independent actions run in parallel
5. **Use `when:` for conditions** - Makes intent clear
6. **Keep providers focused** - One thing per action
7. **Validate inputs** - Ensure data is correct
8. **Handle errors** - Actions stop on failure (or use conditional recovery)
9. **Document complex logic** - Especially in provider inputs
10. **Test edge cases** - Empty values, missing fields, timeouts

## Error Handling

Actions stop at first failure:

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd: [go, build]

  test:
    dependsOn: [build]
    # Never runs if build fails
    provider: shell
    inputs:
      cmd: [go, test]
```

Use conditional actions for recovery:

```yaml
actions:
  primary-action:
    provider: api
    inputs:
      endpoint: https://primary.example.com

  fallback-action:
    when: primary-action.status == "failed"
    provider: api
    inputs:
      endpoint: https://fallback.example.com
```

## Next Steps

- **Action orchestration** → [Action Orchestration Guide](../guides/04-action-orchestration.md)
- **Provider types** → [Providers Guide](../guides/06-providers.md)
- **Full reference** → [Provider Reference](../reference/providers.md)

---

Actions are where scafctl creates real-world impact. Use them wisely to build reliable workflows!
