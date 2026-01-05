# Resolver Schema

## Overview

**Resolvers** produce named values through a four-phase pipeline: resolve → transform → validate → emit.

Each resolver follows the same structure, regardless of complexity.

## Full Schema

```yaml
spec:
  resolvers:
    resolverName:
      description: What this resolver produces
  
      # Phase 1: Resolve - Gather data
      resolve:
        from:
          - provider: cli
            inputs:
              key: keyName
          - provider: env
            inputs:
              key: ENV_VAR
            when: _.environment != "local"
          - provider: git
            inputs:
              field: branch
          - provider: state
            inputs:
              key: previousResolver
          - provider: expression
            inputs:
              expr: _.other + "-value"
          - provider: static
            inputs:
              value: fallback-default
        until: __self != ""
  
      # Phase 2: Transform - Process data
      transform:
        when: _.condition == true
        until: __self != ""
        into:
          - expr: __self.toLowerCase()
          - expr: __self.replace('_', '-')
            when: _.environment == "prod"
          - expr: __self + "-final"
  
      # Phase 3: Validate - Enforce constraints
      validate:
        - expr: __self.matches("^[a-z0-9-]+$")
          message: "Must be lowercase alphanumeric with hyphens"
        - expr: size(__self) >= 2 && size(__self) <= 50
          message: "Must be 2-50 characters"
```

## Phase 1: Resolve

### Structure

```yaml
resolve:
  from:
    - provider: source-type
      # Source-specific fields
```

### Provider Types

#### CLI Input

User input from command line.

```yaml
- provider: cli
  key: projectName
```

Usage:
```bash
scafctl run solution:app -r projectName=my-app
```

#### Environment Variables

Environment variable values.

```yaml
- provider: env
  key: PROJECT_NAME
```

Usage:
```bash
export PROJECT_NAME=my-app
scafctl run solution:app
```

#### Git Metadata

Git repository information.

```yaml
- provider: git
  field: branch   # branch, tag, commit, url, author, date
```

#### State

Previously resolved values (from prior resolver execution).

```yaml
- provider: state
  key: previousResolverName
```

#### Expression

CEL expression computed from other resolvers.

```yaml
- provider: expression
  expr: _.projectName + ":" + _.version
```

Access other resolvers with `_`:
```yaml
expr: _.org + "/" + _.repo
expr: type(__self) == "string" ? parseJson(__self) : __self
```

#### Static

Default fallback value.

```yaml
- provider: static
  value: "default-value"
```

For complex values:
```yaml
- provider: static
  value:
    key1: value1
    key2: value2
```

### Source Evaluation Order

Sources are tried **in the order they appear in the `from:` array**. First non-null wins by default (or use `until:` for custom logic):

- Order determined by your YAML, not by provider type
- Try sources sequentially until one returns non-null
- Use `until:` condition to customize what "wins"

## Phase 2: Transform

### Structure

```yaml
transform:
  when: condition              # Optional: skip if false
  until: stopping-condition   # Optional: stop when true
  into:
    - expr: first-transform
    - expr: second-transform
      when: item-condition    # Optional: skip if false
```

### Context

| Variable | Meaning | Available |
|----------|---------|-----------|
| `__self` | Current value (output of previous transform) | In all transforms |
| `_` | All resolvers | In all transforms |
| `_.resolverName` | Specific resolver value | In all transforms |

### Transform-Level Conditions

#### `when:` at Transform Level

Skip entire transform if condition is false:

```yaml
transform:
  when: _.enabled == true  # If false, skip all items
  into:
    - expr: __self.toLowerCase()
```

#### `until:` at Transform Level

Stop remaining items when condition is true:

```yaml
transform:
  until: __self != ""  # Stop if non-empty
  into:
    - expr: _.primary != "" ? _.primary : ""
    - expr: _.fallback != "" ? _.fallback : ""
    - expr: "default"
```

### Item-Level Conditions

#### `when:` at Item Level

Skip individual item if condition is false:

```yaml
into:
  - expr: __self.toUpperCase()
    when: _.environment == "prod"

  - expr: __self + "-dev"
    when: _.environment != "prod"
```

Skipped items don't execute, next item receives same `__self`.

### Sequential Execution

```yaml
into:
  - expr: __self.toLowerCase()              # "MyApp" → "myapp"
  - expr: __self.replace('_', '-')         # "my_app" → "my-app"
  - expr: __self + "-service"              # "my-app" → "my-app-service"
```

Each step's output becomes next step's `__self`.

### Type Checking

Handle multiple input formats:

```yaml
into:
  # Parse JSON strings to objects
  - expr: |
      type(__self) == "string" && __self.startsWith('{') ?
        parseJson(__self) : __self

  # Process as object
  - expr: |
      type(__self) == "object" ?
        __self + {processed: true} : __self
```

## Phase 3: Validate

### Structure

```yaml
validate:
  - expr: condition
    message: error message
  - expr: another-condition
    message: another error
    when: conditional-gate  # Optional: only validate if true
```

### Context

| Variable | Meaning |
|----------|---------|
| `__self` | The value being validated (after transform) |
| `_` | All resolvers |

### Common Patterns

#### Pattern Matching

```yaml
validate:
  - expr: __self.matches("^[a-z0-9-]+$")
    message: "Must be lowercase alphanumeric with hyphens"
```

#### Range Checking

```yaml
validate:
  - expr: size(__self) >= 2 && size(__self) <= 50
    message: "Must be 2-50 characters"
```

#### Boundary Checking

```yaml
validate:
  - expr: !__self.startsWith('-') && !__self.endsWith('-')
    message: "Cannot start or end with hyphen"
```

#### Value Set

```yaml
validate:
  - expr: __self in ["dev", "staging", "prod"]
    message: "Must be dev, staging, or prod"
```

#### Conditional Validation

```yaml
validate:
  - expr: __self.startsWith('https://')
    message: "Production URLs must use HTTPS"
    when: _.environment == 'production'
  - expr: size(__self) <= 100
    message: "URL length must be 100 characters or less"
    when: _.environment != 'development'
```

The `when` condition is evaluated first. If it returns `false`, the validation rule is skipped.

#### Dynamic Error Messages

Messages support Go templates for context-aware errors:

```yaml
validate:
  - expr: size(__self) >= 2 && size(__self) <= 50
    message: "Name '{{ __self }}' must be 2-50 characters (got {{ size __self }})"
  - expr: __self.matches("^[a-z0-9-]+$")
    message: "Invalid name '{{ __self }}' for environment {{ _.environment }}"
```

Available in message templates:
- `{{ __self }}` - Current value
- `{{ _.resolverName }}` - Any resolver value
- Go template functions (e.g., `{{ size __self }}`)

### Shorthand: Regex Validation

```yaml
validate:
  - regex: "^[a-z0-9-]+$"
    message: "Must be lowercase alphanumeric"
```

Equivalent to:
```yaml
validate:
  - expr: __self.matches("^[a-z0-9-]+$")
    message: "Must be lowercase alphanumeric"
```

## Conditional Resolution Patterns

### Source-Level Conditionals with `when`

Skip sources based on runtime context:

```yaml
resolve:
  from:
    - provider: vault
      when: _.environment == 'production'
      inputs:
        path: "secret/prod/api-key"
    - provider: env
      when: _.environment != 'production'
      inputs:
        key: API_KEY
    - provider: static
      inputs:
        value: "dev-api-key"
```

Each source's `when` condition is evaluated with access to all resolved values via `_`.

### Phase-Level Early Exit with `until`

Stop trying sources when a condition is met:

```yaml
resolve:
  from:
    - provider: cli
      inputs:
        key: config
    - provider: env
      inputs:
        key: CONFIG_PATH
    - provider: git
      inputs:
        field: config
    - provider: static
      inputs:
        value: "./config.yaml"
  until: __self != ""
```

The `until` condition is checked after each successful source. When it evaluates to `true`, no further sources are tried.

Common patterns:
- `until: __self != ""` - Stop at first non-empty value
- `until: __self.startsWith("http")` - Stop at first URL
- `until: size(__self) > 0` - Stop at first non-empty collection

### Combined Conditionals

```yaml
resolve:
  from:
    - provider: vault
      when: _.useVault == true && _.environment == 'production'
      inputs:
        path: "secret/database"
    - provider: api
      when: _.environment != 'local'
      inputs:
        endpoint: "https://config-service/db"
    - provider: env
      inputs:
        key: DATABASE_URL
    - provider: static
      inputs:
        value: "postgresql://localhost:5432/dev"
  until: __self.startsWith("postgresql://")
```

This resolver:
1. Uses Vault only in production when enabled (source-level `when`)
2. Uses API in non-local environments (source-level `when`)
3. Always tries environment variables (no `when`)
4. Falls back to local default (no `when`)
5. Stops as soon as a valid PostgreSQL URL is found (phase-level `until`)

## Phase 4: Emit

Once resolved, transformed, and validated, the value is **emitted**.

Available as:
- `_.resolverName` in other resolvers
- `_.resolverName` in actions
- Default output of `scafctl run` (if no action specified)

## Complete Examples

### Simple Resolver

```yaml
projectName:
  description: Project name from CLI input with fallback
  resolve:
    from:
      - provider: cli
        inputs:
          key: project
      - provider: static
        inputs:
          value: my-app
```

### Resolver with Transform

```yaml
normalizedName:
  description: Normalized project name
  resolve:
    from:
      - provider: cli
        key: name
  transform:
    into:
      - expr: __self.toLowerCase()
      - expr: __self.replace('_', '-')
  validate:
    - expr: __self.matches("^[a-z0-9-]+$")
      message: "Invalid format"
```

### Resolver with Fallback

```yaml
branch:
  description: Git branch with fallback chain
  resolve:
    from:
      - provider: cli
        key: branch
      - provider: static
        value: ""
  transform:
    until: __self != ""
    into:
      - expr: _.userBranch != "" ? _.userBranch : ""
      - expr: _.gitBranch != "" ? _.gitBranch : ""
      - expr: "main"
```

### Type-Based Resolver

```yaml
config:
  description: Config that can be string or object
  resolve:
    from:
      - provider: cli
        key: config
  transform:
    into:
      # Parse JSON if string
      - expr: |
          type(__self) == "string" && __self.startsWith('{') ?
            parseJson(__self) : __self

      # Ensure object
      - expr: |
          type(__self) == "object" ?
            __self + {processed: true} : __self
  validate:
    - expr: type(__self) == "object"
      message: "Must be valid JSON object"
```

### Derived Resolver

```yaml
spec:
  resolvers:
    org:
      resolve:
        from:
          - provider: cli
            inputs:
              key: org
  
    repo:
      resolve:
        from:
          - provider: cli
            inputs:
              key: repo
  
    repoUrl:
      description: Derived from org and repo
      resolve:
        from:
          - provider: expression
            inputs:
              expr: "https://github.com/" + _.org + "/" + _.repo + ".git"
```

## Best Practices

1. **Order sources intentionally** - First source in `from:` array is tried first
2. **Use environment variables for secrets** - Don't hardcode
3. **Use expressions for computed values** - Avoid duplication
4. **Keep transforms simple** - Multiple small steps > one complex step
5. **Validate aggressively** - Fail fast with clear messages
6. **Use descriptions** - Explain what each resolver does
7. **Name clearly** - `imageName` better than `img`
8. **Test edge cases** - Empty strings, null, wrong types
9. **Document transforms** - Complex logic needs comments
10. **Use type checking** - Handle multiple input formats

## Field Reference

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | Yes | What this resolver produces |
| `resolve.from` | array | Yes | List of resolution sources |
| `resolve.from[].when` | CEL expr | No | Skip this source if false |
| `resolve.until` | CEL expr | No | Stop trying sources when true |
| `transform.when` | CEL expr | No | Skip transform if false |
| `transform.until` | CEL expr | No | Stop remaining items when true |
| `transform.into` | array | No | Array of transform expressions |
| `transform.into[].expr` | CEL expr | Yes | Expression to execute |
| `transform.into[].when` | CEL expr | No | Skip item if false |
| `validate[]` | array | No | Array of validation rules |
| `validate[].expr` | CEL expr | Yes* | Condition that must be true (required if regex not provided) |
| `validate[].message` | string/template | Yes | Error message (supports Go templates with `{{ __self }}` and `{{ _.key }}`) |
| `validate[].regex` | pattern | No | Shorthand for regex pattern |
| `validate[].when` | CEL expr | No | Skip validation rule if false |

## Next Steps

- **Transform details** → [Transform Phase Guide](../guides/03-transform-phase.md)
- **Action schema** → [Action Schema](./action-schema.md)
- **Full resolver guide** → [Resolver Pipeline Guide](../guides/02-resolver-pipeline.md)

---

Resolvers are the foundation of scafctl. They produce the data that everything else uses.
