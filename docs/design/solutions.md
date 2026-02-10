# Solutions

## Implementation Status

| Feature | Status | Notes |
| ------- | ------ | ----- |
| Solution structure (apiVersion/kind/metadata/spec) | ✅ Implemented | `pkg/solution/solution.go` |
| Metadata fields | ✅ Implemented | Includes icon/banner beyond original design |
| Catalog fields | ✅ Implemented | visibility, beta, disabled |
| Spec with resolvers | ✅ Implemented | `pkg/solution/spec.go` |
| Workflow with actions/finally | ✅ Implemented | Uses `workflow.actions` and `workflow.finally` |
| Dependencies (plugins) | ⏳ Planned | Declared under `bundle.plugins` — see [catalog-build-bundling.md](catalog-build-bundling.md) |
| Validation | ✅ Implemented | `pkg/solution/spec_validation.go` |
| Run command | ✅ Implemented | `scafctl run solution` |
| Render command | ✅ Implemented | `scafctl render solution` |

---

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

compose:                             # ⏳ Planned — merge partial YAML files into this solution
  - resolvers.yaml
  - workflow.yaml

bundle:                              # ⏳ Planned — build-time packaging metadata
  include:                           # Glob patterns for files to bundle
    - templates/**/*.tmpl
  plugins:                           # External plugin dependencies
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
      defaults:
        region: us-east-1

spec:
  resolvers: {}
  actions: {}
~~~

Top-level sections:

- `apiVersion` - API version (scafctl.io/v1)
- `kind` - Resource type (Solution)
- `metadata` - Name, version, description, maintainers, tags
- `compose` - Relative paths to partial YAML files merged into this solution (⏳ planned)
- `bundle` - Build-time packaging: files to include and plugin dependencies (⏳ planned). See [catalog-build-bundling.md](catalog-build-bundling.md)
- `catalog` - Publishing metadata (visibility, beta flag, disabled flag)
- `spec` - Execution specification
  - `resolvers` - pure data derivation
  - `workflow` - action execution specification
    - `actions` - side-effect execution graph
    - `finally` - cleanup actions that run after all regular actions

Execution semantics are driven by the `spec` section.

Expressions use CEL (`celexp.Expression`), and templates use Go templating (`gotmpl.GoTemplatingContent`).

The default `apiVersion` is scafctl.io/v1; breaking schema changes follow semver.

---

## Dependencies

> ⏳ **Planned Feature**: Plugin dependencies are not yet implemented. See [catalog-build-bundling.md](catalog-build-bundling.md) for the full design.

Solutions declare plugin dependencies under `bundle.plugins`, not as a separate top-level `dependencies` section. This keeps all packaging-and-distribution metadata together under `bundle`.

~~~yaml
bundle:
  plugins:
    - name: aws-provider
      kind: provider
      version: "^1.5.0"
      defaults:
        region: us-east-1
    - name: gcp-provider
      kind: provider
      version: ">=2.0.0"
~~~

Each plugin entry declares:
- `name` — catalog reference for the plugin
- `kind` — plugin type (`provider` or `auth-handler`)
- `version` — semver constraint
- `defaults` (optional) — default input values (supports full `ValueRef`: literal, `expr:`, `tmpl:`, `rslvr:`) shallow-merged beneath inline inputs

Planned behavior:

1. scafctl checks if required plugins exist in the local catalog
2. Missing plugins are pulled from configured remote catalogs
3. Version constraints are validated
4. Plugins are dynamically loaded to make their providers available
5. Plugin defaults are shallow-merged beneath inline provider inputs (inline always wins)

This will enable solutions to use providers from external plugins without bundling them.

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
workflow:
  actions:
    deploy:
      description: Deploy container to production
      displayName: Deploy to Production
      provider: api
      when:
        expr: "_.environment == \"prod\""
      onError: fail                   # fail, continue, ignore
      timeout: 5m                     # action timeout
      retry:                          # retry configuration
        maxAttempts: 3
        backoff: exponential
        initialDelay: 1s
        maxDelay: 30s
      inputs:
        endpoint: https://api.example.com/deploy
        method: POST
        body:
          image:
            rslvr: image
          environment:
            rslvr: environment

  finally:                            # Cleanup actions
    cleanup:
      description: Cleanup temporary resources
      provider: shell
      inputs:
        command: rm -rf /tmp/deploy-*
~~~

Properties:

- May perform side effects
- Executed after resolvers
- Form a DAG
- May depend on other actions
- `finally` actions run after all regular actions complete

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

Execution stops on the first failed action; dependent actions are skipped.

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

  workflow:
    actions:
      deploy:
        description: Deploy to target environment
        provider: api
        when:
          expr: "_.environment == \"prod\""
        timeout: 5m
        retry:
          maxAttempts: 3
          backoff: exponential
          initialDelay: 1s
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

catalog:                              # Optional: publishing metadata (no execution impact)
  visibility: private                 # Optional: public|private|internal (default: private)
  beta: false                         # Optional: beta flag (default: false)
  disabled: false                     # Optional: availability flag (default: false)

compose:                              # Optional: partial YAML files merged into this solution
  - resolvers.yaml
  - workflow.yaml

bundle:                               # Optional: build-time packaging metadata
  include:                            # Optional: glob patterns for files to bundle
    - templates/**/*.tmpl
  plugins:                            # Optional: external plugin dependencies
    - name: aws-provider
      kind: provider                  # provider | auth-handler
      version: "^1.5.0"
      defaults:                       # Optional: default input values (supports ValueRef)
        region: us-east-1

spec:
  resolvers:                          # Optional: data resolution
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

  workflow:                           # Optional: action execution
    actions:                          # Optional: side effects
      actionName:
        description: string           # Optional: action purpose
        displayName: string           # Optional: human-friendly name
        sensitive: bool               # Optional: mask in logs
        provider: provider-name       # Required: execution provider
        when:                         # Optional: conditional execution
          expr: string
          tmpl: string
          rslvr: resolverName
        onError: fail|continue|ignore # Optional: error handling
        timeout: duration             # Optional: max execution time
        retry:                        # Optional: retry configuration
          maxAttempts: int
          backoff: fixed|linear|exponential
          initialDelay: duration
          maxDelay: duration
        forEach:                      # Optional: iteration
          item: itemName              # Variable name for current item
          in: _.resolverName          # Array to iterate over
        dependsOn:                    # Optional: action dependencies
          - actionName
        inputs: {}                    # Optional: provider-specific inputs

    finally:                          # Optional: cleanup actions
      actionName:
        # Same fields as actions, except:
        # - No forEach allowed
        # - Cannot dependsOn regular actions
        # - Has implicit dependency on all regular actions
~~~

### Metadata Fields

- `name` (required) - Unique identifier for the solution
- `version` (required) - Semantic version (e.g., 1.0.0)
- `description` (optional) - Brief description of purpose
- `displayName` (optional) - Human-friendly display name
- `category` (optional) - Classification category
- `tags` (optional) - Array of searchable tags
- `maintainers` (optional) - Contact information
- `links` (optional) - External documentation references
- `icon` (optional) - URL or path to solution icon image
- `banner` (optional) - URL or path to solution banner image

### Resolver Fields

- `description` (optional) - Purpose of this resolver
- `resolve.with` (required) - Array of providers to try in order
- `transform.with` (optional) - Array of transformation providers
- `validate.with` (optional) - Array of validation providers

### Action Fields

- `name` (set from map key) - Action identifier
- `description` (optional) - Purpose of this action
- `displayName` (optional) - Human-friendly display name
- `sensitive` (optional) - If true, inputs/outputs are masked in logs
- `provider` (required) - Provider name to execute
- `when` (optional) - Conditional execution (supports expr, tmpl, rslvr)
- `onError` (optional) - Error handling: `fail` (default), `continue`, `ignore`
- `timeout` (optional) - Maximum execution duration (e.g., `30s`, `5m`)
- `retry` (optional) - Retry configuration:
  - `maxAttempts` - Total execution attempts (min: 1)
  - `backoff` - Strategy: `fixed` (default), `linear`, `exponential`
  - `initialDelay` - Delay before first retry
  - `maxDelay` - Maximum delay between retries
- `forEach` (optional) - Iteration over array values (not allowed in `finally`)
- `dependsOn` (optional) - Array of action names to execute first
- `inputs` (optional) - Provider-specific input map

### Action Results

Action results are available to dependent actions via `__actions.<name>`:

- `inputs` - Resolved inputs passed to provider
- `results` - Provider output data
- `status` - Execution status: `pending`, `running`, `succeeded`, `failed`, `skipped`, `timeout`, `cancelled`
- `skipReason` - Why skipped: `condition` or `dependency-failed`
- `startTime` - Execution start time
- `endTime` - Execution end time
- `error` - Error message if failed

For `forEach` actions, results include iteration details with index and per-iteration status.

### Input Forms

All action inputs and many provider inputs support four forms:

1. **Literal**: `key: value`
2. **Resolver binding**: `key: { rslvr: resolverName }`
3. **Expression**: `key: { expr: "celExpression" }`
4. **Template**: `key: { tmpl: "{{ .template }}" }`

Inputs may bind to resolver outputs and, when dependency rules allow, to prior action results. Providers always receive fully materialized values; CEL, templates, and bindings are resolved before provider execution or render emission.

See [Providers](providers.md) for detailed documentation.

---

## Summary

A solution is the highest-level abstraction in scafctl. It defines data, execution, and intent in a single declarative specification. By separating resolvers from actions and supporting both render and run modes, solutions remain analyzable, deterministic, and portable across execution environments.
