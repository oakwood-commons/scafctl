package celmap

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAddFunc_Metadata tests the metadata of the AddFunc
func TestAddFunc_Metadata(t *testing.T) {
	addFunc := AddFunc()

	assert.Equal(t, "map.add", addFunc.Name)
	assert.Equal(t, "Adds a key-value pair to a map and returns a new map with the added entry. The original map is not modified. Use map.add(map, key, value) to add an entry to a map", addFunc.Description)
	assert.NotEmpty(t, addFunc.EnvOptions)
	assert.NotEmpty(t, addFunc.Examples)
	assert.Len(t, addFunc.Examples, 3)
}

func TestAddFunc_CELIntegration(t *testing.T) {
	addFunc := AddFunc()

	env, err := cel.NewEnv(addFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "add string value to empty map",
			expression: `map.add({}, "name", "John")`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok, "result should be a map")
				assert.Len(t, m, 1)
				assert.Equal(t, "John", m["name"])
			},
		},
		{
			name:       "add string value to existing map",
			expression: `map.add({"name": "John"}, "age", "30")`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok, "result should be a map")
				assert.Len(t, m, 2)
				assert.Equal(t, "John", m["name"])
				assert.Equal(t, "30", m["age"])
			},
		},
		{
			name:       "add number value to map",
			expression: `map.add({"x": 10}, "y", 20)`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok, "result should be a map")
				assert.Len(t, m, 2)
				assert.Equal(t, int64(10), m["x"])
				assert.Equal(t, int64(20), m["y"])
			},
		},
		{
			name:       "add boolean value to map",
			expression: `map.add({"name": "test"}, "active", true)`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok, "result should be a map")
				assert.Len(t, m, 2)
				assert.Equal(t, "test", m["name"])
				assert.Equal(t, true, m["active"])
			},
		},
		{
			name:       "overwrite existing key",
			expression: `map.add({"name": "John"}, "name", "Jane")`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok, "result should be a map")
				assert.Len(t, m, 1)
				assert.Equal(t, "Jane", m["name"])
			},
		},
		{
			name:       "add list value to map",
			expression: `map.add({"name": "John"}, "hobbies", ["reading", "coding"])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok, "result should be a map")
				assert.Len(t, m, 2)
				assert.Equal(t, "John", m["name"])
				// Lists in CEL maps are stored as []ref.Val
				hobbies, ok := m["hobbies"].([]ref.Val)
				require.True(t, ok, "hobbies should be a ref.Val list")
				require.Len(t, hobbies, 2)
				assert.Equal(t, "reading", hobbies[0].Value())
				assert.Equal(t, "coding", hobbies[1].Value())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)
			tt.validate(t, result.Value())
		})
	}
}

func TestAddFunc_ChainedOperations(t *testing.T) {
	addFunc := AddFunc()

	env, err := cel.NewEnv(addFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`map.add(map.add(map.add({}, "a", 1), "b", 2), "c", 3)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	m, ok := result.Value().(map[string]any)
	require.True(t, ok, "result should be a map")
	assert.Len(t, m, 3)
	assert.Equal(t, int64(1), m["a"])
	assert.Equal(t, int64(2), m["b"])
	assert.Equal(t, int64(3), m["c"])
}

func TestAddFunc_WithVariables(t *testing.T) {
	addFunc := AddFunc()

	env, err := cel.NewEnv(
		addFunc.EnvOptions[0],
		cel.Variable("baseMap", cel.MapType(cel.StringType, cel.DynType)),
		cel.Variable("key", cel.StringType),
		cel.Variable("value", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`map.add(baseMap, key, value)`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{
		"baseMap": map[string]any{"existing": "value"},
		"key":     "newKey",
		"value":   "newValue",
	})
	require.NoError(t, err)

	m, ok := result.Value().(map[string]any)
	require.True(t, ok, "result should be a map")
	assert.Len(t, m, 2)
	assert.Equal(t, "value", m["existing"])
	assert.Equal(t, "newValue", m["newKey"])
}

func TestAddFunc_TypeError(t *testing.T) {
	addFunc := AddFunc()

	env, err := cel.NewEnv(
		addFunc.EnvOptions[0],
		cel.Variable("numKey", cel.IntType),
	)
	require.NoError(t, err)

	// CEL will catch type errors at compile time for invalid key type
	_, issues := env.Compile(`map.add({}, numKey, "value")`)
	require.Error(t, issues.Err())
	assert.Contains(t, issues.Err().Error(), "found no matching overload")
}

func TestAddFunc_OriginalMapUnmodified(t *testing.T) {
	addFunc := AddFunc()

	env, err := cel.NewEnv(
		addFunc.EnvOptions[0],
		cel.Variable("original", cel.MapType(cel.StringType, cel.DynType)),
	)
	require.NoError(t, err)

	// First, add to the map
	ast1, issues := env.Compile(`map.add(original, "new", "value")`)
	require.NoError(t, issues.Err())

	prog1, err := env.Program(ast1)
	require.NoError(t, err)

	originalMap := map[string]any{"existing": "value"}
	result1, _, err := prog1.Eval(map[string]any{
		"original": originalMap,
	})
	require.NoError(t, err)

	// Verify the result has the new key
	m1, ok := result1.Value().(map[string]any)
	require.True(t, ok)
	assert.Len(t, m1, 2)
	assert.Equal(t, "value", m1["new"])

	// Verify the original map still has only one entry
	// Note: In CEL, maps passed as variables might be copied,
	// so we're testing that the function returns a new map
	ast2, issues := env.Compile(`original`)
	require.NoError(t, issues.Err())

	prog2, err := env.Program(ast2)
	require.NoError(t, err)

	result2, _, err := prog2.Eval(map[string]any{
		"original": originalMap,
	})
	require.NoError(t, err)

	m2, ok := result2.Value().(map[string]any)
	require.True(t, ok)
	// The original should still have only 1 key
	assert.Len(t, m2, 1)
	assert.Equal(t, "value", m2["existing"])
}

func TestAddFunc_ComplexValues(t *testing.T) {
	addFunc := AddFunc()

	env, err := cel.NewEnv(addFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "add map value",
			expression: `map.add({"user": "John"}, "address", {"city": "NYC", "zip": "10001"})`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, "John", m["user"])
				// Nested maps in CEL are stored as map[ref.Val]ref.Val
				address, ok := m["address"].(map[ref.Val]ref.Val)
				require.True(t, ok, "address should be a ref.Val map")
				// We need to iterate and convert keys to find our values
				var cityVal, zipVal ref.Val
				for k, v := range address {
					switch k.Value().(string) {
					case "city":
						cityVal = v
					case "zip":
						zipVal = v
					}
				}
				require.NotNil(t, cityVal, "city should exist")
				require.NotNil(t, zipVal, "zip should exist")
				assert.Equal(t, "NYC", cityVal.Value())
				assert.Equal(t, "10001", zipVal.Value())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)
			tt.validate(t, result.Value())
		})
	}
}

func BenchmarkAddFunc_CEL(b *testing.B) {
	addFunc := AddFunc()
	env, _ := cel.NewEnv(addFunc.EnvOptions...)
	ast, _ := env.Compile(`map.add({"name": "John"}, "age", "30")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkAddFunc_CEL_Chained(b *testing.B) {
	addFunc := AddFunc()
	env, _ := cel.NewEnv(addFunc.EnvOptions...)
	ast, _ := env.Compile(`map.add(map.add(map.add({}, "a", 1), "b", 2), "c", 3)`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

// AddFailIfExistsFunc Tests

func TestAddFailIfExistsFunc_Metadata(t *testing.T) {
	fn := AddFailIfExistsFunc()

	assert.Equal(t, "map.addFailIfExists", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.EnvOptions)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.Examples, 2)
}

func TestAddFailIfExistsFunc_CELIntegration(t *testing.T) {
	fn := AddFailIfExistsFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name        string
		expression  string
		expectError bool
		validate    func(t *testing.T, result any)
	}{
		{
			name:       "add new key succeeds",
			expression: `map.addFailIfExists({"name": "John"}, "age", 30)`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, "John", m["name"])
				assert.Equal(t, int64(30), m["age"])
			},
		},
		{
			name:        "add existing key fails",
			expression:  `map.addFailIfExists({"name": "John"}, "name", "Jane")`,
			expectError: true,
		},
		{
			name:       "add to empty map",
			expression: `map.addFailIfExists({}, "key", "value")`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 1)
				assert.Equal(t, "value", m["key"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "already exists")
			} else {
				require.NoError(t, err)
				tt.validate(t, result.Value())
			}
		})
	}
}

// AddIfMissingFunc Tests

func TestAddIfMissingFunc_Metadata(t *testing.T) {
	fn := AddIfMissingFunc()

	assert.Equal(t, "map.addIfMissing", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.EnvOptions)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.Examples, 3)
}

func TestAddIfMissingFunc_CELIntegration(t *testing.T) {
	fn := AddIfMissingFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "add new key",
			expression: `map.addIfMissing({"name": "John"}, "age", 30)`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, "John", m["name"])
				assert.Equal(t, int64(30), m["age"])
			},
		},
		{
			name:       "existing key not overwritten",
			expression: `map.addIfMissing({"name": "John"}, "name", "Jane")`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 1)
				assert.Equal(t, "John", m["name"]) // Original value preserved
			},
		},
		{
			name:       "chain operations for defaults",
			expression: `map.addIfMissing(map.addIfMissing({"name": "John"}, "name", "Default"), "age", 25)`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, "John", m["name"]) // Not overwritten
				assert.Equal(t, int64(25), m["age"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)
			tt.validate(t, result.Value())
		})
	}
}

// SelectFunc Tests

func TestSelectFunc_Metadata(t *testing.T) {
	fn := SelectFunc()

	assert.Equal(t, "map.select", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.EnvOptions)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.Examples, 3)
}

func TestSelectFunc_CELIntegration(t *testing.T) {
	fn := SelectFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "select specific keys",
			expression: `map.select({"name": "John", "age": 30, "city": "NYC"}, ["name", "city"])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, "John", m["name"])
				assert.Equal(t, "NYC", m["city"])
				assert.NotContains(t, m, "age")
			},
		},
		{
			name:       "select with non-existent keys",
			expression: `map.select({"name": "John", "age": 30}, ["name", "country"])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 1)
				assert.Equal(t, "John", m["name"])
				assert.NotContains(t, m, "country")
			},
		},
		{
			name:       "select all keys",
			expression: `map.select({"a": 1, "b": 2}, ["a", "b"])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, int64(1), m["a"])
				assert.Equal(t, int64(2), m["b"])
			},
		},
		{
			name:       "select empty list returns empty map",
			expression: `map.select({"a": 1, "b": 2}, [])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Empty(t, m)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)
			tt.validate(t, result.Value())
		})
	}
}

// OmitFunc Tests

func TestOmitFunc_Metadata(t *testing.T) {
	fn := OmitFunc()

	assert.Equal(t, "map.omit", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.EnvOptions)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.Examples, 3)
}

func TestOmitFunc_CELIntegration(t *testing.T) {
	fn := OmitFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "omit single key",
			expression: `map.omit({"name": "John", "age": 30, "city": "NYC"}, ["age"])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, "John", m["name"])
				assert.Equal(t, "NYC", m["city"])
				assert.NotContains(t, m, "age")
			},
		},
		{
			name:       "omit multiple keys",
			expression: `map.omit({"a": 1, "b": 2, "c": 3, "d": 4}, ["b", "d"])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, int64(1), m["a"])
				assert.Equal(t, int64(3), m["c"])
				assert.NotContains(t, m, "b")
				assert.NotContains(t, m, "d")
			},
		},
		{
			name:       "omit non-existent keys",
			expression: `map.omit({"name": "John"}, ["age", "city"])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 1)
				assert.Equal(t, "John", m["name"])
			},
		},
		{
			name:       "omit empty list returns same map",
			expression: `map.omit({"a": 1, "b": 2}, [])`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 2)
				assert.Equal(t, int64(1), m["a"])
				assert.Equal(t, int64(2), m["b"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)
			tt.validate(t, result.Value())
		})
	}
}

// MergeFunc Tests

func TestMergeFunc_Metadata(t *testing.T) {
	fn := MergeFunc()

	assert.Equal(t, "map.merge", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.EnvOptions)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.Examples, 3)
}

func TestMergeFunc_CELIntegration(t *testing.T) {
	fn := MergeFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name:       "merge non-overlapping maps",
			expression: `map.merge({"name": "John", "age": 30}, {"city": "NYC", "country": "USA"})`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 4)
				assert.Equal(t, "John", m["name"])
				assert.Equal(t, int64(30), m["age"])
				assert.Equal(t, "NYC", m["city"])
				assert.Equal(t, "USA", m["country"])
			},
		},
		{
			name:       "second map overwrites conflicts",
			expression: `map.merge({"name": "John", "age": 30}, {"age": 31, "city": "NYC"})`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 3)
				assert.Equal(t, "John", m["name"])
				assert.Equal(t, int64(31), m["age"]) // Overwritten
				assert.Equal(t, "NYC", m["city"])
			},
		},
		{
			name:       "merge with empty map",
			expression: `map.merge({"name": "John"}, {})`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 1)
				assert.Equal(t, "John", m["name"])
			},
		},
		{
			name:       "chain multiple merges",
			expression: `map.merge(map.merge({"a": 1}, {"b": 2}), {"c": 3})`,
			validate: func(t *testing.T, result any) {
				m, ok := result.(map[string]any)
				require.True(t, ok)
				assert.Len(t, m, 3)
				assert.Equal(t, int64(1), m["a"])
				assert.Equal(t, int64(2), m["b"])
				assert.Equal(t, int64(3), m["c"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)
			tt.validate(t, result.Value())
		})
	}
}

// Benchmarks for new functions

func BenchmarkAddFailIfExistsFunc_CEL(b *testing.B) {
	fn := AddFailIfExistsFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`map.addFailIfExists({"name": "John"}, "age", 30)`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkAddIfMissingFunc_CEL(b *testing.B) {
	fn := AddIfMissingFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`map.addIfMissing({"name": "John"}, "age", 30)`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkSelectFunc_CEL(b *testing.B) {
	fn := SelectFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`map.select({"name": "John", "age": 30, "city": "NYC"}, ["name", "city"])`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkOmitFunc_CEL(b *testing.B) {
	fn := OmitFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`map.omit({"a": 1, "b": 2, "c": 3, "d": 4}, ["b", "d"])`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkMergeFunc_CEL(b *testing.B) {
	fn := MergeFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`map.merge({"name": "John", "age": 30}, {"city": "NYC", "country": "USA"})`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

// RecurseFunc Tests

func TestRecurseFunc_Metadata(t *testing.T) {
	fn := RecurseFunc()

	assert.Equal(t, "map.recurse", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.EnvOptions)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.Examples, 3)
}

func TestRecurseFunc_CELIntegration(t *testing.T) {
	fn := RecurseFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		validate   func(t *testing.T, result any)
	}{
		{
			name: "simple linear dependency chain",
			expression: `map.recurse(
				[{"id": "a", "deps": ["b"]}, {"id": "b", "deps": ["c"]}, {"id": "c", "deps": []}],
				["a"],
				"id",
				"deps"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok, "result should be a list of maps")
				assert.Len(t, list, 3, "should include a, b, and c")

				// Verify all three objects are present
				ids := make(map[string]bool)
				for _, obj := range list {
					ids[obj["id"].(string)] = true
				}
				assert.True(t, ids["a"])
				assert.True(t, ids["b"])
				assert.True(t, ids["c"])
			},
		},
		{
			name: "multiple start objects",
			expression: `map.recurse(
				[{"id": "a", "deps": ["b"]}, {"id": "b", "deps": []}, {"id": "c", "deps": ["d"]}, {"id": "d", "deps": []}],
				["a", "c"],
				"id",
				"deps"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok)
				assert.Len(t, list, 4, "should include a, b, c, and d")
			},
		},
		{
			name: "no dependencies",
			expression: `map.recurse(
				[{"id": "a", "deps": []}],
				["a"],
				"id",
				"deps"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok)
				assert.Len(t, list, 1)
				assert.Equal(t, "a", list[0]["id"])
			},
		},
		{
			name: "circular dependencies",
			expression: `map.recurse(
				[{"id": "a", "deps": ["b"]}, {"id": "b", "deps": ["a"]}],
				["a"],
				"id",
				"deps"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok)
				assert.Len(t, list, 2, "should handle circular deps and include both a and b once")
			},
		},
		{
			name: "diamond dependency pattern",
			expression: `map.recurse(
				[
					{"id": "a", "deps": ["b", "c"]},
					{"id": "b", "deps": ["d"]},
					{"id": "c", "deps": ["d"]},
					{"id": "d", "deps": []}
				],
				["a"],
				"id",
				"deps"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok)
				assert.Len(t, list, 4, "should include a, b, c, and d (d only once)")

				// Count how many times 'd' appears (should be once)
				dCount := 0
				for _, obj := range list {
					if obj["id"].(string) == "d" {
						dCount++
					}
				}
				assert.Equal(t, 1, dCount, "d should appear exactly once")
			},
		},
		{
			name: "missing dependency is ignored",
			expression: `map.recurse(
				[{"id": "a", "deps": ["b", "missing"]}, {"id": "b", "deps": []}],
				["a"],
				"id",
				"deps"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok)
				assert.Len(t, list, 2, "should include a and b, ignore missing")
			},
		},
		{
			name: "different property names",
			expression: `map.recurse(
				[{"name": "app", "requires": ["lib1"]}, {"name": "lib1", "requires": ["lib2"]}, {"name": "lib2", "requires": []}],
				["app"],
				"name",
				"requires"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok)
				assert.Len(t, list, 3)

				names := make(map[string]bool)
				for _, obj := range list {
					names[obj["name"].(string)] = true
				}
				assert.True(t, names["app"])
				assert.True(t, names["lib1"])
				assert.True(t, names["lib2"])
			},
		},
		{
			name: "objects with additional properties preserved",
			expression: `map.recurse(
				[
					{"id": "a", "deps": ["b"], "version": "1.0"},
					{"id": "b", "deps": [], "version": "2.0"}
				],
				["a"],
				"id",
				"deps"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok)
				assert.Len(t, list, 2)

				// Verify additional properties are preserved
				for _, obj := range list {
					assert.Contains(t, obj, "version")
				}
			},
		},
		{
			name: "empty start list returns empty result",
			expression: `map.recurse(
				[{"id": "a", "deps": ["b"]}, {"id": "b", "deps": []}],
				[],
				"id",
				"deps"
			)`,
			validate: func(t *testing.T, result any) {
				list, ok := result.([]map[string]any)
				require.True(t, ok)
				assert.Empty(t, list)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.NoError(t, issues.Err())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]any{})
			require.NoError(t, err)
			tt.validate(t, result.Value())
		})
	}
}

func TestRecurseFunc_ComplexScenarios(t *testing.T) {
	fn := RecurseFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	t.Run("package manager scenario", func(t *testing.T) {
		ast, issues := env.Compile(`map.recurse(
			[
				{"package": "react", "deps": ["react-dom", "prop-types"]},
				{"package": "react-dom", "deps": ["scheduler"]},
				{"package": "prop-types", "deps": []},
				{"package": "scheduler", "deps": []},
				{"package": "axios", "deps": []},
				{"package": "lodash", "deps": []}
			],
			["react"],
			"package",
			"deps"
		)`)
		require.NoError(t, issues.Err())

		prog, err := env.Program(ast)
		require.NoError(t, err)

		result, _, err := prog.Eval(map[string]any{})
		require.NoError(t, err)

		list, ok := result.Value().([]map[string]any)
		require.True(t, ok)
		assert.Len(t, list, 4, "should include react, react-dom, prop-types, and scheduler")

		// Verify axios and lodash are NOT included
		packages := make(map[string]bool)
		for _, obj := range list {
			packages[obj["package"].(string)] = true
		}
		assert.True(t, packages["react"])
		assert.True(t, packages["react-dom"])
		assert.True(t, packages["prop-types"])
		assert.True(t, packages["scheduler"])
		assert.False(t, packages["axios"], "axios should not be included")
		assert.False(t, packages["lodash"], "lodash should not be included")
	})

	t.Run("deep nesting", func(t *testing.T) {
		ast, issues := env.Compile(`map.recurse(
			[
				{"id": "1", "deps": ["2"]},
				{"id": "2", "deps": ["3"]},
				{"id": "3", "deps": ["4"]},
				{"id": "4", "deps": ["5"]},
				{"id": "5", "deps": []}
			],
			["1"],
			"id",
			"deps"
		)`)
		require.NoError(t, issues.Err())

		prog, err := env.Program(ast)
		require.NoError(t, err)

		result, _, err := prog.Eval(map[string]any{})
		require.NoError(t, err)

		list, ok := result.Value().([]map[string]any)
		require.True(t, ok)
		assert.Len(t, list, 5, "should include all 5 levels")
	})
}

func TestRecurseFunc_WithVariables(t *testing.T) {
	fn := RecurseFunc()
	env, err := cel.NewEnv(
		fn.EnvOptions[0],
		cel.Variable("allObjects", cel.ListType(cel.MapType(cel.StringType, cel.DynType))),
		cel.Variable("startIds", cel.ListType(cel.StringType)),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`map.recurse(allObjects, startIds, "id", "deps")`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{
		"allObjects": []map[string]any{
			{"id": "a", "deps": []any{"b"}},
			{"id": "b", "deps": []any{}},
		},
		"startIds": []string{"a"},
	})
	require.NoError(t, err)

	list, ok := result.Value().([]map[string]any)
	require.True(t, ok)
	assert.Len(t, list, 2)
}

func BenchmarkRecurseFunc_CEL_Simple(b *testing.B) {
	fn := RecurseFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`map.recurse(
		[{"id": "a", "deps": ["b"]}, {"id": "b", "deps": ["c"]}, {"id": "c", "deps": []}],
		["a"],
		"id",
		"deps"
	)`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkRecurseFunc_CEL_Complex(b *testing.B) {
	fn := RecurseFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`map.recurse(
		[
			{"id": "a", "deps": ["b", "c"]},
			{"id": "b", "deps": ["d", "e"]},
			{"id": "c", "deps": ["e", "f"]},
			{"id": "d", "deps": []},
			{"id": "e", "deps": []},
			{"id": "f", "deps": []}
		],
		["a"],
		"id",
		"deps"
	)`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}
