# Examples: Actions

This directory contains example action configurations demonstrating the Actions feature in scafctl.

## Examples

| Example | Description |
|---------|-------------|
| [hello-world.yaml](hello-world.yaml) | Simplest possible action workflow |
| [sequential-chain.yaml](sequential-chain.yaml) | Linear action dependency chain (A → B → C) |
| [parallel-with-deps.yaml](parallel-with-deps.yaml) | Diamond pattern with parallel execution |
| [action-alias.yaml](action-alias.yaml) | Action aliases for shorter expression references |
| [foreach-deploy.yaml](foreach-deploy.yaml) | ForEach expansion for deploying to multiple targets |
| [exclusive-actions.yaml](exclusive-actions.yaml) | Mutual exclusion for actions that share a resource |
| [error-handling.yaml](error-handling.yaml) | Error handling with onError: continue |
| [retry-backoff.yaml](retry-backoff.yaml) | Retry with exponential backoff |
| [conditional-retry.yaml](conditional-retry.yaml) | Conditional retry with retryIf expressions |
| [conditional-execution.yaml](conditional-execution.yaml) | When conditions for conditional execution |
| [finally-cleanup.yaml](finally-cleanup.yaml) | Finally section for cleanup actions |
| [complex-workflow.yaml](complex-workflow.yaml) | Full CI/CD-style workflow with all features |
| [template-render.yaml](template-render.yaml) | Real-world: read template, render with go-template, write output |

## Running Examples

### Render Mode (Generate Artifact)

```bash
# Render to JSON (default)
scafctl render solution -f examples/actions/hello-world.yaml

# Render to YAML
scafctl render solution -f examples/actions/hello-world.yaml -o yaml

# Render to file
scafctl render solution -f examples/actions/hello-world.yaml -o json > output.json
```

### Run Mode (Direct Execution)

```bash
# Run the solution (shows progress with --progress flag)
scafctl run solution -f examples/actions/hello-world.yaml

# Run with progress output
scafctl run solution -f examples/actions/hello-world.yaml --progress

# Run with JSON output (for scripts/pipelines)
scafctl run solution -f examples/actions/hello-world.yaml -o json

# Dry-run (show what would execute)
scafctl run solution -f examples/actions/hello-world.yaml --dry-run

# Override resolver values
scafctl run solution -f examples/actions/foreach-deploy.yaml -r targets='["server1","server2"]'

# Run resolvers only (for debugging)
scafctl run resolver -f examples/actions/hello-world.yaml
```

## Concepts Demonstrated

### Dependencies

Actions can declare dependencies using `dependsOn`:

```yaml
workflow:
  actions:
    build:
      provider: exec
      inputs:
        command: "go build ./..."
    
    test:
      provider: exec
      dependsOn: [build]
      inputs:
        command: "go test ./..."
```

### ForEach Expansion

Actions can iterate over arrays:

```yaml
workflow:
  actions:
    deploy:
      provider: exec
      forEach:
        in:
          expr: "_.targets"
      inputs:
        command:
          expr: "'kubectl apply -f deployment-' + __item + '.yaml'"
```

### Conditions

Actions can be conditional:

```yaml
workflow:
  actions:
    prod-deploy:
      provider: exec
      when:
        expr: "_.environment == 'prod'"
      inputs:
        command: "deploy.sh"
```

### Error Handling

Actions can continue on failure:

```yaml
workflow:
  actions:
    optional-step:
      provider: exec
      onError: continue
      inputs:
        command: "optional-command.sh"
```

### Retry

Actions can retry on failure:

```yaml
workflow:
  actions:
    flaky-api-call:
      provider: http
      retry:
        maxAttempts: 3
        backoff: exponential
        initialDelay: 1s
        maxDelay: 30s
      inputs:
        url: "https://api.example.com/flaky"
```

### Timeout

Actions can have timeouts:

```yaml
workflow:
  actions:
    slow-operation:
      provider: exec
      timeout: 5m
      inputs:
        command: "long-running-script.sh"
```

### Finally Section

Cleanup actions that always run:

```yaml
workflow:
  actions:
    main-work:
      provider: exec
      inputs:
        command: "work.sh"
  
  finally:
    cleanup:
      provider: exec
      inputs:
        command: "cleanup.sh"
```

### Action Data Flow

Actions can access results from previous actions:

```yaml
workflow:
  actions:
    build:
      provider: exec
      inputs:
        command: "go build -o app"
    
    deploy:
      provider: exec
      dependsOn: [build]
      inputs:
        artifact:
          expr: "__actions.build.results.output"
```
