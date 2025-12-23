# Resolver Pipeline Guide

## Overview

Every resolver follows a **four-phase pipeline**:

```
resolve → transform → validate → emit
```

This deterministic flow ensures data is gathered, normalized, validated, and made available consistently.

## Phase 1: Resolve

The **resolve phase** gathers data from multiple sources. Sources are evaluated in the order they appear in the `from:` array. By default, the first non-null value wins. You can customize this behavior with the `until:` condition.

### Provider Inputs and Go Templates

All provider inputs support **Go templating** via the `{{ }}` syntax. This allows you to reference other resolved values dynamically:

```yaml
spec:
  resolvers:
    apiToken:
      resolve:
        from:
          - provider: env
            inputs:
              key: API_TOKEN

    config:
      resolve:
        from:
          - provider: api
            inputs:
              endpoint: "https://api.example.com/{{ _.environment }}/config"
              method: GET
              headers:
                Authorization: "Bearer {{ _.apiToken }}"
```

Template expressions can access:
- `{{ _.resolverName }}` - Other resolved values
- Standard Go template functions (see `pkg/gotmpl` for details)

### Source Evaluation Order

Sources are tried **in the exact order you define them**. There is no implicit priority by provider type:

```yaml
spec:
  resolvers:
    exampleResolver:
      resolve:
        from:
          - provider: cli              # 1st: Try CLI input first
            inputs:
              key: value_key
          - provider: env              # 2nd: Then environment variable
            inputs:
              key: ENV_VAR_NAME
          - provider: git              # 3rd: Then Git metadata
            inputs:
              field: branch
          - provider: state            # 4th: Then previous state
            inputs:
              key: state_key
          - provider: expression       # 5th: Then CEL expression
            inputs:
              expr: _.someResolver + ".suffix"
          - provider: static           # 6th: Finally static default
            inputs:
              value: default_value
```

You control the order! Rearrange sources to change which is tried first.

### CLI Input Example

```yaml
spec:
  resolvers:
    environment:
      resolve:
        from:
          - provider: cli
            inputs:
              key: env
```

Run with:
```bash
scafctl run solution:myapp -r env=production
```

### Environment Variables

```yaml
spec:
  resolvers:
    apiKey:
      resolve:
        from:
          - provider: env
            inputs:
              key: API_KEY
```

Set before running:
```bash
export API_KEY="secret-123"
scafctl run solution:myapp
```

### Git Metadata

```yaml
spec:
  resolvers:
    gitBranch:
      resolve:
        from:
          - provider: git
            inputs:
              field: branch

    gitTag:
      resolve:
        from:
          - provider: git
            inputs:
              field: tag

    gitCommit:
      resolve:
        from:
          - provider: git
            inputs:
              field: commit
```

### Expressions: Computed Values

Use CEL to derive values from other resolvers:

```yaml
spec:
  resolvers:
    projectName:
      resolve:
        from:
          - provider: cli
            inputs:
              key: project

    version:
      resolve:
        from:
          - provider: cli
            inputs:
              key: version

    imageName:
      resolve:
        from:
          - provider: expression
            inputs:
              expr: _.projectName + ":" + _.version
```

The expression runs **after** its dependencies are resolved. Use `_` to access all resolved values.

### Static Defaults (Fallback Pattern)

```yaml
spec:
  resolvers:
    environment:
      resolve:
        from:
          - provider: cli
            inputs:
              key: env
          - provider: env
            inputs:
              key: ENVIRONMENT
          - provider: static
            inputs:
              value: development
```

This pattern tries CLI first, then environment variable, then falls back to static default. Defaults only run if all previous sources fail or return null.

### Conditional Source Resolution

Use **`when:`** conditions to skip sources based on context, and **`until:`** to stop trying sources early:

#### Skip Sources with `when:`

```yaml
spec:
  resolvers:
    apiEndpoint:
      resolve:
        from:
          - provider: api
            when: _.environment == 'production'
            inputs:
              endpoint: "https://prod-api.example.com/config"
              
          - provider: api
            when: _.environment == 'staging'
            inputs:
              endpoint: "https://staging-api.example.com/config"
              
          - provider: static
            inputs:
              value: "http://localhost:8080"
```

Only the source matching the current environment will execute.

#### Early Exit with `until:`

```yaml
spec:
  resolvers:
    config:
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
              value: "./default-config.yaml"
        until: __self != ""
```

The `until:` condition stops trying sources as soon as a non-empty value is found. This is useful for:
- Performance optimization (skip expensive sources)
- First-valid-value patterns
- Custom resolution logic beyond "first non-null"

#### Combined Conditionals

```yaml
spec:
  resolvers:
    databaseUrl:
      resolve:
        from:
          - provider: vault
            when: _.environment == 'production' && _.useVault == true
            inputs:
              path: "secret/prod/database"
              
          - provider: env
            when: _.environment != 'local'
            inputs:
              key: DATABASE_URL
              
          - provider: static
            inputs:
              value: "postgresql://localhost:5432/dev"
        until: __self.startsWith("postgresql://")
```

This setup:
1. Uses Vault only in production when enabled
2. Uses environment variables in non-local environments
3. Falls back to local default
4. Stops as soon as a valid PostgreSQL URL is found

## Phase 2: Transform

The **transform phase** processes the resolved value. Transforms run sequentially, each receiving the output of the previous one as `__self`.

### Basic Transform

```yaml
spec:
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
spec:
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

### Conditional Validation

Validation rules can be conditionally applied using `when:`:

```yaml
validate:
  - expr: __self.startsWith('https://')
    message: "Production URLs must use HTTPS"
    when: _.environment == 'production'
  - expr: size(__self) <= 100
    message: "URL must be 100 characters or less"
    when: _.environment != 'development'
```

The `when` condition is evaluated first. If it returns `false`, the validation rule is skipped.

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

### Dynamic Error Messages

Validation messages support Go templating for dynamic, context-aware error messages:

```yaml
validate:
  - expr: size(__self) >= 2 && size(__self) <= 50
    message: "Name '{{ __self }}' must be 2-50 characters (got {{ size __self }})"
  - expr: __self.matches("^[a-z0-9-]+$")
    message: "Name '{{ __self }}' contains invalid characters (environment: {{ _.environment }})"
  - expr: !__self.contains(_.blockedWord)
    message: "Cannot use blocked word '{{ _.blockedWord }}' in '{{ __self }}'"
```

The message template has access to:
- `{{ __self }}` - The value being validated
- `{{ _.resolverName }}` - Any resolved resolver values
- Standard Go template functions

This enables precise, contextual error messages that help users understand exactly what went wrong.

## Phase 4: Emit

Once a resolver is resolved, transformed, and validated, its value is **emitted**. It becomes available to:

- Other resolvers (via `_` context)
- Actions (via `_` context)
- Default output (via `scafctl run` with no action)

```yaml
# Resolvers can depend on other resolvers
spec:
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

Resolvers form a **Directed Acyclic Graph (DAG)** based on dependencies. They execute in **phases**, with resolvers in each phase running **concurrently**:

```yaml
spec:
  resolvers:
    # Phase 1: No dependencies - run concurrently
    name:
      resolve:
        from: [...]

    org:
      resolve:
        from: [...]

    # Phase 2: Depends on both name and org - runs after Phase 1 completes
    imageName:
      resolve:
        from:
          - provider: expression
            expr: _.org + "/" + _.name
```

**Execution model:**
1. **Phase 1**: `name` and `org` execute **concurrently** (no dependencies)
2. Wait for Phase 1 to complete
3. **Phase 2**: `imageName` executes (dependencies satisfied)

This phase-based concurrent execution maximizes parallelism while respecting dependencies. Resolvers with the same dependency depth execute together, improving performance for solutions with many independent resolvers.

### Minimal Resolver Execution

When you target specific actions with `--action`, scafctl analyzes dependencies and executes **only the resolvers those actions need**:

```bash
# Execute only resolvers needed by 'deploy' action
scafctl run solution:myapp --action deploy

# Execute resolvers needed by multiple actions
scafctl run solution:myapp --action build --action test
```

**How it works:**
- Static analysis identifies all resolver references in action inputs, conditions, and foreach expressions
- Transitive dependencies are included (if action needs C, C needs B, B needs A → all three execute)
- Action `dependsOn` chains are followed automatically
- Resolvers not needed by target actions are skipped

**Example:**

```yaml
spec:
  resolvers:
    environment:    # Needed by apiUrl
      resolve: [...]
    
    apiUrl:         # Needed by deploy
      resolve:
        from:
          - provider: expression
            inputs:
              expr: _.environment == 'prod' ? 'api.prod.com' : 'api.dev.com'
    
    databaseUrl:    # NOT needed by deploy
      resolve: [...]

  actions:
    deploy:
      provider: api
      inputs:
        endpoint: "https://{{ _.apiUrl }}/deploy"
```

Running `--action deploy` executes only `environment` and `apiUrl`, skipping `databaseUrl`. This significantly improves performance for large solutions.

**Force all resolvers:**
```bash
scafctl run solution:myapp --action deploy --resolve-all
```

## Examples

### Example 1: Git Repository Name

```yaml
repoName:
  description: Extracted and normalized repository name

  resolve:
    from:
      - provider: cli
        inputs:
          key: repo
      - provider: expression
        inputs:
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
        inputs:
          key: env
      - provider: env
        inputs:
          key: TARGET_ENV
      - provider: git
        inputs:
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
        inputs:
          key: version
      - provider: git
        inputs:
          field: tag
      - provider: static
        inputs:
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
