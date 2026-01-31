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
| [validation.yaml](validation.yaml) | Input validation patterns |

### Integration

| Example | Description |
|---------|-------------|
| [http-api.yaml](http-api.yaml) | Fetching data from HTTP APIs |
| [secrets.yaml](secrets.yaml) | Secure handling of sensitive values |
| [identity.yaml](identity.yaml) | Accessing authentication identity info |

## Running Examples

```bash
# Run an example
scafctl run solution -f examples/resolvers/hello-world.yaml

# Run with parameters
scafctl run solution -f examples/resolvers/parameters.yaml -r name=Alice

# Show execution progress
scafctl run solution -f examples/resolvers/dependencies.yaml --progress

# Output as YAML
scafctl run solution -f examples/resolvers/env-config.yaml -o yaml
```

## Creating Your Own

Use these examples as templates for your own solutions. Key things to remember:

1. **Start simple** - Begin with static values and add complexity
2. **Use types** - Explicitly declare types for better error messages
3. **Validate inputs** - Add validation rules for critical values
4. **Handle errors** - Use `error_behavior: continue` for fallbacks
5. **Mark sensitive data** - Use `sensitive: true` to redact secrets from logs
