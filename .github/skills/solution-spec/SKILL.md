---
name: solution-spec
description: "Solution YAML specification reference for scafctl. Schema, resolver phases, action workflow, ValueRef format, DAG resolution, and testing. Use when working on solution loading, parsing, execution, or spec types."
---

# Solution Spec Reference

## Top-Level Structure

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: my-solution          # Required, DNS-safe
  version: 1.0.0             # Semver
  displayName: My Solution
  description: Short description
  category: infrastructure
  tags: [go, cloud]
  maintainers:
    - name: Team
      email: team@example.com
  links:
    - name: Docs
      url: https://example.com
  icon: https://example.com/icon.png
  banner: https://example.com/banner.png
catalog:
  visibility: public         # public | private | internal
  beta: false
  disabled: false
spec:
  resolvers: {}              # map[string]*Resolver
  workflow: {}               # Optional action workflow
  testing: {}                # Optional test suite
```

## Resolver Structure

Resolvers are the DAG nodes. Map keys are resolver names (DNS-safe).

```yaml
spec:
  resolvers:
    my-resolver:
      description: What this resolver does
      type: string           # string|int|float|bool|array|object|time|duration|any
      sensitive: false       # Redact from logs/output
      when: "_.some_flag"    # CEL condition -- skip if false
      dependsOn: [other]     # Explicit deps (auto-extracted from expressions too)
      timeout: 30s
      example: "sample"

      # Phase 1: Resolve -- get the value
      resolve:
        - provider: parameter  # Provider name
          with:                # Provider inputs (ValueRef format)
            prompt: "Enter name"
            default: "world"
        - provider: env        # Fallback chain -- first non-null wins
          with:
            name: MY_ENV_VAR
          until: "_ != ''"     # Stop condition (CEL)

      # Phase 2: Transform -- reshape the value
      transform:
        - provider: cel
          with:
            expression: "_.upperAscii()"
        - provider: gotmpl
          with:
            template: "prefix-{{.}}"

      # Phase 3: Validate -- check the value
      validate:
        - provider: validation
          with:
            rules:
              - expr: "size(_) > 0"
                message: "Must not be empty"

      messages:
        error: "Failed to resolve {{.name}}"  # Go template
```

### Phase Execution Order

1. **Resolve**: Providers execute in order. First non-null result wins (`until:` controls early stop).
2. **Transform**: Applied sequentially. `__self` is the current value.
3. **Validate**: All rules checked. Resolver fails if any validation fails.

### Dependency Resolution (DAG)

Dependencies are extracted automatically from:
- CEL expressions: `_.resolverName` references
- Go templates: `{{.resolverName}}` references
- Explicit `dependsOn` list

The executor builds a DAG, topologically sorts into phases, then executes phases sequentially with concurrent execution within each phase.

## ValueRef Format

Used everywhere a value can be dynamic (resolver inputs, action inputs, messages):

| Format | YAML | When to Use |
|--------|------|-------------|
| Literal | `key: value` | Static values |
| Resolver | `key: {rslvr: resolver-name}` | Reference another resolver's output |
| CEL | `key: {expr: "_.field.upperAscii()"}` | Dynamic computation |
| Go Template | `key: {tmpl: "Hello {{.name}}"}` | Text rendering with template logic |

### Decision Guide

- **Literal**: When the value is known at authoring time
- **Resolver ref** (`rslvr`): When you need another resolver's raw output
- **CEL** (`expr`): Data manipulation, conditionals, type coercion, list/map operations
- **Go Template** (`tmpl`): Text rendering, multi-line output, file content generation

## Action Workflow

Actions execute after all resolvers complete. They form their own DAG.

```yaml
spec:
  workflow:
    resultSchemaMode: error   # error | warn | ignore
    actions:
      write-config:
        provider: directory
        inputs:
          source: {rslvr: templates-dir}
          destination: {expr: "_.output_path"}
        dependsOn: []
        when: "_.enabled"         # CEL condition
        onError: fail             # fail | continue
        timeout: 30s
        exclusive: [other-write]  # Mutual exclusion
        retry:
          maxAttempts: 3
          backoff: exponential
        forEach:
          source: "_.items"       # CEL expression returning array
          item: __item            # Variable name for current item
```

### Action-Specific Context

- `__actions`: Map of completed action results (keyed by action name)
- Each action result has: `success` (bool), plus provider-specific fields

## Testing

Solutions support functional tests via a separate `tests.yaml` composed into the solution:

```yaml
# tests.yaml
apiVersion: scafctl.io/v1
kind: Tests
tests:
  - name: basic-test
    command: run solution
    args: [-f, ./cldctl/solution.yaml]
    inputs:
      resolver-name: test-value
    assertions:
      - expr: "__output.resolver_name == 'expected'"
    files:
      - path: output/file.txt
        contains: "expected content"
    exitCode: 0
```

Compose into solution: `compose: [tests.yaml]`

## Built-in Providers

| Provider | Capability | Purpose |
|----------|-----------|---------|
| parameter | from | Interactive user prompts |
| env | from | Environment variables |
| static | from | Hardcoded values |
| file | from | Read file contents |
| exec | from | Run external commands |
| shell | from | Shell command execution |
| http | from | HTTP requests |
| git | from | Git operations |
| github | from | GitHub API |
| secret | from | Secret management |
| identity | from | Auth identity tokens |
| metadata | from | Solution metadata access |
| solution | from | Cross-solution references |
| cel | transform | CEL expression evaluation |
| gotmpl | transform | Go template rendering |
| hcl | transform | HCL format conversion |
| validation | validation | Rule-based validation |
| message | action | User-facing messages |
| directory | action | Directory/file operations |
| debug | from | Debug output |
| sleep | from | Delay execution |

## Key Types (pkg/spec/)

- `Solution`: Top-level with APIVersion, Kind, Metadata, Catalog, Spec
- `Resolver`: Name, Type, Phases (Resolve/Transform/Validate), When, DependsOn
- `ValueRef`: Literal | Resolver | Expr | Tmpl
- `Workflow`: Actions map, ResultSchemaMode
- `Action`: Provider, Inputs, DependsOn, When, OnError, Retry, ForEach

## Key Packages

- `pkg/spec/`: YAML types and parsing
- `pkg/solution/`: Solution loading and execution orchestration
- `pkg/resolver/`: DAG building, phase execution, dependency extraction
- `pkg/action/`: Action workflow execution
- `pkg/provider/`: Provider interface and registry
