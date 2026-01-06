# Providers

## Purpose

Providers are stateless execution primitives. They exist to perform a single, well-defined operation given validated inputs and return either a result or an error. Providers do not own orchestration, control flow, transformation, or validation logic. This separation keeps solutions deterministic, testable, and provider-agnostic.

Providers are used by:

- Resolvers during the resolve phase
- Actions during execution

Providers are never invoked implicitly. A provider only runs when explicitly referenced by a resolver source or an action.

---

## Responsibilities

A provider is responsible for:

- Declaring its identity and capabilities
- Defining an explicit input schema
- Decoding validated input into a strongly typed structure
- Executing its operation
- Returning output data or an error

A provider is not responsible for:

- Deciding when it runs
- Resolving dependencies
- Transforming or validating data beyond schema enforcement
- Owning orchestration or control flow
- Reading global configuration directly

---

## Provider Model

### Conceptual Flow

- inputs (map)
  - schema validation
  - decode to typed input
  - execute operation
  - output or error

Providers behave as pure execution units with side effects limited strictly to the operation they encapsulate.

---

## Provider Interface

Providers expose a descriptor and an execution function.

~~~go
type Provider interface {
  Descriptor() ProviderDescriptor
  Execute(ctx Context, input any, dataCtx map[string]any) (any, error)
}

type ProviderDescriptor struct {
  Name   string
  Kind   string
  Schema SchemaDefinition
  Decode func(map[string]any) (any, error)
}

type SchemaDefinition struct {
  Parameters map[string]ParameterDefinition
}

type ParameterDefinition struct {
  Type        string
  Description string
  Required    bool
  Default     any
}
~~~

---

## Why Schema-First

Providers are schema-first so that:

- Invalid configurations fail before execution
- Solutions can be statically validated
- CLI help can be generated deterministically
- Errors are precise and actionable
- Providers remain self-describing and discoverable

Schema validation always occurs before Decode and Execute.

---

## Execution Context

The Execute function receives:

- `ctx`: execution context for cancellation and timeouts
- `input`: decoded, strongly typed provider input
- `dataCtx`: resolved data context equivalent to `_` in expressions

The data context allows providers to consume resolved values without re-resolving them.

---

## Built-in Provider Example: shell

### Descriptor

~~~go
type ShellInputs struct {
  Cmd []string `json:"cmd"`
  Dir string   `json:"dir"`
  Env []string `json:"env"`
}

func (p shellProvider) Descriptor() ProviderDescriptor {
  return ProviderDescriptor{
    Name: "shell",
    Kind: "sh",
    Schema: SchemaDefinition{
      Parameters: map[string]ParameterDefinition{
        "cmd": {
          Type: "array",
          Required: true,
          Description: "Command and arguments to execute",
        },
        "dir": {
          Type: "string",
          Required: false,
          Description: "Working directory",
        },
        "env": {
          Type: "array",
          Required: false,
          Description: "Environment variables in KEY=VALUE format",
        },
      },
    },
    Decode: func(raw map[string]any) (any, error) {
      var in ShellInputs
      err := mapstructure.Decode(raw, &in)
      return in, err
    },
  }
}
~~~

### Action Usage

~~~yaml
spec:
  actions:
    build:
      provider: shell
      inputs:
        dir: {{ _.projectRoot }}
        cmd:
          - go build ./...
~~~

---

## Provider Invocation from Resolvers

Resolvers invoke providers during the resolve phase.

~~~yaml
spec:
  resolvers:
    gitBranch:
      resolve:
        from:
          - provider: git
            inputs:
              field: branch
~~~

Resolver execution flow:

1. Provider is selected
2. Inputs are validated against the provider schema
3. Inputs are decoded into a typed structure
4. Provider executes
5. Result is emitted

---

## Provider Invocation from Actions

Actions invoke providers to perform side effects.

~~~yaml
spec:
  actions:
    deploy:
      provider: api
      inputs:
        endpoint: https://api.example.com/deploy
        method: POST
        body: '{"version":"{{ _.version }}"}'
~~~

Action orchestration, dependencies, conditions, and iteration are handled entirely outside the provider.

---

## Outputs

Providers may return structured output. Action outputs can be projected and referenced by subsequent actions.

~~~yaml
spec:
  actions:
    fetchConfig:
      provider: api
      inputs:
        endpoint: https://api.example.com/config
      outputs:
        config: .data
~~~

Output projection is expression-based and provider-agnostic.

---

## Provider Configuration and Reuse

Providers may be configured once and reused across actions.

~~~yaml
spec:
  providers:
    github:
      type: api
      config:
        baseUrl: https://api.github.com
        defaultHeaders:
          Authorization: Bearer {{ _.githubToken }}

  actions:
    createRelease:
      provider: github
      inputs:
        endpoint: /repos/{{ _.org }}/{{ _.repo }}/releases
        method: POST
~~~

Configuration is merged with action inputs before schema validation.

---

## Plugin Providers

External providers are loaded dynamically as plugins.

### Registration

~~~go
func RegisterProvider(p Provider) {
  registry[p.Descriptor().Name] = p
}
~~~

Plugin providers must expose a factory that returns a Provider instance. Once registered, plugin providers behave identically to built-in providers.

---

## CLI Interaction

### List Providers

~~~bash
scafctl providers list
~~~

### Describe Provider

~~~bash
scafctl providers describe shell
~~~

The description includes:

- Provider name and kind
- Input parameters
- Required fields
- Defaults and descriptions

---

## Design Constraints

- Providers must be stateless
- Providers must declare all inputs explicitly
- Providers must fail fast on invalid input
- Providers must not depend on execution order
- Providers must not own orchestration or control flow

---

Below is a non-exhaustive list of possible providers and an example of how each would be invoked in a resolver `resolve.from` section. The intent is to make it obvious that all data enters the system through explicit provider calls.

The `cli` provider is renamed to `parameter` to better reflect its role.

---

## Common Providers

### parameter

Reads a value passed explicitly at invocation time.

Typical sources:

- CLI flags
- Action-scoped parameters
- Runtime overrides

~~~yaml
resolve:
  from:
    - provider: parameter
      inputs:
        key: name
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

Possible fields include:

- branch
- commit
- tag
- remoteUrl

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

### cel (built-in)

Derives a value using common expression language

~~~yaml
resolve:
  from:
    - provider: cel
      expr: _.org + "/" + _.repo
~~~

---

### catalog

Resolves an entry from the scafctl catalog.

~~~yaml
resolve:
  from:
    - provider: catalog
      inputs:
        ref: solution:base-service
        field: version
~~~

---

### parameter with fallback example

Demonstrates ordered resolution and fallback semantics.

~~~yaml
resolve:
  from:
    - provider: parameter
      inputs:
        key: env
    - provider: env
      inputs:
        key: ENVIRONMENT
    - provider: static
      inputs:
        value: dev
~~~

## Summary

Providers form the execution boundary of scafctl. They are schema-first, stateless, and explicitly invoked. By isolating execution behind providers, scafctl preserves declarative solutions, strong validation, and predictable behavior.
