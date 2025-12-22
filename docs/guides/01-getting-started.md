# Getting Started with scafctl

## What is scafctl?

scafctl is a **schema-first execution engine** for building automated workflows. It's designed around a core principle:

> **Nothing runs unless explicitly requested.**

Instead of implicit defaults and magic behavior, scafctl is explicit and deterministic. You declare what data you need (resolvers), how to transform it (transforms), and what side effects you want (actions). Then you run exactly what you ask for.

## Core Concepts

### Resolvers: Data With Purpose

Resolvers produce named values through a **four-phase pipeline**:

```
resolve → transform → validate → emit
```

Each phase serves a specific purpose:

1. **Resolve** - Gather data from multiple sources (CLI, environment, static values, other resolvers)
2. **Transform** - Process and normalize the data
3. **Validate** - Ensure data meets constraints
4. **Emit** - Make it available to other resolvers and actions

Example:

```yaml
resolvers:
  projectName:
    resolve:
      from:
        - provider: cli          # User input (highest priority)
          key: name
        - provider: env          # Environment variable
          key: PROJECT_NAME
        - provider: static       # Default (lowest priority)
          value: "my-app"

    transform:
      into:
        - expr: __self.toLowerCase()

    validate:
      - expr: __self.matches("^[a-z0-9-]+$")
        message: "Must be lowercase alphanumeric with hyphens"

    # Result emitted as _.projectName
```

### Actions: Side Effects on Demand

Actions execute side effects only when explicitly invoked:

```yaml
actions:
  build:
    description: Build the application
    provider: shell
    when: _.version != "0.0.0-dev"  # Skip if dev version
    inputs:
      cmd:
        - "echo Building {{ _.projectName }}"
        - "go build -o bin/{{ _.projectName }}"
```

Use `when:` to make actions conditional, `dependsOn:` to express dependencies, and `foreach:` to iterate.

### Templates: Pure Rendering

Templates render files or text without side effects:

```yaml
templates:
  - name: dockerfile
    source:
      type: inline
      files:
        - path: Dockerfile
          content: |
            FROM golang:1.21
            WORKDIR /app
            RUN go build -o bin/{{ _.projectName }}
```

Or reference templates in resolvers to compute values from rendered output.

## Your First Solution

A solution is a complete workflow declaration. Create `solution.yaml`:

```yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: hello-world
  version: 1.0.0
  displayName: Hello World Example
  description: Simple scafctl workflow
  category: automation

spec:

  # Resolvers produce data
  resolvers:
    greeting:
      description: A greeting message
      resolve:
        from:
          - provider: cli
            key: greeting
          - provider: static
            value: "Hello, World!"

  # Actions execute when requested
  actions:
    say-hello:
      description: Print the greeting
      provider: shell
      inputs:
        cmd:
          - echo {{ _.greeting }}
```

Run it:

```bash
# Use the default greeting
scafctl run solution:hello-world

# Use custom input
scafctl run solution:hello-world -r greeting="Hello, scafctl!"

# See what would happen without executing
scafctl run solution:hello-world --dry-run
```

### Force a Fresh Run

scafctl caches provider initialization and resolver evaluations during a single process run to keep repeat commands fast. When you want to recompute everything from scratch, add `--no-cache` to bypass those caches:

```bash
scafctl run solution:hello-world --no-cache
```

This flag applies to every command, including `run`, `test`, and `build`.

## Resolver Pipeline Deep Dive

### Resolve Phase

The resolve phase gathers data from multiple sources. Sources are evaluated in order; the first non-null value wins:

```yaml
resolve:
  from:
    - provider: cli           # 1. User input (scafctl run -r key=value)
      key: myValue
    - provider: env           # 2. Environment variable
      key: MY_VALUE
    - provider: git           # 3. Git metadata
      field: branch
    - provider: expression    # 4. CEL expression over other resolvers
      expr: _.otherResolver + "-suffix"
    - provider: static        # 5. Default fallback
      value: "default"
```

### Transform Phase

Transform processes the resolved value through an array of operations. Each step receives `__self` (previous value) and has access to all resolvers via `_`:

```yaml
transform:
  into:
    - expr: __self.toLowerCase()
    - expr: __self.replace('_', '-')
    - expr: __self + "-" + _.version
```

Stop early with `until:` at the transform level:

```yaml
transform:
  until: __self != ""  # Stop if we have a non-empty value
  into:
    - expr: _.primaryValue != "" ? _.primaryValue : __self
    - expr: _.fallbackValue != "" ? _.fallbackValue : __self
    - expr: "default-value"
```

Or make individual items conditional:

```yaml
transform:
  into:
    - expr: __self.toUpperCase()
      when: __self == "prod"
    - expr: __self + "-dev"
      when: __self != "prod"
```

### Validate Phase

Validation ensures data meets constraints. All rules must pass:

```yaml
validate:
  - expr: __self.matches("^[a-z0-9-]+$")
    message: "Must be lowercase alphanumeric with hyphens"
  - expr: size(__self) >= 2 && size(__self) <= 50
    message: "Must be 2-50 characters"
```

### Emit Phase

Once resolved, transformed, and validated, the value is emitted and available to all other resolvers and actions as `_.resolverName`.

## Expression Languages

scafctl uses two languages strategically:

### CEL: Logic & Data

Use **CEL** for logic, conditions, type checking, and data operations:

```yaml
# In resolve
- provider: expression
  expr: _.projectName + ":" + _.version

# In validate
- expr: size(__self) >= 2 && !__self.startsWith('-')

# In action conditions
when: _.environment != "dev"

# In transform conditionals
when: type(__self) == "string"
```

### Go Templates: Text Rendering

Use **templating** in text fields for path generation and command strings:

```yaml
# In provider inputs (templating)
cmd:
  - "echo Building {{ _.projectName }}"
  - "docker build -t {{ _.registry }}/{{ _.imageName }} ."

# In paths (templating)
path: ./config/{{ _.environment }}/settings.yaml

# In messages (templating)
message: "Deployed {{ _.service }} version {{ _.version }}"
```

## Common Patterns

### Fallback Chain

Use transform with `until:` to implement fallback logic:

```yaml
resolvers:
  branchName:
    resolve:
      from:
        - provider: cli
          key: branch
        - provider: static
          value: ""

    transform:
      until: __self != ""
      into:
        - expr: _.userBranch
        - expr: _.gitBranch
        - expr: "main"
```

### Type-Based Processing

Check types to accept multiple input formats:

```yaml
transform:
  into:
    - expr: type(__self) == "string" ? parseJson(__self) : __self
      when: __self.startsWith('{')
    - expr: type(__self) == "object" ? __self.url : __self
```

### Conditional Actions

Use `when:` and `dependsOn:` to build DAGs:

```yaml
actions:
  test:
    provider: shell
    inputs:
      cmd: [go, test, ./...]

  build:
    provider: shell
    dependsOn: [test]
    when: _.buildEnabled == true
    inputs:
      cmd: [go, build, -o, bin/app]

  deploy:
    provider: api
    dependsOn: [build]
    when: _.environment == "prod"
    inputs:
      endpoint: https://api.example.com/deploy
      method: POST
      body: '{"image": "{{ _.imageName }}"}'
```

## Next Steps

- **Deep dive into resolvers** → [Resolver Pipeline](./02-resolver-pipeline.md)
- **Master transforms** → [Transform Phase](./03-transform-phase.md)
- **Build complex workflows** → [Action Orchestration](./04-action-orchestration.md)
- **Understand expressions** → [Expression Language](./05-expression-language.md)
- **Look up providers** → [Providers Guide](./06-providers.md)

---

Ready to build? Check out `/examples` for complete, runnable solutions!
