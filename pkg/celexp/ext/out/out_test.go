// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package out

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNilFunc_Metadata(t *testing.T) {
	fn := NilFunc()
	assert.Equal(t, "out.nil", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.EnvOptions, 1)
}

func TestNilFunc_CELIntegration(t *testing.T) {
	fn := NilFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name string
		expr string
	}{
		{
			name: "nil with string",
			expr: `out.nil("some value")`,
		},
		{
			name: "nil with number",
			expr: `out.nil(42)`,
		},
		{
			name: "nil with boolean",
			expr: `out.nil(true)`,
		},
		{
			name: "nil with map",
			expr: `out.nil({"key": "value"})`,
		},
		{
			name: "nil with list",
			expr: `out.nil([1, 2, 3])`,
		},
		{
			name: "nil with null",
			expr: `out.nil(null)`,
		},
		{
			name: "nil with nested structure",
			expr: `out.nil({"user": {"name": "John", "age": 30}})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expr)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)

			// Verify the result is NullValue
			assert.Equal(t, types.NullValue, result)
		})
	}
}

func TestNilFunc_WithVariables(t *testing.T) {
	fn := NilFunc()
	env, err := cel.NewEnv(
		fn.EnvOptions[0],
		cel.Variable("myVar", cel.DynType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`out.nil(myVar)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	// Test with different variable values
	testCases := []struct {
		name  string
		value any
	}{
		{"string", "hello"},
		{"number", 123},
		{"boolean", true},
		{"map", map[string]any{"key": "value"}},
		{"list", []any{1, 2, 3}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, _, err := prog.Eval(map[string]any{
				"myVar": tc.value,
			})
			require.NoError(t, err)
			assert.Equal(t, types.NullValue, result)
		})
	}
}

func TestNilFunc_InConditional(t *testing.T) {
	fn := NilFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	// Test using out.nil in a conditional expression (both branches must return null)
	ast, issues := env.Compile(`true ? out.nil("discarded") : out.nil("also discarded")`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, types.NullValue, result)
}

func TestNilFunc_ChainedOperations(t *testing.T) {
	fn := NilFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	// Test that out.nil can be used with complex expressions
	ast, issues := env.Compile(`out.nil({"name": "John", "age": 30}.name)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, types.NullValue, result)
}

func TestNilFunc_MultipleInvocations(t *testing.T) {
	fn := NilFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	// Test multiple calls in same expression
	ast, issues := env.Compile(`out.nil(42) == out.nil("string")`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	// Both return null, so they should be equal
	assert.Equal(t, true, result.Value())
}

// Benchmark tests
func BenchmarkNilFunc_CEL_String(b *testing.B) {
	fn := NilFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`out.nil("some string value")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkNilFunc_CEL_Map(b *testing.B) {
	fn := NilFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`out.nil({"key1": "value1", "key2": "value2", "key3": "value3"})`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkNilFunc_CEL_ComplexExpression(b *testing.B) {
	fn := NilFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`out.nil({"user": {"name": "John", "roles": ["admin", "user"]}})`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}
