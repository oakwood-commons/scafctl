package validationprovider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// ValidationProvider provides validation operations using regex patterns and CEL expressions.
type ValidationProvider struct {
	descriptor *provider.Descriptor
}

// NewValidationProvider creates a new validation provider instance.
func NewValidationProvider() *ValidationProvider {
	maxPatternLength := 1000
	version := semver.MustParse("1.0.0")

	return &ValidationProvider{
		descriptor: &provider.Descriptor{
			Name:        "validation",
			DisplayName: "Validation Provider",
			Description: "Provider for validation using regex patterns (match/notMatch) and CEL expressions",
			APIVersion:  "v1",
			Version:     version,
			Category:    "validation",
			Capabilities: []provider.Capability{
				provider.CapabilityValidation,
				provider.CapabilityTransform,
			},
			Schema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"value": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Value to validate (if not using __self from transform context)",
						Example:     "my-value",
					},
					"match": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Regex pattern that must match the value",
						Example:     "^[a-z0-9-]+$",
						MaxLength:   &maxPatternLength,
					},
					"notMatch": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "Regex pattern that must NOT match the value",
						Example:     "^test-",
						MaxLength:   &maxPatternLength,
					},
					"expression": {
						Type:        provider.PropertyTypeString,
						Required:    false,
						Description: "CEL expression that must evaluate to true (has access to __self)",
						Example:     "__self in [\"dev\", \"staging\", \"prod\"]",
						MaxLength:   &maxPatternLength,
					},
				},
			},
			OutputSchema: provider.SchemaDefinition{
				Properties: map[string]provider.PropertyDefinition{
					"valid": {
						Type:        provider.PropertyTypeBool,
						Description: "Whether the value passed validation",
					},
					"details": {
						Type:        provider.PropertyTypeString,
						Description: "Details about validation failure (if any)",
					},
				},
			},
			Examples: []provider.Example{
				{
					Name:        "Regex pattern match validation",
					Description: "Validate that a string matches a specific regex pattern",
					YAML: `name: validate-naming
provider: validation
inputs:
  value: "my-service-name"
  match: "^[a-z0-9-]+$"`,
				},
				{
					Name:        "Regex pattern negative validation",
					Description: "Ensure a string does NOT match a forbidden pattern",
					YAML: `name: validate-no-test-prefix
provider: validation
inputs:
  value: "production-service"
  notMatch: "^test-"`,
				},
				{
					Name:        "CEL expression validation",
					Description: "Validate using a CEL expression to check allowed values",
					YAML: `name: validate-environment
provider: validation
inputs:
  value: "prod"
  expression: "__self in [\"dev\", \"staging\", \"prod\"]"`,
				},
				{
					Name:        "Complex CEL validation",
					Description: "Use CEL to validate complex conditions on string values",
					YAML: `name: validate-version-format
provider: validation
inputs:
  value: "v1.2.3"
  expression: "__self.startsWith(\"v\") && __self.size() >= 5"`,
				},
				{
					Name:        "Combined regex and CEL validation",
					Description: "Apply both regex pattern matching and CEL expression validation",
					YAML: `name: strict-validation
provider: validation
inputs:
  value: "service-123"
  match: "^[a-z0-9-]+$"
  expression: "__self.size() <= 50"`,
				},
			},
		},
	}
}

// Descriptor returns the provider's descriptor.
func (p *ValidationProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs validation.
//
//nolint:revive // ctx required by Provider interface
func (p *ValidationProvider) Execute(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	// Get the value to validate
	value, ok := inputs["value"].(string)
	if !ok {
		// Check for __self (from transform context)
		if selfVal, exists := inputs["__self"]; exists {
			if selfStr, ok := selfVal.(string); ok {
				value = selfStr
			} else {
				return nil, fmt.Errorf("__self must be a string, got %T", selfVal)
			}
		} else {
			return nil, fmt.Errorf("either 'value' or '__self' is required")
		}
	}

	// Get validation criteria
	matchPattern, _ := inputs["match"].(string)
	notMatchPattern, _ := inputs["notMatch"].(string)
	expression, _ := inputs["expression"].(string)

	// At least one validation criterion is required
	if matchPattern == "" && notMatchPattern == "" && expression == "" {
		return nil, fmt.Errorf("at least one of 'match', 'notMatch', or 'expression' is required")
	}

	// Validate with match pattern
	if matchPattern != "" {
		matched, err := regexp.MatchString(matchPattern, value)
		if err != nil {
			return nil, fmt.Errorf("invalid match pattern: %w", err)
		}
		if !matched {
			return &provider.Output{
				Data: map[string]any{
					"valid":   false,
					"details": fmt.Sprintf("value does not match pattern: %s", matchPattern),
				},
			}, nil
		}
	}

	// Validate with notMatch pattern
	if notMatchPattern != "" {
		matched, err := regexp.MatchString(notMatchPattern, value)
		if err != nil {
			return nil, fmt.Errorf("invalid notMatch pattern: %w", err)
		}
		if matched {
			return &provider.Output{
				Data: map[string]any{
					"valid":   false,
					"details": fmt.Sprintf("value matches forbidden pattern: %s", notMatchPattern),
				},
			}, nil
		}
	}

	// Validate with CEL expression
	if expression != "" {
		valid, err := p.evaluateExpression(expression, value)
		if err != nil {
			return nil, fmt.Errorf("expression evaluation failed: %w", err)
		}
		if !valid {
			return &provider.Output{
				Data: map[string]any{
					"valid":   false,
					"details": fmt.Sprintf("expression evaluated to false: %s", expression),
				},
			}, nil
		}
	}

	// All validations passed
	return &provider.Output{
		Data: map[string]any{
			"valid":   true,
			"details": "all validations passed",
		},
	}, nil
}

func (p *ValidationProvider) evaluateExpression(expression, value string) (bool, error) {
	// Create context with __self
	variables := map[string]any{
		"__self": value,
	}

	// Create environment options
	envOpts := []cel.EnvOption{
		cel.Variable("__self", cel.StringType),
	}

	// Compile the expression
	expr := celexp.Expression(expression)
	compiled, err := expr.Compile(envOpts)
	if err != nil {
		return false, fmt.Errorf("failed to compile expression: %w", err)
	}

	// Evaluate
	result, err := compiled.Eval(variables)
	if err != nil {
		return false, fmt.Errorf("failed to evaluate expression: %w", err)
	}

	// Check result type
	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expression must return boolean, got %T", result)
	}

	return boolResult, nil
}
