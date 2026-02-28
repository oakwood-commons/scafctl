// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFunctionDetail_Full(t *testing.T) {
	fn := gotmpl.ExtFunction{
		Name:        "toHcl",
		Description: "Converts to HCL format",
		Custom:      true,
		Links:       []string{"https://example.com"},
		Examples: []gotmpl.Example{
			{
				Description: "Convert a map",
				Template:    `{{ .data | toHcl }}`,
			},
		},
	}

	result := BuildFunctionDetail(&fn)

	assert.Equal(t, "toHcl", result["name"])
	assert.Equal(t, true, result["custom"])
	assert.Equal(t, "Converts to HCL format", result["description"])
	assert.Equal(t, []string{"https://example.com"}, result["links"])

	examples, ok := result["examples"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, examples, 1)
	assert.Equal(t, "Convert a map", examples[0]["description"])
	assert.Equal(t, `{{ .data | toHcl }}`, examples[0]["template"])
}

func TestBuildFunctionDetail_Minimal(t *testing.T) {
	fn := gotmpl.ExtFunction{
		Name:   "upper",
		Custom: false,
	}

	result := BuildFunctionDetail(&fn)

	assert.Equal(t, "upper", result["name"])
	assert.Equal(t, false, result["custom"])
	assert.NotContains(t, result, "description")
	assert.NotContains(t, result, "links")
	assert.NotContains(t, result, "examples")
}

func TestBuildFunctionList(t *testing.T) {
	funcs := gotmpl.ExtFunctionList{
		{Name: "upper", Custom: false, Description: "To uppercase"},
		{Name: "toHcl", Custom: true, Description: "Converts to HCL"},
	}

	result := BuildFunctionList(funcs)

	require.Len(t, result, 2)
	assert.Equal(t, "upper", result[0]["name"])
	assert.Equal(t, "toHcl", result[1]["name"])
}

func TestBuildFunctionDetail_WithExampleLinks(t *testing.T) {
	fn := gotmpl.ExtFunction{
		Name: "test",
		Examples: []gotmpl.Example{
			{
				Description: "Example with links",
				Template:    `{{ test }}`,
				Links:       []string{"https://example.com/doc"},
			},
		},
	}

	result := BuildFunctionDetail(&fn)

	examples, ok := result["examples"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, examples, 1)
	assert.Equal(t, []string{"https://example.com/doc"}, examples[0]["links"])
}
