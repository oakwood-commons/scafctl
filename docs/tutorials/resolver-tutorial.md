---
title: "Resolver Tutorial"
weight: 20
---

# Resolver Tutorial

This tutorial walks you through using scafctl resolvers to dynamically resolve configuration values. You'll learn how to define resolvers, use different providers, handle dependencies, and implement common patterns.

## Prerequisites

- scafctl installed and available in your PATH
- Basic familiarity with YAML syntax
- Understanding of environment variables

## Table of Contents

1. [Your First Resolver](#your-first-resolver)
2. [Using Parameters](#using-parameters)
3. [Resolver Dependencies](#resolver-dependencies)
4. [Transformations](#transformations)
5. [Validation](#validation)
6. [Conditional Execution](#conditional-execution)
7. [Error Handling](#error-handling)
8. [Working with HTTP APIs](#working-with-http-apis)
9. [Common Patterns](#common-patterns)

---

## Your First Resolver

Let's create a simple solution with one resolver that returns a static value.

### Step 1: Create the Solution File

Create a file called `hello.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: hello-world
  version: 1.0.0

spec:
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Hello, World!"
```

### Step 2: Run the Solution

{{< tabs "resolver-tutorial-cmd-1" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f hello.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f hello.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:
```
╭─ scafctl run resolver ─╮
│KEY       VALUE         │
│─────────────────────── │
│greeting  Hello, World! │
╰ _ ─────────── map: 1/1 ╯
```

> [!NOTE]
> **Tip**: Add `-o json` to get JSON output: `scafctl run resolver -f hello.yaml -o json`

### Understanding the Structure

- **apiVersion/kind**: Identifies this as a scafctl Solution
- **metadata**: Solution name, version, and description
- **spec.resolvers**: Map of resolver definitions
- **greeting**: The resolver name (used as the output key)
- **type**: Expected output type (optional, defaults to `any`)
- **resolve.with**: List of provider sources to try

---

## Using Parameters

Parameters let you pass values from the command line to your resolvers.

### Step 1: Create a Parameterized Solution

Create `greet.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: parameterized-greeting
  version: 1.0.0

spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              key: user_name
          - provider: static
            inputs:
              value: "World"
    
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'Hello, ' + _.name + '!'"
```

### Step 2: Run with Parameters

{{< tabs "resolver-tutorial-cmd-2" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greet.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greet.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
╭─ scafctl run resolver ─╮
│KEY       VALUE         │
│─────────────────────── │
│greeting  Hello, World! │
│name      World         │
╰ _ ─────────── map: 1/2 ╯
```

Pass a parameter:

{{< tabs "resolver-tutorial-cmd-3" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greet.yaml -r user_name=Alice
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f greet.yaml -r user_name=Alice
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
╭─ scafctl run resolver ─╮
│KEY       VALUE         │
│─────────────────────── │
│greeting  Hello, Alice! │
│name      Alice         │
╰ _ ─────────── map: 1/2 ╯
```

### Using Parameter Files

For complex parameter sets, use a parameter file:

Create `params.yaml`:
```yaml
user_name: Charlie
environment: production
region: us-west-2
```

Run with the file:
{{< tabs "resolver-param-file" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f greet.yaml -r @params.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
# Wrap @file in single quotes to avoid splatting operator
scafctl run resolver -f greet.yaml -r '@params.yaml'
```
{{% /tab %}}
{{< /tabs >}}

You can also pipe parameters from stdin using `@-`:
{{< tabs "resolver-param-stdin" >}}
{{% tab "Bash" %}}
```bash
echo '{"user_name": "Charlie"}' | scafctl run resolver -f greet.yaml -r @-
cat params.yaml | scafctl run resolver -f greet.yaml -r @-
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
'{"user_name": "Charlie"}' | scafctl run resolver -f greet.yaml -r '@-'
Get-Content params.yaml | scafctl run resolver -f greet.yaml -r '@-'
```
{{% /tab %}}
{{< /tabs >}}

To pipe raw text into a single parameter key, use `key=@-`:
{{< tabs "resolver-param-raw-stdin" >}}
{{% tab "Bash" %}}
```bash
# Raw stdin into a single key (not parsed as YAML/JSON)
echo Charlie | scafctl run resolver -f greet.yaml -r user_name=@-

# Read a file's content into a key
scafctl run resolver -f greet.yaml -r user_name=@name.txt
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
'Charlie' | scafctl run resolver -f greet.yaml -r 'user_name=@-'
scafctl run resolver -f greet.yaml -r 'user_name=@name.txt'
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
╭─ scafctl run resolver ──╮
│KEY       VALUE          │
│─────────────────────────│
│greeting  Hello, Charlie!│
│name      Charlie        │
╰ _ ──────────── map: 1/2 ╯
```

---

## Resolver Dependencies

Resolvers can reference other resolvers using `_.resolver_name` syntax in CEL expressions.

### Step 1: Create a Solution with Dependencies

Create `config.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: config-builder
  version: 1.0.0

spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              key: env
          - provider: static
            inputs:
              value: development
    
    port:
      type: int
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              key: port
          - provider: static
            inputs:
              value: 8080
    
    base_url:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                _.environment == 'production' 
                  ? 'https://api.example.com' 
                  : 'http://localhost:' + string(_.port)
    
    config:
      type: any
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                {
                  'environment': _.environment,
                  'port': _.port,
                  'baseUrl': _.base_url,
                  'debug': _.environment != 'production'
                }
```

### Step 2: Run and Observe Phases

{{< tabs "resolver-tutorial-cmd-4" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f config.yaml --progress
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f config.yaml --progress
```
{{% /tab %}}
{{< /tabs >}}

The `--progress` flag shows how resolvers execute in phases based on dependencies:

```
Phase 1: environment, port
Phase 2: base_url
Phase 3: config
```

### Dependency Rules

- Resolvers in the same phase run concurrently
- A resolver waits for all its dependencies to complete
- Circular dependencies cause an error

---

## Transformations

Transform values after they're resolved using the `transform` phase.

### Example: String Manipulation

Create a file called `transform.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: transform-example
  version: 1.0.0

spec:
  resolvers:
    raw_input:
      type: string
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              key: input
          - provider: static
            inputs:
              value: "  Hello World  "
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.trim()"
          - provider: cel
            inputs:
              expression: "__self.lowerAscii()"
```

**Key Concept**: In the transform phase, `__self` refers to the current value being transformed. Each transform step receives the output of the previous step.

Run it:

{{< tabs "resolver-tutorial-cmd-5" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f transform.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f transform.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "raw_input": "hello world"
}
```

> [!NOTE]
> **Tip**: `scafctl run resolver -o json` includes `__execution` metadata by default. Use `--hide-execution` for cleaner output. All examples in this tutorial use `--hide-execution`. See the [Run Resolver Tutorial](run-resolver-tutorial.md) for details on the execution metadata.

The value was trimmed of whitespace, then lowercased — each transform step feeds into the next.

### Example: Data Enrichment

Create a file called `enrich.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: enrich-config
  version: 1.0.0

spec:
  resolvers:
    base_config:
      type: any
      resolve:
        with:
          - provider: static
            inputs:
              value:
                name: my-app
                version: "1.0.0"
      transform:
        with:
          # Add timestamp
          - provider: cel
            inputs:
              expression: "map.merge(__self, {'timestamp': time.now()})"
          # Add environment-specific settings
          - provider: cel
            inputs:
              expression: "map.merge(__self, {'debug': true, 'logLevel': 'info'})"
```

Run it:

{{< tabs "resolver-tutorial-cmd-6" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f enrich.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f enrich.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output (timestamp will vary):

```json
{
  "base_config": {
    "debug": true,
    "logLevel": "info",
    "name": "my-app",
    "timestamp": "2026-02-16T12:00:00.000000-05:00",
    "version": "1.0.0"
  }
}
```

---

## Validation

Validate resolved values to ensure they meet requirements.

### Example: Port Range Validation

Create a file called `validated-config.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: validated-config
  version: 1.0.0

spec:
  resolvers:
    port:
      type: int
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              key: port
          - provider: static
            inputs:
              value: 8080
      validate:
        with:
          - provider: validation
            inputs:
              expression: "__self >= 1024 && __self <= 65535"
              message: "Port must be between 1024 and 65535"
```

Run it with a valid port:

{{< tabs "resolver-tutorial-cmd-7" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f validated-config.yaml -r port=8080 -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f validated-config.yaml -r port=8080 -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "port": 8080
}
```

Run it with an invalid port to see the validation error:

{{< tabs "resolver-tutorial-cmd-8" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f validated-config.yaml -r port=80
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f validated-config.yaml -r port=80
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
❌ resolver execution failed: ... validation: Port must be between 1024 and 65535
```

### Example: Multiple Validations

Create a file called `email-validator.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: email-validator
  version: 1.0.0

spec:
  resolvers:
    email:
      type: string
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              key: email
          - provider: static
            inputs:
              value: "user@example.com"
      validate:
        with:
          - provider: validation
            inputs:
              match: "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
              message: "Invalid email format"
          - provider: validation
            inputs:
              expression: "!__self.endsWith('.test')"
              message: "Test emails not allowed"
```

**Note**: All validation rules run and errors are aggregated. You'll see all failures, not just the first one.

Run it:

{{< tabs "resolver-tutorial-cmd-9" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f email-validator.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f email-validator.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "email": "user@example.com"
}
```

Now try an invalid value that fails **both** validations — not a valid email format **and** ends with `.test`:

{{< tabs "resolver-tutorial-cmd-10" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f email-validator.yaml -r email="not-an-email.test" -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f email-validator.yaml -r email="not-an-email.test" -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```text
❌ resolver execution failed: phase 1 failed: resolver "email" failed: resolver "email" validation failed with 2 errors:
  - [rule 1] validation: Invalid email format
  - [rule 2] validation: Test emails not allowed
```

Both validation errors are reported together rather than failing on the first one.

---

## Conditional Execution

Skip resolvers or phases based on conditions.

### Resolver-Level Condition

Create a file called `conditional.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: conditional-example
  version: 1.0.0

spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              key: env
          - provider: static
            inputs:
              value: development
    
    # Only runs in production
    prod_secrets:
      when:
        expr: "_.environment == 'production'"
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "prod-secret-value"
```

Run it with `development` (default) — the `prod_secrets` resolver is skipped:

{{< tabs "resolver-tutorial-cmd-11" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f conditional.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f conditional.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output (only `environment` is resolved; `prod_secrets` is skipped):

```json
{
  "environment": "development"
}
```

Run it with `production` — the `prod_secrets` resolver executes:

{{< tabs "resolver-tutorial-cmd-12" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f conditional.yaml -r env=production -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f conditional.yaml -r env=production -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "environment": "production",
  "prod_secrets": "prod-secret-value"
}
```

### Phase-Level Condition

Create a file called `phase-condition.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: phase-condition
  version: 1.0.0

spec:
  resolvers:
    feature_flags:
      type: any
      resolve:
        with:
          - provider: static
            inputs:
              value:
                enable_transform: true
      transform:
        with:
          - provider: cel
            when:
              expr: "__self.enable_transform == true"
            inputs:
              expression: "map.merge(__self, {'transformed': true})"
```

Run it:

{{< tabs "resolver-tutorial-cmd-13" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f phase-condition.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f phase-condition.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "feature_flags": {
    "enable_transform": true,
    "transformed": true
  }
}
```

---

## Error Handling

Handle errors gracefully with fallback sources.

### Fallback Pattern

Create a file called `fallback.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: fallback-example
  version: 1.0.0

spec:
  resolvers:
    config:
      type: any
      resolve:
        with:
          # Try remote config first
          - provider: http
            inputs:
              url: https://config.example.com/settings
              timeout: 5s
          # Fall back to local file
          - provider: file
            inputs:
              operation: read
              path: ./config.json
          # Last resort: default values
          - provider: static
            inputs:
              value:
                debug: false
                timeout: 30
```

Run it (the HTTP and file providers will fail, so it falls back to static):

{{< tabs "resolver-tutorial-cmd-14" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f fallback.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f fallback.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "config": {
    "debug": false,
    "timeout": 30
  }
}
```

**onError Options** (resolve phase):
- `continue` (default): Try the next source in the list. The resolve phase acts as an implicit fallback chain.
- `fail`: Stop execution immediately and return the error without trying remaining sources.

### Custom Error Messages

Use the `messages` field to replace default error text with user-friendly messages. The `messages.error` field supports static strings, CEL expressions (`expr:`), and Go templates (`tmpl:`):

```yaml
# messages-error.yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: custom-error-messages
  version: 1.0.0

spec:
  resolvers:
    port:
      description: Application port number
      messages:
        error: "Port must be between 1024 and 65535. Got: {{ .__error }}"
      resolve:
        with:
          - provider: parameter
            inputs:
              name: port
      validate:
        with:
          - provider: validation
            inputs:
              rules:
                - expr: "int(__self) >= 1024 && int(__self) <= 65535"
                  message: invalid port range

    apiEndpoint:
      description: API endpoint URL
      messages:
        error:
          expr: "'Failed to reach API endpoint. Error: ' + __error"
      resolve:
        with:
          - provider: http
            inputs:
              url: https://api.example.com/health
              method: GET
```

The `messages.error` value is evaluated when any phase fails (resolve, transform, or validate). Two variables are available:

| Variable | Description |
|----------|-------------|
| `_` | The resolver data map (all resolved values so far) |
| `__error` | The original error message string |

When a custom error message is set, it replaces the default error text in all output — CLI, MCP tools, and snapshot captures.

---

## Working with HTTP APIs

Fetch configuration from remote APIs.

### Basic HTTP Request

Create a file called `http-example.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: http-example
  version: 1.0.0

spec:
  resolvers:
    api_data:
      type: any
      resolve:
        with:
          - provider: http
            inputs:
              url: https://httpbin.org/get
              method: GET
              headers:
                Accept: application/json
              timeout: 10s
```

Run it:

{{< tabs "resolver-tutorial-cmd-15" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f http-example.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f http-example.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output (body and headers will vary):

```json
{
  "api_data": {
    "body": "...",
    "headers": { "...": "..." },
    "statusCode": 200
  }
}
```

### With Authentication

Create a file called `auth-api.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: authenticated-api
  version: 1.0.0

spec:
  resolvers:
    api_token:
      sensitive: true  # Redact in table output
      resolve:
        with:
          - provider: env
            inputs:
              operation: get
              name: API_TOKEN
    
    api_data:
      type: any
      resolve:
        with:
          - provider: http
            inputs:
              url: https://httpbin.org/headers
              headers:
                Authorization:
                  tmpl: "Bearer {{._.api_token}}"
```

Run it (requires the `API_TOKEN` environment variable to be set):

{{< tabs "resolver-tutorial-cmd-16" >}}
{{% tab "Bash" %}}
```bash
export API_TOKEN=your-token-here
scafctl run resolver -f auth-api.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
$env:API_TOKEN = "your-token-here"
scafctl run resolver -f auth-api.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

---

## Common Patterns

The following patterns are complete, self-contained solution files. Create a new file for each pattern to try it out.

### Pattern 1: Environment-Based Configuration

Create a file called `env-config.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: env-config
  version: 1.0.0

spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              key: env
          - provider: static
            inputs:
              value: development
    
    database_url:
      type: string
      sensitive: true
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                _.environment == 'production' 
                  ? 'postgres://prod-db.example.com:5432/app'
                  : 'postgres://localhost:5432/app_dev'
```

{{< tabs "resolver-tutorial-cmd-17" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f env-config.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f env-config.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
╭─ scafctl run resolver ─╮
│KEY           VALUE     │
│─────────────────────── │
│database_url  [REDACTED]│
│environment   development│
╰ _ ─────────── map: 1/2 ╯
```

> [!NOTE]
> **Note**: Fields marked `sensitive: true` are shown as `[REDACTED]` in table output.

Structured output (JSON, YAML) reveals sensitive values for machine consumption:

{{< tabs "resolver-tutorial-cmd-18" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f env-config.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f env-config.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "database_url": "postgres://localhost:5432/app_dev",
  "environment": "development"
}
```

Use `--show-sensitive` to reveal values in table output:

{{< tabs "resolver-tutorial-cmd-19" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f env-config.yaml --show-sensitive
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f env-config.yaml --show-sensitive
```
{{% /tab %}}
{{< /tabs >}}

> [!NOTE]
> **Sensitive Redaction Behavior**: Sensitive values are redacted in table/interactive output (human-facing) but revealed in JSON/YAML output (machine-facing), following the same model as Terraform. Use `--show-sensitive` to reveal values in all output formats.

### Pattern 2: Feature Toggles

Create a file called `feature-toggles.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: feature-toggles
  version: 1.0.0

spec:
  resolvers:
    features:
      type: any
      resolve:
        with:
          - provider: http
            onError: continue
            inputs:
              url: https://features.example.com/api/flags
          - provider: static
            inputs:
              value:
                new_ui: false
                dark_mode: true
    
    ui_config:
      type: any
      resolve:
        with:
          - provider: cel
            inputs:
              expression: |
                {
                  'theme': _.features.dark_mode ? 'dark' : 'light',
                  'version': _.features.new_ui ? 'v2' : 'v1'
                }
```

{{< tabs "resolver-tutorial-cmd-20" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f feature-toggles.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f feature-toggles.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "features": {
    "dark_mode": true,
    "new_ui": false
  },
  "ui_config": {
    "theme": "dark",
    "version": "v1"
  }
}
```

### Pattern 3: Secret Management

Create a file called `secrets.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: secrets
  version: 1.0.0

spec:
  resolvers:
    db_password:
      type: string
      sensitive: true
      resolve:
        with:
          # In practice, use env or file providers here
          # with onError: continue to fall through
          - provider: static
            inputs:
              value: "default-dev-password"
    
    connection_string:
      sensitive: true
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "'postgres://app:' + _.db_password + '@db.example.com:5432/app'"
```

{{< tabs "resolver-tutorial-cmd-21" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f secrets.yaml
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f secrets.yaml
```
{{% /tab %}}
{{< /tabs >}}

Output:

```
╭─ scafctl run resolver ──────╮
│KEY                VALUE     │
│────────────────────────────  │
│connection_string  [REDACTED] │
│db_password        [REDACTED] │
╰ _ ──────────────── map: 1/2  ╯
```

> [!NOTE]
> **Note**: Both resolvers are marked `sensitive: true`, so their values are redacted in table output.

Structured output reveals the actual values:

{{< tabs "resolver-tutorial-cmd-22" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f secrets.yaml -o json --hide-execution
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f secrets.yaml -o json --hide-execution
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "connection_string": "postgres://app:default-dev-password@db.example.com:5432/app",
  "db_password": "default-dev-password"
}
```

> [!NOTE]
> **Tip**: Use table output (the default) when sharing your screen or in CI logs to avoid accidentally exposing secrets. Use `-o json` or `-o yaml` when piping to downstream tools that need the actual values.

### Pattern 4: Multi-Stage Pipeline

Create a file called `pipeline.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: data-pipeline
  version: 1.0.0

spec:
  resolvers:
    raw_data:
      type: any
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - id: 1
                  name: "Alice"
                  email: "alice@example.com"
                  active: true
                - id: 2
                  name: "Bob"
                  email: "bob@example.com"
                  active: false
                - id: 3
                  name: "Charlie"
                  email: "charlie@example.com"
                  active: true
                - id: 4
                  name: "Diana"
                  email: "diana@example.com"
                  active: true
    
    parsed_data:
      type: any
      resolve:
        with:
          - provider: cel
            inputs:
              expression: "_.raw_data"
      transform:
        with:
          # Filter active users
          - provider: cel
            inputs:
              expression: "__self.filter(u, u.active == true)"
          # Select only needed fields
          - provider: cel
            inputs:
              expression: "__self.map(u, {'id': u.id, 'name': u.name, 'email': u.email})"
      validate:
        with:
          - provider: validation
            inputs:
              expression: "size(__self) > 0"
              message: "No active users found"
```

{{< tabs "resolver-tutorial-cmd-23" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver -f pipeline.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver -f pipeline.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

Output:

```json
{
  "parsed_data": [
    { "email": "alice@example.com", "id": 1, "name": "Alice" },
    { "email": "charlie@example.com", "id": 3, "name": "Charlie" },
    { "email": "diana@example.com", "id": 4, "name": "Diana" }
  ],
  "raw_data": [
    { "active": true, "email": "alice@example.com", "id": 1, "name": "Alice" },
    { "active": false, "email": "bob@example.com", "id": 2, "name": "Bob" },
    { "active": true, "email": "charlie@example.com", "id": 3, "name": "Charlie" },
    { "active": true, "email": "diana@example.com", "id": 4, "name": "Diana" }
  ]
}
```

Bob was filtered out of `parsed_data` because `active` was `false`. The `raw_data` resolver is also included since `run resolver` returns all resolvers by default.

---

## Array Iteration with `forEach`

### Transform Phase `forEach`

When a resolver produces an array, the `transform.with.forEach` clause processes each element independently and collects results back into an array:

Save this as `foreach-demo.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: foreach-demo
  version: 1.0.0

spec:
  resolvers:
    doubled:
      type: '[]int'
      resolve:
        with:
          - provider: static
            inputs:
              value: [1, 2, 3, 4, 5]
      transform:
        with:
          - provider: cel
            forEach:
              item: num       # alias for the current element
              index: i        # alias for the current index
            inputs:
              expression: "num * 2"
```

Run it to see the result:

{{< tabs "resolver-foreach-run" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver doubled -f foreach-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver doubled -f foreach-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

#### Filtering with `when` and `forEach`

By default, items where the `when` condition evaluates to `false` are **removed** from the output array. This makes `forEach` + `when` a natural filter:

```yaml
transform:
  with:
    - provider: cel
      forEach:
        item: num
      when:
        expr: "num % 2 == 0"    # only even numbers
      inputs:
        expression: "num * 2"
```

Input `[1, 2, 3, 4, 5]` → output `[4, 8]` (only even numbers, doubled).

To retain index alignment (`nil` in place of skipped items), set `keepSkipped: true`:

```yaml
forEach:
  item: num
  keepSkipped: true    # output: [nil, 4, nil, 8, nil]
```

---

### Resolve Phase `forEach` with `filter`

For resolvers that produce arrays by resolving each element individually, use `forEach` directly in the `resolve` phase. This is useful when you want to iterate over an existing array and resolve a value for each item using provider logic or `when` conditions:

Save this as `foreach-filter-demo.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: foreach-filter-demo
  version: 1.0.0

spec:
  resolvers:
    allUsers:
      type: '[]object'
      resolve:
        with:
          - provider: static
            inputs:
              value:
                - {name: Alice, active: true}
                - {name: Bob,   active: false}
                - {name: Carol, active: true}

    activeUsers:
      type: '[]object'
      resolve:
        forEach:
          items:
            expr: allUsers       # source array (evaluates allUsers resolver)
          as: user               # alias for each element
          filter: true           # remove nil results from output
          resolve:
            with:
              - provider: static
                when:
                  expr: 'user.active == true'
                inputs:
                  value:
                    expr: user
```

Without `filter: true` the output would include `nil` for Bob:

```
[{name: Alice, active: true}, nil, {name: Carol, active: true}]
```

With `filter: true` the output contains only matched items:

```
[{name: Alice, active: true}, {name: Carol, active: true}]
```

Run it:

{{< tabs "resolver-tutorial-cmd-24" >}}
{{% tab "Bash" %}}
```bash
scafctl run resolver activeUsers -f foreach-filter-demo.yaml -o json
```
{{% /tab %}}
{{% tab "PowerShell" %}}
```powershell
scafctl run resolver activeUsers -f foreach-filter-demo.yaml -o json
```
{{% /tab %}}
{{< /tabs >}}

#### `filter` vs `keepSkipped`

| | `resolve.forEach` with `filter: true` | `transform.with.forEach` with `keepSkipped: true` |
|-|--------------------------------------|--------------------------------------------------|
| **Phase** | Resolve | Transform |
| **Default** | Keep nil (index-aligned) | Remove nil (auto-filter) |
| **Opt-in** | `filter: true` removes nil | `keepSkipped: true` retains nil |

---

## Troubleshooting

### Common Issues

**Circular dependency error**
```
Error: circular dependency detected: a -> b -> a
```
Solution: Refactor to break the cycle, possibly by combining resolvers.

**Type coercion error**
```
Error: cannot coerce "hello" to int
```
Solution: Ensure your provider returns a value compatible with the declared type.

**Timeout error**
```
Error: resolver "slow_api" timed out after 30s
```
Solution: Increase the timeout in the resolver definition:
```yaml
timeout: 60s
```

**Validation failed**
```
Error: validation failed: Port must be between 1024 and 65535
```
Solution: Check your input values meet the validation requirements.

---

## Next Steps

- [Run Resolver Tutorial](run-resolver-tutorial.md) — Debug and inspect resolver execution
- [Run Provider Tutorial](run-provider-tutorial.md) — Test providers in isolation
- [Actions Tutorial](actions-tutorial.md) — Learn about workflows
- [CEL Expressions Tutorial](cel-tutorial.md) — Master CEL expressions and extension functions
- [Provider Reference](provider-reference.md) — Complete provider documentation
