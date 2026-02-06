---
title: "Provider Development Guide"
weight: 60
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
    "github.com/oakwood-commons/scafctl/pkg/provider"
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
        Schema: provider.SchemaDefinition{
            Properties: map[string]provider.PropertyDefinition{
                "input": {
                    Type:        provider.PropertyTypeString,
                    Required:    true,
                    Description: "The input value to process",
                },
            },
        },
        OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
            provider.CapabilityFrom: {
                Properties: map[string]provider.PropertyDefinition{
                    "result": {
                        Type:        provider.PropertyTypeString,
                        Description: "The processed result",
                    },
                },
            },
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
| `Schema` | Input property definitions |
| `OutputSchemas` | Output schemas per capability |
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

Define input properties with validation:

```go
Schema: provider.SchemaDefinition{
    Properties: map[string]provider.PropertyDefinition{
        "url": {
            Type:        provider.PropertyTypeString,
            Required:    true,
            Description: "The URL to fetch",
            Format:      "uri",
            Example:     "https://api.example.com/data",
        },
        "timeout": {
            Type:        provider.PropertyTypeInt,
            Required:    false,
            Description: "Request timeout in seconds",
            Default:     30,
            Minimum:     ptrFloat(1),
            Maximum:     ptrFloat(300),
        },
        "headers": {
            Type:        provider.PropertyTypeAny,
            Required:    false,
            Description: "HTTP headers as key-value pairs",
        },
    },
},
```

### Property Types

| Type | Go Equivalent | Description |
|------|--------------|-------------|
| `string` | `string` | Text values |
| `int` | `int64` | Integer numbers |
| `float` | `float64` | Decimal numbers |
| `bool` | `bool` | Boolean values |
| `array` | `[]any` | List of values |
| `any` | `any` | Any type (object, nested, etc.) |

### Property Constraints

```go
PropertyDefinition{
    Type:        provider.PropertyTypeString,
    Required:    true,           // Must be provided
    Default:     "default",      // Default if not provided
    MinLength:   ptrInt(1),      // Minimum string length
    MaxLength:   ptrInt(100),    // Maximum string length
    Pattern:     "^[a-z]+$",     // Regex pattern
    Minimum:     ptrFloat(0),    // Minimum number
    Maximum:     ptrFloat(100),  // Maximum number
    MinItems:    ptrInt(1),      // Minimum array items
    MaxItems:    ptrInt(10),     // Maximum array items
    Enum:        []any{"a","b"}, // Allowed values
    Format:      "uri",          // Format hint (uri, email, uuid)
    IsSecret:    true,           // Mask in logs
    Deprecated:  false,          // Mark as deprecated
}
```

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
OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
    provider.CapabilityValidation: {
        Properties: map[string]provider.PropertyDefinition{
            "valid": {
                Type:        provider.PropertyTypeBool,
                Required:    true,
                Description: "Whether validation passed",
            },
            "errors": {
                Type:        provider.PropertyTypeArray,
                Description: "Validation error messages",
            },
        },
    },
},
```

### Action Capability

```go
OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
    provider.CapabilityAction: {
        Properties: map[string]provider.PropertyDefinition{
            "success": {
                Type:        provider.PropertyTypeBool,
                Required:    true,
                Description: "Whether action succeeded",
            },
        },
    },
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

```go
PropertyDefinition{
    Type:     provider.PropertyTypeString,
    IsSecret: true,  // Will be masked in logs
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
    "github.com/oakwood-commons/scafctl/pkg/logger"
    "github.com/oakwood-commons/scafctl/pkg/provider"
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
        Schema: provider.SchemaDefinition{
            Properties: map[string]provider.PropertyDefinition{
                "value": {
                    Type:        provider.PropertyTypeAny,
                    Required:    true,
                    Description: "The value to pass through",
                },
                "maxPerMinute": {
                    Type:        provider.PropertyTypeInt,
                    Required:    true,
                    Description: "Maximum calls per minute",
                    Minimum:     ptrFloat(1),
                    Maximum:     ptrFloat(1000),
                    Example:     60,
                },
            },
        },
        OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
            provider.CapabilityTransform: {
                Properties: map[string]provider.PropertyDefinition{
                    "value": {
                        Type:        provider.PropertyTypeAny,
                        Description: "The passed-through value",
                    },
                    "remaining": {
                        Type:        provider.PropertyTypeInt,
                        Description: "Remaining calls in current window",
                    },
                },
            },
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

func ptrFloat(v float64) *float64 { return &v }
```

## Next Steps

- See [Plugin Development Guide](plugin-development.md) to create external plugins
- Review [Provider Reference](provider-reference.md) for built-in provider examples
- Check [Contributing Guidelines](../CONTRIBUTING.md) for code standards
