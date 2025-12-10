package strings

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "mixed case with hyphens",
			input:    "My-String-Name",
			expected: "mystringname",
		},
		{
			name:     "mixed case with underscores",
			input:    "My_String_Name",
			expected: "mystringname",
		},
		{
			name:     "mixed case with spaces",
			input:    "My String Name",
			expected: "mystringname",
		},
		{
			name:     "all special characters mixed",
			input:    "My-String_Name Test",
			expected: "mystringnametest",
		},
		{
			name:     "already clean lowercase",
			input:    "mystringname",
			expected: "mystringname",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "- _ ",
			expected: "",
		},
		{
			name:     "uppercase only",
			input:    "UPPERCASE",
			expected: "uppercase",
		},
		{
			name:     "numbers preserved",
			input:    "Test-123_String 456",
			expected: "test123string456",
		},
		{
			name:     "unicode characters",
			input:    "café-résumé",
			expected: "caférésumé",
		},
		{
			name:     "multiple consecutive special chars",
			input:    "test---___   string",
			expected: "teststring",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCleanFunc_CELIntegration(t *testing.T) {
	cleanFunc := CleanFunc()

	env, err := cel.NewEnv(cleanFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name        string
		expression  string
		expected    string
		shouldError bool
	}{
		{
			name:       "basic clean operation",
			expression: `strings.clean("My-String_Name")`,
			expected:   "mystringname",
		},
		{
			name:       "clean with spaces",
			expression: `strings.clean("Hello World Test")`,
			expected:   "helloworldtest",
		},
		{
			name:       "clean empty string",
			expression: `strings.clean("")`,
			expected:   "",
		},
		{
			name:       "clean already clean string",
			expression: `strings.clean("alreadyclean")`,
			expected:   "alreadyclean",
		},
		{
			name:       "clean with numbers",
			expression: `strings.clean("Test-123")`,
			expected:   "test123",
		},
		{
			name:       "clean uppercase",
			expression: `strings.clean("UPPERCASE")`,
			expected:   "uppercase",
		},
		{
			name:       "clean mixed special chars",
			expression: `strings.clean("test-case_example name")`,
			expected:   "testcaseexamplename",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(tt.expression)
			require.Nil(t, issues, "compilation failed: %v", issues)

			prog, err := env.Program(ast)
			require.NoError(t, err)

			result, _, err := prog.Eval(map[string]interface{}{})

			if tt.shouldError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result.Value())
			}
		})
	}
}

func TestCleanFunc_Metadata(t *testing.T) {
	cleanFunc := CleanFunc()

	assert.Equal(t, "strings.clean", cleanFunc.Name)
	assert.Equal(t, "Cleans a string by converting it to lowercase and removing hyphens, underscores, and spaces", cleanFunc.Description)
	assert.Equal(t, []string{"strings.clean"}, cleanFunc.FunctionNames)
	assert.True(t, cleanFunc.Custom)
	assert.NotEmpty(t, cleanFunc.EnvOptions)
}

func TestCleanFunc_TypeError(t *testing.T) {
	cleanFunc := CleanFunc()

	env, err := cel.NewEnv(cleanFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name             string
		expression       string
		expectedErrorMsg string
	}{
		{
			name:             "integer argument",
			expression:       `strings.clean(123)`,
			expectedErrorMsg: "found no matching overload for 'strings.clean' applied to '(int)'",
		},
		{
			name:             "boolean argument",
			expression:       `strings.clean(true)`,
			expectedErrorMsg: "found no matching overload for 'strings.clean' applied to '(bool)'",
		},
		{
			name:             "list argument",
			expression:       `strings.clean(["test"])`,
			expectedErrorMsg: "found no matching overload for 'strings.clean' applied to '(list",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// CEL catches type errors at compile time, providing clear error messages
			_, issues := env.Compile(tt.expression)
			require.NotNil(t, issues, "expected compilation error for wrong type")
			assert.Contains(t, issues.String(), tt.expectedErrorMsg)
		})
	}
}

func TestCleanFunc_WithVariables(t *testing.T) {
	cleanFunc := CleanFunc()

	env, err := cel.NewEnv(
		cleanFunc.EnvOptions...,
	)
	require.NoError(t, err)

	env, err = env.Extend(
		cel.Variable("input", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`strings.clean(input)`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	testCases := []struct {
		input    string
		expected string
	}{
		{"Test-Input", "testinput"},
		{"ANOTHER_EXAMPLE", "anotherexample"},
		{"with spaces", "withspaces"},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"input": tc.input,
		})
		require.NoError(t, err)
		assert.Equal(t, tc.expected, result.Value())
	}
}

func TestCleanFunc_ChainedOperations(t *testing.T) {
	cleanFunc := CleanFunc()

	env, err := cel.NewEnv(cleanFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`strings.clean("Hello-World") + strings.clean("Test_Case")`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "helloworldtestcase", result.Value())
}

func BenchmarkCleanString(b *testing.B) {
	testStrings := []string{
		"My-String_Name Test",
		"UPPERCASE-WITH_SEPARATORS AND SPACES",
		"alreadyclean",
		"",
		"Very-Long_String-With_Many-Special_Characters And Spaces Throughout",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range testStrings {
			cleanString(s)
		}
	}
}

func BenchmarkCleanFunc_CEL(b *testing.B) {
	cleanFunc := CleanFunc()
	env, _ := cel.NewEnv(cleanFunc.EnvOptions...)
	ast, _ := env.Compile(`strings.clean("Test-String_With Spaces")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}

// Title function tests

func TestTitleString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase sentence",
			input:    "hello world",
			expected: "Hello World",
		},
		{
			name:     "uppercase sentence",
			input:    "HELLO WORLD",
			expected: "Hello World",
		},
		{
			name:     "mixed case",
			input:    "hELLo WoRLd",
			expected: "Hello World",
		},
		{
			name:     "already title case",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "single word",
			input:    "hello",
			expected: "Hello",
		},
		{
			name:     "with punctuation",
			input:    "hello, world!",
			expected: "Hello, World!",
		},
		{
			name:     "with hyphens",
			input:    "hello-world-test",
			expected: "Hello-World-Test",
		},
		{
			name:     "with underscores",
			input:    "hello_world_test",
			expected: "Hello_world_test",
		},
		{
			name:     "numbers",
			input:    "test 123 case",
			expected: "Test 123 Case",
		},
		{
			name:     "multiple spaces",
			input:    "hello  world",
			expected: "Hello  World",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := titleString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTitleFunc_CELIntegration(t *testing.T) {
	titleFunc := TitleFunc()

	env, err := cel.NewEnv(titleFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   string
	}{
		{
			name:       "basic title operation",
			expression: `strings.title("hello world")`,
			expected:   "Hello World",
		},
		{
			name:       "uppercase to title",
			expression: `strings.title("HELLO WORLD")`,
			expected:   "Hello World",
		},
		{
			name:       "empty string",
			expression: `strings.title("")`,
			expected:   "",
		},
		{
			name:       "already title case",
			expression: `strings.title("Hello World")`,
			expected:   "Hello World",
		},
		{
			name:       "with punctuation",
			expression: `strings.title("hello, world!")`,
			expected:   "Hello, World!",
		},
		{
			name:       "single word",
			expression: `strings.title("hello")`,
			expected:   "Hello",
		},
		{
			name:       "with numbers",
			expression: `strings.title("test 123")`,
			expected:   "Test 123",
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

func TestTitleFunc_Metadata(t *testing.T) {
	titleFunc := TitleFunc()

	assert.Equal(t, "strings.title", titleFunc.Name)
	assert.Equal(t, "Converts a string to title case using English language rules", titleFunc.Description)
	assert.Equal(t, []string{"strings.title"}, titleFunc.FunctionNames)
	assert.True(t, titleFunc.Custom)
	assert.NotEmpty(t, titleFunc.EnvOptions)
}

func TestTitleFunc_TypeError(t *testing.T) {
	titleFunc := TitleFunc()

	env, err := cel.NewEnv(titleFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name             string
		expression       string
		expectedErrorMsg string
	}{
		{
			name:             "integer argument",
			expression:       `strings.title(123)`,
			expectedErrorMsg: "found no matching overload for 'strings.title' applied to '(int)'",
		},
		{
			name:             "boolean argument",
			expression:       `strings.title(false)`,
			expectedErrorMsg: "found no matching overload for 'strings.title' applied to '(bool)'",
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

func TestTitleFunc_WithVariables(t *testing.T) {
	titleFunc := TitleFunc()

	env, err := cel.NewEnv(
		titleFunc.EnvOptions...,
	)
	require.NoError(t, err)

	env, err = env.Extend(
		cel.Variable("input", cel.StringType),
	)
	require.NoError(t, err)

	ast, issues := env.Compile(`strings.title(input)`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	testCases := []struct {
		input    string
		expected string
	}{
		{"hello world", "Hello World"},
		{"UPPERCASE TEXT", "Uppercase Text"},
		{"mixed CaSe", "Mixed Case"},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"input": tc.input,
		})
		require.NoError(t, err)
		assert.Equal(t, tc.expected, result.Value())
	}
}

func TestTitleFunc_ChainedOperations(t *testing.T) {
	titleFunc := TitleFunc()

	env, err := cel.NewEnv(titleFunc.EnvOptions...)
	require.NoError(t, err)

	ast, issues := env.Compile(`strings.title("hello") + " " + strings.title("world")`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	result, _, err := prog.Eval(map[string]interface{}{})
	require.NoError(t, err)
	assert.Equal(t, "Hello World", result.Value())
}

func BenchmarkTitleString(b *testing.B) {
	testStrings := []string{
		"hello world",
		"UPPERCASE TEXT WITH MULTIPLE WORDS",
		"Already Title Case",
		"",
		"very long string with many words to test performance",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, s := range testStrings {
			titleString(s)
		}
	}
}

func BenchmarkTitleFunc_CEL(b *testing.B) {
	titleFunc := TitleFunc()
	env, _ := cel.NewEnv(titleFunc.EnvOptions...)
	ast, _ := env.Compile(`strings.title("hello world test")`)
	prog, _ := env.Program(ast)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}
