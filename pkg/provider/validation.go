package provider

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/go-playground/validator/v10"
)

// SchemaValidator provides validation for provider inputs and outputs against schema definitions.
type SchemaValidator struct {
	validate *validator.Validate
}

// NewSchemaValidator creates a new schema validator with custom validation rules.
func NewSchemaValidator() *SchemaValidator {
	v := validator.New()
	_ = v.RegisterValidation("propertytype", validatePropertyType)
	_ = v.RegisterValidation("capability", validateCapability)
	return &SchemaValidator{validate: v}
}

// ValidationError represents a single field validation error with contextual information.
type ValidationError struct {
	Field      string
	Value      any
	Constraint string
	Message    string
	Actual     string
	Expected   string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Actual != "" && e.Expected != "" {
		return fmt.Sprintf("%s: %s (actual: %s, expected: %s)", e.Field, e.Message, e.Actual, e.Expected)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []*ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("validation failed with %d errors:\n", len(e)))
	for _, err := range e {
		sb.WriteString("  - ")
		sb.WriteString(err.Error())
		sb.WriteString("\n")
	}
	return sb.String()
}

// ValidateInputs validates provider inputs against the schema definition.
func (sv *SchemaValidator) ValidateInputs(inputs map[string]any, schema SchemaDefinition) error {
	return sv.validateAgainstSchema(inputs, schema, "inputs")
}

// ValidateOutput validates provider output data against the output schema definition.
func (sv *SchemaValidator) ValidateOutput(output any, schema SchemaDefinition) error {
	var outputMap map[string]any
	switch v := output.(type) {
	case map[string]any:
		outputMap = v
	case nil:
		outputMap = make(map[string]any)
	default:
		outputMap = map[string]any{"value": output}
	}
	return sv.validateAgainstSchema(outputMap, schema, "output")
}

func (sv *SchemaValidator) validateAgainstSchema(data map[string]any, schema SchemaDefinition, contextPath string) error {
	if len(schema.Properties) == 0 {
		return nil
	}

	var errors ValidationErrors

	for propName, propDef := range schema.Properties {
		fieldPath := fmt.Sprintf("%s.%s", contextPath, propName)
		value, exists := data[propName]

		if propDef.Required && !exists {
			errors = append(errors, &ValidationError{
				Field:      fieldPath,
				Value:      nil,
				Constraint: "required",
				Message:    "field is required but not provided",
				Expected:   "value present",
				Actual:     "missing",
			})
			continue
		}

		if !exists {
			continue
		}

		if errs := sv.validateProperty(fieldPath, value, propDef); errs != nil {
			errors = append(errors, errs...)
		}
	}

	if len(errors) > 0 {
		return errors
	}
	return nil
}

func (sv *SchemaValidator) validateProperty(fieldPath string, value any, propDef PropertyDefinition) []*ValidationError {
	var errors []*ValidationError

	if !propDef.Type.IsValid() {
		errors = append(errors, &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "type",
			Message:    fmt.Sprintf("invalid property type: %s", propDef.Type),
			Expected:   "one of: string, int, float, bool, map, array, any",
			Actual:     string(propDef.Type),
		})
		return errors
	}

	if propDef.Type != PropertyTypeAny {
		if err := sv.validateTypeMatch(fieldPath, value, propDef.Type); err != nil {
			errors = append(errors, err)
			return errors
		}
	}

	if len(propDef.Enum) > 0 {
		if err := sv.validateEnum(fieldPath, value, propDef.Enum); err != nil {
			errors = append(errors, err)
		}
	}

	switch propDef.Type {
	case PropertyTypeString:
		errors = append(errors, sv.validateStringConstraints(fieldPath, value, propDef)...)
	case PropertyTypeInt, PropertyTypeFloat:
		errors = append(errors, sv.validateNumericConstraints(fieldPath, value, propDef)...)
	case PropertyTypeArray:
		errors = append(errors, sv.validateArrayConstraints(fieldPath, value, propDef)...)
	case PropertyTypeBool, PropertyTypeAny:
		// No additional constraints for these types
	}

	if propDef.Format != "" && propDef.Type == PropertyTypeString {
		if err := sv.validateFormat(fieldPath, value, propDef.Format); err != nil {
			errors = append(errors, err)
		}
	}

	return errors
}

func (sv *SchemaValidator) validateTypeMatch(fieldPath string, value any, propType PropertyType) *ValidationError {
	actualType := getActualType(value)
	if !isTypeCompatible(actualType, propType) {
		return &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "type",
			Message:    "type mismatch",
			Expected:   string(propType),
			Actual:     actualType,
		}
	}
	return nil
}

func (sv *SchemaValidator) validateEnum(fieldPath string, value any, enum []any) *ValidationError {
	for _, allowed := range enum {
		if reflect.DeepEqual(value, allowed) {
			return nil
		}
	}
	return &ValidationError{
		Field:      fieldPath,
		Value:      value,
		Constraint: "enum",
		Message:    "value not in allowed enumeration",
		Expected:   fmt.Sprintf("one of: %v", enum),
		Actual:     fmt.Sprintf("%v", value),
	}
}

func (sv *SchemaValidator) validateStringConstraints(fieldPath string, value any, propDef PropertyDefinition) []*ValidationError {
	var errors []*ValidationError
	str, ok := value.(string)
	if !ok {
		return errors
	}

	if propDef.MinLength != nil && len(str) < *propDef.MinLength {
		errors = append(errors, &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "minLength",
			Message:    "string too short",
			Expected:   fmt.Sprintf("min: %d", *propDef.MinLength),
			Actual:     fmt.Sprintf("length: %d", len(str)),
		})
	}

	if propDef.MaxLength != nil && len(str) > *propDef.MaxLength {
		errors = append(errors, &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "maxLength",
			Message:    "string too long",
			Expected:   fmt.Sprintf("max: %d", *propDef.MaxLength),
			Actual:     fmt.Sprintf("length: %d", len(str)),
		})
	}

	if propDef.Pattern != "" {
		matched, err := regexp.MatchString(propDef.Pattern, str)
		if err != nil {
			errors = append(errors, &ValidationError{
				Field:      fieldPath,
				Value:      value,
				Constraint: "pattern",
				Message:    fmt.Sprintf("invalid regex pattern: %s", err.Error()),
				Expected:   propDef.Pattern,
				Actual:     "invalid pattern",
			})
		} else if !matched {
			errors = append(errors, &ValidationError{
				Field:      fieldPath,
				Value:      value,
				Constraint: "pattern",
				Message:    "string does not match pattern",
				Expected:   propDef.Pattern,
				Actual:     str,
			})
		}
	}

	return errors
}

func (sv *SchemaValidator) validateNumericConstraints(fieldPath string, value any, propDef PropertyDefinition) []*ValidationError {
	var errors []*ValidationError

	var numValue float64
	switch v := value.(type) {
	case int:
		numValue = float64(v)
	case int32:
		numValue = float64(v)
	case int64:
		numValue = float64(v)
	case float32:
		numValue = float64(v)
	case float64:
		numValue = v
	default:
		return errors
	}

	if propDef.Minimum != nil && numValue < *propDef.Minimum {
		errors = append(errors, &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "minimum",
			Message:    "value too small",
			Expected:   fmt.Sprintf("min: %v", *propDef.Minimum),
			Actual:     fmt.Sprintf("%v", numValue),
		})
	}

	if propDef.Maximum != nil && numValue > *propDef.Maximum {
		errors = append(errors, &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "maximum",
			Message:    "value too large",
			Expected:   fmt.Sprintf("max: %v", *propDef.Maximum),
			Actual:     fmt.Sprintf("%v", numValue),
		})
	}

	return errors
}

func (sv *SchemaValidator) validateArrayConstraints(fieldPath string, value any, propDef PropertyDefinition) []*ValidationError {
	var errors []*ValidationError

	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return errors
	}

	length := rv.Len()

	if propDef.MinItems != nil && length < *propDef.MinItems {
		errors = append(errors, &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "minItems",
			Message:    "array too small",
			Expected:   fmt.Sprintf("min: %d items", *propDef.MinItems),
			Actual:     fmt.Sprintf("%d items", length),
		})
	}

	if propDef.MaxItems != nil && length > *propDef.MaxItems {
		errors = append(errors, &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "maxItems",
			Message:    "array too large",
			Expected:   fmt.Sprintf("max: %d items", *propDef.MaxItems),
			Actual:     fmt.Sprintf("%d items", length),
		})
	}

	return errors
}

func (sv *SchemaValidator) validateFormat(fieldPath string, value any, format string) *ValidationError {
	str, ok := value.(string)
	if !ok {
		return nil
	}

	var pattern string
	var description string

	switch format {
	case "uri", "url":
		pattern = `^https?://[^\s]+$`
		description = "valid URI"
	case "email":
		pattern = `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`
		description = "valid email address"
	case "uuid":
		pattern = `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`
		description = "valid UUID"
	case "date":
		pattern = `^\d{4}-\d{2}-\d{2}$`
		description = "valid date (YYYY-MM-DD)"
	case "date-time":
		pattern = `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})$`
		description = "valid date-time (ISO 8601)"
	default:
		return nil
	}

	matched, err := regexp.MatchString(pattern, str)
	if err != nil || !matched {
		return &ValidationError{
			Field:      fieldPath,
			Value:      value,
			Constraint: "format",
			Message:    fmt.Sprintf("invalid format: %s", format),
			Expected:   description,
			Actual:     str,
		}
	}

	return nil
}

func getActualType(value any) string {
	if value == nil {
		return "nil"
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.String:
		return "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Bool:
		return "bool"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map:
		// Maps should use PropertyTypeAny
		return "any"
	case reflect.Invalid, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Complex64, reflect.Complex128, reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer,
		reflect.Struct, reflect.UnsafePointer:
		return rv.Kind().String()
	default:
		return rv.Kind().String()
	}
}

func isTypeCompatible(actualType string, expectedType PropertyType) bool {
	switch expectedType {
	case PropertyTypeString:
		return actualType == "string"
	case PropertyTypeInt:
		return actualType == "int"
	case PropertyTypeFloat:
		return actualType == "float" || actualType == "int"
	case PropertyTypeBool:
		return actualType == "bool"
	case PropertyTypeArray:
		return actualType == "array"
	case PropertyTypeAny:
		return true
	default:
		return false
	}
}

func validatePropertyType(fl validator.FieldLevel) bool {
	propType, ok := fl.Field().Interface().(PropertyType)
	if !ok {
		return false
	}
	return propType.IsValid()
}

func validateCapability(fl validator.FieldLevel) bool {
	capability, ok := fl.Field().Interface().(Capability)
	if !ok {
		return false
	}
	return capability.IsValid()
}
