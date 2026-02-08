// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp_test

import (
	"fmt"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ExampleTesting_tableDriver demonstrates table-driven tests for CEL expressions.
func Example_testing_tableDriven() {
	tests := []struct {
		name     string
		expr     string
		vars     map[string]any
		expected any
	}{
		{
			name: "simple_arithmetic",
			expr: "x + y",
			vars: map[string]any{
				"x": int64(5),
				"y": int64(10),
			},
			expected: int64(15),
		},
		{
			name: "string_contains",
			expr: "text.contains('world')",
			vars: map[string]any{
				"text": "hello world",
			},
			expected: true,
		},
		{
			name: "list_filtering",
			expr: "items.filter(x, x > 5).size()",
			vars: map[string]any{
				"items": []any{int64(3), int64(7), int64(9), int64(2)},
			},
			expected: int64(2),
		},
	}

	for _, tt := range tests {
		// In real tests, use t.Run() - this example just prints
		expr := celexp.Expression(tt.expr)
		compiled, _ := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
			cel.Variable("text", cel.StringType),
			cel.Variable("items", cel.ListType(cel.IntType)),
		})

		result, _ := compiled.Eval(tt.vars)
		fmt.Printf("%s: %v == %v: %v\n", tt.name, result, tt.expected, result == tt.expected)
	}

	// Output:
	// simple_arithmetic: 15 == 15: true
	// string_contains: true == true: true
	// list_filtering: 2 == 2: true
}

// TestCELExpression_Basic demonstrates basic test structure.
func TestCELExpression_Basic(t *testing.T) {
	expr := celexp.Expression("x * 2")
	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("x", cel.IntType),
	})
	require.NoError(t, err, "compilation should succeed")

	result, err := compiled.Eval(map[string]any{"x": int64(5)})
	require.NoError(t, err, "evaluation should succeed")

	assert.Equal(t, int64(10), result, "5 * 2 should equal 10")
}

// TestCELExpression_TableDriven demonstrates real table-driven tests.
func TestCELExpression_TableDriven(t *testing.T) {
	tests := []struct {
		name     string
		expr     string
		envOpts  []cel.EnvOption
		vars     map[string]any
		expected any
		wantErr  bool
	}{
		{
			name: "addition",
			expr: "a + b",
			envOpts: []cel.EnvOption{
				cel.Variable("a", cel.IntType),
				cel.Variable("b", cel.IntType),
			},
			vars:     map[string]any{"a": int64(3), "b": int64(7)},
			expected: int64(10),
			wantErr:  false,
		},
		{
			name: "string_interpolation",
			expr: "'Hello, ' + name + '!'",
			envOpts: []cel.EnvOption{
				cel.Variable("name", cel.StringType),
			},
			vars:     map[string]any{"name": "World"},
			expected: "Hello, World!",
			wantErr:  false,
		},
		{
			name: "conditional",
			expr: "age >= 18 ? 'adult' : 'minor'",
			envOpts: []cel.EnvOption{
				cel.Variable("age", cel.IntType),
			},
			vars:     map[string]any{"age": int64(25)},
			expected: "adult",
			wantErr:  false,
		},
		{
			name: "list_operations",
			expr: "items.size() > 0 && items[0] == 'first'",
			envOpts: []cel.EnvOption{
				cel.Variable("items", cel.ListType(cel.StringType)),
			},
			vars:     map[string]any{"items": []any{"first", "second"}},
			expected: true,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := celexp.Expression(tt.expr)
			compiled, err := expr.Compile(tt.envOpts)
			require.NoError(t, err, "compilation should not fail")

			result, err := compiled.Eval(tt.vars)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestCELExpression_ErrorCases demonstrates testing error scenarios.
func TestCELExpression_ErrorCases(t *testing.T) {
	t.Run("compilation_error_undefined_function", func(t *testing.T) {
		expr := celexp.Expression("undefinedFunc(x)")
		_, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undeclared reference")
	})

	t.Run("type_mismatch_at_compile", func(t *testing.T) {
		expr := celexp.Expression("x + 'string'")
		_, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
		})
		assert.Error(t, err)
	})

	t.Run("evaluation_error_division_by_zero", func(t *testing.T) {
		expr := celexp.Expression("x / y")
		compiled, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		})
		require.NoError(t, err)

		_, err = compiled.Eval(map[string]any{
			"x": int64(10),
			"y": int64(0),
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "by zero")
	})
}

// TestCELExpression_WithCache demonstrates testing with caching.
func TestCELExpression_WithCache(t *testing.T) {
	cache := celexp.NewProgramCache(10)
	expr := celexp.Expression("x * 2")
	envOpts := []cel.EnvOption{cel.Variable("x", cel.IntType)}

	// First compilation - cache miss
	compiled1, err := expr.Compile(envOpts, celexp.WithCache(cache))
	require.NoError(t, err)

	stats := cache.Stats()
	assert.Equal(t, uint64(0), stats.Hits, "first compile should be cache miss")
	assert.Equal(t, uint64(1), stats.Misses)

	// Second compilation - cache hit
	compiled2, err := expr.Compile(envOpts, celexp.WithCache(cache))
	require.NoError(t, err)

	stats = cache.Stats()
	assert.Equal(t, uint64(1), stats.Hits, "second compile should be cache hit")

	// Verify both work correctly
	result1, _ := compiled1.Eval(map[string]any{"x": int64(5)})
	result2, _ := compiled2.Eval(map[string]any{"x": int64(5)})
	assert.Equal(t, result1, result2)
}

// ExampleTesting_assertions demonstrates different assertion styles.
func Example_testing_assertions() {
	// This would be in a real test function with *testing.T
	expr := celexp.Expression("items.size()")
	compiled, _ := expr.Compile([]cel.EnvOption{
		cel.Variable("items", cel.ListType(cel.StringType)),
	})

	result, _ := compiled.Eval(map[string]any{
		"items": []any{"a", "b", "c"},
	})

	// In real tests you would use:
	// assert.Equal(t, int64(3), result)
	// require.NotNil(t, result)
	// assert.Greater(t, result.(int64), int64(0))

	fmt.Printf("Result: %v (type: %T)\n", result, result)
	fmt.Printf("List size is correct: %v\n", result == int64(3))

	// Output:
	// Result: 3 (type: int64)
	// List size is correct: true
}

// TestCELExpression_Benchmarking demonstrates benchmark structure.
func BenchmarkCELExpression_Simple(b *testing.B) {
	expr := celexp.Expression("x * y + z")
	compiled, _ := expr.Compile([]cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
		cel.Variable("z", cel.IntType),
	})

	vars := map[string]any{
		"x": int64(10),
		"y": int64(20),
		"z": int64(5),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = compiled.Eval(vars)
	}
}

// TestCELExpression_Subtests demonstrates subtest organization.
func TestCELExpression_Subtests(t *testing.T) {
	expr := celexp.Expression("user.role == expectedRole")
	compiled, err := expr.Compile([]cel.EnvOption{
		cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("expectedRole", cel.StringType),
	})
	require.NoError(t, err)

	t.Run("admin_role", func(t *testing.T) {
		result, err := compiled.Eval(map[string]any{
			"user":         map[string]any{"role": "admin"},
			"expectedRole": "admin",
		})
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("user_role", func(t *testing.T) {
		result, err := compiled.Eval(map[string]any{
			"user":         map[string]any{"role": "user"},
			"expectedRole": "user",
		})
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("role_mismatch", func(t *testing.T) {
		result, err := compiled.Eval(map[string]any{
			"user":         map[string]any{"role": "user"},
			"expectedRole": "admin",
		})
		require.NoError(t, err)
		assert.False(t, result.(bool))
	})
}
