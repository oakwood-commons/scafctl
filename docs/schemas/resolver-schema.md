# Resolver Schema

## Overview

**Resolvers** produce named values through a four-phase pipeline: resolve → transform → validate → emit.

Each resolver follows the same structure, regardless of complexity.

## Full Schema

```yaml
resolvers:
  resolverName:
    description: What this resolver produces

    # Phase 1: Resolve - Gather data
    resolve:
      from:
        - provider: cli
          key: keyName
        - provider: env
          key: ENV_VAR
        - provider: git
          field: branch
        - provider: state
          key: previousResolver
        - provider: expression
          expr: _.other + "-value"
        - provider: static
          value: fallback-default

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

Highest priority - user input from command line.

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

### Source Priority

Sources evaluated in order; first non-null wins:

1. CLI input (highest)
2. Environment variables
3. Git metadata
4. State
5. Expression
6. Static (lowest)

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
        key: project
      - provider: static
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
resolvers:
  org:
    resolve:
      from:
        - provider: cli
          key: org

  repo:
    resolve:
      from:
        - provider: cli
          key: repo

  repoUrl:
    description: Derived from org and repo
    resolve:
      from:
        - provider: expression
          expr: "https://github.com/" + _.org + "/" + _.repo + ".git"
```

## Best Practices

1. **Start with CLI input** - User choice is highest priority
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
| `transform.when` | CEL expr | No | Skip transform if false |
| `transform.until` | CEL expr | No | Stop remaining items when true |
| `transform.into` | array | No | Array of transform expressions |
| `transform.into[].expr` | CEL expr | Yes | Expression to execute |
| `transform.into[].when` | CEL expr | No | Skip item if false |
| `validate[]` | array | No | Array of validation rules |
| `validate[].expr` | CEL expr | Yes | Condition that must be true |
| `validate[].message` | string | Yes | Error message if false |
| `validate[].regex` | pattern | No | Shorthand for regex pattern |

## Next Steps

- **Transform details** → [Transform Phase Guide](../guides/03-transform-phase.md)
- **Action schema** → [Action Schema](./action-schema.md)
- **Full resolver guide** → [Resolver Pipeline Guide](../guides/02-resolver-pipeline.md)

---

Resolvers are the foundation of scafctl. They produce the data that everything else uses.
