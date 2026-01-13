package provider

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSchemaValidator(t *testing.T) {
	validator := NewSchemaValidator()
	assert.NotNil(t, validator)
	assert.NotNil(t, validator.validate)
}

func TestValidationError_Error(t *testing.T) {
	err := &ValidationError{
		Field:      "name",
		Value:      "abc",
		Constraint: "minLength",
		Message:    "must be at least 5 characters",
		Actual:     "3",
		Expected:   "5",
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "name")
	assert.Contains(t, errStr, "must be at least 5 characters")
}

func TestValidationErrors_Error(t *testing.T) {
	errs := ValidationErrors{
		{Field: "name", Constraint: "required", Message: "is required"},
		{Field: "age", Constraint: "minimum", Message: "must be >= 0"},
		{Field: "email", Constraint: "format", Message: "invalid format"},
	}

	errStr := errs.Error()
	assert.Contains(t, errStr, "validation failed")
	assert.Contains(t, errStr, "name")
	assert.Contains(t, errStr, "age")
	assert.Contains(t, errStr, "email")
}

func TestValidationErrors_ErrorSingle(t *testing.T) {
	errs := ValidationErrors{
		{Field: "name", Constraint: "required", Message: "is required"},
	}

	errStr := errs.Error()
	assert.Contains(t, errStr, "name")
	assert.Contains(t, errStr, "is required")
}

func TestSchemaValidator_ValidateInputs_Success(t *testing.T) {
	minLen := 5
	maxLen := 100
	minValue := 0.0
	maxValue := 100.0

	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"name": {
				Type:        PropertyTypeString,
				Required:    true,
				Description: "Resource name",
				MinLength:   &minLen,
				MaxLength:   &maxLen,
			},
			"count": {
				Type:        PropertyTypeInt,
				Description: "Count",
				Minimum:     &minValue,
				Maximum:     &maxValue,
			},
		},
	}

	inputs := map[string]any{
		"name":  "valid-name-123",
		"count": 50,
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	assert.NoError(t, err)
}

func TestSchemaValidator_ValidateInputs_RequiredMissing(t *testing.T) {
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"name": {
				Type:     PropertyTypeString,
				Required: true,
			},
		},
	}

	inputs := map[string]any{}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)

	var valErrs ValidationErrors
	ok := errors.As(err, &valErrs)
	require.True(t, ok)
	assert.Len(t, valErrs, 1)
	assert.Equal(t, "inputs.name", valErrs[0].Field)
	assert.Contains(t, valErrs[0].Message, "required")
}

func TestSchemaValidator_ValidateInputs_TypeMismatch(t *testing.T) {
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"count": {
				Type:     PropertyTypeInt,
				Required: true,
			},
		},
	}

	inputs := map[string]any{
		"count": "not-a-number",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)

	var valErrs ValidationErrors
	ok := errors.As(err, &valErrs)
	require.True(t, ok)
	assert.Len(t, valErrs, 1)
	assert.Equal(t, "inputs.count", valErrs[0].Field)
	assert.Contains(t, valErrs[0].Message, "type")
}

func TestSchemaValidator_ValidateInputs_MinLength(t *testing.T) {
	minLen := 5

	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"name": {
				Type:      PropertyTypeString,
				MinLength: &minLen,
			},
		},
	}

	inputs := map[string]any{
		"name": "abc",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)

	var valErrs ValidationErrors
	ok := errors.As(err, &valErrs)
	require.True(t, ok)
	assert.Len(t, valErrs, 1)
	assert.Equal(t, "inputs.name", valErrs[0].Field)
	assert.Contains(t, valErrs[0].Message, "string too short")
}

func TestSchemaValidator_ValidateInputs_MaxLength(t *testing.T) {
	maxLen := 5

	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"name": {
				Type:      PropertyTypeString,
				MaxLength: &maxLen,
			},
		},
	}

	inputs := map[string]any{
		"name": "verylongname",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)

	var valErrs ValidationErrors
	ok := errors.As(err, &valErrs)
	require.True(t, ok)
	assert.Len(t, valErrs, 1)
	assert.Contains(t, valErrs[0].Message, "string too long")
}

func TestSchemaValidator_ValidateInputs_Pattern(t *testing.T) {
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"name": {
				Type:    PropertyTypeString,
				Pattern: "^[a-z0-9-]+$",
			},
		},
	}

	inputs := map[string]any{
		"name": "InvalidName_With_CAPS",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)

	var valErrs ValidationErrors
	ok := errors.As(err, &valErrs)
	require.True(t, ok)
	assert.Len(t, valErrs, 1)
	assert.Contains(t, valErrs[0].Message, "pattern")
}

func TestSchemaValidator_ValidateInputs_NumericConstraints(t *testing.T) {
	minValue := 10.0
	maxValue := 100.0

	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"count": {
				Type:    PropertyTypeInt,
				Minimum: &minValue,
				Maximum: &maxValue,
			},
		},
	}

	tests := []struct {
		name      string
		value     any
		expectErr bool
		errMsg    string
	}{
		{"within range", 50, false, ""},
		{"below minimum", 5, true, "value too small"},
		{"above maximum", 150, true, "value too large"},
		{"at minimum", 10, false, ""},
		{"at maximum", 100, false, ""},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := map[string]any{"count": tt.value}
			err := validator.ValidateInputs(inputs, schema)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSchemaValidator_ValidateInputs_FloatAcceptsInt(t *testing.T) {
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"value": {
				Type: PropertyTypeFloat,
			},
		},
	}

	inputs := map[string]any{
		"value": 42,
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	assert.NoError(t, err)
}

func TestSchemaValidator_ValidateInputs_ArrayConstraints(t *testing.T) {
	minItems := 2
	maxItems := 5

	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"tags": {
				Type:     PropertyTypeArray,
				MinItems: &minItems,
				MaxItems: &maxItems,
			},
		},
	}

	tests := []struct {
		name      string
		value     any
		expectErr bool
		errMsg    string
	}{
		{"within range", []string{"a", "b", "c"}, false, ""},
		{"too few items", []string{"a"}, true, "array too small"},
		{"too many items", []string{"a", "b", "c", "d", "e", "f"}, true, "array too large"},
		{"at minimum", []string{"a", "b"}, false, ""},
		{"at maximum", []string{"a", "b", "c", "d", "e"}, false, ""},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := map[string]any{"tags": tt.value}
			err := validator.ValidateInputs(inputs, schema)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSchemaValidator_ValidateInputs_EnumValidation(t *testing.T) {
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"status": {
				Type: PropertyTypeString,
				Enum: []any{"pending", "active", "inactive"},
			},
		},
	}

	tests := []struct {
		name      string
		value     string
		expectErr bool
	}{
		{"valid enum", "active", false},
		{"invalid enum", "deleted", true},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inputs := map[string]any{"status": tt.value}
			err := validator.ValidateInputs(inputs, schema)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "enum")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSchemaValidator_ValidateInputs_FormatValidation(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		value     string
		expectErr bool
	}{
		{"valid uri", "uri", "https://example.com/path", false},
		{"invalid uri", "uri", "not a uri", true},
		{"valid email", "email", "user@example.com", false},
		{"invalid email", "email", "not-an-email", true},
		{"valid uuid", "uuid", "550e8400-e29b-41d4-a716-446655440000", false},
		{"invalid uuid", "uuid", "not-a-uuid", true},
		{"valid date", "date", "2024-01-15", false},
		{"invalid date", "date", "not-a-date", true},
		{"valid date-time", "date-time", "2024-01-15T10:30:00Z", false},
		{"invalid date-time", "date-time", "not-a-datetime", true},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"field": {
						Type:   PropertyTypeString,
						Format: tt.format,
					},
				},
			}

			inputs := map[string]any{"field": tt.value}
			err := validator.ValidateInputs(inputs, schema)

			if tt.expectErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "format")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSchemaValidator_ValidateInputs_MultipleErrors(t *testing.T) {
	minLen := 5

	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"name": {
				Type:      PropertyTypeString,
				Required:  true,
				MinLength: &minLen,
			},
			"email": {
				Type:     PropertyTypeString,
				Required: true,
				Format:   "email",
			},
		},
	}

	inputs := map[string]any{
		"name":  "abc",
		"email": "not-an-email",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)

	var valErrs ValidationErrors
	ok := errors.As(err, &valErrs)
	require.True(t, ok)
	assert.Len(t, valErrs, 2)
}

func TestSchemaValidator_ValidateOutput_MapOutput(t *testing.T) {
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"id": {
				Type: PropertyTypeString,
			},
		},
	}

	output := &Output{
		Data: map[string]any{
			"id": "resource-123",
		},
	}

	validator := NewSchemaValidator()
	err := validator.ValidateOutput(output, schema)
	assert.NoError(t, err)
}

func TestSchemaValidator_ValidateOutput_NonMapOutput(t *testing.T) {
	schema := SchemaDefinition{}

	output := &Output{
		Data: "simple-string-result",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateOutput(output, schema)
	assert.NoError(t, err)
}

func TestSchemaValidator_ValidateOutput_EmptySchema(t *testing.T) {
	schema := SchemaDefinition{}

	output := &Output{
		Data: map[string]any{
			"anything": "goes",
		},
	}

	validator := NewSchemaValidator()
	err := validator.ValidateOutput(output, schema)
	assert.NoError(t, err)
}

func TestGetActualType_Integration(t *testing.T) {
	// Test type detection indirectly through validation
	tests := []struct {
		name      string
		propType  PropertyType
		value     any
		expectErr bool
	}{
		{"string matches", PropertyTypeString, "hello", false},
		{"int matches", PropertyTypeInt, 42, false},
		{"float matches", PropertyTypeFloat, 3.14, false},
		{"bool matches", PropertyTypeBool, true, false},
		{"any accepts map", PropertyTypeAny, map[string]any{"key": "value"}, false},
		{"wrong type", PropertyTypeString, 42, true},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"field": {Type: tt.propType},
				},
			}
			inputs := map[string]any{"field": tt.value}
			err := validator.ValidateInputs(inputs, schema)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTypeCompatibility(t *testing.T) {
	// Test type compatibility indirectly through validation
	tests := []struct {
		name      string
		propType  PropertyType
		value     any
		expectErr bool
	}{
		{"exact string match", PropertyTypeString, "test", false},
		{"exact int match", PropertyTypeInt, 42, false},
		{"float accepts int", PropertyTypeFloat, 42, false},
		{"int does not accept float", PropertyTypeInt, 3.14, true},
		{"any accepts string", PropertyTypeAny, "test", false},
		{"any accepts int", PropertyTypeAny, 42, false},
		{"any accepts map", PropertyTypeAny, map[string]any{"key": "val"}, false},
		{"string does not accept int", PropertyTypeString, 42, true},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := SchemaDefinition{
				Properties: map[string]PropertyDefinition{
					"field": {Type: tt.propType},
				},
			}
			inputs := map[string]any{"field": tt.value}
			err := validator.ValidateInputs(inputs, schema)

			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFormatValidation_EmailValidFormat(t *testing.T) {
	// Test format validation indirectly through schema validation
	validator := NewSchemaValidator()
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"email": {
				Type:   PropertyTypeString,
				Format: "email",
			},
		},
	}

	inputs := map[string]any{
		"email": "test@example.com",
	}

	err := validator.ValidateInputs(inputs, schema)
	assert.NoError(t, err)
}

func TestInvalidRegexPattern(t *testing.T) {
	// Test invalid regex pattern handling through schema validation
	validator := NewSchemaValidator()
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"field": {
				Type:    PropertyTypeString,
				Pattern: "[invalid(regex",
			},
		},
	}

	inputs := map[string]any{
		"field": "value",
	}

	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)

	var valErrs ValidationErrors
	ok := errors.As(err, &valErrs)
	require.True(t, ok)
	assert.Len(t, valErrs, 1)
	assert.Contains(t, valErrs[0].Message, "invalid regex")
}

// Benchmarks

func BenchmarkValidateInputs_Simple(b *testing.B) {
	validator := NewSchemaValidator()
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"name": {
				Type:     PropertyTypeString,
				Required: true,
			},
		},
	}
	inputs := map[string]any{
		"name": "test-name",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateInputs(inputs, schema)
	}
}

func BenchmarkValidateInputs_Complex(b *testing.B) {
	minLen := 5
	maxLen := 100
	minValue := 0.0
	maxValue := 100.0
	minItems := 1
	maxItems := 10

	validator := NewSchemaValidator()
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"name": {
				Type:      PropertyTypeString,
				Required:  true,
				MinLength: &minLen,
				MaxLength: &maxLen,
				Pattern:   "^[a-z0-9-]+$",
			},
			"count": {
				Type:    PropertyTypeInt,
				Minimum: &minValue,
				Maximum: &maxValue,
			},
			"tags": {
				Type:     PropertyTypeArray,
				MinItems: &minItems,
				MaxItems: &maxItems,
			},
			"email": {
				Type:   PropertyTypeString,
				Format: "email",
			},
		},
	}
	inputs := map[string]any{
		"name":  "test-resource-123",
		"count": 50,
		"tags":  []string{"tag1", "tag2", "tag3"},
		"email": "user@example.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateInputs(inputs, schema)
	}
}

func BenchmarkValidateOutput(b *testing.B) {
	validator := NewSchemaValidator()
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"id": {
				Type:     PropertyTypeString,
				Required: true,
			},
		},
	}
	output := &Output{
		Data: map[string]any{
			"id": "resource-123",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateOutput(output, schema)
	}
}

func BenchmarkValidateStringConstraints(b *testing.B) {
	minLen := 5
	maxLen := 100
	validator := NewSchemaValidator()
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"field": {
				Type:      PropertyTypeString,
				MinLength: &minLen,
				MaxLength: &maxLen,
				Pattern:   "^[a-z0-9-]+$",
			},
		},
	}
	inputs := map[string]any{
		"field": "test-resource-name",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateInputs(inputs, schema)
	}
}

func BenchmarkValidateNumericConstraints(b *testing.B) {
	minValue := 0.0
	maxValue := 100.0
	validator := NewSchemaValidator()
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"field": {
				Type:    PropertyTypeInt,
				Minimum: &minValue,
				Maximum: &maxValue,
			},
		},
	}
	inputs := map[string]any{
		"field": 50,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateInputs(inputs, schema)
	}
}

func BenchmarkValidateFormat_Email(b *testing.B) {
	validator := NewSchemaValidator()
	schema := SchemaDefinition{
		Properties: map[string]PropertyDefinition{
			"field": {
				Type:   PropertyTypeString,
				Format: "email",
			},
		},
	}
	inputs := map[string]any{
		"field": "user@example.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateInputs(inputs, schema)
	}
}

func BenchmarkGetActualType(b *testing.B) {
	value := "test string"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = getActualType(value)
	}
}

func BenchmarkRegexCompile(b *testing.B) {
	pattern := "^[a-z0-9-]+$"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = regexp.Compile(pattern)
	}
}

func BenchmarkRegexMatch(b *testing.B) {
	pattern := regexp.MustCompile("^[a-z0-9-]+$")
	value := "test-resource-name-123"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pattern.MatchString(value)
	}
}

func BenchmarkValidationErrors_Error(b *testing.B) {
	errs := ValidationErrors{
		{Field: "field1", Message: "error 1"},
		{Field: "field2", Message: "error 2"},
		{Field: "field3", Message: "error 3"},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = errs.Error()
	}
}

func BenchmarkStringBuilder(b *testing.B) {
	errs := []string{"error 1", "error 2", "error 3"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var sb strings.Builder
		for _, err := range errs {
			sb.WriteString(err)
			sb.WriteString("\n")
		}
		_ = sb.String()
	}
}
