// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/examples"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleListExamples(t *testing.T) {
	t.Run("lists all examples", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_examples"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleListExamples(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		exs, ok := data["examples"].([]any)
		require.True(t, ok, "expected examples array")
		assert.Greater(t, len(exs), 0, "should find at least one example")

		count, ok := data["count"].(float64)
		require.True(t, ok)
		assert.Equal(t, float64(len(exs)), count)
	})

	t.Run("filters by category", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_examples"
		request.Params.Arguments = map[string]any{
			"category": "actions",
		}

		result, err := srv.handleListExamples(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		exs, ok := data["examples"].([]any)
		require.True(t, ok)
		for _, ex := range exs {
			exMap := ex.(map[string]any)
			assert.Equal(t, "actions", exMap["category"])
		}
	})

	t.Run("empty category returns no results", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "list_examples"
		request.Params.Arguments = map[string]any{
			"category": "nonexistent_category_xyz",
		}

		result, err := srv.handleListExamples(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		text := extractText(t, result)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))

		exs, ok := data["examples"].([]any)
		require.True(t, ok)
		assert.Empty(t, exs)
	})
}

func TestHandleGetExample(t *testing.T) {
	t.Run("reads a valid example", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		// First list to find a valid example
		listReq := mcp.CallToolRequest{}
		listReq.Params.Arguments = map[string]any{"category": "actions"}
		listResult, err := srv.handleListExamples(context.Background(), listReq)
		require.NoError(t, err)

		text := extractText(t, listResult)
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(text), &data))
		exs := data["examples"].([]any)
		require.NotEmpty(t, exs, "need at least one action example")

		firstExample := exs[0].(map[string]any)
		exPath := firstExample["path"].(string)

		// Now get that example
		request := mcp.CallToolRequest{}
		request.Params.Name = "get_example"
		request.Params.Arguments = map[string]any{
			"path": exPath,
		}

		result, err := srv.handleGetExample(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, result.IsError)

		content := extractText(t, result)
		assert.NotEmpty(t, content)
	})

	t.Run("rejects path traversal with dotdot", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_example"
		request.Params.Arguments = map[string]any{
			"path": "../go.mod",
		}

		result, err := srv.handleGetExample(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("rejects nonexistent file", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_example"
		request.Params.Arguments = map[string]any{
			"path": "nonexistent/file.yaml",
		}

		result, err := srv.handleGetExample(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
	})

	t.Run("requires path argument", func(t *testing.T) {
		srv, err := NewServer(WithServerVersion("test"))
		require.NoError(t, err)

		request := mcp.CallToolRequest{}
		request.Params.Name = "get_example"
		request.Params.Arguments = map[string]any{}

		result, err := srv.handleGetExample(context.Background(), request)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.True(t, result.IsError)
	})
}

func TestExamplesScan(t *testing.T) {
	t.Run("scans all examples", func(t *testing.T) {
		items, err := examples.Scan("")
		require.NoError(t, err)
		assert.Greater(t, len(items), 0)

		// Verify items are sorted
		for i := 1; i < len(items); i++ {
			if items[i].Category == items[i-1].Category {
				assert.LessOrEqual(t, items[i-1].Name, items[i].Name)
			} else {
				assert.Less(t, items[i-1].Category, items[i].Category)
			}
		}
	})

	t.Run("filters by category", func(t *testing.T) {
		items, err := examples.Scan("solutions")
		require.NoError(t, err)
		for _, item := range items {
			assert.Equal(t, "solutions", item.Category)
		}
	})

	t.Run("skips bad-solution examples", func(t *testing.T) {
		items, err := examples.Scan("")
		require.NoError(t, err)
		for _, item := range items {
			assert.NotContains(t, item.Path, "bad-solution")
		}
	})
}

func TestExamplesDescriptionFromPath(t *testing.T) {
	t.Run("known path returns specific description", func(t *testing.T) {
		desc := examples.DescriptionFromPath("actions/hello-world.yaml")
		assert.Equal(t, "Simple hello world action", desc)
	})

	t.Run("unknown path generates fallback", func(t *testing.T) {
		desc := examples.DescriptionFromPath("custom/my-custom-thing.yaml")
		assert.Contains(t, desc, "My Custom Thing")
		assert.Contains(t, desc, "example")
	})

	t.Run("dashes replaced with spaces", func(t *testing.T) {
		desc := examples.DescriptionFromPath("something/some-long-name.yaml")
		assert.Contains(t, desc, "Some Long Name")
	})

	t.Run("underscores replaced with spaces", func(t *testing.T) {
		desc := examples.DescriptionFromPath("something/some_other_name.yaml")
		assert.Contains(t, desc, "Some Other Name")
	})
}

func TestExamplesCategories(t *testing.T) {
	cats := examples.Categories()
	assert.NotEmpty(t, cats)
	assert.Contains(t, cats, "actions")
	assert.Contains(t, cats, "solutions")
}
