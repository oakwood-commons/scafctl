# Resolver Examples

This directory contains practical examples demonstrating various resolver patterns and use cases.

## Examples

### Basic Examples

| Example | Description |
|---------|-------------|
| [hello-world.yaml](hello-world.yaml) | Simplest possible resolver |
| [parameters.yaml](parameters.yaml) | Using CLI parameters with defaults |
| [dependencies.yaml](dependencies.yaml) | Resolver dependencies and execution phases |
| [plan-aware.yaml](plan-aware.yaml) | Resolvers that adapt behavior based on execution phase |
| [messages.yaml](messages.yaml) | Custom error messages on resolver failure |

### Configuration Patterns

| Example | Description |
|---------|-------------|
| [env-config.yaml](env-config.yaml) | Environment-based configuration |
| [feature-flags.yaml](feature-flags.yaml) | Feature toggle pattern with fallbacks |

### Data Processing

| Example | Description |
|---------|-------------|
| [transform-pipeline.yaml](transform-pipeline.yaml) | Multi-step data transformation |
| [foreach-filter.yaml](foreach-filter.yaml) | ForEach in resolve phase with filter to strip nil results |
| [validation.yaml](validation.yaml) | Input validation patterns |

### CEL Expressions

| Example | Description |
|---------|-------------|
| [cel-basics.yaml](cel-basics.yaml) | Literals, resolver refs, operators, strings, lists |
| [cel-builtins.yaml](cel-builtins.yaml) | Math, encoders, sets, optionals, bindings, string/list ops |
| [cel-common-patterns.yaml](cel-common-patterns.yaml) | Conditionals, map building, filtering, merging, resource generation |
| [cel-extensions.yaml](cel-extensions.yaml) | All custom CEL extension functions (arrays, map, strings, etc.) |
| [cel-transforms.yaml](cel-transforms.yaml) | Data transformation patterns (filter, aggregate, enrich) |

### Go Templates

| Example | Description |
|---------|-------------|
| [go-template-extensions.yaml](go-template-extensions.yaml) | Custom extension functions: toHcl, toYaml, fromYaml |
| [go-template-sprig.yaml](go-template-sprig.yaml) | Sprig template functions (strings, type conversion, etc.) |
| [go-template-ignored-blocks.yaml](go-template-ignored-blocks.yaml) | Using scafctl:ignore blocks to skip template rendering |

### Integration

| Example | Description |
|---------|-------------|
| [secrets.yaml](secrets.yaml) | Secure handling of sensitive values |
| [identity.yaml](identity.yaml) | Accessing authentication identity info |

## Running Examples

~~~bash
# Run resolvers only (for debugging and inspection)
scafctl run resolver -f examples/resolvers/hello-world.yaml

# Run specific resolvers with verbose output
scafctl run resolver greeting --verbose -f examples/resolvers/dependencies.yaml

# Run with parameters
scafctl run resolver -f examples/resolvers/parameters.yaml -r name=Alice

# Show execution progress
scafctl run resolver -f examples/resolvers/dependencies.yaml --progress

# Output as YAML
scafctl run resolver -f examples/resolvers/env-config.yaml -o yaml
~~~

## Creating Your Own

Use these examples as templates for your own solutions. Key things to remember:

1. **Start simple** -- begin with static values and add complexity
2. **Use types** -- explicitly declare types for better error messages
3. **Validate inputs** -- add validation rules for critical values
4. **Handle errors** -- use `onError: continue` for fallbacks
5. **Mark sensitive data** -- use `sensitive: true` to redact secrets in table output (JSON/YAML reveal values for machine consumption; use `--show-sensitive` to reveal in all formats)
