# Expression Language Guide

## Overview

scafctl uses **two expression languages strategically**:

| Language | Purpose | Context | Syntax |
|----------|---------|---------|--------|
| **CEL** | Logic, conditions, data | `expr:`, `when:`, `validate:` | `_.name + ":v" + _.version` |
| **Go Templates** | Text rendering, paths | `cmd:`, `path:`, `message:` | `{{ _.name }}:v{{ _.version }}` |

This dual-language approach separates concerns:
- CEL handles logic and control flow
- Templating handles text generation

## When to Use CEL

### 1. Expressions (`expr:`)

Use CEL in `expr:` fields for logic:

```yaml
resolve:
  from:
    - provider: expression
      expr: _.projectName + ":" + _.version
    - provider: expression
      expr: _.gitBranch.startsWith("main") ? "stable" : "dev"
    - provider: expression
      expr: type(__self) == "object" ? __self.url : __self
```

### 2. Conditions (`when:`)

Use CEL in `when:` for conditional execution:

```yaml
# At transform level
transform:
  when: _.enabled == true
  into:
    - expr: __self.toLowerCase()

# At action level
actions:
  deploy:
    when: _.environment == "prod" && _.approved == true
    provider: api
    inputs:
      endpoint: https://deploy.example.com/trigger
      method: POST

# At item level
transform:
  into:
    - expr: __self.toUpperCase()
      when: _.environment == "prod"
```

### 3. Validation (`validate:`)

Use CEL in `validate:` for constraints:

```yaml
validate:
  - expr: __self.matches("^[a-z0-9-]+$")
    message: "Must be lowercase alphanumeric"
  - expr: size(__self) >= 2 && size(__self) <= 50
    message: "Must be 2-50 characters"
  - expr: !__self.startsWith('-') && !__self.endsWith('-')
    message: "Cannot start or end with hyphen"
```

### 4. Filtering (`until:`)

Use CEL in `until:` for conditional stopping:

```yaml
transform:
  until: __self != "" && size(__self) > 0
  into:
    - expr: _.primaryValue != "" ? _.primaryValue : ""
    - expr: _.fallbackValue != "" ? _.fallbackValue : ""
    - expr: "default-value"
```

### 5. Arrays (`dependsOn:`)

Use CEL for action dependencies:

```yaml
actions:
  deploy:
    dependsOn: [build, test, validate]
    provider: api
    inputs:
      endpoint: https://deploy.example.com
      method: POST
```

## When to Use Templating

### 1. Commands (`cmd:`)

Use templating in shell commands:

```yaml
actions:
  build:
    provider: shell
    inputs:
      cmd:
        - "echo Building {{ _.projectName }}"
        - "go build -o bin/{{ _.projectName }} ./cmd/main.go"
```

### 2. Paths

Use templating in any path field:

```yaml
inputs:
  path: ./config/{{ _.environment }}/settings.yaml
  sourcePath: ./templates/{{ _.region }}/dockerfile
  destinationPath: ./build/{{ _.version }}/output
```

### 3. Messages

Use templating in display strings:

```yaml
message: "Deployed {{ _.service }} version {{ _.version }} to {{ _.environment }}"
subject: "Build {{ _.buildId }} completed"
body: "Service {{ _.name }} is now running on {{ _.hostname }}"
```

### 4. Endpoints

Use templating in URLs:

```yaml
inputs:
  endpoint: https://{{ _.region }}.api.example.com/v1/deploy
  url: https://github.com/{{ _.org }}/{{ _.repo }}/releases
  webhook: https://hooks.example.com/services/{{ _.teamId }}/{{ _.channelId }}
```

### 5. Branch Names

Use templating for branch creation:

```yaml
outputs:
  branch: "release/v{{ _.version }}"
  tag: "v{{ _.version }}-{{ _.buildNumber }}"
```

## Reserved Keywords: Always CEL

These fields **always use CEL**, never templating:

```yaml
# Conditional execution (always CEL)
when: _.environment != "dev"

# Expression evaluation (always CEL)
expr: _.name + "-" + _.version

# Array iteration (always CEL)
foreach:
  over: _.environments      # Always CEL
  as: __item

# Validation (always CEL)
validate:
  - expr: __self.matches("^[a-z]+$")

# Dependencies (always CEL)
dependsOn: [action1, action2]

# Projection output (always CEL)
expressions:
  - _.resolved1
  - _.resolved2
```

## Field Name Convention

Field names **guide** the default language:

| Suffix | Default Language | Examples |
|--------|------------------|----------|
| `*path` | Templating | `path`, `sourcePath`, `destinationPath` |
| `*message` | Templating | `message`, `subject`, `body` |
| `*endpoint` | Templating | `endpoint`, `url`, `webhook` |
| `*command` | Templating | `command`, `cmd` |
| `*branch` | Templating | `branch`, `tag` |
| `expr` | CEL | `expr` |
| `when` | CEL | `when` |

When uncertain about a custom field, use **templating** (it's the default).

## CEL Basics

### Simple Values

```yaml
expr: _.projectName
expr: "literal-string"
expr: 123
expr: true
```

### String Concatenation

```yaml
expr: _.org + "/" + _.repo
expr: "v" + _.version
expr: _.name + "-" + _.environment
```

### Type Checking

```yaml
expr: type(__self) == "string"
expr: type(__self) == "object"
expr: type(__self) == "array"
expr: type(__self) != "null"
```

### Conditionals (Ternary)

```yaml
expr: _.environment == "prod" ? "stable" : "dev"
expr: __self.startsWith("v") ? __self.substring(1) : __self
expr: size(__self) > 20 ? __self.substring(0, 20) : __self
```

### Comparisons

```yaml
expr: _.version == "1.2.3"
expr: _.count > 10
expr: _.size >= 5 && _.size <= 50
expr: _.name != "unknown"
expr: _.status in ["active", "pending"]
```

### String Methods

```yaml
expr: __self.toLowerCase()
expr: __self.toUpperCase()
expr: __self.replace('_', '-')
expr: __self.split(',')
expr: __self.startsWith('v')
expr: __self.endsWith('.git')
expr: __self.contains('github')
expr: __self.substring(0, 5)
expr: __self.trim()
expr: __self.matches("^[a-z]+$")
```

### Array Methods

```yaml
expr: size(__self)
expr: __self[0]
expr: __self.contains(item)
expr: [item for item in __self if item != ""]
```

### JSON Parsing

```yaml
expr: parseJson(__self)
expr: __self | toJson
```

## Go Template Basics

### Variable Substitution

```yaml
cmd:
  - "echo {{ _.projectName }}"
  - "docker build -t {{ _.registry }}/{{ _.imageName }} ."
path: ./config/{{ _.environment }}/app.yaml
message: "Deployed {{ _.service }} to {{ _.region }}"
```

### Conditionals

```yaml
message: |
  {{ if eq _.environment "prod" }}
    Production deployment
  {{ else }}
    Development deployment
  {{ end }}
```

### Loops

```yaml
cmd:
  - |
    {{- range _.services }}
    echo "Starting {{ . }}"
    {{ end -}}
```

### Functions

```yaml
message: "Total items: {{ len _.items }}"
branch: "{{ lower _.feature }}-{{ .version }}"
```

## Mixing Both Languages

Often you need both:

```yaml
actions:
  deploy:
    # CEL condition (logic)
    when: _.deployEnabled == true && _.version != "0.0.0-dev"

    # CEL foreach (iteration)
    forEach:
      over: _.deploymentTargets
      as: __target

    # Templating in command (text rendering)
    provider: shell
    inputs:
      cmd:
        - "echo Deploying to {{ __target.region }}"
        - "deploy-service --name {{ _.serviceName }} --region {{ __target.region }}"
```

## Common Mistakes

### Mistake 1: CEL in Templating Field

❌ **Wrong**: CEL expression in path
```yaml
path: ./config/{{ _.environment == "prod" ? "prod" : "dev" }}/settings.yaml
```

✅ **Correct**: Use CEL computed value
```yaml
resolvers:
  configDir:
    resolve:
      from:
        - provider: expression
          expr: _.environment == "prod" ? "prod" : "dev"

actions:
  setup:
    inputs:
      path: ./config/{{ _.configDir }}/settings.yaml
```

### Mistake 2: Templating in Expression

❌ **Wrong**: Templating in expr
```yaml
expr: __self == "{{ _.expected }}"
```

✅ **Correct**: Use CEL variable access
```yaml
expr: __self == _.expected
```

### Mistake 3: Forgetting `{{ }}` in Templates

❌ **Wrong**: No templating delimiters
```yaml
cmd:
  - "echo _.projectName"  # Prints literal "_.projectName"
```

✅ **Correct**: Add templating delimiters
```yaml
cmd:
  - "echo {{ _.projectName }}"  # Prints value
```

### Mistake 4: Complex Logic in Templates

❌ **Wrong**: Complex logic in templating
```yaml
message: |
  {{ if and (eq _.env "prod") (gt _.version "1.0") }}
    Ready for prod
  {{ else }}
    Not ready
  {{ end }}
```

✅ **Correct**: Use CEL-computed resolver
```yaml
resolvers:
  isReadyForProd:
    resolve:
      from:
        - provider: expression
          expr: _.environment == "prod" && _.version > "1.0"

actions:
  deploy:
    when: _.isReadyForProd == true
    inputs:
      cmd: [deploy-to-prod]
```

## Context Variables

### `_` - All Resolvers

Access any resolver by name:

```yaml
expr: _.projectName
expr: _.version
expr: _.buildEnabled
```

Access nested objects:

```yaml
expr: _.config.database.host
expr: _.environment.GOARCH
```

### `__self` - Current Transform Value

In transforms and validation:

```yaml
transform:
  into:
    - expr: __self.toLowerCase()
    - expr: __self.replace('_', '-')

validate:
  - expr: __self.matches("^[a-z]+$")
```

### `__item` and Custom Foreach Aliases

In foreach iteration:

```yaml
actions:
  deploy:
    forEach:
      over: _.regions
      as: __region
    inputs:
      cmd:
        - "deploy-to-{{ __region }}"
        - "verify-deployment-{{ __region }}"
```

- Omit `as:` to use the default `__item` alias.
- Provide an alias starting with `__` (for example `__region`) when you need a clearer name.

## Best Practices

1. **Use CEL for logic** - Cleaner, more powerful
2. **Use templating for text** - Simpler, more readable
3. **Keep expressions simple** - Break complex logic into multiple resolvers
4. **Validate early** - Check data types and formats
5. **Use meaningful names** - `isProduction` better than `isProd`
6. **Comment complex expressions** - Explain non-obvious logic
7. **Test edge cases** - Empty strings, null values, type mismatches
8. **Prefer CEL functions** - Use `matches()` over regex patterns
9. **Use ternaries sparingly** - Multiple levels are hard to read
10. **Leverage type checking** - Handle multiple input formats gracefully

## Expression Reference

### CEL Operators

| Operator | Example | Result |
|----------|---------|--------|
| `+` | `"a" + "b"` | `"ab"` |
| `-` | `5 - 3` | `2` |
| `*` | `4 * 3` | `12` |
| `/` | `10 / 2` | `5` |
| `%` | `10 % 3` | `1` |
| `==` | `"a" == "a"` | `true` |
| `!=` | `"a" != "b"` | `true` |
| `<` | `3 < 5` | `true` |
| `<=` | `3 <= 3` | `true` |
| `>` | `5 > 3` | `true` |
| `>=` | `5 >= 5` | `true` |
| `&&` | `true && false` | `false` |
| `\|\|` | `true \|\| false` | `true` |
| `!` | `!true` | `false` |
| `?:` | `a ? b : c` | `b` or `c` |
| `[]` | `arr[0]` | First element |
| `.` | `obj.field` | Field value |

### CEL Functions

| Function | Example | Returns |
|----------|---------|---------|
| `size()` | `size("hello")` | `5` |
| `type()` | `type(__self)` | `"string"` |
| `startsWith()` | `"hello".startsWith("he")` | `true` |
| `endsWith()` | `"hello".endsWith("lo")` | `true` |
| `contains()` | `"hello".contains("ell")` | `true` |
| `matches()` | `"test123".matches("^[a-z]+[0-9]$")` | `true` |
| `toLowerCase()` | `"Hello".toLowerCase()` | `"hello"` |
| `toUpperCase()` | `"Hello".toUpperCase()` | `"HELLO"` |
| `substring()` | `"hello".substring(1, 4)` | `"ell"` |
| `split()` | `"a,b,c".split(",")` | `["a", "b", "c"]` |
| `replace()` | `"hello".replace("l", "L")` | `"heLLo"` |
| `trim()` | `"  hello  ".trim()` | `"hello"` |
| `parseJson()` | `parseJson('{"a":1}')` | `{a: 1}` |
| `toJson()` | `{a: 1}.toJson()` | `'{"a":1}'` |

## Next Steps

- **Learn providers** → [Providers Guide](./06-providers.md)
- **Deep dive into CEL** → [CEL Functions Reference](../reference/cel-functions.md)
- **Template functions** → [Template Functions Reference](../reference/template-functions.md)

---

Mastering both languages unlocks powerful, flexible workflows in scafctl!
