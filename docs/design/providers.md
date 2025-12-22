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
    Execute(ctx Context, input any) (any, error)
}

type ProviderDescriptor struct {
    Name    string
    Kind    string            // e.g. "sh", "api", "filesystem"
    Schema  cuesdk.Value      // JSON/CUE schema for inputs
    Decode  func(map[string]any) (any, error)
}
```

- `Schema` captures the provider-specific fields (`dir`, `cmd`, … for shell; `endpoint`, `method`, … for API).
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
        Schema: cuesdk.MustCompile(`{
            dir?: string,
            cmd: [...string],
            env?: [...string],
        }`),
        Decode: func(raw map[string]any) (any, error) {
            var input ShellInputs
            if err := mapstructure.Decode(raw, &input); err != nil {
                return nil, err
            }
            return input, nil
        },
    }
}

func (shellProvider) Execute(ctx Context, input any) (any, error) {
    in := input.(ShellInputs)
    // run commands in in.Dir with in.Cmd, etc.
    return nil, nil
}
```

The solution author simply writes:

```yaml
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
```

Each provider defines its own struct. Solutions stay consistent:

```yaml
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
3. `Schema` is used to validate the inputs (via CUE/JSON schema).
4. `Decode` converts the map into the typed struct.
5. The provider `Execute` receives the typed struct.

This keeps Go strongly typed while preserving YAML flexibility.

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
