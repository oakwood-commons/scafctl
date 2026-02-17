---
title: "Actions"
weight: 4
---

# Actions

## Purpose

Actions describe side effects as a declarative execution graph. They exist to model what should be done, not how data is derived.

Actions consume resolved data, declare dependencies, and reference results from other actions in a structured way. Actions may be executed directly by scafctl or rendered for execution by another system.

Resolvers compute data.
Actions perform work.

---

## Responsibilities

An action is responsible for:

- Declaring an executable operation
- Selecting a provider
- Declaring dependencies on other actions
- Consuming resolver values and action results
- Defining execution conditions

An action is not responsible for:

- Resolving or transforming data
- Mutating resolver values
- Performing implicit execution
- Managing shared state

---

## Action Graph

Actions form a directed acyclic graph.

Each action node contains:

- A provider
- Inputs
- Optional results
- Optional conditions
- Explicit dependencies

---

## Commands and Modes

Actions support two top-level commands.

### run

Executes the action graph directly.

~~~bash
scafctl run solution myapp
~~~

Behavior:

- Resolve all resolvers
- Evaluate all CEL and templates needed to render actions
- Execute actions in dependency order
- Perform side effects

### render

Renders a fully resolved action graph without executing any action providers.

~~~bash
scafctl render solution myapp --output=json
scafctl render solution myapp --output=yaml
~~~

Behavior:

- Resolve all resolvers
- Evaluate all CEL and templates needed to render actions
- Emit an executor-ready action graph artifact (JSON by default, YAML optional)
- No action providers are executed
- No action results are produced at render time

Render produces an artifact, not new runtime data. Output files use `.json` or `.yaml` extensions.

---

## Action Definition

Actions are defined under `spec.workflow`, which contains two sections: `actions` for main execution and `finally` for cleanup.

~~~yaml
spec:
  workflow:
    actions:
      deploy:
        provider: api
        inputs:
          endpoint: https://api.example.com/deploy

    finally:
      cleanup:
        provider: exec
        inputs:
          command: "rm -rf /tmp/build-artifacts"
~~~

Each action declares exactly one provider execution.

### Full Action Schema

~~~yaml
spec:
  workflow:
    actions:
      <actionName>:
        # Metadata
        description: "Human-readable description of what this action does"
        displayName: "Deploy Application"
        sensitive: false  # Whether results should be redacted in table output

        # Provider
        provider: api

        # Inputs (supports literal, rslvr, expr, tmpl)
        inputs:
          endpoint: https://api.example.com/deploy

        # Dependencies
        dependsOn: [build, test]

        # Conditional execution
        when:
          expr: _.environment == "prod"

        # Error handling
        onError: fail  # fail | continue

        # Timeout
        timeout: 30s

        # Retry configuration
        retry:
          maxAttempts: 3
          backoff: exponential  # fixed | linear | exponential
          initialDelay: 1s
          maxDelay: 30s

        # Iteration (not available in finally section)
        forEach:
          item: region
          index: i
          in:
            rslvr: regions
          concurrency: 5
          onError: continue  # fail | continue (default: fail)
~~~

Action names must match the pattern `^[a-zA-Z_][a-zA-Z0-9_-]*$`. Names starting with `__` are reserved for internal use.

**Note:** The `forEach` field is only available in `workflow.actions`, not in `workflow.finally`. Cleanup actions in the finally section cannot use iteration.

---

## Provider Inputs

Action inputs are materialized by scafctl before action execution, or before graph emission in render mode.

Supported input forms:

### Literal

~~~yaml
inputs:
  retries: 3
~~~

### Resolver Binding

~~~yaml
inputs:
  image:
    rslvr: image
~~~

### Expression

~~~yaml
inputs:
  tag:
    expr: _.version + "-stable"
~~~

### Template

~~~yaml
inputs:
  path:
    tmpl: "./config/{{ _.environment }}/app.yaml"
~~~

Providers never see CEL, templates, or resolver references. Providers receive concrete values.

---

## Dependencies

Actions declare dependencies explicitly using `dependsOn`.

~~~yaml
workflow:
  actions:
    build:
      provider: exec

    deploy:
      dependsOn: [build]
      provider: api
~~~

Rules:

- Dependencies form a DAG
- An action runs only after all dependencies complete (success or failure depending on `onError`)
- Cycles are rejected at validation time

---

## Results

Actions expose results implicitly from provider output. The provider's `Output.Data` becomes the action's results automatically.

~~~yaml
workflow:
  actions:
    fetchConfig:
      provider: api
      inputs:
        endpoint: https://api.example.com/config
      # Results are implicitly Output.Data from the provider execution
~~~

Results are available to dependent actions via the `__actions` namespace.

---

## Consuming Results from Dependencies

Actions consume results from dependencies using expressions or templates that reference the `__actions` namespace.

### Using expressions

~~~yaml
workflow:
  actions:
    fetchConfig:
      provider: api
      inputs:
        endpoint: https://api.example.com/config

    deploy:
      dependsOn: [fetchConfig]
      provider: api
      inputs:
        # Reference entire results object
        body:
          expr: __actions.fetchConfig.results
        # Reference nested field
        timeout:
          expr: __actions.fetchConfig.results.config.timeout
        # Combine with resolver data
        message:
          expr: '"Deploying to " + _.environment + " with config v" + string(__actions.fetchConfig.results.version)'
~~~

### Using templates

~~~yaml
inputs:
  body:
    tmpl: "Config value: {{ .__actions.fetchConfig.results.configKey }}"
~~~

### Rules

- `__actions.<name>` may only reference declared dependencies
- Referencing a non-dependency is a validation error
- In render mode, expressions referencing `__actions` are preserved as deferred expressions
- External executors must be CEL-capable to evaluate deferred expressions

---

## The `__actions` Namespace

After action execution, results and metadata are available in the `__actions` namespace:

~~~yaml
__actions:
  <actionName>:
    inputs: <materialized inputs passed to provider>
    results: <Output.Data from provider>
    status: succeeded | failed | skipped | timeout | cancelled
    skipReason: condition | dependency-failed  # Only present when status is skipped
    startTime: "2026-01-29T10:00:00Z"
    endTime: "2026-01-29T10:00:05Z"
    error: "error message"  # See Error Field section below
~~~

### Error Field

The `error` field presence depends on the action's status:

| Status | `error` field |
|--------|---------------|
| `succeeded` | Not present |
| `failed` | **Required** - contains the error message |
| `timeout` | **Required** - contains timeout details |
| `skipped` | Not present |
| `cancelled` | Optional - may contain cancellation reason if available |

### Inputs Field

The `inputs` field contains the fully materialized inputs that were passed to the provider. This is useful for debugging and for `finally` actions that need to understand what values were used:

~~~yaml
# Given this action definition:
workflow:
  actions:
    deploy:
      provider: api
      inputs:
        endpoint:
          tmpl: "https://{{ _.region }}.example.com/deploy"
        body:
          expr: '{"image": _.image}'

# The __actions namespace will contain:
__actions:
  deploy:
    inputs:
      endpoint: "https://us-east.example.com/deploy"
      body: {"image": "nginx:1.27"}
    results: { ... }
    status: succeeded
~~~

### Skip Reason

When an action has `status: skipped`, the `skipReason` field indicates why:

| `skipReason` | Description |
|--------------|-------------|
| `condition` | The `when` expression evaluated to false |
| `dependency-failed` | A dependency failed with `onError: fail` |

### Status Values

| Status | Description |
|--------|-------------|
| `pending` | Action is waiting for dependencies to complete |
| `running` | Action is currently executing |
| `succeeded` | Action completed successfully |
| `failed` | Action failed due to an error |
| `skipped` | Action was skipped (see `skipReason` for details) |
| `timeout` | Action exceeded its configured timeout |
| `cancelled` | Action was cancelled before or during execution |

The `pending` and `running` statuses are transient and only observable during execution (e.g., via progress callbacks or real-time monitoring). In the final `__actions` namespace after execution completes, only terminal statuses appear.

This namespace is available in:

- CEL expressions (`expr`)
- Go templates (`tmpl`)
- `when` conditions of dependent actions

---

## Conditions

Actions may be conditionally enabled using `when`.

~~~yaml
workflow:
  actions:
    deploy:
      when:
        expr: _.environment == "prod"
      provider: api
~~~

Conditions can also reference action results from dependencies:

~~~yaml
workflow:
  actions:
    test:
      provider: exec
      inputs:
        command: "npm test"

    deploy:
      dependsOn: [test]
      when:
        expr: __actions.test.status == "succeeded"
      provider: api
~~~

### Expression Evaluation Timing

Expressions and templates are evaluated at different times depending on what they reference:

| References | Evaluation Time | Rendered Output |
|------------|-----------------|----------------|
| Only `_` (resolver data) | Render time | Concrete value (e.g., `when: true`) |
| `__actions` (action results) | Runtime | Preserved expression (deferred) |
| Mixed `_` and `__actions` | Runtime | Preserved expression (deferred) |

Examples:

~~~yaml
# Evaluated at render time → becomes: when: true
when:
  expr: _.environment == "prod"

# Deferred to runtime (references action results)
when:
  expr: __actions.test.status == "succeeded"

# Deferred to runtime (mixed references)
inputs:
  message:
    expr: '"Deploying " + _.appName + " (test: " + __actions.test.status + ")"'

# Combined condition (deferred - references __actions)
when:
  expr: _.environment == "prod" && __actions.test.status == "succeeded"
~~~

In render mode, deferred expressions are preserved in the output with a `deferred: true` marker:

~~~json
{
  "when": {
    "expr": "__actions.test.status == \"succeeded\"",
    "deferred": true
  }
}
~~~

Behavior:

- `when.expr` is evaluated during render (resolver values only) or at runtime (if referencing action results)
- The rendered action includes a boolean condition or deferred expression
- In run mode, scafctl skips actions whose condition is false
- In render mode, the emitted graph includes evaluated values for resolver-only conditions, and preserves expressions for runtime evaluation

---

## Error Handling

Actions support `onError` to control behavior on failure.

~~~yaml
actions:
  notify:
    provider: slack
    onError: continue  # Don't fail the whole graph if Slack is down
    inputs:
      message: "Starting deployment"

  deploy:
    provider: api
    onError: fail  # Default: stop everything if this fails
    inputs:
      endpoint: https://api.example.com/deploy
~~~

| `onError` Value | Behavior |
|-----------------|----------|
| `fail` (default) | Stop entire graph execution immediately |
| `continue` | Mark action as failed, continue executing remaining actions |

When `onError: continue`:

- The action is marked as failed in `__actions.<name>.status`
- All remaining actions continue to execute
- Dependent actions are responsible for checking `__actions.<dependency>.status` and handling failures appropriately
- The `__actions.<name>.error` field contains the error message
- `finally` actions always have access to `__actions.<name>.error` regardless of `onError` setting

---

## Timeout

Actions support individual timeouts.

~~~yaml
actions:
  deploy:
    provider: api
    timeout: 30s
    inputs:
      endpoint: https://api.example.com/deploy
~~~

If an action exceeds its timeout, it fails with a timeout error. The default timeout is inherited from global configuration.

---

## Retry Configuration

Actions support retry policies for transient failures.

~~~yaml
actions:
  deploy:
    provider: api
    retry:
      maxAttempts: 3
      backoff: exponential
      initialDelay: 1s
      maxDelay: 30s
    inputs:
      endpoint: https://api.example.com/deploy
~~~

### Retry Fields

| Field | Description | Default |
|-------|-------------|---------|
| `maxAttempts` | Maximum number of attempts (including initial) | 1 (no retry) |
| `backoff` | Backoff strategy: `fixed`, `linear`, `exponential` | `fixed` |
| `initialDelay` | Delay before first retry | `1s` |
| `maxDelay` | Maximum delay between retries | `30s` |
### Retry and Timeout Interaction

If an action times out, **no further retries are attempted**. A timeout is treated as a terminal failure. The action is marked with `status: timeout` and execution moves on.

Example worst-case timing for a successful retry scenario:
```yaml
timeout: 30s
retry:
  maxAttempts: 3
  initialDelay: 5s
  backoff: exponential
```
- Attempt 1: fails at 10s → wait 5s
- Attempt 2: fails at 15s → wait 10s  
- Attempt 3: succeeds at 20s
- Total: 60s

If attempt 2 times out (30s), the action fails immediately with `status: timeout`.
### Backoff Strategies

- **fixed**: Always wait `initialDelay` between attempts
- **linear**: Delay increases by `initialDelay` each attempt (1s, 2s, 3s, ...)
- **exponential**: Delay doubles each attempt (1s, 2s, 4s, ...) up to `maxDelay`

Providers can declare `retryable: false` in their descriptor if retries are never appropriate (e.g., destructive operations).

---

## Sensitive Actions

Actions can be marked as sensitive to control result visibility.

~~~yaml
actions:
  getSecret:
    provider: vault
    sensitive: true
    inputs:
      path: secret/data/api-key
~~~

When `sensitive: true`:

- Results are redacted in table/interactive output (human-facing)
- JSON and YAML output reveals values for machine consumption (Terraform model)
- Use `--show-sensitive` to reveal values in all output formats
- The value is still available to dependent actions
- Error messages are sanitized to prevent leaking sensitive data (e.g., secrets in request bodies)
- The `__actions.<name>.error` field contains a sanitized error message

---

## Iteration

Actions may be expanded declaratively using `forEach`.

~~~yaml
workflow:
  actions:
    deploy:
      forEach:
        item: region
        index: i
        in:
          rslvr: regions
        concurrency: 5
        onError: continue
      provider: api
      inputs:
        endpoint:
          tmpl: "https://{{ .region.api }}/deploy"
~~~

### ForEach Fields

| Field | Description | Default |
|-------|-------------|---------|
| `item` | Variable name for current element | `__item` |
| `index` | Variable name for current index | `__index` |
| `in` | Array to iterate (ValueRef: literal, rslvr, expr, tmpl) | Required |
| `concurrency` | Max parallel iterations (0 = unlimited) | 0 |
| `onError` | Error handling: `fail` or `continue` | `fail` |

### ForEach Error Handling

When `onError: continue` (explicit):
- All iterations execute regardless of individual failures
- Failed iterations are marked with `status: failed`
- Results from successful iterations are still available

When `onError: fail` (default):
- Execution stops after the first iteration fails
- Pending iterations are marked with `status: cancelled`
- Already-running iterations continue to completion (no mid-execution cancellation)

### Expansion Behavior

- Iteration is expanded during render
- Produces multiple action nodes with index-based naming: `deploy[0]`, `deploy[1]`, `deploy[2]`, ...
- Each iteration is independent
- All expanded actions inherit the original action's `dependsOn`
- Dependents of the original action depend on **all** expanded instances (waits for all to complete before starting)
- Action names containing `[` or `]` are reserved and rejected at validation time to prevent naming collisions

### ForEach Dependency Expansion Example

When an action with `forEach` has dependents, those dependents wait for all expanded instances:

~~~yaml
# Original definition:
workflow:
  actions:
    deploy:
      forEach:
        in:
          rslvr: regions  # ["us-east", "us-west"]
      provider: api

    notify:
      dependsOn: [deploy]  # Depends on the forEach action
      provider: slack

# Rendered graph (simplified):
workflow:
  actions:
    deploy[0]:
      provider: api
    deploy[1]:
      provider: api
    notify:
      dependsOn: [deploy[0], deploy[1]]  # Expanded to all instances
      provider: slack
~~~

The `notify` action will only start after both `deploy[0]` and `deploy[1]` complete.

### Accessing ForEach Results

ForEach results are accessible both individually and as an aggregate:

~~~yaml
# Individual iteration results (by expanded action name)
__actions["deploy[0]"].results   # First iteration result
__actions["deploy[1]"].results   # Second iteration result
__actions["deploy[0]"].status    # Status of first iteration

# Aggregate results (array of all iteration results)
__actions.deploy.results          # [result0, result1, result2, ...]
__actions.deploy.iterations       # Full iteration metadata (see below)
~~~

The `iterations` field provides detailed metadata for each expansion:

~~~yaml
__actions.deploy.iterations:
  - index: 0
    name: "deploy[0]"
    results: { ... }      # Output.Data from this iteration
    status: succeeded
    startTime: "2026-01-29T10:00:00Z"
    endTime: "2026-01-29T10:00:05Z"
  - index: 1
    name: "deploy[1]"
    results: { ... }
    status: failed
    error: "connection timeout"
    # ...
~~~

This enables filtering and aggregation in expressions:

~~~yaml
# Count failed iterations
expr: __actions.deploy.iterations.filter(i, i.status == "failed").size()

# Get all successful results
expr: __actions.deploy.iterations.filter(i, i.status == "succeeded").map(i, i.results)
~~~

### Variables Available in ForEach

During forEach iteration, the following variables are available in expressions and templates:

~~~yaml
# Given: regions = ["us-east", "us-west", "eu-central"]
# For iteration index 1:

__item: "us-west"      # Current element (always available)
__index: 1             # Current 0-based index (always available)
region: "us-west"      # Custom alias (from item: region)
i: 1                   # Custom alias (from index: i)
~~~

The built-in `__item` and `__index` variables are always available regardless of custom aliases. Custom aliases (`item` and `index` fields) provide more readable names for use in expressions and templates.

Example using built-in variables:

~~~yaml
forEach:
  in:
    rslvr: servers
  # Using defaults: __item and __index
provider: exec
inputs:
  command:
    expr: '"deploy to " + __item.hostname + " (" + string(__index) + ")"'
~~~

---

## Finally Actions

Cleanup actions are defined in the `workflow.finally` section, separate from regular actions for clear visual separation between main execution and cleanup.

~~~yaml
spec:
  workflow:
    actions:
      build:
        provider: exec
        inputs:
          command: "make build"

      deploy:
        dependsOn: [build]
        provider: api
        inputs:
          endpoint: https://api.example.com/deploy

    finally:
      cleanup:
        provider: exec
        inputs:
          command: "rm -rf /tmp/build-artifacts"

      notify:
        dependsOn: [cleanup]  # Can depend on other finally actions
        provider: slack
        inputs:
          message:
            expr: '"Build " + (__actions.deploy.status == "succeeded" ? "succeeded" : "failed")'
~~~

### Finally Behavior

- Finally actions run after **all** regular actions complete (success, failure, or skip)
- Finally actions have access to `__actions` results from all regular actions, including failed ones
- Finally actions can declare `dependsOn` other finally actions (for ordering within the finally phase)
- Finally actions **cannot** `dependsOn` regular actions (they implicitly wait for all regular actions)
- Finally actions can reference `__actions.<regularAction>.results` and `__actions.<regularAction>.status` in expressions
- `forEach` is not available in the finally section (enforced at validation time)
- Finally actions do not block regular actions

### Cross-Section References

Finally actions can read from regular actions but cannot depend on them:

~~~yaml
spec:
  workflow:
    actions:
      deploy:
        provider: api
        inputs:
          endpoint: https://api.example.com/deploy

    finally:
      report:
        # ✅ Valid: Reference results/status via expressions
        provider: slack
        inputs:
          message:
            expr: '"Deploy status: " + __actions.deploy.status'
          details:
            expr: __actions.deploy.error  # Available if deploy failed

      cleanup:
        # ❌ Invalid: Cannot dependsOn regular actions
        # dependsOn: [deploy]  # This would be a validation error
        provider: exec
        inputs:
          command: "rm -rf /tmp/build"
~~~

### Execution Order

1. All regular actions (in `workflow.actions`) execute according to their DAG
2. Once all regular actions complete (success, failure, or skip), finally actions begin
3. Finally actions execute in their own dependency order within the finally section

---

## Progress Callbacks

Action execution supports progress callbacks for real-time feedback during execution. This enables progress bars, live logging, and monitoring integrations.

### Callback Events

| Event | Description |
|-------|-------------|
| `OnActionStart` | Fired when an action begins execution |
| `OnActionComplete` | Fired when an action completes successfully |
| `OnActionFailed` | Fired when an action fails (includes error) |
| `OnActionSkipped` | Fired when an action is skipped (includes skip reason) |
| `OnActionTimeout` | Fired when an action times out |
| `OnActionCancelled` | Fired when an action is cancelled |
| `OnRetryAttempt` | Fired before a retry attempt (includes attempt number) |
| `OnForEachProgress` | Fired as forEach iterations complete (includes completed/total counts) |
| `OnPhaseStart` | Fired when a new execution phase begins |
| `OnPhaseComplete` | Fired when an execution phase completes |
| `OnFinallyStart` | Fired when the finally section begins |
| `OnFinallyComplete` | Fired when the finally section completes |

### Callback Interface

~~~go
// ProgressCallback receives execution progress events for actions.
type ProgressCallback interface {
    OnActionStart(actionName string)
    OnActionComplete(actionName string, results any)
    OnActionFailed(actionName string, err error)
    OnActionSkipped(actionName string, reason string)
    OnActionTimeout(actionName string, timeout time.Duration)
    OnActionCancelled(actionName string)
    OnRetryAttempt(actionName string, attempt int, maxAttempts int, err error)
    OnForEachProgress(actionName string, completed int, total int)
    OnPhaseStart(phase int, actionNames []string)
    OnPhaseComplete(phase int)
    OnFinallyStart()
    OnFinallyComplete()
}
~~~

Progress callbacks are optional and do not affect execution semantics.

---

## Rendered Graph Shape

After rendering, scafctl emits a graph that contains only concrete inputs and explicit references.

~~~json
{
  "apiVersion": "scafctl.oakwood-commons.github.io/v1alpha1",
  "kind": "ActionGraph",
  "executionOrder": [
    ["fetchConfig"],
    ["deploy"],
    ["deploy-regions[0]", "deploy-regions[1]"]
  ],
  "finallyOrder": [
    ["cleanup"]
  ],
  "actions": {
    "fetchConfig": {
      "provider": "api",
      "inputs": {
        "endpoint": "https://api.example.com/config"
      },
      "onError": "fail",
      "timeout": "30s"
    },
    "deploy": {
      "provider": "api",
      "dependsOn": ["fetchConfig"],
      "when": {
        "expr": "__actions.fetchConfig.status == \"succeeded\"",
        "deferred": true
      },
      "onError": "fail",
      "timeout": "60s",
      "inputs": {
        "body": {
          "expr": "__actions.fetchConfig.results",
          "deferred": true
        }
      }
    },
    "cleanup": {
      "provider": "shell",
      "section": "finally",
      "inputs": {
        "command": "rm -rf /tmp/build"
      }
    },
    "deploy-regions[0]": {
      "provider": "api",
      "dependsOn": ["deploy"],
      "inputs": {
        "endpoint": "https://us-east.example.com/deploy",
        "region": "us-east"
      },
      "forEach": {
        "expandedFrom": "deploy-regions",
        "index": 0
      }
    },
    "deploy-regions[1]": {
      "provider": "api",
      "dependsOn": ["deploy"],
      "inputs": {
        "endpoint": "https://us-west.example.com/deploy",
        "region": "us-west"
      },
      "forEach": {
        "expandedFrom": "deploy-regions",
        "index": 1
      }
    }
  }
}
~~~

The same graph in YAML format (`--output=yaml`):

~~~yaml
apiVersion: scafctl.oakwood-commons.github.io/v1alpha1
kind: ActionGraph
executionOrder:
  - [fetchConfig]
  - [deploy]
  - [deploy-regions[0], deploy-regions[1]]
finallyOrder:
  - [cleanup]
actions:
  fetchConfig:
    provider: api
    inputs:
      endpoint: https://api.example.com/config
    onError: fail
    timeout: 30s

  deploy:
    provider: api
    dependsOn: [fetchConfig]
    when:
      expr: __actions.fetchConfig.status == "succeeded"
      deferred: true
    onError: fail
    timeout: 60s
    inputs:
      body:
        expr: __actions.fetchConfig.results
        deferred: true

  cleanup:
    provider: exec
    section: finally
    inputs:
      command: rm -rf /tmp/build

  deploy-regions[0]:
    provider: api
    dependsOn: [deploy]
    inputs:
      endpoint: https://us-east.example.com/deploy
      region: us-east
    forEach:
      expandedFrom: deploy-regions
      index: 0

  deploy-regions[1]:
    provider: api
    dependsOn: [deploy]
    inputs:
      endpoint: https://us-west.example.com/deploy
      region: us-west
    forEach:
      expandedFrom: deploy-regions
      index: 1
~~~

### Rendered Graph Notes

- CEL expressions and templates referencing only resolver data are evaluated to concrete values
- Expressions referencing `__actions` are preserved as deferred expressions for runtime evaluation
- `when` conditions based on resolver values are evaluated to booleans
- `when` conditions referencing `__actions` are preserved as deferred expressions
- `forEach` actions are expanded to individual action nodes
- External executors must be CEL-capable to evaluate deferred expressions
- No runtime action result values exist until execution time
- `executionOrder` is an array of phases, where each phase is an array of action names that can execute concurrently
- `finallyOrder` is a separate array of phases for finally actions
- Finally actions include `"section": "finally"` in the rendered output

---

## Design Constraints

- Actions never feed resolvers
- Resolvers always run before actions
- All CEL and templates (that don't reference `__actions`) are resolved before action execution or graph emission
- Action-to-action data flow is explicit via expressions referencing `__actions.<name>.results`
- Side effects are restricted to actions
- Providers are execution primitives used by actions
- Providers must have `CapabilityAction` to be used in actions
- Providers with `CapabilityAction` must define `OutputSchemas[action]` with at least:
  - `success` (bool): Whether the action succeeded
  - `data` (any): The result data (becomes `__actions.<name>.results`)
- External executors must be CEL-capable to evaluate deferred expressions

---

## Validation Rules

The following are validated at parse/load time:

1. Action names must match `^[a-zA-Z_][a-zA-Z0-9_-]*$`
2. Action names starting with `__` are reserved
3. Action names containing `[` or `]` are reserved (used for forEach expansion)
4. Action names must be unique across both `workflow.actions` and `workflow.finally` sections
5. `dependsOn` in `workflow.actions` must reference existing actions in `workflow.actions`
6. `dependsOn` in `workflow.finally` must reference existing actions in `workflow.finally` only (cannot depend on regular actions)
7. `dependsOn` must not create cycles (within each section)
8. Provider must exist and have `CapabilityAction`
9. `__actions.<name>` references in expressions/templates must reference:
   - In `workflow.actions`: actions in `dependsOn` (same section)
   - In `workflow.finally`: any regular action OR finally actions in `dependsOn`
10. `forEach` is only allowed in `workflow.actions`, not in `workflow.finally`
11. `retry.maxAttempts` must be >= 1
12. `timeout` must be a valid duration
13. `forEach.onError` must be `fail` or `continue` if specified
14. `forEach.concurrency` must be >= 0 if specified

---

## Future Enhancements

The following features are planned for future implementation:

### Result Schema Validation

Actions could optionally declare an expected result schema for validation and documentation:

~~~yaml
actions:
  fetchConfig:
    provider: api
    inputs:
      endpoint: https://api.example.com/config
    results:
      schema:
        properties:
          version:
            type: string
            description: Configuration version
          settings:
            type: object
            description: Application settings
        required: [version, settings]
~~~

**Benefits:**

- **Validation**: Verify provider output matches expected shape at runtime
- **Documentation**: Self-documenting result structures for solution readers
- **Type hints**: Better CEL/template autocomplete in editors for `__actions.<name>.results.*`
- **Contract enforcement**: Catch provider output changes that break dependent actions early

**Behavior:**

- When `results.schema` is defined, `Output.Data` from the provider is validated against it
- Schema validation errors cause the action to fail (unless `onError: continue`)
- Schema is optional—actions without it pass through `Output.Data` unchanged
- The schema uses standard JSON Schema format (`*jsonschema.Schema`), the same as provider input schemas

---

### Conditional Retry

Retry policies could support conditions to retry only on specific error types:

~~~yaml
actions:
  deploy:
    provider: api
    retry:
      maxAttempts: 3
      backoff: exponential
      initialDelay: 1s
      retryIf:
        expr: __error.statusCode == 429 || __error.statusCode >= 500
    inputs:
      endpoint: https://api.example.com/deploy
~~~

**Motivation:**

Not all failures are transient. Retrying a 400 Bad Request wastes time and resources. Conditional retry enables:

- **Selective retry**: Only retry rate limits (429) and server errors (5xx)
- **Fail fast**: Immediately fail on client errors (4xx except 429)
- **Custom logic**: Retry based on error message patterns or custom error codes

**The `__error` Namespace:**

During retry evaluation, `__error` provides context about the failure:

~~~yaml
__error:
  message: "Service temporarily unavailable"  # Error message
  statusCode: 503                              # HTTP status (if applicable)
  code: "UNAVAILABLE"                          # Provider-specific error code
  retryable: true                              # Provider's retryability hint
  attempt: 2                                   # Current attempt number (1-based)
~~~

**Default behavior:**

When `retryIf` is not specified:
- If provider sets `retryable: false` in error → no retry
- Otherwise → retry on any failure (current behavior)

**Interaction with provider `retryable: false`:**

Providers can declare `retryable: false` in their descriptor for destructive operations. The `retryIf` condition takes precedence—if a user explicitly defines a condition, it overrides the provider hint. This allows advanced users to retry even "non-retryable" operations when they know it's safe.

---

### Matrix Strategy

A matrix strategy for parallel expansion across multiple dimensions:

~~~yaml
actions:
  deploy:
    matrix:
      region: [us-east, us-west, eu-central]
      env: [staging, prod]
    provider: api
    inputs:
      endpoint:
        tmpl: "https://{{ .region }}.example.com/{{ .env }}/deploy"
~~~

Behavior:

- Expands to all combinations (6 actions in the example above)
- Each combination runs as an independent action
- Naming convention: `deploy-0`, `deploy-1`, ..., `deploy-5`
- Supports `exclude` to skip specific combinations:

~~~yaml
actions:
  deploy:
    matrix:
      region: [us-east, us-west, eu-central]
      env: [staging, prod]
      exclude:
        - region: eu-central
          env: staging  # Don't deploy staging to EU
    provider: api
~~~

- Supports `include` to add specific combinations with extra variables:

~~~yaml
actions:
  deploy:
    matrix:
      region: [us-east, us-west]
      env: [staging, prod]
      include:
        - region: ap-south
          env: prod
          extra: "asia-specific-config"  # Additional variable
    provider: api
~~~

- `matrix` is only available in `workflow.actions`, not in `workflow.finally` (same as `forEach`)

---

### Action Alias

Actions could declare an alias for shorter, more readable references in expressions:

~~~yaml
spec:
  workflow:
    actions:
      fetchConfiguration:
        provider: api
        alias: config  # Short alias for this action
        inputs:
          endpoint: https://api.example.com/config

      deploy:
        dependsOn: [fetchConfiguration]
        provider: api
        inputs:
          # Instead of: __actions.fetchConfiguration.results.endpoint
          # Use the shorter alias:
          endpoint:
            expr: config.results.endpoint
          version:
            expr: config.results.version
~~~

**Benefits:**

- **Readability**: Shorter, more meaningful names in expressions
- **Refactoring**: Change action names without updating all expressions (alias stays the same)
- **Consistency**: Use domain-specific terminology in expressions

**Rules:**

- Alias must be unique across all actions (including other aliases)
- Alias cannot conflict with reserved names (`_`, `__actions`, `__item`, `__index`, `__error`, etc.)
- Alias follows the same naming pattern as action names: `^[a-zA-Z_][a-zA-Z0-9_-]*$`
- The original `__actions.<actionName>` reference remains valid alongside the alias

---

### Exclusive Actions (Mutual Exclusion)

Actions could declare other actions they cannot run in parallel with, even if they would otherwise be scheduled concurrently:

~~~yaml
spec:
  workflow:
    actions:
      updateDatabase:
        provider: sql
        exclusive: [migrateDatabase]  # Cannot run at same time
        inputs:
          query: "UPDATE users SET status = 'active'"

      migrateDatabase:
        provider: sql
        inputs:
          script: "./migrations/001.sql"

      sendNotification:
        provider: slack
        inputs:
          message: "Update complete"
~~~

**Use Cases:**

- **Resource contention**: Two actions that access the same database/file/service
- **Rate limiting**: Avoid overwhelming an external API
- **Data consistency**: Prevent concurrent modifications to shared state

**Behavior:**

- `exclusive` is **one-way**: declaring `exclusive: [X]` on action A prevents X from running in parallel with A
- The other action does NOT need to declare the same exclusivity (though it can for documentation clarity)
- If both A and B are ready to run and A declares `exclusive: [B]`, the executor will run one, then the other (order determined by other factors like declaration order)
- `exclusive` does not imply `dependsOn` - the actions may run in any order, just not simultaneously
- `exclusive` applies to expanded forEach actions: if `deploy` declares `exclusive: [migrate]`, then `deploy[0]`, `deploy[1]`, etc. all exclude `migrate`

**Validation:**

- Referenced actions must exist in the same section (`workflow.actions` or `workflow.finally`)
- Self-reference is invalid (`exclusive: [self]`)

---

### Action Concurrency Limit

A CLI parameter to limit the maximum number of actions executing concurrently:

~~~bash
scafctl run solution myapp --max-action-concurrency=5
~~~

**Motivation:**

Solution authors don't know what machine will execute the solution. The operator running the solution knows their machine's capabilities (CPU, memory, network connections, API rate limits). A CLI parameter allows runtime tuning without modifying the solution.

**Behavior:**

- `0` (default): Unlimited concurrency within each phase
- `N > 0`: At most N actions execute simultaneously, even if more are ready
- Applies globally across all phases, not per-phase
- Does not affect `forEach.concurrency` (that's per-iteration within a single action)

---

## Cancellation Behavior

When execution is cancelled (e.g., user interrupt, external signal):

### Running Actions

- Receive a cancellation signal (context cancellation)
- Given a grace period (configurable, default: 30s) to clean up
- After grace period, forcibly terminated
- Marked with `status: cancelled` in `__actions`

### Pending Actions

- Not started
- Marked with `status: cancelled` in `__actions`

### Finally Actions (workflow.finally)

- **Always execute** regardless of cancellation (they are designed for cleanup)
- Have access to `__actions` including cancelled action statuses from regular actions
- Execute in their own dependency order within the finally section
- Can be forcibly terminated only with a second cancellation signal (force kill)

### Status Values (Complete)

| Status | Description |
|--------|-------------|
| `pending` | Action is waiting for dependencies (transient) |
| `running` | Action is currently executing (transient) |
| `succeeded` | Action completed successfully |
| `failed` | Action failed due to an error |
| `skipped` | Action was skipped (see `skipReason` for details) |
| `timeout` | Action exceeded its configured timeout |
| `cancelled` | Action was cancelled before or during execution |

---

## Summary

Actions in scafctl follow a Tekton-inspired model: explicit dependencies, implicit results (from provider output), and expression-based result references. scafctl can execute the graph with `run` or compile an executor-ready graph artifact with `render`. Features include:

- **Workflow structure**: `spec.workflow` containing `actions` for main execution and `finally` for cleanup
- **Error handling**: `onError` with `fail` or `continue` semantics (applies to both actions and forEach iterations)
- **Retries**: Configurable retry policies with backoff strategies
- **Timeouts**: Per-action timeout configuration
- **Conditions**: `when` expressions supporting both resolver and action result references
- **Iteration**: `forEach` expansion with `concurrency` and `onError` options (in `workflow.actions` only)
- **Cleanup**: Dedicated `workflow.finally` section for cleanup actions that always run
- **Cancellation**: Graceful shutdown with guaranteed finally execution
- **Progress callbacks**: Real-time execution feedback for UIs and monitoring

External executors consuming rendered graphs must be CEL-capable to evaluate deferred expressions that reference action results.

This keeps data flow explicit, execution predictable, and integration with external orchestrators straightforward.
