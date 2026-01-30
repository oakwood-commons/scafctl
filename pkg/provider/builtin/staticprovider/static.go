package staticprovider

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ProviderName is the name of this provider.
const ProviderName = "static"

// StaticProvider returns a static value without performing any operations.
// This is useful for default values, constants, or testing.
type StaticProvider struct{}

// New creates a new static provider instance.
func New() *StaticProvider {
	return &StaticProvider{}
}

// Descriptor returns the provider's metadata and schema.
func (p *StaticProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        "static",
		DisplayName: "Static Value Provider",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Description: "Returns a static value without performing any operations. Useful for constants, defaults, and testing.",
		Schema: provider.SchemaDefinition{
			Properties: map[string]provider.PropertyDefinition{
				"value": {
					Type:        provider.PropertyTypeAny,
					Required:    true,
					Description: "The static value to return (can be any type: string, number, boolean, object, array)",
					Example:     "example-value",
				},
			},
		},
		OutputSchemas: map[provider.Capability]provider.SchemaDefinition{
			provider.CapabilityFrom: {
				Properties: map[string]provider.PropertyDefinition{
					"value": {
						Type:        provider.PropertyTypeAny,
						Description: "The static value that was provided (returned directly)",
						Example:     "example-value",
					},
				},
			},
			provider.CapabilityTransform: {
				Properties: map[string]provider.PropertyDefinition{
					"value": {
						Type:        provider.PropertyTypeAny,
						Description: "The static value that was provided (returned directly)",
						Example:     "example-value",
					},
				},
			},
		},
		Capabilities: []provider.Capability{
			provider.CapabilityFrom,
			provider.CapabilityTransform,
		},
		Category:     "Core",
		Tags:         []string{"static", "constant", "testing", "default"},
		MockBehavior: "Returns the specified static value unchanged",
		Examples: []provider.Example{
			{
				Name:        "String value",
				Description: "Return a static string value",
				YAML: `name: environment
type: static
from:
  value: production`,
			},
			{
				Name:        "Numeric value",
				Description: "Return a static numeric value",
				YAML: `name: port
type: static
from:
  value: 8080`,
			},
			{
				Name:        "Boolean value",
				Description: "Return a static boolean value",
				YAML: `name: enabled
type: static
from:
  value: true`,
			},
			{
				Name:        "Object value",
				Description: "Return a static object/map value",
				YAML: `name: config
type: static
from:
  value:
    host: localhost
    port: 8080
    ssl: true`,
			},
			{
				Name:        "Array value",
				Description: "Return a static array value",
				YAML: `name: environments
type: static
from:
  value:
    - dev
    - staging
    - production`,
			},
		},
	}
}

// Execute returns the static value provided in the inputs.
func (p *StaticProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	value, ok := inputs["value"]
	if !ok {
		return nil, fmt.Errorf("%s: missing required input: value", ProviderName)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName)

	// Return value directly - the resolver executor expects output.Data to be the actual value
	return &provider.Output{
		Data: value,
	}, nil
}
