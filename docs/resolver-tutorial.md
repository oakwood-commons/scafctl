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
name: hello-world
version: "1.0.0"
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

```bash
scafctl run solution -f hello.yaml
```

Output:
```json
{
  "greeting": "Hello, World!"
}
```

### Understanding the Structure

- **name/version**: Solution metadata
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
name: parameterized-greeting
version: "1.0.0"
spec:
  resolvers:
    name:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              name: user_name
              default: "World"
    
    greeting:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "'Hello, ' + _.name + '!'"
```

### Step 2: Run with Parameters

```bash
# Use default value
scafctl run solution -f greet.yaml

# Pass a parameter
scafctl run solution -f greet.yaml -r user_name=Alice

# Pass multiple parameters
scafctl run solution -f greet.yaml -r user_name=Bob -r another=value
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
```bash
scafctl run solution -f greet.yaml -r @params.yaml
```

---

## Resolver Dependencies

Resolvers can reference other resolvers using `_.resolver_name` syntax in CEL expressions.

### Step 1: Create a Solution with Dependencies

Create `config.yaml`:

```yaml
name: config-builder
version: "1.0.0"
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              name: env
              default: development
    
    port:
      type: int
      resolve:
        with:
          - provider: parameter
            inputs:
              name: port
              default: 8080
    
    base_url:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: |
                  _.environment == 'production' 
                    ? 'https://api.example.com' 
                    : 'http://localhost:' + string(_.port)
    
    config:
      type: any
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: |
                  {
                    'environment': _.environment,
                    'port': _.port,
                    'baseUrl': _.base_url,
                    'debug': _.environment != 'production'
                  }
```

### Step 2: Run and Observe Phases

```bash
scafctl run solution -f config.yaml --progress
```

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

```yaml
name: transform-example
version: "1.0.0"
spec:
  resolvers:
    raw_input:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              name: input
              default: "  Hello World  "
      transform:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "__self.trim()"
          - provider: cel
            inputs:
              expression:
                expr: "__self.lowerAscii()"
```

**Key Concept**: In the transform phase, `__self` refers to the current value being transformed. Each transform step receives the output of the previous step.

### Example: Data Enrichment

```yaml
name: enrich-config
version: "1.0.0"
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
              expression:
                expr: "__self.merge({'timestamp': now()})"
          # Add environment-specific settings
          - provider: cel
            inputs:
              expression:
                expr: "__self.merge({'debug': true, 'logLevel': 'info'})"
```

---

## Validation

Validate resolved values to ensure they meet requirements.

### Example: Port Range Validation

```yaml
name: validated-config
version: "1.0.0"
spec:
  resolvers:
    port:
      type: int
      resolve:
        with:
          - provider: parameter
            inputs:
              name: port
              default: 8080
      validate:
        with:
          - provider: validation
            inputs:
              expression:
                expr: "__self >= 1024 && __self <= 65535"
              message: "Port must be between 1024 and 65535"
```

### Example: Multiple Validations

```yaml
name: email-validator
version: "1.0.0"
spec:
  resolvers:
    email:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              name: email
      validate:
        with:
          - provider: validation
            inputs:
              pattern: "^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$"
              message: "Invalid email format"
          - provider: validation
            inputs:
              expression:
                expr: "!__self.endsWith('.test')"
              message: "Test emails not allowed"
```

**Note**: All validation rules run and errors are aggregated. You'll see all failures, not just the first one.

---

## Conditional Execution

Skip resolvers or phases based on conditions.

### Resolver-Level Condition

```yaml
name: conditional-example
version: "1.0.0"
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              name: env
              default: development
    
    # Only runs in production
    prod_secrets:
      when:
        expr: "_.environment == 'production'"
      resolve:
        with:
          - provider: env
            inputs:
              name: PROD_API_KEY
```

### Phase-Level Condition

```yaml
name: phase-condition
version: "1.0.0"
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
        when:
          expr: "_.feature_flags.enable_transform == true"
        with:
          - provider: cel
            inputs:
              expression:
                expr: "__self.merge({'transformed': true})"
```

---

## Error Handling

Handle errors gracefully with fallback sources.

### Fallback Pattern

```yaml
name: fallback-example
version: "1.0.0"
spec:
  resolvers:
    config:
      type: any
      resolve:
        with:
          # Try remote config first
          - provider: http
            error_behavior: continue
            inputs:
              url: https://config.example.com/settings
              timeout: 5s
          # Fall back to local file
          - provider: file
            error_behavior: continue
            inputs:
              path: ./config.json
          # Last resort: default values
          - provider: static
            inputs:
              value:
                debug: false
                timeout: 30
```

**Error Behaviors**:
- `fail` (default): Stop execution and return error
- `continue`: Try the next source in the list

---

## Working with HTTP APIs

Fetch configuration from remote APIs.

### Basic HTTP Request

```yaml
name: http-example
version: "1.0.0"
spec:
  resolvers:
    api_data:
      type: any
      resolve:
        with:
          - provider: http
            inputs:
              url: https://api.example.com/config
              method: GET
              headers:
                Accept: application/json
              timeout: 10s
```

### With Authentication

```yaml
name: authenticated-api
version: "1.0.0"
spec:
  resolvers:
    api_token:
      sensitive: true  # Redact in logs
      resolve:
        with:
          - provider: env
            inputs:
              name: API_TOKEN
    
    api_data:
      type: any
      resolve:
        with:
          - provider: http
            inputs:
              url: https://api.example.com/secure/config
              headers:
                Authorization:
                  tmpl: "Bearer {{._.api_token}}"
```

---

## Common Patterns

### Pattern 1: Environment-Based Configuration

```yaml
name: env-config
version: "1.0.0"
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              name: env
              default: development
    
    database_url:
      type: string
      sensitive: true
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: |
                  _.environment == 'production' 
                    ? 'postgres://prod-db.example.com:5432/app'
                    : 'postgres://localhost:5432/app_dev'
```

### Pattern 2: Feature Toggles

```yaml
name: feature-toggles
version: "1.0.0"
spec:
  resolvers:
    features:
      type: any
      resolve:
        with:
          - provider: http
            error_behavior: continue
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
              expression:
                expr: |
                  {
                    'theme': _.features.dark_mode ? 'dark' : 'light',
                    'version': _.features.new_ui ? 'v2' : 'v1'
                  }
```

### Pattern 3: Secret Management

```yaml
name: secrets
version: "1.0.0"
spec:
  resolvers:
    db_password:
      sensitive: true
      resolve:
        with:
          # Try environment variable first
          - provider: env
            error_behavior: continue
            inputs:
              name: DB_PASSWORD
          # Fall back to file-based secret
          - provider: file
            inputs:
              path: /run/secrets/db_password
    
    connection_string:
      sensitive: true
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: |
                  'postgres://app:' + _.db_password + '@db.example.com:5432/app'
```

### Pattern 4: Multi-Stage Pipeline

```yaml
name: data-pipeline
version: "1.0.0"
spec:
  resolvers:
    raw_data:
      resolve:
        with:
          - provider: http
            inputs:
              url: https://api.example.com/users
    
    parsed_data:
      type: any
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "_.raw_data"
      transform:
        with:
          # Filter active users
          - provider: cel
            inputs:
              expression:
                expr: "__self.filter(u, u.active == true)"
          # Select only needed fields
          - provider: cel
            inputs:
              expression:
                expr: "__self.map(u, {'id': u.id, 'name': u.name, 'email': u.email})"
      validate:
        with:
          - provider: validation
            inputs:
              expression:
                expr: "size(__self) > 0"
              message: "No active users found"
```

---

## Next Steps

- Explore the [built-in providers](../pkg/provider/builtin/README.md)
- Read about [CEL expressions](../pkg/celexp/README.md)
- Check out the [example solutions](./examples/)
- Review the [resolver package reference](../pkg/resolver/README.md)

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
