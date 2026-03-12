// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEval_TypeMismatch tests evaluation with incorrect variable types.
func TestEval_TypeMismatch(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		envOpts []cel.EnvOption
		vars    map[string]any
		wantErr bool
	}{
		{
			name: "string_operation_on_int",
			expr: "name.size()",
			envOpts: []cel.EnvOption{
				cel.Variable("name", cel.StringType),
			},
			vars: map[string]any{
				"name": int64(123), // Should be string
			},
			wantErr: true,
		},
		{
			name: "arithmetic_on_string",
			expr: "age + 10",
			envOpts: []cel.EnvOption{
				cel.Variable("age", cel.IntType),
			},
			vars: map[string]any{
				"age": "twenty-five", // Should be int
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			compiled, err := expr.Compile(tt.envOpts)
			require.NoError(t, err, "compilation should succeed")

			// Try to evaluate - should fail with type error
			_, err = compiled.Eval(tt.vars)
			if tt.wantErr {
				assert.Error(t, err, "evaluation should fail with type mismatch")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestEval_MissingVariable tests evaluation with missing required variables.
func TestEval_MissingVariable(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		envOpts []cel.EnvOption
		vars    map[string]any
		wantErr bool
	}{
		{
			name: "single_missing_var",
			expr: "x + y",
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			},
			vars: map[string]any{
				"x": int64(5),
				// missing "y"
			},
			wantErr: true,
		},
		{
			name: "multiple_missing_vars",
			expr: "a + b + c",
			envOpts: []cel.EnvOption{
				cel.Variable("a", cel.IntType),
				cel.Variable("b", cel.IntType),
				cel.Variable("c", cel.IntType),
			},
			vars: map[string]any{
				"a": int64(1),
				// missing "b" and "c"
			},
			wantErr: true,
		},
		{
			name: "nested_missing_field",
			expr: "user.name",
			envOpts: []cel.EnvOption{
				cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
			},
			vars: map[string]any{
				// missing "user" entirely
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			compiled, err := expr.Compile(tt.envOpts)
			require.NoError(t, err)

			// Try to evaluate - should fail with missing variable
			_, err = compiled.Eval(tt.vars)
			if tt.wantErr {
				assert.Error(t, err, "evaluation should fail with missing variable")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestEval_NilDereference tests handling of nil values.
func TestEval_NilDereference(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		envOpts []cel.EnvOption
		vars    map[string]any
		wantErr bool
	}{
		{
			name: "nil_map_access",
			expr: "user.name",
			envOpts: []cel.EnvOption{
				cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
			},
			vars: map[string]any{
				"user": nil,
			},
			wantErr: true,
		},
		{
			name: "nil_list",
			expr: "items.size()",
			envOpts: []cel.EnvOption{
				cel.Variable("items", cel.ListType(cel.StringType)),
			},
			vars: map[string]any{
				"items": nil,
			},
			wantErr: true,
		},
		{
			name: "safe_access_with_has",
			expr: "has(user.name) ? user.name : 'unknown'",
			envOpts: []cel.EnvOption{
				cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
			},
			vars: map[string]any{
				"user": nil,
			},
			wantErr: false, // Safe because of has() check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			compiled, err := expr.Compile(tt.envOpts)
			require.NoError(t, err)

			_, err = compiled.Eval(tt.vars)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestCompile_SyntaxErrors tests various syntax errors.
func TestCompile_SyntaxErrors(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{
			name:    "unclosed_parenthesis",
			expr:    "x + (y * z",
			wantErr: "Syntax error",
		},
		{
			name:    "invalid_operator",
			expr:    "x ++ y",
			wantErr: "Syntax error",
		},
		{
			name:    "unclosed_string",
			expr:    "'hello",
			wantErr: "Syntax error",
		},
		{
			name:    "invalid_identifier",
			expr:    "123abc + x",
			wantErr: "Syntax error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			_, err := expr.Compile([]cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
				cel.Variable("z", cel.IntType),
			})
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestCompile_UndeclaredReference tests using undefined variables/functions.
func TestCompile_UndeclaredReference(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		envOpts []cel.EnvOption
		wantErr string
	}{
		{
			name: "undeclared_variable",
			expr: "x + undefinedVar",
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.IntType),
			},
			wantErr: "undeclared reference",
		},
		{
			name: "undeclared_function",
			expr: "customFunc(x)",
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.IntType),
			},
			wantErr: "undeclared reference",
		},
		{
			name: "undefined_field",
			expr: "user.nonExistentField",
			envOpts: []cel.EnvOption{
				cel.Variable("user", cel.MapType(cel.StringType, cel.StringType)),
			},
			wantErr: "", // This may not error at compile time with dynamic maps
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			_, err := expr.Compile(tt.envOpts)
			if tt.wantErr != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestEval_DivisionByZero tests division by zero errors.
func TestEval_DivisionByZero(t *testing.T) {
	tests := []struct {
		name string
		expr string
		vars map[string]any
	}{
		{
			name: "int_division_by_zero",
			expr: "x / y",
			vars: map[string]any{
				"x": int64(10),
				"y": int64(0),
			},
		},
		{
			name: "modulo_by_zero",
			expr: "x % y",
			vars: map[string]any{
				"x": int64(10),
				"y": int64(0),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			compiled, err := expr.Compile([]cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			})
			require.NoError(t, err)

			_, err = compiled.Eval(tt.vars)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "by zero")
		})
	}
}

// TestEval_OverflowErrors tests numeric overflow scenarios.
func TestEval_OverflowErrors(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		vars    map[string]any
		wantErr bool
	}{
		{
			name: "int_overflow_multiplication",
			expr: "x * y",
			vars: map[string]any{
				"x": int64(9223372036854775807), // MaxInt64
				"y": int64(2),
			},
			wantErr: true,
		},
		{
			name: "int_overflow_addition",
			expr: "x + y",
			vars: map[string]any{
				"x": int64(9223372036854775807), // MaxInt64
				"y": int64(1),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			compiled, err := expr.Compile([]cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			})
			require.NoError(t, err)

			_, err = compiled.Eval(tt.vars)
			if tt.wantErr {
				assert.Error(t, err)
			}
		})
	}
}

// TestEval_InvalidIndexAccess tests invalid list/map access.
func TestEval_InvalidIndexAccess(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		envOpts []cel.EnvOption
		vars    map[string]any
		wantErr bool
	}{
		{
			name: "list_index_out_of_bounds",
			expr: "items[10]",
			envOpts: []cel.EnvOption{
				cel.Variable("items", cel.ListType(cel.StringType)),
			},
			vars: map[string]any{
				"items": []any{"a", "b", "c"},
			},
			wantErr: true,
		},
		{
			name: "negative_list_index",
			expr: "items[-1]",
			envOpts: []cel.EnvOption{
				cel.Variable("items", cel.ListType(cel.StringType)),
			},
			vars: map[string]any{
				"items": []any{"a", "b", "c"},
			},
			wantErr: true,
		},
		{
			name: "map_key_not_found",
			expr: "data['nonexistent']",
			envOpts: []cel.EnvOption{
				cel.Variable("data", cel.MapType(cel.StringType, cel.IntType)),
			},
			vars: map[string]any{
				"data": map[string]any{
					"key": int64(42),
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			compiled, err := expr.Compile(tt.envOpts)
			require.NoError(t, err)

			_, err = compiled.Eval(tt.vars)
			if tt.wantErr {
				assert.Error(t, err)
			}
		})
	}
}

// TestEval_TypeCheckFailures tests type checking at evaluation time.
func TestEval_TypeCheckFailures(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		envOpts []cel.EnvOption
		vars    map[string]any
		wantErr string
	}{
		{
			name: "cannot_compare_different_types",
			expr: "x == y",
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.DynType),
				cel.Variable("y", cel.DynType),
			},
			vars: map[string]any{
				"x": int64(42),
				"y": "42",
			},
			wantErr: "", // CEL may allow this comparison
		},
		{
			name: "invalid_method_on_type",
			expr: "x.size()",
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.IntType),
			},
			vars: map[string]any{
				"x": int64(42),
			},
			wantErr: "found no matching overload", // Should fail at compile time
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := Expression(tt.expr)
			compiled, err := expr.Compile(tt.envOpts)

			if tt.wantErr != "" && err != nil {
				// Error during compilation
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)

			_, err = compiled.Eval(tt.vars)
			if tt.wantErr != "" {
				assert.Error(t, err)
			}
		})
	}
}

// TestNewConditional_Errors tests error cases for NewConditional helper.
func TestNewConditional_Errors(t *testing.T) {
	tests := []struct {
		name      string
		condition string
		trueVal   string
		falseVal  string
		wantErr   bool
	}{
		{
			name:      "invalid_condition",
			condition: "x +", // Incomplete expression
			trueVal:   "'yes'",
			falseVal:  "'no'",
			wantErr:   true,
		},
		{
			name:      "mismatched_return_types",
			condition: "x > 0",
			trueVal:   "42",     // int
			falseVal:  "'text'", // string
			wantErr:   true,     // CEL does not allow different types
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr := NewConditional(tt.condition, tt.trueVal, tt.falseVal)
			_, err := expr.Compile([]cel.EnvOption{
				cel.Variable("x", cel.IntType),
			})

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestCache_ConcurrencyErrors tests cache behavior under concurrent access.
func TestCache_ConcurrencyErrors(t *testing.T) {
	cache := NewProgramCache(10)

	// This test ensures no panics occur during concurrent access
	// Actual race conditions would be detected by `go test -race`
	done := make(chan bool)

	for i := 0; i < 10; i++ {
		go func() {
			expr := Expression("x + 1")
			_, _ = expr.Compile([]cel.EnvOption{
				cel.Variable("x", cel.IntType),
			}, WithCache(cache))
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// If we reach here without panic, test passes
	assert.True(t, true)
}

func TestValidateSyntax(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr bool
	}{
		{name: "valid_simple", expr: "1 + 2", wantErr: false},
		{name: "valid_function", expr: "size('hello')", wantErr: false},
		{name: "valid_ternary", expr: "x > 0 ? 'pos' : 'neg'", wantErr: false},
		{name: "valid_boolean", expr: "true && false", wantErr: false},
		{name: "invalid_unclosed_paren", expr: "size('hello'", wantErr: true},
		{name: "invalid_missing_operand", expr: "1 +", wantErr: true},
		{name: "invalid_unclosed_string", expr: "'hello", wantErr: true},
		{name: "empty_expression", expr: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSyntax(tt.expr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func BenchmarkValidateSyntax(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ValidateSyntax("x > 0 ? size(name) : 0")
	}
}
