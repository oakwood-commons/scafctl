package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/google/cel-go/cel"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

const (
	// SecretMask is the string used to redact secret values in logs and errors
	SecretMask = "***REDACTED***"
)

// InputValue represents a single input value with exactly one form specified.
// Only one of Literal, Rslvr, Expr, or Tmpl should be set.
type InputValue struct {
	Literal any                        `json:"literal,omitempty" yaml:"literal,omitempty" doc:"Direct static value"`
	Rslvr   string                     `json:"rslvr,omitempty" yaml:"rslvr,omitempty" doc:"Resolver binding (e.g., 'environment')"`
	Expr    celexp.Expression          `json:"expr,omitempty" yaml:"expr,omitempty" doc:"CEL expression to evaluate"`
	Tmpl    gotmpl.GoTemplatingContent `json:"tmpl,omitempty" yaml:"tmpl,omitempty" doc:"Go template to render"`
}

// InputResolver resolves provider inputs from various forms into concrete values.
type InputResolver struct {
	// schema defines the expected input structure and constraints (JSON Schema)
	schema *jsonschema.Schema

	// sensitiveFields lists property names that contain sensitive data
	sensitiveFields map[string]bool

	// resolverContext contains all emitted resolver values from context
	resolverContext map[string]any

	// ctx is the execution context for CEL and template evaluation
	ctx context.Context
}

// NewInputResolver creates a new input resolver for the given schema and context.
// The sensitiveFields parameter lists field names that should be redacted in errors.
func NewInputResolver(ctx context.Context, schema *jsonschema.Schema, sensitiveFields []string) *InputResolver {
	resolverCtx, ok := ResolverContextFromContext(ctx)
	if !ok || resolverCtx == nil {
		resolverCtx = make(map[string]any)
	}

	sf := make(map[string]bool, len(sensitiveFields))
	for _, f := range sensitiveFields {
		sf[f] = true
	}

	return &InputResolver{
		schema:          schema,
		sensitiveFields: sf,
		resolverContext: resolverCtx,
		ctx:             ctx,
	}
}

// ResolveInputs resolves all provider inputs from their declared forms into concrete values.
// It validates exclusivity (only one form per property), resolves each input based on its form,
// applies type coercion, and validates the final values against the schema.
//
// rawInputs is expected to be map[string]InputValue or map[string]any (for backwards compatibility).
// Returns a map[string]any with resolved concrete values, ready for provider execution.
func (r *InputResolver) ResolveInputs(rawInputs any) (map[string]any, error) {
	if rawInputs == nil {
		rawInputs = make(map[string]any)
	}

	// Convert rawInputs to map[string]InputValue
	inputMap, err := r.normalizeInputMap(rawInputs)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize inputs: %w", err)
	}

	resolved := make(map[string]any)

	if r.schema == nil || r.schema.Properties == nil {
		// No schema properties — pass through any literal inputs
		for propName, inputValue := range inputMap {
			value, err := r.resolveValue(propName, inputValue, false)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve property %q: %w", propName, err)
			}
			resolved[propName] = value
		}
		return resolved, nil
	}

	// Build required fields set from schema
	requiredSet := make(map[string]bool, len(r.schema.Required))
	for _, req := range r.schema.Required {
		requiredSet[req] = true
	}

	// Resolve each property defined in the schema
	for propName, propSchema := range r.schema.Properties {
		inputValue, exists := inputMap[propName]
		isRequired := requiredSet[propName]
		isSecret := r.sensitiveFields[propName]

		// Check if property is required
		if !exists {
			if isRequired {
				// Try to use default value from schema
				if propSchema != nil && propSchema.Default != nil {
					var defaultVal any
					if err := json.Unmarshal(propSchema.Default, &defaultVal); err == nil {
						resolved[propName] = defaultVal
						continue
					}
				}
				return nil, fmt.Errorf("required property %q is missing", propName)
			}
			// Optional property not provided — check for default
			if propSchema != nil && propSchema.Default != nil {
				var defaultVal any
				if err := json.Unmarshal(propSchema.Default, &defaultVal); err == nil {
					resolved[propName] = defaultVal
				}
			}
			continue
		}

		// Validate exclusivity (only one form specified)
		if err := r.validateExclusivity(propName, inputValue); err != nil {
			return nil, err
		}

		// Resolve the value based on its form
		value, err := r.resolveValue(propName, inputValue, isSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve property %q: %w", propName, err)
		}

		// Apply type coercion based on JSON Schema type
		targetType := ""
		if propSchema != nil {
			targetType = propSchema.Type
		}
		coerced, err := r.coerceType(propName, value, targetType)
		if err != nil {
			if isSecret {
				return nil, fmt.Errorf("failed to coerce property %q to type %s: coercion failed for secret value", propName, targetType)
			}
			return nil, fmt.Errorf("failed to coerce property %q to type %s: %w", propName, targetType, err)
		}

		resolved[propName] = coerced
	}

	return resolved, nil
}

// normalizeInputMap converts various input formats to map[string]InputValue.
func (r *InputResolver) normalizeInputMap(rawInputs any) (map[string]InputValue, error) {
	if rawInputs == nil {
		return make(map[string]InputValue), nil
	}

	// Try to convert to map[string]any first
	inputsMap, ok := rawInputs.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("inputs must be a map[string]any, got %T", rawInputs)
	}

	result := make(map[string]InputValue)

	for key, value := range inputsMap {
		// Check if it's already an InputValue
		if iv, ok := value.(InputValue); ok {
			result[key] = iv
			continue
		}

		// Check if it's a map that can be converted to InputValue
		if valueMap, ok := value.(map[string]any); ok {
			iv := InputValue{}

			if literal, exists := valueMap["literal"]; exists {
				iv.Literal = literal
			}
			if rslvr, exists := valueMap["rslvr"]; exists {
				if s, ok := rslvr.(string); ok {
					iv.Rslvr = s
				} else {
					return nil, fmt.Errorf("property %q: rslvr must be a string, got %T", key, rslvr)
				}
			}
			if expr, exists := valueMap["expr"]; exists {
				if s, ok := expr.(string); ok {
					iv.Expr = celexp.Expression(s)
				} else if e, ok := expr.(celexp.Expression); ok {
					iv.Expr = e
				} else {
					return nil, fmt.Errorf("property %q: expr must be a string or Expression, got %T", key, expr)
				}
			}
			if tmpl, exists := valueMap["tmpl"]; exists {
				if s, ok := tmpl.(string); ok {
					iv.Tmpl = gotmpl.GoTemplatingContent(s)
				} else if t, ok := tmpl.(gotmpl.GoTemplatingContent); ok {
					iv.Tmpl = t
				} else {
					return nil, fmt.Errorf("property %q: tmpl must be a string or GoTemplatingContent, got %T", key, tmpl)
				}
			}

			result[key] = iv
			continue
		}

		// Treat as literal value
		result[key] = InputValue{Literal: value}
	}

	return result, nil
}

// validateExclusivity ensures only one input form is specified per property.
func (r *InputResolver) validateExclusivity(propName string, input InputValue) error {
	formsSet := 0

	if input.Literal != nil {
		formsSet++
	}
	if input.Rslvr != "" {
		formsSet++
	}
	if input.Expr != "" {
		formsSet++
	}
	if input.Tmpl != "" {
		formsSet++
	}

	if formsSet == 0 {
		return fmt.Errorf("property %q: no input form specified (must provide one of: literal, rslvr, expr, tmpl)", propName)
	}

	if formsSet > 1 {
		return fmt.Errorf("property %q: multiple input forms specified (only one of literal, rslvr, expr, tmpl is allowed)", propName)
	}

	return nil
}

// resolveValue resolves an InputValue to a concrete value based on its form.
func (r *InputResolver) resolveValue(propName string, input InputValue, isSecret bool) (any, error) {
	// Literal form - return as-is
	if input.Literal != nil {
		return input.Literal, nil
	}

	// Resolver binding form - lookup in resolver context
	if input.Rslvr != "" {
		return r.resolveRslvr(propName, input.Rslvr, isSecret)
	}

	// CEL expression form - evaluate expression
	if input.Expr != "" {
		return r.resolveCEL(propName, input.Expr, isSecret)
	}

	// Go template form - render template
	if input.Tmpl != "" {
		return r.resolveTemplate(propName, input.Tmpl, isSecret)
	}

	return nil, fmt.Errorf("property %q: no input form provided", propName)
}

// resolveRslvr resolves a resolver binding (e.g., "environment" or "config.namespace").
func (r *InputResolver) resolveRslvr(_, binding string, isSecret bool) (any, error) {
	// Split on dots for nested access
	parts := strings.Split(binding, ".")

	// Start with the resolver context
	current := any(r.resolverContext)

	// Walk the path
	for i, part := range parts {
		// Try to access as map
		if m, ok := current.(map[string]any); ok {
			value, exists := m[part]
			if !exists {
				if isSecret {
					return nil, fmt.Errorf("resolver binding %s: path not found", SecretMask)
				}
				return nil, fmt.Errorf("resolver binding %q not found at part %q", binding, part)
			}
			current = value
			continue
		}

		// Not a map and not the last part - error
		if i < len(parts)-1 {
			if isSecret {
				return nil, fmt.Errorf("cannot access nested property %s: path invalid", SecretMask)
			}
			return nil, fmt.Errorf("cannot access nested property: %q is not a map at part %q", binding, part)
		}
	}

	return current, nil
}

// resolveCEL evaluates a CEL expression against the resolver context.
func (r *InputResolver) resolveCEL(_ string, expr celexp.Expression, isSecret bool) (any, error) {
	// Build variable declarations for all keys in resolver context
	envOpts := make([]cel.EnvOption, 0, len(r.resolverContext))
	for key := range r.resolverContext {
		// Declare all resolver context variables as dynamic type
		envOpts = append(envOpts, cel.Variable(key, cel.DynType))
	}

	// Compile the expression with context
	result, err := expr.Compile(envOpts, celexp.WithContext(r.ctx))
	if err != nil {
		if isSecret {
			return nil, fmt.Errorf("failed to compile CEL expression: %w", maskError(err))
		}
		return nil, fmt.Errorf("failed to compile CEL expression: %w", err)
	}

	// Evaluate with resolver context as variables
	value, err := result.EvalWithContext(r.ctx, r.resolverContext)
	if err != nil {
		if isSecret {
			return nil, fmt.Errorf("failed to evaluate CEL expression: %w", maskError(err))
		}
		return nil, fmt.Errorf("failed to evaluate CEL expression: %w", err)
	}

	return value, nil
}

// resolveTemplate renders a Go template with the resolver context as data.
func (r *InputResolver) resolveTemplate(propName string, tmpl gotmpl.GoTemplatingContent, isSecret bool) (any, error) {
	// Create template service
	svc := gotmpl.NewService(nil)

	// Render template
	result, err := svc.Execute(r.ctx, gotmpl.TemplateOptions{
		Content: string(tmpl),
		Name:    propName,
		Data:    r.resolverContext,
	})
	if err != nil {
		if isSecret {
			return nil, fmt.Errorf("failed to render template: %w", maskError(err))
		}
		return nil, fmt.Errorf("failed to render template: %w", err)
	}

	// Return the rendered string
	return result.Output, nil
}

// coerceType converts a value to the target JSON Schema type.
func (r *InputResolver) coerceType(_ string, value any, targetType string) (any, error) {
	if value == nil {
		return nil, nil
	}

	// If no target type or empty, no coercion needed
	if targetType == "" {
		return value, nil
	}

	// Get the actual type of the value
	actualType := reflect.TypeOf(value)
	actualKind := actualType.Kind()

	switch targetType {
	case "string":
		if actualKind == reflect.String {
			return value, nil
		}
		return fmt.Sprintf("%v", value), nil

	case "integer":
		if actualKind == reflect.Int || actualKind == reflect.Int64 || actualKind == reflect.Int32 {
			return value, nil
		}
		if s, ok := value.(string); ok {
			i, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot convert %q to integer: %w", s, err)
			}
			return int(i), nil
		}
		if f, ok := value.(float64); ok {
			return int(f), nil
		}
		return nil, fmt.Errorf("cannot convert %T to integer", value)

	case "number":
		if actualKind == reflect.Float64 || actualKind == reflect.Float32 {
			return value, nil
		}
		if s, ok := value.(string); ok {
			f, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot convert %q to number: %w", s, err)
			}
			return f, nil
		}
		if i, ok := value.(int); ok {
			return float64(i), nil
		}
		if i, ok := value.(int64); ok {
			return float64(i), nil
		}
		return nil, fmt.Errorf("cannot convert %T to number", value)

	case "boolean":
		if actualKind == reflect.Bool {
			return value, nil
		}
		if s, ok := value.(string); ok {
			b, err := strconv.ParseBool(s)
			if err != nil {
				return nil, fmt.Errorf("cannot convert %q to boolean: %w", s, err)
			}
			return b, nil
		}
		return nil, fmt.Errorf("cannot convert %T to boolean", value)

	case "array":
		if actualKind == reflect.Slice || actualKind == reflect.Array {
			return value, nil
		}
		if s, ok := value.(string); ok {
			if s == "" {
				return []string{}, nil
			}
			parts := strings.Split(s, ",")
			trimmed := make([]string, len(parts))
			for i, part := range parts {
				trimmed[i] = strings.TrimSpace(part)
			}
			return trimmed, nil
		}
		return nil, fmt.Errorf("cannot convert %T to array", value)

	case "object":
		// No coercion needed for objects
		return value, nil

	default:
		// Unknown type, no coercion
		return value, nil
	}
}

// maskError redacts potentially sensitive information from error messages.
func maskError(err error) error {
	if err == nil {
		return nil
	}
	// For now, just return a generic masked error
	// In a production system, this could be more sophisticated
	return fmt.Errorf("operation failed (details redacted for security)")
}

// MaskValue redacts a value if it should be kept secret.
// This is useful for logging and error messages.
func MaskValue(value any, isSecret bool) string {
	if isSecret {
		return SecretMask
	}
	return fmt.Sprintf("%v", value)
}
