# Resolver Pipeline Guide

## Overview

Every resolver follows a **four-phase pipeline**:

```
resolve → transform → validate → emit
```

This deterministic flow ensures data is gathered, normalized, validated, and made available consistently.

## Phase 1: Resolve

The **resolve phase** gathers data from multiple sources. Sources are evaluated in priority order; the first non-null value wins.

### Source Priority Order

```yaml
resolve:
  from:
    - provider: cli              # 1. User input (highest priority)
    - provider: env              # 2. Environment variables
    - provider: git              # 3. Git repository metadata
    - provider: state            # 4. Previously resolved values
    - provider: expression       # 5. CEL expression over other resolvers
    - provider: static           # 6. Static default (lowest priority)
```

### CLI Input: Highest Priority

```yaml
resolvers:
  environment:
    resolve:
      from:
        - provider: cli
          key: env
```

Run with:
```bash
scafctl run solution:myapp -r env=production
```

### Environment Variables

```yaml
resolvers:
  apiKey:
    resolve:
      from:
        - provider: env
          key: API_KEY
```

Set before running:
```bash
export API_KEY="secret-123"
scafctl run solution:myapp
```

### Git Metadata

```yaml
resolvers:
  gitBranch:
    resolve:
      from:
        - provider: git
          field: branch

  gitTag:
    resolve:
      from:
        - provider: git
          field: tag

  gitCommit:
    resolve:
      from:
        - provider: git
          field: commit
```

### Expressions: Computed Values

Use CEL to derive values from other resolvers:

```yaml
resolvers:
  projectName:
    resolve:
      from:
        - provider: cli
          key: project

  version:
    resolve:
      from:
        - provider: cli
          key: version

  imageName:
    resolve:
      from:
        - provider: expression
          expr: _.projectName + ":" + _.version
```

The expression runs **after** its dependencies are resolved. Use `_` to access all resolved values.

### Static Defaults: Lowest Priority

```yaml
resolvers:
  environment:
    resolve:
      from:
        - provider: cli
          key: env
        - provider: env
          key: ENVIRONMENT
        - provider: static
          value: development
```

Defaults are fallbacks; they're only used if all higher-priority sources are unavailable.

## Phase 2: Transform

The **transform phase** processes the resolved value. Transforms run sequentially, each receiving the output of the previous one as `__self`.

### Basic Transform

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
```

Input: `"MyProject"` → Output: `"myproject"`

### Transform Array: Sequential Processing

```yaml
transform:
  into:
    - expr: __self.toLowerCase()         # Step 1
    - expr: __self.replace('_', '-')     # Step 2
    - expr: "prefix-" + __self           # Step 3
```

Flow: `"My_Project"` → `"my_project"` → `"my-project"` → `"prefix-my-project"`

Each step has access to:
- `__self` - Current value (output of previous step)
- `_` - All resolved resolver values (for accessing other resolvers)

### Context: Accessing Other Resolvers

```yaml
resolvers:
  version:
    resolve:
      from:
        - provider: static
          value: "1.2.3"

  imageName:
    resolve:
      from:
        - provider: static
          value: "myapp"

    transform:
      into:
        - expr: __self + ":" + _.version
```

Output: `"myapp:1.2.3"`

### Conditional Transform: `until:` at Transform Level

Stop remaining transforms once a condition is met:

```yaml
transform:
  until: __self != ""
  into:
    - expr: _.primaryBranch != "" ? _.primaryBranch : __self
    - expr: _.fallbackBranch != "" ? _.fallbackBranch : __self
    - expr: "main"
```

Execution:
1. Try primary branch
2. If empty, try fallback
3. If still empty, use "main"
4. Stop as soon as `until:` condition is true

### Conditional Transform: `when:` at Item Level

Run individual items conditionally:

```yaml
transform:
  into:
    - expr: __self.toUpperCase()
      when: __self == "prod"

    - expr: __self + "-staging"
      when: __self != "prod"

    - expr: __self.replace('_', '-')  # Always runs (no when:)
```

If the `when:` condition is false, that item is skipped and the next item receives the same `__self` value.

### Transform with Type Checking

Use `type(__self)` to handle multiple input formats:

```yaml
transform:
  into:
    # Step 1: Parse JSON strings to objects
    - expr: |
        type(__self) == "string" && __self.startsWith('{') ?
          parseJson(__self) : __self

    # Step 2: Enrich parsed objects
    - expr: |
        type(__self) == "object" ?
          __self + {processed: true} : __self

    # Step 3: Extract URL if we have object
    - expr: |
        type(__self) == "object" ? __self.url : __self
```

Flexibility: Accept user input as either URL string OR JSON config object.

### Transform at Transform Level: `when:`

Apply condition to entire transform phase:

```yaml
transform:
  when: _.isEnabled == true
  into:
    - expr: __self.toLowerCase()
    - expr: __self.replace('_', '-')
```

If `when:` is false at transform level, **all items are skipped** and the resolved value passes through unchanged.

## Phase 3: Validate

The **validate phase** ensures transformed data meets constraints. All validation rules must pass.

### Basic Validation

```yaml
validate:
  - expr: __self.matches("^[a-z0-9-]+$")
    message: "Must be lowercase alphanumeric with hyphens"
  - expr: size(__self) >= 2 && size(__self) <= 50
    message: "Must be 2-50 characters"
  - expr: !__self.startsWith('-') && !__self.endsWith('-')
    message: "Cannot start or end with hyphen"
```

If any rule fails, the resolver fails with the specified error message.

### Regex Validation (Shorthand)

```yaml
validate:
  - regex: "^[a-z0-9-]+$"
    message: "Must be lowercase alphanumeric"
```

Equivalent to: `expr: __self.matches("^[a-z0-9-]+$")`

### Complex Validation

```yaml
validate:
  - expr: |
      (__self.startsWith('https://') || __self.startsWith('git@')) &&
      __self.endsWith('.git')
    message: "Must be valid Git repository URL"
  - expr: !__self.contains('localhost')
    message: "Cannot use localhost URLs"
```

## Phase 4: Emit

Once a resolver is resolved, transformed, and validated, its value is **emitted**. It becomes available to:

- Other resolvers (via `_` context)
- Actions (via `_` context)
- Default output (via `scafctl run` with no action)

```yaml
# Resolvers can depend on other resolvers
resolvers:
  projectName: {...}
  version: {...}

  imageName:
    resolve:
      from:
        - provider: expression
          expr: _.projectName + ":" + _.version  # Uses emitted values
```

## Execution Order

Resolvers form a **Directed Acyclic Graph (DAG)** based on dependencies. They execute in topological order:

```yaml
resolvers:
  name:
    resolve:
      from: [...]

  org:
    resolve:
      from: [...]

  # Depends on both name and org
  imageName:
    resolve:
      from:
        - provider: expression
          expr: _.org + "/" + _.name
```

Execution: `name` and `org` can run in parallel, then `imageName` runs after both complete.

## Examples

### Example 1: Git Repository Name

```yaml
repoName:
  description: Extracted and normalized repository name

  resolve:
    from:
      - provider: cli
        key: repo
      - provider: expression
        expr: _.repoUrl.split('/').last()  # Extract from URL

  transform:
    into:
      - expr: __self.replace('.git', '')
      - expr: __self.replace('_', '-')
      - expr: __self.toLowerCase()

  validate:
    - expr: __self.matches("^[a-z0-9-]+$")
      message: "Invalid repo name format"
```

### Example 2: Environment with Fallback

```yaml
environment:
  resolve:
    from:
      - provider: cli
        key: env
      - provider: env
        key: TARGET_ENV
      - provider: git
        field: branch

  transform:
    into:
      - expr: __self.toLowerCase()

  validate:
    - expr: ["dev", "staging", "prod"].contains(__self)
      message: "Must be dev, staging, or prod"
```

### Example 3: Version with Type Handling

```yaml
version:
  resolve:
    from:
      - provider: cli
        key: version
      - provider: git
        field: tag
      - provider: static
        value: "0.0.0-dev"

  transform:
    into:
      # Remove 'v' prefix if present
      - expr: __self.startsWith('v') ? __self.substring(1) : __self

      # Validate semver pattern
      - expr: __self.matches("^[0-9]+\\.[0-9]+\\.[0-9]+") ? __self : "0.0.0-dev"

  validate:
    - expr: __self.matches("^[0-9]+\\.[0-9]+(\\.[0-9]+)?(-[a-z0-9]+)?$")
      message: "Invalid version format"
```

## Best Practices

1. **Use CLI for user input** - Make it the first source
2. **Use static for defaults** - Make it the last source
3. **Keep transforms simple** - Break complex logic into multiple steps
4. **Validate aggressively** - Fail fast with clear messages
5. **Document via description** - Explain the resolver's purpose
6. **Use expressions for computed values** - Don't duplicate logic
7. **Use transform until: carefully** - It can be surprising if misused
8. **Name resolvers clearly** - `imageName` is better than `img`

## Next Steps

- **Dive into transforms** → [Transform Phase](./03-transform-phase.md)
- **Learn actions** → [Action Orchestration](./04-action-orchestration.md)
- **Expression syntax** → [Expression Language](./05-expression-language.md)

---

For a working example, see `examples/git-repo-normalizer/solution.yaml` in the repository.
