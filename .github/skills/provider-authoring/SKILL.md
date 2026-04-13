---
name: provider-authoring
description: "How to create new scafctl providers. Interface, Descriptor, capabilities, schema, testing patterns, and registration. Use when creating a new provider or modifying the provider system."
---

# Provider Authoring Guide

## Provider Interface

Every provider implements two methods:

```go
type Provider interface {
    Descriptor() *Descriptor
    Execute(ctx context.Context, input any) (*Output, error)
}
```

## Package Structure

```
pkg/provider/builtin/<name>provider/
    <name>.go                    # Provider implementation
    <name>_test.go               # Unit tests
    <name>_benchmark_test.go     # Benchmarks (required)
    mock.go                      # Mock ops interface
```

## Step-by-Step Creation

### 1. Define Constants

```go
package myprovider

const (
    ProviderName = "my-provider"
    Version      = "1.0.0"
)
```

### 2. Define Ops Interface (for testability)

```go
// Ops abstracts external dependencies for testing.
type Ops interface {
    DoThing(ctx context.Context, input string) (string, error)
}

// DefaultOps is the real implementation.
type DefaultOps struct{}

func (d DefaultOps) DoThing(ctx context.Context, input string) (string, error) {
    // Real implementation
}
```

### 3. Define Input Struct

Use Huma validation tags on all fields:

```go
type Input struct {
    Name    string `json:"name" yaml:"name" doc:"Resource name" maxLength:"253" example:"my-resource"`
    Count   int    `json:"count" yaml:"count" doc:"Number of items" minimum:"1" maximum:"100" example:"10"`
    Enabled bool   `json:"enabled,omitempty" yaml:"enabled,omitempty" doc:"Enable feature"`
}
```

### 4. Implement Provider

```go
type MyProvider struct {
    ops Ops
}

func New(ops Ops) *MyProvider {
    return &MyProvider{ops: ops}
}

func NewDefault() *MyProvider {
    return New(DefaultOps{})
}
```

### 5. Implement Descriptor()

```go
func (p *MyProvider) Descriptor() *provider.Descriptor {
    return &provider.Descriptor{
        Name:        ProviderName,
        DisplayName: "My Provider",
        Description: "Does something useful",
        APIVersion:  "scafctl.io/v1",
        Version:     Version,
        Schema:      schemahelper.GenerateSchema(Input{}),
        Capabilities: []provider.Capability{
            provider.CapabilityFrom,          // Can be used in resolve phase
        },
        Category: "utility",
        Tags:     []string{"utility"},
    }
}
```

### 6. Implement Execute()

```go
func (p *MyProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    in, ok := input.(*Input)
    if !ok {
        return nil, fmt.Errorf("invalid input type: %T", input)
    }

    result, err := p.ops.DoThing(ctx, in.Name)
    if err != nil {
        return nil, fmt.Errorf("failed to do thing: %w", err)
    }

    return &provider.Output{Value: result}, nil
}
```

### 7. Register the Provider

In `pkg/provider/builtin/builtin.go`:

```go
import "github.com/abaker/scafctl/pkg/provider/builtin/myprovider"

func init() {
    Register(myprovider.NewDefault())
}
```

## Capabilities

| Capability | Phase | Required Output Fields |
|-----------|-------|----------------------|
| `CapabilityFrom` | Resolve | None (any value) |
| `CapabilityTransform` | Transform | None (transformed value) |
| `CapabilityValidation` | Validate | `valid` (bool), `errors` ([]string) |
| `CapabilityAuthentication` | Auth | `authenticated` (bool), `token` (string) |
| `CapabilityAction` | Action | `success` (bool) |

A provider can have multiple capabilities.

## Descriptor Fields Reference

| Field | Type | Required | Purpose |
|-------|------|----------|---------|
| Name | string | Yes | Unique provider identifier |
| DisplayName | string | Yes | Human-readable name |
| Description | string | Yes | What the provider does |
| APIVersion | string | Yes | Always `"scafctl.io/v1"` |
| Version | string | Yes | Semver version |
| Schema | *jsonschema.Schema | Yes | Input JSON schema (via schemahelper) |
| Capabilities | []Capability | Yes | What phases this provider supports |
| OutputSchemas | map[Capability]*jsonschema.Schema | No | Per-capability output schemas |
| SensitiveFields | []string | No | Fields to redact in logs |
| Category | string | No | Grouping category |
| Tags | []string | No | Discovery tags |
| Icon | string | No | Icon URL |
| Links | []Link | No | Documentation links |
| Examples | []Example | No | Usage examples |
| Decode | func(any) (any, error) | No | Custom type converter |
| ExtractDependencies | func(any) []string | No | Custom dependency extraction |
| WhatIf | func(any) string | No | Dry-run description |
| Beta | bool | No | Mark as beta |
| IsDeprecated | bool | No | Mark as deprecated |

## Testing Patterns

### Unit Tests

Test via the ops interface -- mock external dependencies:

```go
func TestExecute(t *testing.T) {
    tests := []struct {
        name    string
        input   *Input
        ops     Ops       // Mock ops
        want    any
        wantErr bool
    }{
        {
            name:  "success",
            input: &Input{Name: "test"},
            ops:   &mockOps{result: "output"},
            want:  "output",
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            p := New(tt.ops)
            got, err := p.Execute(context.Background(), tt.input)
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            assert.NoError(t, err)
            assert.Equal(t, tt.want, got.Value)
        })
    }
}
```

### Benchmark Tests (Required)

```go
func BenchmarkExecute(b *testing.B) {
    p := New(&mockOps{result: "output"})
    input := &Input{Name: "bench"}
    ctx := context.Background()

    b.ResetTimer()
    for b.Loop() {
        _, _ = p.Execute(ctx, input)
    }
}
```

### Mock File (mock.go)

```go
type mockOps struct {
    result string
    err    error
}

func (m *mockOps) DoThing(_ context.Context, _ string) (string, error) {
    return m.result, m.err
}
```

## Existing Providers Reference

| Provider | Package | Capability | Key Pattern |
|----------|---------|-----------|-------------|
| parameter | parameterprovider | from | Interactive prompts via terminal |
| env | envprovider | from | Env var reading with expand option |
| static | staticprovider | from | Pass-through literal values |
| file | fileprovider | from | File I/O with encoding options |
| exec | execprovider | from | Command execution with expand |
| http | httpprovider | from | HTTP client with auth support |
| cel | celprovider | transform | CEL expression evaluation |
| gotmpl | gotmplprovider | transform | Go template rendering |
| validation | validationprovider | validation | Rule-based input validation |
| directory | directoryprovider | action | File/directory operations |
| message | messageprovider | action | Terminal output messages |

## Key Packages

- `pkg/provider/`: Provider interface, Descriptor, Output, Capability constants
- `pkg/provider/builtin/`: Registration and built-in providers
- `pkg/schema/schemahelper/`: JSON schema generation from structs
