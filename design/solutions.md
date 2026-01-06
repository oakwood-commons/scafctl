# Solutions

## Purpose

A solution is the top-level unit of configuration in scafctl. It defines *what exists*, *how data is obtained*, and *what actions should occur*.

Solutions compose resolvers, actions, providers, and plugins into a single, declarative model that can be executed or rendered.

A solution is declarative, deterministic, and self-contained.

---

## What a Solution Is

A solution is:

- A specification of intent
- A container for resolvers and actions
- The boundary for dependency analysis
- The unit passed to scafctl commands

A solution answers:

- What data is needed?
- How is that data derived?
- What side effects should occur?
- In what order?

---

## What a Solution Is Not

A solution is not:

- A script
- A workflow engine
- A runtime environment
- A provider implementation

Solutions do not contain imperative logic. All logic is expressed through resolvers, actions, and providers.

---

## Solution Structure

A solution is defined as a single YAML document.

~~~yaml
spec:
  resolvers: {}
  actions: {}
~~~

Top-level sections:

- `resolvers` - pure data derivation
- `actions` - side-effect execution graph

Additional metadata may be present, but execution semantics are driven by these sections.

---

## Core Sections

### Resolvers

Resolvers define how data is sourced, transformed, validated, and emitted.

~~~yaml
resolvers:
  image:
    resolve:
      from:
        - provider: static
          inputs:
            value: nginx:1.27
~~~

Properties:

- Pure and deterministic
- Executed before any actions
- Form a DAG
- May execute asynchronously

Resolver outputs are available under `_`.

---

### Actions

Actions define side effects as a declarative execution graph.

~~~yaml
actions:
  deploy:
    provider: api
    inputs:
      image: _.image
~~~

Properties:

- May perform side effects
- Executed after resolvers
- Form a DAG
- May depend on other actions

Actions may be executed or rendered.

---

## Execution Lifecycle

A solution follows a fixed lifecycle.

### 1. Load

- Parse solution YAML
- Validate schema
- Discover required providers and plugins

---

### 2. Resolve

- Execute all required resolvers
- Evaluate all resolver CEL and templates
- Build resolver DAG
- Emit resolved values into `_`

Resolvers may execute concurrently.

---

### 3. Render Actions

- Evaluate `when` expressions
- Expand `forEach`
- Resolve all action inputs
- Produce a concrete action graph

At this point:

- No CEL remains
- No templates remain
- No resolver references remain

---

### 4. Execute or Emit

Depending on the command:

#### Run

~~~bash
scafctl run solution:myapp
~~~

- Execute the rendered action graph
- Invoke providers
- Perform side effects

#### Render

~~~bash
scafctl render solution:myapp
~~~

- Emit the rendered action graph
- Do not execute providers
- Produce an executor-ready artifact

---

## Solution as a Compilation Unit

Under this model, scafctl behaves like a compiler.

Input:

- Solution YAML
- CEL expressions
- Templates
- Provider references

Output:

- Either side effects (run)
- Or a fully resolved action graph (render)

Resolvers and actions are never interleaved.

---

## Dependency Analysis

All dependencies are inferred and validated.

### Resolver Dependencies

- Derived from `_` references
- Must form a DAG
- Cycles are rejected

### Action Dependencies

- Declared explicitly with `dependsOn`
- Must form a DAG
- Cycles are rejected

Resolvers never depend on actions.

---

## Determinism and Reproducibility

A solution is deterministic if:

- Providers are deterministic
- Inputs are stable
- No side effects occur outside actions

Render mode guarantees reproducible output given the same inputs.

---

## Minimal Execution

scafctl executes only what is required.

~~~bash
scafctl run solution:myapp --action deploy
~~~

Behavior:

- Resolve only resolvers required by `deploy`
- Render only dependent actions
- Execute minimal graph

---

## Design Constraints

- Solutions are declarative
- Resolvers are pure
- Actions contain all side effects
- Providers are the only execution mechanism
- Rendering and execution are separate concerns

---

## Example Solution

~~~yaml
spec:
  resolvers:
    environment:
      resolve:
        from:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: dev

  actions:
    deploy:
      when:
        expr: _.environment == "prod"
      provider: api
      inputs:
        env: _.environment
~~~

---

## Summary

A solution is the highest-level abstraction in scafctl. It defines data, execution, and intent in a single declarative specification. By separating resolvers from actions and supporting both render and run modes, solutions remain analyzable, deterministic, and portable across execution environments.
