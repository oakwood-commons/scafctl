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

For resolver execution:

- `_` contains only resolver outputs
- Nothing exists in `_` unless emitted by a resolver

For action execution:

- Providers may receive additional action-local data, but this does not affect resolver semantics

Special symbols passed by the caller:

- `__self` will be the current value of the resolver
- `__item` will be the current item in the foreach loop

Providers do not evaluate expressions or templates themselves. All inputs are fully resolved before invocation.

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

Providers receive execution control information through the context parameter, including which capability mode they're being invoked in and whether dry-run is enabled.

**Context Keys:**

scafctl uses typed context keys to prevent collisions and provide type safety:

- `executionModeKey` (unexported) - The capability being invoked (from, transform, validation, action, authentication) as `ProviderCapability`
  - Access via: `ExecutionModeFromContext(ctx)`
- `dryRunKey` (unexported) - Boolean indicating whether this is a dry-run execution
  - Access via: `DryRunFromContext(ctx)`

Typed keys ensure external packages cannot accidentally use the same context keys, preventing subtle bugs.

**Execution Mode:**

- scafctl passes the execution mode to indicate which capability is being invoked
- The execution mode must match one of the provider's declared capabilities
- Providers can use this to adjust behavior based on context (e.g., read-only vs mutation)
- This enables providers to support multiple capabilities with context-aware behavior

**Dry-Run Mode:**

- Dry-run mode is signaled for testing and planning without side effects
- When dry-run is enabled, providers return mock/sample output instead of performing real operations
- All providers must support dry-run mode

**Implementation Pattern:**

~~~go
// Provider implementation checks context for execution mode and dry-run
func (p *APIProvider) Execute(ctx context.Context, input any) (ProviderOutput, error) {
  // Extract execution mode using typed accessor
  execMode, ok := ExecutionModeFromContext(ctx)
  if !ok {
    return ProviderOutput{}, fmt.Errorf("execution mode not provided in context")
  }
  
  // Validate execution mode matches declared capabilities
  if !p.supportsCapability(execMode) {
    return ProviderOutput{}, fmt.Errorf("provider does not support capability: %s", execMode)
  }
  
  // Check if dry-run mode is enabled using typed accessor
  isDryRun := DryRunFromContext(ctx)
  
  if isDryRun {
    // Return mock output based on execution mode
    return p.mockExecute(execMode, input)
  }
  
  // Adjust behavior based on execution mode
  switch execMode {
  case CapabilityFrom:
    // Read-only operation for resolver context
    return p.executeGET(input)
  case CapabilityAction:
    // Allow mutations for action context
    return p.executeMutation(input)
  case CapabilityValidation:
    // Return boolean validation result
    return p.executeValidation(input)
  default:
    return ProviderOutput{}, fmt.Errorf("unsupported execution mode: %s", execMode)
  }
}
~~~

**ProviderDescriptor Declaration:**

- `MockBehavior` - Documents what the provider returns during dry-run mode (required for all providers since dry-run support is mandatory)

**Requirements:**

- Providers must validate the execution mode matches one of their declared capabilities
- Execution mode determines provider behavior (e.g., read-only vs mutation, data vs boolean return)
- Mock output (in `ProviderOutput.Data`) must conform to the same schema as real output (validated by `OutputSchema`)
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
- Must return a boolean indicating validation success (true) or failure (false)
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
  Descriptor() ProviderDescriptor
  Execute(ctx context.Context, input any) (ProviderOutput, error)
}

// ProviderOutput is the standardized return structure for all provider executions.
// It wraps the actual data along with optional warnings and metadata.
type ProviderOutput struct {
  Data     any            `json:"data" doc:"The actual output data from provider execution (validated against OutputSchema)"`
  Warnings []string       `json:"warnings,omitempty" doc:"Non-fatal warnings generated during execution" maxItems:"50"`
  Metadata map[string]any `json:"metadata,omitempty" doc:"Optional execution metadata (timing, resource usage, etc.)"`
}

// ProviderCapability represents an execution context a provider can participate in.
// This type provides compile-time type safety while still serializing as strings in YAML/JSON.
type ProviderCapability string

const (
  CapabilityFrom           ProviderCapability = "from"           // Provider can be used in resolver 'from' section
  CapabilityTransform      ProviderCapability = "transform"      // Provider can be used in resolver 'transform' section
  CapabilityValidation     ProviderCapability = "validation"     // Provider can be used in resolver 'validate' section
  CapabilityAuthentication ProviderCapability = "authentication" // Provider handles authentication
  CapabilityAction         ProviderCapability = "action"         // Provider can be invoked as an action
)

// IsValid checks if the capability is a known, recognized capability.
// This allows forward compatibility with unknown capabilities while validating known ones.
func (c ProviderCapability) IsValid() bool {
  switch c {
  case CapabilityFrom, CapabilityTransform, CapabilityValidation, CapabilityAuthentication, CapabilityAction:
    return true
  default:
    return false
  }
}

// contextKey is an unexported type for context keys to prevent collisions.
// Using a custom type ensures external packages cannot accidentally use the same key.
type contextKey int

const (
  executionModeKey contextKey = iota // Key for ProviderCapability execution mode
  dryRunKey                           // Key for boolean dry-run flag
)

// Context value accessors for provider implementations
func ExecutionModeFromContext(ctx context.Context) (ProviderCapability, bool) {
  mode, ok := ctx.Value(executionModeKey).(ProviderCapability)
  return mode, ok
}

func DryRunFromContext(ctx context.Context) bool {
  dryRun, _ := ctx.Value(dryRunKey).(bool)
  return dryRun
}

type ProviderDescriptor struct {
  // Identity and versioning
  Name        string          `json:"name" yaml:"name" doc:"The unique name of the provider" minLength:"2" maxLength:"100" example:"shell" pattern:"^[a-z][a-z0-9-]*[a-z0-9]$" required:"true"`
  DisplayName string          `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable name for UI/catalog" minLength:"3" maxLength:"100" example:"Shell Command Executor"`
  APIVersion  string          `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty" doc:"Provider API version for compatibility" example:"v1" pattern:"^v[0-9]+$"`
  // Version uses github.com/Masterminds/semver/v3 for semantic versioning.
  Version     *semver.Version `json:"version,omitempty" yaml:"version,omitempty" doc:"Semantic version of the provider" example:"1.2.3"`
  Description string          `json:"description,omitempty" yaml:"description,omitempty" doc:"Brief description of provider purpose" minLength:"10" maxLength:"500" example:"Executes shell commands in the local environment"`
  
  // Schema definitions
  Schema       SchemaDefinition `json:"schema" yaml:"schema" doc:"Input parameter schema definition" required:"true"`
  OutputSchema SchemaDefinition `json:"outputSchema,omitempty" yaml:"outputSchema,omitempty" doc:"Expected output structure schema for type-safe validation"`
  Decode       func(map[string]any) (any, error) `json:"-" yaml:"-"`
  
  // Execution behavior
  MockBehavior string                `json:"mockBehavior,omitempty" yaml:"mockBehavior,omitempty" doc:"Description of mock execution behavior for dry-run mode (all providers must support dry-run)" minLength:"10" maxLength:"500" example:"Returns sample output without executing command"`
  Capabilities []ProviderCapability `json:"capabilities" yaml:"capabilities" doc:"Execution contexts this provider supports (from, transform, validation, authentication, action)" minItems:"1" maxItems:"10" required:"true"`
  
  // Catalog and distribution metadata
  Category   string            `json:"category,omitempty" yaml:"category,omitempty" doc:"Provider category for classification" minLength:"3" maxLength:"50" example:"infrastructure"`
  Tags       []string          `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable keywords for discovery" minItems:"0" maxItems:"20"`
  Icon       string            `json:"icon,omitempty" yaml:"icon,omitempty" doc:"URL or path to provider icon for catalog display" maxLength:"500" format:"uri" example:"https://example.com/icon.png"`
  Links      []Link            `json:"links,omitempty" yaml:"links,omitempty" doc:"External documentation and reference links" maxItems:"10"`
  Examples   []ProviderExample `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Usage examples demonstrating provider invocation patterns" minItems:"1" maxItems:"5"`
  Deprecated bool              `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Whether provider is deprecated" example:"false"`
  Beta       bool              `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Whether provider is in beta/preview status" example:"false"`
  
  // Maintainer information
  Maintainers []Contact `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"People or teams responsible for the provider" minItems:"1" maxItems:"10"`
}

type SchemaDefinition struct {
  Parameters map[string]ParameterDefinition `json:"parameters,omitempty" yaml:"parameters,omitempty" doc:"Map of parameter names to their definitions"`
}

// ParameterType represents the data type of a provider parameter.
// This type provides compile-time type safety while still serializing as strings in YAML/JSON.
type ParameterType string

const (
  ParameterTypeString ParameterType = "string" // String values
  ParameterTypeInt    ParameterType = "int"    // Integer values
  ParameterTypeFloat  ParameterType = "float"  // Floating-point values
  ParameterTypeBool   ParameterType = "bool"   // Boolean values
  ParameterTypeMap    ParameterType = "map"    // Map/object values
  ParameterTypeArray  ParameterType = "array"  // Array/slice values
  ParameterTypeAny    ParameterType = "any"    // Any type (use sparingly)
)

// IsValid checks if the parameter type is a known, recognized type.
// This allows forward compatibility with unknown types while validating known ones.
func (t ParameterType) IsValid() bool {
  switch t {
  case ParameterTypeString, ParameterTypeInt, ParameterTypeFloat, ParameterTypeBool, ParameterTypeMap, ParameterTypeArray, ParameterTypeAny:
    return true
  default:
    return false
  }
}

type ParameterDefinition struct {
  Type        ParameterType `json:"type" yaml:"type" doc:"Parameter data type (string, int, float, bool, map, array, any)" example:"string" required:"true"`
  Required    bool   `json:"required,omitempty" yaml:"required,omitempty" doc:"Whether parameter is required" example:"true"`
  Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of the parameter" minLength:"5" maxLength:"500" example:"The name of the resource to create"`
  Default     any    `json:"default,omitempty" yaml:"default,omitempty" doc:"Default value for optional parameters" example:"default-value"`
  Example     any    `json:"example,omitempty" yaml:"example,omitempty" doc:"Example value for documentation" example:"my-resource"`
  
  // Validation constraints
  MinLength int      `json:"minLength,omitempty" yaml:"minLength,omitempty" doc:"Minimum string length constraint" example:"3"`
  MaxLength int      `json:"maxLength,omitempty" yaml:"maxLength,omitempty" doc:"Maximum string length constraint" example:"100"`
  Pattern   string   `json:"pattern,omitempty" yaml:"pattern,omitempty" doc:"Regex pattern for validation" example:"^[a-z0-9-]+$"`
  Enum      []any    `json:"enum,omitempty" yaml:"enum,omitempty" doc:"Allowed values enumeration"`
  Format    string   `json:"format,omitempty" yaml:"format,omitempty" doc:"Type hint (uri, email, date, etc.)" example:"uri"`
  
  Deprecated bool `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Whether parameter is deprecated" example:"false"`
  IsSecret   bool `json:"isSecret,omitempty" yaml:"isSecret,omitempty" doc:"Whether parameter contains sensitive data (for render-mode redaction and security handling)" example:"false"`
}

// Contact represents the maintainer's contact information, including their name and email address.
type Contact struct {
  Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"The name of the maintainer" minLength:"3" maxLength:"60" example:"John Doe" pattern:"^[\\w \\-.'(),&]+$"`
  Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"The email of the maintainer" minLength:"5" maxLength:"100" example:"john.doe@example.com" pattern:"[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,3}"`
}

// Link represents a named hyperlink with validation constraints.
type Link struct {
  Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"The name of the link" minLength:"3" maxLength:"30" example:"Documentation" pattern:"^(\\w|\\-|\\_|\\ )+$"`
  URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"The URL of the link" minLength:"12" maxLength:"500" example:"https://google.com" format:"uri" pattern:"^(http|https):\\/\\/.+"`
}

// ProviderExample represents a usage example demonstrating how to invoke the provider.
// Examples help with documentation generation, catalog display, and IDE support.
type ProviderExample struct {
  Name        string `json:"name,omitempty" yaml:"name,omitempty" doc:"Name of the example use case" minLength:"3" maxLength:"50" example:"Basic usage" pattern:"^[\\w \\-.'()]+$"`
  Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Description of what the example demonstrates" minLength:"10" maxLength:"300" example:"Reads environment variable and transforms to uppercase"`
  YAML        string `json:"yaml" yaml:"yaml" doc:"YAML example showing provider usage in resolver or action context" minLength:"10" maxLength:"2000" required:"true"`
}
~~~

This interface is illustrative. The exact implementation may evolve, but the contract remains schema-first and explicit.

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
  from:
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
  into:
    - provider: cel
      inputs:
        expression: __self.toLowerCase()
~~~

Each step receives the previous value as `__self`.

---

## Providers in Validation

Validation is provider-backed.

Any provider that returns a boolean may be used as a validation provider.

### Built-in Provider: validation

The built-in `validation` provider supports:

- `match` - regex pattern that must match (supports all input forms)
- `notMatch` - regex pattern that must not match (supports all input forms)
- `expression` - CEL expression returning boolean

Rules:

- `match` and `notMatch` may be combined
- `match` and `notMatch` support all four input forms (literal, rslvr, expr, tmpl)
- `expression` is for CEL-based validation
- The provider returns a single boolean result

Examples:

Literal regex patterns:

~~~yaml
validate:
  from:
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
  from:
    - provider: validation
      inputs:
        match:
          rslvr: validationPattern
      message: "Must match validation pattern"
~~~

Using template for pattern:

~~~yaml
validate:
  from:
    - provider: validation
      inputs:
        match:
          tmpl: "^{{ .allowedPrefix }}-[a-z0-9]+$"
      message: "Must match allowed prefix"
~~~

Using CEL expression for validation logic:

~~~yaml
validate:
  from:
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
    provider: shell
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

Reads a value supplied at invocation time.

~~~yaml
resolve:
  from:
    - provider: parameter
      inputs:
        key: env
~~~

---

### env

Reads from the process environment.

~~~yaml
resolve:
  from:
    - provider: env
      inputs:
        key: PROJECT_NAME
~~~

---

### static

Supplies a literal value.

~~~yaml
resolve:
  from:
    - provider: static
      inputs:
        value: my-app
~~~

---

### filesystem

Reads data from the local filesystem.

~~~yaml
resolve:
  from:
    - provider: filesystem
      inputs:
        operation: read
        path: ./config/name.txt
~~~

---

### git

Reads data from a git repository or working tree.

~~~yaml
resolve:
  from:
    - provider: git
      inputs:
        field: branch
~~~

---

### api

Fetches data from an HTTP endpoint.

~~~yaml
resolve:
  from:
    - provider: api
      inputs:
        endpoint: https://api.example.com/project
        method: GET
~~~

---

### cel

Derives a value using CEL.

~~~yaml
resolve:
  from:
    - provider: cel
      inputs:
        expression: _.org + "/" + _.repo
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
