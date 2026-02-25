// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFunctionDetail(t *testing.T) {
	t.Parallel()
	fn := &celexp.ExtFunction{
		Name:          "test",
		Description:   "test desc",
		Custom:        true,
		FunctionNames: []string{"fn1", "fn2"},
		Links:         []string{"https://example.com"},
		Examples: []celexp.Example{
			{Description: "ex1", Expression: "test()"},
		},
	}

	result := BuildFunctionDetail(fn)
	assert.Equal(t, "test", result["name"])
	assert.Equal(t, true, result["custom"])
	assert.Equal(t, "test desc", result["description"])
	assert.Equal(t, []string{"fn1", "fn2"}, result["functionNames"])
	assert.Equal(t, []string{"https://example.com"}, result["links"])

	examples, ok := result["examples"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, examples, 1)
	assert.Equal(t, "ex1", examples[0]["description"])
	assert.Equal(t, "test()", examples[0]["expression"])
}

func TestBuildFunctionDetail_Minimal(t *testing.T) {
	t.Parallel()
	fn := &celexp.ExtFunction{
		Name:   "minimal",
		Custom: false,
	}

	result := BuildFunctionDetail(fn)
	assert.Equal(t, "minimal", result["name"])
	assert.Equal(t, false, result["custom"])
	assert.Nil(t, result["description"])
	assert.Nil(t, result["functionNames"])
	assert.Nil(t, result["links"])
	assert.Nil(t, result["examples"])
}

func TestBuildFunctionList(t *testing.T) {
	t.Parallel()
	funcs := celexp.ExtFunctionList{
		{Name: "func1", Custom: true},
		{Name: "func2", Custom: false},
	}

	result := BuildFunctionList(funcs)
	assert.Len(t, result, 2)
	assert.Equal(t, "func1", result[0]["name"])
	assert.Equal(t, "func2", result[1]["name"])
}

func TestBuildFunctionList_Empty(t *testing.T) {
	t.Parallel()
	result := BuildFunctionList(celexp.ExtFunctionList{})
	assert.Empty(t, result)
}
