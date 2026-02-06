# Actions Tutorial

This tutorial introduces the Actions feature in scafctl, which enables executing side-effect operations as a declarative action graph.

## Overview

Actions are the execution phase of a scafctl solution. While **resolvers** compute and gather data, **actions** perform work based on that data.

```
┌─────────────┐     ┌─────────────┐     ┌─────────────┐
│  Resolvers  │ ──► │   Actions   │ ──► │   Results   │
│ (compute)   │     │  (execute)  │     │  (outputs)  │
└─────────────┘     └─────────────┘     └─────────────┘
```

**Key Principles:**
- Resolvers compute data, actions perform work
- All resolvers evaluate before any action executes
- Actions can depend on other actions
- Actions can access results from completed actions

## Quick Start

### 1. Hello World

The simplest action workflow:

```yaml
apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: hello-actions
  version: 1.0.0
  description: My first action workflow

spec:
  # Resolvers provide data for actions
  resolvers:
    greeting:
      type: string
      resolve:
        with:
          - provider: static
            inputs:
              value: "Hello, World!"

  # Workflow defines action execution
  workflow:
    actions:
      greet:
        provider: exec
        inputs:
          command:
            expr: "'echo ' + _.greeting"
```

Run it:
```bash
scafctl run solution -f hello.yaml
```

### 2. Dependencies

Actions can depend on other actions:

```yaml
workflow:
  actions:
    build:
      provider: exec
      inputs:
        command: "go build ./..."
    
    test:
      provider: exec
      dependsOn: [build]  # test runs AFTER build
      inputs:
        command: "go test ./..."
    
    deploy:
      provider: exec
      dependsOn: [test]   # deploy runs AFTER test
      inputs:
        command: "deploy.sh"
```

This creates a linear chain: `build → test → deploy`

### 3. Parallel Execution

Actions without dependencies (or with the same dependencies) run in parallel:

```yaml
workflow:
  actions:
    init:
      provider: exec
      inputs:
        command: "init.sh"
    
    build:
      provider: exec
      dependsOn: [init]
      inputs:
        command: "build.sh"
    
    test:
      provider: exec
      dependsOn: [init]    # Same dependency as build
      inputs:
        command: "test.sh"  # Runs in PARALLEL with build
    
    deploy:
      provider: exec
      dependsOn: [build, test]  # Waits for BOTH
      inputs:
        command: "deploy.sh"
```

This creates a diamond pattern:
```
    init
    /  \
 build  test  ← run in parallel
    \  /
   deploy
```

## Core Concepts

### Providers

Actions use providers to execute operations. Providers must have the `action` capability.

Built-in providers with action capability:
- `shell` - Execute shell commands
- `http` - Make HTTP requests
- `file` - File operations
- `git` - Git operations
- `sleep` - Delay execution

```yaml
actions:
  api-call:
    provider: http
    inputs:
      method: POST
      url: "https://api.example.com/deploy"
      body:
        version:
          expr: "_.version"
```

### Inputs

Inputs are passed to the provider. They support:

**Literal values:**
```yaml
inputs:
  command: "echo hello"
  count: 42
  enabled: true
```

**Resolver references:**
```yaml
inputs:
  environment:
    rslvr: environment  # References resolver named "environment"
```

**CEL expressions:**
```yaml
inputs:
  url:
    expr: "'https://api.example.com/' + _.endpoint"
```

**Go templates:**
```yaml
inputs:
  message:
    tmpl: "Deploying {{ ._.project }} to {{ ._.environment }}"
```

### Action Results

Actions can access results from previous actions using `__actions`:

```yaml
actions:
  build:
    provider: exec
    inputs:
      command: "go build -o app"
  
  deploy:
    provider: exec
    dependsOn: [build]
    inputs:
      # Access build action's results
      artifact:
        expr: "__actions.build.results.output"
```

**Available in `__actions.<name>`:**
- `results` - The action's output data
- `status` - Execution status (succeeded, failed, skipped)
- `inputs` - Resolved input values
- `error` - Error message (if failed)

### Conditions (`when`)

Actions can be conditional:

```yaml
actions:
  prod-deploy:
    provider: exec
    when:
      expr: "_.environment == 'prod'"
    inputs:
      command: "prod-deploy.sh"
```

If the condition evaluates to `false`, the action is skipped with `SkipReasonCondition`.

### ForEach

Execute an action for each item in an array:

```yaml
resolvers:
  servers:
    type: array
    resolve:
      with:
        - provider: static
          inputs:
            value: ["web1", "web2", "web3"]

workflow:
  actions:
    deploy:
      provider: exec
      forEach:
        in:
          expr: "_.servers"
      inputs:
        server:
          expr: "__item"   # Current array element
        index:
          expr: "__index"  # Current index (0, 1, 2, ...)
        command:
          expr: "'deploy.sh ' + __item"
```

The action expands to: `deploy[0]`, `deploy[1]`, `deploy[2]`

**ForEach options:**
```yaml
forEach:
  in:
    expr: "_.servers"
  item: server      # Alias for __item
  index: i          # Alias for __index
  concurrency: 2    # Max parallel iterations
  onError: continue # Continue on iteration failure
```

### Error Handling

**Default behavior (`fail`):** Stop workflow on failure
```yaml
actions:
  critical:
    provider: exec
    onError: fail  # Default - stops workflow if this fails
```

**Continue on failure:**
```yaml
actions:
  optional:
    provider: exec
    onError: continue  # Workflow continues even if this fails
```

**With ForEach:**
```yaml
actions:
  deploy:
    provider: exec
    forEach:
      in:
        expr: "_.servers"
      onError: continue  # Continue deploying to other servers
```

### Retry

Automatic retry with backoff:

```yaml
actions:
  flaky-api:
    provider: http
    retry:
      maxAttempts: 5
      backoff: exponential  # fixed, linear, or exponential
      initialDelay: 1s
      maxDelay: 30s
    inputs:
      url: "https://flaky-api.example.com"
```

**Backoff strategies:**
- `fixed` - Constant delay (e.g., always 1s)
- `linear` - Delay increases linearly (1s, 2s, 3s, ...)
- `exponential` - Delay doubles (1s, 2s, 4s, 8s, ...)

### Timeout

Limit action execution time:

```yaml
actions:
  slow-operation:
    provider: exec
    timeout: 5m  # Fails after 5 minutes
    inputs:
      command: "long-running-script.sh"
```

### Finally Section

Cleanup actions that **always run**, even if main actions fail:

```yaml
workflow:
  actions:
    create-resources:
      provider: exec
      inputs:
        command: "create-test-db.sh"
    
    run-tests:
      provider: exec
      dependsOn: [create-resources]
      inputs:
        command: "run-tests.sh"  # Might fail

  finally:
    cleanup:
      provider: exec
      inputs:
        command: "cleanup.sh"  # ALWAYS runs
    
    notify:
      provider: exec
      dependsOn: [cleanup]
      inputs:
        command: "notify.sh"
```

**Finally rules:**
- Finally actions have implicit dependency on all main actions
- Finally actions can only depend on other finally actions
- ForEach is not allowed in finally section

## Running Solutions

### Execute Mode

Run a solution (resolvers + actions):

```bash
# Basic execution
scafctl run solution -f my-solution.yaml

# Run with progress output
scafctl run solution -f my-solution.yaml --progress

# JSON output (for scripts/pipelines)
scafctl run solution -f my-solution.yaml -o json

# Override resolver values
scafctl run solution -f my-solution.yaml -r environment=prod

# Limit parallel actions
scafctl run solution -f my-solution.yaml --max-action-concurrency=4

# Dry-run (show plan without executing)
scafctl run solution -f my-solution.yaml --dry-run

# Run resolvers only (skip actions)
scafctl run solution -f my-solution.yaml --skip-actions
```

### Render Mode

Generate an executor-ready artifact:

```bash
# Render to JSON (default)
scafctl render solution -f my-solution.yaml

# Render to YAML
scafctl render solution -f my-solution.yaml -o yaml

# Write to file
scafctl render solution -f my-solution.yaml -o json > graph.json
```

The rendered output includes:
- Expanded actions (ForEach iterations)
- Materialized inputs
- Execution phases
- Dependency graph

## Best Practices

### 1. Keep Actions Focused

Each action should do one thing well:

```yaml
# Good: separate concerns
actions:
  build:
    provider: exec
    inputs:
      command: "go build"
  
  test:
    provider: exec
    inputs:
      command: "go test"

# Avoid: combining unrelated operations
actions:
  build-and-test:
    provider: exec
    inputs:
      command: "go build && go test && other-stuff"
```

### 2. Use Meaningful Names

```yaml
# Good
actions:
  deploy-to-staging:
    ...
  run-integration-tests:
    ...

# Avoid
actions:
  step1:
    ...
  step2:
    ...
```

### 3. Set Appropriate Timeouts

```yaml
actions:
  quick-check:
    timeout: 30s
  
  build:
    timeout: 15m
  
  e2e-tests:
    timeout: 1h
```

### 4. Use OnError Wisely

```yaml
actions:
  # Critical operations should fail fast
  database-migration:
    onError: fail
  
  # Optional operations can continue
  send-notification:
    onError: continue
```

### 5. Leverage ForEach for Parallelism

```yaml
actions:
  deploy:
    forEach:
      in:
        expr: "_.targets"
      concurrency: 3  # Deploy to 3 targets at once
```

### 6. Use Finally for Cleanup

```yaml
workflow:
  actions:
    create-test-env:
      ...
  
  finally:
    destroy-test-env:
      ...  # Always clean up
```

## Examples

See the [examples/actions/](../examples/actions/) directory for complete working examples:

- `hello-world.yaml` - Simplest action workflow
- `sequential-chain.yaml` - Linear dependencies
- `parallel-with-deps.yaml` - Diamond pattern
- `foreach-deploy.yaml` - Multi-target deployment
- `error-handling.yaml` - OnError continue
- `retry-backoff.yaml` - Retry strategies
- `conditional-execution.yaml` - When conditions
- `finally-cleanup.yaml` - Cleanup actions
- `complex-workflow.yaml` - Full CI/CD example

## Troubleshooting

### Action Skipped

Check:
1. `when` condition evaluated to false
2. A dependency failed (see `SkipReasonDependencyFailed`)

### Timeout Exceeded

The action took longer than its timeout. Consider:
1. Increasing the timeout
2. Breaking into smaller actions
3. Checking for hanging processes

### Retry Exhausted

All retry attempts failed. Check:
1. The underlying service availability
2. Network connectivity
3. Consider increasing `maxAttempts`

### Cycle Detected

Actions have circular dependencies. Check `dependsOn` references:
```yaml
# Invalid: circular dependency
actions:
  a:
    dependsOn: [b]
  b:
    dependsOn: [a]  # Cycle!
```

## Next Steps

- Browse the [design documentation](design/actions.md)
- Explore [provider documentation](design/providers.md)
- Check out [CEL integration](design/cel-integration.md)
