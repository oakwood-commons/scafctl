// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package builder

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"100B", 100, false},
		{"1KB", 1024, false},
		{"50MB", 50 * 1024 * 1024, false},
		{"1GB", 1024 * 1024 * 1024, false},
		{"100", 100, false},
		{"50mb", 50 * 1024 * 1024, false},
		{"invalid", 0, true},
		{"MB", 0, true},
		{"xGB", 0, true}, // invalid: non-numeric prefix
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseByteSize(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFormatByteSize(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{50 * 1024 * 1024, "50.0 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, format.Bytes(tt.input))
		})
	}
}

func newTestCatalogRegistry(t *testing.T) (*catalog.Registry, *catalog.LocalCatalog) {
	t.Helper()
	tmpDir := t.TempDir()
	local, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)
	reg := catalog.NewRegistryWithLocal(local, logr.Discard())
	return reg, local
}

func TestRegistryFetcherAdapter_FetchSolution_InvalidRef(t *testing.T) {
	reg, _ := newTestCatalogRegistry(t)
	adapter := &RegistryFetcherAdapter{Registry: reg}

	_, _, err := adapter.FetchSolution(context.Background(), "")
	require.Error(t, err)
}

func TestRegistryFetcherAdapter_FetchSolution_NotFound(t *testing.T) {
	reg, _ := newTestCatalogRegistry(t)
	adapter := &RegistryFetcherAdapter{Registry: reg}

	_, _, err := adapter.FetchSolution(context.Background(), "nonexistent@1.0.0")
	require.Error(t, err)
}

func TestRegistryFetcherAdapter_FetchSolution_Found(t *testing.T) {
	ctx := context.Background()
	reg, local := newTestCatalogRegistry(t)

	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    "test-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := local.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	adapter := &RegistryFetcherAdapter{Registry: reg}
	content, info, err := adapter.FetchSolution(ctx, "test-sol@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("content"), content)
	assert.Equal(t, "test-sol", info.Reference.Name)
}

func TestRegistryFetcherAdapter_ListSolutions(t *testing.T) {
	ctx := context.Background()
	reg, local := newTestCatalogRegistry(t)

	ref1 := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "list-sol", Version: semver.MustParse("1.0.0")}
	ref2 := catalog.Reference{Kind: catalog.ArtifactKindSolution, Name: "list-sol", Version: semver.MustParse("2.0.0")}
	_, err := local.Store(ctx, ref1, []byte("v1"), nil, nil, false)
	require.NoError(t, err)
	_, err = local.Store(ctx, ref2, []byte("v2"), nil, nil, false)
	require.NoError(t, err)

	adapter := &RegistryFetcherAdapter{Registry: reg}
	results, err := adapter.ListSolutions(ctx, "list-sol")
	require.NoError(t, err)
	assert.Len(t, results, 2)
}

func TestCatalogPluginResolver_ResolvePlugin_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	resolver := &CatalogPluginResolver{Catalog: cat}
	_, err = resolver.ResolvePlugin(context.Background(), "missing-plugin", catalog.ArtifactKindProvider, "")
	require.Error(t, err)
}

func TestCatalogPluginResolver_ResolvePlugin_Found(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindProvider,
		Name:    "my-plugin",
		Version: semver.MustParse("1.0.0"),
	}
	_, err = cat.Store(ctx, ref, []byte("plugin-content"), nil, nil, false)
	require.NoError(t, err)

	resolver := &CatalogPluginResolver{Catalog: cat}
	info, err := resolver.ResolvePlugin(ctx, "my-plugin", catalog.ArtifactKindProvider, "")
	require.NoError(t, err)
	assert.Equal(t, "my-plugin", info.Reference.Name)
}

func TestStoreSolutionArtifact_NoBuildResult(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	result, err := StoreSolutionArtifact(ctx, cat, "my-sol", semver.MustParse("1.0.0"),
		[]byte("solution-content"), nil, StoreOptions{Source: "test.yaml"})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "my-sol", result.Info.Reference.Name)
}

func TestStoreSolutionArtifact_WithBuildResultTar(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	br := &BuildResult{TarData: []byte("fake-tar-data")}
	result, err := StoreSolutionArtifact(ctx, cat, "tar-sol", semver.MustParse("1.0.0"),
		[]byte("solution-content"), br, StoreOptions{})
	require.NoError(t, err)
	assert.Equal(t, "tar-sol", result.Info.Reference.Name)
}

func TestStoreSolutionArtifact_InvalidatesArtifactCache(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	// Pre-populate the artifact cache with a stale entry
	artifactCacheDir := filepath.Join(t.TempDir(), "artifact-cache")
	ttl := time.Hour
	ac := cache.NewArtifactCache(artifactCacheDir, ttl)
	require.NoError(t, ac.Put("solution", "cached-sol", "1.0.0", "", []byte("stale-content"), nil))

	// Verify stale entry exists
	content, _, ok, err := ac.Get("solution", "cached-sol", "1.0.0")
	require.NoError(t, err)
	require.True(t, ok, "stale cache entry should exist before store")
	assert.Equal(t, []byte("stale-content"), content)

	// Store a new artifact with cache invalidation enabled
	result, err := StoreSolutionArtifact(ctx, cat, "cached-sol", semver.MustParse("1.0.0"),
		[]byte("fresh-content"), nil, StoreOptions{
			ArtifactCacheDir: artifactCacheDir,
			ArtifactCacheTTL: ttl,
		})
	require.NoError(t, err)
	assert.Equal(t, "cached-sol", result.Info.Reference.Name)

	// Verify artifact cache entry was invalidated
	_, _, ok, err = ac.Get("solution", "cached-sol", "1.0.0")
	require.NoError(t, err)
	assert.False(t, ok, "artifact cache entry should be invalidated after store")
}

func TestStoreSolutionArtifact_NoCacheDirSkipsInvalidation(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	// Empty ArtifactCacheDir — should not panic or error
	result, err := StoreSolutionArtifact(ctx, cat, "no-cache", semver.MustParse("1.0.0"),
		[]byte("content"), nil, StoreOptions{ArtifactCacheDir: ""})
	require.NoError(t, err)
	assert.Equal(t, "no-cache", result.Info.Reference.Name)
}

func TestStoreSolutionArtifact_WithBuildFingerprint(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	buildCacheDir := filepath.Join(t.TempDir(), "build-cache")

	br := &BuildResult{
		TarData:          []byte("tar"),
		BuildFingerprint: "abc123",
		BuildCacheDir:    buildCacheDir,
		InputFileCount:   5,
	}
	result, err := StoreSolutionArtifact(ctx, cat, "fp-sol", semver.MustParse("2.0.0"),
		[]byte("content"), br, StoreOptions{Source: "test.yaml"})
	require.NoError(t, err)
	assert.Equal(t, "fp-sol", result.Info.Reference.Name)
	assert.True(t, result.CacheWritten)
}

func TestStoreSolutionArtifact_NilVersionReturnsError(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	_, err = StoreSolutionArtifact(ctx, cat, "nil-ver-sol", nil,
		[]byte("content"), nil, StoreOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version is required")
}

func TestStoreSolutionArtifact_NoBuildFingerprint(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	// BuildResult with empty fingerprint — should not write build cache
	br := &BuildResult{TarData: []byte("tar")}
	result, err := StoreSolutionArtifact(ctx, cat, "no-fp", semver.MustParse("1.0.0"),
		[]byte("content"), br, StoreOptions{})
	require.NoError(t, err)
	assert.False(t, result.CacheWritten)
}

func TestStoreSolutionArtifact_ForceOverwrite(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	cat, err := catalog.NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	version := semver.MustParse("1.0.0")

	// Store first time
	_, err = StoreSolutionArtifact(ctx, cat, "force-test", version,
		[]byte("v1-content"), nil, StoreOptions{})
	require.NoError(t, err)

	// Store again without force — should fail
	_, err = StoreSolutionArtifact(ctx, cat, "force-test", version,
		[]byte("v2-content"), nil, StoreOptions{Force: false})
	require.Error(t, err)

	// Store again with force — should succeed
	result, err := StoreSolutionArtifact(ctx, cat, "force-test", version,
		[]byte("v3-content"), nil, StoreOptions{Force: true})
	require.NoError(t, err)
	assert.Equal(t, "force-test", result.Info.Reference.Name)
}
