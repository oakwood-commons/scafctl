package conversion

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListToStringSlice(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    []string
		expectError bool
		errorMsg    string
	}{
		{
			name:     "empty list",
			input:    "[]",
			expected: []string{},
		},
		{
			name:     "single element",
			input:    "['hello']",
			expected: []string{"hello"},
		},
		{
			name:     "multiple elements",
			input:    "['foo', 'bar', 'baz']",
			expected: []string{"foo", "bar", "baz"},
		},
		{
			name:     "empty strings",
			input:    "['', 'hello', '']",
			expected: []string{"", "hello", ""},
		},
		{
			name:     "special characters",
			input:    "['hello-world', 'foo_bar', 'test@example.com']",
			expected: []string{"hello-world", "foo_bar", "test@example.com"},
		},
		{
			name:        "list with integers",
			input:       "[1, 2, 3]",
			expectError: true,
			errorMsg:    "list contains non-string element of type int",
		},
		{
			name:        "mixed types",
			input:       "['hello', 42, 'world']",
			expectError: true,
			errorMsg:    "list contains non-string element of type int",
		},
		{
			name:        "list with booleans",
			input:       "['true', true]",
			expectError: true,
			errorMsg:    "list contains non-string element of type bool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := cel.NewEnv()
			require.NoError(t, err)

			ast, issues := env.Compile(tt.input)
			require.Nil(t, issues)

			prg, err := env.Program(ast)
			require.NoError(t, err)

			out, _, err := prg.Eval(map[string]interface{}{})
			require.NoError(t, err)

			result, err := ListToStringSlice(out)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestListToStringSlice_NonListInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		errorMsg string
	}{
		{
			name:     "string input",
			input:    "'hello'",
			errorMsg: "expected list, got string",
		},
		{
			name:     "integer input",
			input:    "42",
			errorMsg: "expected list, got int",
		},
		{
			name:     "boolean input",
			input:    "true",
			errorMsg: "expected list, got bool",
		},
		{
			name:     "map input",
			input:    "{'key': 'value'}",
			errorMsg: "expected list, got map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := cel.NewEnv()
			require.NoError(t, err)

			ast, issues := env.Compile(tt.input)
			require.Nil(t, issues)

			prg, err := env.Program(ast)
			require.NoError(t, err)

			out, _, err := prg.Eval(map[string]interface{}{})
			require.NoError(t, err)

			result, err := ListToStringSlice(out)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.errorMsg)
			assert.Nil(t, result)
		})
	}
}

func TestListToStringSlice_DirectValues(t *testing.T) {
	t.Run("valid string list", func(t *testing.T) {
		listVal := types.DefaultTypeAdapter.NativeToValue([]string{"a", "b", "c"})
		result, err := ListToStringSlice(listVal)
		assert.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, result)
	})

	t.Run("empty list", func(t *testing.T) {
		listVal := types.DefaultTypeAdapter.NativeToValue([]string{})
		result, err := ListToStringSlice(listVal)
		assert.NoError(t, err)
		assert.Equal(t, []string{}, result)
	})

	t.Run("non-list value", func(t *testing.T) {
		val := types.String("not a list")
		result, err := ListToStringSlice(val)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected list")
		assert.Nil(t, result)
	})
}

func BenchmarkListToStringSlice(b *testing.B) {
	env, _ := cel.NewEnv()
	ast, _ := env.Compile("['foo', 'bar', 'baz', 'qux', 'quux']")
	prg, _ := env.Program(ast)
	out, _, _ := prg.Eval(map[string]interface{}{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ListToStringSlice(out)
	}
}

func TestToObject(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    map[string]any
		expectError bool
		errorMsg    string
	}{
		{
			name:     "empty map",
			input:    "{}",
			expected: map[string]any{},
		},
		{
			name:  "simple string values",
			input: "{'name': 'John', 'city': 'NYC'}",
			expected: map[string]any{
				"name": "John",
				"city": "NYC",
			},
		},
		{
			name:  "mixed value types",
			input: "{'name': 'John', 'age': 30, 'active': true}",
			expected: map[string]any{
				"name":   "John",
				"age":    int64(30),
				"active": true,
			},
		},
		{
			name:  "nested map",
			input: "{'user': {'name': 'John', 'age': 30}}",
			expected: map[string]any{
				"user": map[ref.Val]ref.Val{},
			},
		},
		{
			name:  "map with list value",
			input: "{'tags': ['go', 'cel', 'test']}",
			expected: map[string]any{
				"tags": []ref.Val{},
			},
		},
		{
			name:  "map with null value",
			input: "{'value': null}",
			expected: map[string]any{
				"value": struct{}{}, // Special marker for null - we'll verify it exists but not the exact value
			},
		},
		{
			name:  "map with empty string",
			input: "{'key': ''}",
			expected: map[string]any{
				"key": "",
			},
		},
		{
			name:  "map with zero value",
			input: "{'count': 0}",
			expected: map[string]any{
				"count": int64(0),
			},
		},
		{
			name:        "non-map input - string",
			input:       "'not a map'",
			expectError: true,
			errorMsg:    "expected map, got string",
		},
		{
			name:        "non-map input - list",
			input:       "['foo', 'bar']",
			expectError: true,
			errorMsg:    "expected map, got list",
		},
		{
			name:        "non-map input - integer",
			input:       "42",
			expectError: true,
			errorMsg:    "expected map, got int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := cel.NewEnv()
			require.NoError(t, err)

			ast, issues := env.Compile(tt.input)
			require.NoError(t, issues.Err())

			prg, err := env.Program(ast)
			require.NoError(t, err)

			out, _, err := prg.Eval(map[string]interface{}{})
			require.NoError(t, err)

			result, err := ToObject(out)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				// For simple types, compare directly
				for key, expectedVal := range tt.expected {
					actualVal, exists := result[key]
					assert.True(t, exists, "key %s should exist", key)
					// For complex types (nested maps, lists, null), just verify they exist
					switch expectedVal.(type) {
					case map[ref.Val]ref.Val, []ref.Val, struct{}:
						assert.NotNil(t, actualVal)
					default:
						assert.Equal(t, expectedVal, actualVal, "value for key %s", key)
					}
				}
			}
		})
	}

	t.Run("direct call with nil", func(t *testing.T) {
		result, err := ToObject(types.NullValue)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected map")
		assert.Nil(t, result)
	})
}

func TestListToObjectSlice(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedLen  int
		validateFunc func(t *testing.T, result []map[string]any)
		expectError  bool
		errorMsg     string
	}{
		{
			name:        "empty list",
			input:       "[]",
			expectedLen: 0,
			validateFunc: func(t *testing.T, result []map[string]any) {
				assert.Empty(t, result)
			},
		},
		{
			name:        "single map",
			input:       "[{'name': 'John', 'age': 30}]",
			expectedLen: 1,
			validateFunc: func(t *testing.T, result []map[string]any) {
				assert.Equal(t, "John", result[0]["name"])
				assert.Equal(t, int64(30), result[0]["age"])
			},
		},
		{
			name:        "multiple maps",
			input:       "[{'name': 'John', 'age': 30}, {'name': 'Jane', 'age': 25}]",
			expectedLen: 2,
			validateFunc: func(t *testing.T, result []map[string]any) {
				assert.Equal(t, "John", result[0]["name"])
				assert.Equal(t, int64(30), result[0]["age"])
				assert.Equal(t, "Jane", result[1]["name"])
				assert.Equal(t, int64(25), result[1]["age"])
			},
		},
		{
			name:        "maps with different keys",
			input:       "[{'id': 1, 'type': 'user'}, {'name': 'Product', 'price': 99.99}]",
			expectedLen: 2,
			validateFunc: func(t *testing.T, result []map[string]any) {
				assert.Equal(t, int64(1), result[0]["id"])
				assert.Equal(t, "user", result[0]["type"])
				assert.Equal(t, "Product", result[1]["name"])
				assert.Equal(t, 99.99, result[1]["price"])
			},
		},
		{
			name:        "maps with empty maps",
			input:       "[{}, {'key': 'value'}, {}]",
			expectedLen: 3,
			validateFunc: func(t *testing.T, result []map[string]any) {
				assert.Empty(t, result[0])
				assert.Equal(t, "value", result[1]["key"])
				assert.Empty(t, result[2])
			},
		},
		{
			name:        "maps with null values",
			input:       "[{'value': null}, {'value': 'test'}]",
			expectedLen: 2,
			validateFunc: func(t *testing.T, result []map[string]any) {
				// Just verify the key exists - CEL represents null differently than Go nil
				_, exists := result[0]["value"]
				assert.True(t, exists, "key 'value' should exist in first map")
				assert.Equal(t, "test", result[1]["value"])
			},
		},
		{
			name:        "list with non-map element - string",
			input:       "[{'key': 'value'}, 'not a map']",
			expectError: true,
			errorMsg:    "list contains non-map element",
		},
		{
			name:        "list with non-map element - integer",
			input:       "[{'key': 'value'}, 42]",
			expectError: true,
			errorMsg:    "list contains non-map element",
		},
		{
			name:        "list with non-map element - list",
			input:       "[{'key': 'value'}, ['nested', 'list']]",
			expectError: true,
			errorMsg:    "list contains non-map element",
		},
		{
			name:        "non-list input - map",
			input:       "{'key': 'value'}",
			expectError: true,
			errorMsg:    "expected list, got map",
		},
		{
			name:        "non-list input - string",
			input:       "'not a list'",
			expectError: true,
			errorMsg:    "expected list, got string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, err := cel.NewEnv()
			require.NoError(t, err)

			ast, issues := env.Compile(tt.input)
			require.NoError(t, issues.Err())

			prg, err := env.Program(ast)
			require.NoError(t, err)

			out, _, err := prg.Eval(map[string]interface{}{})
			require.NoError(t, err)

			result, err := ListToObjectSlice(out)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Len(t, result, tt.expectedLen)
				if tt.validateFunc != nil {
					tt.validateFunc(t, result)
				}
			}
		})
	}

	t.Run("direct call with nil", func(t *testing.T) {
		result, err := ListToObjectSlice(types.NullValue)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected list")
		assert.Nil(t, result)
	})
}

func BenchmarkToObject(b *testing.B) {
	env, _ := cel.NewEnv()
	ast, _ := env.Compile("{'name': 'John', 'age': 30, 'city': 'NYC', 'active': true}")
	prg, _ := env.Program(ast)
	out, _, _ := prg.Eval(map[string]interface{}{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ToObject(out)
	}
}

func BenchmarkListToObjectSlice(b *testing.B) {
	env, _ := cel.NewEnv()
	ast, _ := env.Compile("[{'name': 'John', 'age': 30}, {'name': 'Jane', 'age': 25}, {'name': 'Bob', 'age': 35}]")
	prg, _ := env.Program(ast)
	out, _, _ := prg.Eval(map[string]interface{}{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ListToObjectSlice(out)
	}
}
