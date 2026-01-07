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

### Notes

- Should providers support mocking so they can be dry run?

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

Each input field supports exactly one of the following forms. Choose the most appropriate form based on your use case.

### 1. Literal Value

Set a property directly as a literal. The value is passed as-is with no evaluation.

~~~yaml
inputs:
  image: nginx:1.27
  retries: 3
  enabled: true
~~~

### 2. Resolver Binding

Reference a resolver directly using `rslv`. The value emitted by the resolver is copied, preserving its type.

~~~yaml
inputs:
  image:
    rslv: imageResolver
  environment:
    rslv: deploymentEnv
~~~

This is the canonical form for passing resolver outputs to providers.

### 3. Expression

Evaluate a CEL expression using `expr`. The expression is evaluated using the resolver context (`_`).

~~~yaml
inputs:
  image:
    expr: _.org + "/" + _.repo + ":" + _.version
  tags:
    expr: _.environments.map(e, e.toUpperCase())
~~~

Expressions are computed on-the-fly and may combine multiple resolver values.

### 4. Template String

Render a Go template using `tmpl`. Always produces a string.

~~~yaml
inputs:
  path:
    tmpl: "./{{ .environment }}/main.tf"
  message:
    tmpl: "Deploying {{ .app }} to {{ .region }}"
~~~

Templates are useful for constructing formatted strings from resolver values.

### Exclusivity Rule

For a single input field, you must specify exactly one of:

- A literal value
- `rslv: resolverName`
- `expr: celExpression`
- `tmpl: "templateString"`

It is an error to specify more than one form for the same field.

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
        expression: __self.toLowerCase()
~~~

Each step receives the previous value as `__self`.

---

## Providers in Validation

Validation is provider-backed.

Any provider that returns a boolean may be used as a validation provider.

### Built-in Provider: validation

The built-in `validation` provider supports:

- `match` - regex pattern that must match (supports all input forms)
- `notMatch` - regex pattern that must not match (supports all input forms)
- `expression` - CEL expression returning boolean

Rules:

- `match` and `notMatch` may be combined
- `match` and `notMatch` support all four input forms (literal, rslv, expr, tmpl)
- `expression` is for CEL-based validation
- The provider returns a single boolean result

Examples:

Literal regex patterns:

~~~yaml
validate:
  from:
    - provider: validation
      inputs:
        match: "^[a-z0-9-]+$"
        notMatch: "^fff$"
      message: "Invalid value"
~~~

Using expression for computed regex:

~~~yaml
validate:
  from:
    - provider: validation
      inputs:
        match:
          expr: "\"^\" + _.prefix + \"[a-z]+$\""
      message: "Must match prefix pattern"
~~~

Using resolver for dynamic pattern:

~~~yaml
validate:
  from:
    - provider: validation
      inputs:
        match:
          rslv: validationPattern
      message: "Must match validation pattern"
~~~

Using template for pattern:

~~~yaml
validate:
  from:
    - provider: validation
      inputs:
        match:
          tmpl: "^{{ .allowedPrefix }}-[a-z0-9]+$"
      message: "Must match allowed prefix"
~~~

Using CEL expression for validation logic:

~~~yaml
validate:
  from:
    - provider: validation
      inputs:
        expression: "__self in [\"dev\", \"staging\", \"prod\"]"
      message: "Invalid environment"
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
        expression: _.org + "/" + _.repo
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
