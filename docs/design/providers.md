# Provider Interface & Input Contracts

> **Goal:** Explain how pluggable providers can expose strongly typed schemas while solutions remain provider-agnostic.

## Overview

Providers are the execution primitives behind resolvers and actions. Each provider implementation supplies two key pieces:

1. **Descriptor metadata** — name, type, documentation, version
2. **Input contract** — schema describing accepted options, decoded into a Go struct

When a solution references `provider: shell`, the engine looks up the provider descriptor, validates the `inputs:` map against the provider’s schema, decodes it into a strongly typed struct, and then executes the provider.

## Registrations in Go

```go
// Provider interface implemented by built-in and plugin providers.
type Provider interface {
    Descriptor() ProviderDescriptor
    Execute(ctx Context, input any, dataCtx map[string]any) (any, error)
}

type ProviderDescriptor struct {
    Name    string
    Kind    string              // e.g. "sh", "api", "filesystem"
    Schema  SchemaDefinition    // Parameter definitions for validation
    Decode  func(map[string]any) (any, error)
}

type SchemaDefinition struct {
    Parameters map[string]ParameterDefinition
}

type ParameterDefinition struct {
    Type        string // "string", "array", "object", "boolean", "number", "integer"
    Description string // Human-readable description
    Required    bool   // Whether parameter is mandatory
    Default     any    // Optional default value
}
```

- `Execute` receives three parameters:
  - `ctx` - Go context for cancellation and timeout
  - `input` - Decoded, strongly-typed input struct specific to the provider
  - `dataCtx` - Data context containing resolved values from resolvers and previous actions (accessible as `_` in expressions)
- `Schema` captures the provider-specific parameter definitions (`dir`, `cmd`, … for shell; `endpoint`, `method`, … for API).
- `Decode` turns the untyped `map[string]any` from YAML into a typed struct. If decoding fails, validation error bubbles back to the user before execution.

## Shell Provider Example

```go
// ShellInputs describes the schema for shell provider actions.
type ShellInputs struct {
    Dir string   `json:"dir"`
    Cmd []string `json:"cmd"`
    Env []string `json:"env"`
}

func (shellProvider) Descriptor() ProviderDescriptor {
    return ProviderDescriptor{
        Name: "shell",
        Kind: "sh",
        Schema: SchemaDefinition{
            Parameters: map[string]ParameterDefinition{
                "cmd": {
                    Type:        "array",
                    Description: "Command and arguments to execute",
                    Required:    true,
                },
                "dir": {
                    Type:        "string",
                    Description: "Working directory for command execution",
                    Required:    false,
                },
                "env": {
                    Type:        "array",
                    Description: "Environment variables in KEY=VALUE format",
                    Required:    false,
                },
            },
        },
        Decode: func(raw map[string]any) (any, error) {
            var input ShellInputs
            if err := mapstructure.Decode(raw, &input); err != nil {
                return nil, err
            }
            return input, nil
        },
    }
}

func (shellProvider) Execute(ctx Context, input any, dataCtx map[string]any) (any, error) {
    in := input.(ShellInputs)
    // run commands in in.Dir with in.Cmd, etc.
    // dataCtx contains resolved values from resolvers (e.g., dataCtx["projectRoot"], dataCtx["goBinary"])
    return nil, nil
}
```

The solution author simply writes:

```yaml
spec:
  actions:
    test:
      provider: shell
      inputs:
        dir: {{ _.projectRoot }}
        cmd:
          - echo "Running go test"
          - {{ _.goBinary }} test ./...
```

The engine ensures the `inputs` block conforms to `ShellInputs` before `Execute` runs. If the user enters `endpoint:` by mistake, validation fails with a user-friendly error.

## API Provider Sketch

```go
type APIInputs struct {
    Endpoint string            `json:"endpoint"`
    Method   string            `json:"method"`
    Headers  map[string]string `json:"headers"`
    Body     string            `json:"body"`
}

func (apiProvider) Descriptor() ProviderDescriptor {
    return ProviderDescriptor{
        Name: "api",
        Kind: "http",
        Schema: SchemaDefinition{
            Parameters: map[string]ParameterDefinition{
                "endpoint": {
                    Type:        "string",
                    Description: "API endpoint URL",
                    Required:    true,
                },
                "method": {
                    Type:        "string",
                    Description: "HTTP method",
                    Required:    false,
                    Default:     "GET",
                },
                "headers": {
                    Type:        "object",
                    Description: "HTTP headers to include in request",
                    Required:    false,
                },
                "body": {
                    Type:        "string",
                    Description: "Request body content",
                    Required:    false,
                },
            },
        },
        Decode: func(raw map[string]any) (any, error) {
            var input APIInputs
            if err := mapstructure.Decode(raw, &input); err != nil {
                return nil, err
            }
            return input, nil
        },
    }
}
```

Each provider defines its own struct. Solutions stay consistent:

```yaml
spec:
  actions:
    deploy:
      provider: api
      inputs:
        endpoint: https://api.example.com/deploy
        method: POST
        headers:
          Authorization: "Bearer {{ _.token }}"
        body: '{"version": "{{ _.version }}"}'
```

## Validation Pipeline

1. Solution YAML is loaded into `Action.Inputs` as `map[string]any`.
2. Engine resolves the provider descriptor.
3. `Schema.Parameters` is used to validate the inputs:
   - Check required parameters are present
   - Validate parameter types match definitions
4. `Decode` converts the validated map into the typed struct.
5. The provider `Execute` receives the typed struct along with the data context containing resolver values and previous action outputs.

This keeps Go strongly typed while preserving YAML flexibility. The data context (`dataCtx`) allows providers to access resolved values that were referenced in input expressions (like `{{ _.projectRoot }}`), enabling dynamic behavior based on the current execution state.

## Provider Plugins

Third-party providers register descriptors at runtime:

```go
func RegisterProvider(p Provider) {
    registry[p.Descriptor().Name] = p
}
```

Plugins must expose a symbol like `func Provider() Provider` so the host can load descriptors dynamically.

## Related Docs

- [Provider Schema](../schemas/provider-schema.md) — user-facing schema fields
- [Authentication Reference](../reference/auth.md) — example of pluggable providers for auth
- [CLI Providers Guide](../guides/06-providers.md)
