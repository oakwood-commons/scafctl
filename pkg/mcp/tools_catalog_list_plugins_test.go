// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedLocalPluginCatalog stores provider, auth-handler, and solution artifacts
// in an isolated local catalog so catalog_list_plugins has data to return.
func seedLocalPluginCatalog(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	lgr := logr.Discard()
	cat, err := catalog.NewLocalCatalog(lgr)
	require.NoError(t, err)

	store := func(name, version string, kind catalog.ArtifactKind) {
		ref := catalog.Reference{Name: name, Kind: kind, Version: semver.MustParse(version)}
		_, err := cat.Store(ctx, ref, []byte(name+" "+version), nil, nil, false)
		require.NoError(t, err)
	}

	store("github", "1.0.0", catalog.ArtifactKindProvider)
	store("github", "1.2.0", catalog.ArtifactKindProvider)
	store("adfs", "0.5.0", catalog.ArtifactKindAuthHandler)
	// A solution must NOT appear in plugin listings.
	store("my-solution", "3.0.0", catalog.ArtifactKindSolution)
}

type pluginEnvelope struct {
	Plugins []pluginCatalogEntry `json:"plugins"`
	Count   int                  `json:"count"`
}

func decodePluginEnvelope(t *testing.T, result *mcp.CallToolResult) pluginEnvelope {
	t.Helper()
	require.False(t, result.IsError, "unexpected error result")
	var env pluginEnvelope
	require.NoError(t, json.Unmarshal([]byte(extractJSONContent(t, result)), &env))
	return env
}

func callListPlugins(t *testing.T, srv *Server, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Name = "catalog_list_plugins"
	req.Params.Arguments = args
	result, err := srv.handleCatalogListPlugins(context.Background(), req)
	require.NoError(t, err)
	return result
}

func TestHandleCatalogListPlugins_ExcludesSolutions(t *testing.T) {
	setTestXDG(t, t.TempDir())
	seedLocalPluginCatalog(t)

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	env := decodePluginEnvelope(t, callListPlugins(t, srv, map[string]any{"catalog": "local"}))
	require.NotEmpty(t, env.Plugins)
	for _, p := range env.Plugins {
		assert.NotEqual(t, "solution", p.Kind, "solutions must not appear in plugin listing")
		assert.Contains(t, []string{"provider", "auth-handler"}, p.Kind)
	}
	// github (2 versions) + adfs (1) = 3 plugin rows; no solution.
	assert.Equal(t, 3, env.Count)
	assert.Equal(t, len(env.Plugins), env.Count, "count must match plugins length")

	// Output must be deterministically ordered (name, then kind, then version).
	names := make([]string, len(env.Plugins))
	for i, p := range env.Plugins {
		names[i] = p.Name
	}
	assert.True(t, sort.StringsAreSorted(names), "plugin entries must be sorted by name; got %v", names)
	// adfs (auth-handler) sorts before github (provider) by name.
	assert.Equal(t, "adfs", env.Plugins[0].Name)
}

func TestHandleCatalogListPlugins_NameFilter(t *testing.T) {
	setTestXDG(t, t.TempDir())
	seedLocalPluginCatalog(t)

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	env := decodePluginEnvelope(t, callListPlugins(t, srv, map[string]any{"catalog": "local", "name": "github"}))
	require.Len(t, env.Plugins, 2, "both github versions should be returned")
	for _, p := range env.Plugins {
		assert.Equal(t, "github", p.Name)
		assert.Equal(t, "provider", p.Kind)
	}
}

func TestHandleCatalogListPlugins_VersionConstraint(t *testing.T) {
	setTestXDG(t, t.TempDir())
	seedLocalPluginCatalog(t)

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	env := decodePluginEnvelope(t, callListPlugins(t, srv, map[string]any{
		"catalog": "local", "name": "github", "version": ">=1.1.0",
	}))
	require.Len(t, env.Plugins, 1, "only github 1.2.0 satisfies >=1.1.0")
	assert.Equal(t, "1.2.0", env.Plugins[0].Version)
}

func TestHandleCatalogListPlugins_InvalidConstraint(t *testing.T) {
	setTestXDG(t, t.TempDir())
	seedLocalPluginCatalog(t)

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	result := callListPlugins(t, srv, map[string]any{
		"catalog": "local", "name": "github", "version": "@@bogus@@",
	})
	assert.True(t, result.IsError, "an invalid version constraint must return an error result")
}

func TestHandleCatalogListPlugins_SemverVersionOrder(t *testing.T) {
	setTestXDG(t, t.TempDir())

	ctx := context.Background()
	cat, err := catalog.NewLocalCatalog(logr.Discard())
	require.NoError(t, err)
	for _, v := range []string{"2.0.0", "10.0.0", "9.0.0"} {
		ref := catalog.Reference{Name: "bigver", Kind: catalog.ArtifactKindProvider, Version: semver.MustParse(v)}
		_, storeErr := cat.Store(ctx, ref, []byte("bigver "+v), nil, nil, false)
		require.NoError(t, storeErr)
	}

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	env := decodePluginEnvelope(t, callListPlugins(t, srv, map[string]any{"catalog": "local", "name": "bigver"}))
	require.Len(t, env.Plugins, 3)
	// Semver-descending (newest first): 10.0.0 must precede 2.0.0, which a
	// lexicographic sort would get wrong.
	assert.Equal(t, []string{"10.0.0", "9.0.0", "2.0.0"},
		[]string{env.Plugins[0].Version, env.Plugins[1].Version, env.Plugins[2].Version})
}

func TestHandleCatalogListPlugins_EmptyLocalCatalog(t *testing.T) {
	setTestXDG(t, t.TempDir())

	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	env := decodePluginEnvelope(t, callListPlugins(t, srv, map[string]any{"catalog": "local"}))
	assert.Empty(t, env.Plugins)
	assert.Equal(t, 0, env.Count)
}

func TestCatalogListPluginsToolRegistered(t *testing.T) {
	srv, err := NewServer(WithServerVersion("test"))
	require.NoError(t, err)

	tools := srv.mcpServer.ListTools()
	_, ok := tools["catalog_list_plugins"]
	assert.True(t, ok, "catalog_list_plugins tool should be registered")
}
