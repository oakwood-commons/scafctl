# Providers

## Purpose

Providers are stateless execution primitives. They perform a single, well-defined operation given validated inputs and return either a result or an error.

Providers do not own orchestration, control flow, dependency resolution, or lifecycle decisions. This separation keeps solutions deterministic, testable, and explicit.

Providers are used by:

- Resolvers (during resolve, transform, and validate phases)
- Actions (during execution or render)

Providers are never invoked implicitly. A provider runs only when explicitly referenced.

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

**Key principles:**

- Resolver context contains only resolver outputs (nothing exists unless emitted by a resolver)
- Providers do not evaluate expressions or templates themselves—all inputs are fully resolved before invocation
- The resolver context map is read-only; providers should not mutate it
- Access to the resolver context is optional—providers that don't need it don't have to retrieve it

---

## Execution Lifecycle

Providers follow a strict execution pipeline to ensure consistent behavior and validation:

```
Provider Invocation Request
        |
        v
[1. Schema Validation]
   (validate inputs against Descriptor.Schema)
        |
        v
[2. Decode] (optional)
   (convert map[string]any to strongly-typed struct)
        |
        v
[3. Execute]
   (provider-specific logic based on execution mode and dry-run flag)
        |
        v
[4. Output Schema Validation]
   (validate Output.Data against Descriptor.OutputSchemas[capability])
        |
        v
[5. Return Output]
   (Data, Warnings, Metadata)
```

**Lifecycle Phases:**

1. **Schema Validation** - scafctl validates all input values against the provider's declared `Schema` before invocation. Invalid inputs result in an error before `Execute()` is called.

2. **Decode** - If the provider defines a `Decode` function in its descriptor, scafctl calls it to convert the validated `map[string]any` into a strongly-typed structure. This step is optional; providers can work directly with maps.

3. **Execute** - The provider's `Execute(ctx, input)` method runs with the validated (and optionally decoded) inputs. The provider performs its operation based on the execution mode and dry-run flag from the context. Providers that need access to resolver values retrieve them via `ResolverContextFromContext(ctx)`.

4. **Output Schema Validation** - scafctl validates the `Output.Data` field against the provider's `OutputSchemas` for the current capability. Each capability can define different required output fields. This ensures both real and mock outputs conform to the declared structure for the specific execution context.

5. **Return** - The validated `Output` (containing `Data`, optional `Warnings`, and optional `Metadata`) is returned to the caller (resolver or action orchestrator).

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

Provider execution is instrumented for observability at two levels:

### In-Memory Metrics (CLI Output)

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

Provider metrics are also exported as Prometheus metrics for integration with monitoring systems:

**`scafctl_provider_execution_duration_seconds`** (Histogram)
- Labels: `provider_name`, `status` (success/failure)
- Tracks execution duration distribution per provider

**`scafctl_provider_execution_total`** (Counter)
- Labels: `provider_name`, `status` (success/failure)
- Tracks total invocation count per provider

These metrics are recorded automatically when Prometheus metrics are enabled via `metrics.RegisterMetrics()`.

### Logging

All providers implement structured logging via logr:
- Execution start/completion logged at V(1) verbosity
- Error details logged at V(0) verbosity
- Provider name included in all log messages

**Usage:**
```bash
scafctl run solution -f solution.yaml --log-level 1
```

---

## Provider Model

### Notes

- All Providers must support mocking to enable dry-run execution for testing, planning, and verification without performing real side effects.

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
- `dryRunKey` (unexported) - Boolean indicating whether this is a dry-run execution
  - Access via: `DryRunFromContext(ctx)`
- `resolverContextKey` (unexported) - The resolver context map containing all emitted resolver values
  - Access via: `ResolverContextFromContext(ctx)` returns `map[string]any`

Typed keys ensure external packages cannot accidentally use the same context keys, preventing subtle bugs.

**Execution Mode (Capability):**

- scafctl passes the provider capability via the execution mode context value to indicate how the provider is being invoked
- The capability must match one of the provider's declared capabilities
- Providers can use this to adjust behavior based on context (e.g., read-only vs mutation)
- This enables providers to support multiple capabilities with context-aware behavior

**Dry-Run Mode:**

- Dry-run mode is signaled for testing and planning without side effects
- When dry-run is enabled, providers return mock/sample output instead of performing real operations
- All providers must support dry-run mode

**Implementation Pattern:**

~~~go
// Provider implementation checks context for execution mode and dry-run
// Note: This example uses helper methods that represent implementation-specific logic.
// Real providers would implement these based on their specific needs:
//   - mockExecute: Returns mock Output for dry-run based on execution mode
//   - executeGET: Performs read-only operations (e.g., HTTP GET), returns Output with fetched data
//   - executeTransform: Transforms __self value, returns Output with transformed result
//   - executeValidation: Validates input/state, returns Output with boolean in Data field
//   - executeAuth: Handles authentication (token generation, validation), returns Output with auth data
//   - executeMutation: Performs side-effect operations (e.g., HTTP POST/PUT/DELETE), returns Output with result
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
  
  // Check if dry-run mode is enabled using typed accessor
  isDryRun := DryRunFromContext(ctx)
  
  if isDryRun {
    // In dry-run mode, providers must avoid side effects and return a deterministic
    // Output that represents what *would* happen. The mockExecute helper
    // typically uses the provider's MockBehavior configuration to construct an
    // appropriate mock response for the given execution mode.
    return p.mockExecute(execMode, input)
  }
  
  // Adjust behavior based on execution mode
  switch execMode {
  case CapabilityFrom:
    // Read-only operation for resolver context (fetch/read data)
    return p.executeGET(input)
  case CapabilityTransform:
    // Transform operation receives input with __self and returns transformed result
    return p.executeTransform(input)
  case CapabilityValidation:
    // Return boolean validation result in Output.Data
    return p.executeValidation(input)
  case CapabilityAuthentication:
    // Handle authentication flows (token generation, credential validation, etc.)
    return p.executeAuth(input)
  case CapabilityAction:
    // Allow mutations for action context (write/update/delete operations)
    return p.executeMutation(input)
  default:
    return Output{}, fmt.Errorf("unsupported execution mode: %s", execMode)
  }
}
~~~

**Descriptor Declaration:**

- `MockBehavior` - Documents what the provider returns during dry-run mode (required for all providers since dry-run support is mandatory)

**Requirements:**

- Providers must validate the execution mode matches one of their declared capabilities
- Execution mode determines provider behavior (e.g., read-only vs mutation, data vs boolean return)
- Mock output (in `Output.Data`) must conform to the same schema as real output (validated by `OutputSchemas` for the current capability)
- Mock execution must be deterministic and predictable
- Providers that cannot meaningfully mock (e.g., read-only queries) should return representative sample data
- Side-effect providers must not perform any operations in dry-run mode
- Warnings and metadata are optional but encouraged for providing execution context

---

## Provider Capabilities

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
  executionModeKey   contextKey = "scafctl.provider.executionMode"   // Key for Capability execution mode
  dryRunKey          contextKey = "scafctl.provider.dryRun"          // Key for boolean dry-run flag
  resolverContextKey contextKey = "scafctl.provider.resolverContext" // Key for resolver context map
  parametersKey      contextKey = "scafctl.provider.parameters"      // Key for CLI parameters map
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
  
  // Schema definitions
  Schema        SchemaDefinition                    `json:"schema" yaml:"schema" doc:"Input schema" required:"true"`
  OutputSchemas map[Capability]SchemaDefinition     `json:"outputSchemas" yaml:"outputSchemas" doc:"Output schemas per capability" required:"true"`
  // Decode converts validated map[string]any inputs into strongly-typed structs for internal use.
  // Called after schema validation but before Execute(). Optional - providers can work with map[string]any directly.
  // When Decode is set, the Executor calls it and passes the result directly to Execute().
  Decode        func(map[string]any) (any, error) `json:"-" yaml:"-"`
  
  // ExtractDependencies extracts resolver references from provider-specific input formats.
  // Called during dependency graph construction to determine resolver execution order.
  // Optional - if nil, the generic extraction logic is used which handles:
  // - ValueRef.Resolver references
  // - CEL expressions (_.resolverName patterns)
  // - Go templates ({{._.resolverName}} patterns with default delimiters)
  // Providers should implement this when they have custom input formats that may
  // contain resolver references, such as templates with custom delimiters.
  // The function receives the raw inputs map and returns resolver names that this input depends on.
  ExtractDependencies func(inputs map[string]any) []string `json:"-" yaml:"-"`
  
  // Execution behavior
  MockBehavior string       `json:"mockBehavior" yaml:"mockBehavior" doc:"Dry-run behavior description" minLength:"10" maxLength:"500" required:"true"`
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

// SchemaDefinition defines the structure of inputs or outputs for a provider.
// Used for both input schema (Descriptor.Schema) and output schemas (Descriptor.OutputSchemas).
// Note: When used in OutputSchemas, the Required field in PropertyDefinition is typically not meaningful
// since outputs are generated by the provider rather than validated as required inputs.
type SchemaDefinition struct {
  Properties map[string]PropertyDefinition `json:"properties,omitempty" yaml:"properties,omitempty" doc:"Map of property names to their definitions"`
}

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

// PropertyType represents the data type of a provider property (input or output).
// This type provides compile-time type safety while still serializing as strings in YAML/JSON.
type PropertyType string

const (
  PropertyTypeString PropertyType = "string" // String values
  PropertyTypeInt    PropertyType = "int"    // Integer values
  PropertyTypeFloat  PropertyType = "float"  // Floating-point values
  PropertyTypeBool   PropertyType = "bool"   // Boolean values
  PropertyTypeArray  PropertyType = "array"  // Array/slice values
  PropertyTypeAny    PropertyType = "any"    // Any type (use sparingly for maps and mixed types)
)

// IsValid checks if the property type is a known, recognized type.
// Returns false for unknown types to ensure only declared property types are used.
func (t PropertyType) IsValid() bool {
  switch t {
  case PropertyTypeString, PropertyTypeInt, PropertyTypeFloat, PropertyTypeBool, PropertyTypeArray, PropertyTypeAny:
    return true
  default:
    return false
  }
}

// Type Usage Notes:
//
// - PropertyTypeAny should be used for:
//   - Map/object types (map[string]any)
//   - Mixed-type fields that can accept multiple types
//   - Complex nested structures
// - There is no explicit "map" type; use PropertyTypeAny for map properties
// - Use PropertyTypeArray for slices/arrays; the element type is inferred at runtime

type PropertyDefinition struct {
  Type        PropertyType `json:"type" yaml:"type" doc:"Property data type (string, int, float, bool, array, any)" example:"string" required:"true"`
  Required    bool   `json:"required,omitempty" yaml:"required,omitempty" doc:"Whether property is required" example:"true"`
  Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of the property" minLength:"5" maxLength:"500" example:"The name of the resource to create"`
  Default     any    `json:"default,omitempty" yaml:"default,omitempty" doc:"Default value for optional properties" example:"default-value"`
  Example     any    `json:"example,omitempty" yaml:"example,omitempty" doc:"Example value for documentation" example:"my-resource"`
  
  // Validation constraints for strings
  // Pointers are used to distinguish between "not set" and "set to zero"
  MinLength *int      `json:"minLength,omitempty" yaml:"minLength,omitempty" doc:"Minimum string length constraint (applies to string type)"`
  MaxLength *int      `json:"maxLength,omitempty" yaml:"maxLength,omitempty" doc:"Maximum string length constraint (applies to string type)"`
  Pattern   string   `json:"pattern,omitempty" yaml:"pattern,omitempty" doc:"Regex pattern for validation (applies to string type)" example:"^[a-z0-9-]+$"`
  
  // Validation constraints for numbers (int, float)
  // Pointers are used to distinguish between "not set" and "set to zero"
  Minimum *float64 `json:"minimum,omitempty" yaml:"minimum,omitempty" doc:"Minimum numeric value constraint (applies to int and float types)"`
  Maximum *float64 `json:"maximum,omitempty" yaml:"maximum,omitempty" doc:"Maximum numeric value constraint (applies to int and float types)"`
  
  // Validation constraints for arrays
  // Pointers are used to distinguish between "not set" and "set to zero"
  MinItems *int `json:"minItems,omitempty" yaml:"minItems,omitempty" doc:"Minimum array length constraint (applies to array type)"`
  MaxItems *int `json:"maxItems,omitempty" yaml:"maxItems,omitempty" doc:"Maximum array length constraint (applies to array type)"`
  
  // General validation
  Enum      []any    `json:"enum,omitempty" yaml:"enum,omitempty" doc:"Allowed values enumeration (applies to any type)"`
  Format    string   `json:"format,omitempty" yaml:"format,omitempty" doc:"Type hint for specialized validation (uri, email, date, uuid, etc.)" example:"uri"`
  
  Deprecated bool `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Whether property is deprecated" example:"false"`
  IsSecret   bool `json:"isSecret,omitempty" yaml:"isSecret,omitempty" doc:"Whether property contains sensitive data (for render-mode redaction and security handling)" example:"false"`
}

// Validation Behavior:
// - Constraints that don't match the property type are silently ignored during validation
//   (e.g., MinLength on an int property, Minimum on a string property)
// - This allows flexible schema definitions without strict constraint-type coupling
// - Only constraints matching the property type are enforced

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

### MockBehavior Field Guidance

The `MockBehavior` field in `Descriptor` documents what the provider returns during dry-run mode. Mock implementations must be deterministic, predictable, and schema-compliant.

**Examples by capability:**

**CapabilityFrom** (data fetching):
- `"Returns sample user object with id='mock-user-123', name='Mock User', email='mock@example.com'"`
- `"Returns empty array [] when no mock data is configured"`
- `"Returns last known cached value if available, otherwise returns placeholder data"`

**CapabilityTransform** (data transformation):
- `"Applies transformation logic to __self using the same code path as real execution"`
- `"Returns __self unchanged to simulate identity transformation"`
- `"Returns deterministic output based on input pattern (e.g., uppercased __self)"`

**CapabilityValidation** (validation logic):
- `"Returns true (valid) for all inputs in mock mode"`
- `"Returns validation result based on input patterns without external checks"`
- `"Performs local validation logic but skips remote API verification"`

**CapabilityAuthentication** (authentication flows):
- `"Returns mock JWT token 'mock.jwt.token' with standard claims"`
- `"Returns success response without contacting authentication service"`
- `"Returns cached credentials if available, otherwise returns placeholder token"`

**CapabilityAction** (side-effect operations):
- `"Returns success status without executing shell command"`
- `"Returns simulated API response without making HTTP request"`
- `"Logs intended operation and returns mock success result"`

**Best Practices:**
- Mock output must match `OutputSchemas` for the current capability exactly (same types and structure)
- Use consistent, recognizable mock values (e.g., 'mock-' prefix for IDs)
- Document what happens with different input variations
- For transformations, prefer real logic over stubs when side-effect-free
- For validations, document whether mocks return true, false, or conditional results

---

## Input Resolution

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
      cmd:
        - go
        - build
        - ./...
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

Reads data from the local filesystem.

> **Note:** Previously documented as `filesystem`.

~~~yaml
resolve:
  with:
    - provider: file
      inputs:
        operation: read
        path: ./config/name.txt
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

~~~yaml
resolve:
  with:
    - provider: go-template
      inputs:
        name: greeting-template
        template: "Hello, {{.name}}!"
~~~

**Inputs:**
- `name` (required): Name for the template, used in error messages and logging
- `template` (required): Go template content to render
- `missingKey`: Behavior when a map key is missing: `default`, `zero`, or `error`
- `leftDelim`, `rightDelim`: Custom delimiters (default: `{{` and `}}`)
- `data`: Additional data to merge with resolver context

---

## Security Considerations

Providers handle sensitive data through structured security mechanisms:

### Secret Handling

**`IsSecret` Flag:**

Provider input properties marked with `IsSecret: true` receive special handling:

- **Logging Redaction**: Secret values are redacted in logs, displaying `***REDACTED***` instead of actual values
- **Render Mode**: When solutions are rendered (dry-run or plan mode), secret fields show `<secret>` placeholders
- **Audit Trails**: Secret access is logged (without values) for security auditing
- **Memory Handling**: scafctl makes best-effort attempts to zero sensitive memory after use

**Example:**

~~~go
type PropertyDefinition struct {
  // ...
  IsSecret bool `json:"isSecret,omitempty" doc:"Whether property contains sensitive data"`
}
~~~

### Provider Responsibilities

Providers that handle sensitive data must:

1. **Declare Secret Inputs**: Mark all sensitive input properties with `IsSecret: true`
2. **Avoid Logging Secrets**: Never log secret values, even at debug verbosity levels
3. **Secure Transmission**: Use TLS/HTTPS for transmitting secrets over networks
4. **Memory Management**: Clear sensitive data from memory when no longer needed
5. **Error Messages**: Ensure error messages don't leak secret values

### Built-in Protections

scafctl provides automatic protections:

- Secret values are excluded from context dumps and debug output
- Provider descriptors with `IsSecret` inputs trigger additional validation
- The execution framework redacts secrets in trace logs
- Mock outputs for secret fields use placeholder values

### Authentication Providers

Providers with `CapabilityAuthentication` have additional requirements:

- All credential inputs should be marked `IsSecret: true`
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

Providers receive and should respect standard Go context patterns:

### Execution Control Context

**Required Context Values** (read-only for providers):

- **Execution Mode** (`executionModeKey`): The provider capability being invoked (from, transform, validation, authentication, action)
  - Access via: `ExecutionModeFromContext(ctx)`
  - Providers must validate this matches their declared capabilities
  - Used to determine behavior (e.g., read-only vs mutation)

- **Dry-Run Flag** (`dryRunKey`): Boolean indicating mock execution
  - Access via: `DryRunFromContext(ctx)`
  - When true, providers must avoid side effects and return mock data
  - All providers must support dry-run mode

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
  if isDryRun {
    return p.mockExecute(execMode, input)
  }
  
  return p.realExecute(ctx, execMode, input)
}
~~~
  
  isDryRun := DryRunFromContext(ctx)
  if isDryRun {
    return p.mockExecute(execMode, input)
  }
  
  // Use logger from context
  lgr := logger.FromContext(ctx)
  lgr.V(1).Info("executing API call", "endpoint", p.endpoint)
  
  // Propagate context to HTTP request for cancellation/tracing
  req, err := http.NewRequestWithContext(ctx, "GET", p.endpoint, nil)
  if err != nil {
    return nil, err
  }
  
  // Check for cancellation before expensive operation
  select {
  case <-ctx.Done():
    return nil, ctx.Err()
  default:
  }
  
  resp, err := p.client.Do(req)
  // ...
}
~~~

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
