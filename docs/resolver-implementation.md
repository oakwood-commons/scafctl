# Resolver Implementation Plan

This document outlines the implementation plan for the Resolver feature in scafctl, based on the [design specification](design/resolvers.md) and analysis of the current codebase.

---

## Executive Summary

The resolver system is **substantially implemented** in `pkg/resolver/`. The existing implementation includes:
- ✅ Type system with coercion (`types.go`)
- ✅ ValueRef with custom YAML unmarshalling (`valueref.go`)
- ✅ Resolver context with `sync.Map` (`context.go`)
- ✅ Dependency extraction from expressions/templates (`graph.go`)
- ✅ DAG-based phase execution (`phase.go`, `executor.go`)
- ✅ Metrics tracking (`metrics.go`, `metrics_summary.go`)
- ✅ Snapshot testing support (`snapshot.go`)
- ✅ Structured error types (`errors.go`)
- ✅ Validation aggregation and sensitive value redaction

**Remaining work** focuses on:
1. ~~Integration with Solution loading/parsing~~ ✅ Complete
2. ~~CLI integration (`run solution` command)~~ ✅ Complete
3. ~~Enhanced `__self` handling in transform/validate phases~~ ✅ Complete
4. ~~Error handling & observability~~ ✅ Complete
5. ~~Provider registry improvements~~ ✅ Complete
6. ~~End-to-end testing and validation~~ ✅ Complete
7. ~~Documentation~~ ✅ Complete

**🎉 All implementation phases are complete!**

---

## Decisions Made

| Topic | Decision |
|-------|----------|
| Actions | Deferred entirely - not part of this implementation |
| Catalog support | Local files first - catalog support added later |
| Render command | Deferred - only implement `run` command for now |
| Spec type location | In `pkg/solution/` package |
| Provider registry | Lazy initialization with global default registry |
| Timeout configuration | Configurable via CLI flags |
| Sensitive value handling | Implement redaction in logs, errors, output |
| Conditional resolvers | Skipped resolvers are truly absent from `_` |
| Multiple parameter values | Parameter provider auto-detects arrays |
| Default output format | JSON |
| `--resolve-all` flag | Ensures all resolvers run (normal `when:` evaluation) |
| Resolver selection | Implement `--only <resolver-name>` flag |
| Exit codes | Implement specific exit codes |
| Progress output | Implement `--progress` flag using `vbauerster/mpb` library |
| Solution file discovery | Use existing `FindSolution()` function |
| Stdin support | Yes, `-f -` reads from stdin |
| Parameter file support | Yes, `-r @params.yaml` (YAML and JSON supported) |
| Output to file | Rely on shell redirection |
| Value size warnings | Implement with CLI flags |

---

## Current State Analysis

### Existing Implementation (`pkg/resolver/`)

| File | Purpose | Status |
|------|---------|--------|
| [types.go](../pkg/resolver/types.go) | Type definitions, coercion logic | ✅ Complete |
| [valueref.go](../pkg/resolver/valueref.go) | ValueRef with YAML unmarshalling | ✅ Complete |
| [context.go](../pkg/resolver/context.go) | Thread-safe resolver context | ✅ Complete |
| [graph.go](../pkg/resolver/graph.go) | Dependency extraction | ✅ Complete |
| [phase.go](../pkg/resolver/phase.go) | Phase grouping from DAG | ✅ Complete |
| [executor.go](../pkg/resolver/executor.go) | Phase-based execution | ✅ Complete |
| [metrics.go](../pkg/resolver/metrics.go) | Execution metrics | ✅ Complete |
| [snapshot.go](../pkg/resolver/snapshot.go) | Snapshot testing | ✅ Complete |
| [diff.go](../pkg/resolver/diff.go) | Value diffing | ✅ Complete |

### Existing Providers (`pkg/provider/builtin/`)

| Provider | Purpose | Status |
|----------|---------|--------|
| `parameter` | CLI parameter access | ✅ Complete |
| `static` | Static values | ✅ Complete |
| `validation` | Regex/CEL validation | ✅ Complete |
| `cel` | CEL expression evaluation | ✅ Complete |
| `env` | Environment variables | ✅ Complete |
| `http` | HTTP requests | ✅ Complete |
| `file` | Filesystem operations | ✅ Complete |
| `exec` | Command execution | ✅ Complete |
| `git` | Git operations | ✅ Complete |
| `sleep` | Delay execution | ✅ Complete |
| `debug` | Debugging utilities | ✅ Complete |
| `go-template` | Go text/template rendering | ✅ Complete |

### Supporting Packages

| Package | Purpose | Status |
|---------|---------|--------|
| `pkg/celexp/` | CEL expression handling | ✅ Complete |
| `pkg/gotmpl/` | Go template handling | ✅ Complete |
| `pkg/dag/` | DAG construction & traversal | ✅ Complete |
| `pkg/flags/` | CLI flag parsing with CSV | ✅ Complete |
| `pkg/solution/get/` | Solution loading with `FindSolution()` | ✅ Complete |

### Dependency Extraction Architecture

The resolver system uses a two-tier approach to extract dependencies from resolver inputs:

**1. Generic Extraction (Default)**

The generic extraction in `graph.go` handles common patterns:
- `ValueRef.Resolver` - Direct resolver references
- CEL expressions - Extracts `_.resolverName` patterns from `expr` fields
- Go templates - Extracts `{{._.resolverName}}` patterns from `tmpl` fields (with default `{{`/`}}` delimiters)

**2. Provider-Specific Extraction (Optional)**

Providers can implement custom dependency extraction via the `ExtractDependencies` function on their `Descriptor`. This is useful for providers with custom input formats that may contain resolver references.

**Type Definition:**
```go
// DescriptorLookup returns a provider's Descriptor by name.
// Used during dependency extraction to access provider-specific extraction functions.
type DescriptorLookup func(providerName string) *provider.Descriptor
```

**Built-in Providers with Custom Extraction:**

| Provider | Extraction Logic |
|----------|-----------------|
| `cel` | Uses `celexp.Expression.GetUnderscoreVariables()` to parse `_.` variable references from the `expression` input |
| `go-template` | Uses `gotmpl.GetGoTemplateReferences()` with custom delimiters from `leftDelim`/`rightDelim` inputs to extract references from the `template` input |

**When to Implement ExtractDependencies:**

A provider should implement `ExtractDependencies` when:
- It has string inputs that may contain resolver references in a custom format
- It uses custom delimiters for templates (e.g., `<%` `%>` instead of `{{` `}}`)
- The generic extraction would miss or incorrectly extract dependencies

**Example:**

```go
// In provider descriptor
ExtractDependencies: func(inputs map[string]any) []string {
    expr, ok := inputs["expression"].(string)
    if !ok {
        return nil
    }
    vars, err := celexp.Expression(expr).GetUnderscoreVariables()
    if err != nil {
        return nil
    }
    return vars
}
```

---

## Implementation Tasks

### Phase 1: Solution Integration (Priority: HIGH) ✅ COMPLETE

The Solution type needs a `Spec` section with resolvers.

#### 1.1 Add Spec Section to Solution

**File:** `pkg/solution/spec.go` ✅ Created

```go
package solution

import "github.com/oakwood-commons/scafctl/pkg/resolver"

// Spec defines the execution specification for a solution
type Spec struct {
    Resolvers map[string]*resolver.Resolver `json:"resolvers,omitempty" yaml:"resolvers,omitempty" doc:"Resolver definitions keyed by name"`
}

// ResolversToSlice converts the Resolvers map to a slice for execution.
func (s *Spec) ResolversToSlice() []*resolver.Resolver

// HasResolvers returns true if the spec contains any resolver definitions.
func (s *Spec) HasResolvers() bool
```

**File:** `pkg/solution/solution.go` ✅ Updated

```go
type Solution struct {
    // ... existing fields ...
    
    // Spec defines the execution specification containing resolvers, templates, and actions.
    Spec Spec `json:"spec,omitempty" yaml:"spec,omitempty" doc:"Execution specification"`
}
```

**Tasks:**
- [x] Create `pkg/solution/spec.go` with `Spec` type
- [x] Add `Spec` field to `Solution` struct
- [x] Add validation for resolver names (no `__` prefix, no whitespace)
- [x] Add YAML/JSON unmarshalling tests

#### 1.2 Solution Validation

**File:** `pkg/solution/spec_validation.go` ✅ Created

```go
package solution

// ValidateSpec performs validation specific to the spec section of a solution.
// It validates resolver naming conventions and checks for circular dependencies.
func (s *Solution) ValidateSpec() error

// validateResolverName validates a resolver name according to naming conventions:
// - Cannot start with "__" (reserved for internal use)
// - Cannot contain whitespace
// - Must not be empty
func validateResolverName(name string) error
```

**Tasks:**
- [x] Create `pkg/solution/spec_validation.go`
- [x] Implement `ValidateSpec()` method
- [x] Call `ValidateSpec()` in existing `Validate()` method
- [x] Add tests for validation edge cases (`pkg/solution/spec_validation_test.go`)

#### 1.3 Test Coverage

**Files Created:**
- `pkg/solution/spec_test.go` - Tests for Spec type, YAML/JSON marshalling
- `pkg/solution/spec_validation_test.go` - Tests for resolver name validation, cycle detection

---

### Phase 2: CLI Integration (Priority: HIGH) ✅ COMPLETE

#### 2.1 Run Command ✅ COMPLETE

**Files Created:**
- `pkg/cmd/scafctl/run/run.go` - Main run command with subcommand support
- `pkg/cmd/scafctl/run/solution.go` - Solution subcommand with full flag support
- `pkg/cmd/scafctl/run/params.go` - Parameter file loading and CLI flag parsing
- `pkg/cmd/scafctl/run/progress.go` - Progress reporting with mpb library

**Command Signature:**
```bash
scafctl run solution [flags]

Flags:
  -f, --file string           Solution file path (auto-discovered if not provided)
  -r, --resolver strings      Resolver parameters (key=value or @file.yaml)
  -o, --output string         Output format: json (default), yaml, quiet
      --only string           Execute only this resolver and its dependencies
      --resolve-all           Execute all resolvers regardless of action requirements
      --progress              Show execution progress (output to stderr)
      --warn-value-size int   Warn when value exceeds this size in bytes (default: 1MB)
      --max-value-size int    Fail when value exceeds this size in bytes (default: 10MB)
      --resolver-timeout dur  Timeout per resolver (default: 30s)
      --phase-timeout dur     Timeout per phase (default: 5m)
```

**Exit Codes:**
```go
const (
    ExitSuccess            = 0  // Successful execution
    ExitResolverFailed     = 1  // Resolver execution failed
    ExitValidationFailed   = 2  // Validation failed
    ExitInvalidSolution    = 3  // Circular dependency / invalid solution
    ExitFileNotFound       = 4  // File not found / parse error
)
```

**Tasks:**
- [x] Create `pkg/cmd/scafctl/run/run.go`
- [x] Create `pkg/cmd/scafctl/run/solution.go` for `run solution` subcommand
- [x] Implement parameter parsing with `-r` flag (key=value and @file.yaml)
- [x] Support stdin input with `-f -`
- [x] Use `get.Getter.FindSolution()` for auto-discovery
- [x] Wire up `resolver.Executor` for execution
- [x] Implement `--only` flag for single resolver execution
- [x] Implement `--progress` flag with stderr output
- [x] Implement `--resolve-all` flag
- [x] Implement value size warnings/limits
- [x] Output JSON by default, support YAML and quiet modes
- [x] Implement specific exit codes
- [x] Add comprehensive tests (`pkg/cmd/scafctl/run/solution_test.go`)

#### 2.2 Parameter File Loading ✅ COMPLETE

**File:** `pkg/cmd/scafctl/run/params.go`

```go
package run

import (
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    
    "gopkg.in/yaml.v3"
)

// LoadParameterFile loads parameters from a YAML or JSON file
func LoadParameterFile(path string) (map[string]any, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read parameter file %q: %w", path, err)
    }
    
    ext := strings.ToLower(filepath.Ext(path))
    result := make(map[string]any)
    
    switch ext {
    case ".yaml", ".yml":
        if err := yaml.Unmarshal(data, &result); err != nil {
            return nil, fmt.Errorf("failed to parse YAML parameter file %q: %w", path, err)
        }
    case ".json":
        if err := json.Unmarshal(data, &result); err != nil {
            return nil, fmt.Errorf("failed to parse JSON parameter file %q: %w", path, err)
        }
    default:
        // Try YAML first, then JSON
        if err := yaml.Unmarshal(data, &result); err != nil {
            if err := json.Unmarshal(data, &result); err != nil {
                return nil, fmt.Errorf("failed to parse parameter file %q (tried YAML and JSON)", path)
            }
        }
    }
    
    return result, nil
}

// ParseResolverFlag parses -r flag values, handling @file.yaml syntax
func ParseResolverFlag(values []string) (map[string]any, error) {
    result := make(map[string]any)
    
    for _, v := range values {
        if strings.HasPrefix(v, "@") {
            // Load from file
            filePath := strings.TrimPrefix(v, "@")
            fileParams, err := LoadParameterFile(filePath)
            if err != nil {
                return nil, err
            }
            // Merge file params into result
            for k, val := range fileParams {
                result[k] = val
            }
        } else {
            // Parse key=value using flags.ParseKeyValueCSV
            parsed, err := flags.ParseKeyValueCSV([]string{v})
            if err != nil {
                return nil, err
            }
            // Merge parsed values, auto-detecting arrays for duplicate keys
            for k, vals := range parsed {
                if existing, ok := result[k]; ok {
                    // Convert to array if needed
                    switch e := existing.(type) {
                    case []any:
                        result[k] = append(e, vals...)
                    default:
                        result[k] = append([]any{e}, vals...)
                    }
                } else if len(vals) == 1 {
                    result[k] = vals[0]
                } else {
                    result[k] = vals
                }
            }
        }
    }
    
    return result, nil
}
```

**Tasks:**
- [x] Create `pkg/cmd/scafctl/run/params.go`
- [x] Implement `LoadParameterFile()` with YAML/JSON support
- [x] Implement `ParseResolverFlags()` with @file.yaml support
- [x] Handle duplicate keys → array conversion
- [x] Add tests for various parameter formats (`pkg/cmd/scafctl/run/params_test.go`)

#### 2.3 Progress Output ✅ COMPLETE

**Library:** [vbauerster/mpb](https://github.com/vbauerster/mpb) - Multi-progress bar library

**Why mpb:**
- Supports multiple concurrent progress bars (ideal for parallel resolver phases)
- Non-blocking, writes to configurable output (stderr)
- Gracefully handles non-TTY environments (falls back to simple output)
- Doesn't take over the terminal like full TUI frameworks
- Thread-safe for concurrent updates

**File:** `pkg/cmd/scafctl/run/progress.go` (new)

```go
package run

import (
    "io"
    "time"

    "github.com/vbauerster/mpb/v8"
    "github.com/vbauerster/mpb/v8/decor"
)

// ProgressReporter outputs execution progress using mpb
type ProgressReporter struct {
    progress  *mpb.Progress
    bars      map[string]*mpb.Bar
    total     int
    startTime time.Time
}

// NewProgressReporter creates a new progress reporter writing to the given output
func NewProgressReporter(writer io.Writer, total int) *ProgressReporter {
    p := mpb.New(
        mpb.WithOutput(writer),
        mpb.WithWidth(60),
        mpb.WithRefreshRate(100*time.Millisecond),
    )
    return &ProgressReporter{
        progress:  p,
        bars:      make(map[string]*mpb.Bar),
        total:     total,
        startTime: time.Now(),
    }
}

// StartPhase creates progress bars for all resolvers in a phase
func (p *ProgressReporter) StartPhase(phaseNum int, resolverNames []string) {
    for _, name := range resolverNames {
        bar := p.progress.AddBar(1,
            mpb.PrependDecorators(
                decor.Name(name, decor.WCSyncSpaceR),
                decor.OnComplete(decor.Spinner(nil, decor.WCSyncSpace), "✓"),
            ),
            mpb.AppendDecorators(
                decor.OnComplete(decor.Elapsed(decor.ET_STYLE_GO), ""),
            ),
        )
        p.bars[name] = bar
    }
}

// Complete marks a resolver as complete
func (p *ProgressReporter) Complete(resolverName string) {
    if bar, ok := p.bars[resolverName]; ok {
        bar.Increment()
    }
}

// Failed marks a resolver as failed
func (p *ProgressReporter) Failed(resolverName string, err error) {
    if bar, ok := p.bars[resolverName]; ok {
        bar.Abort(false) // Don't clear the bar, show failure state
    }
}

// Skipped marks a resolver as skipped
func (p *ProgressReporter) Skipped(resolverName string) {
    if bar, ok := p.bars[resolverName]; ok {
        bar.SetTotal(0, true) // Mark as complete with zero work
    }
}

// Wait waits for all progress bars to complete and returns total duration
func (p *ProgressReporter) Wait() time.Duration {
    p.progress.Wait()
    return time.Since(p.startTime)
}
```

**Tasks:**
- [x] Add `github.com/vbauerster/mpb/v8` to `go.mod`
- [x] Create `pkg/cmd/scafctl/run/progress.go`
- [x] Implement `ProgressReporter` interface with mpb
- [x] Add phase-based bar creation for concurrent resolver display
- [x] Integrate with executor callbacks/hooks
- [x] Handle non-TTY gracefully (mpb does this automatically)

---

### Phase 3: Enhanced Executor Features (Priority: MEDIUM) ✅ COMPLETE

#### 3.1 `__self` Handling ✅ COMPLETE

**Implementation:** Added `evaluateConditionWithSelf()` method and updated `executeProviderWithSelf()` to consistently pass `__self` through all phases.

**Tasks:**
- [x] Ensure `__self` is available in `until:` conditions during resolve phase
- [x] Ensure `__self` is passed between transform steps
- [x] Ensure `__self` is the final transformed value in validate phase
- [x] Add tests for `__self` in each context

**File:** `pkg/resolver/executor.go`

```go
// evaluateConditionWithSelf evaluates a condition expression with __self available
func (e *Executor) evaluateConditionWithSelf(ctx context.Context, expr *celexp.Expression, selfValue any) (bool, error) {
    celCtx := celexp.NewContext()
    // ... adds resolverCtx values and __self
    return celexp.EvaluateExpression(ctx, celCtx, string(*expr), celexp.WithExpectedType(celexp.TypeBool))
}

// In executeResolvePhase - after each source, evaluate until with __self
if phase.Until != nil {
    stop, err := e.evaluateConditionWithSelf(ctx, phase.Until, value)
    // ...
}
```

#### 3.2 Conditional Resolver Handling ✅ COMPLETE

**Implementation:** Modified the defer function in `executeResolver` to only store non-skipped resolvers in the context.

**File:** `pkg/resolver/executor.go`

```go
// In executeResolver defer function:
defer func() {
    // ...
    // Only store result if resolver was not skipped
    // Skipped resolvers should be truly absent from _ (per design spec)
    if result.Status != ExecutionStatusSkipped {
        resolverCtx.SetResult(r.Name, result)
    }
}()
```

**Tasks:**
- [x] Modify executor to not store skipped resolvers in context
- [x] Update tests to verify skipped resolvers are absent from `_`
- [x] Skipped status is still recorded in ExecutionResult.Resolvers for metrics

#### 3.3 Value Size Limits ✅ COMPLETE

**Implementation:** Added `warnValueSize` and `maxValueSize` fields to Executor with corresponding option functions. Size checking happens after the validate phase, before the final value is emitted.

**File:** `pkg/resolver/executor.go`

```go
type Executor struct {
    // ...
    warnValueSize int64 // Warn when value exceeds this size in bytes (0 = disabled)
    maxValueSize  int64 // Fail when value exceeds this size in bytes (0 = disabled)
}

func WithWarnValueSize(bytes int64) ExecutorOption {
    return func(e *Executor) {
        e.warnValueSize = bytes
    }
}

func WithMaxValueSize(bytes int64) ExecutorOption {
    return func(e *Executor) {
        e.maxValueSize = bytes
    }
}

// In executeResolver, after validate phase:
if e.warnValueSize > 0 || e.maxValueSize > 0 {
    if valueBytes, err := json.Marshal(value); err == nil {
        valueSize := int64(len(valueBytes))
        if e.maxValueSize > 0 && valueSize > e.maxValueSize {
            return fmt.Errorf("resolver %q value size %d bytes exceeds maximum %d bytes",
                r.Name, valueSize, e.maxValueSize)
        }
        if e.warnValueSize > 0 && valueSize > e.warnValueSize {
            lgr.Info("resolver value size exceeds warning threshold",
                "resolver", r.Name, "size", valueSize, "threshold", e.warnValueSize)
        }
    }
}
```

**Tasks:**
- [x] Add `warnValueSize` and `maxValueSize` to Executor
- [x] Add corresponding ExecutorOption functions
- [x] Implement size checking after value resolution
- [x] Add tests for size limits

---

### Phase 4: Error Handling & Observability (Priority: MEDIUM) ✅ COMPLETE

#### 4.1 Structured Error Types ✅ COMPLETE

**File:** `pkg/resolver/errors.go`

**Implementation:** Created comprehensive error types with proper struct tags and helper functions.

```go
package resolver

// ExecutionError represents an error during resolver execution
type ExecutionError struct {
    ResolverName string `json:"resolverName" yaml:"resolverName"`
    Phase        string `json:"phase" yaml:"phase"` // "resolve", "transform", "validate"
    Step         int    `json:"step" yaml:"step"`
    Provider     string `json:"provider" yaml:"provider"`
    Cause        error  `json:"-" yaml:"-"`
}

// ValidationFailure represents a single validation rule failure
type ValidationFailure struct {
    Rule      int    `json:"rule" yaml:"rule"`
    Provider  string `json:"provider" yaml:"provider"`
    Message   string `json:"message" yaml:"message"`
    Cause     error  `json:"-" yaml:"-"`
    Sensitive bool   `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
}

// AggregatedValidationError collects all validation failures
type AggregatedValidationError struct {
    ResolverName string              `json:"resolverName" yaml:"resolverName"`
    Value        any                 `json:"-" yaml:"-"`
    Failures     []ValidationFailure `json:"failures" yaml:"failures"`
    Sensitive    bool                `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
}

// CircularDependencyError represents a cycle in resolver dependencies
type CircularDependencyError struct {
    Cycle []string `json:"cycle" yaml:"cycle"`
}

// PhaseTimeoutError represents a timeout during phase execution
type PhaseTimeoutError struct {
    Phase            int      `json:"phase" yaml:"phase"`
    ResolversWaiting []string `json:"resolversWaiting" yaml:"resolversWaiting"`
}

// ValueSizeError represents a value that exceeds the maximum allowed size
type ValueSizeError struct {
    ResolverName string `json:"resolverName" yaml:"resolverName"`
    ActualSize   int64  `json:"actualSize" yaml:"actualSize"`
    MaxSize      int64  `json:"maxSize" yaml:"maxSize"`
}

// TypeCoercionError represents a failure to coerce a value to the expected type
type TypeCoercionError struct {
    ResolverName string `json:"resolverName" yaml:"resolverName"`
    Phase        string `json:"phase" yaml:"phase"`
    SourceType   string `json:"sourceType" yaml:"sourceType"`
    TargetType   Type   `json:"targetType" yaml:"targetType"`
    Cause        error  `json:"-" yaml:"-"`
}

// RedactedError wraps an error to provide a redacted version
type RedactedError struct {
    Original error  `json:"-" yaml:"-"`
    Redacted string `json:"error" yaml:"error"`
}

// Helper functions: IsExecutionError, IsValidationError, IsCircularDependencyError, etc.
```

**Tasks:**
- [x] Create `pkg/resolver/errors.go`
- [x] Update executor to use structured error types
- [x] Add error type assertions in tests

#### 4.2 Validation Aggregation ✅ COMPLETE

**Implementation:** Modified `executeValidatePhase` to run ALL validation rules and collect failures, returning an `AggregatedValidationError` with all failures.

```go
// executeValidatePhase now accepts resolverName and sensitive flag
func (e *Executor) executeValidatePhase(ctx context.Context, resolverName string, sensitive bool, phase *ValidatePhase, value any) (int, error) {
    // Create validation error to collect failures
    validationErr := &AggregatedValidationError{
        ResolverName: resolverName,
        Value:        value,
        Sensitive:    sensitive,
        Failures:     make([]ValidationFailure, 0),
    }

    // Run ALL validation rules and collect failures
    for i, validation := range phase.With {
        _, err := e.executeProviderWithSelf(ctx, validation.Provider, validation.Inputs, value)
        if err != nil {
            // Build and add failure (redacted if sensitive)
            validationErr.AddFailure(ValidationFailure{...})
        }
    }

    // Return aggregated error if any failures occurred
    if validationErr.HasFailures() {
        return providerCallCount, validationErr
    }
    return providerCallCount, nil
}
```

**Tasks:**
- [x] Modify `executeValidatePhase` to run all validations
- [x] Collect all failure messages
- [x] Return aggregated `AggregatedValidationError`
- [x] Add tests for multiple validation failures

#### 4.3 Sensitive Value Redaction ✅ COMPLETE

**Implementation:** Added redaction helper functions in `pkg/resolver/executor.go`:

```go
// redactForLog returns [REDACTED] if sensitive is true
func redactForLog(value string, sensitive bool) string

// RedactValue returns [REDACTED] for any value if sensitive is true
func RedactValue(value any, sensitive bool) any

// RedactError wraps an error with redaction if sensitive is true
func RedactError(err error, sensitive bool) error

// RedactMapValues redacts all values in a map if sensitive is true
func RedactMapValues(m map[string]any, sensitive bool) map[string]any
```

**Tasks:**
- [x] Implement `redactForLog()` helper function
- [x] Implement `RedactValue()` for any value type
- [x] Implement `RedactError()` to wrap errors with redaction
- [x] Implement `RedactMapValues()` for map redaction
- [x] Redact validation messages for sensitive resolvers
- [x] Add comprehensive tests for redaction (`redaction_test.go`)

---

### Phase 5: Provider Registry (Priority: MEDIUM) ✅ COMPLETE

#### 5.1 Default Registry ✅ COMPLETE

**File:** `pkg/provider/builtin/builtin.go` (updated)

**Implementation:** Added `DefaultRegistry()` function with `sync.Once` for thread-safe lazy initialization. Also added `MustDefaultRegistry()` convenience function and `ProviderNames()` helper.

```go
package builtin

import (
    "sync"
    
    "github.com/oakwood-commons/scafctl/pkg/provider"
    // ... provider imports
)

var (
    defaultRegistryOnce sync.Once
    defaultRegistry     *provider.Registry
    registrationErr     error
)

// DefaultRegistry returns a shared registry with all built-in providers pre-registered.
// Thread-safe and initialized only once using sync.Once.
func DefaultRegistry() (*provider.Registry, error) {
    defaultRegistryOnce.Do(func() {
        defaultRegistry = provider.NewRegistry()
        registrationErr = registerAllToRegistry(defaultRegistry)
    })
    return defaultRegistry, registrationErr
}

// MustDefaultRegistry returns the default registry, panicking if registration fails.
func MustDefaultRegistry() *provider.Registry

// ProviderNames returns the names of all built-in providers.
func ProviderNames() []string

// RegisterAll registers all built-in providers in the global registry.
// Deprecated: Use DefaultRegistry() instead for better encapsulation.
func RegisterAll() error
```

**Built-in Providers (12 total):**
- `http` - HTTP requests
- `env` - Environment variables
- `cel` - CEL expression evaluation
- `file` - Filesystem operations
- `validation` - Regex/CEL validation
- `exec` - Command execution
- `git` - Git operations
- `debug` - Debugging utilities
- `sleep` - Delay execution
- `parameter` - CLI parameter access
- `static` - Static values
- `go-template` - Go text/template rendering

**Tasks:**
- [x] Add `DefaultRegistry()` function with `sync.Once` lazy initialization
- [x] Add `MustDefaultRegistry()` convenience function
- [x] Add `ProviderNames()` helper function
- [x] Mark `RegisterAll()` as deprecated
- [x] Update `pkg/cmd/scafctl/run/solution.go` to use `DefaultRegistry()`
- [x] Add comprehensive tests (`pkg/provider/builtin/builtin_test.go`)
- [x] Add test to verify all providers are registered (`TestAllProvidersRegistered`)

---

### Phase 6: Testing & Validation (Priority: HIGH) ✅ COMPLETE

#### 6.1 Unit Tests ✅ COMPLETE

**Files Created/Updated:**
- `pkg/resolver/types_test.go` - Added overflow/precision loss tests
- `pkg/resolver/executor_test.go` - Added timeout and concurrency tests
- `pkg/resolver/redaction_test.go` - Sensitive value redaction tests

**Tasks:**
- [x] Test type coercion edge cases (overflow, precision loss) - `TestCoerceType_Overflow`, `TestCoerceType_PrecisionLoss`
- [x] Test circular dependency detection - existing tests in `graph_test.go`
- [x] Test conditional resolver execution (`when:`) - `TestExecutor_Execute_WhenCondition`
- [x] Test partial emission on failure - `TestExecutor_Execute_PartialSuccess`
- [x] Test timeout handling - `TestExecutor_Execute_Timeout`, `TestExecutor_Execute_PhaseTimeout`, `TestExecutor_Execute_ContextCancellation`
- [x] Test concurrent execution - `TestExecutor_Execute_ConcurrentStress`
- [x] Test value size limits - `TestExecutor_Execute_MaxValueSize`
- [x] Test sensitive value redaction - tests in `redaction_test.go`

#### 6.2 Integration Tests ✅ COMPLETE

**File:** `pkg/resolver/integration_test.go`

**Tasks:**
- [x] Test full solution with multiple resolvers - `TestIntegration_FullSolution`
- [x] Test CLI parameter passing (including @file.yaml) - `TestSolutionOptions_Run_ParameterFromFile`
- [x] Test stdin input - `TestSolutionOptions_Run_StdinInput`
- [x] Test `--only` flag - `TestSolutionOptions_Run_OnlyFlag`, `TestSolutionOptions_Run_OnlyNonexistent`
- [x] Test `--resolve-all` flag - `TestSolutionOptions_Run_ResolveAllFlag`
- [x] Test error scenarios end-to-end - `TestIntegration_ErrorHandling`, `TestIntegration_ValidationErrors`
- [x] Test exit codes - verified via test status and error returns

**Additional Integration Tests Added:**
- `TestIntegration_ConditionalExecution` - When conditions
- `TestIntegration_SnapshotCapture` - Snapshot functionality
- `TestIntegration_PhaseBasedExecution` - Multi-phase execution
- `TestIntegration_DiamondDependency` - Complex DAG patterns
- `TestIntegration_MetricsCollection` - Metrics accuracy

#### 6.3 Snapshot Tests ✅ COMPLETE

**Files Created:**
- `pkg/resolver/snapshot_test.go` - Snapshot fixture loading and comparison tests
- `pkg/resolver/testdata/snapshots/simple_chain.yaml` - Simple chain solution fixture
- `pkg/resolver/testdata/snapshots/simple_chain_expected.json` - Expected snapshot output
- `pkg/resolver/testdata/snapshots/diamond_pattern.yaml` - Diamond dependency fixture
- `pkg/resolver/testdata/snapshots/diamond_pattern_expected.json` - Expected snapshot output
- `pkg/resolver/testdata/snapshots/error_handling.yaml` - Error handling fixture
- `pkg/resolver/testdata/snapshots/error_handling_expected.json` - Expected snapshot output

**Tasks:**
- [x] Create snapshot fixtures for common patterns - simple chain, diamond, error handling
- [x] Add snapshot comparison in CI - `TestSnapshot_LoadFixture` verifies JSON structure

#### 6.4 CLI Scenario Tests ✅ COMPLETE

**File:** `pkg/cmd/scafctl/run/solution_test.go`

**Tests Added:**
- `TestSolutionOptions_Run_StdinInput` - Reading solution from stdin
- `TestSolutionOptions_Run_OnlyFlag` - Execute single resolver
- `TestSolutionOptions_Run_OnlyNonexistent` - Error on invalid resolver name
- `TestSolutionOptions_Run_ParameterFromFile` - @params.yaml support
- `TestSolutionOptions_Run_ParameterKeyValue` - key=value parameters
- `TestSolutionOptions_Run_YAMLOutput` - YAML output format
- `TestSolutionOptions_Run_QuietOutput` - Quiet mode
- `TestSolutionOptions_Run_InvalidOutputFormat` - Error on bad format
- `TestSolutionOptions_Run_EmptySolution` - Handle empty solution
- `TestSolutionOptions_Run_SensitiveRedaction` - Verify sensitive values redacted
- `TestSolutionOptions_Run_MaxValueSizeExceeded` - Value size limit enforcement
- `TestSolutionOptions_Run_ResolveAllFlag` - --resolve-all behavior

**Bug Fixed During Testing:**
- Parameters were not being passed to the provider context in `executor.go`
- Added `ctx = provider.WithParameters(ctx, params)` to fix parameter provider access

---

### Phase 7: Documentation (Priority: LOW) ✅ COMPLETE

#### 7.1 Package Documentation ✅ COMPLETE

**Files Created/Updated:**
- `pkg/resolver/README.md` - Comprehensive package documentation with:
  - Overview and features
  - Quick start guide
  - Architecture explanation
  - Complete API reference
  - Provider documentation
  - Best practices
  - Code examples

**Tasks:**
- [x] Add README.md to `pkg/resolver/` - Complete with table of contents, examples, and API docs
- [x] Document public API with examples - Included in README
- [x] Add godoc comments to exported types - Existing types already documented

#### 7.2 User Documentation ✅ COMPLETE

**Files Created:**
- `docs/resolver-tutorial.md` - Step-by-step tutorial covering:
  - First resolver
  - Using parameters
  - Dependencies
  - Transformations
  - Validation
  - Conditional execution
  - Error handling
  - HTTP APIs
  - Common patterns

- `examples/resolvers/README.md` - Examples index
- `examples/resolvers/hello-world.yaml` - Simplest resolver
- `examples/resolvers/parameters.yaml` - CLI parameter usage
- `examples/resolvers/dependencies.yaml` - Dependency phases
- `examples/resolvers/env-config.yaml` - Environment-based config
- `examples/resolvers/feature-flags.yaml` - Feature toggles
- `examples/resolvers/transform-pipeline.yaml` - Data transformation
- `examples/resolvers/validation.yaml` - Input validation
- `examples/resolvers/secrets.yaml` - Sensitive value handling
- `examples/resolver-demo.yaml` - Updated to use correct spec structure

**Tasks:**
- [x] Create resolver tutorial - `docs/resolver-tutorial.md`
- [x] Add examples to `examples/` directory - 9 example files created
- [x] Update CLI help text - Enhanced with sections for parameters, execution, output, and exit codes

---

## Implementation Details

### Provider Registry Initialization

The default registry uses lazy initialization with `sync.Once` to ensure:
- Single instance across the application
- Thread-safe initialization
- All built-in providers registered on first access

```go
// Usage in run command:
executor := resolver.NewExecutor(builtin.DefaultRegistry())
```

### Parameter Provider Array Detection

The parameter provider automatically detects when multiple values are provided for the same key and returns them as an array:

```bash
# These are equivalent:
scafctl run solution -r region=us-east1 -r region=us-west1
scafctl run solution -r "region=us-east1,region=us-west1"

# Result: region = ["us-east1", "us-west1"]
```

### Timeout Configuration

Timeouts can be configured at multiple levels:
1. CLI flags (`--resolver-timeout`, `--phase-timeout`)
2. Solution-level (future: in spec)
3. Environment variables (future: via Viper integration)

---

## Implementation Order

Based on dependencies and priority:

```
Week 1-2: Solution Integration ✅ COMPLETE (Jan 27, 2026)
├── ✅ Create pkg/solution/spec.go
├── ✅ Add Spec field to Solution
├── ✅ Create pkg/solution/spec_validation.go
├── ✅ Add spec_test.go and spec_validation_test.go
└── ✅ All tests passing, linting clean

Week 2-3: CLI Integration  
├── Create pkg/cmd/scafctl/run/run.go
├── Create pkg/cmd/scafctl/run/solution.go
├── Create pkg/cmd/scafctl/run/params.go
├── Create pkg/cmd/scafctl/run/progress.go
├── Wire up resolver execution
└── Implement exit codes

Week 3-4: Provider Registry & Executor Enhancements
├── Create pkg/provider/builtin/registry.go
├── Implement conditional resolver handling
├── Implement value size limits
├── Create pkg/resolver/errors.go
└── Implement validation aggregation

Week 4-5: Testing & Refinement
├── Unit tests for all new code
├── Integration tests
├── Snapshot tests
└── Documentation
```

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Circular dependency detection misses edge cases | High | Extensive test coverage with complex graphs |
| Concurrent execution race conditions | High | Use existing `sync.Map`, add stress tests |
| Large value memory pressure | Medium | Implement size warnings/limits with CLI flags |
| Provider timeout interactions | Medium | Clear documentation of timeout precedence |
| Parameter file parsing edge cases | Medium | Comprehensive tests for YAML/JSON formats |

---

## Success Criteria

1. **Functional:** `scafctl run solution` executes all resolver phases correctly
2. **Concurrent:** Resolvers in the same phase execute concurrently
3. **Safe:** Circular dependencies are detected at load time
4. **Observable:** Progress output and metrics provide visibility
5. **Flexible:** Parameters can be passed via CLI, files, or stdin
6. **Testable:** Snapshot testing enables regression detection
7. **Documented:** Clear API documentation and user guides

---

## Appendix: File Structure After Implementation

```
pkg/
├── resolver/
│   ├── context.go         # Thread-safe resolver context
│   ├── context_test.go
│   ├── diff.go            # Value diffing utilities
│   ├── errors.go          # ✅ NEW: Structured error types (ExecutionError, AggregatedValidationError, etc.)
│   ├── errors_test.go     # ✅ NEW: Tests for error types
│   ├── executor.go        # ✅ UPDATED: Structured errors, validation aggregation, redaction
│   ├── executor_test.go
│   ├── graph.go           # Dependency extraction
│   ├── graph_test.go
│   ├── metrics.go         # Execution metrics
│   ├── metrics_summary.go
│   ├── phase.go           # Phase grouping
│   ├── phase_test.go
│   ├── README.md          # NEW: Package documentation
│   ├── redaction_test.go  # ✅ NEW: Tests for redaction helpers
│   ├── solution.go        # ✅ UPDATED: Added Spec field
│   ├── solution_test.go
│   ├── spec.go            # ✅ NEW: Spec type with ResolversToSlice(), HasResolvers()
│   ├── spec_test.go       # ✅ NEW: Tests for Spec marshalling/unmarshalling
│   ├── spec_validation.go # ✅ NEW: ValidateSpec(), validateResolverName()
│   ├── spec_validation_test.go # ✅ NEW: Tests for validation
│   └── get/
│       └── get.go         # Existing: FindSolution()
├── provider/
│   └── builtin/
│       ├── builtin.go     # ✅ UPDATED: DefaultRegistry(), MustDefaultRegistry(), ProviderNames()
│       ├── builtin_test.go # ✅ NEW: Tests for DefaultRegistry
│       └── ...            # Existing providers
└── cmd/
    └── scafctl/
        └── run/
            ├── run.go       # NEW: Run command
            ├── solution.go  # NEW: Run solution subcommand
            ├── params.go    # NEW: Parameter parsing
            └── progress.go  # NEW: Progress output
```

---

## References

- [Resolver Design Specification](design/resolvers.md)
- [Solution Design Specification](design/solutions.md)
- [CLI Design Specification](design/cli.md)
- [Provider Design Specification](design/providers.md)
