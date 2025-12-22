# Transform Phase Guide

## Overview

The **transform phase** processes a resolved value through a sequence of operations. It's where you normalize, enrich, and prepare data before validation.

## Basic Structure

All transforms use the `into:` array:

```yaml
transform:
  into:
    - expr: first transformation
    - expr: second transformation
    - expr: third transformation
```

Each expression receives `__self` (the previous value) and has access to all resolvers via `_`.

## Sequential Execution

Transforms execute in order, with each step's output becoming the next step's input:

```yaml
transform:
  into:
    - expr: __self.toLowerCase()        # "MyProject" → "myproject"
    - expr: __self.replace('_', '-')    # "my_project" → "my-project"
    - expr: __self + "-service"         # "my-project" → "my-project-service"
```

## Context Variables

### `__self` - Current Value

The value being transformed (output of previous step or initial resolve value):

```yaml
transform:
  into:
    - expr: __self.startsWith('v') ? __self.substring(1) : __self
    - expr: __self.toUpperCase()
    - expr: __self.length > 20 ? __self.substring(0, 20) : __self
```

### `_` - All Resolvers

Access to all resolved resolver values:

```yaml
resolvers:
  version:
    resolve:
      from: [...]

  imageName:
    resolve:
      from:
        - provider: static
          value: "app"

    transform:
      into:
        - expr: __self + ":" + _.version  # Access _.version
        - expr: __self + "_" + _.hash     # Access _.hash
```

## Conditional Transforms

### `when:` at Item Level

Run individual items conditionally:

```yaml
transform:
  into:
    # Only runs if environment is production
    - expr: __self.toUpperCase()
      when: _.environment == "prod"

    # Only runs if environment is NOT production
    - expr: __self + "-dev"
      when: _.environment != "prod"

    # Always runs (no when: condition)
    - expr: __self.replace('_', '-')
```

If a `when:` condition is false, that item is skipped but the next item still receives the same `__self` value.

**Important**: Skipped items don't change the current value, they just don't execute.

### `when:` at Transform Level

Run entire transform phase conditionally:

```yaml
transform:
  when: _.isEnabled == true  # If false, skip ALL items
  into:
    - expr: __self.toLowerCase()
    - expr: __self.replace('_', '-')
```

If transform-level `when:` is false, **no items execute** and the resolved value passes through unchanged.

## Conditional Stopping

### `until:` at Transform Level

Stop processing remaining items once a condition is met:

```yaml
transform:
  until: __self != ""  # Stop when we have a non-empty value
  into:
    - expr: _.primaryValue != "" ? _.primaryValue : __self
    - expr: _.fallbackValue != "" ? _.fallbackValue : __self
    - expr: "default-value"
```

Execution:
1. Run first item
2. Check `until:` condition - if true, stop
3. Otherwise, run second item
4. Check `until:` condition - if true, stop
5. Otherwise, run third item

Use case: **Fallback chains**. Try primary source, then fallback, then default.

### `until:` with Type Checking

Use type checks to stop at appropriate parse state:

```yaml
transform:
  into:
    # Parse string to JSON object if it looks like JSON
    - expr: |
        type(__self) == "string" && __self.startsWith('{') ?
          parseJson(__self) : __self

    # Enrich object with defaults
    - expr: |
        type(__self) == "object" ?
          __self + {processed: true, timestamp: now()}
          : __self

    # Extract final value
    - expr: |
        type(__self) == "object" ? __self.url : __self
```

This accepts:
- String URLs: `"https://..."`
- JSON objects: `{"url": "https://...", "name": "..."}`

Handles both formats correctly.

## Common Patterns

### Pattern 1: Normalization Chain

```yaml
resolvers:
  projectName:
    resolve:
      from:
        - provider: cli
          key: name

    transform:
      into:
        - expr: __self.toLowerCase()
        - expr: __self.replace('_', '-')
        - expr: __self.replace(' ', '-')
```

### Pattern 2: Fallback with Until

```yaml
resolvers:
  branch:
    resolve:
      from:
        - provider: cli
          key: branch
        - provider: static
          value: ""

    transform:
      until: __self != ""
      into:
        - expr: _.userBranch != "" ? _.userBranch : __self
        - expr: _.gitBranch != "" ? _.gitBranch : __self
        - expr: "main"
```

### Pattern 3: Type-Based Processing

```yaml
resolvers:
  config:
    resolve:
      from:
        - provider: cli
          key: config

    transform:
      into:
        # Try parsing as JSON
        - expr: |
            type(__self) == "string" ?
              (try(parseJson(__self)) catch __self) : __self

        # If it's an object, ensure required fields
        - expr: |
            type(__self) == "object" ?
              __self + {validated: true} : __self
```

### Pattern 4: Environment-Specific Transform

```yaml
resolvers:
  deploymentPath:
    resolve:
      from:
        - provider: cli
          key: path

    transform:
      into:
        # Add environment directory
        - expr: __self + "/" + _.environment

        # Only uppercase in production
        - expr: |
            _.environment == "prod" ?
              __self.toUpperCase() : __self
          when: _.environment != "dev"
```

### Pattern 5: Derived Values from Multiple Resolvers

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
    resolve:
      from:
        - provider: expression
          expr: "https://github.com/" + _.org + "/" + _.repo

    transform:
      into:
        - expr: __self + ".git"
```

## Advanced Techniques

### Combining `when:` and `until:`

```yaml
transform:
  when: _.isProduction == true    # Skip entire transform if not prod
  until: __self.length > 20        # In prod, stop when long enough
  into:
    - expr: __self.toUpperCase()
    - expr: __self + "_PROD"
    - expr: __self + "_FINALIZED"
```

### Array Processing with Transform

Parse and enrich array values:

```yaml
transform:
  into:
    # Parse comma-separated string to array
    - expr: |
        type(__self) == "string" ?
          __self.split(',') : __self

    # Filter empty strings
    - expr: |
        [item for item in __self if item.trim() != ""]

    # Trim and lowercase each
    - expr: |
        [item.trim().toLowerCase() for item in __self]
```

### Retry Logic with Transform

```yaml
resolvers:
  data:
    resolve:
      from:
        - provider: api
          endpoint: https://api.example.com/data

    transform:
      until: __self != null && type(__self) == "object"
      into:
        # First attempt: fetch as-is
        - expr: __self

        # Retry: parse if string
        - expr: |
            type(__self) == "string" ?
              parseJson(__self) : __self

        # Fallback: return empty object
        - expr: __self || {}
```

## Performance Considerations

### Early Stopping with `until:`

Reduces unnecessary processing:

```yaml
transform:
  until: __self != "" && size(__self) > 0
  into:
    - expr: _.primarySource != "" ? _.primarySource : ""
    - expr: _.secondarySource != "" ? _.secondarySource : ""
    - expr: _.tertiarySource != "" ? _.tertiarySource : ""
    - expr: "default"  # Never reaches if any source provided
```

### Conditional Skipping with `when:`

Avoids expensive operations unnecessarily:

```yaml
transform:
  into:
    # Skip expensive API call if already valid
    - expr: fetchFromAPI(__self)
      when: !__self.matches("^[a-z0-9-]+$")
```

## Troubleshooting

### Issue: Transform not running

Check:
1. Is `when:` condition true?
2. Has `until:` already stopped execution?
3. Are there syntax errors in CEL expression?

```yaml
transform:
  when: _.enabled == true      # Check this condition
  until: __self != ""           # Check if already satisfied
  into:
    - expr: __self.toLowerCase()  # Check CEL syntax
```

### Issue: Unexpected value after transform

Check:
1. Are items skipped due to `when:` conditions?
2. Did `until:` stop processing?
3. Is the expression using correct operators?

```yaml
# Debug by checking each step
transform:
  into:
    - expr: __self    # Step 1: what's the input?
    - expr: __self.toLowerCase()  # Step 2: what's the output?
```

## Best Practices

1. **Keep expressions simple** - Complex logic should span multiple items
2. **Use `until:` for fallback chains** - Makes intent clear
3. **Use item-level `when:` for logic branches** - Easier to understand than nested ternaries
4. **Validate after transform** - Transform normalizes, validate enforces
5. **Document complex transforms** - Add comments explaining the logic
6. **Test edge cases** - Empty strings, null values, wrong types
7. **Use type checking** - Handle multiple input formats gracefully

## Examples in Repository

See `examples/git-repo-normalizer/solution.yaml` for comprehensive examples:
- Simple normalization transforms
- Conditional stopping with `until:`
- Type-based transforms with type checking
- Item-level `when:` conditions

## Next Steps

- **Learn action execution** → [Action Orchestration](./04-action-orchestration.md)
- **Master expressions** → [Expression Language](./05-expression-language.md)
- **Reference transforms** → [Resolver Schema](../schemas/resolver-schema.md)

---

Transform is the heart of data processing in scafctl. Master it to build powerful, flexible workflows!
