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

A solution is defined as a single YAML document following Kubernetes conventions.

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: solution-name
  version: 1.0.0
  description: What this solution does
  maintainers:
    - name: Person Name
      email: person@example.com

spec:
  resolvers: {}
  actions: {}
~~~

Top-level sections:

- `apiVersion` - API version (scafctl.io/v1)
- `kind` - Resource type (Solution)
- `metadata` - Name, version, description, maintainers, tags
- `spec` - Execution specification
  - `resolvers` - pure data derivation
  - `actions` - side-effect execution graph

Execution semantics are driven by the `spec` section.

---

## Core Sections

### Resolvers

Resolvers define how data is sourced, transformed, validated, and emitted.

~~~yaml
resolvers:
  image:
    description: Container image to deploy
    resolve:
      with:
        - provider: parameter
          inputs:
            key: image
        - provider: static
          inputs:
            value: nginx:1.27
    validate:
      with:
        - provider: validation
          inputs:
            match: "^[a-z0-9.-]+:[a-z0-9.-]+$"
          message: "Image must be in format name:tag"
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
    description: Deploy container to production
    provider: api
    when:
      expr: "_.environment == \"prod\""
    inputs:
      endpoint: https://api.example.com/deploy
      method: POST
      body:
        image:
          rslvr: image
        environment:
          rslvr: environment
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
scafctl run solution myapp
~~~

- Execute the rendered action graph
- Invoke providers
- Perform side effects

#### Render

~~~bash
scafctl render solution myapp
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
scafctl run solution myapp --action deploy
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
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: simple-deploy
  version: 1.0.0
  description: Simple deployment solution

spec:
  resolvers:
    environment:
      description: Target deployment environment
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: dev
      transform:
        with:
          - provider: cel
            inputs:
              expression: "__self.toLowerCase()"
      validate:
        with:
          - provider: validation
            inputs:
              expression: "__self in [\"dev\", \"staging\", \"prod\"]"
            message: "Environment must be dev, staging, or prod"

  actions:
    deploy:
      description: Deploy to target environment
      provider: api
      when:
        expr: "_.environment == \"prod\""
      inputs:
        endpoint: https://api.example.com/deploy
        method: POST
        body:
          environment:
            rslvr: environment
~~~

---

## Schema Reference

### Complete Structure

~~~yaml
apiVersion: scafctl.io/v1
kind: Solution

metadata:
  name: solution-identifier          # Required: unique identifier
  version: 1.0.0                      # Required: semver format
  description: What this solution does # Required: human description
  displayName: Human-Readable Name    # Optional: display name
  category: infrastructure            # Optional: categorization
  tags:                               # Optional: searchable tags
    - tag1
    - tag2
  maintainers:                        # Optional: contact info
    - name: Person Name
      email: person@example.com
  links:                              # Optional: external references
    - name: Documentation
      url: https://docs.example.com

spec:
  resolvers:                          # Required: data resolution
    resolverName:
      description: string             # Optional: resolver purpose
      resolve:
        with:                         # Required: provider list
          - provider: provider-name
            inputs: {}
      transform:                      # Optional: transformation pipeline
        with:
          - provider: cel
            inputs:
              expression: string
      validate:                       # Optional: validation rules
        with:
          - provider: validation
            inputs:
              expression: string
              match: string
              notMatch: string
            message: string

  actions:                            # Optional: side effects
    actionName:
      description: string             # Optional: action purpose
      provider: provider-name         # Required: execution provider
      when:                           # Optional: conditional execution
        expr: string
        tmpl: string
        rslvr: resolverName
      until:                          # Optional: retry condition
        expr: string
        tmpl: string
        rslvr: resolverName
      forEach:                        # Optional: iteration
        item: itemName                # Variable name for current item
        in: _.resolverName            # Array to iterate over
      dependsOn:                      # Optional: action dependencies
        - actionName
      inputs: {}                      # Required: provider-specific inputs
      results:                        # Optional: output definitions
        resultName:
          from: string                # CEL expression
~~~

### Metadata Fields

- `name` (required) - Unique identifier for the solution
- `version` (required) - Semantic version (e.g., 1.0.0)
- `description` (required) - Brief description of purpose
- `displayName` (optional) - Human-friendly display name
- `category` (optional) - Classification category
- `tags` (optional) - Array of searchable tags
- `maintainers` (optional) - Contact information
- `links` (optional) - External documentation references

### Resolver Fields

- `description` (optional) - Purpose of this resolver
- `resolve.from` (required) - Array of providers to try in order
- `transform.into` (optional) - Array of transformation providers
- `validate.from` (optional) - Array of validation providers

### Action Fields

- `description` (optional) - Purpose of this action
- `provider` (required) - Provider name to execute
- `when` (optional) - Conditional execution (supports expr, tmpl, rslvr)
- `until` (optional) - Retry condition (supports expr, tmpl, rslvr)
- `forEach` (optional) - Iteration over array values
- `dependsOn` (optional) - Array of action names to execute first
- `inputs` (required) - Provider-specific input map
- `results` (optional) - Output value definitions

### Input Forms

All action inputs and many provider inputs support four forms:

1. **Literal**: `key: value`
2. **Resolver binding**: `key: { rslvr: resolverName }`
3. **Expression**: `key: { expr: "celExpression" }`
4. **Template**: `key: { tmpl: "{{ .template }}" }`

See [Providers](providers.md) for detailed documentation.

---

## Summary

A solution is the highest-level abstraction in scafctl. It defines data, execution, and intent in a single declarative specification. By separating resolvers from actions and supporting both render and run modes, solutions remain analyzable, deterministic, and portable across execution environments.
