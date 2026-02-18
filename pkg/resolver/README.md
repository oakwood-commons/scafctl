# Resolver Package

The `resolver` package provides a powerful, dependency-aware configuration resolution system for scafctl. It enables declarative value resolution through providers, with support for type coercion, conditional execution, transformations, validation, and concurrent execution.

## Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Architecture](#architecture)
  - [Resolver Structure](#resolver-structure)
  - [Execution Phases](#execution-phases)
  - [Dependency Resolution](#dependency-resolution)
- [API Reference](#api-reference)
  - [Resolver Type](#resolver-type)
  - [Type System](#type-system)
  - [Executor](#executor)
  - [ValueRef](#valueref)
- [Providers](#providers)
- [Conditional Execution](#conditional-execution)
- [Error Handling](#error-handling)
- [Metrics & Observability](#metrics--observability)
- [Snapshots](#snapshots)
- [Best Practices](#best-practices)
- [Examples](#examples)

## Overview

The resolver package enables dynamic configuration resolution through a declarative YAML-based system. Resolvers can:

- **Fetch values** from various sources (environment, HTTP, files, parameters)
- **Transform values** through CEL expressions or Go templates
- **Validate values** against rules (regex, CEL expressions)
- **Execute concurrently** when dependencies allow
- **Handle errors gracefully** with configurable behavior

## Features

- **Dependency-based execution**: Automatic DAG construction and phase-based execution
- **Type coercion**: Automatic conversion between types (string, int, float, bool, array, time, duration)
- **Conditional execution**: Skip resolvers based on runtime conditions
- **Concurrent execution**: Resolvers without dependencies run in parallel
- **Value transformations**: Transform values using CEL or Go templates
- **Validation**: Validate values against regex patterns or CEL expressions
- **Sensitive values**: Automatic redaction of sensitive data in logs and errors
- **Metrics collection**: Execution timing, provider calls, and resource usage
- **Snapshot support**: Capture execution state for debugging and testing

## Installation

```go
import "github.com/oakwood-commons/scafctl/pkg/resolver"
```

## Quick Start

### Basic Resolver Definition

```yaml
name: my-solution
version: "1.0.0"
spec:
  resolvers:
    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: development

    port:
      type: int
      resolve:
        with:
          - provider: static
            inputs:
              value: 8080

    server-url:
      type: string
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "'http://localhost:' + string(_.port)"
```

### Programmatic Execution

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/oakwood-commons/scafctl/pkg/provider/builtin"
    "github.com/oakwood-commons/scafctl/pkg/resolver"
    "github.com/oakwood-commons/scafctl/pkg/solution"
)

func main() {
    ctx := context.Background()

    // Get the default provider registry
    registry, err := builtin.DefaultRegistry(ctx)
    if err != nil {
        panic(err)
    }

    // Create executor with options
    executor := resolver.NewExecutor(registry,
        resolver.WithDefaultTimeout(30*time.Second),
        resolver.WithPhaseTimeout(5*time.Minute),
        resolver.WithMaxValueSize(10*1024*1024), // 10MB
    )

    // Define resolvers
    resolvers := []*resolver.Resolver{
        {
            Name: "greeting",
            Type: resolver.TypeString,
            Resolve: &resolver.ResolvePhase{
                With: []resolver.ProviderSource{
                    {
                        Provider: "static",
                        Inputs: map[string]resolver.ValueRef{
                            "value": {Literal: "Hello, World!"},
                        },
                    },
                },
            },
        },
    }

    // Execute
    result, err := executor.Execute(ctx, resolvers, nil)
    if err != nil {
        panic(err)
    }

    // Access resolved values
    fmt.Println(result.Resolvers["greeting"].Value) // "Hello, World!"
}
```

## Architecture

### Resolver Structure

Each resolver has three optional phases that execute in order:

```
┌─────────────────────────────────────────────────────────┐
│                      Resolver                           │
├─────────────────────────────────────────────────────────┤
│  1. Resolve Phase (required)                            │
│     - Fetch initial value from providers                │
│     - Multiple sources tried until success              │
│                                                         │
│  2. Transform Phase (optional)                          │
│     - Apply transformations to the value                │
│     - CEL expressions or Go templates                   │
│     - Multiple steps executed sequentially              │
│                                                         │
│  3. Validate Phase (optional)                           │
│     - Validate the final value                          │
│     - All rules run (aggregated errors)                 │
│     - Regex patterns or CEL expressions                 │
└─────────────────────────────────────────────────────────┘
```

### Execution Phases

Resolvers are automatically organized into execution phases based on dependencies:

```
Phase 1: [env, region]          ← No dependencies, run concurrently
         ↓
Phase 2: [port, config-url]     ← Depend on phase 1 resolvers, run concurrently
         ↓
Phase 3: [server-config]        ← Depends on phase 1 and 2 resolvers
```

### Dependency Resolution

Dependencies are automatically extracted from:

- **CEL expressions**: `_.other_resolver` references
- **Go templates**: `{{.other_resolver}}` references (with default delimiters)
- **Resolver references**: `rslvr: other_resolver` in ValueRef

**Explicit Dependencies:**

Use the `dependsOn` field when automatic extraction isn't possible (e.g., templates loaded from files):

```yaml
name: formatted-output
dependsOn:
  - config
  - credentials
resolve:
  with:
    - provider: file
      inputs:
        path: "/path/to/template.tmpl"
transform:
  with:
    - provider: go-template
      inputs:
        template:
          rslvr: formatted-output
```

Explicit `dependsOn` dependencies are merged with auto-extracted dependencies.

**Provider-Specific Extraction:**

Providers can implement custom dependency extraction via `ExtractDependencies` on their `Descriptor`. This is useful for providers with custom input formats:

- **cel provider**: Extracts `_.resolverName` patterns from the `expression` input using CEL AST parsing
- **go-template provider**: Extracts references from the `template` input, respecting custom `leftDelim`/`rightDelim` settings

See `provider.Descriptor.ExtractDependencies` for implementation details.

## API Reference

### Resolver Type

```go
// Resolver represents a single resolver definition
type Resolver struct {
    Name        string         `json:"name" yaml:"name"`
    Description string         `json:"description,omitempty" yaml:"description,omitempty"`
    Sensitive   bool           `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
    Type        Type           `json:"type,omitempty" yaml:"type,omitempty"`
    When        *Condition     `json:"when,omitempty" yaml:"when,omitempty"`
    DependsOn   []string       `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`  // Explicit dependencies
    Timeout     *time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
    Resolve     *ResolvePhase  `json:"resolve" yaml:"resolve"`
    Transform   *TransformPhase `json:"transform,omitempty" yaml:"transform,omitempty"`
    Validate    *ValidatePhase  `json:"validate,omitempty" yaml:"validate,omitempty"`
}
```

### Type System

Supported types with automatic coercion:

| Type | Go Type | YAML Example |
|------|---------|--------------|
| `string` | `string` | `type: string` |
| `int` | `int64` | `type: int` |
| `float` | `float64` | `type: float` |
| `bool` | `bool` | `type: bool` |
| `array` | `[]any` | `type: array` |
| `time` | `time.Time` | `type: time` |
| `duration` | `time.Duration` | `type: duration` |
| `any` | `any` | `type: any` (default) |

Type aliases:
- `timestamp`, `datetime` → `time`
- `integer` → `int`
- `number` → `float`
- `boolean` → `bool`

### Executor

```go
// NewExecutor creates a new resolver executor
func NewExecutor(registry RegistryInterface, opts ...ExecutorOption) *Executor

// ExecutorOptions
func WithDefaultTimeout(timeout time.Duration) ExecutorOption
func WithPhaseTimeout(timeout time.Duration) ExecutorOption
func WithMaxConcurrency(max int) ExecutorOption
func WithWarnValueSize(bytes int64) ExecutorOption
func WithMaxValueSize(bytes int64) ExecutorOption

// Execute runs all resolvers and returns results
func (e *Executor) Execute(ctx context.Context, resolvers []*Resolver, params map[string]any) (*ExecutionResult, error)
```

### ValueRef

ValueRef enables flexible value references in YAML:

```yaml
# Literal value
inputs:
  value: "hello"

# Resolver reference
inputs:
  config:
    rslvr: other-resolver

# CEL expression
inputs:
  calculated:
    expr: "_.count * 2"

# Go template
inputs:
  message:
    tmpl: "Hello, {{.name}}!"
```

## Providers

Built-in providers available in the default registry:

| Provider | Purpose | Example |
|----------|---------|---------|
| `static` | Static values | `value: "hello"` |
| `parameter` | CLI parameters | `name: env, default: prod` |
| `env` | Environment variables | `name: HOME` |
| `cel` | CEL expressions | `expression: _.a + _.b` |
| `http` | HTTP requests | `url: https://api.example.com` |
| `file` | File contents | `path: ./config.json` |
| `exec` | Command execution | `command: echo hello` |
| `git` | Git operations | `operation: branch` |
| `validation` | Value validation | `pattern: ^[a-z]+$` |
| `sleep` | Delay execution | `duration: 1s` |
| `debug` | Debug utilities | `message: checkpoint` |

## Conditional Execution

### Resolver-level conditions

```yaml
resolvers:
  prod-config:
    when:
      expr: "_.environment == 'production'"
    resolve:
      with:
        - provider: http
          inputs:
            url: https://config.prod.example.com/settings
```

### Phase-level conditions

```yaml
resolvers:
  config:
    resolve:
      when:
        expr: "_.use_remote == true"
      with:
        - provider: http
          inputs:
            url: https://config.example.com
    transform:
      when:
        expr: "_.environment == 'prod'"
      with:
        - provider: cel
          inputs:
            expression:
              expr: "__self.merge({'ssl': true})"
```

## Error Handling

### Error Behavior

Control how errors are handled:

```yaml
resolvers:
  config:
    resolve:
      with:
        - provider: http
          error_behavior: continue  # Try next source on error
          inputs:
            url: https://primary.example.com
        - provider: http
          inputs:
            url: https://fallback.example.com
```

### Error Types

```go
// ExecutionError - Error during resolver execution
type ExecutionError struct {
    ResolverName string
    Phase        string  // "resolve", "transform", "validate"
    Step         int
    Provider     string
    Cause        error
}

// AggregatedValidationError - Multiple validation failures
type AggregatedValidationError struct {
    ResolverName string
    Failures     []ValidationFailure
}

// CircularDependencyError - Cycle detected in dependencies
type CircularDependencyError struct {
    Cycle []string
}
```

## Metrics & Observability

### Execution Metrics

```go
result, _ := executor.Execute(ctx, resolvers, params)

// Access metrics
fmt.Println(result.Metrics.TotalResolvers)
fmt.Println(result.Metrics.TotalPhases)
fmt.Println(result.Metrics.TotalDuration)

// Per-resolver metrics
for name, r := range result.Resolvers {
    fmt.Printf("%s: %v (calls: %d)\n", name, r.Duration, r.ProviderCalls)
}
```

### Metrics Summary

```go
summary := resolver.NewMetricsSummary(result)
summary.Print(os.Stdout)
```

Output:
```
=== Execution Summary ===
Total resolvers: 5
Successful: 4
Failed: 1
Skipped: 0
Total duration: 1.234s

=== Phase Breakdown ===
Phase 1: 3 resolvers (456ms)
Phase 2: 2 resolvers (778ms)
```

## Snapshots

Capture execution state for debugging and testing:

```go
// Capture snapshot
snapshot := resolver.CaptureSnapshot(ctx, 
    "my-solution", "1.0.0", "v0.1.0",
    params, duration, "success")

// Save to file
data, _ := json.MarshalIndent(snapshot, "", "  ")
os.WriteFile("snapshot.json", data, 0644)

// Load and compare
loaded, _ := resolver.LoadSnapshot("snapshot.json")
diff := resolver.DiffSnapshots(expected, actual)
```

## Best Practices

### 1. Use Appropriate Types

```yaml
# ✅ Good - explicit types
port:
  type: int
  resolve: ...

# ❌ Avoid - relying on default 'any' type
port:
  resolve: ...
```

### 2. Handle Sensitive Values

```yaml
# ✅ Good - mark sensitive values
api-key:
  sensitive: true
  resolve:
    with:
      - provider: env
        inputs:
          name: API_KEY
```

### 3. Use Fallback Sources

```yaml
# ✅ Good - multiple sources with fallback
config:
  resolve:
    with:
      - provider: http
        error_behavior: continue
        inputs:
          url: https://config.primary.com
      - provider: file
        inputs:
          path: ./fallback-config.json
```

### 4. Validate Critical Values

```yaml
# ✅ Good - validate important values
port:
  type: int
  resolve: ...
  validate:
    with:
      - provider: validation
        inputs:
          expression:
            expr: "__self >= 1 && __self <= 65535"
          message: "Port must be between 1 and 65535"
```

### 5. Set Appropriate Timeouts

```yaml
# ✅ Good - set timeout for slow operations
external-config:
  timeout: 60s  # Override default 30s
  resolve:
    with:
      - provider: http
        inputs:
          url: https://slow-api.example.com
```

## Examples

### Configuration with Environment Override

```yaml
spec:
  resolvers:
    base-config:
      type: any
      resolve:
        with:
          - provider: file
            inputs:
              path: ./config.yaml
              format: yaml

    environment:
      type: string
      resolve:
        with:
          - provider: parameter
            inputs:
              key: env
          - provider: static
            inputs:
              value: development

    final-config:
      type: any
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: |
                  _.base_config.merge({
                    'environment': _.environment,
                    'debug': _.environment != 'production'
                  })
```

### Multi-stage Pipeline

```yaml
spec:
  resolvers:
    raw-data:
      resolve:
        with:
          - provider: http
            inputs:
              url: https://api.example.com/data

    parsed-data:
      type: any
      resolve:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "_.raw_data.fromJson()"
      transform:
        with:
          - provider: cel
            inputs:
              expression:
                expr: "__self.items.filter(i, i.active == true)"
      validate:
        with:
          - provider: validation
            inputs:
              expression:
                expr: "size(__self) > 0"
              message: "No active items found"
```

### Conditional Feature Flags

```yaml
spec:
  resolvers:
    feature-flags:
      type: any
      resolve:
        with:
          - provider: http
            error_behavior: continue
            inputs:
              url: https://flags.example.com/api/flags
          - provider: static
            inputs:
              value:
                new_ui: false
                analytics: true

    ui-version:
      type: string
      when:
        expr: "_.feature_flags.new_ui == true"
      resolve:
        with:
          - provider: static
            inputs:
              value: "v2"
```

## Thread Safety

The resolver package is designed for concurrent use:

- `Executor` is safe for concurrent `Execute()` calls
- `ResolverContext` uses `sync.Map` for thread-safe value storage
- Metrics collection is thread-safe

## Related Packages

- [`pkg/provider`](../provider/) - Provider interface and registry
- [`pkg/provider/builtin`](../provider/builtin/) - Built-in provider implementations
- [`pkg/celexp`](../celexp/) - CEL expression compilation and evaluation
- [`pkg/gotmpl`](../gotmpl/) - Go template processing
- [`pkg/dag`](../dag/) - DAG construction and traversal
- [`pkg/solution`](../solution/) - Solution definition and loading

