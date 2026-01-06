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

Actions form a directed acyclic graph similar to Tekton Tasks.

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
scafctl run solution:myapp
~~~

Behavior:
- Resolve all resolvers
- Evaluate all CEL and templates needed to render actions
- Execute actions in dependency order
- Perform side effects

### render

Renders a fully resolved action graph without executing any action providers.

~~~bash
scafctl render solution:myapp
~~~

Behavior:
- Resolve all resolvers
- Evaluate all CEL and templates needed to render actions
- Emit an executor-ready action graph artifact
- No action providers are executed
- No action results are produced at render time

Render produces an artifact, not new runtime data.

---

## Action Definition

Actions are defined under `spec.actions`.

~~~yaml
spec:
  actions:
    deploy:
      provider: api
      inputs:
        endpoint: https://api.example.com/deploy
~~~

Each action declares exactly one provider execution.

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
  image: _.image
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
  path: "./config/{{ _.environment }}/app.yaml"
~~~

Providers never see CEL, templates, or resolver references. Providers receive concrete values.

---

## Dependencies

Actions declare dependencies explicitly using `dependsOn`.

~~~yaml
actions:
  build:
    provider: shell

  deploy:
    dependsOn: [build]
    provider: api
~~~

Rules:
- Dependencies form a DAG
- An action runs only after all dependencies succeed
- Cycles are rejected

---

## Results (Tekton-style)

Actions may expose named results, similar to Tekton task results.

~~~yaml
actions:
  fetchConfig:
    provider: api
    inputs:
      endpoint: https://api.example.com/config
    results:
      config:
        from: result.data
~~~

### Result semantics

- `result` refers to the provider execution result for the current action
- `results` are named, immutable projections of `result`
- Results are available only after the action executes
- In render mode, results are declared but not populated

---

## Consuming Results from Dependencies

Actions consume results from dependencies using explicit bindings, following the Tekton model.

~~~yaml
actions:
  fetchConfig:
    provider: api
    inputs:
      endpoint: https://api.example.com/config
    results:
      config:
        from: result.data

  deploy:
    dependsOn: [fetchConfig]
    provider: api
    inputs:
      body:
        fromAction:
          name: fetchConfig
          result: config
~~~

Tekton equivalent reference:

~~~text
$(tasks.fetchConfig.results.config)
~~~

Rules:
- `fromAction` may only reference declared dependencies
- Referencing a non-dependency is a validation error
- External executors do not evaluate CEL or templates
- The graph encodes action-to-action data flow explicitly

---

## Conditions

Actions may be conditionally enabled using `when`.

~~~yaml
actions:
  deploy:
    when:
      expr: _.environment == "prod"
    provider: api
~~~

Behavior:
- `when.expr` is evaluated during render
- The rendered action includes a boolean condition
- In run mode, scafctl skips actions whose condition is false
- In render mode, the emitted graph includes the evaluated condition value

---

## Iteration

Actions may be expanded declaratively using `forEach`.

~~~yaml
actions:
  deploy:
    forEach:
      over: _.regions
      as: region
    provider: api
    inputs:
      endpoint: "https://{{ region.api }}/deploy"
~~~

Behavior:
- Iteration is expanded during render
- Produces multiple action nodes
- Each iteration is independent

---

## Rendered Graph Shape (Conceptual)

After rendering, scafctl emits a graph that contains only concrete inputs and explicit references.

~~~yaml
actions:
  fetchConfig:
    provider: api
    inputs:
      endpoint: https://api.example.com/config
    results:
      config: "<runtime>"

  deploy:
    provider: api
    dependsOn: [fetchConfig]
    when: true
    inputs:
      body:
        fromAction:
          name: fetchConfig
          result: config
~~~

Notes:
- `results` are placeholders in render output
- `fromAction` references are preserved for the executor
- No runtime action result values exist until execution time

---

## Design Constraints

- Actions never feed resolvers
- Resolvers always run before actions
- All CEL and templates are resolved before action execution or graph emission
- Action-to-action data flow is explicit via `results` and `fromAction`
- Side effects are restricted to actions
- Providers are execution primitives used by actions

---

## Summary

Actions in scafctl follow a Tekton-inspired model: explicit dependencies, named results, and declarative result bindings. scafctl can execute the graph with `run` or compile an executor-ready graph artifact with `render`. This keeps data flow explicit, execution predictable, and integration with external orchestrators straightforward.
