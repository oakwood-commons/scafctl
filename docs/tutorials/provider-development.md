---
title: "Provider Development Guide"
weight: 120
---

# Provider Development Guide

This guide explains how to create custom providers for scafctl. Providers are the building blocks that perform operations like fetching data, transforming values, validating inputs, and executing actions.

## Provider Architecture

Providers implement a simple interface:

```go
type Provider interface {
    // Descriptor returns metadata, schema, and capabilities
    Descriptor() *Descriptor

    // Execute performs the provider's operation
    Execute(ctx context.Context, input any) (*Output, error)
}
```

## Quick Start: Minimal Provider

Here's the simplest possible provider:

```go
package myprovider

import (
    "context"

    "github.com/Masterminds/semver/v3"
    "github.com/google/jsonschema-go/jsonschema"
    "github.com/oakwood-commons/scafctl/pkg/provider"
    "github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

type MyProvider struct{}

func New() *MyProvider {
    return &MyProvider{}
}

func (p *MyProvider) Descriptor() *provider.Descriptor {
    return &provider.Descriptor{
        Name:        "my-provider",
        DisplayName: "My Custom Provider",
        APIVersion:  "v1",
        Version:     semver.MustParse("1.0.0"),
        Description: "Does something useful for resolvers and actions",
        Schema: schemahelper.ObjectSchema([]string{"input"}, map[string]*jsonschema.Schema{
            "input": schemahelper.StringProp("The input value to process"),
        }),
        OutputSchemas: map[provider.Capability]*jsonschema.Schema{
            provider.CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
                "result": schemahelper.StringProp("The processed result"),
            }),
        },
        Capabilities: []provider.Capability{provider.CapabilityFrom},
        Category:     "utility",
        MockBehavior: "Returns a mock result without actual processing",
    }
}

func (p *MyProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    inputs := input.(map[string]any)
    value := inputs["input"].(string)
    
    return &provider.Output{
        Data: map[string]any{
            "result": "Processed: " + value,
        },
    }, nil
}
```

## Provider Descriptor

The `Descriptor` defines everything about your provider:

### Required Fields

| Field | Description |
|-------|-------------|
| `Name` | Unique identifier (lowercase, hyphens only) |
| `APIVersion` | API contract version (e.g., `"v1"`) |
| `Version` | Semantic version of implementation |
| `Description` | What the provider does (10-500 chars) |
| `Schema` | Input schema (`*jsonschema.Schema`) |
| `OutputSchemas` | Output schemas per capability (`map[Capability]*jsonschema.Schema`) |
| `Capabilities` | What operations the provider supports |
| `MockBehavior` | Description of dry-run behavior |

### Capabilities

Providers declare which contexts they can operate in:

| Capability | Usage | Description |
|------------|-------|-------------|
| `from` | Resolvers | Fetch/generate data (HTTP, file, env, etc.) |
| `transform` | Resolvers | Transform resolver values |
| `validation` | Resolvers | Validate resolver values |
| `action` | Actions | Perform side effects (deploy, notify, etc.) |
| `authentication` | Auth | Handle authentication flows |

### Schema Definition

Define input properties with validation using the `schemahelper` package:

```go
import (
    "encoding/json"

    "github.com/google/jsonschema-go/jsonschema"
    "github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

Schema: schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
    "url": schemahelper.StringProp("The URL to fetch",
        schemahelper.WithFormat("uri"),
        schemahelper.WithExample("https://api.example.com/data"),
    ),
    "timeout": schemahelper.IntProp("Request timeout in seconds",
        schemahelper.WithDefault(json.RawMessage(`30`)),
        schemahelper.WithMinimum(1),
        schemahelper.WithMaximum(300),
    ),
    "headers": schemahelper.AnyProp("HTTP headers as key-value pairs"),
}),
```

### Property Types

| JSON Schema Type | Helper | Go Equivalent | Description |
|------------------|--------|--------------|-------------|
| `"string"` | `schemahelper.StringProp` | `string` | Text values |
| `"integer"` | `schemahelper.IntProp` | `int64` | Integer numbers |
| `"number"` | `schemahelper.NumberProp` | `float64` | Decimal numbers |
| `"boolean"` | `schemahelper.BoolProp` | `bool` | Boolean values |
| `"array"` | `schemahelper.ArrayProp` | `[]any` | List of values |
| (omitted) | `schemahelper.AnyProp` | `any` | Any type (object, nested, etc.) |

### Property Constraints

Constraints are applied via option functions on the schema helpers:

```go
// String property with constraints
schemahelper.StringProp("Description of the field",
    schemahelper.WithDefault(json.RawMessage(`"default"`)), // Default if not provided
    schemahelper.WithMinLength(1),                          // Minimum string length
    schemahelper.WithMaxLength(100),                        // Maximum string length
    schemahelper.WithPattern("^[a-z]+$"),                   // Regex pattern
    schemahelper.WithEnum("a", "b"),                        // Allowed values
    schemahelper.WithFormat("uri"),                         // Format hint (uri, email, uuid)
    schemahelper.WithExample("example-value"),              // Example value
    schemahelper.WithDeprecated(),                          // Mark as deprecated
)

// Integer property with constraints
schemahelper.IntProp("Numeric field",
    schemahelper.WithMinimum(0),    // Minimum number
    schemahelper.WithMaximum(100),  // Maximum number
)

// Array property with constraints
schemahelper.ArrayProp("List field",
    schemahelper.WithMinItems(1),   // Minimum array items
    schemahelper.WithMaxItems(10),  // Maximum array items
)
```

Required fields are declared on the parent object schema, not per-property:

```go
// "url" is required, "timeout" and "headers" are optional
schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{ ... })
```

Sensitive fields are declared on the Descriptor (see [Mark Sensitive Data](#4-mark-sensitive-data)).

## Execute Method

The `Execute` method receives validated inputs and returns an `Output`:

```go
func (p *MyProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    // Type assert to map (unless using Decode)
    inputs, ok := input.(map[string]any)
    if !ok {
        return nil, fmt.Errorf("expected map[string]any, got %T", input)
    }

    // Extract inputs (already validated by schema)
    url := inputs["url"].(string)
    timeout := 30 // default
    if t, ok := inputs["timeout"].(int); ok {
        timeout = t
    }

    // Check for dry-run mode
    if provider.DryRunFromContext(ctx) {
        return &provider.Output{
            Data: map[string]any{"mocked": true},
            Metadata: map[string]any{"mode": "dry-run"},
        }, nil
    }

    // Perform actual operation
    result, err := doSomething(url, timeout)
    if err != nil {
        return nil, fmt.Errorf("operation failed: %w", err)
    }

    return &provider.Output{
        Data: result,
        Warnings: []string{"optional warning message"},
        Metadata: map[string]any{"requestId": "abc123"},
    }, nil
}
```

### Context Values

Access context information:

```go
// Check if this is a dry-run
isDryRun := provider.DryRunFromContext(ctx)

// Get execution mode (from, transform, validation, action)
mode := provider.ExecutionModeFromContext(ctx)

// Access resolved values from other resolvers
resolverCtx := provider.ResolverContextFromContext(ctx)
value := resolverCtx["otherResolver"]

// Get logger
lgr := logger.FromContext(ctx)
lgr.V(1).Info("processing", "url", url)
```

## Using Typed Inputs (Decode)

For complex providers, use typed structs instead of `map[string]any`:

```go
type HTTPInput struct {
    URL     string            `json:"url"`
    Method  string            `json:"method"`
    Headers map[string]string `json:"headers"`
    Body    any               `json:"body"`
    Timeout int               `json:"timeout"`
}

func (p *HTTPProvider) Descriptor() *provider.Descriptor {
    return &provider.Descriptor{
        // ... other fields ...
        Decode: func(inputs map[string]any) (any, error) {
            var cfg HTTPInput
            data, _ := json.Marshal(inputs)
            if err := json.Unmarshal(data, &cfg); err != nil {
                return nil, err
            }
            // Apply defaults
            if cfg.Method == "" {
                cfg.Method = "GET"
            }
            if cfg.Timeout == 0 {
                cfg.Timeout = 30
            }
            return &cfg, nil
        },
    }
}

func (p *HTTPProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    // Input is now typed!
    cfg := input.(*HTTPInput)
    
    resp, err := http.Get(cfg.URL)
    // ...
}
```

## Registering Providers

### Built-in Providers

Add your provider to the builtin registry:

```go
// pkg/provider/builtin/builtin.go

func DefaultRegistry() (*provider.Registry, error) {
    registry := provider.NewRegistry()
    
    // Register built-in providers
    registry.Register(envprovider.New())
    registry.Register(httpprovider.New())
    registry.Register(myprovider.New())  // Add your provider
    
    return registry, nil
}
```

### Testing Your Provider

```go
func TestMyProvider_Execute(t *testing.T) {
    p := New()
    
    // Test descriptor
    desc := p.Descriptor()
    assert.Equal(t, "my-provider", desc.Name)
    assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
    
    // Test execution
    ctx := context.Background()
    output, err := p.Execute(ctx, map[string]any{
        "input": "test value",
    })
    
    require.NoError(t, err)
    assert.NotNil(t, output.Data)
    
    result := output.Data.(map[string]any)
    assert.Equal(t, "Processed: test value", result["result"])
}

func TestMyProvider_DryRun(t *testing.T) {
    p := New()
    ctx := provider.WithDryRun(context.Background(), true)
    
    output, err := p.Execute(ctx, map[string]any{
        "input": "test",
    })
    
    require.NoError(t, err)
    // Verify mock behavior
}
```

## Output Schema Requirements

Certain capabilities require specific output fields:

### Validation Capability

```go
OutputSchemas: map[provider.Capability]*jsonschema.Schema{
    provider.CapabilityValidation: schemahelper.ObjectSchema([]string{"valid"}, map[string]*jsonschema.Schema{
        "valid":  schemahelper.BoolProp("Whether validation passed"),
        "errors": schemahelper.ArrayProp("Validation error messages"),
    }),
},
```

### Action Capability

```go
OutputSchemas: map[provider.Capability]*jsonschema.Schema{
    provider.CapabilityAction: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
        "success": schemahelper.BoolProp("Whether action succeeded"),
    }),
},
```

## Best Practices

### 1. Handle Dry-Run Mode

Always check for dry-run and return realistic mock data:

```go
if provider.DryRunFromContext(ctx) {
    return &provider.Output{
        Data: map[string]any{
            "id":      "mock-123",
            "status":  "created",
            "message": "[DRY-RUN] Would create resource",
        },
    }, nil
}
```

### 2. Use Proper Error Messages

```go
// Good: Context and wrapped error
return nil, fmt.Errorf("http: failed to fetch %s: %w", url, err)

// Bad: Generic message
return nil, err
```

### 3. Log at Appropriate Levels

```go
lgr := logger.FromContext(ctx)
lgr.V(1).Info("starting request", "url", url)      // Debug
lgr.V(0).Info("request complete", "status", 200)   // Info
lgr.Error(err, "request failed")                    // Error
```

### 4. Mark Sensitive Data

Use `SensitiveFields` on the Descriptor to indicate which input fields should be masked in logs:

```go
func (p *MyProvider) Descriptor() *provider.Descriptor {
    return &provider.Descriptor{
        // ... other fields ...
        SensitiveFields: []string{"password", "token"},  // These fields will be masked in logs
    }
}
```

### 5. Provide Good Examples

```go
Examples: []provider.Example{
    {
        Name:        "Basic usage",
        Description: "Fetch data from an API",
        YAML: `name: api-data
resolve:
  from:
    provider: my-provider
    inputs:
      url: https://api.example.com/data`,
    },
},
```

## Directory Structure

```
pkg/provider/builtin/myprovider/
├── my_provider.go      # Main implementation
├── my_provider_test.go # Tests
└── README.md           # Documentation (optional)
```

## Complete Example: Rate Limiter Provider

```go
package ratelimitprovider

import (
    "context"
    "fmt"
    "sync"
    "time"

    "github.com/Masterminds/semver/v3"
    "github.com/google/jsonschema-go/jsonschema"
    "github.com/oakwood-commons/scafctl/pkg/logger"
    "github.com/oakwood-commons/scafctl/pkg/provider"
    "github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

type RateLimitProvider struct {
    mu        sync.Mutex
    lastCall  time.Time
    callCount int
}

func New() *RateLimitProvider {
    return &RateLimitProvider{}
}

func (p *RateLimitProvider) Descriptor() *provider.Descriptor {
    return &provider.Descriptor{
        Name:        "rate-limit",
        DisplayName: "Rate Limiter",
        APIVersion:  "v1",
        Version:     semver.MustParse("1.0.0"),
        Description: "Enforces rate limits on resolver execution. Use as a transform step.",
        Category:    "utility",
        Tags:        []string{"rate-limit", "throttle", "control"},
        Schema: schemahelper.ObjectSchema([]string{"value", "maxPerMinute"}, map[string]*jsonschema.Schema{
            "value":        schemahelper.AnyProp("The value to pass through"),
            "maxPerMinute": schemahelper.IntProp("Maximum calls per minute",
                schemahelper.WithMinimum(1),
                schemahelper.WithMaximum(1000),
                schemahelper.WithExample(60),
            ),
        }),
        OutputSchemas: map[provider.Capability]*jsonschema.Schema{
            provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
                "value":     schemahelper.AnyProp("The passed-through value"),
                "remaining": schemahelper.IntProp("Remaining calls in current window"),
            }),
        },
        Capabilities: []provider.Capability{provider.CapabilityTransform},
        MockBehavior: "Returns the value without enforcing rate limit",
    }
}

func (p *RateLimitProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    lgr := logger.FromContext(ctx)
    inputs := input.(map[string]any)
    
    value := inputs["value"]
    maxPerMin := int(inputs["maxPerMinute"].(int64))
    
    if provider.DryRunFromContext(ctx) {
        return &provider.Output{
            Data: map[string]any{
                "value":     value,
                "remaining": maxPerMin,
            },
        }, nil
    }
    
    p.mu.Lock()
    defer p.mu.Unlock()
    
    now := time.Now()
    if now.Sub(p.lastCall) > time.Minute {
        p.callCount = 0
        p.lastCall = now
    }
    
    if p.callCount >= maxPerMin {
        return nil, fmt.Errorf("rate limit exceeded: %d calls/min", maxPerMin)
    }
    
    p.callCount++
    remaining := maxPerMin - p.callCount
    
    lgr.V(1).Info("rate limit check passed", "remaining", remaining)
    
    return &provider.Output{
        Data: map[string]any{
            "value":     value,
            "remaining": remaining,
        },
    }, nil
}

```

## Next Steps

- See [Plugin Development Guide](plugin-development.md) to create external plugins
- Review [Provider Reference](provider-reference.md) for built-in provider examples
- Check [Contributing Guidelines](../CONTRIBUTING.md) for code standards
