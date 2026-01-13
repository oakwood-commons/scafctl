package provider

import (
	"context"

	"github.com/Masterminds/semver/v3"
)

// Provider is the core interface that all providers must implement.
// Providers are stateless execution primitives that perform single, well-defined operations.
type Provider interface {
	// Descriptor returns the provider's metadata, schema, and capabilities.
	Descriptor() *Descriptor

	// Execute runs the provider logic with resolved inputs.
	Execute(ctx context.Context, inputs map[string]any) (*Output, error)
}

// Descriptor contains provider identity, versioning, schemas, capabilities, and catalog metadata.
type Descriptor struct {
	Name         string                            `json:"name" yaml:"name" doc:"Provider name (must be unique)" required:"true"`
	DisplayName  string                            `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name"`
	APIVersion   string                            `json:"apiVersion" yaml:"apiVersion" doc:"API version" required:"true"`
	Version      *semver.Version                   `json:"version" yaml:"version" doc:"Semantic version" required:"true"`
	Description  string                            `json:"description" yaml:"description" doc:"Provider description" required:"true"`
	Schema       SchemaDefinition                  `json:"schema" yaml:"schema" doc:"Input properties schema" required:"true"`
	OutputSchema SchemaDefinition                  `json:"outputSchema,omitempty" yaml:"outputSchema,omitempty" doc:"Output structure schema"`
	Decode       func(map[string]any) (any, error) `json:"-" yaml:"-"`
	MockBehavior string                            `json:"mockBehavior,omitempty" yaml:"mockBehavior,omitempty" doc:"Dry-run behavior description"`
	Capabilities []Capability                      `json:"capabilities" yaml:"capabilities" doc:"Provider capabilities" required:"true"`
	Category     string                            `json:"category,omitempty" yaml:"category,omitempty" doc:"Provider category"`
	Tags         []string                          `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Tags for searchability" maxItems:"10"`
	Icon         string                            `json:"icon,omitempty" yaml:"icon,omitempty" doc:"Icon identifier or URL"`
	Links        []Link                            `json:"links,omitempty" yaml:"links,omitempty" doc:"Related links" maxItems:"5"`
	Examples     []Example                         `json:"examples,omitempty" yaml:"examples,omitempty" doc:"Usage examples" maxItems:"10"`
	Deprecated   bool                              `json:"deprecated,omitempty" yaml:"deprecated,omitempty" doc:"Whether deprecated"`
	Beta         bool                              `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Whether in beta"`
	Maintainers  []Contact                         `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"Maintainers" maxItems:"10"`
}

// Output is the standardized return structure for all provider executions.
type Output struct {
	Data     any            `json:"data" yaml:"data" doc:"Provider output data" required:"true"`
	Warnings []string       `json:"warnings,omitempty" yaml:"warnings,omitempty" doc:"Non-fatal warning messages" maxItems:"20"`
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
	Name  string `json:"name,omitempty" yaml:"name,omitempty" doc:"Maintainer name" minLength:"3" maxLength:"60"`
	Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"Maintainer email" minLength:"5" maxLength:"100"`
}

// Link represents a named hyperlink.
type Link struct {
	Name string `json:"name,omitempty" yaml:"name,omitempty" doc:"Link name" minLength:"3" maxLength:"30"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty" doc:"Link URL" minLength:"12" maxLength:"500" format:"uri"`
}

// Example represents a usage example for a provider.
type Example struct {
	Name        string `json:"name,omitempty" yaml:"name,omitempty" doc:"Example name" minLength:"3" maxLength:"50"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Example description" minLength:"10" maxLength:"300"`
	YAML        string `json:"yaml" yaml:"yaml" doc:"YAML example" minLength:"10" maxLength:"2000" required:"true"`
}
