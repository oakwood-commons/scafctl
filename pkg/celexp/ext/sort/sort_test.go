// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celsort

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestObjectsFunc_Metadata(t *testing.T) {
	fn := ObjectsFunc()
	assert.Equal(t, "sort.objects", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.EnvOptions, 1)
}

func TestObjectsFunc_CELIntegration(t *testing.T) {
	fn := ObjectsFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		expr     string
		expected []map[string]any
	}{
		{
			name: "sort by string property",
			expr: `sort.objects([{"name": "Charlie"}, {"name": "Alice"}, {"name": "Bob"}], "name")`,
			expected: []map[string]any{
				{"name": "Alice"},
				{"name": "Bob"},
				{"name": "Charlie"},
			},
		},
		{
			name: "sort by numeric property",
			expr: `sort.objects([{"id": 3}, {"id": 1}, {"id": 2}], "id")`,
			expected: []map[string]any{
				{"id": int64(1)},
				{"id": int64(2)},
				{"id": int64(3)},
			},
		},
		{
			name: "sort with multiple properties",
			expr: `sort.objects([{"name": "Charlie", "age": 30}, {"name": "Alice", "age": 25}, {"name": "Bob", "age": 35}], "name")`,
			expected: []map[string]any{
				{"name": "Alice", "age": int64(25)},
				{"name": "Bob", "age": int64(35)},
				{"name": "Charlie", "age": int64(30)},
			},
		},
		{
			name:     "sort empty list",
			expr:     `sort.objects([], "name")`,
			expected: []map[string]any{},
		},
		{
			name: "sort single item",
			expr: `sort.objects([{"name": "Alice"}], "name")`,
			expected: []map[string]any{
				{"name": "Alice"},
			},
		},
		{
			name: "sort with missing property",
			expr: `sort.objects([{"name": "Bob"}, {"name": "Alice", "priority": 1}, {"name": "Charlie"}], "priority")`,
			expected: []map[string]any{
				{"name": "Alice", "priority": int64(1)},
				{"name": "Bob"},
				{"name": "Charlie"},
			},
		},
		{
			name: "sort by boolean property",
			expr: `sort.objects([{"name": "A", "active": true}, {"name": "B", "active": false}, {"name": "C", "active": true}], "active")`,
			expected: []map[string]any{
				{"name": "B", "active": false},
				{"name": "A", "active": true},
				{"name": "C", "active": true},
			},
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

			assert.Equal(t, tt.expected, result.Value())
		})
	}
}

func TestObjectsDescendingFunc_Metadata(t *testing.T) {
	fn := ObjectsDescendingFunc()
	assert.Equal(t, "sort.objectsDescending", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.EnvOptions, 1)
}

func TestObjectsDescendingFunc_CELIntegration(t *testing.T) {
	fn := ObjectsDescendingFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		expr     string
		expected []map[string]any
	}{
		{
			name: "sort by string property descending",
			expr: `sort.objectsDescending([{"name": "Alice"}, {"name": "Charlie"}, {"name": "Bob"}], "name")`,
			expected: []map[string]any{
				{"name": "Charlie"},
				{"name": "Bob"},
				{"name": "Alice"},
			},
		},
		{
			name: "sort by numeric property descending",
			expr: `sort.objectsDescending([{"id": 1}, {"id": 3}, {"id": 2}], "id")`,
			expected: []map[string]any{
				{"id": int64(3)},
				{"id": int64(2)},
				{"id": int64(1)},
			},
		},
		{
			name: "sort with multiple properties descending",
			expr: `sort.objectsDescending([{"name": "Alice", "age": 25}, {"name": "Charlie", "age": 30}, {"name": "Bob", "age": 35}], "age")`,
			expected: []map[string]any{
				{"name": "Bob", "age": int64(35)},
				{"name": "Charlie", "age": int64(30)},
				{"name": "Alice", "age": int64(25)},
			},
		},
		{
			name:     "sort empty list descending",
			expr:     `sort.objectsDescending([], "name")`,
			expected: []map[string]any{},
		},
		{
			name: "sort single item descending",
			expr: `sort.objectsDescending([{"name": "Alice"}], "name")`,
			expected: []map[string]any{
				{"name": "Alice"},
			},
		},
		{
			name: "sort with missing property descending",
			expr: `sort.objectsDescending([{"name": "Bob"}, {"name": "Alice", "priority": 5}, {"name": "Charlie", "priority": 3}], "priority")`,
			expected: []map[string]any{
				{"name": "Alice", "priority": int64(5)},
				{"name": "Charlie", "priority": int64(3)},
				{"name": "Bob"},
			},
		},
		{
			name: "sort by boolean property descending",
			expr: `sort.objectsDescending([{"name": "A", "active": false}, {"name": "B", "active": true}, {"name": "C", "active": false}], "active")`,
			expected: []map[string]any{
				{"name": "B", "active": true},
				{"name": "A", "active": false},
				{"name": "C", "active": false},
			},
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

			assert.Equal(t, tt.expected, result.Value())
		})
	}
}

func TestObjectsFunc_WithVariables(t *testing.T) {
	fn := ObjectsFunc()
	env, err := cel.NewEnv(
		fn.EnvOptions[0],
		cel.Variable("objects", cel.ListType(cel.MapType(cel.StringType, cel.DynType))),
		cel.Variable("property", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`sort.objects(objects, property)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{
		"objects": []map[string]any{
			{"name": "Charlie", "age": 30},
			{"name": "Alice", "age": 25},
			{"name": "Bob", "age": 35},
		},
		"property": "age",
	})
	require.NoError(t, err)

	expected := []map[string]any{
		{"name": "Alice", "age": int64(25)},
		{"name": "Charlie", "age": int64(30)},
		{"name": "Bob", "age": int64(35)},
	}
	assert.Equal(t, expected, result.Value())
}

func TestObjectsFunc_StableSort(t *testing.T) {
	fn := ObjectsFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	// Test that stable sort preserves order for equal elements
	ast, issues := env.Compile(`sort.objects([
		{"group": "A", "order": 1},
		{"group": "A", "order": 2},
		{"group": "B", "order": 3},
		{"group": "A", "order": 4}
	], "group")`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	expected := []map[string]any{
		{"group": "A", "order": int64(1)},
		{"group": "A", "order": int64(2)},
		{"group": "A", "order": int64(4)},
		{"group": "B", "order": int64(3)},
	}
	assert.Equal(t, expected, result.Value())
}

func TestObjectsFunc_MixedNumericTypes(t *testing.T) {
	fn := ObjectsFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`sort.objects([
		{"value": 3.5},
		{"value": 1},
		{"value": 2.7}
	], "value")`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	resultList := result.Value().([]map[string]any)
	require.Len(t, resultList, 3)

	// Check that values are in ascending order
	val0 := toFloat64Value(resultList[0]["value"])
	val1 := toFloat64Value(resultList[1]["value"])
	val2 := toFloat64Value(resultList[2]["value"])

	assert.True(t, val0 < val1)
	assert.True(t, val1 < val2)
}

func TestObjectsFunc_ErrorCases(t *testing.T) {
	fn := ObjectsFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name        string
		expr        string
		expectError bool
	}{
		{
			name:        "non-string property name",
			expr:        `sort.objects([{"name": "Alice"}], 123)`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expr)
			if issues.Err() != nil {
				if tt.expectError {
					return
				}
				t.Fatalf("compile error: %v", issues.Err())
			}

			prog, err := env.Program(ast)
			require.NoError(t, err)

			_, _, err = prog.Eval(map[string]any{})
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Benchmark tests
func BenchmarkObjectsFunc_CEL_Small(b *testing.B) {
	fn := ObjectsFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`sort.objects([{"name": "Charlie"}, {"name": "Alice"}, {"name": "Bob"}], "name")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkObjectsFunc_CEL_Large(b *testing.B) {
	fn := ObjectsFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`sort.objects([
		{"id": 10}, {"id": 5}, {"id": 8}, {"id": 3}, {"id": 7},
		{"id": 2}, {"id": 9}, {"id": 1}, {"id": 6}, {"id": 4}
	], "id")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkObjectsDescendingFunc_CEL_Small(b *testing.B) {
	fn := ObjectsDescendingFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`sort.objectsDescending([{"name": "Alice"}, {"name": "Charlie"}, {"name": "Bob"}], "name")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

// Helper function for tests
func toFloat64Value(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}
