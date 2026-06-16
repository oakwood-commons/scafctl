// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package url

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeFunc(t *testing.T) {
	fn := EncodeFunc()
	env, err := cel.NewEnv(append(fn.EnvOptions, cel.Variable("input", cel.StringType))...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string no encoding needed",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "string with spaces",
			input:    "hello world",
			expected: "hello+world",
		},
		{
			name:     "special characters",
			input:    "key=value&other=test",
			expected: "key%3Dvalue%26other%3Dtest",
		},
		{
			name:     "unicode characters",
			input:    "café",
			expected: "caf%C3%A9",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "forward slashes",
			input:    "path/to/resource",
			expected: "path%2Fto%2Fresource",
		},
		{
			name:     "already encoded string gets double-encoded",
			input:    "hello%20world",
			expected: "hello%2520world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(`url.encode(input)`)
			require.Empty(t, issues.Errors())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			out, _, err := prog.Eval(map[string]any{"input": tt.input})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, out.Value())
		})
	}
}

func TestDecodeFunc(t *testing.T) {
	fn := DecodeFunc()
	env, err := cel.NewEnv(append(fn.EnvOptions, cel.Variable("input", cel.StringType))...)
	require.NoError(t, err)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no decoding needed",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "percent-encoded spaces",
			input:    "hello%20world",
			expected: "hello world",
		},
		{
			name:     "plus-encoded spaces",
			input:    "hello+world",
			expected: "hello world",
		},
		{
			name:     "special characters",
			input:    "key%3Dvalue%26other%3Dtest",
			expected: "key=value&other=test",
		},
		{
			name:     "unicode characters",
			input:    "caf%C3%A9",
			expected: "café",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ast, issues := env.Compile(`url.decode(input)`)
			require.Empty(t, issues.Errors())

			prog, err := env.Program(ast)
			require.NoError(t, err)

			out, _, err := prog.Eval(map[string]any{"input": tt.input})
			require.NoError(t, err)
			assert.Equal(t, tt.expected, out.Value())
		})
	}
}

func TestDecodeFunc_InvalidInput(t *testing.T) {
	fn := DecodeFunc()
	env, err := cel.NewEnv(append(fn.EnvOptions, cel.Variable("input", cel.StringType))...)
	require.NoError(t, err)

	ast, issues := env.Compile(`url.decode(input)`)
	require.Empty(t, issues.Errors())

	prog, err := env.Program(ast)
	require.NoError(t, err)

	// Invalid percent-encoding should produce an error
	_, _, err = prog.Eval(map[string]any{"input": "%ZZ"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "url.decode:")
}

func TestEncodeFunc_Metadata(t *testing.T) {
	fn := EncodeFunc()
	assert.Equal(t, "url.encode", fn.Name)
	assert.Equal(t, "url.encode(string) -> string", fn.Signature)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.FunctionNames, 1)
}

func TestDecodeFunc_Metadata(t *testing.T) {
	fn := DecodeFunc()
	assert.Equal(t, "url.decode", fn.Name)
	assert.Equal(t, "url.decode(string) -> string", fn.Signature)
	assert.True(t, fn.Custom)
	assert.NotEmpty(t, fn.Description)
	assert.NotEmpty(t, fn.Examples)
	assert.Len(t, fn.FunctionNames, 1)
}

func BenchmarkEncodeFunc(b *testing.B) {
	fn := EncodeFunc()
	env, err := cel.NewEnv(append(fn.EnvOptions, cel.Variable("input", cel.StringType))...)
	if err != nil {
		b.Fatal(err)
	}
	ast, iss := env.Compile(`url.encode(input)`)
	if iss.Err() != nil {
		b.Fatal(iss.Err())
	}
	prog, err := env.Program(ast)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _, _ = prog.Eval(map[string]any{"input": "hello world/path?query=value&key=café"})
	}
}

func BenchmarkDecodeFunc(b *testing.B) {
	fn := DecodeFunc()
	env, err := cel.NewEnv(append(fn.EnvOptions, cel.Variable("input", cel.StringType))...)
	if err != nil {
		b.Fatal(err)
	}
	ast, iss := env.Compile(`url.decode(input)`)
	if iss.Err() != nil {
		b.Fatal(iss.Err())
	}
	prog, err := env.Program(ast)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for b.Loop() {
		_, _, _ = prog.Eval(map[string]any{"input": "hello+world%2Fpath%3Fquery%3Dvalue%26key%3Dcaf%C3%A9"})
	}
}
