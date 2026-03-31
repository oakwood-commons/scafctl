---
title: "Providers"
weight: 3
---

# Providers

## Purpose

Providers are stateless execution primitives. They perform a single, well-defined operation given validated inputs and return either a result or an error.

Providers do not own orchestration, control flow, dependency resolution, or lifecycle decisions. This separation keeps solutions deterministic, testable, and explicit.

Providers are used by:

- Resolvers (during resolve, transform, and validate phases)
- Actions (during execution or render)

Providers are never invoked implicitly. A provider runs only when explicitly referenced.

---

## Implementation Status

| Feature | Status | Location |
|---------|--------|----------|
| Provider Interface | ✅ Implemented | `pkg/provider/provider.go` |
| Descriptor with schemas | ✅ Implemented | `pkg/provider/provider.go` |
| All 5 Capabilities | ✅ Implemented | `from`, `transform`, `validation`, `authentication`, `action` |
| Execution Context | ✅ Implemented | `pkg/provider/context.go` |
| Input Resolution (literal, rslvr, expr, tmpl) | ✅ Implemented | `pkg/provider/inputs.go` |
| Schema Validation | ✅ Implemented | `pkg/provider/validation.go` |
| Executor Lifecycle | ✅ Implemented | `pkg/provider/executor.go` |
| In-Memory Metrics (`--show-metrics`) | ✅ Implemented | `pkg/provider/metrics.go` |
| Prometheus Metrics | ✅ Implemented | `pkg/metrics/metrics.go` |
| Registry with versioning | ✅ Implemented | `pkg/provider/registry.go` |
| Built-in Providers | ✅ Implemented | `pkg/provider/builtin/` |
| Capability-required output fields | ✅ Implemented | `CapabilityRequiredOutputFields` |
| Secret handling (`SensitiveFields`) | ✅ Implemented | Redaction via `SecretMask` |
| Iteration Context | ✅ Implemented | `__item`, `__index`, aliases |

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

> **Status**: ✅ Implemented in `pkg/provider/context.go`

Providers are invoked with a resolved execution context.

**Resolver Context in Go Context:**

The resolver context (the `_` map containing all emitted resolver values) is stored in the Go `context.Context` as a `sync.Map`:

- Access via: `ResolverContextFromContext(ctx)` returns `map[string]any`
- In resolver execution, contains all previously emitted resolver values
- In action execution, may contain additional action-local variables
- Providers access resolver values via the returned map: `data["resolverName"]`

**Special symbols in resolver context:**

- `__self` - The current value being transformed or validated (available in transform and validate phases)
- `__item` - The current item in a foreach loop (available in action iterations)
- `__index` - The current zero-based index in a foreach loop (available in action iterations)
- `__actions` - Map of completed action results (available during action execution)

**Iteration aliases:**

Actions with `forEach` can define custom aliases for iteration variables:
- `itemAlias` - Custom variable name for `__item` (e.g., `service` instead of `__item`)
- `indexAlias` - Custom variable name for `__index` (e.g., `i` instead of `__index`)

Both default names and aliases are available simultaneously in the context.

**Key principles:**

- Resolver context contains only resolver outputs (nothing exists unless emitted by a resolver)
- Providers do not evaluate expressions or templates themselves—all inputs are fully resolved before invocation
- The resolver context map is read-only; providers should not mutate it
- Access to the resolver context is optional—providers that don't need it don't have to retrieve it

---

## Execution Lifecycle

> **Status**: ✅ Implemented in `pkg/provider/executor.go`

Providers follow a strict execution pipeline to ensure consistent behavior and validation:

{{< mermaid >}}
flowchart TD
    A[Provider Invocation Request] --> B["1. Schema Validation<br/>Validate inputs against Descriptor.Schema"]
    B --> C["2. Decode (optional)<br/>Convert map to strongly-typed struct"]
    C --> D["3. Execute<br/>Provider-specific logic"]
    D --> E["4. Output Schema Validation<br/>Validate Output.Data against OutputSchemas"]
    E --> F["5. Return Output<br/>Data, Warnings, Metadata"]
{{< /mermaid >}}

**Lifecycle Phases:**

1. **Schema Validation** - scafctl validates all input values against the provider's declared `Schema` before invocation. Invalid inputs result in an error before `Execute()` is called.

2. **Decode** - If the provider defines a `Decode` function in its descriptor, scafctl calls it to convert the validated `map[string]any` into a strongly-typed structure. This step is optional; providers can work directly with maps.

3. **Execute** - The provider's `Execute(ctx, input)` method runs with the validated (and optionally decoded) inputs. The provider performs its operation based on the execution mode and dry-run flag from the context. Providers that need access to resolver values retrieve them via `ResolverContextFromContext(ctx)`. Providers producing user-visible output can stream it to the terminal via `IOStreamsFromContext(ctx)` and set `Output.Streamed = true` to prevent double-printing.

4. **Output Schema Validation** - scafctl validates the `Output.Data` field against the provider's `OutputSchemas` for the current capability. Each capability can define different required output fields. This ensures both real and mock outputs conform to the declared structure for the specific execution context.

5. **Return** - The validated `Output` (containing `Data`, optional `Warnings`, optional `Metadata`, and optional `Streamed` flag) is returned to the caller (resolver or action orchestrator).

**Error Handling:**

- Errors during schema validation (phase 1) or output schema validation (phase 4) are structural errors indicating misconfiguration
- Errors during decode (phase 2) indicate type conversion failures
- Errors during execute (phase 3) are provider-specific operational errors
- All errors prevent the provider output from being used in subsequent resolution steps

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

---

## Observability & Metrics

> **Status**: ✅ Implemented

Provider execution is instrumented for observability at two levels:

### In-Memory Metrics (CLI Output)

> **Status**: ✅ Implemented in `pkg/provider/metrics.go`

When running with `--show-metrics`, scafctl collects per-provider execution statistics:

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

**Tracked metrics per provider:**
- `ExecutionCount` - Total number of invocations
- `SuccessCount` - Number of successful executions
- `FailureCount` - Number of failed executions
- `TotalDurationNs` - Cumulative execution time
- `LastExecutionNs` - Timestamp of most recent execution

**Usage:**
```bash
scafctl run solution -f solution.yaml --show-metrics
```

### Prometheus Metrics (Observability)

> **Status**: ✅ Implemented in `pkg/metrics/metrics.go`

Provider metrics are also exported as Prometheus metrics for integration with monitoring systems:

**`scafctl_provider_execution_duration_seconds`** (Histogram)
- Labels: `provider_name`, `status` (success/failure)
- Tracks execution duration distribution per provider
- Recorded via `metrics.RecordProviderExecution()`

**`scafctl_provider_execution_total`** (Counter)
- Labels: `provider_name`, `status` (success/failure)
- Tracks total invocation count per provider
- Recorded via `metrics.RecordProviderExecution()`

These metrics are recorded automatically when Prometheus metrics are enabled via `metrics.RegisterMetrics()`. The provider executor calls `GlobalMetrics.Record()` which dispatches to both in-memory and Prometheus collectors.

### Logging

All providers implement structured logging via logr:
- Execution start/completion logged at V(1) verbosity
- Error details logged at V(0) verbosity
- Provider name included in all log messages

**Usage:**
```bash
# Logs are suppressed by default. Enable debug to see provider execution:
scafctl run solution -f solution.yaml --debug

# Or use a specific V-level:
scafctl run solution -f solution.yaml --log-level 1
```

---

## Provider Model

### Notes

- All action-capable providers should implement a `WhatIf` function on their `Descriptor` to generate context-specific messages describing what they would do with given inputs.
- When `WhatIf` is not implemented, `DescribeWhatIf()` returns a generic message: "Would execute {provider} provider".

### Conceptual Flow

- inputs (map)
  - schema validation
  - decode to typed input
  - execute operation
  - output or error

Providers behave as isolated execution units with no implicit coupling to other providers.

### Context-Based Execution Control

Providers receive execution control information through the context parameter, including which provider capability (execution mode) they're being invoked with and whether dry-run is enabled.

**Context Keys:**

scafctl uses typed context keys to prevent collisions and provide type safety:

- `executionModeKey` (unexported) - The provider capability (execution mode) being invoked (from, transform, validation, action, authentication) as `Capability`
  - Access via: `ExecutionModeFromContext(ctx)`
- `dryRunKey` (unexported) - Boolean indicating whether this is a dry-run execution (set by `run provider --dry-run` only)
  - Access via: `DryRunFromContext(ctx)`
  - Not set during `run solution --dry-run` — solution-level dry-run uses the `WhatIf` function on the `Descriptor` instead (providers are never executed)
- `resolverContextKey` (unexported) - The resolver context map containing all emitted resolver values
  - Access via: `ResolverContextFromContext(ctx)` returns `map[string]any`

Typed keys ensure external packages cannot accidentally use the same context keys, preventing subtle bugs.

**Execution Mode (Capability):**

- scafctl passes the provider capability via the execution mode context value to indicate how the provider is being invoked
- The capability must match one of the provider's declared capabilities
- Providers can use this to adjust behavior based on context (e.g., read-only vs mutation)
- This enables providers to support multiple capabilities with context-aware behavior

**WhatIf Descriptions:**

- Dry-run uses a WhatIf model: resolvers execute normally (they are side-effect-free), and each action's provider generates a WhatIf message describing what it would do
- Providers implement `WhatIf` on their `Descriptor` to generate context-specific descriptions from materialized inputs
- The `DescribeWhatIf(ctx, input)` helper method on `Descriptor` provides a fallback chain: WhatIf func → generic message
- Action providers are never invoked during dry-run — only their WhatIf descriptions are used

**Implementation Pattern:**

~~~go
// Provider implementation checks context for execution mode
// Note: This example uses helper methods that represent implementation-specific logic.
func (p *APIProvider) Execute(ctx context.Context, input any) (Output, error) {
  // Extract execution mode using typed accessor
  execMode, ok := ExecutionModeFromContext(ctx)
  if !ok {
    return Output{}, fmt.Errorf("execution mode not provided in context")
  }
  
  // Validate execution mode matches declared capabilities
  descriptor := p.Descriptor()
  supported := false
  for _, cap := range descriptor.Capabilities {
    if cap == execMode {
      supported = true
      break
    }
  }
  if !supported {
    return Output{}, fmt.Errorf("provider does not support capability: %s", execMode)
  }
  
  // Adjust behavior based on execution mode
  switch execMode {
  case CapabilityFrom:
    return p.executeGET(input)
  case CapabilityTransform:
    return p.executeTransform(input)
  case CapabilityValidation:
    return p.executeValidation(input)
  case CapabilityAuthentication:
    return p.executeAuth(input)
  case CapabilityAction:
    return p.executeMutation(input)
  default:
    return Output{}, fmt.Errorf("unsupported execution mode: %s", execMode)
  }
}
~~~

**WhatIf Implementation:**

Action-capable providers register a `WhatIf` function on their `Descriptor`:

~~~go
desc := &provider.Descriptor{
    Name:         "my-provider",
    WhatIf: func(ctx context.Context, input any) (string, error) {
        inputs, _ := input.(map[string]any)
        target, _ := inputs["target"].(string)
        return fmt.Sprintf("Would deploy to %s", target), nil
    },
    // ...
}
~~~

**Descriptor Declaration:**

- `WhatIf` - Optional function that generates a context-specific description of what the provider would do with given inputs

**Requirements:**

- Providers must validate the execution mode matches one of their declared capabilities
- Execution mode determines provider behavior (e.g., read-only vs mutation, data vs boolean return)
- Action-capable providers should implement `WhatIf` for accurate dry-run descriptions
- The `DescribeWhatIf(ctx, input)` method on `Descriptor` provides the fallback chain: WhatIf func → generic message
- Warnings and metadata are optional but encouraged for providing execution context

---

## Provider Capabilities

> **Status**: ✅ Implemented - All 5 capability types active

Providers declare their supported execution contexts through capabilities. Capabilities indicate which parts of the scafctl execution model a provider can participate in.

### Capability Types

**`from`** - Provider can be used in the `from` section of resolvers to supply or fetch values:

- Examples: `env`, `parameter`, `filesystem`, `api`, `git`
- Must return data that can be assigned to a resolver's value

**`transform`** - Provider can be used in the `transform.into` section of resolvers to modify values:

- Examples: `cel`, string manipulation providers, data conversion providers
- Receives `__self` as the current value and returns the transformed result
- Must be deterministic and produce consistent output for the same input

**`validation`** - Provider can be used in the `validate.from` section of resolvers:

- Examples: `validation` (built-in), custom validation providers
- Must return an `Output` whose `Data` field is a boolean indicating validation success (true) or failure (false)
- Should provide meaningful error context when validation fails

**`authentication`** - Provider supports authentication mechanisms:

- Examples: `oauth`, `api-key`, `certificate`, `token`
- May handle credential management, token refresh, or authentication flows
- Can be used by other providers that require authentication

**`action`** - Provider can be invoked as an action to perform side effects:

- Examples: `shell`, `api` (with POST/PUT/DELETE), `filesystem` (write operations)
- May modify external state, create resources, or trigger workflows
- Must support dry-run mode for planning and testing

### Requirements

- Every provider must declare at least one capability
- A provider may support multiple capabilities (e.g., `api` provider supports both `from` and `action`)
- The `Capabilities` field is used for:
  - Validation at provider registration
  - Catalog filtering and discovery
  - IDE/CLI autocomplete and validation
  - Runtime checks to ensure providers are used in valid contexts

### Future Extensibility

The capability model is designed for extension. Future capabilities may include:

- `caching` - Provider supports result caching
- `streaming` - Provider supports streaming data
- `batch` - Provider supports batch operations
- `webhook` - Provider can receive webhook notifications

---

## Provider Interface (Conceptual)

~~~go
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

// Output is the standardized return structure for all provider executions.
// It wraps the actual data along with optional warnings and metadata.
type Output struct {
  Data     any            `json:"data" doc:"The actual output data from provider execution (validated against OutputSchemas for current capability)"`
  Warnings []string       `json:"warnings,omitempty" doc:"Non-fatal warnings generated during execution" maxItems:"50"`
  Metadata map[string]any `json:"metadata,omitempty" doc:"Optional execution metadata (timing, resource usage, etc.)"`
}

// Capability represents an execution context a provider can participate in.
// This type provides compile-time type safety while still serializing as strings in YAML/JSON.
type Capability string

const (
  CapabilityFrom           Capability = "from"           // Provider can be used in resolver 'from' section
  CapabilityTransform      Capability = "transform"      // Provider can be used in resolver 'transform' section
  CapabilityValidation     Capability = "validation"     // Provider can be used in resolver 'validate' section
  CapabilityAuthentication Capability = "authentication" // Provider handles authentication
  CapabilityAction         Capability = "action"         // Provider can be invoked as an action
)

// IsValid checks if the capability is a known, recognized capability.
// Returns false for unknown capabilities to ensure only declared capability types are used.
func (c Capability) IsValid() bool {
  switch c {
  case CapabilityFrom, CapabilityTransform, CapabilityValidation, CapabilityAuthentication, CapabilityAction:
    return true
  default:
    return false
  }
}

// Context keys use string type for better debugging and traceability in logs.
// Using a custom type ensures external packages cannot accidentally use the same key.
type contextKey string

const (
  executionModeKey    contextKey = "scafctl.provider.executionMode"    // Key for Capability execution mode
  dryRunKey           contextKey = "scafctl.provider.dryRun"           // Key for boolean dry-run flag
  resolverContextKey  contextKey = "scafctl.provider.resolverContext"  // Key for resolver context map
  parametersKey       contextKey = "scafctl.provider.parameters"       // Key for CLI parameters map
  iterationContextKey contextKey = "scafctl.provider.iterationContext" // Key for forEach iteration context
)

// NOTE: These keys are intentionally unexported. scafctl's orchestration layer
// sets them using internal helpers such as:
//   - WithExecutionMode(ctx context.Context, mode Capability) context.Context
//   - WithDryRun(ctx context.Context, dryRun bool) context.Context
//   - WithResolverContext(ctx context.Context, data map[string]any) context.Context
//   - WithParameters(ctx context.Context, parameters map[string]any) context.Context
// Provider implementations should treat these as read-only and access them only
// via the accessor functions below, which provide context value accessors for
// provider implementations.
func ExecutionModeFromContext(ctx context.Context) (Capability, bool) {
  mode, ok := ctx.Value(executionModeKey).(Capability)
  return mode, ok
}

func DryRunFromContext(ctx context.Context) bool {
  dryRun, _ := ctx.Value(dryRunKey).(bool)
  return dryRun
}

// ResolverContextFromContext retrieves the resolver context map from the context.
// Returns the resolver context map and true if found, nil and false otherwise.
func ResolverContextFromContext(ctx context.Context) (map[string]any, bool) {
  resolverCtx, ok := ctx.Value(resolverContextKey).(map[string]any)
  return resolverCtx, ok
}

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

type Descriptor struct {
  // Identity and versioning
  Name        string          `json:"name" yaml:"name" doc:"Unique provider identifier" minLength:"2" maxLength:"100" example:"http" pattern:"^[a-z][a-z0-9-]*$" required:"true"`
  DisplayName string          `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name" maxLength:"100" example:"HTTP Client"`
  APIVersion  string          `json:"apiVersion" yaml:"apiVersion" doc:"Provider API version" example:"v1" pattern:"^v[0-9]+$" required:"true"`
  // Version uses github.com/Masterminds/semver/v3 for semantic versioning.
  Version     *semver.Version `json:"version" yaml:"version" doc:"Semantic version" required:"true"`
  Description string          `json:"description" yaml:"description" doc:"Provider description" minLength:"10" maxLength:"500" required:"true"`
  
  // Schema definitions (using JSON Schema via github.com/google/jsonschema-go/jsonschema)
  Schema          *jsonschema.Schema                    `json:"schema" yaml:"schema" doc:"Input schema (JSON Schema)" required:"true"`
  OutputSchemas   map[Capability]*jsonschema.Schema      `json:"outputSchemas" yaml:"outputSchemas" doc:"Output schemas per capability (JSON Schema)" required:"true"`
  SensitiveFields []string                              `json:"sensitiveFields,omitempty" yaml:"sensitiveFields,omitempty" doc:"Property names containing sensitive data" maxItems:"50"`
  // Decode converts validated map[string]any inputs into strongly-typed structs for internal use.
  // Called after schema validation but before Execute(). Optional - providers can work with map[string]any directly.
  // When Decode is set, the Executor calls it and passes the result directly to Execute().
  Decode        func(map[string]any) (any, error) `json:"-" yaml:"-"`
  
  // ExtractDependencies extracts resolver references from provider-specific input formats.
  // Called during dependency graph construction to determine resolver execution order.
  // Optional - if nil, the generic extraction logic is used which handles:
  // - ValueRef.Resolver references
  // - CEL expressions (_.resolverName patterns)
  // - Go templates ({{.resolverName}} patterns with default delimiters)
  // Providers should implement this when they have custom input formats that may
  // contain resolver references, such as templates with custom delimiters.
  // The function receives the raw inputs map and returns resolver names that this input depends on.
  ExtractDependencies func(inputs map[string]any) []string `json:"-" yaml:"-"`
  
  // Execution behavior
  Capabilities []Capability `json:"capabilities" yaml:"capabilities" doc:"Supported execution contexts" minItems:"1" required:"true"`
  
  // Catalog and distribution metadata
  Category   string    `json:"category,omitempty" yaml:"category,omitempty" doc:"Classification category" maxLength:"50" example:"network"`
  Tags       []string  `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable keywords" maxItems:"20"`
  Icon       string    `json:"icon,omitempty" yaml:"icon,omitempty" doc:"Icon URL" format:"uri" maxLength:"500"`
  Links      []Link    `json:"links,omitempty" yaml:"links,omitempty" doc:"Related links" maxItems:"10"`
  Examples   []Example `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Usage examples" maxItems:"10"`
  Deprecated bool      `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Deprecation status"`
  Beta       bool      `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Beta status"`
  
  // Maintainer information
  Maintainers []Contact `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"Maintainer contacts" maxItems:"10"`
}

// Schemas use *jsonschema.Schema from github.com/google/jsonschema-go/jsonschema.
// This replaces the former custom SchemaDefinition type with a standard JSON Schema representation.
//
// A jsonschema.Schema is a rich struct that supports all JSON Schema draft features including:
//   - Type ("string", "integer", "number", "boolean", "array", "object", or "" for any)
//   - Properties (map[string]*jsonschema.Schema for nested object schemas)
//   - Required ([]string listing required property names on the parent schema)
//   - Validation constraints (MinLength, MaxLength, Pattern, Minimum, Maximum, MinItems, MaxItems, Enum, Format)
//   - Metadata (Description, Title, Examples []any, Default json.RawMessage, Deprecated bool)
//   - Security hints (WriteOnly bool — used for sensitive/secret properties)
//   - Composition (Items for array element schemas, AdditionalProperties for maps)
//
// Provider schemas are typically constructed using the schemahelper package (pkg/provider/schemahelper/)
// which provides ergonomic builder functions like ObjectSchema(), StringProp(), IntProp(), etc.
//
// Example:
//   schema := schemahelper.ObjectSchema(
//     []string{"url", "method"},  // required fields
//     map[string]*jsonschema.Schema{
//       "url":    schemahelper.StringProp("Request URL", schemahelper.WithFormat("uri")),
//       "method": schemahelper.StringProp("HTTP method", schemahelper.WithEnum("GET", "POST", "PUT", "DELETE")),
//       "body":   schemahelper.AnyProp("Request body"),
//     },
//   )

// Required Output Fields by Capability:
//
// Certain capabilities mandate specific fields in their output schemas:
//
//   validation:      must include "valid" (bool) and "errors" ([]string)
//   authentication:  must include "authenticated" (bool) and "token" (string)
//   action:          must include "success" (bool)
//   from:            no required fields
//   transform:       no required fields
//
// These requirements are enforced at provider registration time.
// Providers can add additional fields beyond the required minimums.

// Property types use standard JSON Schema type strings set on the jsonschema.Schema.Type field.
// The former custom PropertyType enum has been removed in favor of these standard values:
//
//   "string"   - String values (was PropertyTypeString)
//   "integer"  - Integer values (was PropertyTypeInt)
//   "number"   - Floating-point values (was PropertyTypeFloat)
//   "boolean"  - Boolean values (was PropertyTypeBool)
//   "array"    - Array/slice values (was PropertyTypeArray)
//   "object"   - Object/map values (new — previously used PropertyTypeAny for maps)
//   ""         - Any type, no constraint (was PropertyTypeAny)
//
// The old IsValid() method is no longer needed; type validation is handled by the
// provider registry at registration time using a fixed set of valid type strings.
//
// The schemahelper package (pkg/provider/schemahelper/) provides typed builder
// functions that set the correct Type automatically:
//   schemahelper.StringProp()  → Type: "string"
//   schemahelper.IntProp()     → Type: "integer"
//   schemahelper.NumberProp()  → Type: "number"
//   schemahelper.BoolProp()    → Type: "boolean"
//   schemahelper.ArrayProp()   → Type: "array"
//   schemahelper.ObjectProp()  → Type: "object"
//   schemahelper.AnyProp()     → Type: "" (no type constraint)

// Individual properties are now defined directly as *jsonschema.Schema values.
// The former PropertyDefinition struct has been removed. Here is how old fields map
// to jsonschema.Schema fields:
//
//   Old PropertyDefinition field  →  jsonschema.Schema field
//   ─────────────────────────────────────────────────────────
//   Type        PropertyType       →  Type        string          ("string", "integer", etc.)
//   Required    bool               →  Required    []string        (on the PARENT schema, not per-property)
//   Description string             →  Description string
//   Default     any                →  Default     json.RawMessage (JSON-encoded default value)
//   Example     any                →  Examples    []any           (slice of example values)
//   MinLength   *int               →  MinLength   *int
//   MaxLength   *int               →  MaxLength   *int
//   Pattern     string             →  Pattern     string
//   Minimum     *float64           →  Minimum     *float64
//   Maximum     *float64           →  Maximum     *float64
//   MinItems    *int               →  MinItems    *int
//   MaxItems    *int               →  MaxItems    *int
//   Enum        []any              →  Enum        []any
//   Format      string             →  Format      string
//   Deprecated  bool               →  Deprecated  bool
//   IsSecret    bool               →  (removed — see SensitiveFields on Descriptor + WriteOnly on schema)
//
// Key differences from the old approach:
//   - Required is declared on the parent object schema as a string slice, not per-property.
//   - Default is json.RawMessage (use schemahelper.WithDefault() for ergonomic setting).
//   - Example is now Examples []any (a slice), set via schemahelper.WithExample().
//   - IsSecret is replaced by two mechanisms: Descriptor.SensitiveFields lists secret field names
//     for runtime redaction, and WriteOnly: true on a schema property signals secret intent.
//   - Additional fields are available: Title, Items (array element schema), AdditionalProperties, WriteOnly.
//
// Validation Behavior:
// - JSON Schema validation is type-aware: constraints only apply when they match the property type
//   (e.g., MinLength is only checked for "string" types, Minimum for "integer"/"number")
// - Validation is performed by the provider registry and executor framework
// - The schemahelper builder functions ensure type-appropriate constraints are set correctly

// Contact represents the maintainer's contact information, including their name and email address.
type Contact struct {
  Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"Maintainer name" maxLength:"60" example:"Jane Doe"`
  Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"Maintainer email" format:"email" maxLength:"100"`
}

// Link represents a named hyperlink with validation constraints.
type Link struct {
  Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"Link name" maxLength:"30" example:"Documentation"`
  URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"Link URL" format:"uri" maxLength:"500"`
}

// Example represents a usage example demonstrating how to invoke the provider.
// Examples help with documentation generation, catalog display, and IDE support.
type Example struct {
  Name        string `json:"name,omitempty" yaml:"name,omitempty" doc:"Example name" maxLength:"50" example:"Basic usage"`
  Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Example description" maxLength:"300"`
  YAML        string `json:"yaml" yaml:"yaml" doc:"YAML example" minLength:"10" maxLength:"2000" required:"true"`
}
~~~

This interface is illustrative. The exact implementation may evolve, but the contract remains schema-first and explicit.

---

## Input Resolution

> **Status**: ✅ Implemented in `pkg/provider/inputs.go`

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

Reference a resolver directly using `rslvr`. The value emitted by the resolver is copied, preserving its type.

~~~yaml
inputs:
  image:
    rslvr: imageResolver
  environment:
    rslvr: deploymentEnv
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

**Type Coercion:**

Templates always produce string output. When a template is used for a non-string input property:
- For `int` and `float` properties: The string is parsed using standard conversion (e.g., "42" → 42, "3.14" → 3.14)
- For `bool` properties: The string is parsed as boolean ("true"/"false", case-insensitive)
- For `map` and `array` properties: The string is parsed as JSON
- For `any` properties: The string value is passed as-is

Parsing errors result in schema validation failure before the provider executes. This validation occurs during the input resolution phase, not within the provider itself.

### Exclusivity Rule

For a single input field, you must specify exactly one of:

- A literal value
- `rslvr: resolverName`
- `expr: celExpression`
- `tmpl: "templateString"`

It is an error to specify more than one form for the same field.

---

## Providers in Resolvers

Resolvers invoke providers to obtain, transform, or validate values.

~~~yaml
resolve:
  with:
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
  with:
    - provider: cel
      inputs:
        expression: __self.toLowerCase()
~~~

Each step receives the previous value as `__self`.

---

## Providers in Validation

Validation is provider-backed.

**Return Value Structure:**

Validation providers follow a simple pattern:

~~~go
// Success - return the validated value directly (useful in transform chains)
return &Output{
  Data: valueBeingValidated,
  Metadata: map[string]any{
    "matchedPatterns": matchedPatterns, // optional context
  },
}, nil

// Failure - return an error
return nil, fmt.Errorf("validation failed: %s", message)
~~~

**Key points:**
- On success, `Data` contains the validated value (not a wrapper map)
- This enables validation to be used in transform chains where the value flows through
- Validation failures always return an error (not `Data.valid = false`)
- Optional metadata can provide context about which validations passed
- This approach distinguishes "validation ran and failed" (error) from "validation couldn't run" (different error)

The `Output.Warnings` field may be used to provide additional context for non-fatal issues, and error messages are typically provided through the resolver's `message` field rather than the provider output.

### Built-in Provider: validation

The built-in `validation` provider supports:

- `match` - regex pattern that must match (supports all input forms)
- `notMatch` - regex pattern that must not match (supports all input forms)
- `expression` - CEL expression returning boolean

Rules:

- `match` and `notMatch` may be combined
- `match` and `notMatch` support all four input forms (literal, rslvr, expr, tmpl)
- `expression` is for CEL-based validation
- On success, the provider returns the validated value in `Output.Data`
- On failure, the provider returns an error

Examples:

Literal regex patterns:

~~~yaml
validate:
  with:
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
  with:
    - provider: validation
      inputs:
        match:
          rslvr: validationPattern
      message: "Must match validation pattern"
~~~

Using template for pattern:

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        match:
          tmpl: "^{{ .allowedPrefix }}-[a-z0-9]+$"
      message: "Must match allowed prefix"
~~~

Using CEL expression for validation logic:

~~~yaml
validate:
  with:
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
    provider: exec
    inputs:
      command: "go build ./..."
~~~

Action orchestration, dependencies, iteration, and conditional execution are handled outside the provider.

---

## Built-in Providers (Non-Exhaustive)

### parameter

Reads a value supplied at invocation time via CLI flags.

~~~yaml
resolve:
  with:
    - provider: parameter
      inputs:
        key: env
~~~

---

### env

Reads from the process environment.

~~~yaml
resolve:
  with:
    - provider: env
      inputs:
        key: PROJECT_NAME
~~~

---

### static

Supplies a literal value.

~~~yaml
resolve:
  with:
    - provider: static
      inputs:
        value: my-app
~~~

---

### file

Filesystem operations: read, write, check existence, delete, and batch write-tree.

> **Note:** Previously documented as `filesystem`.

~~~yaml
# Read a file
resolve:
  with:
    - provider: file
      inputs:
        operation: read
        path: ./config/name.txt
~~~

**Operations:**
- `read` — Read file content
- `write` — Write content to a file
- `exists` — Check if a file exists
- `delete` — Delete a file
- `write-tree` — Batch-write an array of `{path, content}` entries under a base directory

**`write-tree` inputs:**
- `basePath` (required): Destination root directory
- `entries` (required): Array of `{path, content}` objects
- `outputPath`: Go template to transform each entry’s output path. Variables: `__filePath`, `__fileName`, `__fileStem`, `__fileExtension`, `__fileDir`

~~~yaml
# Batch-write rendered templates, stripping .tpl extensions
workflow:
  actions:
    write-output:
      provider: file
      inputs:
        operation: write-tree
        basePath: ./output
        entries:
          rslvr: rendered
        outputPath: >-
          {{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}
~~~

---

### directory

Lists, creates, removes, and copies directories with support for recursive traversal, glob/regex filtering, content reading, and checksums.

~~~yaml
resolve:
  with:
    - provider: directory
      inputs:
        operation: list
        path: ./src
        recursive: true
        filterGlob: "*.go"
~~~

---

### git

Reads data from a git repository or working tree.

~~~yaml
resolve:
  with:
    - provider: git
      inputs:
        field: branch
~~~

---

### http

Fetches data from an HTTP endpoint or makes HTTP requests.

> **Note:** Previously documented as `api`.

~~~yaml
resolve:
  with:
    - provider: http
      inputs:
        url: https://api.example.com/project
        method: GET
~~~

---

### cel

Derives a value using CEL expressions.

~~~yaml
resolve:
  with:
    - provider: cel
      inputs:
        expression: _.org + "/" + _.repo
~~~

---

### validation

Validates values using regex patterns and CEL expressions.

~~~yaml
validate:
  with:
    - provider: validation
      inputs:
        match: "^[a-z0-9-]+$"
      message: "Invalid value"
~~~

---

### exec

Executes shell commands.

~~~yaml
actions:
  build:
    provider: exec
    inputs:
      command: go build ./...
~~~

---

### debug

Provides debugging utilities for development and troubleshooting.

~~~yaml
resolve:
  with:
    - provider: debug
      inputs:
        message: "Current value"
        value:
          rslvr: someResolver
~~~

---

### message

Outputs styled, feature-rich terminal messages during solution execution. Supports built-in message types (success, warning, error, info, debug, plain), custom formatting with colors and icons via lipgloss, destination control (stdout/stderr), and respects `--quiet` and `--no-color` flags. For dynamic interpolation, use the framework's `tmpl:` or `expr:` ValueRef on the `message` input.

**Capabilities:** `action`

**Input Fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `message` | string | — | Message text. Use `tmpl:` or `expr:` ValueRef for dynamic content. |
| `type` | enum | `info` | `success`, `warning`, `error`, `info`, `debug`, `plain` |
| `label` | string | — | Contextual prefix rendered as dimmed `[label]` between icon and message |
| `style` | object | — | Custom formatting that merges on top of type defaults: `color`, `bold`, `italic`, `icon` |
| `destination` | enum | `stdout` | `stdout` or `stderr` |
| `newline` | bool | `true` | Whether to append trailing newline |

~~~yaml
# Built-in type styling
resolve:
  with:
    - provider: message
      inputs:
        message: "Deployment completed successfully"
        type: success

# Custom styling with icon
resolve:
  with:
    - provider: message
      inputs:
        message: "Starting pipeline"
        style:
          color: "#FF5733"
          bold: true
          icon: "\U0001F680"

# Go template interpolation via tmpl: ValueRef
resolve:
  with:
    - provider: message
      inputs:
        message:
          tmpl: "Deploying {{ .appName }} to {{ .environment }}"
        type: info

# CEL expression via expr: ValueRef
resolve:
  with:
    - provider: message
      inputs:
        message:
          expr: "'Processed ' + string(size(_.items)) + ' items'"
        type: success

~~~

---

### sleep

Introduces delays for testing and rate-limiting scenarios.

~~~yaml
resolve:
  with:
    - provider: sleep
      inputs:
        duration: 1s
~~~

---

### go-template

Renders Go text/template content with resolver data as the template context.
Supports single-template rendering (`render`, the default) and batch directory
rendering (`render-tree`).

~~~yaml
# Single template render
resolve:
  with:
    - provider: go-template
      inputs:
        name: greeting-template
        template: "Hello, {{.name}}!"
~~~

**Inputs (shared):**
- `operation`: `render` (default) or `render-tree`
- `name`: Name for the template (optional, defaults to `"render-tree"` for render-tree)
- `missingKey`: Behavior when a map key is missing: `default`, `zero`, or `error`
- `leftDelim`, `rightDelim`: Custom delimiters (default: `{{` and `}}`)
- `data`: Additional data to merge with resolver context
- `ignoredBlocks`: Blocks to pass through without template processing

**`render-tree` inputs:**
- `entries` (required): Array of `{path, content}` objects (typically from the directory provider with `includeContent: true`)

~~~yaml
# Batch-render a directory of templates
resolve:
  with:
    - provider: go-template
      inputs:
        operation: render-tree
        entries:
          expr: '_.templateFiles.entries'
        data:
          rslvr: vars
~~~

---

### secret

Retrieves encrypted secrets from the scafctl secrets store.

~~~yaml
resolve:
  with:
    - provider: secret
      inputs:
        operation: get
        name: api-key
~~~

**Operations:**
- `get` - Retrieve a secret by name
- `list` - List available secrets (optionally filtered by pattern)

**Inputs:**
- `operation` (required): One of `get` or `list`
- `name`: Secret name (required for `get` operation)
- `pattern`: Regex pattern for filtering secrets in `list` operation
- `required`: Whether the secret must exist (default: `true`)
- `fallback`: Fallback value if secret doesn't exist and `required: false`

**Output (get operation):**
~~~yaml
value: "the-secret-value"
name: "api-key"
~~~

**Security:** Secrets are stored with AES-256-GCM encryption and master keys are managed via OS keychain integration.
- `missingKey`: Behavior when a map key is missing: `default`, `zero`, or `error`
- `leftDelim`, `rightDelim`: Custom delimiters (default: `{{` and `}}`)
- `data`: Additional data to merge with resolver context

---

### hcl

Processes HCL (HashiCorp Configuration Language) content. Supports four operations: `parse` (default) extracts structured block information; `format` canonically formats; `validate` checks syntax; `generate` produces HCL from structured input. Accepts single files, multiple paths, or a directory of `.tf` files.

~~~yaml
resolve:
  with:
    - provider: hcl
      inputs:
        content: |
          variable "region" {
            type    = string
            default = "us-east-1"
          }
~~~

**Capabilities:** `from`, `transform`

**Inputs:**
- `operation` (optional): `parse` (default), `format`, `validate`, or `generate`
- `content` (optional): Raw HCL content to process as a string
- `path` (optional): Path to a single HCL file
- `paths` (optional): Array of HCL file paths — results are merged (parse) or returned per-file (format/validate)
- `dir` (optional): Directory path — all `.tf`/`.tf.json` files are processed
- `blocks` (optional): Structured block data for `generate` (same schema as parse output)
- `output_format` (optional): Generation output format — `hcl` (default, native HCL syntax) or `json` (Terraform JSON syntax `.tf.json`)

For `parse`/`format`/`validate`, provide exactly one of `content`, `path`, `paths`, or `dir` (mutually exclusive). For `generate`, use `blocks` and optionally `output_format`.

**Output (operation: parse):** An object with arrays/maps for each block type:
- `variables`: Array of variable definitions (name, type, default, description, sensitive, nullable, validation)
- `resources`: Array of resource blocks (type, name, attributes, sub-blocks)
- `data`: Array of data source blocks (type, name, attributes, sub-blocks)
- `modules`: Array of module blocks (name, source, version, attributes)
- `outputs`: Array of output blocks (name, value, description, sensitive)
- `locals`: Map of local values (merged across multiple `locals` blocks)
- `providers`: Array of provider configurations (name, alias, region, attributes)
- `terraform`: Object with required_version, required_providers, backend, cloud
- `moved`: Array of moved blocks (from, to)
- `import`: Array of import blocks (to, id, provider)
- `check`: Array of check blocks (name, data, assertions)

When multiple files are parsed (`paths`/`dir`), results are merged: arrays are concatenated, `locals` and `terraform` maps are merged (last-file-wins for conflicts).

**Output (operation: format):**
- `formatted`: The canonically formatted HCL content as a string
- `changed`: Boolean indicating whether the formatter modified the content

Multi-file format returns `{ files: [{filename, formatted, changed}, ...], changed: bool }`.

**Output (operation: validate):**
- `valid`: Boolean — `true` if no syntax errors
- `error_count`: Number of error-level diagnostics
- `diagnostics`: Array of diagnostic entries (severity, summary, detail, range)

Multi-file validate returns `{ valid: bool, error_count: int, files: [...] }`.

**Output (operation: generate):**
- `hcl`: Generated HCL text string (native HCL syntax or Terraform JSON depending on `output_format`)
- Metadata includes `output_format` (`hcl` or `json`) indicating which format was produced

**Expression handling:** Literal values (strings, numbers, booleans, lists, maps) are evaluated to native types. Complex expressions (variable references, function calls, conditionals) are returned as raw source text strings.

---

### identity

Provides authentication identity information from auth handlers without exposing tokens or secrets.

~~~yaml
resolve:
  with:
    - provider: identity
      inputs:
        operation: claims
~~~

**Operations:**
- `status` - Get authentication status (authenticated, identity type, expiry info)
- `claims` - Get identity claims (name, email, subject, issuer, etc.)
- `list` - List all available auth handlers with their status

**Inputs:**
- `operation` (required): One of `status`, `claims`, or `list`
- `handler`: Name of the auth handler to query (e.g., `entra`). If not specified, uses the first authenticated handler.

**Output (claims operation):**
~~~yaml
authenticated: true
handler: entra
identityType: user
claims:
  email: user@example.com
  name: John Doe
  subject: abc123
  tenantId: 12345-...
  displayIdentity: user@example.com
~~~

**Output (status operation):**
~~~yaml
authenticated: true
handler: entra
identityType: user
tenantId: 12345-...
expiresAt: "2024-01-15T10:30:00Z"
expiresIn: "55m30s"
~~~

**Security:** This provider never exposes access tokens, refresh tokens, or any sensitive credentials. It only provides identity metadata suitable for logging, auditing, and conditional logic.

---

## Security Considerations

> **Status**: ✅ Implemented

Providers handle sensitive data through structured security mechanisms:

### Secret Handling

**`SensitiveFields` on Descriptor + `WriteOnly` on Schema:**

Secret handling uses two complementary mechanisms:

1. **`Descriptor.SensitiveFields []string`** — Lists property names that contain sensitive data. Used at runtime for redaction in logs, errors, and snapshot output.
2. **`WriteOnly: true` on schema properties** — Standard JSON Schema annotation that signals a property is write-only (e.g., passwords, tokens). Set via `schemahelper.WithWriteOnly()`.

Properties listed in `SensitiveFields` receive special handling:

- **Logging Redaction**: Secret values are redacted in logs, displaying `***REDACTED***` instead of actual values
- **Render Mode**: When solutions are rendered (dry-run or plan mode), secret fields show `<secret>` placeholders
- **Audit Trails**: Secret access is logged (without values) for security auditing
- **Memory Handling**: scafctl makes best-effort attempts to zero sensitive memory after use

**Example:**

~~~go
Descriptor{
  // ... other fields ...
  SensitiveFields: []string{"password", "apiKey"},
  Schema: schemahelper.ObjectSchema(
    []string{"username", "password"},
    map[string]*jsonschema.Schema{
      "username": schemahelper.StringProp("Account username"),
      "password": schemahelper.StringProp("Account password", schemahelper.WithWriteOnly()),
      "apiKey":   schemahelper.StringProp("Optional API key", schemahelper.WithWriteOnly()),
    },
  ),
}
~~~

### Provider Responsibilities

Providers that handle sensitive data must:

1. **Declare Sensitive Fields**: List all sensitive property names in `Descriptor.SensitiveFields`
2. **Mark Schema Properties**: Set `WriteOnly: true` on sensitive schema properties (use `schemahelper.WithWriteOnly()`)
3. **Avoid Logging Secrets**: Never log secret values, even at debug verbosity levels
4. **Secure Transmission**: Use TLS/HTTPS for transmitting secrets over networks
5. **Memory Management**: Clear sensitive data from memory when no longer needed
6. **Error Messages**: Ensure error messages don't leak secret values

### Built-in Protections

scafctl provides automatic protections:

- Secret values are excluded from context dumps and debug output
- Provider descriptors with `SensitiveFields` trigger additional validation at registration
- The execution framework redacts secrets in trace logs
- Mock outputs for secret fields use placeholder values

### Authentication Providers

Providers with `CapabilityAuthentication` have additional requirements:

- All credential inputs should be listed in `SensitiveFields` and marked `WriteOnly: true` on the schema
- Token refresh operations must not log credential values
- Failed authentication must not expose credential details in error messages
- Mock authentication must return realistic-looking but non-functional credentials

### Guidelines

- **Principle of Least Exposure**: Only mark truly sensitive fields as secrets (not every string)
- **Input Validation**: Validate secret format without logging the value
- **Output Security**: Authentication tokens and API keys in outputs should also be treated as secrets
- **Documentation**: Document which inputs/outputs contain sensitive data in provider examples

---

## Context Propagation

> **Status**: ✅ Implemented in `pkg/provider/context.go`

Providers receive and should respect standard Go context patterns:

### Execution Control Context

**Required Context Values** (read-only for providers):

- **Execution Mode** (`executionModeKey`): The provider capability being invoked (from, transform, validation, authentication, action)
  - Access via: `ExecutionModeFromContext(ctx)`
  - Providers must validate this matches their declared capabilities
  - Used to determine behavior (e.g., read-only vs mutation)

- **Dry-Run Flag** (`dryRunKey`): Boolean indicating mock execution
  - Access via: `DryRunFromContext(ctx)`
  - Set by `run provider --dry-run`; providers should avoid side effects and return mock data
  - Not set during `run solution --dry-run` — solution-level dry-run uses the `WhatIf` function on the `Descriptor` instead (providers are never executed)

### Standard Context Patterns

**Cancellation and Timeouts:**

Providers should respect context cancellation:

~~~go
func (p *HTTPProvider) Execute(ctx context.Context, input any) (*Output, error) {
  req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
  if err != nil {
    return nil, err
  }
  
  resp, err := p.client.Do(req)
  if err != nil {
    // Context cancellation will be reflected here
    return nil, fmt.Errorf("request failed: %w", err)
  }
  // ...
}
~~~

**Logging Context:**

Providers should use the logger from context when available:

~~~go
func (p *ShellProvider) Execute(ctx context.Context, input any) (*Output, error) {
  lgr := logger.FromContext(ctx)
  lgr.V(1).Info("executing shell command", "cmd", cmd)
  // ...
}
~~~

**Tracing Context:**

When tracing is enabled, providers should propagate context through external calls:

- HTTP clients should use `http.NewRequestWithContext(ctx, ...)`
- Database calls should accept context: `db.QueryContext(ctx, ...)`
- Long-running operations should check `ctx.Done()` periodically

### Context Best Practices

1. **Always Accept Context**: The first parameter to `Execute()` is `context.Context`
2. **Check Cancellation**: For long operations, periodically check `ctx.Done()`
3. **Propagate Context**: Pass context to all downstream operations (HTTP, DB, subprocesses)
4. **Don't Store Context**: Never store context in struct fields; always pass as parameter
5. **Respect Timeouts**: Operations should abort when context deadline expires

### Example: Context-Aware Provider

~~~go
func (p *APIProvider) Execute(ctx context.Context, input any) (*Output, error) {
  // Extract execution control
  execMode, ok := ExecutionModeFromContext(ctx)
  if !ok {
    return nil, fmt.Errorf("execution mode not in context")
  }
  
  isDryRun := DryRunFromContext(ctx)
  
  // Extract timeout
  deadline, hasDeadline := ctx.Deadline()
  if hasDeadline {
    timeout := time.Until(deadline)
    // Adjust operation based on available time
  }
  
  // Extract logger
  lgr := logger.FromContext(ctx)
  lgr.V(1).Info("executing provider", "mode", execMode, "dryRun", isDryRun)
  
  // Access resolver context if needed
  // For example, in a CEL provider that needs to evaluate expressions:
  data := ResolverContextFromContext(ctx)
  
  if execMode == CapabilityTransform || execMode == CapabilityValidation {
    selfValue := data["__self"] // Current value in transform/validate
    // Use selfValue in transformation or validation logic
  }
  
  // Access other resolver values
  if environment, ok := data["environment"].(string); ok {
    lgr.V(1).Info("using environment", "env", environment)
  }
  
  // Execute based on mode and dry-run flag
  // DryRunFromContext is set by `run provider --dry-run`
  if isDryRun {
    return p.mockExecute(execMode, input)
  }
  
  return p.realExecute(ctx, execMode, input)
}
~~~

---

## Trace Instrumentation

> **Status**: ✅ Implemented in `pkg/provider/executor.go`

Every provider execution is wrapped in an OpenTelemetry span, giving end-to-end visibility across resolver phases and provider calls.

### Span Name and Tracer

| Property | Value |
|---|---|
| Tracer name | `github.com/oakwood-commons/scafctl/provider` |
| Span name | `provider.Execute` |
| Span kind | `SpanKindInternal` |

### Span Attributes

| Attribute | Type | Description |
|---|---|---|
| `provider.name` | string | Registered name of the provider (e.g. `static`, `cel`, `http`) |

### Error Recording

If the provider returns a non-nil error the span is marked with `codes.Error` and the error message is recorded before the span ends:

~~~go
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
}
~~~

### Context Propagation

The span context is stored inside the `context.Context` that is threaded through every provider call. Child operations (e.g. HTTP provider outbound requests via `otelhttp.NewTransport`) automatically become child spans of the provider span, giving a complete trace hierarchy:

```
resolver.Execute
  └─ resolver.executeResolver
        └─ provider.Execute          ← this span
              └─ HTTP GET /api/...   ← child from otelhttp transport
```

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
