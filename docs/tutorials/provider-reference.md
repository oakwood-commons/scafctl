# Provider Reference

This document provides a reference for all built-in providers in scafctl.

## Overview

Providers are execution primitives used by resolvers and actions. Each provider has **capabilities** that determine where it can be used:

| Capability | Used In | Description |
|------------|---------|-------------|
| `from` | Resolver `resolve.with` | Fetch or generate data |
| `transform` | Resolver `transform.with` | Transform data |
| `validation` | Resolver `validate.with` | Validate data |
| `action` | Action `provider` | Perform side effects |
| `authentication` | HTTP auth | Provide authentication |

## Capabilities Matrix

| Provider | from | transform | validation | action |
|----------|:----:|:---------:|:----------:|:------:|
| [cel](#cel) | ❌ | ✅ | ❌ | ✅ |
| [debug](#debug) | ✅ | ✅ | ✅ | ✅ |
| [env](#env) | ✅ | ❌ | ❌ | ❌ |
| [exec](#exec) | ✅ | ✅ | ❌ | ✅ |
| [file](#file) | ✅ | ✅ | ❌ | ✅ |
| [git](#git) | ✅ | ❌ | ❌ | ✅ |
| [go-template](#go-template) | ❌ | ✅ | ❌ | ✅ |
| [http](#http) | ✅ | ✅ | ❌ | ✅ |
| [identity](#identity) | ✅ | ❌ | ❌ | ❌ |
| [parameter](#parameter) | ✅ | ❌ | ❌ | ❌ |
| [secret](#secret) | ✅ | ❌ | ❌ | ❌ |
| [sleep](#sleep) | ✅ | ✅ | ✅ | ✅ |
| [static](#static) | ✅ | ✅ | ❌ | ❌ |
| [validation](#validation) | ❌ | ✅ | ✅ | ❌ |

---

## cel

Transform and evaluate data using CEL (Common Expression Language) expressions.

### Capabilities

`transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `expression` | string | ✅ | CEL expression to evaluate. Resolver data available under `_` |
| `variables` | any | ❌ | Additional variables for the CEL context |

### Output

Returns the evaluation result (any type).

### Examples

```yaml
# Transform: uppercase a string
transform:
  with:
    - provider: cel
      inputs:
        expression: "__self.toUpperCase()"

# Action: compute a value
provider: cel
inputs:
  expression: "_.items.map(i, i.price).sum()"
```

---

## debug

Debugging provider for inspecting resolver data during workflow execution.

### Capabilities

`from`, `transform`, `validation`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `expression` | string | ❌ | CEL expression to filter/transform data before output |
| `label` | string | ❌ | Label or message for debug output context |
| `format` | string | ❌ | Output format: `yaml`, `json`, `pretty` (default: `yaml`) |
| `destination` | string | ❌ | Where to output: `stdout`, `stderr`, `file` (default: `stdout`) |
| `path` | string | ❌ | File path when destination is `file` |
| `colorize` | bool | ❌ | Whether to colorize terminal output |

### Examples

```yaml
# Debug all resolver data
resolve:
  with:
    - provider: debug
      inputs:
        label: "Resolver Context"

# Debug specific value
transform:
  with:
    - provider: debug
      inputs:
        expression: "_.config"
        format: json
```

---

## env

Read environment variables.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `get`, `list` |
| `name` | string | ❌ | Variable name (required for `get`) |
| `default` | string | ❌ | Default value if variable not set |
| `prefix` | string | ❌ | Filter variables by prefix (for `list`) |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `value` | string | Variable value (for `get`) |
| `variables` | map | Key-value pairs (for `list`) |
| `found` | bool | Whether the variable exists |

### Examples

```yaml
# Get environment variable with default
resolve:
  with:
    - provider: env
      inputs:
        operation: get
        name: DATABASE_URL
        default: "postgres://localhost/dev"

# List all vars with prefix
resolve:
  with:
    - provider: env
      inputs:
        operation: list
        prefix: "APP_"
```

---

## exec

Execute shell commands.

### Capabilities

`from`, `transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `command` | string | ✅ | Command to execute |
| `args` | array | ❌ | Command arguments |
| `stdin` | string | ❌ | Standard input to provide |
| `dir` | string | ❌ | Working directory |
| `env` | object | ❌ | Environment variables |
| `timeout` | int | ❌ | Timeout in seconds (max 3600) |
| `shell` | bool | ❌ | Execute through shell for pipes/redirections |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `stdout` | string | Standard output |
| `stderr` | string | Standard error |
| `exitCode` | int | Exit code |
| `success` | bool | Whether command succeeded (action only) |

### Examples

```yaml
# Simple command
provider: exec
inputs:
  command: "echo"
  args: ["Hello", "World"]

# Shell pipeline
provider: exec
inputs:
  command: "cat /etc/hosts | grep localhost"
  shell: true

# With environment variables
provider: exec
inputs:
  command: "./deploy.sh"
  dir: "/opt/app"
  env:
    ENVIRONMENT: production
  timeout: 300
```

---

## file

Filesystem operations: read, write, check existence, delete.

### Capabilities

`from`, `transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `read`, `write`, `exists`, `delete` |
| `path` | string | ✅ | File path (absolute or relative) |
| `content` | string | ❌ | Content to write (required for `write`) |
| `createDirs` | bool | ❌ | Create parent directories if missing |
| `encoding` | string | ❌ | File encoding: `utf-8`, `binary` (default: `utf-8`) |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `content` | string | File content (for `read`) |
| `exists` | bool | Whether file exists |
| `size` | int | File size in bytes |
| `success` | bool | Operation success (action only) |

### Examples

```yaml
# Read file
resolve:
  with:
    - provider: file
      inputs:
        operation: read
        path: "./config.json"

# Write file
provider: file
inputs:
  operation: write
  path: "./output/result.txt"
  content:
    expr: "_.processedData"
  createDirs: true

# Check if file exists
resolve:
  with:
    - provider: file
      inputs:
        operation: exists
        path: "./optional-config.yaml"
```

---

## git

Git version control operations.

### Capabilities

`from`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `clone`, `pull`, `status`, `add`, `commit`, `push`, `checkout`, `branch`, `log`, `tag` |
| `url` | string | ❌ | Repository URL (for `clone`) |
| `path` | string | ❌ | Local repository path |
| `branch` | string | ❌ | Branch name |
| `message` | string | ❌ | Commit message |
| `files` | array | ❌ | Files to add |
| `tag` | string | ❌ | Tag name |
| `remote` | string | ❌ | Remote name (default: `origin`) |
| `depth` | int | ❌ | Clone depth for shallow clone |
| `username` | string | ❌ | Username for authentication |
| `password` | string | ❌ | Password/token (secret) |
| `force` | bool | ❌ | Force the operation |

### Examples

```yaml
# Clone repository
provider: git
inputs:
  operation: clone
  url: https://github.com/org/repo.git
  path: ./repo
  depth: 1

# Commit and push
provider: git
inputs:
  operation: commit
  path: ./repo
  message:
    expr: "'Release ' + _.version"

# Then push
provider: git
inputs:
  operation: push
  path: ./repo
```

---

## go-template

Transform data using Go text/template syntax.

### Capabilities

`transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `template` | string | ✅ | Go template content |
| `name` | string | ✅ | Template name (for error messages) |
| `missingKey` | string | ❌ | Behavior for missing keys: `default`, `zero`, `error` |
| `leftDelim` | string | ❌ | Left delimiter (default: `{{`) |
| `rightDelim` | string | ❌ | Right delimiter (default: `}}`) |
| `data` | any | ❌ | Additional data to merge with resolver context |

### Output

Returns the rendered template as a string.

### Examples

```yaml
# Render template
transform:
  with:
    - provider: go-template
      inputs:
        name: config
        template: |
          server:
            host: {{ .host }}
            port: {{ .port }}
            env: {{ .environment }}
```

---

## http

HTTP client for API calls.

### Capabilities

`from`, `transform`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `url` | string | ✅ | URL to request |
| `method` | string | ❌ | HTTP method (default: `GET`) |
| `headers` | object | ❌ | HTTP headers |
| `body` | string | ❌ | Request body |
| `timeout` | int | ❌ | Timeout in seconds (max 300) |
| `retry` | object | ❌ | Retry configuration |
| `auth` | string | ❌ | Auth provider (e.g., `entra`) |
| `scope` | string | ❌ | OAuth scope for authentication |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `statusCode` | int | HTTP status code |
| `body` | string | Response body |
| `headers` | object | Response headers |
| `success` | bool | Whether request succeeded (action only) |

### Examples

```yaml
# GET request
resolve:
  with:
    - provider: http
      inputs:
        url: https://api.example.com/config
        headers:
          Accept: application/json

# POST with body
provider: http
inputs:
  url: https://api.example.com/deploy
  method: POST
  headers:
    Content-Type: application/json
  body:
    expr: 'toJson({"image": _.image, "env": _.environment})'
  timeout: 60

# With retry
provider: http
inputs:
  url: https://api.example.com/status
  retry:
    maxAttempts: 3
    backoff: exponential
    initialDelay: 1s
```

---

## identity

Get authentication identity information without exposing tokens.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `status`, `claims`, `list` |
| `handler` | string | ❌ | Auth handler name (e.g., `entra`) |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `authenticated` | bool | Whether authenticated |
| `identityType` | string | Type of identity |
| `claims` | object | Token claims |
| `tenantId` | string | Tenant ID |
| `expiresAt` | string | Token expiration |
| `handlers` | array | Available handlers (for `list`) |

### Examples

```yaml
# Check if authenticated
resolve:
  with:
    - provider: identity
      inputs:
        operation: status
        handler: entra

# Get claims
resolve:
  with:
    - provider: identity
      inputs:
        operation: claims
        handler: entra
```

---

## parameter

Access CLI parameters passed via `-r` flags.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `key` | string | ✅ | Parameter name |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `value` | any | Parameter value |
| `found` | bool | Whether parameter was provided |
| `type` | string | Detected type of value |

### Examples

```yaml
# Get parameter with fallback
resolve:
  with:
    - provider: parameter
      inputs:
        key: environment
    - provider: static
      inputs:
        value: "dev"  # Default if not provided
```

Usage:

```bash
scafctl run solution -f sol.yaml -r environment=production
```

---

## secret

Retrieve encrypted secrets from the scafctl secrets store.

### Capabilities

`from`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `operation` | string | ✅ | Operation: `get` or `list` |
| `name` | string | ❌ | Secret name (for `get`) |
| `pattern` | string | ❌ | Regex pattern to match names |
| `required` | bool | ❌ | Error if not found |
| `default` | string | ❌ | Value when not found |

### Examples

```yaml
# Get secret
resolve:
  with:
    - provider: secret
      inputs:
        operation: get
        name: api-key
        required: true

# Get with default
resolve:
  with:
    - provider: secret
      inputs:
        operation: get
        name: optional-key
        default: "fallback-value"
```

Manage secrets via CLI:

```bash
scafctl secrets set api-key "my-secret-value"
scafctl secrets list
```

---

## sleep

Pause execution for a specified duration.

### Capabilities

`from`, `transform`, `validation`, `action`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `duration` | string | ✅ | Duration (Go format: `1s`, `500ms`, `2m`) |

### Examples

```yaml
# Wait between API calls
provider: sleep
inputs:
  duration: "2s"
```

---

## static

Return a constant value. Useful for defaults and fallbacks.

### Capabilities

`from`, `transform`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `value` | any | ✅ | Static value to return |

### Examples

```yaml
# Default fallback
resolve:
  with:
    - provider: env
      inputs:
        operation: get
        name: CONFIG_PATH
    - provider: static
      inputs:
        value: "/etc/app/config.yaml"

# Complex default
resolve:
  with:
    - provider: static
      inputs:
        value:
          timeout: 30
          retries: 3
          endpoints:
            - https://primary.example.com
            - https://backup.example.com
```

---

## validation

Validate data using regex patterns and CEL expressions.

### Capabilities

`transform`, `validation`

### Inputs

| Field | Type | Required | Description |
|-------|------|:--------:|-------------|
| `value` | string | ❌ | Value to validate (uses `__self` in transform context) |
| `match` | string | ❌ | Regex pattern that must match |
| `notMatch` | string | ❌ | Regex pattern that must NOT match |
| `expression` | string | ❌ | CEL expression that must be true |
| `message` | string | ❌ | Custom error message on failure |

### Output

| Field | Type | Description |
|-------|------|-------------|
| `valid` | bool | Whether validation passed |
| `errors` | array | Validation error messages |
| `details` | string | Failure details |

### Examples

```yaml
# Validate with regex
validate:
  with:
    - provider: validation
      inputs:
        match: "^[a-z][a-z0-9-]+$"
        message: "Name must be lowercase alphanumeric with dashes"

# Validate with CEL
validate:
  with:
    - provider: validation
      inputs:
        expression: "__self in ['dev', 'staging', 'prod']"
        message: "Environment must be dev, staging, or prod"

# Combined validation
validate:
  with:
    - provider: validation
      inputs:
        match: "^v[0-9]+\\.[0-9]+\\.[0-9]+$"
        expression: "!__self.startsWith('v0.')"
        message: "Version must be semver format and >= v1.0.0"
```
