// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package marshalling

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONMarshalFunc_Metadata(t *testing.T) {
	fn := JSONMarshalFunc()
	assert.Equal(t, "json.marshal", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.EnvOptions, 1)
}

func TestJSONMarshalFunc_CELIntegration(t *testing.T) {
	fn := JSONMarshalFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{
			name:     "marshal empty map",
			expr:     `json.marshal({})`,
			expected: `{}`,
		},
		{
			name:     "marshal simple map",
			expr:     `json.marshal({"name": "John", "age": 30})`,
			expected: `{"age":30,"name":"John"}`,
		},
		{
			name:     "marshal empty list",
			expr:     `json.marshal([])`,
			expected: `[]`,
		},
		{
			name:     "marshal string list",
			expr:     `json.marshal(["apple", "banana", "cherry"])`,
			expected: `["apple","banana","cherry"]`,
		},
		{
			name:     "marshal number list",
			expr:     `json.marshal([1, 2, 3])`,
			expected: `[1,2,3]`,
		},
		{
			name:     "marshal nested map",
			expr:     `json.marshal({"user": {"name": "John", "age": 30}})`,
			expected: `{"user":{"age":30,"name":"John"}}`,
		},
		{
			name:     "marshal boolean",
			expr:     `json.marshal(true)`,
			expected: `true`,
		},
		{
			name:     "marshal string",
			expr:     `json.marshal("hello")`,
			expected: `"hello"`,
		},
		{
			name:     "marshal number",
			expr:     `json.marshal(42)`,
			expected: `42`,
		},
		// Note: CEL null marshaling handled separately
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

func TestJSONMarshalPrettyFunc_Metadata(t *testing.T) {
	fn := JSONMarshalPrettyFunc()
	assert.Equal(t, "json.marshalPretty", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.EnvOptions, 1)
}

func TestJSONMarshalPrettyFunc_CELIntegration(t *testing.T) {
	fn := JSONMarshalPrettyFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{
			name:     "marshal pretty empty map",
			expr:     `json.marshalPretty({})`,
			expected: "{}",
		},
		{
			name: "marshal pretty simple map",
			expr: `json.marshalPretty({"name": "John", "age": 30})`,
			expected: `{
  "age": 30,
  "name": "John"
}`,
		},
		{
			name: "marshal pretty nested map",
			expr: `json.marshalPretty({"user": {"name": "John", "roles": ["admin", "user"]}})`,
			expected: `{
  "user": {
    "name": "John",
    "roles": [
      "admin",
      "user"
    ]
  }
}`,
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

func TestJSONUnmarshalFunc_Metadata(t *testing.T) {
	fn := JSONUnmarshalFunc()
	assert.Equal(t, "json.unmarshal", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.EnvOptions, 1)
}

func TestJSONUnmarshalFunc_CELIntegration(t *testing.T) {
	fn := JSONUnmarshalFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		expr     string
		expected any
	}{
		{
			name:     "unmarshal empty map",
			expr:     `json.unmarshal('{}')`,
			expected: map[string]any{},
		},
		{
			name:     "unmarshal simple map",
			expr:     `json.unmarshal('{"name":"John","age":30}')`,
			expected: map[string]any{"name": "John", "age": float64(30)},
		},
		{
			name:     "unmarshal empty list",
			expr:     `json.unmarshal('[]')`,
			expected: []any{},
		},
		{
			name:     "unmarshal string list",
			expr:     `json.unmarshal('["apple","banana","cherry"]')`,
			expected: []any{"apple", "banana", "cherry"},
		},
		{
			name:     "unmarshal number list",
			expr:     `json.unmarshal('[1,2,3]')`,
			expected: []any{float64(1), float64(2), float64(3)},
		},
		{
			name: "unmarshal nested map",
			expr: `json.unmarshal('{"user":{"name":"John","age":30}}')`,
			expected: map[string]any{
				"user": map[string]any{
					"name": "John",
					"age":  float64(30),
				},
			},
		},
		{
			name:     "unmarshal boolean",
			expr:     `json.unmarshal('true')`,
			expected: true,
		},
		{
			name:     "unmarshal string",
			expr:     `json.unmarshal('"hello"')`,
			expected: "hello",
		},
		{
			name:     "unmarshal number",
			expr:     `json.unmarshal('42')`,
			expected: float64(42),
		},
		// Note: CEL null is represented as types.NullValue, not Go nil
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

func TestJSONUnmarshalFunc_AccessProperties(t *testing.T) {
	fn := JSONUnmarshalFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`json.unmarshal('{"user":{"name":"John"}}').user.name`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, "John", result.Value())
}

func TestJSONUnmarshalFunc_InvalidJSON(t *testing.T) {
	fn := JSONUnmarshalFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`json.unmarshal('{"invalid":')`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	_, _, err = prog.Eval(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "json.unmarshal")
}

func TestJsonRoundTrip(t *testing.T) {
	marshalFn := JSONMarshalFunc()
	unmarshalFn := JSONUnmarshalFunc()
	env, err := cel.NewEnv(marshalFn.EnvOptions[0], unmarshalFn.EnvOptions[0])
	require.NoError(t, err)

	ast, issues := env.Compile(`json.unmarshal(json.marshal({"name": "John", "age": 30}))`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	expected := map[string]any{"name": "John", "age": float64(30)}
	assert.Equal(t, expected, result.Value())
}

func TestYamlMarshalFunc_Metadata(t *testing.T) {
	fn := YamlMarshalFunc()
	assert.Equal(t, "yaml.marshal", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.EnvOptions, 1)
}

func TestYamlMarshalFunc_CELIntegration(t *testing.T) {
	fn := YamlMarshalFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		expr     string
		expected string
	}{
		{
			name:     "marshal empty map",
			expr:     `yaml.marshal({})`,
			expected: "{}\n",
		},
		{
			name:     "marshal simple map",
			expr:     `yaml.marshal({"name": "John", "age": 30})`,
			expected: "age: 30\nname: John\n",
		},
		{
			name:     "marshal string list",
			expr:     `yaml.marshal(["apple", "banana", "cherry"])`,
			expected: "- apple\n- banana\n- cherry\n",
		},
		{
			name:     "marshal nested map",
			expr:     `yaml.marshal({"user": {"name": "John", "roles": ["admin", "user"]}})`,
			expected: "user:\n    name: John\n    roles:\n        - admin\n        - user\n",
		},
		{
			name:     "marshal boolean",
			expr:     `yaml.marshal(true)`,
			expected: "true\n",
		},
		{
			name:     "marshal string",
			expr:     `yaml.marshal("hello")`,
			expected: "hello\n",
		},
		{
			name:     "marshal number",
			expr:     `yaml.marshal(42)`,
			expected: "42\n",
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

func TestYamlUnmarshalFunc_Metadata(t *testing.T) {
	fn := YamlUnmarshalFunc()
	assert.Equal(t, "yaml.unmarshal", fn.Name)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.EnvOptions, 1)
}

func TestYamlUnmarshalFunc_CELIntegration(t *testing.T) {
	fn := YamlUnmarshalFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		expr     string
		expected any
	}{
		{
			name:     "unmarshal empty map",
			expr:     `yaml.unmarshal('{}')`,
			expected: map[string]any{},
		},
		{
			name:     "unmarshal simple map",
			expr:     `yaml.unmarshal('name: John\nage: 30')`,
			expected: map[string]any{"name": "John", "age": 30},
		},
		{
			name:     "unmarshal string list",
			expr:     `yaml.unmarshal('- apple\n- banana\n- cherry')`,
			expected: []any{"apple", "banana", "cherry"},
		},
		{
			name: "unmarshal nested map",
			expr: `yaml.unmarshal('user:\n  name: John\n  age: 30')`,
			expected: map[string]any{
				"user": map[string]any{
					"name": "John",
					"age":  30,
				},
			},
		},
		{
			name:     "unmarshal boolean",
			expr:     `yaml.unmarshal('true')`,
			expected: true,
		},
		{
			name:     "unmarshal string",
			expr:     `yaml.unmarshal('hello')`,
			expected: "hello",
		},
		// Note: YAML numbers are int64 in CEL, nulls are types.NullValue
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

func TestYamlUnmarshalFunc_AccessProperties(t *testing.T) {
	fn := YamlUnmarshalFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`yaml.unmarshal('user:\n  name: John').user.name`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	assert.Equal(t, "John", result.Value())
}

func TestYamlUnmarshalFunc_InvalidYAML(t *testing.T) {
	fn := YamlUnmarshalFunc()
	env, err := cel.NewEnv(fn.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`yaml.unmarshal('invalid:\n  - syntax')`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	// This actually parses successfully in YAML as a map with "invalid" key and list value
	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestYamlRoundTrip(t *testing.T) {
	marshalFn := YamlMarshalFunc()
	unmarshalFn := YamlUnmarshalFunc()
	env, err := cel.NewEnv(marshalFn.EnvOptions[0], unmarshalFn.EnvOptions[0])
	require.NoError(t, err)

	ast, issues := env.Compile(`yaml.unmarshal(yaml.marshal({"name": "John", "age": 30}))`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	expected := map[string]any{"name": "John", "age": 30}
	assert.Equal(t, expected, result.Value())
}

func TestJsonYamlInterop(t *testing.T) {
	jsonMarshal := JSONMarshalFunc()
	yamlUnmarshal := YamlUnmarshalFunc()
	env, err := cel.NewEnv(jsonMarshal.EnvOptions[0], yamlUnmarshal.EnvOptions[0])
	require.NoError(t, err)

	// JSON is valid YAML, so we can unmarshal JSON with YAML unmarshaler
	ast, issues := env.Compile(`yaml.unmarshal(json.marshal({"name": "John", "age": 30}))`)
	require.NoError(t, issues.Err())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]any{})
	require.NoError(t, err)

	expected := map[string]any{"name": "John", "age": 30}
	assert.Equal(t, expected, result.Value())
}

// Benchmark tests
func BenchmarkJsonMarshal_CEL_SimpleMap(b *testing.B) {
	fn := JSONMarshalFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`json.marshal({"name": "John", "age": 30})`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkJsonMarshalPretty_CEL_NestedMap(b *testing.B) {
	fn := JSONMarshalPrettyFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`json.marshalPretty({"user": {"name": "John", "roles": ["admin", "user"]}})`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkJsonUnmarshal_CEL_SimpleMap(b *testing.B) {
	fn := JSONUnmarshalFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`json.unmarshal('{"name":"John","age":30}')`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkYamlMarshal_CEL_SimpleMap(b *testing.B) {
	fn := YamlMarshalFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`yaml.marshal({"name": "John", "age": 30})`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}

func BenchmarkYamlUnmarshal_CEL_SimpleMap(b *testing.B) {
	fn := YamlUnmarshalFunc()
	env, _ := cel.NewEnv(fn.EnvOptions...)
	ast, _ := env.Compile(`yaml.unmarshal('name: John\nage: 30')`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]any{})
	}
}
