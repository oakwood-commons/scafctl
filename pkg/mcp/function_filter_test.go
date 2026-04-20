// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testFuncWithSubs implements namedFunction + subNamed for testing.
type testFuncWithSubs struct {
	testFunc
	subNames []string
}

func (t testFuncWithSubs) GetSubNames() []string { return t.subNames }

func TestFilterAndReturnNamedFunctions_MatchesSubNames(t *testing.T) {
	funcs := []testFuncWithSubs{
		{testFunc: testFunc{name: "encoders", description: "Encoding functions"}, subNames: []string{"base64.encode", "base64.decode"}},
		{testFunc: testFunc{name: "strings", description: "String functions"}, subNames: []string{"strings.upper", "strings.lower"}},
	}

	// Match by sub-function name
	result, err := filterAndReturnNamedFunctions(funcs, "base64", "test function", "test_tool")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsError)

	// Match by group name
	result, err = filterAndReturnNamedFunctions(funcs, "encoders", "test function", "test_tool")
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsError)

	// No match
	result, err = filterAndReturnNamedFunctions(funcs, "nonexistent", "test function", "test_tool")
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestSearchFunctions_MatchesSubNames(t *testing.T) {
	funcs := []testFuncWithSubs{
		{testFunc: testFunc{name: "encoders", description: "Encoding functions"}, subNames: []string{"base64.encode", "base64.decode"}},
		{testFunc: testFunc{name: "strings", description: "String functions"}, subNames: []string{"strings.upper", "strings.lower"}},
	}

	// Match by sub-function name
	filtered, errResult := searchFunctions(funcs, "base64.decode", "test function", "test_tool")
	assert.Nil(t, errResult)
	require.Len(t, filtered, 1)
	assert.Equal(t, "encoders", filtered[0].GetName())

	// Match by description
	filtered, errResult = searchFunctions(funcs, "Encoding", "test function", "test_tool")
	assert.Nil(t, errResult)
	require.Len(t, filtered, 1)
	assert.Equal(t, "encoders", filtered[0].GetName())

	// No match
	filtered, errResult = searchFunctions(funcs, "nonexistent", "test function", "test_tool")
	assert.NotNil(t, errResult)
	assert.Nil(t, filtered)
}

func TestFilterAndReturnNamedFunctions_WithoutSubNames(t *testing.T) {
	funcs := []testFunc{
		{name: "strlen", description: "String length"},
		{name: "concat", description: "Concatenate strings"},
	}

	// Types without GetSubNames still work (name match only)
	result, err := filterAndReturnNamedFunctions(funcs, "str", "test function", "test_tool")
	require.NoError(t, err)
	assert.False(t, result.IsError)

	// No match
	result, err = filterAndReturnNamedFunctions(funcs, "nonexistent", "test function", "test_tool")
	require.NoError(t, err)
	assert.True(t, result.IsError)
}
