# Providers Guide

## Overview

**Providers** are stateless execution primitives. Each provider does one thing well:
- Input → Operation → Output/Error

Providers never own orchestration, validation, or transformation. They're pure computation engines.

## Provider Types

### 1. Shell (`sh`)

Execute shell commands.

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd:
        - "echo Building {{ _.projectName }}"
        - "go build -o bin/{{ _.projectName }} ./cmd"
```

**Input:**
- `cmd` - Command or array of commands

**Output:**
- stdout, stderr, exit code

**Use cases:**
- Running build commands
- Executing CI/CD pipelines
- File operations
- System scripts

### 2. API

Make HTTP requests.

```yaml
actions:
  deploy:
    provider: api
    inputs:
      endpoint: https://api.example.com/deploy
      method: POST
      headers:
        Authorization: Bearer {{ _.token }}
        Content-Type: application/json
      body: |
        {
          "service": "{{ _.serviceName }}",
          "version": "{{ _.version }}"
        }
```

**Input:**
- `endpoint` - URL
- `method` - HTTP method (GET, POST, PUT, DELETE, etc.)
- `headers` - HTTP headers (optional)
- `body` - Request body (optional)
- `query` - Query parameters (optional)

**Output:**
- Response status, headers, body

**Use cases:**
- Webhooks
- REST API calls
- Notifications
- External service triggers

### 3. Filesystem

File operations.

```yaml
actions:
  create-config:
    provider: filesystem
    inputs:
      operation: write
      path: ./config/{{ _.environment }}/app.yaml
      content: |
        environment: {{ _.environment }}
        version: {{ _.version }}
        debug: {{ _.debug }}
```

**Input:**
- `operation` - `read`, `write`, `delete`, `copy`, `mkdir`
- `path` - File or directory path
- `content` - Content to write (for write operation)
- `source` - Source path (for copy operation)
- `destination` - Destination path (for copy operation)

**Output:**
- File contents (for read)
- Status (for write/delete/copy)

**Use cases:**
- Generate configuration files
- Create directories
- Copy templates
- Delete temporary files

### 4. Git

Git operations.

```yaml
actions:
  clone-repo:
    provider: git
    inputs:
      operation: clone
      repository: {{ _.repoUrl }}
      destination: ./src
      branch: {{ _.branchName }}

  commit-changes:
    provider: git
    inputs:
      operation: commit
      path: ./src
      message: "Release {{ _.version }}"
      author:
        name: {{ _.authorName }}
        email: {{ _.authorEmail }}
```

**Input:**
- `operation` - `clone`, `commit`, `push`, `pull`, `tag`, `branch`
- `repository` - Git URL
- `destination` - Where to clone
- `branch` - Branch name
- `message` - Commit message
- `author` - Author info (name, email)

**Output:**
- Clone status
- Commit hash
- Push status

**Use cases:**
- Clone repositories
- Create releases
- Commit changes
- Push tags
- Manage branches

### 5. Go Template

Render Go templates.

```yaml
actions:
  generate-dockerfile:
    provider: go-template
    inputs:
      source: |
        FROM golang:{{ .GoVersion }}
        WORKDIR /app
        COPY . .
        RUN go build -o bin/{{ .AppName }} ./cmd
      context:
        GoVersion: "{{ _.goVersion }}"
        AppName: "{{ _.projectName }}"
        Registry: "{{ _.registry }}"
```

**Input:**
- `source` - Template string or file
- `context` - Variables for template

**Output:**
- Rendered template

**Use cases:**
- Generate configuration files
- Create Dockerfiles
- Build shell scripts
- Template any text format

### 6. CEL Expression

Evaluate CEL expressions.

```yaml
actions:
  validate-config:
    provider: cel
    inputs:
      expression: |
        _.version.matches("^[0-9]+\\.[0-9]+\\.[0-9]+$") &&
        _.environment in ["dev", "staging", "prod"] &&
        size(_.regions) > 0
```

**Input:**
- `expression` - CEL expression

**Output:**
- Expression result

**Use cases:**
- Validate data
- Compute values
- Check preconditions
- Complex logic

## Provider Configuration

Providers can be defined once and reused:

```yaml
spec:
  providers:
    - name: github-api
      type: api
      config:
        baseUrl: https://api.github.com
        defaultHeaders:
          Authorization: Bearer {{ _.githubToken }}
          Accept: application/vnd.github.v3+json

  actions:
    create-release:
      provider: github-api
      inputs:
        endpoint: /repos/{{ _.org }}/{{ _.repo }}/releases
        method: POST
        body: |
          {
            "tag_name": "v{{ _.version }}",
            "name": "Release {{ _.version }}"
          }
```

## Common Patterns

### Pattern 1: Build Pipeline

```yaml
actions:
  lint:
    provider: shell
    inputs:
      cmd: [golangci-lint, run, ./...]

  test:
    provider: shell
    dependsOn: [lint]
    inputs:
      cmd: [go, test, -v, ./...]

  build:
    provider: shell
    dependsOn: [test]
    inputs:
      cmd: [go, build, -o, bin/app, ./cmd]

  package:
    provider: shell
    dependsOn: [build]
    inputs:
      cmd: [tar, -czf, app.tar.gz, bin/]
```

### Pattern 2: Configuration Generation

```yaml
actions:
  generate-dockerfile:
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

  generate-config:
    provider: go-template
    inputs:
      source: |
        database:
          host: {{ .DbHost }}
          port: {{ .DbPort }}
          name: {{ .DbName }}
        server:
          port: {{ .ServerPort }}
          environment: {{ .Env }}
      context:
        DbHost: "{{ _.dbHost }}"
        DbPort: "{{ _.dbPort }}"
        DbName: "{{ _.projectName }}"
        ServerPort: "{{ _.serverPort }}"
        Env: "{{ _.environment }}"
```

### Pattern 3: Multi-Region Deployment

```yaml
actions:
  deploy:
    provider: api
    forEach:
      over: _.regions
      as: region
    inputs:
      endpoint: https://{{ __item.api }}/deploy
      method: POST
      headers:
        Authorization: Bearer {{ _.apiToken }}
      body: |
        {
          "service": "{{ _.serviceName }}",
          "version": "{{ _.version }}",
          "replicas": {{ __item.replicas }}
        }

  verify:
    provider: shell
    dependsOn: [deploy]
    forEach:
      over: _.regions
      as: region
    inputs:
      cmd:
        - "curl -f https://{{ __item.hostname }}/health"
        - "echo 'Deployment verified in {{ __item.name }}'"
```

### Pattern 4: Notification

```yaml
actions:
  notify-slack:
    provider: api
    when: _.environment == "prod"
    inputs:
      endpoint: https://hooks.slack.com/services/YOUR/WEBHOOK
      method: POST
      body: |
        {
          "text": "Deployment",
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

  notify-email:
    provider: api
    when: _.sendEmail == true
    inputs:
      endpoint: https://email.example.com/send
      method: POST
      body: |
        {
          "to": "{{ _.recipients }}",
          "subject": "Deployment: {{ _.serviceName }} v{{ _.version }}",
          "body": "Service deployed successfully"
        }
```

### Pattern 5: Git Workflow

```yaml
actions:
  clone:
    provider: git
    inputs:
      operation: clone
      repository: {{ _.repoUrl }}
      destination: ./workspace
      branch: {{ _.branchName }}

  create-release:
    provider: git
    dependsOn: [clone]
    inputs:
      operation: tag
      path: ./workspace
      tag: v{{ _.version }}
      message: Release {{ _.version }}

  push:
    provider: shell
    dependsOn: [create-release]
    inputs:
      cmd:
        - cd ./workspace
        - git push origin v{{ _.version }}
```

## Error Handling

Providers return errors that stop execution:

```yaml
actions:
  fetch-data:
    provider: api
    inputs:
      endpoint: https://api.example.com/data
      # If API returns error, execution stops

  deploy:
    dependsOn: [fetch-data]
    # This never runs if fetch-data fails
    provider: shell
    inputs:
      cmd: [deploy]
```

Use conditions to handle errors gracefully:

```yaml
actions:
  try-fetch:
    provider: api
    inputs:
      endpoint: https://primary-api.example.com/data

  use-fallback:
    provider: api
    when: try-fetch.status == "failed"  # Conditional fallback
    inputs:
      endpoint: https://fallback-api.example.com/data
```

## Combining Providers

Build complex workflows by combining providers:

```yaml
actions:
  # Step 1: Generate config from template
  generate-config:
    provider: go-template
    inputs:
      source: |
        environment: {{ .Env }}
        version: {{ .Version }}
      context:
        Env: "{{ _.environment }}"
        Version: "{{ _.version }}"

  # Step 2: Write config to file
  write-config:
    dependsOn: [generate-config]
    provider: filesystem
    inputs:
      operation: write
      path: ./config/app.yaml
      content: "{{ _.generateConfig }}"  # Use previous output

  # Step 3: Build with config
  build:
    dependsOn: [write-config]
    provider: shell
    inputs:
      cmd:
        - go build -config ./config/app.yaml

  # Step 4: Create release
  release:
    dependsOn: [build]
    provider: git
    inputs:
      operation: tag
      tag: v{{ _.version }}
      message: Release {{ _.version }}

  # Step 5: Notify
  notify:
    dependsOn: [release]
    provider: api
    inputs:
      endpoint: https://hooks.slack.com/services/WEBHOOK
      method: POST
      body: |
        {"text": "Released v{{ _.version }}"}
```

## Best Practices

1. **Keep providers focused** - One concern per provider
2. **Use shell for simple tasks** - It's powerful and flexible
3. **Use API for external services** - Cleaner than shell curl
4. **Use filesystem for file generation** - More reliable than shell
5. **Use git for git operations** - Abstracts git complexity
6. **Configure providers once** - Reuse across actions
7. **Handle errors explicitly** - Don't rely on defaults
8. **Name actions clearly** - Shows what provider does
9. **Verify outputs** - Check that providers succeed
10. **Test edge cases** - Empty responses, timeouts, failures

## Troubleshooting

### Issue: Provider fails silently

Check:
1. Are there error messages?
2. Is the provider configured correctly?
3. Are inputs valid?

```bash
# See what would happen without executing
scafctl run solution:myapp --dry-run

# Force the run to ignore caches
scafctl run solution:myapp --dry-run --no-cache
```

### Issue: Command not found in shell provider

Check:
1. Is tool installed?
2. Is it in PATH?
3. Is shell environment correct?

```yaml
# Explicitly specify path or use full binary
cmd: [/usr/local/bin/golangci-lint, run]
```

### Issue: API call fails

Check:
1. Is endpoint correct?
2. Are headers valid?
3. Is authentication configured?

Use templating to debug:

```yaml
# Print actual request
cmd:
  - "echo Calling: https://{{ _.region }}.api.example.com/deploy"
  - "curl -X POST https://{{ _.region }}.api.example.com/deploy"
```

## Next Steps

- **Full provider reference** → [Provider Reference](../reference/providers.md)
- **Expression language** → [Expression Language](./05-expression-language.md)
- **Provider schema** → [Provider Schema](../schemas/provider-schema.md)

---

Providers are the execution core of scafctl. Use them to orchestrate any external system!
