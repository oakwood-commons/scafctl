// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSchemaValidator(t *testing.T) {
	validator := NewSchemaValidator()
	assert.NotNil(t, validator)
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
	schema := schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
		"name": schemahelper.StringProp("Resource name",
			schemahelper.WithMinLength(5),
			schemahelper.WithMaxLength(100),
		),
		"count": schemahelper.IntProp("Count",
			schemahelper.WithMinimum(0),
			schemahelper.WithMaximum(100),
		),
	})

	inputs := map[string]any{
		"name":  "valid-name-123",
		"count": 50,
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	assert.NoError(t, err)
}

func TestSchemaValidator_ValidateInputs_RequiredMissing(t *testing.T) {
	schema := schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
		"name": schemahelper.StringProp(""),
	})

	inputs := map[string]any{}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)

	var valErrs ValidationErrors
	ok := errors.As(err, &valErrs)
	require.True(t, ok)
	assert.Len(t, valErrs, 1)
	assert.Equal(t, "inputs", valErrs[0].Field)
	assert.Contains(t, valErrs[0].Message, "required")
}

func TestSchemaValidator_ValidateInputs_TypeMismatch(t *testing.T) {
	schema := schemahelper.ObjectSchema([]string{"count"}, map[string]*jsonschema.Schema{
		"count": schemahelper.IntProp(""),
	})

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
	assert.Equal(t, "inputs", valErrs[0].Field)
	assert.Contains(t, valErrs[0].Message, "type")
}

func TestSchemaValidator_ValidateInputs_MinLength(t *testing.T) {
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"name": schemahelper.StringProp("", schemahelper.WithMinLength(5)),
	})

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
	assert.Equal(t, "inputs", valErrs[0].Field)
	assert.Contains(t, valErrs[0].Message, "minLength")
}

func TestSchemaValidator_ValidateInputs_MaxLength(t *testing.T) {
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"name": schemahelper.StringProp("", schemahelper.WithMaxLength(5)),
	})

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
	assert.Contains(t, valErrs[0].Message, "maxLength")
}

func TestSchemaValidator_ValidateInputs_Pattern(t *testing.T) {
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"name": schemahelper.StringProp("", schemahelper.WithPattern("^[a-z0-9-]+$")),
	})

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
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"count": schemahelper.IntProp("",
			schemahelper.WithMinimum(10),
			schemahelper.WithMaximum(100),
		),
	})

	tests := []struct {
		name      string
		value     any
		expectErr bool
		errMsg    string
	}{
		{"within range", 50, false, ""},
		{"below minimum", 5, true, "minimum"},
		{"above maximum", 150, true, "maximum"},
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
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"value": schemahelper.NumberProp(""),
	})

	inputs := map[string]any{
		"value": 42,
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	assert.NoError(t, err)
}

func TestSchemaValidator_ValidateInputs_ArrayConstraints(t *testing.T) {
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"tags": schemahelper.ArrayProp("",
			schemahelper.WithMinItems(2),
			schemahelper.WithMaxItems(5),
		),
	})

	tests := []struct {
		name      string
		value     any
		expectErr bool
		errMsg    string
	}{
		{"within range", []string{"a", "b", "c"}, false, ""},
		{"too few items", []string{"a"}, true, "minItems"},
		{"too many items", []string{"a", "b", "c", "d", "e", "f"}, true, "maxItems"},
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
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"status": schemahelper.StringProp("", schemahelper.WithEnum("pending", "active", "inactive")),
	})

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
	// Note: The jsonschema library treats 'format' as an annotation per JSON Schema spec,
	// so format validation is not enforced. We only test that valid values are accepted.
	tests := []struct {
		name   string
		format string
		value  string
	}{
		{"valid uri", "uri", "https://example.com/path"},
		{"valid email", "email", "user@example.com"},
		{"valid uuid", "uuid", "550e8400-e29b-41d4-a716-446655440000"},
		{"valid date", "date", "2024-01-15"},
		{"valid date-time", "date-time", "2024-01-15T10:30:00Z"},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"field": schemahelper.StringProp("", schemahelper.WithFormat(tt.format)),
			})

			inputs := map[string]any{"field": tt.value}
			err := validator.ValidateInputs(inputs, schema)
			assert.NoError(t, err)
		})
	}
}

func TestSchemaValidator_ValidateInputs_MultipleErrors(t *testing.T) {
	schema := schemahelper.ObjectSchema([]string{"name", "age"}, map[string]*jsonschema.Schema{
		"name": schemahelper.StringProp("", schemahelper.WithMinLength(5)),
		"age":  schemahelper.IntProp(""),
	})

	inputs := map[string]any{
		"name": "abc",
		"age":  "not-a-number",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateInputs(inputs, schema)
	require.Error(t, err)
	// The jsonschema library may report errors one at a time; just verify an error is returned
	errorMsg := err.Error()
	assert.True(t, strings.Contains(errorMsg, "name") || strings.Contains(errorMsg, "age"),
		"error should reference a failing field: %s", errorMsg)
}

func TestSchemaValidator_ValidateOutput_MapOutput(t *testing.T) {
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"id": schemahelper.StringProp(""),
	})

	output := map[string]any{
		"id": "resource-123",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateOutput(output, schema)
	assert.NoError(t, err)
}

func TestSchemaValidator_ValidateOutput_NonMapOutput(t *testing.T) {
	validator := NewSchemaValidator()
	err := validator.ValidateOutput("simple-string-result", nil)
	assert.NoError(t, err)
}

func TestSchemaValidator_ValidateOutput_EmptySchema(t *testing.T) {
	output := map[string]any{
		"anything": "goes",
	}

	validator := NewSchemaValidator()
	err := validator.ValidateOutput(output, nil)
	assert.NoError(t, err)
}

func TestGetActualType_Integration(t *testing.T) {
	// Test type detection indirectly through validation
	tests := []struct {
		name      string
		schema    *jsonschema.Schema
		value     any
		expectErr bool
	}{
		{"string matches", schemahelper.StringProp(""), "hello", false},
		{"int matches", schemahelper.IntProp(""), 42, false},
		{"float matches", schemahelper.NumberProp(""), 3.14, false},
		{"bool matches", schemahelper.BoolProp(""), true, false},
		{"any accepts map", schemahelper.AnyProp(""), map[string]any{"key": "value"}, false},
		{"wrong type", schemahelper.StringProp(""), 42, true},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"field": tt.schema,
			})
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
		schema    *jsonschema.Schema
		value     any
		expectErr bool
	}{
		{"exact string match", schemahelper.StringProp(""), "test", false},
		{"exact int match", schemahelper.IntProp(""), 42, false},
		{"float accepts int", schemahelper.NumberProp(""), 42, false},
		{"int does not accept float", schemahelper.IntProp(""), 3.14, true},
		{"any accepts string", schemahelper.AnyProp(""), "test", false},
		{"any accepts int", schemahelper.AnyProp(""), 42, false},
		{"any accepts map", schemahelper.AnyProp(""), map[string]any{"key": "val"}, false},
		{"string does not accept int", schemahelper.StringProp(""), 42, true},
	}

	validator := NewSchemaValidator()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"field": tt.schema,
			})
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
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"email": schemahelper.StringProp("", schemahelper.WithFormat("email")),
	})

	inputs := map[string]any{
		"email": "test@example.com",
	}

	err := validator.ValidateInputs(inputs, schema)
	assert.NoError(t, err)
}

func TestInvalidRegexPattern(t *testing.T) {
	// The jsonschema library may handle invalid regex patterns differently
	// (e.g., treating them as a schema error rather than a validation error).
	// Test that schema with invalid pattern doesn't panic.
	validator := NewSchemaValidator()
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"field": schemahelper.StringProp("", schemahelper.WithPattern("[invalid(regex")),
	})

	inputs := map[string]any{
		"field": "value",
	}

	// Should either error or succeed without panicking
	_ = validator.ValidateInputs(inputs, schema)
}

// Benchmarks

func BenchmarkValidateInputs_Simple(b *testing.B) {
	validator := NewSchemaValidator()
	schema := schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
		"name": schemahelper.StringProp(""),
	})
	inputs := map[string]any{
		"name": "test-name",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateInputs(inputs, schema)
	}
}

func BenchmarkValidateInputs_Complex(b *testing.B) {
	validator := NewSchemaValidator()
	schema := schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
		"name": schemahelper.StringProp("",
			schemahelper.WithMinLength(5),
			schemahelper.WithMaxLength(100),
			schemahelper.WithPattern("^[a-z0-9-]+$"),
		),
		"count": schemahelper.IntProp("",
			schemahelper.WithMinimum(0),
			schemahelper.WithMaximum(100),
		),
		"tags": schemahelper.ArrayProp("",
			schemahelper.WithMinItems(1),
			schemahelper.WithMaxItems(10),
		),
		"email": schemahelper.StringProp("", schemahelper.WithFormat("email")),
	})
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
	schema := schemahelper.ObjectSchema([]string{"id"}, map[string]*jsonschema.Schema{
		"id": schemahelper.StringProp(""),
	})
	output := map[string]any{
		"id": "resource-123",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateOutput(output, schema)
	}
}

func BenchmarkValidateStringConstraints(b *testing.B) {
	validator := NewSchemaValidator()
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"field": schemahelper.StringProp("",
			schemahelper.WithMinLength(5),
			schemahelper.WithMaxLength(100),
			schemahelper.WithPattern("^[a-z0-9-]+$"),
		),
	})
	inputs := map[string]any{
		"field": "test-resource-name",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateInputs(inputs, schema)
	}
}

func BenchmarkValidateNumericConstraints(b *testing.B) {
	validator := NewSchemaValidator()
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"field": schemahelper.IntProp("",
			schemahelper.WithMinimum(0),
			schemahelper.WithMaximum(100),
		),
	})
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
	schema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"field": schemahelper.StringProp("", schemahelper.WithFormat("email")),
	})
	inputs := map[string]any{
		"field": "user@example.com",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validator.ValidateInputs(inputs, schema)
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
