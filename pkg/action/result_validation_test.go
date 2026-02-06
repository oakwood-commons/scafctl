package action

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to create a schema from type string
func schemaWithType(t string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: t}
}

// Helper to create an object schema with properties
func objectSchema(properties map[string]*jsonschema.Schema, required []string) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:       "object",
		Properties: properties,
	}
	if len(required) > 0 {
		s.Required = required
	}
	return s
}

func TestValidateResult_NilSchema(t *testing.T) {
	err := ValidateResult(map[string]any{"foo": "bar"}, nil)
	assert.NoError(t, err, "nil schema should pass validation")
}

func TestValidateResult_TypeValidation(t *testing.T) {
	tests := []struct {
		name      string
		result    any
		schema    *jsonschema.Schema
		expectErr bool
	}{
		{
			name:   "object type matches map",
			result: map[string]any{"key": "value"},
			schema: schemaWithType("object"),
		},
		{
			name:      "object type fails on array",
			result:    []any{"a", "b"},
			schema:    schemaWithType("object"),
			expectErr: true,
		},
		{
			name:   "array type matches slice",
			result: []any{1, 2, 3},
			schema: schemaWithType("array"),
		},
		{
			name:      "array type fails on map",
			result:    map[string]any{},
			schema:    schemaWithType("array"),
			expectErr: true,
		},
		{
			name:   "string type matches string",
			result: "hello",
			schema: schemaWithType("string"),
		},
		{
			name:      "string type fails on int",
			result:    42,
			schema:    schemaWithType("string"),
			expectErr: true,
		},
		{
			name:   "integer type matches int",
			result: 42,
			schema: schemaWithType("integer"),
		},
		{
			name:   "number type matches float64",
			result: 3.14,
			schema: schemaWithType("number"),
		},
		{
			name:   "number type accepts int",
			result: 42,
			schema: schemaWithType("number"),
		},
		{
			name:   "boolean type matches bool",
			result: true,
			schema: schemaWithType("boolean"),
		},
		{
			name:      "boolean type fails on string",
			result:    "true",
			schema:    schemaWithType("boolean"),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, tt.schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_RequiredProperties(t *testing.T) {
	schema := objectSchema(
		map[string]*jsonschema.Schema{
			"id":   schemaWithType("integer"),
			"name": schemaWithType("string"),
		},
		[]string{"id", "name"},
	)

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name:   "all required present",
			result: map[string]any{"id": 1, "name": "test"},
		},
		{
			name:   "extra properties allowed by default",
			result: map[string]any{"id": 1, "name": "test", "extra": "value"},
		},
		{
			name:      "missing required id",
			result:    map[string]any{"name": "test"},
			expectErr: true,
		},
		{
			name:      "missing required name",
			result:    map[string]any{"id": 1},
			expectErr: true,
		},
		{
			name:      "empty object",
			result:    map[string]any{},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_PropertyValidation(t *testing.T) {
	schema := objectSchema(
		map[string]*jsonschema.Schema{
			"status":  schemaWithType("string"),
			"count":   schemaWithType("integer"),
			"enabled": schemaWithType("boolean"),
		},
		nil,
	)

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name:   "valid types",
			result: map[string]any{"status": "ok", "count": 5, "enabled": true},
		},
		{
			name:      "wrong type for status",
			result:    map[string]any{"status": 123},
			expectErr: true,
		},
		{
			name:      "wrong type for count",
			result:    map[string]any{"count": "five"},
			expectErr: true,
		},
		{
			name:      "wrong type for enabled",
			result:    map[string]any{"enabled": 1},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_AdditionalPropertiesFalse(t *testing.T) {
	// Use a "false" schema (not: {}) to reject all additional properties
	falseSchema := &jsonschema.Schema{Not: &jsonschema.Schema{}}
	schema := &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			"id":   schemaWithType("integer"),
			"name": schemaWithType("string"),
		},
		AdditionalProperties: falseSchema,
	}

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name:   "exact properties",
			result: map[string]any{"id": 1, "name": "test"},
		},
		{
			name:   "subset of properties",
			result: map[string]any{"id": 1},
		},
		{
			name:      "extra property rejected",
			result:    map[string]any{"id": 1, "name": "test", "extra": "value"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_StringConstraints(t *testing.T) {
	minLen := 3
	maxLen := 10
	schema := objectSchema(
		map[string]*jsonschema.Schema{
			"code": {
				Type:      "string",
				MinLength: &minLen,
				MaxLength: &maxLen,
				Pattern:   "^[A-Z]+$",
			},
		},
		[]string{"code"},
	)

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name:   "valid string",
			result: map[string]any{"code": "ABC"},
		},
		{
			name:   "max length boundary",
			result: map[string]any{"code": "ABCDEFGHIJ"},
		},
		{
			name:      "too short",
			result:    map[string]any{"code": "AB"},
			expectErr: true,
		},
		{
			name:      "too long",
			result:    map[string]any{"code": "ABCDEFGHIJK"},
			expectErr: true,
		},
		{
			name:      "pattern mismatch lowercase",
			result:    map[string]any{"code": "abc"},
			expectErr: true,
		},
		{
			name:      "pattern mismatch with numbers",
			result:    map[string]any{"code": "ABC123"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_NumericConstraints(t *testing.T) {
	minVal := 0.0
	maxVal := 100.0
	schema := objectSchema(
		map[string]*jsonschema.Schema{
			"score": {
				Type:    "integer",
				Minimum: &minVal,
				Maximum: &maxVal,
			},
			"ratio": {
				Type:    "number",
				Minimum: &minVal,
				Maximum: &maxVal,
			},
		},
		nil,
	)

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name:   "valid score and ratio",
			result: map[string]any{"score": 50, "ratio": 0.75},
		},
		{
			name:   "boundary values",
			result: map[string]any{"score": 0, "ratio": 100.0},
		},
		{
			name:      "score below minimum",
			result:    map[string]any{"score": -1},
			expectErr: true,
		},
		{
			name:      "score above maximum",
			result:    map[string]any{"score": 101},
			expectErr: true,
		},
		{
			name:      "ratio below minimum",
			result:    map[string]any{"ratio": -0.1},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_ArrayValidation(t *testing.T) {
	minItems := 1
	maxItems := 5
	schema := &jsonschema.Schema{
		Type:     "array",
		MinItems: &minItems,
		MaxItems: &maxItems,
		Items:    schemaWithType("string"),
	}

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name:   "valid array",
			result: []any{"a", "b", "c"},
		},
		{
			name:   "boundary min",
			result: []any{"a"},
		},
		{
			name:   "boundary max",
			result: []any{"a", "b", "c", "d", "e"},
		},
		{
			name:      "empty array violates minItems",
			result:    []any{},
			expectErr: true,
		},
		{
			name:      "too many items",
			result:    []any{"a", "b", "c", "d", "e", "f"},
			expectErr: true,
		},
		{
			name:      "wrong item type",
			result:    []any{"a", 2, "c"},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_NestedObjects(t *testing.T) {
	schema := objectSchema(
		map[string]*jsonschema.Schema{
			"user": objectSchema(
				map[string]*jsonschema.Schema{
					"id":   schemaWithType("integer"),
					"name": schemaWithType("string"),
					"profile": objectSchema(
						map[string]*jsonschema.Schema{
							"email": schemaWithType("string"),
							"age":   schemaWithType("integer"),
						},
						nil,
					),
				},
				[]string{"id", "name"},
			),
		},
		[]string{"user"},
	)

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name: "valid nested object",
			result: map[string]any{
				"user": map[string]any{
					"id":   1,
					"name": "John",
					"profile": map[string]any{
						"email": "john@example.com",
						"age":   30,
					},
				},
			},
		},
		{
			name: "nested without profile",
			result: map[string]any{
				"user": map[string]any{
					"id":   1,
					"name": "John",
				},
			},
		},
		{
			name:      "missing required user",
			result:    map[string]any{},
			expectErr: true,
		},
		{
			name: "missing required nested id",
			result: map[string]any{
				"user": map[string]any{
					"name": "John",
				},
			},
			expectErr: true,
		},
		{
			name: "wrong type in nested object",
			result: map[string]any{
				"user": map[string]any{
					"id":   "not-an-int",
					"name": "John",
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_EnumConstraint(t *testing.T) {
	schema := objectSchema(
		map[string]*jsonschema.Schema{
			"status": {
				Type: "string",
				Enum: []any{"pending", "active", "completed"},
			},
			"priority": {
				Type: "integer",
				Enum: []any{1, 2, 3},
			},
		},
		nil,
	)

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name:   "valid enum values",
			result: map[string]any{"status": "active", "priority": 2},
		},
		{
			name:      "invalid status enum",
			result:    map[string]any{"status": "unknown"},
			expectErr: true,
		},
		{
			name:      "invalid priority enum",
			result:    map[string]any{"priority": 4},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_ArrayOfObjects(t *testing.T) {
	schema := &jsonschema.Schema{
		Type: "array",
		Items: objectSchema(
			map[string]*jsonschema.Schema{
				"id":   schemaWithType("integer"),
				"name": schemaWithType("string"),
			},
			[]string{"id", "name"},
		),
	}

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name: "valid array of objects",
			result: []any{
				map[string]any{"id": 1, "name": "First"},
				map[string]any{"id": 2, "name": "Second"},
			},
		},
		{
			name:   "empty array",
			result: []any{},
		},
		{
			name: "invalid item missing required field",
			result: []any{
				map[string]any{"id": 1, "name": "First"},
				map[string]any{"id": 2}, // missing name
			},
			expectErr: true,
		},
		{
			name: "invalid item wrong type",
			result: []any{
				map[string]any{"id": "not-int", "name": "First"},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateResult_PropertyItemsSchema(t *testing.T) {
	minItems := 1
	schema := objectSchema(
		map[string]*jsonschema.Schema{
			"tags": {
				Type:     "array",
				MinItems: &minItems,
				Items:    schemaWithType("string"),
			},
		},
		[]string{"tags"},
	)

	tests := []struct {
		name      string
		result    any
		expectErr bool
	}{
		{
			name:   "valid tags array",
			result: map[string]any{"tags": []any{"go", "cli", "tool"}},
		},
		{
			name:      "empty tags violates minItems",
			result:    map[string]any{"tags": []any{}},
			expectErr: true,
		},
		{
			name:      "wrong item type in tags",
			result:    map[string]any{"tags": []any{"go", 123}},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResult(tt.result, schema)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestResultValidationErrors_MultipleErrors(t *testing.T) {
	schema := objectSchema(
		map[string]*jsonschema.Schema{
			"id":     schemaWithType("integer"),
			"name":   schemaWithType("string"),
			"status": schemaWithType("string"),
		},
		[]string{"id", "name", "status"},
	)

	// Result missing multiple required fields and wrong type
	result := map[string]any{
		"id": "not-an-int", // wrong type
		// missing name and status
	}

	err := ValidateResult(result, schema)
	require.Error(t, err)

	// Should contain error messages about validation failures
	errStr := err.Error()
	// The jsonschema library will report type mismatch or required property errors
	assert.NotEmpty(t, errStr)
}

func TestValidateResult_NilResult(t *testing.T) {
	schema := schemaWithType("object")

	err := ValidateResult(nil, schema)
	// nil result with object expectation should fail
	require.Error(t, err)
}

func TestValidateResult_SchemaResolveError(t *testing.T) {
	// Create a schema with an invalid $ref to trigger resolve error
	schema := &jsonschema.Schema{
		Ref: "invalid://not-a-valid-ref",
	}

	err := ValidateResult(map[string]any{}, schema)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve")
}
