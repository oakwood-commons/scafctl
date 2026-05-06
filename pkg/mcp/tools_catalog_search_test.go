// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/search"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSolution stores a minimal solution in the local catalog so that
// search/list tools can discover it.
func seedSolution(t *testing.T, name, version string) {
	t.Helper()
	localCatalog, err := catalog.NewLocalCatalog(logr.Discard())
	require.NoError(t, err)

	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, name+"@"+version)
	require.NoError(t, err)

	specYAML := []byte("name: " + name + "\nversion: " + version + "\n")
	_, err = localCatalog.Store(context.Background(), ref, specYAML, nil, map[string]string{
		"org.opencontainers.image.description": "Test solution " + name,
	}, false)
	require.NoError(t, err)
}

// unmarshalResult extracts JSON from a tool result's text content.
func unmarshalResult(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	text := result.Content[0].(mcp.TextContent).Text
	require.NoError(t, json.Unmarshal([]byte(text), target))
}

func TestHandleCatalogSearch_LocalOnly(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	seedSolution(t, "test-solution", "1.0.0")

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "catalog_search"
	request.Params.Arguments = map[string]any{
		"query":   "test",
		"catalog": "local",
	}

	result, err := srv.handleCatalogSearch(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var body map[string]any
	unmarshalResult(t, result, &body)
	assert.Greater(t, body["count"].(float64), float64(0))
}

func TestHandleCatalogSearch_NoResults(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "catalog_search"
	request.Params.Arguments = map[string]any{
		"query":   "nonexistent-xyz-12345",
		"catalog": "local",
	}

	result, err := srv.handleCatalogSearch(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleCatalogListSolutions_LocalOnly(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	seedSolution(t, "list-sol", "1.0.0")

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "catalog_list_solutions"
	request.Params.Arguments = map[string]any{
		"catalog": "local",
	}

	result, err := srv.handleCatalogListSolutions(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var body map[string]any
	unmarshalResult(t, result, &body)
	assert.Greater(t, body["count"].(float64), float64(0))
}

func TestHandleCatalogListSolutions_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_CACHE_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(func() { xdg.Reload() })

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "catalog_list_solutions"
	request.Params.Arguments = map[string]any{
		"catalog": "local",
	}

	result, err := srv.handleCatalogListSolutions(context.Background(), request)
	require.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleCatalogListRegistered_NoConfig(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "catalog_list_registered"

	result, err := srv.handleCatalogListRegistered(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var body map[string]any
	unmarshalResult(t, result, &body)
	// At minimum "local" is always listed.
	assert.GreaterOrEqual(t, body["count"].(float64), float64(1))

	catalogList := body["catalogs"].([]any)
	first := catalogList[0].(map[string]any)
	assert.Equal(t, config.CatalogNameLocal, first["name"])
	assert.Equal(t, "filesystem", first["type"])
}

func TestHandleCatalogListRegistered_WithConfig(t *testing.T) {
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: config.CatalogNameLocal, Type: config.CatalogTypeFilesystem},
			{Name: "my-registry", Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/example/catalog"},
			{Name: config.CatalogNameOfficial, Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/official/catalog"},
		},
		Settings: config.Settings{
			DefaultCatalog: "my-registry",
		},
	}

	srv, err := NewServer(WithServerVersion("test"), WithServerConfig(cfg))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "catalog_list_registered"

	result, err := srv.handleCatalogListRegistered(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var body map[string]any
	unmarshalResult(t, result, &body)
	assert.Equal(t, float64(3), body["count"].(float64))
}

func TestHandleCatalogListRegistered_FallsBackToContextConfig(t *testing.T) {
	// Simulate the case where WithServerConfig was not called (s.config is nil)
	// but the config is available in the parent context.
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: config.CatalogNameLocal, Type: config.CatalogTypeFilesystem},
			{Name: "corp-registry", Type: config.CatalogTypeOCI, URL: "oci://registry.corp.example.com/solutions"},
			{Name: config.CatalogNameOfficial, Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/oakwood-commons"},
		},
		Settings: config.Settings{
			DefaultCatalog: "corp-registry",
		},
	}

	parentCtx := config.WithConfig(context.Background(), cfg)
	srv, err := NewServer(
		WithServerVersion("test"),
		WithServerContext(parentCtx),
		// Intentionally NOT passing WithServerConfig -- simulates the nil-config case.
	)
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	request.Params.Name = "catalog_list_registered"

	result, err := srv.handleCatalogListRegistered(context.Background(), request)
	require.NoError(t, err)
	assert.False(t, result.IsError)

	var body map[string]any
	unmarshalResult(t, result, &body)
	assert.Equal(t, float64(3), body["count"].(float64),
		"should return all catalogs from context config, not just local")

	// Verify corp-registry is included.
	catalogList := body["catalogs"].([]any)
	names := make([]string, len(catalogList))
	for i, c := range catalogList {
		names[i] = c.(map[string]any)["name"].(string)
	}
	assert.Contains(t, names, "corp-registry")
	assert.Contains(t, names, config.CatalogNameOfficial)
	assert.Contains(t, names, config.CatalogNameLocal)
}

func TestHandleCatalogListRegistered_OfficialDisabled(t *testing.T) {
	cfg := &config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: config.CatalogNameLocal, Type: config.CatalogTypeFilesystem},
			{Name: config.CatalogNameOfficial, Type: config.CatalogTypeOCI, URL: "oci://ghcr.io/official/catalog"},
		},
		Settings: config.Settings{
			DisableOfficialCatalog: true,
		},
	}

	srv, err := NewServer(WithServerVersion("test"), WithServerConfig(cfg))
	require.NoError(t, err)

	request := mcp.CallToolRequest{}
	result, err := srv.handleCatalogListRegistered(context.Background(), request)
	require.NoError(t, err)

	var body map[string]any
	unmarshalResult(t, result, &body)
	// Only local, official is disabled.
	assert.Equal(t, float64(1), body["count"].(float64))
}

func TestDeduplicateResults(t *testing.T) {
	t.Parallel()

	results := []search.Result{
		{SolutionEntry: search.SolutionEntry{Name: "alpha"}, Score: 10.0},
		{SolutionEntry: search.SolutionEntry{Name: "alpha"}, Score: 20.0},
		{SolutionEntry: search.SolutionEntry{Name: "beta"}, Score: 5.0},
	}

	deduped := deduplicateResults(results)
	assert.Len(t, deduped, 2)
	// alpha should keep score 20 (highest).
	assert.Equal(t, "alpha", deduped[0].Name)
	assert.Equal(t, 20.0, deduped[0].Score)
	assert.Equal(t, "beta", deduped[1].Name)
}

func TestDeduplicateEntries(t *testing.T) {
	t.Parallel()

	entries := []search.SolutionEntry{
		{Name: "alpha", Version: ""},
		{Name: "alpha", Version: "1.0.0"},
		{Name: "beta", Version: "2.0.0"},
	}

	deduped := deduplicateEntries(entries)
	assert.Len(t, deduped, 2)
	// Prefer entry with version.
	for _, e := range deduped {
		if e.Name == "alpha" {
			assert.Equal(t, "1.0.0", e.Version)
		}
	}
}
