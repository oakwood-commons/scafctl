package arrays

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringAdd_CELIntegration(t *testing.T) {
	stringAddFunc := StringAddFunc()

	env, err := cel.NewEnv(stringAddFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   []string
	}{
		{
			name:       "add to empty list",
			expression: `arrays.strings.add([], "new")`,
			expected:   []string{"new"},
		},
		{
			name:       "add to list with one element",
			expression: `arrays.strings.add(["first"], "second")`,
			expected:   []string{"first", "second"},
		},
		{
			name:       "add to list with multiple elements",
			expression: `arrays.strings.add(["a", "b", "c"], "d")`,
			expected:   []string{"a", "b", "c", "d"},
		},
		{
			name:       "add empty string",
			expression: `arrays.strings.add(["test"], "")`,
			expected:   []string{"test", ""},
		},
		{
			name:       "add with special characters",
			expression: `arrays.strings.add(["hello"], "world!")`,
			expected:   []string{"hello", "world!"},
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

func TestStringAdd_Metadata(t *testing.T) {
	stringAddFunc := StringAddFunc()

	assert.Equal(t, "arrays.strings.add", stringAddFunc.Name)
	assert.Equal(t, "Appends a string to a list of strings and returns the new list. Use arrays.strings.add(list, 'value') to add a single string to the end of the list", stringAddFunc.Description)
	assert.Equal(t, []string{"arrays.strings.add"}, stringAddFunc.FunctionNames)
	assert.True(t, stringAddFunc.Custom)
	assert.NotEmpty(t, stringAddFunc.EnvOptions)
}

func TestStringAdd_TypeError(t *testing.T) {
	stringAddFunc := StringAddFunc()

	env, err := cel.NewEnv(stringAddFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name             string
		expression       string
		expectedErrorMsg string
	}{
		{
			name:             "non-list first argument",
			expression:       `arrays.strings.add("not a list", "value")`,
			expectedErrorMsg: "found no matching overload",
		},
		{
			name:             "non-string second argument",
			expression:       `arrays.strings.add(["test"], 123)`,
			expectedErrorMsg: "found no matching overload",
		},
		{
			name:             "integer list",
			expression:       `arrays.strings.add([1, 2, 3], "test")`,
			expectedErrorMsg: "found no matching overload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := env.Compile(tt.expression)
			require.NotNil(t, issues, "expected compilation error")
			assert.Contains(t, issues.String(), tt.expectedErrorMsg)
		})
	}
}

func TestStringAdd_WithVariables(t *testing.T) {
	stringAddFunc := StringAddFunc()

	env, err := cel.NewEnv(
		stringAddFunc.EnvOptions...,
	)
	require.NoError(t, err)

	env, err = env.Extend(
		cel.Variable("myList", cel.ListType(cel.StringType)),
		cel.Variable("myValue", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`arrays.strings.add(myList, myValue)`)
	if issues != nil {
		t.Logf("Compilation issues: %v", issues)
	}
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	testCases := []struct {
		list     []string
		value    string
		expected []string
	}{
		{[]string{"a", "b"}, "c", []string{"a", "b", "c"}},
		{[]string{}, "first", []string{"first"}},
		{[]string{"hello"}, "world", []string{"hello", "world"}},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"myList":  tc.list,
			"myValue": tc.value,
		})
		require.NoError(t, err)
		assert.Equal(t, tc.expected, result.Value())
	}
}

func TestStringAdd_ChainedOperations(t *testing.T) {
	stringAddFunc := StringAddFunc()

	env, err := cel.NewEnv(stringAddFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`arrays.strings.add(arrays.strings.add(["a"], "b"), "c")`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b", "c"}, result.Value())
}

func BenchmarkStringAdd_CEL(b *testing.B) {
	stringAddFunc := StringAddFunc()
	env, _ := cel.NewEnv(stringAddFunc.EnvOptions...)
	ast, _ := env.Compile(`arrays.strings.add(["a", "b", "c"], "d")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

func TestStringsUnique_CELIntegration(t *testing.T) {
	uniqueFunc := StringsUniqueFunc()

	env, err := cel.NewEnv(uniqueFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   []string
	}{
		{
			name:       "empty list",
			expression: `arrays.strings.unique([])`,
			expected:   []string{},
		},
		{
			name:       "single element",
			expression: `arrays.strings.unique(["hello"])`,
			expected:   []string{"hello"},
		},
		{
			name:       "no duplicates",
			expression: `arrays.strings.unique(["a", "b", "c"])`,
			expected:   []string{"a", "b", "c"},
		},
		{
			name:       "with duplicates",
			expression: `arrays.strings.unique(["a", "b", "a", "c", "b"])`,
			expected:   []string{"a", "b", "c"},
		},
		{
			name:       "all duplicates",
			expression: `arrays.strings.unique(["test", "test", "test"])`,
			expected:   []string{"test"},
		},
		{
			name:       "consecutive duplicates",
			expression: `arrays.strings.unique(["a", "a", "b", "b", "c", "c"])`,
			expected:   []string{"a", "b", "c"},
		},
		{
			name:       "with empty strings",
			expression: `arrays.strings.unique(["", "a", "", "b", ""])`,
			expected:   []string{"", "a", "b"},
		},
		{
			name:       "preserves order",
			expression: `arrays.strings.unique(["z", "a", "z", "b", "a"])`,
			expected:   []string{"z", "a", "b"},
		},
		{
			name:       "special characters",
			expression: `arrays.strings.unique(["hello-world", "foo_bar", "hello-world", "test@example.com"])`,
			expected:   []string{"hello-world", "foo_bar", "test@example.com"},
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

func TestStringsUnique_Metadata(t *testing.T) {
	uniqueFunc := StringsUniqueFunc()

	assert.Equal(t, "arrays.strings.unique", uniqueFunc.Name)
	assert.Equal(t, "Returns a new list containing only unique strings from the input list, removing all duplicates while preserving the original order of first occurrence. Use arrays.strings.unique(list) to deduplicate a list of strings", uniqueFunc.Description)
	assert.Equal(t, []string{"arrays.strings.unique"}, uniqueFunc.FunctionNames)
	assert.True(t, uniqueFunc.Custom)
	assert.NotEmpty(t, uniqueFunc.EnvOptions)
}

func TestStringsUnique_TypeError(t *testing.T) {
	uniqueFunc := StringsUniqueFunc()

	env, err := cel.NewEnv(uniqueFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name             string
		expression       string
		expectedErrorMsg string
	}{
		{
			name:             "non-list argument",
			expression:       `arrays.strings.unique("not a list")`,
			expectedErrorMsg: "found no matching overload",
		},
		{
			name:             "integer list",
			expression:       `arrays.strings.unique([1, 2, 3])`,
			expectedErrorMsg: "found no matching overload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := env.Compile(tt.expression)
			require.NotNil(t, issues, "expected compilation error")
			assert.Contains(t, issues.String(), tt.expectedErrorMsg)
		})
	}
}

func TestStringsUnique_WithVariables(t *testing.T) {
	uniqueFunc := StringsUniqueFunc()

	env, err := cel.NewEnv(uniqueFunc.EnvOptions...)
	require.NoError(t, err)

	env, err = env.Extend(
		cel.Variable("myList", cel.ListType(cel.StringType)),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`arrays.strings.unique(myList)`)
	if issues != nil {
		t.Logf("Compilation issues: %v", issues)
	}
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	testCases := []struct {
		list     []string
		expected []string
	}{
		{[]string{"a", "b", "a"}, []string{"a", "b"}},
		{[]string{}, []string{}},
		{[]string{"x", "y", "z"}, []string{"x", "y", "z"}},
		{[]string{"dup", "dup", "dup"}, []string{"dup"}},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"myList": tc.list,
		})
		require.NoError(t, err)
		assert.Equal(t, tc.expected, result.Value())
	}
}

func TestStringsUnique_ChainedOperations(t *testing.T) {
	uniqueFunc := StringsUniqueFunc()
	addFunc := StringAddFunc()

	env, err := cel.NewEnv(uniqueFunc.EnvOptions...)
	require.NoError(t, err)

	env, err = env.Extend(addFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`arrays.strings.unique(arrays.strings.add(["a", "b", "a"], "b"))`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, result.Value())
}

func BenchmarkStringsUnique_CEL(b *testing.B) {
	uniqueFunc := StringsUniqueFunc()
	env, _ := cel.NewEnv(uniqueFunc.EnvOptions...)
	ast, _ := env.Compile(`arrays.strings.unique(["a", "b", "a", "c", "b", "d"])`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}
