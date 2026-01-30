package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// Provider is the core interface that all providers must implement.
// Providers are stateless execution primitives that perform single, well-defined operations.
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

// Descriptor contains provider identity, versioning, schemas, capabilities, and catalog metadata.
type Descriptor struct {
	// Name is the unique identifier for this provider. Must be lowercase with hyphens only.
	// Used to reference the provider in configurations and the registry.
	Name string `json:"name" yaml:"name" doc:"Unique provider identifier" minLength:"2" maxLength:"100" example:"http" pattern:"^[a-z][a-z0-9-]*$" required:"true"`

	// DisplayName is the human-readable name shown in UIs and documentation.
	// Optional - defaults to Name if not specified.
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name" maxLength:"100" example:"HTTP Client"`

	// APIVersion indicates the provider API contract version (e.g., "v1").
	// Used for compatibility checking and migration support.
	APIVersion string `json:"apiVersion" yaml:"apiVersion" doc:"Provider API version" example:"v1" pattern:"^v[0-9]+$" required:"true"`

	// Version is the semantic version of this provider implementation.
	// Follows semver conventions for versioning provider releases.
	Version *semver.Version `json:"version" yaml:"version" doc:"Semantic version" required:"true"`

	// Description provides a concise explanation of what the provider does.
	// Displayed in catalogs, help text, and documentation.
	Description string `json:"description" yaml:"description" doc:"Provider description" minLength:"10" maxLength:"500" required:"true"`

	// Schema defines the structure and validation rules for provider inputs.
	// Used for input validation, documentation generation, and UI form building.
	Schema SchemaDefinition `json:"schema" yaml:"schema" doc:"Input schema" required:"true"`

	// OutputSchemas defines the output structure for each supported capability.
	// Each capability can produce different output shapes. Required for all declared capabilities.
	// Certain capabilities have required minimum fields:
	//   - validation: must include "valid" (bool) and "errors" ([]string)
	//   - authentication: must include "authenticated" (bool) and "token" (string)
	//   - action: must include "success" (bool)
	//   - from: no required fields
	//   - transform: no required fields
	OutputSchemas map[Capability]SchemaDefinition `json:"outputSchemas" yaml:"outputSchemas" doc:"Output schemas per capability" required:"true"`

	// Decode converts validated map[string]any inputs into strongly-typed structs for internal use.
	// Called after schema validation but before Execute(). Optional - providers can work with map[string]any directly.
	// When Decode is set, the Executor calls it and passes the result directly to Execute().
	Decode func(map[string]any) (any, error) `json:"-" yaml:"-"`

	// ExtractDependencies extracts resolver dependencies from the provider's inputs.
	// Called during dependency graph building to determine execution order.
	// Optional - if nil, the generic extraction logic is used (which handles common patterns like
	// CEL expressions with _.resolverName and Go templates with {{.resolverName}}).
	// Providers should implement this when they have custom input formats or need special handling
	// (e.g., go-template provider with custom delimiters).
	// The function receives the raw inputs map and returns a slice of resolver names that are referenced.
	ExtractDependencies func(inputs map[string]any) []string `json:"-" yaml:"-"`

	// MockBehavior describes what the provider does during dry-run/mock execution.
	// Should explain the simulated behavior without side effects.
	MockBehavior string `json:"mockBehavior" yaml:"mockBehavior" doc:"Dry-run behavior description" minLength:"10" maxLength:"500" required:"true"`

	// Capabilities declares the execution contexts this provider supports.
	// Determines where the provider can be used (from, transform, validation, etc.).
	Capabilities []Capability `json:"capabilities" yaml:"capabilities" doc:"Supported execution contexts" minItems:"1" required:"true"`

	// Category classifies the provider for organization in catalogs and documentation.
	// Examples: "network", "storage", "security", "utility".
	Category string `json:"category,omitempty" yaml:"category,omitempty" doc:"Classification category" maxLength:"50" example:"network"`

	// Tags are searchable keywords for discovery and filtering.
	// Used in catalog searches and provider listings.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Searchable keywords" maxItems:"20"`

	// Icon is a URL to an image representing the provider.
	// Displayed in UIs and documentation alongside the provider name.
	Icon string `json:"icon,omitempty" yaml:"icon,omitempty" doc:"Icon URL" format:"uri" maxLength:"500"`

	// Links provides related resources such as documentation, source code, or tutorials.
	Links []Link `json:"links,omitempty" yaml:"links,omitempty" doc:"Related links" maxItems:"10"`

	// Examples contains sample configurations demonstrating provider usage.
	// Shown in documentation and can be used for testing.
	Examples []Example `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Usage examples" maxItems:"10"`

	// Deprecated: This field indicates the provider should no longer be used.
	// Deprecated providers may be removed in future versions.
	Deprecated bool `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Deprecation status"`

	// Beta indicates the provider is experimental and may have breaking changes.
	// Beta providers are not recommended for production use.
	Beta bool `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Beta status"`

	// Maintainers lists the people or teams responsible for this provider.
	// Used for contact and support information.
	Maintainers []Contact `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"Maintainer contacts" maxItems:"10"`
}

// Output is the standardized return structure for all provider executions.
type Output struct {
	Data     any            `json:"data" yaml:"data" doc:"Provider output data" required:"true"`
	Warnings []string       `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Non-fatal warning messages" maxItems:"50"`
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty" doc:"Execution metadata"`
}

// SchemaDefinition defines the structure of inputs or outputs for a provider.
type SchemaDefinition struct {
	Properties map[string]PropertyDefinition `json:"properties,omitempty" yaml:"properties,omitempty" doc:"Property definitions"`
}

// PropertyType represents the data type of a provider property.
type PropertyType string

const (
	PropertyTypeString PropertyType = "string"
	PropertyTypeInt    PropertyType = "int"
	PropertyTypeFloat  PropertyType = "float"
	PropertyTypeBool   PropertyType = "bool"
	PropertyTypeArray  PropertyType = "array"
	PropertyTypeAny    PropertyType = "any"
)

// IsValid checks if the property type is valid.
func (t PropertyType) IsValid() bool {
	switch t {
	case PropertyTypeString, PropertyTypeInt, PropertyTypeFloat, PropertyTypeBool, PropertyTypeArray, PropertyTypeAny:
		return true
	default:
		return false
	}
}

// String returns the string representation.
func (t PropertyType) String() string {
	return string(t)
}

// PropertyDefinition describes a single property for a provider.
type PropertyDefinition struct {
	Type        PropertyType `json:"type" yaml:"type" doc:"Property data type" required:"true"`
	Required    bool         `json:"required,omitempty" yaml:"required,omitempty" doc:"Whether required"`
	Description string       `json:"description,omitempty" yaml:"description,omitempty" doc:"Property description" minLength:"5" maxLength:"500"`
	Default     any          `json:"default,omitempty" yaml:"default,omitempty" doc:"Default value"`
	Example     any          `json:"example,omitempty" yaml:"example,omitempty" doc:"Example value"`
	MinLength   *int         `json:"minLength,omitempty" yaml:"minLength,omitempty" doc:"Minimum string length"`
	MaxLength   *int         `json:"maxLength,omitempty" yaml:"maxLength,omitempty" doc:"Maximum string length"`
	Pattern     string       `json:"pattern,omitempty" yaml:"pattern,omitempty" doc:"Regex pattern for validation"`
	Minimum     *float64     `json:"minimum,omitempty" yaml:"minimum,omitempty" doc:"Minimum numeric value"`
	Maximum     *float64     `json:"maximum,omitempty" yaml:"maximum,omitempty" doc:"Maximum numeric value"`
	MinItems    *int         `json:"minItems,omitempty" yaml:"minItems,omitempty" doc:"Minimum array length"`
	MaxItems    *int         `json:"maxItems,omitempty" yaml:"maxItems,omitempty" doc:"Maximum array length"`
	Enum        []any        `json:"enum,omitempty" yaml:"enum,omitempty" doc:"Allowed values"`
	Format      string       `json:"format,omitempty" yaml:"format,omitempty" doc:"Format hint (uri, email, date, uuid, etc.)"`
	Deprecated  bool         `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Whether deprecated"`
	IsSecret    bool         `json:"isSecret,omitempty" yaml:"isSecret,omitempty" doc:"Whether contains sensitive data"`
}

// Capability represents the types of operations a provider can perform.
type Capability string

const (
	CapabilityFrom           Capability = "from"
	CapabilityTransform      Capability = "transform"
	CapabilityValidation     Capability = "validation"
	CapabilityAuthentication Capability = "authentication"
	CapabilityAction         Capability = "action"
)

// IsValid checks if the capability is valid.
func (c Capability) IsValid() bool {
	switch c {
	case CapabilityFrom, CapabilityTransform, CapabilityValidation, CapabilityAuthentication, CapabilityAction:
		return true
	default:
		return false
	}
}

// String returns the string representation.
func (c Capability) String() string {
	return string(c)
}

// Contact represents maintainer contact information.
type Contact struct {
	Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"Maintainer name" maxLength:"60" example:"Jane Doe"`
	Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"Maintainer email" format:"email" maxLength:"100"`
}

// Link represents a named hyperlink.
type Link struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"Link name" maxLength:"30" example:"Documentation"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"Link URL" format:"uri" maxLength:"500"`
}

// Example represents a usage example for a provider.
type Example struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty" doc:"Example name" maxLength:"50" example:"Basic usage"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Example description" maxLength:"300"`
	YAML        string `json:"yaml" yaml:"yaml" doc:"YAML example" minLength:"10" maxLength:"2000" required:"true"`
}

// RequiredOutputField defines a required field for a capability's output schema.
type RequiredOutputField struct {
	Name string
	Type PropertyType
}

// CapabilityRequiredOutputFields maps capabilities to their required output fields.
// Providers must include these minimum fields in their OutputSchemas for each capability.
var CapabilityRequiredOutputFields = map[Capability][]RequiredOutputField{
	CapabilityFrom:      {}, // No required fields
	CapabilityTransform: {}, // No required fields
	CapabilityValidation: {
		{Name: "valid", Type: PropertyTypeBool},
		{Name: "errors", Type: PropertyTypeArray},
	},
	CapabilityAuthentication: {
		{Name: "authenticated", Type: PropertyTypeBool},
		{Name: "token", Type: PropertyTypeString},
	},
	CapabilityAction: {
		{Name: "success", Type: PropertyTypeBool},
	},
}

// ValidateDescriptor validates that a Descriptor meets all requirements.
// Returns an error if:
//   - OutputSchemas is missing for any declared capability
//   - Required fields are missing for capabilities that mandate them
//   - Field types don't match the expected types
func ValidateDescriptor(desc *Descriptor) error {
	if desc == nil {
		return errors.New("descriptor is nil")
	}

	if len(desc.Capabilities) == 0 {
		return errors.New("descriptor must declare at least one capability")
	}

	if desc.OutputSchemas == nil {
		return errors.New("descriptor must define OutputSchemas")
	}

	var errs []error

	for _, cap := range desc.Capabilities {
		schema, exists := desc.OutputSchemas[cap]
		if !exists {
			errs = append(errs, fmt.Errorf("missing output schema for capability %q", cap))
			continue
		}

		requiredFields := CapabilityRequiredOutputFields[cap]
		for _, req := range requiredFields {
			prop, found := schema.Properties[req.Name]
			if !found {
				errs = append(errs, fmt.Errorf("capability %q requires output field %q", cap, req.Name))
				continue
			}
			if prop.Type != req.Type {
				errs = append(errs, fmt.Errorf("capability %q field %q must be type %q, got %q", cap, req.Name, req.Type, prop.Type))
			}
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
