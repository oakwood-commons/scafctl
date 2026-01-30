# Provider Implementation Plan

This document outlines the changes required to align the provider implementation with the design document and address identified gaps.

## Summary of Decisions

| Issue | Decision |
|-------|----------|
| Context key type | Keep string approach (more debuggable) |
| ResolverContextFromContext | Update design doc to match implementation (return tuple) |
| parametersKey | Add to design doc |
| Execution mode validation | Implement in Executor (hybrid approach) |
| Provider naming | Update design docs (api→http, filesystem→file) |
| Execute input parameter | Change to `input any` - Executor passes decoded value directly when Decode is set |
| Validation provider return | Return validated value directly on success |
| Decode function | Executor calls Decode (when set) and passes result directly to Execute |
| Warnings maxItems | Change to 50 in implementation |
| APIVersion | Ensure all providers set it |
| Struct tags | Add simplified huma-compatible tags (use `format` over `pattern` where possible) |
| Registry validation | Require explicit schema initialization, enforce MockBehavior |
| MockBehavior | Enforce presence for all providers |
| PropertyType for maps | Clarify `any` should be used |
| Error handling | Providers should wrap errors with provider name |
| Logging | Add structured logging (logr) to all providers |
| Metrics | Add optional execution metrics collection |
| Context cancellation | All long-running providers must respect context cancellation |
| HTTP retry | Add configurable retry support to HTTP provider |
| Streaming output | Deferred to future phase |

---

## Phase 1: Design Document Updates

### 1.1 Update Context Keys Section

**File:** `docs/design/providers.md`

**Current (lines ~330-340):**
```go
type contextKey int

const (
  executionModeKey   contextKey = iota
  dryRunKey
  resolverContextKey
)
```

**Change to:**
```go
// Context keys use string type for better debugging and traceability in logs.
type contextKey string

const (
  executionModeKey   contextKey = "scafctl.provider.executionMode"
  dryRunKey          contextKey = "scafctl.provider.dryRun"
  resolverContextKey contextKey = "scafctl.provider.resolverContext"
  parametersKey      contextKey = "scafctl.provider.parameters"
)
```

**Also add accessor functions:**
```go
// WithParameters returns a new context with the CLI parameters map.
// Parameters are parsed from -r/--resolver flags and stored for retrieval by the parameter provider.
func WithParameters(ctx context.Context, parameters map[string]any) context.Context {
  return context.WithValue(ctx, parametersKey, parameters)
}

// ParametersFromContext retrieves the CLI parameters map from the context.
// Returns the parameters map and true if found, nil and false otherwise.
func ParametersFromContext(ctx context.Context) (map[string]any, bool) {
  params, ok := ctx.Value(parametersKey).(map[string]any)
  return params, ok
}
```

---

### 1.2 Update ResolverContextFromContext Signature

**File:** `docs/design/providers.md`

**Current (lines ~360-365):**
```go
func ResolverContextFromContext(ctx context.Context) map[string]any {
  data, _ := ctx.Value(resolverContextKey).(map[string]any)
  return data // Returns nil map if not set, which is safe to read from
}
```

**Change to:**
```go
// ResolverContextFromContext retrieves the resolver context map from the context.
// Returns the resolver context map and true if found, nil and false otherwise.
func ResolverContextFromContext(ctx context.Context) (map[string]any, bool) {
  resolverCtx, ok := ctx.Value(resolverContextKey).(map[string]any)
  return resolverCtx, ok
}
```

---

### 1.3 Update Built-in Providers Section

**File:** `docs/design/providers.md`

**Current section "Built-in Providers" lists:**
- parameter, env, static, filesystem, git, api, cel

**Change to include all implemented providers:**

```markdown
## Built-in Providers (Non-Exhaustive)

### parameter

Reads a value supplied at invocation time via CLI flags.

### env

Reads from the process environment.

### static

Supplies a literal value.

### file

Reads data from the local filesystem.

> **Note:** Previously documented as `filesystem`.

### git

Reads data from a git repository or working tree.

### http

Fetches data from an HTTP endpoint or makes HTTP requests.

> **Note:** Previously documented as `api`.

### cel

Derives a value using CEL expressions.

### validation

Validates values using regex patterns and CEL expressions.

### exec

Executes shell commands.

### debug

Provides debugging utilities for development and troubleshooting.

### sleep

Introduces delays for testing and rate-limiting scenarios.
```

---

### 1.4 Update Validation Provider Return Spec

**File:** `docs/design/providers.md`

**Current (lines ~460-470):**
> Validation providers must return an `Output` whose `Data` field contains a boolean value

**Change to (simplified - return value directly on success):**
```markdown
**Return Value Structure:**

Validation providers follow a simple pattern:

```go
// Success - return the validated value directly (useful in transform chains)
return &Output{
  Data: valueBeingValidated,
  Metadata: map[string]any{
    "matchedPatterns": matchedPatterns, // optional context
  },
}, nil

// Failure - return an error
return nil, fmt.Errorf("validation failed: %s", message)
```

**Key points:**
- On success, `Data` contains the validated value (not a wrapper map)
- This enables validation to be used in transform chains where the value flows through
- Validation failures always return an error (not `Data.valid = false`)
- Optional metadata can provide context about which validations passed
- This approach distinguishes "validation ran and failed" (error) from "validation couldn't run" (different error)
```

---

### 1.5 Update PropertyType Documentation

**File:** `docs/design/providers.md`

**Add clarification after PropertyType constants:**

```markdown
**Type Usage Notes:**

- `PropertyTypeAny` should be used for:
  - Map/object types (`map[string]any`)
  - Mixed-type fields that can accept multiple types
  - Complex nested structures
- There is no explicit `map` type; use `any` for map properties
```

---

### 1.6 Update Output.Warnings maxItems

**File:** `docs/design/providers.md`

**Change:**
```go
Warnings []string `json:"warnings,omitempty" doc:"Non-fatal warnings generated during execution" maxItems:"50"`
```

---

### 1.7 Update Provider Interface Execute Signature

**File:** `docs/design/providers.md`

**Current:**
```go
type Provider interface {
  Descriptor() *Descriptor
  Execute(ctx context.Context, inputs map[string]any) (*Output, error)
}
```

**Change to:**
```go
type Provider interface {
  // Descriptor returns the provider's metadata, schema, and capabilities.
  Descriptor() *Descriptor
  
  // Execute runs the provider logic with resolved inputs.
  // The input parameter is either:
  //   - map[string]any if Descriptor().Decode is nil
  //   - The decoded type if Descriptor().Decode is set and returns a typed struct
  // Resolver values can be accessed via ResolverContextFromContext(ctx).
  // Execution mode and dry-run flag are available via ExecutionModeFromContext(ctx) and DryRunFromContext(ctx).
  Execute(ctx context.Context, input any) (*Output, error)
}
```

**Rationale:** This is a breaking change but acceptable since the application is not in production. Benefits:
- Cleaner design: decoded value goes directly to Execute, no context indirection
- Type safety: providers do one type assertion at the top, then work with typed struct
- Clear contract: if you define `Decode`, you get typed input; if not, you get the map
- `Decode` becomes truly optional: simple providers can skip it entirely

---

### 1.8 Add Execution Mode Validation Documentation

**File:** `docs/design/providers.md`

**Add new section under "Execution Lifecycle":**

```markdown
### Execution Mode Validation

The `Executor` validates execution mode before invoking providers:

1. **Presence Check**: Execution mode must be set in the context via `WithExecutionMode()`
2. **Capability Check**: The execution mode must match one of the provider's declared capabilities

This validation happens in the `Executor`, not in individual providers. Providers can trust that:
- The execution mode is valid and matches their capabilities
- They can retrieve it via `ExecutionModeFromContext(ctx)` if needed for behavior branching

**Error Examples:**
- `"execution mode not provided in context"` - Caller forgot to set execution mode
- `"provider 'http' does not support capability 'authentication'"` - Mode doesn't match declared capabilities

**Validation Mode:** Strict (always enforced, not configurable)
```

---

## Phase 2: Implementation Changes

### 2.1 Update Provider Interface

**File:** `pkg/provider/provider.go`

**Current (line 17):**
```go
Execute(ctx context.Context, inputs map[string]any) (*Output, error)
```

**Change to:**
```go
// Execute runs the provider logic with resolved inputs.
// The input parameter is either:
//   - map[string]any if Descriptor().Decode is nil
//   - The decoded type if Descriptor().Decode is set
Execute(ctx context.Context, input any) (*Output, error)
```

**Provider implementation patterns:**

```go
// Pattern 1: Provider WITH Decode function (typed input)
func (p *HTTPProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    in := input.(HTTPInput)  // Single type assertion - Executor guarantees this type
    // Use in.URL, in.Method, in.Headers directly with full type safety
    // ...
}

// Pattern 2: Provider WITHOUT Decode function (map input)
func (p *StaticProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    inputs := input.(map[string]any)  // Executor passes map when Decode is nil
    value := inputs["value"]
    // ...
}
```

---

### 2.2 Update Output.Warnings Tag

**File:** `pkg/provider/provider.go`

**Current (line 48):**
```go
Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Non-fatal warning messages" maxItems:"20"`
```

**Change to:**
```go
Warnings []string `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Non-fatal warning messages" maxItems:"50"`
```

---

### 2.3 Add Struct Tags to Descriptor

**File:** `pkg/provider/provider.go`

**Current Descriptor struct (lines 24-45):**
```go
type Descriptor struct {
	Name         string                            `json:"name" yaml:"name" doc:"Provider name (must be unique)" required:"true"`
	DisplayName  string                            `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name"`
	APIVersion   string                            `json:"apiVersion" yaml:"apiVersion" doc:"API version" required:"true"`
	// ... etc
}
```

**Change to (simplified huma-compatible tags - use `format` over `pattern` where possible):**
```go
type Descriptor struct {
	Name         string          `json:"name" yaml:"name" doc:"Unique provider identifier" minLength:"2" maxLength:"100" example:"http" pattern:"^[a-z][a-z0-9-]*$" required:"true"`
	DisplayName  string          `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name" maxLength:"100" example:"HTTP Client"`
	APIVersion   string          `json:"apiVersion" yaml:"apiVersion" doc:"Provider API version" example:"v1" pattern:"^v[0-9]+$" required:"true"`
	Version      *semver.Version `json:"version" yaml:"version" doc:"Semantic version" required:"true"`
	Description  string          `json:"description" yaml:"description" doc:"Provider description" minLength:"10" maxLength:"500" required:"true"`
	Schema       SchemaDefinition                      `json:"schema" yaml:"schema" doc:"Input schema" required:"true"`
	OutputSchemas map[Capability]SchemaDefinition      `json:"outputSchemas" yaml:"outputSchemas" doc:"Output schemas per capability" required:"true"`
	Decode       func(map[string]any) (any, error) `json:"-" yaml:"-"`
	MockBehavior string                            `json:"mockBehavior" yaml:"mockBehavior" doc:"Dry-run behavior description" minLength:"10" maxLength:"500" required:"true"`
	Capabilities []Capability                      `json:"capabilities" yaml:"capabilities" doc:"Supported execution contexts" minItems:"1" required:"true"`
	Category     string                            `json:"category,omitempty" yaml:"category,omitempty" doc:"Classification category" maxLength:"50" example:"network"`
	Tags         []string                          `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable keywords" maxItems:"20"`
	Icon         string                            `json:"icon,omitempty" yaml:"icon,omitempty" doc:"Icon URL" format:"uri" maxLength:"500"`
	Links        []Link                            `json:"links,omitempty" yaml:"links,omitempty" doc:"Related links" maxItems:"10"`
	Examples     []Example                         `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Usage examples" maxItems:"10"`
	Deprecated   bool                              `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Deprecation status"`
	Beta         bool                              `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Beta status"`
	Maintainers  []Contact                         `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"Maintainer contacts" maxItems:"10"`
}
```

**Key simplifications:**
- Removed `patternDescription` (huma provides good default error messages)
- Used `format:"uri"` instead of custom URL pattern for Icon
- Used `format:"email"` in Contact instead of custom pattern
- Shorter, clearer `doc` descriptions
- Removed redundant validation where huma defaults suffice

---

### 2.4 Add Struct Tags to Contact, Link, Example

**File:** `pkg/provider/provider.go`

**Update Contact (simplified - use `format:"email"` instead of pattern):**
```go
type Contact struct {
	Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"Maintainer name" maxLength:"60" example:"Jane Doe"`
	Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"Maintainer email" format:"email" maxLength:"100"`
}
```

**Update Link (simplified - use `format:"uri"`):**
```go
type Link struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"Link name" maxLength:"30" example:"Documentation"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"Link URL" format:"uri" maxLength:"500"`
}
```

**Update Example:**
```go
type Example struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty" doc:"Example name" maxLength:"50" example:"Basic usage"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Example description" maxLength:"300"`
	YAML        string `json:"yaml" yaml:"yaml" doc:"YAML example" minLength:"10" maxLength:"2000" required:"true"`
}
```

---

### 2.5 Update Executor to Validate Execution Mode

**File:** `pkg/provider/executor.go`

**Add new function after imports:**
```go
// validateExecutionMode checks that execution mode is set and matches provider capabilities.
func validateExecutionMode(ctx context.Context, desc *Descriptor) error {
	execMode, ok := ExecutionModeFromContext(ctx)
	if !ok {
		return fmt.Errorf("execution mode not provided in context")
	}

	// Check if the execution mode matches declared capabilities
	for _, cap := range desc.Capabilities {
		if cap == execMode {
			return nil
		}
	}

	return fmt.Errorf("provider %q does not support capability %q; supported: %v", desc.Name, execMode, desc.Capabilities)
}
```

**Update Execute method (after line 77, before input resolution):**

Insert after `desc := provider.Descriptor()` check:
```go
	// Validate execution mode
	if err := validateExecutionMode(ctx, desc); err != nil {
		return nil, err
	}
```

---

### 2.6 Update Executor to Call Decode and Pass to Execute

**File:** `pkg/provider/executor.go`

**Update Execute method - change the provider execution section (around line 92):**

**Current:**
```go
	// Execute the provider (it will handle dry-run mode via context)
	outputPtr, err := provider.Execute(ctx, resolvedInputs)
```

**Change to:**
```go
	// Determine what to pass to Execute:
	// - If Decode is defined: call it and pass the decoded (typed) value
	// - If Decode is nil: pass the raw map[string]any
	var executionInput any = resolvedInputs
	if desc.Decode != nil {
		decoded, err := desc.Decode(resolvedInputs)
		if err != nil {
			return nil, fmt.Errorf("failed to decode inputs: %w", err)
		}
		executionInput = decoded
	}

	// Execute the provider with either typed input or map
	outputPtr, err := provider.Execute(ctx, executionInput)
```

**Key points:**
- No context indirection needed - decoded value goes directly to Execute
- Providers with `Decode` receive their typed struct
- Providers without `Decode` receive `map[string]any` (existing behavior)
- Clean, explicit data flow

---

### 2.7 Update Registry Validation for MockBehavior and Schema

**File:** `pkg/provider/registry.go`

**Update validateDescriptor (around line 245):**

**Add after capabilities validation:**
```go
	// MockBehavior is required (all providers must support dry-run)
	if desc.MockBehavior == "" {
		return fmt.Errorf("provider MockBehavior cannot be empty (all providers must support dry-run)")
	}
```

**Update schema validation (around line 257):**

**Current:**
```go
	// Schema is required
	if desc.Schema.Properties == nil {
		return fmt.Errorf("provider schema cannot be nil")
	}
```

**Change to (require explicit initialization, don't mutate):**
```go
	// Schema Properties must be explicitly initialized
	// Providers with no inputs should use an empty map: map[string]PropertyDefinition{}
	if desc.Schema.Properties == nil {
		return fmt.Errorf("provider Schema.Properties must be initialized (use empty map for providers with no inputs)")
	}
```

**Rationale:** Don't silently mutate the descriptor. Require explicit initialization which is more Go-idiomatic and prevents accidental nil maps.

---

### 2.8 Update All Built-in Providers

Each provider needs:
1. `Execute` signature changed from `inputs map[string]any` to `input any`
2. Type assertion at start of Execute (either to typed struct if Decode is set, or to `map[string]any` if not)
3. `APIVersion` field set
4. `MockBehavior` field set
5. Error wrapping with provider name
6. Structured logging (if missing)
7. Optionally add `Decode` function for type-safe input handling

**Files to update:**
- `pkg/provider/builtin/httpprovider/http.go`
- `pkg/provider/builtin/envprovider/env.go`
- `pkg/provider/builtin/staticprovider/static.go`
- `pkg/provider/builtin/validationprovider/validation.go`
- `pkg/provider/builtin/celprovider/*.go`
- `pkg/provider/builtin/fileprovider/file.go`
- `pkg/provider/builtin/gitprovider/git.go`
- `pkg/provider/builtin/execprovider/exec.go`
- `pkg/provider/builtin/debugprovider/debug.go`
- `pkg/provider/builtin/sleepprovider/sleep.go`
- `pkg/provider/builtin/parameterprovider/parameter.go`

**Ensure each descriptor has:**
```go
APIVersion:   "v1",
MockBehavior: "Returns mock data without performing actual operation",
```

**Error wrapping pattern (with Decode - typed input):**
```go
func (p *HTTPProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    in := input.(HTTPInput)  // Type assertion - Executor guarantees this when Decode is set
    
    // ... execution using in.URL, in.Method, etc. ...
    if err != nil {
        return nil, fmt.Errorf("%s: %w", p.descriptor.Name, err)
    }
    return &provider.Output{Data: result}, nil
}
```

**Error wrapping pattern (without Decode - map input):**
```go
func (p *StaticProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    inputs := input.(map[string]any)  // Type assertion - Executor passes map when Decode is nil
    
    // ... execution ...
    if err != nil {
        return nil, fmt.Errorf("%s: %w", p.descriptor.Name, err)
    }
    return &provider.Output{Data: result}, nil
}
```

---

### 2.9 HTTP Provider Specific Updates

**File:** `pkg/provider/builtin/httpprovider/http.go`

**Add APIVersion and MockBehavior to descriptor:**
```go
&provider.Descriptor{
	Name:        ProviderName,
	DisplayName: "HTTP Client",
	APIVersion:  "v1",  // ADD THIS
	Description: "Makes HTTP/HTTPS requests to APIs and web services",
	Version:     version,
	Category:    "network",
	MockBehavior: "Returns mock HTTP response with status 200 and placeholder body without making actual network request",  // ADD THIS
	// ... rest unchanged
}
```

---

### 2.10 Env Provider Specific Updates

**File:** `pkg/provider/builtin/envprovider/env.go`

**Add APIVersion and MockBehavior to descriptor:**
```go
&provider.Descriptor{
	Name:        ProviderName,
	DisplayName: "Environment Variables",
	APIVersion:  "v1",  // ADD THIS
	Description: "Provider for reading and setting environment variables",
	Version:     version,
	Category:    "system",
	MockBehavior: "Returns mock environment variable values without accessing or modifying actual environment",  // ADD THIS
	// ... rest unchanged
}
```

---

## Phase 3: Test Updates

### 3.1 Update Test Files

All test files need to be updated for the new `Execute(ctx, input any)` signature.

Tests that call `provider.Execute()` via the Executor need execution mode in context:

**Pattern:**
```go
// Ensure context has execution mode for Executor validation
ctx = provider.WithExecutionMode(ctx, provider.CapabilityFrom)
result, err := executor.Execute(ctx, p, inputs)
```

**Files to update (add execution mode to test contexts):**
- `pkg/provider/executor_test.go`
- `pkg/provider/builtin/httpprovider/http_test.go`
- `pkg/provider/builtin/envprovider/env_test.go`
- `pkg/provider/builtin/staticprovider/static_test.go`
- `pkg/provider/builtin/validationprovider/validation_test.go`
- `pkg/provider/builtin/celprovider/*_test.go`
- `pkg/provider/builtin/fileprovider/file_test.go`
- `pkg/provider/builtin/gitprovider/git_test.go`
- `pkg/provider/builtin/execprovider/exec_test.go`
- `pkg/provider/builtin/debugprovider/*_test.go`
- `pkg/provider/builtin/sleepprovider/sleep_test.go`
- `pkg/provider/builtin/parameterprovider/*_test.go`

**Additional tests to add:**
- Context cancellation tests for long-running providers
- Retry behavior tests for HTTP provider
- Metrics collection tests

---

## Phase 4: Cross-Cutting Concerns

### 4.1 Error Handling Consistency

**Requirement:** All providers should wrap errors with provider context for better debugging.

**Pattern for providers with Decode (typed input):**
```go
func (p *HTTPProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    in := input.(HTTPInput)
    
    result, err := doOperation(in.URL, in.Method)
    if err != nil {
        return nil, fmt.Errorf("%s: %w", p.descriptor.Name, err)
    }
    
    return &provider.Output{Data: result}, nil
}
```

**Pattern for providers without Decode (map input):**
```go
func (p *StaticProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    inputs := input.(map[string]any)
    
    result, err := doOperation(inputs)
    if err != nil {
        return nil, fmt.Errorf("%s: %w", p.descriptor.Name, err)
    }
    
    return &provider.Output{Data: result}, nil
}
```

**Files to update:** All provider Execute methods should prefix errors with provider name.

---

### 4.2 Structured Logging

**Requirement:** Add logr-based structured logging to providers for observability.

**Pattern for providers:**
```go
func (p *HTTPProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    lgr := logger.FromContext(ctx)
    in := input.(HTTPInput)  // or input.(map[string]any) for providers without Decode
    
    // Log at verbose level (V(1) for debug info)
    lgr.V(1).Info("executing provider", "provider", p.descriptor.Name, "url", in.URL)
    
    // ... execution ...
    
    // Log warnings
    if len(warnings) > 0 {
        lgr.V(0).Info("provider completed with warnings", "provider", p.descriptor.Name, "warningCount", len(warnings))
    }
    
    return &provider.Output{Data: result, Warnings: warnings}, nil
}
```

**Files to update:**
- All providers that don't already use logging (check each provider)
- Add import for `github.com/oakwood-commons/scafctl/pkg/logger`

---

### 4.3 Execution Metrics ✅ IMPLEMENTED

**Requirement:** Add optional metrics collection for provider execution.

**Implementation:**

Provider metrics are collected at two levels:
1. **In-memory metrics** (`pkg/provider/metrics.go`) - For CLI output via `--show-metrics`
2. **Prometheus metrics** (`pkg/metrics/metrics.go`) - For observability/scraping

**In-memory Metrics (`pkg/provider/metrics.go`):**
```go
// ExecutionMetrics tracks execution statistics for a provider.
type ExecutionMetrics struct {
    ExecutionCount   uint64 // Total number of executions
    SuccessCount     uint64 // Number of successful executions
    FailureCount     uint64 // Number of failed executions
    TotalDurationNs  uint64 // Total execution duration in nanoseconds
    LastExecutionNs  uint64 // Timestamp of last execution
}

// Metrics provides global provider metrics collection.
type Metrics struct {
    enabled    bool
    prometheus bool       // Whether to also record to Prometheus
    providers  sync.Map   // map[string]*ExecutionMetrics
}

// GlobalMetrics is the default metrics collector.
var GlobalMetrics = &Metrics{enabled: false}

// Key methods:
// - Enable() / Disable() - Toggle in-memory collection
// - EnablePrometheus() / DisablePrometheus() - Toggle Prometheus recording
// - Record(providerName, duration, success) - Record execution result
// - GetMetrics(providerName) - Get metrics for a specific provider
// - GetAllMetrics() - Get all provider metrics (for CLI output)
```

**Prometheus Metrics (`pkg/metrics/metrics.go`):**
```go
// ProviderExecutionDuration - Histogram of execution durations by provider and status
ProviderExecutionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name: "scafctl_provider_execution_duration_seconds",
    Help: "Provider execution duration in seconds",
}, []string{"provider_name", "status"})

// ProviderExecutionTotal - Counter of executions by provider and status
ProviderExecutionTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "scafctl_provider_execution_total",
    Help: "Total number of provider executions",
}, []string{"provider_name", "status"})
```

**Executor integration (`pkg/provider/executor.go`):**
```go
// After provider execution:
GlobalMetrics.Record(desc.Name, executionDuration, err == nil)
```

**CLI Integration (`scafctl run solution --show-metrics`):**

The `--show-metrics` flag enables in-memory metrics collection and displays a summary table after execution:

```
Provider Execution Metrics:
--------------------------------------------------------------------------------
Provider                    Total  Success  Failure  Avg Duration    Success %
--------------------------------------------------------------------------------
cel                             5        5        0          1ms       100.0%
http                            3        2        1         250ms       66.7%
static                          2        2        0          0ms       100.0%
--------------------------------------------------------------------------------
```

---

### 4.4 Context Cancellation Handling ✅ AUDITED

**Requirement:** All long-running providers must respect context cancellation.

**Providers that MUST handle cancellation:**
- `httpprovider` - HTTP requests should use `http.NewRequestWithContext`
- `execprovider` - Shell commands should use `exec.CommandContext`
- `sleepprovider` - Already handles this correctly ✓
- `gitprovider` - Git operations should check context

**Pattern for providers:**
```go
func (p *SomeProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
    // Check cancellation early
    select {
    case <-ctx.Done():
        return nil, fmt.Errorf("%s: %w", p.descriptor.Name, ctx.Err())
    default:
    }
    
    // For long operations, periodically check:
    for _, item := range items {
        select {
        case <-ctx.Done():
            return nil, fmt.Errorf("%s: operation cancelled: %w", p.descriptor.Name, ctx.Err())
        default:
        }
        // process item
    }
    
    return &provider.Output{Data: result}, nil
}
```

**Files to audit and update:**
- `pkg/provider/builtin/httpprovider/http.go` - Verify `NewRequestWithContext` is used
- `pkg/provider/builtin/execprovider/exec.go` - Ensure `CommandContext` is used
- `pkg/provider/builtin/gitprovider/git.go` - Add context checks
- `pkg/provider/builtin/fileprovider/file.go` - Add context checks for large file reads

---

### 4.5 HTTP Provider Retry Behavior

**Requirement:** Add configurable retry support for transient failures.

**Add to HTTP provider schema:**
```go
"retry": {
    Type:        provider.PropertyTypeAny,
    Required:    false,
    Description: "Retry configuration for transient failures",
    Example: map[string]any{
        "maxAttempts": 3,
        "backoff":     "exponential",
        "retryOn":     []int{429, 500, 502, 503, 504},
    },
},
```

**Add retry logic:**
```go
type retryConfig struct {
    MaxAttempts int      `json:"maxAttempts"`
    Backoff     string   `json:"backoff"` // "none", "linear", "exponential"
    RetryOn     []int    `json:"retryOn"` // HTTP status codes to retry
    InitialWait string   `json:"initialWait"` // e.g., "1s"
}

func (p *HTTPProvider) executeWithRetry(ctx context.Context, req *http.Request, cfg retryConfig) (*http.Response, error) {
    var lastErr error
    for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        default:
        }
        
        resp, err := p.client.Do(req)
        if err == nil && !shouldRetry(resp.StatusCode, cfg.RetryOn) {
            return resp, nil
        }
        
        lastErr = err
        if resp != nil {
            lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
            resp.Body.Close()
        }
        
        // Wait before retry (with context)
        wait := calculateBackoff(attempt, cfg)
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-time.After(wait):
        }
    }
    return nil, fmt.Errorf("max retries exceeded: %w", lastErr)
}
```

---

### 4.6 Streaming Output Support (Future)

**Requirement:** Support streaming for large data operations.

**Note:** This is a more significant change that may require interface modifications. Defer to a future phase but document the approach:

**Approach:**
1. Add `StreamingOutput` interface for providers that support streaming
2. Add `SupportsStreaming() bool` to Descriptor
3. Executor detects streaming and handles accordingly

**Candidate providers for streaming:**
- `httpprovider` - Large response bodies
- `fileprovider` - Large file reads
- `execprovider` - Command output streaming

**Deferred to:** Phase 5 (post initial implementation)

---

### 4.7 Simplified Struct Tags

**Recommendation:** Simplify huma tags based on best practices.

**Guidelines:**
1. Always include `doc` tag
2. Use `format` instead of `pattern` when a built-in format exists (email, uri, uuid, etc.)
3. Include `example` for scalar fields only (not arrays/objects)
4. Use `patternDescription` only for complex custom patterns
5. Prefer `omitempty` for optional fields over explicit `required:"false"`

**Updated Descriptor tags (simplified):**
```go
type Descriptor struct {
    Name         string          `json:"name" yaml:"name" doc:"Unique provider identifier" minLength:"2" maxLength:"100" example:"http" pattern:"^[a-z][a-z0-9-]*$" required:"true"`
    DisplayName  string          `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name" maxLength:"100" example:"HTTP Client"`
    APIVersion   string          `json:"apiVersion" yaml:"apiVersion" doc:"Provider API version" example:"v1" pattern:"^v[0-9]+$" required:"true"`
    Version      *semver.Version `json:"version" yaml:"version" doc:"Semantic version" required:"true"`
    Description  string          `json:"description" yaml:"description" doc:"Provider description" minLength:"10" maxLength:"500" required:"true"`
    Schema       SchemaDefinition                      `json:"schema" yaml:"schema" doc:"Input schema" required:"true"`
    OutputSchemas map[Capability]SchemaDefinition      `json:"outputSchemas" yaml:"outputSchemas" doc:"Output schemas per capability" required:"true"`
    Decode       func(map[string]any) (any, error) `json:"-" yaml:"-"`
    MockBehavior string                            `json:"mockBehavior" yaml:"mockBehavior" doc:"Dry-run behavior description" minLength:"10" maxLength:"500" required:"true"`
    Capabilities []Capability                      `json:"capabilities" yaml:"capabilities" doc:"Supported execution contexts" minItems:"1" required:"true"`
    Category     string                            `json:"category,omitempty" yaml:"category,omitempty" doc:"Classification category" maxLength:"50" example:"network"`
    Tags         []string                          `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable keywords" maxItems:"20"`
    Icon         string                            `json:"icon,omitempty" yaml:"icon,omitempty" doc:"Icon URL" format:"uri" maxLength:"500"`
    Links        []Link                            `json:"links,omitempty" yaml:"links,omitempty" doc:"Related links" maxItems:"10"`
    Examples     []Example                         `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Usage examples" maxItems:"10"`
    Deprecated   bool                              `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Deprecation status"`
    Beta         bool                              `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Beta status"`
    Maintainers  []Contact                         `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"Maintainer contacts" maxItems:"10"`
}

type Contact struct {
    Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"Maintainer name" maxLength:"60" example:"Jane Doe"`
    Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"Maintainer email" format:"email" maxLength:"100"`
}

type Link struct {
    Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"Link name" maxLength:"30" example:"Documentation"`
    URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"Link URL" format:"uri" maxLength:"500"`
}

type Example struct {
    Name        string `json:"name,omitempty" yaml:"name,omitempty" doc:"Example name" maxLength:"50" example:"Basic usage"`
    Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Example description" maxLength:"300"`
    YAML        string `json:"yaml" yaml:"yaml" doc:"YAML example" minLength:"10" maxLength:"2000" required:"true"`
}
```

---

### 4.8 Schema Properties Validation

**Recommendation:** Require explicit initialization, don't mutate silently.

**Update `validateDescriptor` in `registry.go`:**
```go
// Schema Properties must be explicitly initialized
if desc.Schema.Properties == nil {
    return fmt.Errorf("provider Schema.Properties must be initialized (use empty map for providers with no inputs)")
}
```

**Update providers with no inputs to use explicit empty map:**
```go
Schema: provider.SchemaDefinition{
    Properties: map[string]provider.PropertyDefinition{}, // explicit empty
},
```

---

### 4.9 Validation Provider Return Simplification

**Recommendation:** Return the validated value directly on success.

**Update validation provider return:**
```go
// Success - return the validated value (useful in transform chains)
return &provider.Output{
    Data: valueBeingValidated,
    Metadata: map[string]any{
        "matchedPatterns": matchedPatterns, // optional context
    },
}, nil

// Failure - return error
return nil, fmt.Errorf("validation failed: %s", message)
```

**Update OutputSchemas:**
```go
OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
    provider.CapabilityValidation: {
        Properties: map[string]provider.PropertyDefinition{
            "valid": {
                Type:        provider.PropertyTypeBool,
                Description: "Whether validation passed",
            },
            "errors": {
                Type:        provider.PropertyTypeArray,
                Description: "List of validation error messages",
            },
            "value": {
                Type:        provider.PropertyTypeAny,
                Description: "The validated value (returned directly for use in transform chains)",
            },
        },
    },
},
```

---

## Phase 5: Migration Checklist

### Pre-Implementation
- [x] Review and approve this plan
- [ ] Create feature branch

### Phase 1: Design Doc Updates ✅ COMPLETED
- [x] Update `docs/design/providers.md` with all documented changes
  - [x] 1.1 Update Context Keys Section (string type, add parametersKey)
  - [x] 1.2 Update ResolverContextFromContext Signature (return tuple)
  - [x] 1.3 Update Built-in Providers Section (api→http, filesystem→file, add validation/exec/debug/sleep)
  - [x] 1.4 Update Validation Provider Return Spec (return validated value directly)
  - [x] 1.5 Update PropertyType Documentation (clarify any for maps)
  - [x] 1.6 Update Output.Warnings maxItems (changed to 50)
  - [x] 1.7 Update Provider Interface Execute Signature documentation
  - [x] 1.8 Add Execution Mode Validation Documentation
  - [x] Simplified struct tags (Contact, Link, Example using format over pattern)

### Phase 2: Core Implementation ✅ COMPLETED
- [x] Update `Provider` interface in `provider.go` (change `inputs map[string]any` to `input any`)
- [x] Update `Output.Warnings` maxItems tag
- [x] Add simplified struct tags to `Descriptor`, `Contact`, `Link`, `Example`
- [x] Add `validateExecutionMode` function to `executor.go`
- [x] Update `Execute` method to call `validateExecutionMode`
- [x] Update `Execute` method to call `Decode` when set and pass result directly to provider
- [x] Update `validateDescriptor` in `registry.go` (MockBehavior required, explicit schema check)

### Phase 3: Provider Updates (Signature + Metadata) ✅ COMPLETED
- [x] Update `httpprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `envprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `staticprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `validationprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `celprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `fileprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `gitprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `execprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `debugprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `sleepprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `parameterprovider` (Execute signature, type assertion, APIVersion, MockBehavior)
- [x] Update `plugin/wrapper.go` (Execute signature, type assertion)

Note: Retry support for HTTP provider deferred to Phase 4.

### Phase 4: Cross-Cutting Concerns ✅ COMPLETED
- [x] Add error wrapping with provider name to all providers
- [x] Add structured logging (logr) to providers missing it
- [x] Create `pkg/provider/metrics.go` for execution metrics
- [x] Update executor to record metrics
- [x] Audit and fix context cancellation handling in all providers
- [x] Add retry support to HTTP provider

### Phase 5: Test Updates ✅ COMPLETED
- [x] Update all provider test files to use new Execute signature
- [x] Add execution mode to test contexts
- [x] Update mock providers with required fields (APIVersion, Description, MockBehavior)
- [x] Add tests for new validation rules (APIVersion, Description, MockBehavior validation)
- [x] Add tests for metrics collection (metrics_test.go)
- [x] Add tests for Decode integration (typed input flow)
  - `TestExecutor_Execute_WithDecode` - Verifies Decode is called and typed input passed
  - `TestExecutor_Execute_WithDecodeError` - Verifies decode errors are propagated
  - `TestExecutor_Execute_WithoutDecode` - Verifies map[string]any passed when no Decode
- [x] Add tests for context cancellation
  - `TestExecutor_Execute_ContextCancellation` - Verifies executor respects context
  - `TestExecProvider_Execute_ContextCancellation` - Verifies exec provider respects context
  - `TestSleepProvider_Execute_ContextCancellation` - Verifies sleep provider respects context (pre-existing)
  - `TestHTTPProvider_Execute_RetryContextCancellation` - Verifies HTTP provider respects context during retry
- [x] Add tests for retry behavior (HTTP provider)
  - `TestHTTPProvider_Execute_RetryOnServerError` - Verifies retry on 500 errors
  - `TestHTTPProvider_Execute_RetryExhausted` - Verifies behavior when all retries fail
  - `TestHTTPProvider_Execute_RetryLinearBackoff` - Verifies linear backoff timing
  - `TestHTTPProvider_Execute_RetryExponentialBackoff` - Verifies exponential backoff timing
  - `TestHTTPProvider_Execute_NoRetryOnNonRetryableStatus` - Verifies no retry on 4xx
  - `TestHTTPProvider_Execute_RetryOnRateLimited` - Verifies retry on 429 rate limit
- [x] Run full test suite: `go test ./...` ✅ All tests passing
- [x] Run linter: `golangci-lint run` ✅ 0 new issues (4 pre-existing unparam warnings)

### Post-Implementation
- [x] Audit resolver code that calls providers - No changes needed (executor.go passes `map[string]any` which works with `input any`)
- [x] Audit action code that calls providers - N/A (no action code exists yet)
- [x] Fix pre-existing unparam warnings - Added `//nolint:unparam` comments with justification
- [x] Verify `writeMetrics()` implementation for `--show-metrics` CLI flag - Already implemented

### Future (Phase 6) - DEFERRED INDEFINITELY
- [ ] Evaluate streaming output support for large data operations
  - Rationale: No current use case; can be revisited when needed
