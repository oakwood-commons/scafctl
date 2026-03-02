// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package regex

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types/ref"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- MatchFunc tests ---

func TestMatchFunc_Metadata(t *testing.T) {
	f := MatchFunc()
	assert.Equal(t, "regex.match", f.Name)
	assert.True(t, f.Custom)
	assert.Contains(t, f.FunctionNames, "regex.match")
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.NotEmpty(t, f.EnvOptions)
}

func TestMatchFunc_CELIntegration(t *testing.T) {
	matchFunc := MatchFunc()

	env, err := cel.NewEnv(matchFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   bool
	}{
		{
			name:       "match at start",
			expression: `regex.match("^Hello", "Hello World")`,
			expected:   true,
		},
		{
			name:       "match digits",
			expression: `regex.match("[0-9]+", "abc123")`,
			expected:   true,
		},
		{
			name:       "no match",
			expression: `regex.match("^xyz", "Hello World")`,
			expected:   false,
		},
		{
			name:       "full match",
			expression: `regex.match("^[a-z]+$", "hello")`,
			expected:   true,
		},
		{
			name:       "email-like pattern",
			expression: `regex.match("[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}", "user@example.com")`,
			expected:   true,
		},
		{
			name:       "empty pattern matches empty string",
			expression: `regex.match("", "")`,
			expected:   true,
		},
		{
			name:       "dot matches any char",
			expression: `regex.match("h.llo", "hello")`,
			expected:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.Value())
		})
	}
}

func TestMatchFunc_InvalidRegex(t *testing.T) {
	matchFunc := MatchFunc()

	env, err := cel.NewEnv(matchFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`regex.match("(invalid[", "test")`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	_, _, err = prog.Eval(map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex pattern")
}

func TestMatchFunc_TypeError(t *testing.T) {
	matchFunc := MatchFunc()

	env, err := cel.NewEnv(matchFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name             string
		expression       string
		expectedErrorMsg string
	}{
		{
			name:             "integer first arg",
			expression:       `regex.match(123, "test")`,
			expectedErrorMsg: "found no matching overload for 'regex.match' applied to '(int, string)'",
		},
		{
			name:             "integer second arg",
			expression:       `regex.match("pattern", 123)`,
			expectedErrorMsg: "found no matching overload for 'regex.match' applied to '(string, int)'",
		},
		{
			name:             "boolean args",
			expression:       `regex.match(true, false)`,
			expectedErrorMsg: "found no matching overload for 'regex.match' applied to '(bool, bool)'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := env.Compile(tt.expression)
			require.NotNil(t, issues, "expected compilation error for wrong type")
			assert.Contains(t, issues.String(), tt.expectedErrorMsg)
		})
	}
}

func TestMatchFunc_WithVariables(t *testing.T) {
	matchFunc := MatchFunc()

	env, err := cel.NewEnv(
		matchFunc.EnvOptions[0],
		cel.Variable("pattern", cel.StringType),
		cel.Variable("input", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`regex.match(pattern, input)`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{
		"pattern": "^test",
		"input":   "testing",
	})
	require.NoError(t, err)
	assert.Equal(t, true, result.Value())
}

// --- ReplaceFunc tests ---

func TestReplaceFunc_Metadata(t *testing.T) {
	f := ReplaceFunc()
	assert.Equal(t, "regex.replace", f.Name)
	assert.True(t, f.Custom)
	assert.Contains(t, f.FunctionNames, "regex.replace")
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.NotEmpty(t, f.EnvOptions)
}

func TestReplaceFunc_CELIntegration(t *testing.T) {
	replaceFunc := ReplaceFunc()

	env, err := cel.NewEnv(replaceFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   string
	}{
		{
			name:       "replace digits with hash",
			expression: `regex.replace("abc123def456", "[0-9]+", "#")`,
			expected:   "abc#def#",
		},
		{
			name:       "replace whitespace with dashes",
			expression: `regex.replace("hello world foo", "\\s+", "-")`,
			expected:   "hello-world-foo",
		},
		{
			name:       "remove non-alphanumeric",
			expression: `regex.replace("hello! @world#", "[^a-zA-Z0-9]", "")`,
			expected:   "helloworld",
		},
		{
			name:       "no match leaves string unchanged",
			expression: `regex.replace("hello", "[0-9]+", "X")`,
			expected:   "hello",
		},
		{
			name:       "replace entire string",
			expression: `regex.replace("test", ".*", "replaced")`,
			expected:   "replaced",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.Value())
		})
	}
}

func TestReplaceFunc_InvalidRegex(t *testing.T) {
	replaceFunc := ReplaceFunc()

	env, err := cel.NewEnv(replaceFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`regex.replace("test", "(invalid[", "x")`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	_, _, err = prog.Eval(map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex pattern")
}

func TestReplaceFunc_TypeError(t *testing.T) {
	replaceFunc := ReplaceFunc()

	env, err := cel.NewEnv(replaceFunc.EnvOptions...)
	require.NoError(t, err)

	_, issues := env.Compile(`regex.replace(123, "pattern", "replacement")`)
	require.NotNil(t, issues, "expected compilation error for wrong type")
}

// --- FindAllFunc tests ---

func TestFindAllFunc_Metadata(t *testing.T) {
	f := FindAllFunc()
	assert.Equal(t, "regex.findAll", f.Name)
	assert.True(t, f.Custom)
	assert.Contains(t, f.FunctionNames, "regex.findAll")
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.NotEmpty(t, f.EnvOptions)
}

func TestFindAllFunc_CELIntegration(t *testing.T) {
	findAllFunc := FindAllFunc()

	env, err := cel.NewEnv(findAllFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   []string
	}{
		{
			name:       "find digit sequences",
			expression: `regex.findAll("[0-9]+", "abc123def456")`,
			expected:   []string{"123", "456"},
		},
		{
			name:       "find words",
			expression: `regex.findAll("[a-zA-Z]+", "hello 123 world 456")`,
			expected:   []string{"hello", "world"},
		},
		{
			name:       "no matches returns empty list",
			expression: `regex.findAll("[0-9]+", "no digits here")`,
			expected:   []string{},
		},
		{
			name:       "single match",
			expression: `regex.findAll("world", "hello world")`,
			expected:   []string{"world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)

			// Convert CEL list to Go slice for comparison
			var actual []string
			switch v := result.Value().(type) {
			case []any:
				for _, item := range v {
					actual = append(actual, item.(string))
				}
			case []ref.Val:
				for _, item := range v {
					actual = append(actual, item.Value().(string))
				}
			default:
				t.Fatalf("unexpected result type: %T", result.Value())
			}
			if actual == nil {
				actual = []string{}
			}
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestFindAllFunc_InvalidRegex(t *testing.T) {
	findAllFunc := FindAllFunc()

	env, err := cel.NewEnv(findAllFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`regex.findAll("(invalid[", "test")`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	_, _, err = prog.Eval(map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex pattern")
}

func TestFindAllFunc_TypeError(t *testing.T) {
	findAllFunc := FindAllFunc()

	env, err := cel.NewEnv(findAllFunc.EnvOptions...)
	require.NoError(t, err)

	_, issues := env.Compile(`regex.findAll(123, "test")`)
	require.NotNil(t, issues, "expected compilation error for wrong type")
}

// --- SplitFunc tests ---

func TestSplitFunc_Metadata(t *testing.T) {
	f := SplitFunc()
	assert.Equal(t, "regex.split", f.Name)
	assert.True(t, f.Custom)
	assert.Contains(t, f.FunctionNames, "regex.split")
	assert.NotEmpty(t, f.Description)
	assert.NotEmpty(t, f.Examples)
	assert.NotEmpty(t, f.EnvOptions)
}

func TestSplitFunc_CELIntegration(t *testing.T) {
	splitFunc := SplitFunc()

	env, err := cel.NewEnv(splitFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   []string
	}{
		{
			name:       "split by whitespace",
			expression: `regex.split("\\s+", "hello   world   foo")`,
			expected:   []string{"hello", "world", "foo"},
		},
		{
			name:       "split by comma",
			expression: `regex.split(",", "a,b,c")`,
			expected:   []string{"a", "b", "c"},
		},
		{
			name:       "split by multiple delimiters",
			expression: `regex.split("[,;]+", "a,b;c,,d")`,
			expected:   []string{"a", "b", "c", "d"},
		},
		{
			name:       "split by digits",
			expression: `regex.split("[0-9]+", "abc123def456ghi")`,
			expected:   []string{"abc", "def", "ghi"},
		},
		{
			name:       "no match returns single element",
			expression: `regex.split(",", "nosep")`,
			expected:   []string{"nosep"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)

			var actual []string
			switch v := result.Value().(type) {
			case []any:
				for _, item := range v {
					actual = append(actual, item.(string))
				}
			case []ref.Val:
				for _, item := range v {
					actual = append(actual, item.Value().(string))
				}
			default:
				t.Fatalf("unexpected result type: %T", result.Value())
			}
			if actual == nil {
				actual = []string{}
			}
			assert.Equal(t, tt.expected, actual)
		})
	}
}

func TestSplitFunc_InvalidRegex(t *testing.T) {
	splitFunc := SplitFunc()

	env, err := cel.NewEnv(splitFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`regex.split("(invalid[", "test")`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	_, _, err = prog.Eval(map[string]interface{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex pattern")
}

func TestSplitFunc_TypeError(t *testing.T) {
	splitFunc := SplitFunc()

	env, err := cel.NewEnv(splitFunc.EnvOptions...)
	require.NoError(t, err)

	_, issues := env.Compile(`regex.split(123, "test")`)
	require.NotNil(t, issues, "expected compilation error for wrong type")
}

// --- Cross-function tests ---

func TestRegex_ChainedOperations(t *testing.T) {
	matchFunc := MatchFunc()
	replaceFunc := ReplaceFunc()
	findAllFunc := FindAllFunc()
	splitFunc := SplitFunc()

	env, err := cel.NewEnv(
		matchFunc.EnvOptions[0],
		replaceFunc.EnvOptions[0],
		findAllFunc.EnvOptions[0],
		splitFunc.EnvOptions[0],
	)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   any
	}{
		{
			name:       "replace then match",
			expression: `regex.match("^[a-z]+$", regex.replace("Hello123", "[^a-z]", ""))`,
			expected:   true,
		},
		{
			name:       "findAll count via size",
			expression: `size(regex.findAll("[0-9]+", "a1b2c3"))`,
			expected:   int64(3),
		},
		{
			name:       "split count via size",
			expression: `size(regex.split(",", "a,b,c,d"))`,
			expected:   int64(4),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result.Value())
		})
	}
}

func TestRegex_WithDynVariables(t *testing.T) {
	matchFunc := MatchFunc()

	env, err := cel.NewEnv(
		matchFunc.EnvOptions[0],
		cel.Variable("_", cel.DynType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`regex.match("^[0-9]+$", _)`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{
		"_": "12345",
	})
	require.NoError(t, err)
	assert.Equal(t, true, result.Value())
}

// --- Benchmarks ---

func BenchmarkMatchFunc(b *testing.B) {
	matchFunc := MatchFunc()
	env, _ := cel.NewEnv(matchFunc.EnvOptions...)
	ast, _ := env.Compile(`regex.match("[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}", "user@example.com")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func BenchmarkReplaceFunc(b *testing.B) {
	replaceFunc := ReplaceFunc()
	env, _ := cel.NewEnv(replaceFunc.EnvOptions...)
	ast, _ := env.Compile(`regex.replace("Hello World 123", "[^a-zA-Z]", "")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func BenchmarkFindAllFunc(b *testing.B) {
	findAllFunc := FindAllFunc()
	env, _ := cel.NewEnv(findAllFunc.EnvOptions...)
	ast, _ := env.Compile(`regex.findAll("[0-9]+", "abc123def456ghi789")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func BenchmarkSplitFunc(b *testing.B) {
	splitFunc := SplitFunc()
	env, _ := cel.NewEnv(splitFunc.EnvOptions...)
	ast, _ := env.Compile(`regex.split("\\s+", "hello world foo bar baz")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}
