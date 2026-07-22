// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package strings

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSlugifyFunc_Metadata(t *testing.T) {
	slugifyFunc := SlugifyFunc()

	assert.Equal(t, "strings.slugify", slugifyFunc.Name)
	assert.Equal(t, "strings.slugify(string) -> string", slugifyFunc.Signature)
	assert.True(t, slugifyFunc.Custom)
	assert.NotEmpty(t, slugifyFunc.Description)
	assert.NotEmpty(t, slugifyFunc.Examples)
	assert.NotEmpty(t, slugifyFunc.EnvOptions)
}

func TestSlugifyFunc_CELIntegration(t *testing.T) {
	slugifyFunc := SlugifyFunc()

	env, err := cel.NewEnv(slugifyFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name       string
		expression string
		expected   string
	}{
		{
			name:       "mixed separators and special chars",
			expression: `strings.slugify("My_Org--Name! (test)")`,
			expected:   "my-org-name-test",
		},
		{
			name:       "dots and slashes replaced",
			expression: `strings.slugify("org/repo.name")`,
			expected:   "org-repo-name",
		},
		{
			name:       "empty string",
			expression: `strings.slugify("")`,
			expected:   "",
		},
		{
			name:       "all special characters",
			expression: `strings.slugify("@#$%^&*()")`,
			expected:   "",
		},
		{
			name:       "already valid label",
			expression: `strings.slugify("my-valid-label")`,
			expected:   "my-valid-label",
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

func TestSlugifyFunc_TypeError(t *testing.T) {
	slugifyFunc := SlugifyFunc()

	env, err := cel.NewEnv(slugifyFunc.EnvOptions...)
	require.NoError(t, err)

	tests := []struct {
		name             string
		expression       string
		expectedErrorMsg string
	}{
		{
			name:             "integer argument",
			expression:       `strings.slugify(123)`,
			expectedErrorMsg: "found no matching overload for 'strings.slugify' applied to '(int)'",
		},
		{
			name:             "list argument",
			expression:       `strings.slugify(["test"])`,
			expectedErrorMsg: "found no matching overload for 'strings.slugify' applied to '(list",
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

func TestSlugifyFunc_WithVariables(t *testing.T) {
	slugifyFunc := SlugifyFunc()

	env, err := cel.NewEnv(slugifyFunc.EnvOptions...)
	require.NoError(t, err)

	env, err = env.Extend(cel.Variable("input", cel.StringType))
	require.NoError(t, err)

	ast, issues := env.Compile(`strings.slugify(input)`)
	require.Nil(t, issues)

	prog, err := env.Program(ast)
	require.NoError(t, err)

	testCases := []struct {
		input    string
		expected string
	}{
		{"My Kube_Namespace", "my-kube-namespace"},
		{"My-GitHub_Org.Name", "my-github-org-name"},
	}

	for _, tc := range testCases {
		result, _, err := prog.Eval(map[string]interface{}{
			"input": tc.input,
		})
		require.NoError(t, err)
		assert.Equal(t, tc.expected, result.Value())
	}
}

func BenchmarkSlugifyFunc_CEL(b *testing.B) {
	slugifyFunc := SlugifyFunc()
	env, _ := cel.NewEnv(slugifyFunc.EnvOptions...)
	ast, _ := env.Compile(`strings.slugify("My Application Name @2024!")`)
	prog, _ := env.Program(ast)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		prog.Eval(map[string]interface{}{})
	}
}
