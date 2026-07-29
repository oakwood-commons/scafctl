// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsBuiltinFunc(t *testing.T) {
	assert.True(t, IsBuiltinFunc("printf"), "text/template builtin")
	assert.True(t, IsBuiltinFunc("index"), "text/template builtin")
	assert.False(t, IsBuiltinFunc(""), "empty name")
	assert.False(t, IsBuiltinFunc("definitelyNotAFunction123"))
}

func TestBuiltinFuncNames_Sorted(t *testing.T) {
	names := BuiltinFuncNames()
	require.NotEmpty(t, names)
	assert.Contains(t, names, "printf")
	assert.IsIncreasing(t, names)
}

func TestExtractFunctionCalls_Simple(t *testing.T) {
	calls, err := ExtractFunctionCalls(`{{ myHelper .x }}`, "", "", []string{"myHelper"})
	require.NoError(t, err)
	assert.Contains(t, calls, "myHelper")
}

func TestExtractFunctionCalls_Nested(t *testing.T) {
	content := `{{ if gt (myLen .items) 0 }}{{ range .items }}{{ myFmt . }}{{ end }}{{ end }}`
	calls, err := ExtractFunctionCalls(content, "", "", []string{"myLen", "myFmt"})
	require.NoError(t, err)
	assert.Contains(t, calls, "myLen")
	assert.Contains(t, calls, "myFmt")
}

func TestExtractFunctionCalls_ExtensionFuncsRecognized(t *testing.T) {
	// upper is a sprig extension function; it must be recognized without being
	// declared so bodies that mix author + built-in helpers parse.
	initExtensionFactory(t)
	calls, err := ExtractFunctionCalls(`{{ myHelper .x | upper }}`, "", "", []string{"myHelper"})
	require.NoError(t, err)
	assert.Contains(t, calls, "myHelper")
}

func TestExtractFunctionCalls_UndeclaredFails(t *testing.T) {
	_, err := ExtractFunctionCalls(`{{ notDeclared .x }}`, "", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not defined")
}

func TestExtractFunctionCalls_SortedUnique(t *testing.T) {
	content := `{{ a }}{{ b }}{{ a }}`
	calls, err := ExtractFunctionCalls(content, "", "", []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, calls)
}

func TestValidateSyntaxWithFuncs(t *testing.T) {
	// An author function name is recognized when supplied.
	err := ValidateSyntaxWithFuncs(`{{ myHelper .x }}`, "", "", []string{"myHelper"})
	assert.NoError(t, err)

	// Without declaring it, the same template fails to parse.
	err = ValidateSyntaxWithFuncs(`{{ myHelper .x }}`, "", "", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not defined")

	// A genuine syntax error is still reported even with declared funcs.
	err = ValidateSyntaxWithFuncs(`{{ myHelper .x `, "", "", []string{"myHelper"})
	require.Error(t, err)
}
