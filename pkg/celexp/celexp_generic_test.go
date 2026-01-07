package celexp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEvalAs_String tests generic evaluation with string type
func TestEvalAs_String(t *testing.T) {
	t.Run("successful string evaluation", func(t *testing.T) {
		expr := Expression("'hello ' + name")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("name", cel.StringType)})
		require.NoError(t, err)

		str, err := EvalAs[string](result, map[string]any{"name": "world"})
		require.NoError(t, err)
		assert.Equal(t, "hello world", str)
	})

	t.Run("string from concatenation", func(t *testing.T) {
		expr := Expression("first + ' ' + last")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("first", cel.StringType),
			cel.Variable("last", cel.StringType),
		})
		require.NoError(t, err)

		str, err := EvalAs[string](result, map[string]any{"first": "John", "last": "Doe"})
		require.NoError(t, err)
		assert.Equal(t, "John Doe", str)
	})

	t.Run("type mismatch error", func(t *testing.T) {
		expr := Expression("42")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[string](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "int64, not string")
	})
}

// TestEvalAs_Bool tests generic evaluation with bool type
func TestEvalAs_Bool(t *testing.T) {
	t.Run("successful bool evaluation", func(t *testing.T) {
		expr := Expression("x > 10")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		b, err := EvalAs[bool](result, map[string]any{"x": int64(15)})
		require.NoError(t, err)
		assert.True(t, b)
	})

	t.Run("false result", func(t *testing.T) {
		expr := Expression("x < 10")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		b, err := EvalAs[bool](result, map[string]any{"x": int64(15)})
		require.NoError(t, err)
		assert.False(t, b)
	})

	t.Run("complex boolean expression", func(t *testing.T) {
		expr := Expression("(x > 10 && y < 20) || z == 'test'")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
			cel.Variable("z", cel.StringType),
		})
		require.NoError(t, err)

		b, err := EvalAs[bool](result, map[string]any{
			"x": int64(15),
			"y": int64(5),
			"z": "other",
		})
		require.NoError(t, err)
		assert.True(t, b)
	})

	t.Run("type mismatch error", func(t *testing.T) {
		expr := Expression("'true'")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[bool](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "string, not bool")
	})
}

// TestEvalAs_Int64 tests generic evaluation with int64 type
func TestEvalAs_Int64(t *testing.T) {
	t.Run("successful int64 evaluation", func(t *testing.T) {
		expr := Expression("x + y")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		})
		require.NoError(t, err)

		num, err := EvalAs[int64](result, map[string]any{"x": int64(10), "y": int64(20)})
		require.NoError(t, err)
		assert.Equal(t, int64(30), num)
	})

	t.Run("multiplication", func(t *testing.T) {
		expr := Expression("x * 2")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		num, err := EvalAs[int64](result, map[string]any{"x": int64(21)})
		require.NoError(t, err)
		assert.Equal(t, int64(42), num)
	})

	t.Run("negative numbers", func(t *testing.T) {
		expr := Expression("-x")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		num, err := EvalAs[int64](result, map[string]any{"x": int64(42)})
		require.NoError(t, err)
		assert.Equal(t, int64(-42), num)
	})

	t.Run("type mismatch error", func(t *testing.T) {
		expr := Expression("3.14")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[int64](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "float64, not int64")
	})
}

// TestEvalAs_Float64 tests generic evaluation with float64 type
func TestEvalAs_Float64(t *testing.T) {
	t.Run("successful float64 evaluation", func(t *testing.T) {
		expr := Expression("x * 1.5")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.DoubleType)})
		require.NoError(t, err)

		num, err := EvalAs[float64](result, map[string]any{"x": 10.0})
		require.NoError(t, err)
		assert.Equal(t, 15.0, num)
	})

	t.Run("division", func(t *testing.T) {
		expr := Expression("x / 2.0")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.DoubleType)})
		require.NoError(t, err)

		num, err := EvalAs[float64](result, map[string]any{"x": 42.0})
		require.NoError(t, err)
		assert.Equal(t, 21.0, num)
	})

	t.Run("type mismatch error", func(t *testing.T) {
		expr := Expression("42")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[float64](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "int64, not float64")
	})
}

// TestEvalAs_List tests generic evaluation with []any type
func TestEvalAs_List(t *testing.T) {
	t.Run("successful list evaluation", func(t *testing.T) {
		expr := Expression("[1, 2, 3]")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		list, err := EvalAs[[]any](result, nil)
		require.NoError(t, err)
		assert.Len(t, list, 3)
		assert.Equal(t, int64(1), list[0])
		assert.Equal(t, int64(2), list[1])
		assert.Equal(t, int64(3), list[2])
	})

	t.Run("list comprehension", func(t *testing.T) {
		expr := Expression("[1, 2, 3].map(x, x * 2)")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		list, err := EvalAs[[]any](result, nil)
		require.NoError(t, err)
		assert.Len(t, list, 3)
		assert.Equal(t, int64(2), list[0])
		assert.Equal(t, int64(4), list[1])
		assert.Equal(t, int64(6), list[2])
	})

	t.Run("empty list", func(t *testing.T) {
		expr := Expression("[]")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		list, err := EvalAs[[]any](result, nil)
		require.NoError(t, err)
		assert.Empty(t, list)
	})

	t.Run("list from variable", func(t *testing.T) {
		expr := Expression("items.filter(x, x > 5)")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("items", cel.ListType(cel.IntType)),
		})
		require.NoError(t, err)

		list, err := EvalAs[[]any](result, map[string]any{
			"items": []int64{1, 3, 7, 10, 2, 8},
		})
		require.NoError(t, err)
		assert.Len(t, list, 3)
		assert.Equal(t, int64(7), list[0])
		assert.Equal(t, int64(10), list[1])
		assert.Equal(t, int64(8), list[2])
	})

	t.Run("type mismatch error", func(t *testing.T) {
		expr := Expression("'not a list'")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[[]any](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "string, not []interface")
	})
}

// TestEvalAs_Map tests generic evaluation with map[string]any type
func TestEvalAs_Map(t *testing.T) {
	t.Run("successful map evaluation", func(t *testing.T) {
		expr := Expression("{'name': 'Alice', 'age': 30}")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		m, err := EvalAs[map[string]any](result, nil)
		require.NoError(t, err)
		assert.Len(t, m, 2)
		assert.Equal(t, "Alice", m["name"])
		assert.Equal(t, int64(30), m["age"])
	})

	t.Run("map from variable", func(t *testing.T) {
		expr := Expression("user")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		})
		require.NoError(t, err)

		m, err := EvalAs[map[string]any](result, map[string]any{
			"user": map[string]any{"id": int64(1), "name": "Bob"},
		})
		require.NoError(t, err)
		assert.Len(t, m, 2)
		assert.Equal(t, int64(1), m["id"])
		assert.Equal(t, "Bob", m["name"])
	})

	t.Run("empty map", func(t *testing.T) {
		expr := Expression("{}")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		m, err := EvalAs[map[string]any](result, nil)
		require.NoError(t, err)
		assert.Empty(t, m)
	})

	t.Run("type mismatch error", func(t *testing.T) {
		expr := Expression("[1, 2, 3]")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[map[string]any](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not map[string]interface")
	})
}

// TestEvalAsWithContext tests context support in generic evaluation
func TestEvalAsWithContext(t *testing.T) {
	t.Run("successful evaluation with context", func(t *testing.T) {
		ctx := context.Background()
		expr := Expression("x + y")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		})
		require.NoError(t, err)

		num, err := EvalAsWithContext[int64](ctx, result, map[string]any{"x": int64(10), "y": int64(20)})
		require.NoError(t, err)
		assert.Equal(t, int64(30), num)
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		expr := Expression("x + y")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		})
		require.NoError(t, err)

		_, err = EvalAsWithContext[int64](ctx, result, map[string]any{"x": int64(10), "y": int64(20)})
		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	})

	t.Run("timeout context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		expr := Expression("name.startsWith('hello')")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("name", cel.StringType)})
		require.NoError(t, err)

		b, err := EvalAsWithContext[bool](ctx, result, map[string]any{"name": "hello world"})
		require.NoError(t, err)
		assert.True(t, b)
	})

	t.Run("all types with context", func(t *testing.T) {
		ctx := context.Background()

		// String
		expr := Expression("'test'")
		result, _ := expr.Compile([]cel.EnvOption{})
		str, err := EvalAsWithContext[string](ctx, result, nil)
		require.NoError(t, err)
		assert.Equal(t, "test", str)

		// Bool
		expr = Expression("true")
		result, _ = expr.Compile([]cel.EnvOption{})
		b, err := EvalAsWithContext[bool](ctx, result, nil)
		require.NoError(t, err)
		assert.True(t, b)

		// Int64
		expr = Expression("42")
		result, _ = expr.Compile([]cel.EnvOption{})
		num, err := EvalAsWithContext[int64](ctx, result, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(42), num)

		// Float64
		expr = Expression("3.14")
		result, _ = expr.Compile([]cel.EnvOption{})
		f, err := EvalAsWithContext[float64](ctx, result, nil)
		require.NoError(t, err)
		assert.Equal(t, 3.14, f)

		// List
		expr = Expression("[1, 2]")
		result, _ = expr.Compile([]cel.EnvOption{})
		list, err := EvalAsWithContext[[]any](ctx, result, nil)
		require.NoError(t, err)
		assert.Len(t, list, 2)

		// Map
		expr = Expression("{'key': 'value'}")
		result, _ = expr.Compile([]cel.EnvOption{})
		m, err := EvalAsWithContext[map[string]any](ctx, result, nil)
		require.NoError(t, err)
		assert.Len(t, m, 1)
	})
}

// TestEvalAs_NilSafety tests nil safety of generic evaluation
func TestEvalAs_NilSafety(t *testing.T) {
	t.Run("nil compile result", func(t *testing.T) {
		var result *CompileResult
		_, err := EvalAs[string](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "compile result or program is nil")
	})

	t.Run("nil program", func(t *testing.T) {
		result := &CompileResult{Program: nil}
		_, err := EvalAs[int64](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "compile result or program is nil")
	})
}

// TestEvalAs_ComplexScenarios tests complex real-world scenarios
func TestEvalAs_ComplexScenarios(t *testing.T) {
	t.Run("nested map access", func(t *testing.T) {
		expr := Expression("user.address.city")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("user", cel.MapType(cel.StringType, cel.DynType)),
		})
		require.NoError(t, err)

		str, err := EvalAs[string](result, map[string]any{
			"user": map[string]any{
				"address": map[string]any{
					"city": "Seattle",
				},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, "Seattle", str)
	})

	t.Run("list length check", func(t *testing.T) {
		expr := Expression("size(items) > 0")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("items", cel.ListType(cel.StringType)),
		})
		require.NoError(t, err)

		b, err := EvalAs[bool](result, map[string]any{
			"items": []string{"a", "b", "c"},
		})
		require.NoError(t, err)
		assert.True(t, b)
	})

	t.Run("conditional expression", func(t *testing.T) {
		expr := Expression("x > 10 ? 'large' : 'small'")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		str, err := EvalAs[string](result, map[string]any{"x": int64(15)})
		require.NoError(t, err)
		assert.Equal(t, "large", str)
	})

	t.Run("string concatenation with type conversion", func(t *testing.T) {
		expr := Expression("'Value: ' + string(num)")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("num", cel.IntType)})
		require.NoError(t, err)

		str, err := EvalAs[string](result, map[string]any{"num": int64(42)})
		require.NoError(t, err)
		assert.Equal(t, "Value: 42", str)
	})
}

// ============================================================================
// Specialized Convenience Types (int and []string)
// ============================================================================

// TestEvalAs_Int tests the int type support for convenience
func TestEvalAs_Int(t *testing.T) {
	tests := []struct {
		name       string
		expression Expression
		envOpts    []cel.EnvOption
		vars       map[string]any
		want       int
		wantErr    bool
		errContain string
	}{
		{
			name:       "successful int evaluation from literal",
			expression: Expression("42"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       42,
			wantErr:    false,
		},
		{
			name:       "int from arithmetic",
			expression: Expression("x + y"),
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			},
			vars: map[string]any{
				"x": int64(10),
				"y": int64(32),
			},
			want:    42,
			wantErr: false,
		},
		{
			name:       "int from multiplication",
			expression: Expression("x * 2"),
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.IntType),
			},
			vars: map[string]any{
				"x": int64(21),
			},
			want:    42,
			wantErr: false,
		},
		{
			name:       "negative int",
			expression: Expression("-100"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       -100,
			wantErr:    false,
		},
		{
			name:       "zero",
			expression: Expression("0"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       0,
			wantErr:    false,
		},
		{
			name:       "conditional returning int",
			expression: Expression("x > 10 ? 100 : 50"),
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.IntType),
			},
			vars: map[string]any{
				"x": int64(15),
			},
			want:    100,
			wantErr: false,
		},
		{
			name:       "type mismatch - string instead of int",
			expression: Expression("'not an int'"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       0,
			wantErr:    true,
			errContain: "expected int64 for conversion to int",
		},
		{
			name:       "type mismatch - float instead of int",
			expression: Expression("3.14"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       0,
			wantErr:    true,
			errContain: "expected int64 for conversion to int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.expression.Compile(tt.envOpts)
			require.NoError(t, err)
			require.NotNil(t, result)

			got, err := EvalAs[int](result, tt.vars)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// TestEvalAs_StringSlice tests the []string type support for convenience
func TestEvalAs_StringSlice(t *testing.T) {
	tests := []struct {
		name       string
		expression Expression
		envOpts    []cel.EnvOption
		vars       map[string]any
		want       []string
		wantErr    bool
		errContain string
	}{
		{
			name:       "successful string slice from literal",
			expression: Expression("['apple', 'banana', 'cherry']"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       []string{"apple", "banana", "cherry"},
			wantErr:    false,
		},
		{
			name:       "empty string slice",
			expression: Expression("[]"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       []string{},
			wantErr:    false,
		},
		{
			name:       "single element string slice",
			expression: Expression("['single']"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       []string{"single"},
			wantErr:    false,
		},
		{
			name:       "string slice from variable",
			expression: Expression("tags"),
			envOpts: []cel.EnvOption{
				cel.Variable("tags", cel.ListType(cel.StringType)),
			},
			vars: map[string]any{
				"tags": []string{"dev", "prod", "staging"},
			},
			want:    []string{"dev", "prod", "staging"},
			wantErr: false,
		},
		{
			name:       "string slice from concatenation",
			expression: Expression("['hello', 'world'] + ['!']"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       []string{"hello", "world", "!"},
			wantErr:    false,
		},
		{
			name:       "string slice with duplicates",
			expression: Expression("['a', 'b', 'a', 'c']"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       []string{"a", "b", "a", "c"},
			wantErr:    false,
		},
		{
			name:       "conditional string slice",
			expression: Expression("x > 0 ? ['positive'] : ['negative']"),
			envOpts: []cel.EnvOption{
				cel.Variable("x", cel.IntType),
			},
			vars: map[string]any{
				"x": int64(5),
			},
			want:    []string{"positive"},
			wantErr: false,
		},
		{
			name:       "type mismatch - mixed types in list",
			expression: Expression("[1, 2, 3]"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       nil,
			wantErr:    true,
			errContain: "not string",
		},
		{
			name:       "type mismatch - list with non-string element",
			expression: Expression("['valid', 123, 'also valid']"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       nil,
			wantErr:    true,
			errContain: "not string",
		},
		{
			name:       "type mismatch - not a list",
			expression: Expression("'not a list'"),
			envOpts:    []cel.EnvOption{},
			vars:       map[string]any{},
			want:       nil,
			wantErr:    true,
			errContain: "not a list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.expression.Compile(tt.envOpts)
			require.NoError(t, err)
			require.NotNil(t, result)

			got, err := EvalAs[[]string](result, tt.vars)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// TestEvalAsWithContext_Int tests int evaluation with context
func TestEvalAsWithContext_Int(t *testing.T) {
	t.Run("successful int evaluation with context", func(t *testing.T) {
		expr := Expression("x * 2")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
		})
		require.NoError(t, err)

		ctx := context.Background()
		got, err := EvalAsWithContext[int](ctx, result, map[string]any{"x": int64(21)})
		require.NoError(t, err)
		assert.Equal(t, 42, got)
	})

	t.Run("cancelled context with int", func(t *testing.T) {
		expr := Expression("42")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = EvalAsWithContext[int](ctx, result, map[string]any{})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("timeout context with int", func(t *testing.T) {
		expr := Expression("100")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		time.Sleep(10 * time.Millisecond) // Ensure timeout

		_, err = EvalAsWithContext[int](ctx, result, map[string]any{})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

// TestEvalAsWithContext_StringSlice tests []string evaluation with context
func TestEvalAsWithContext_StringSlice(t *testing.T) {
	t.Run("successful string slice evaluation with context", func(t *testing.T) {
		expr := Expression("['a', 'b', 'c']")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		ctx := context.Background()
		got, err := EvalAsWithContext[[]string](ctx, result, map[string]any{})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, got)
	})

	t.Run("cancelled context with string slice", func(t *testing.T) {
		expr := Expression("['x', 'y']")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = EvalAsWithContext[[]string](ctx, result, map[string]any{})
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

// TestEvalAs_IntAndStringSlice_RealWorldScenarios tests realistic use cases
func TestEvalAs_IntAndStringSlice_RealWorldScenarios(t *testing.T) {
	t.Run("config validation - port number", func(t *testing.T) {
		expr := Expression("port > 0 && port < 65536 ? port : 8080")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("port", cel.IntType),
		})
		require.NoError(t, err)

		port, err := EvalAs[int](result, map[string]any{"port": int64(3000)})
		require.NoError(t, err)
		assert.Equal(t, 3000, port)
	})

	t.Run("config - file extensions", func(t *testing.T) {
		expr := Expression("extensions.filter(e, e.startsWith('.'))")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("extensions", cel.ListType(cel.StringType)),
		})
		require.NoError(t, err)

		exts, err := EvalAs[[]string](result, map[string]any{
			"extensions": []string{".go", ".md", "txt", ".yaml"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{".go", ".md", ".yaml"}, exts)
	})

	t.Run("config - environment tags", func(t *testing.T) {
		expr := Expression("env == 'prod' ? ['production', 'critical'] : ['development']")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("env", cel.StringType),
		})
		require.NoError(t, err)

		tags, err := EvalAs[[]string](result, map[string]any{"env": "prod"})
		require.NoError(t, err)
		assert.Equal(t, []string{"production", "critical"}, tags)
	})

	t.Run("config - retry count calculation", func(t *testing.T) {
		expr := Expression("critical ? 5 : 3")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("critical", cel.BoolType),
		})
		require.NoError(t, err)

		retries, err := EvalAs[int](result, map[string]any{"critical": true})
		require.NoError(t, err)
		assert.Equal(t, 5, retries)
	})

	t.Run("scaffolding - file paths", func(t *testing.T) {
		expr := Expression("paths.map(p, p + '.template')")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("paths", cel.ListType(cel.StringType)),
		})
		require.NoError(t, err)

		templates, err := EvalAs[[]string](result, map[string]any{
			"paths": []string{"config", "deploy", "test"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"config.template", "deploy.template", "test.template"}, templates)
	})
}
