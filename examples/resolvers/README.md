# Resolver Examples

This directory contains practical examples demonstrating various resolver patterns and use cases.

## Examples

### Basic Examples

| Example | Description |
|---------|-------------|
| [hello-world.yaml](hello-world.yaml) | Simplest possible resolver |
| [parameters.yaml](parameters.yaml) | Using CLI parameters with defaults |
| [dependencies.yaml](dependencies.yaml) | Resolver dependencies and execution phases |

### Configuration Patterns

| Example | Description |
|---------|-------------|
| [env-config.yaml](env-config.yaml) | Environment-based configuration |
| [feature-flags.yaml](feature-flags.yaml) | Feature toggle pattern with fallbacks |
| [multi-env.yaml](multi-env.yaml) | Multi-environment configuration |

### Data Processing

| Example | Description |
|---------|-------------|
| [transform-pipeline.yaml](transform-pipeline.yaml) | Multi-step data transformation |
| [foreach-filter.yaml](foreach-filter.yaml) | ForEach in resolve phase with `filter: true` to strip nil results |
| [validation.yaml](validation.yaml) | Input validation patterns |

### CEL Expressions

| Example | Description |
|---------|-------------|
| [cel-basics.yaml](cel-basics.yaml) | Literals, resolver refs, operators, strings, lists |
| [cel-builtins.yaml](cel-builtins.yaml) | Math, encoders, sets, optionals, bindings, string/list ops |
| [cel-common-patterns.yaml](cel-common-patterns.yaml) | Conditionals, map building, filtering, merging, resource generation |
| [cel-extensions.yaml](cel-extensions.yaml) | All custom CEL extension functions (arrays, map, strings, etc.) |
| [cel-transforms.yaml](cel-transforms.yaml) | Data transformation patterns (filter, aggregate, enrich) |

### Integration

| Example | Description |
|---------|-------------|
| [http-api.yaml](http-api.yaml) | Fetching data from HTTP APIs |
| [secrets.yaml](secrets.yaml) | Secure handling of sensitive values |
| [identity.yaml](identity.yaml) | Accessing authentication identity info |

## Running Examples

```bash
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

# Run resolver-only solution
scafctl run resolver -f examples/resolvers/hello-world.yaml
```

## Creating Your Own

Use these examples as templates for your own solutions. Key things to remember:

1. **Start simple** - Begin with static values and add complexity
2. **Use types** - Explicitly declare types for better error messages
3. **Validate inputs** - Add validation rules for critical values
4. **Handle errors** - Use `error_behavior: continue` for fallbacks
5. **Mark sensitive data** - Use `sensitive: true` to redact secrets in table output (JSON/YAML reveal values for machine consumption; use `--show-sensitive` to reveal in all formats)
