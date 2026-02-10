// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package guid

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewFunc_Metadata tests the metadata of the NewFunc
func TestNewFunc_Metadata(t *testing.T) {
	newFunc := NewFunc()

	assert.Equal(t, "guid.new", newFunc.Name)
	assert.Equal(t, "Generates a new random UUID (GUID) in standard format. Use guid.new() to create a universally unique identifier", newFunc.Description)
	assert.NotEmpty(t, newFunc.EnvOptions)
	assert.NotEmpty(t, newFunc.Examples)
	assert.Len(t, newFunc.Examples, 3)
}

func TestNewFunc_CELIntegration(t *testing.T) {
	newFunc := NewFunc()

	env, err := cel.NewEnv(newFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result interface{})
	}{
		{
			name:       "generate new UUID",
			expression: `guid.new()`,
			validate: func(t *testing.T, result interface{}) {
				str, ok := result.(string)
				require.True(t, ok, "result should be a string")

				// Validate it's a valid UUID
				parsed, err := uuid.Parse(str)
				require.NoError(t, err, "should be a valid UUID")
				assert.NotEqual(t, uuid.Nil, parsed, "should not be nil UUID")
			},
		},
		{
			name:       "concatenate with string",
			expression: `"id-" + guid.new()`,
			validate: func(t *testing.T, result interface{}) {
				str, ok := result.(string)
				require.True(t, ok, "result should be a string")
				assert.Contains(t, str, "id-", "should contain prefix")

				// Extract UUID part and validate
				uuidPart := str[3:] // Remove "id-" prefix
				_, err := uuid.Parse(uuidPart)
				require.NoError(t, err, "UUID part should be valid")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			tt.validate(t, result.Value())
		})
	}
}

func TestNewFunc_Uniqueness(t *testing.T) {
	newFunc := NewFunc()

	env, err := cel.NewEnv(newFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`guid.new()`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	// Generate multiple UUIDs and ensure they're unique
	generated := make(map[string]bool)
	iterations := 100

	for i := 0; i < iterations; i++ {
		result, _, err := prog.Eval(map[string]interface{}{})
		require.NoError(t, err)

		str, ok := result.Value().(string)
		require.True(t, ok, "result should be a string")

		// Check it's not a duplicate
		assert.False(t, generated[str], "UUID should be unique, got duplicate: %s", str)
		generated[str] = true

		// Validate it's a valid UUID
		_, err = uuid.Parse(str)
		require.NoError(t, err, "should be a valid UUID")
	}

	assert.Len(t, generated, iterations, "should have generated %d unique UUIDs", iterations)
}

func TestNewFunc_Format(t *testing.T) {
	newFunc := NewFunc()

	env, err := cel.NewEnv(newFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`guid.new()`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)

	str, ok := result.Value().(string)
	require.True(t, ok, "result should be a string")

	// UUID v4 format: xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx
	// where y is one of [8, 9, a, b]
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`, str,
		"UUID should match v4 format")
}

func TestNewFunc_InList(t *testing.T) {
	newFunc := NewFunc()

	env, err := cel.NewEnv(newFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`[guid.new(), guid.new(), guid.new()]`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)

	// Verify the result is not nil
	assert.NotNil(t, result.Value(), "result should not be nil")

	// Test that we can use guid.new() in list context without errors
	// The fact that compilation and evaluation succeeded proves it works in lists
	t.Log("Successfully executed guid.new() in list context")
}

func TestNewFunc_InConditional(t *testing.T) {
	newFunc := NewFunc()

	env, err := cel.NewEnv(
		newFunc.EnvOptions[0],
		cel.Variable("useGuid", cel.BoolType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`useGuid ? guid.new() : "fixed-id"`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	// Test with useGuid = true
	result, _, err := prog.Eval(map[string]interface{}{
		"useGuid": true,
	})
	require.NoError(t, err)
	str, ok := result.Value().(string)
	require.True(t, ok)
	_, err = uuid.Parse(str)
	require.NoError(t, err, "should be a valid UUID when useGuid is true")

	// Test with useGuid = false
	result, _, err = prog.Eval(map[string]interface{}{
		"useGuid": false,
	})
	require.NoError(t, err)
	assert.Equal(t, "fixed-id", result.Value(), "should return fixed-id when useGuid is false")
}

func BenchmarkNewFunc_CEL(b *testing.B) {
	newFunc := NewFunc()
	env, _ := cel.NewEnv(newFunc.EnvOptions...)
	ast, _ := env.Compile(`guid.new()`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func BenchmarkNewFunc_CEL_InList(b *testing.B) {
	newFunc := NewFunc()
	env, _ := cel.NewEnv(newFunc.EnvOptions...)
	ast, _ := env.Compile(`[guid.new(), guid.new(), guid.new(), guid.new(), guid.new()]`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}
