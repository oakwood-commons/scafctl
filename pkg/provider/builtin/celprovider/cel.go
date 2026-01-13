package celprovider

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/google/cel-go/cel"
	celext "github.com/google/cel-go/ext"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/conversion"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	// ProviderName is the name of the cel provider
	ProviderName = "cel"
	// Version is the version of the cel provider
	Version = "1.0.0"
)

// CelProvider provides data transformation using CEL expressions
type CelProvider struct {
	descriptor *provider.Descriptor
}

// NewCelProvider creates a new CEL provider
func NewCelProvider() *CelProvider {
	version, _ := semver.NewVersion(Version)

	return &CelProvider{
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "CEL Provider",
			Description: "Transform and evaluate data using CEL (Common Expression Language) expressions with resolver data from context",
			Version:     version,
			Category:    "data",
			Capabilities: []provider.Capability{
				provider.CapabilityTransform,
				provider.CapabilityAction,
			},
			Schema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"expression": {
						Type:        provider.PropertyTypeString,
						Description: "CEL expression to evaluate. Resolver data from context is available as variables in the expression.",
						Required:    true,
						Example:     "name.upperAscii()",
						MaxLength:   ptrs.IntPtr(8192),
					},
					"variables": {
						Type:        provider.PropertyTypeAny,
						Description: "Additional variables to make available in the CEL expression context",
						Required:    false,
						Example:     map[string]any{"prefix": "Mr.", "suffix": "Jr."},
					},
				},
			},
			OutputSchema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"result": {
						Type:        provider.PropertyTypeAny,
						Description: "The evaluation result",
						Example:     "HELLO WORLD",
					},
					"expression": {
						Type:        provider.PropertyTypeString,
						Description: "The expression that was evaluated",
						Example:     "name.upperAscii()",
					},
				},
			},
			Examples: []provider.Example{
				{
					Name:        "Transform string to uppercase",
					Description: "Use CEL string extension to convert a string to uppercase using resolver data",
					YAML: `name: uppercase-transform
provider: cel
inputs:
  expression: "name.upperAscii()"`,
				},
				{
					Name:        "Conditional expression with resolver data",
					Description: "Evaluate conditional logic based on resolver context values",
					YAML: `name: environment-check
provider: cel
inputs:
  expression: "environment == 'prod' ? 'production' : 'non-production'"`,
				},
				{
					Name:        "Using custom variables",
					Description: "Evaluate expressions with both resolver data and custom variables",
					YAML: `name: custom-variables
provider: cel
inputs:
  expression: "prefix + ' ' + name + ' ' + suffix"
  variables:
    prefix: "Dr."
    suffix: "PhD"`,
				},
			},
		},
	}
}

// Descriptor returns the provider's descriptor
func (p *CelProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the CEL expression evaluation
func (p *CelProvider) Execute(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	// Check for dry-run mode
	if provider.DryRunFromContext(ctx) {
		return p.executeDryRun(inputs)
	}

	// Extract expression
	exprStr, ok := inputs["expression"].(string)
	if !ok || exprStr == "" {
		return nil, fmt.Errorf("expression is required and must be a string")
	}

	// Get resolver data from context
	resolverData, _ := provider.ResolverContextFromContext(ctx)

	// Build CEL variables from resolver data
	celVars := make(map[string]any)
	for k, v := range resolverData {
		celVars[k] = v
	}

	// Create environment options with string extensions
	envOpts := []cel.EnvOption{
		celext.Strings(),
	}

	// Add resolver data variables to environment
	for k := range celVars {
		envOpts = append(envOpts, cel.Variable(k, cel.DynType))
	}

	// Add additional variables if provided
	if vars, ok := inputs["variables"].(map[string]any); ok {
		for k, v := range vars {
			celVars[k] = v
			envOpts = append(envOpts, cel.Variable(k, cel.DynType))
		}
	}

	// Compile the expression
	expr := celexp.Expression(exprStr)
	compiled, err := expr.Compile(envOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to compile expression: %w", err)
	}

	// Evaluate the CEL expression
	result, err := compiled.Eval(celVars)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression: %w", err)
	}

	// Convert CEL types to Go types (handles ref.Val arrays, maps, etc.)
	goResult := conversion.GoToCelValue(result)
	convertedResult := conversion.CelValueToGo(goResult)

	return &provider.Output{
		Data: map[string]any{
			"result":     convertedResult,
			"expression": exprStr,
		},
	}, nil
}

func (p *CelProvider) executeDryRun(inputs map[string]any) (*provider.Output, error) {
	exprStr, _ := inputs["expression"].(string)

	return &provider.Output{
		Data: map[string]any{
			"result":     "[DRY-RUN] Expression not evaluated",
			"expression": exprStr,
		},
		Metadata: map[string]any{
			"dryRun": true,
		},
	}, nil
}
