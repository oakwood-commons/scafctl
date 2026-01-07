# Providers

## Purpose

Providers are stateless execution primitives. They perform a single, well-defined operation given validated inputs and return either a result or an error.

Providers do not own orchestration, control flow, dependency resolution, or lifecycle decisions. This separation keeps solutions deterministic, testable, and explicit.

Providers are used by:

- Resolvers (during resolve, transform, and validate phases)
- Actions (during execution or render)

Providers are never invoked implicitly. A provider runs only when explicitly referenced.

---

## Responsibilities

A provider is responsible for:

- Declaring its identity and capabilities
- Defining an explicit input schema
- Validating inputs against that schema
- Decoding validated input into strongly typed structures
- Executing its operation
- Returning output data or an error

A provider is not responsible for:

- Deciding when it runs
- Resolving dependencies
- Performing orchestration or control flow
- Mutating shared execution state
- Reading undeclared global state

---

## Execution Context

Providers are invoked with a resolved execution context.

For resolver execution:

- `_` contains only resolver outputs
- Nothing exists in `_` unless emitted by a resolver

For action execution:

- Providers may receive additional action-local data, but this does not affect resolver semantics

Special symbols passed by the caller:

- `__self` when used in resolver transform or validate
- `__item` when used in action foreach

Providers do not evaluate expressions or templates themselves. All inputs are fully resolved before invocation.

---

## Provider Model

### Conceptual Flow

- inputs (map)
  - schema validation
  - decode to typed input
  - execute operation
  - output or error

Providers behave as isolated execution units with no implicit coupling to other providers.

---

## Provider Interface (Conceptual)

~~~go
type Provider interface {
  Descriptor() ProviderDescriptor
  Execute(ctx context.Context, input any) (any, error)
}

type ProviderDescriptor struct {
  Name   string
  Schema SchemaDefinition
  Decode func(map[string]any) (any, error)
}

type SchemaDefinition struct {
  Parameters map[string]ParameterDefinition
}

type ParameterDefinition struct {
  Type        string
  Required    bool
  Description string
}
~~~

This interface is illustrative. The exact implementation may evolve, but the contract remains schema-first and explicit.

---

## Input Resolution

Provider inputs are resolved by scafctl before execution.

Each input field supports exactly one of the following forms:

### 1. Literal Value

Passed as-is with no evaluation.

~~~yaml
inputs:
  image: nginx:1.27
~~~

### 2. Resolver Binding (Canonical)

Copies the value emitted by a resolver, preserving its type.

~~~yaml
inputs:
  image:
    resolver: image
~~~

### 3. Explicit CEL Expression

Evaluated using CEL before provider execution.

~~~yaml
inputs:
  image:
    expr: _.org + "/" + _.repo + ":" + _.version
~~~

### 4. Template String

Rendered using Go templating. Always produces a string.

~~~yaml
inputs:
  path:
    tmpl: "./{{ _.environment }}/main.tf"
~~~

### Exclusivity Rule

For a single input field, it is an error to specify more than one of:

- literal
- `resolver`
- `expr`
- `tmpl`

---

## Providers in Resolvers

Resolvers invoke providers to obtain, transform, or validate values.

~~~yaml
resolve:
  from:
    - provider: env
      inputs:
        key: PROJECT_NAME
~~~

Resolver execution flow:

1. Provider is selected
2. Inputs are resolved and validated
3. Provider executes
4. Result is returned to the resolver
5. Resolver emits the value after transform and validate

Providers used in resolvers must be pure and deterministic.

---

## Providers in Transform

Transform steps are provider executions applied sequentially to a single value.

~~~yaml
transform:
  into:
    - provider: cel
      inputs:
        expr: __self.toLowerCase()
~~~

Each step receives the previous value as `__self`.

---

## Providers in Validation

Validation is provider-backed.

Any provider that returns a boolean may be used as a validation provider.

### Built-in Provider: validation

The built-in `validation` provider supports:

- `match` (regex match)
- `notMatch` (regex must not match)
- `notMatch.expr` (CEL expression returning boolean)
- `match.expr` (CEL expression returning boolean)

Rules:

- `match` and `notMatch` may be combined
- The provider returns a single boolean result

Example:

~~~yaml
validate:
  from:
    - provider: validation
      inputs:
        match: "^[a-z0-9-]+$"
        notMatch: "^fff$"
      message: "Invalid value"
~~~

---

## Providers in Actions

Actions invoke providers to perform side effects or generate artifacts.

~~~yaml
actions:
  build:
    provider: shell
    inputs:
      cmd:
        - go
        - build
        - ./...
~~~

Action orchestration, dependencies, iteration, and conditional execution are handled outside the provider.

---

## Built-in Providers (Non-Exhaustive)

### parameter

Reads a value supplied at invocation time.

~~~yaml
resolve:
  from:
    - provider: parameter
      inputs:
        key: env
~~~

---

### env

Reads from the process environment.

~~~yaml
resolve:
  from:
    - provider: env
      inputs:
        key: PROJECT_NAME
~~~

---

### static

Supplies a literal value.

~~~yaml
resolve:
  from:
    - provider: static
      inputs:
        value: my-app
~~~

---

### filesystem

Reads data from the local filesystem.

~~~yaml
resolve:
  from:
    - provider: filesystem
      inputs:
        operation: read
        path: ./config/name.txt
~~~

---

### git

Reads data from a git repository or working tree.

~~~yaml
resolve:
  from:
    - provider: git
      inputs:
        field: branch
~~~

---

### api

Fetches data from an HTTP endpoint.

~~~yaml
resolve:
  from:
    - provider: api
      inputs:
        endpoint: https://api.example.com/project
        method: GET
~~~

---

### cel

Derives a value using CEL.

~~~yaml
resolve:
  from:
    - provider: cel
      inputs:
        expr: _.org + "/" + _.repo
~~~

---

## Design Constraints

- Providers must be stateless
- Providers must declare all inputs explicitly
- Providers must fail fast on invalid input
- Providers must not depend on execution order
- Providers must not introduce hidden data into resolver context

---

## Summary

Providers are explicit, schema-driven execution units. scafctl resolves all inputs before invoking a provider, ensuring that providers operate only on concrete, validated data. This keeps resolver behavior deterministic and action execution predictable.
