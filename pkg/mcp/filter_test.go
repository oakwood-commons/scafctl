// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
)

func TestContextualToolFilter(t *testing.T) {
	t.Run("all capabilities available - no filtering", func(t *testing.T) {
		srv := &Server{
			authReg:  auth.NewRegistry(),
			config:   &config.Config{Catalogs: []config.CatalogConfig{{Name: "test"}}},
			registry: provider.NewRegistry(),
		}

		filter := contextualToolFilter(srv)
		tools := []mcp.Tool{
			{Name: "auth_status"},
			{Name: "catalog_list"},
			{Name: "list_providers"},
			{Name: "lint_solution"},
		}

		// Even with auth and catalog configured, auth_status/catalog_list are
		// filtered when no handlers or catalogs are empty. But here we have catalogs.
		// Auth needs List() > 0, which is empty so auth tools should still be filtered.
		filtered := filter(context.Background(), tools)
		// auth tools should be filtered (empty registry has no handlers)
		assert.Equal(t, 3, len(filtered))
	})

	t.Run("no auth registry - hides auth tools", func(t *testing.T) {
		srv := &Server{
			authReg:  nil,
			config:   &config.Config{Catalogs: []config.CatalogConfig{{Name: "test"}}},
			registry: provider.NewRegistry(),
		}

		filter := contextualToolFilter(srv)
		tools := []mcp.Tool{
			{Name: "auth_status"},
			{Name: "list_auth_handlers"},
			{Name: "lint_solution"},
			{Name: "list_providers"},
		}

		filtered := filter(context.Background(), tools)
		names := toolNames(filtered)
		assert.NotContains(t, names, "auth_status")
		assert.NotContains(t, names, "list_auth_handlers")
		assert.Contains(t, names, "lint_solution")
		assert.Contains(t, names, "list_providers")
	})

	t.Run("no catalogs - hides catalog tools", func(t *testing.T) {
		srv := &Server{
			config:   &config.Config{},
			registry: provider.NewRegistry(),
		}

		filter := contextualToolFilter(srv)
		tools := []mcp.Tool{
			{Name: "catalog_list"},
			{Name: "catalog_inspect"},
			{Name: "lint_solution"},
		}

		filtered := filter(context.Background(), tools)
		names := toolNames(filtered)
		assert.NotContains(t, names, "catalog_list")
		assert.NotContains(t, names, "catalog_inspect")
		assert.Contains(t, names, "lint_solution")
	})

	t.Run("no registry - hides provider tools", func(t *testing.T) {
		srv := &Server{
			registry: nil,
		}

		filter := contextualToolFilter(srv)
		tools := []mcp.Tool{
			{Name: "list_providers"},
			{Name: "get_provider_schema"},
			{Name: "lint_solution"},
		}

		filtered := filter(context.Background(), tools)
		names := toolNames(filtered)
		assert.NotContains(t, names, "list_providers")
		assert.NotContains(t, names, "get_provider_schema")
		assert.Contains(t, names, "lint_solution")
	})

	t.Run("nil config - hides catalog tools", func(t *testing.T) {
		srv := &Server{
			config: nil,
		}

		filter := contextualToolFilter(srv)
		tools := []mcp.Tool{
			{Name: "catalog_list"},
			{Name: "list_solutions"},
			{Name: "evaluate_cel"},
		}

		filtered := filter(context.Background(), tools)
		names := toolNames(filtered)
		assert.NotContains(t, names, "catalog_list")
		assert.Contains(t, names, "evaluate_cel")
	})

	t.Run("empty tools list returns empty", func(t *testing.T) {
		srv := &Server{}
		filter := contextualToolFilter(srv)
		filtered := filter(context.Background(), []mcp.Tool{})
		assert.Empty(t, filtered)
	})
}

func toolNames(tools []mcp.Tool) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

// testFunc is a mock function for testing filter helpers.
type testFunc struct {
	name        string
	description string
	category    string
}

func (f testFunc) GetName() string        { return f.name }
func (f testFunc) GetDescription() string { return f.description }

func TestSearchFunctions(t *testing.T) {
	functions := []testFunc{
		{name: "json.marshal", description: "Serialize value to JSON string"},
		{name: "yaml.marshal", description: "Serialize value to YAML string"},
		{name: "base64.encode", description: "Encode string to base64"},
		{name: "map.merge", description: "Deep-merge two maps"},
	}

	t.Run("matches name", func(t *testing.T) {
		result, errResult := searchFunctions(functions, "json", "test", "test_tool")
		assert.Nil(t, errResult)
		assert.Len(t, result, 1)
		assert.Equal(t, "json.marshal", result[0].name)
	})

	t.Run("matches description", func(t *testing.T) {
		result, errResult := searchFunctions(functions, "serialize", "test", "test_tool")
		assert.Nil(t, errResult)
		assert.Len(t, result, 2)
	})

	t.Run("case insensitive", func(t *testing.T) {
		result, errResult := searchFunctions(functions, "JSON", "test", "test_tool")
		assert.Nil(t, errResult)
		assert.Len(t, result, 1)
	})

	t.Run("no match returns error result", func(t *testing.T) {
		result, errResult := searchFunctions(functions, "xyznonexistent", "test", "test_tool")
		assert.Nil(t, result)
		assert.NotNil(t, errResult)
		assert.True(t, errResult.IsError)
	})

	t.Run("empty query returns all", func(t *testing.T) {
		result, errResult := searchFunctions(functions, "", "test", "test_tool")
		assert.Nil(t, errResult)
		assert.Len(t, result, 4)
	})
}

func TestBuildFunctionIndex(t *testing.T) {
	functions := []testFunc{
		{name: "json.marshal", category: "encoding"},
		{name: "yaml.marshal", category: "encoding"},
		{name: "size", category: "collections"},
		{name: "trim", category: "strings"},
	}

	index := buildFunctionIndex(functions, func(f testFunc) string { return f.category }, nil)

	assert.Contains(t, index, "# Summary (4 functions)")
	assert.Contains(t, index, "## collections (1)")
	assert.Contains(t, index, "## encoding (2)")
	assert.Contains(t, index, "## strings (1)")
	assert.Contains(t, index, "json.marshal")
	assert.Contains(t, index, "yaml.marshal")
}

func TestBuildFunctionIndex_EmptyCategory(t *testing.T) {
	functions := []testFunc{
		{name: "foo", category: ""},
	}

	index := buildFunctionIndex(functions, func(f testFunc) string { return f.category }, nil)
	assert.Contains(t, index, "## other (1)")
}
