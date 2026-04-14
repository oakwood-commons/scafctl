// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validationprovider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// ProviderName is the name of this provider.
const ProviderName = "validation"

// ValidationProvider provides validation operations using regex patterns and CEL expressions.
type ValidationProvider struct {
	descriptor *provider.Descriptor
}

// NewValidationProvider creates a new validation provider instance.
func NewValidationProvider() *ValidationProvider {
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
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"value": schemahelper.StringProp("Value to validate (if not using __self from transform context)",
					schemahelper.WithExample("my-value")),
				"match": schemahelper.StringProp("Regex pattern that must match the value",
					schemahelper.WithExample("^[a-z0-9-]+$"),
					schemahelper.WithMaxLength(1000)),
				"notMatch": schemahelper.StringProp("Regex pattern that must NOT match the value",
					schemahelper.WithExample("^test-"),
					schemahelper.WithMaxLength(1000)),
				"expression": schemahelper.StringProp("CEL expression that must evaluate to true for validation to pass (has access to __self and _ for resolver data). Mutually exclusive with failWhen",
					schemahelper.WithExample("__self in _.allowedEnvironments"),
					schemahelper.WithMaxLength(1000)),
				"failWhen": schemahelper.StringProp("CEL expression that triggers a validation failure when true (has access to __self and _ for resolver data). Reads naturally for error conditions. Mutually exclusive with expression",
					schemahelper.WithExample("__self.statusCode == 401"),
					schemahelper.WithMaxLength(1000)),
				"message": schemahelper.StringProp("Custom error message to display when validation fails",
					schemahelper.WithExample("Value must be a valid email address"),
					schemahelper.WithMaxLength(1000)),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityValidation: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"valid":   schemahelper.BoolProp("Whether the value passed validation"),
					"errors":  schemahelper.ArrayProp("Validation error messages"),
					"details": schemahelper.StringProp("Details about validation failure (if any)"),
				}),
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"valid":   schemahelper.BoolProp("Whether the value passed validation"),
					"details": schemahelper.StringProp("Details about validation failure (if any)"),
				}),
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
				{
					Name:        "Error-condition validation with failWhen",
					Description: "Fail validation when an error condition is true (reads naturally for error checks)",
					YAML: `name: validate-auth
provider: validation
inputs:
  failWhen: "__self.statusCode == 401"
  message: "Authentication failed (HTTP 401). Run: scafctl auth login entra"`,
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
func (p *ValidationProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	// Get resolver data from context
	resolverData, _ := provider.ResolverContextFromContext(ctx)

	// Get the value to validate - can be any type for expression validation
	var valueAny any
	var valueStr string
	haveStringValue := false

	// First check for explicit value input
	if v, exists := inputs["value"]; exists {
		valueAny = v
		if s, ok := v.(string); ok {
			valueStr = s
			haveStringValue = true
		}
	} else if v, exists := inputs["__self"]; exists {
		// Check for __self in inputs
		valueAny = v
		if s, ok := v.(string); ok {
			valueStr = s
			haveStringValue = true
		}
	} else if v, exists := resolverData["__self"]; exists {
		// Check resolver context for __self (set by executor during validate phase)
		valueAny = v
		if s, ok := v.(string); ok {
			valueStr = s
			haveStringValue = true
		}
	} else {
		return nil, fmt.Errorf("%s: either 'value' or '__self' is required", ProviderName)
	}

	// Get validation criteria
	matchPattern, _ := inputs["match"].(string)
	notMatchPattern, _ := inputs["notMatch"].(string)
	expression, _ := inputs["expression"].(string)
	failWhen, _ := inputs["failWhen"].(string)

	// expression and failWhen are mutually exclusive
	if expression != "" && failWhen != "" {
		return nil, fmt.Errorf("%s: 'expression' and 'failWhen' are mutually exclusive -- use one or the other", ProviderName)
	}

	// At least one validation criterion is required
	if matchPattern == "" && notMatchPattern == "" && expression == "" && failWhen == "" {
		return nil, fmt.Errorf("%s: at least one of 'match', 'notMatch', 'expression', or 'failWhen' is required", ProviderName)
	}

	// Validate with match pattern (requires string value)
	if matchPattern != "" {
		if !haveStringValue {
			return nil, fmt.Errorf("%s: 'match' pattern requires a string value, got %T", ProviderName, valueAny)
		}
		matched, err := regexp.MatchString(matchPattern, valueStr)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid match pattern: %w", ProviderName, err)
		}
		if !matched {
			// Get custom message from inputs if provided
			message := fmt.Sprintf("value does not match pattern: %s", matchPattern)
			if customMsg, ok := inputs["message"].(string); ok && customMsg != "" {
				message = customMsg
			}
			return nil, fmt.Errorf("%s: %s", ProviderName, message)
		}
	}

	// Validate with notMatch pattern (requires string value)
	if notMatchPattern != "" {
		if !haveStringValue {
			return nil, fmt.Errorf("%s: 'notMatch' pattern requires a string value, got %T", ProviderName, valueAny)
		}
		matched, err := regexp.MatchString(notMatchPattern, valueStr)
		if err != nil {
			return nil, fmt.Errorf("%s: invalid notMatch pattern: %w", ProviderName, err)
		}
		if matched {
			// Get custom message from inputs if provided
			message := fmt.Sprintf("value matches forbidden pattern: %s", notMatchPattern)
			if customMsg, ok := inputs["message"].(string); ok && customMsg != "" {
				message = customMsg
			}
			return nil, fmt.Errorf("%s: %s", ProviderName, message)
		}
	}

	// Validate with CEL expression (works with any type)
	// expression: pass when true; failWhen: fail when true
	celExpr := expression
	invertResult := false
	if failWhen != "" {
		celExpr = failWhen
		invertResult = true
	}
	if celExpr != "" {
		// Use EvaluateExpression with resolver data under _ and value as __self
		result, err := celexp.EvaluateExpression(ctx, celExpr, resolverData, map[string]any{
			celexp.VarSelf: valueAny,
		})
		if err != nil {
			fieldName := "expression"
			if invertResult {
				fieldName = "failWhen"
			}
			return nil, fmt.Errorf("%s: %s evaluation failed: %w", ProviderName, fieldName, err)
		}

		// Check result type
		boolResult, ok := result.(bool)
		if !ok {
			fieldName := "expression"
			if invertResult {
				fieldName = "failWhen"
			}
			return nil, fmt.Errorf("%s: %s must return boolean, got %T", ProviderName, fieldName, result)
		}

		// For failWhen, invert: fail when expression is true
		valid := boolResult
		if invertResult {
			valid = !boolResult
		}

		if !valid {
			// Get custom message from inputs if provided
			var message string
			if invertResult {
				message = fmt.Sprintf("error condition met: %s", celExpr)
			} else {
				message = fmt.Sprintf("expression evaluated to false: %s", celExpr)
			}
			if customMsg, ok := inputs["message"].(string); ok && customMsg != "" {
				message = customMsg
			}
			return nil, fmt.Errorf("%s: %s", ProviderName, message)
		}
	}

	// All validations passed
	lgr.V(1).Info("provider completed", "provider", ProviderName, "valid", true)
	return &provider.Output{
		Data: map[string]any{
			"valid":   true,
			"details": "all validations passed",
		},
	}, nil
}
