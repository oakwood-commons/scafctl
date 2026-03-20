// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegistry(t *testing.T) (*Registry, *LocalCatalog) {
	t.Helper()
	local := newTestCatalog(t)
	reg := NewRegistryWithLocal(local, logr.Discard())
	return reg, local
}

func TestRegistry_Fetch_CacheRemoteArtifact(t *testing.T) {
	ctx := context.Background()

	t.Run("caches remote fetch into local catalog", func(t *testing.T) {
		reg, local := newTestRegistry(t)
		reg.SetCacheRemoteArtifacts(true)

		// Set up a "remote" catalog with an artifact
		remote := newTestCatalog(t)
		// Override name to distinguish from local
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "remote-sol",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: remote-sol\nversion: 1.0.0")
		_, err := remote.Store(ctx, ref, content, nil, nil, false)
		require.NoError(t, err)

		reg.AddCatalog(remote)

		// Fetch should succeed from the remote catalog
		fetched, info, err := reg.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, fetched)
		assert.Equal(t, "remote-sol", info.Reference.Name)

		// The artifact should now be cached in the local catalog
		localContent, _, err := local.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, localContent)
	})

	t.Run("does not cache when disabled", func(t *testing.T) {
		reg, local := newTestRegistry(t)
		reg.SetCacheRemoteArtifacts(false)

		// Set up a "remote" catalog with an artifact
		remote := newTestCatalog(t)
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "remote-sol-2",
			Version: semver.MustParse("2.0.0"),
		}
		content := []byte("name: remote-sol-2\nversion: 2.0.0")
		_, err := remote.Store(ctx, ref, content, nil, nil, false)
		require.NoError(t, err)

		reg.AddCatalog(remote)

		// Fetch should succeed from the remote catalog
		fetched, _, err := reg.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, fetched)

		// The artifact should NOT be in the local catalog
		_, _, err = local.Fetch(ctx, ref)
		assert.Error(t, err)
		assert.True(t, IsArtifactNotFoundError(err))
	})

	t.Run("local fetch does not trigger caching", func(t *testing.T) {
		reg, local := newTestRegistry(t)
		reg.SetCacheRemoteArtifacts(true)

		// Store directly in local
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "local-sol",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: local-sol\nversion: 1.0.0")
		_, err := local.Store(ctx, ref, content, nil, nil, false)
		require.NoError(t, err)

		// Fetch from local — should succeed without attempting a re-store
		fetched, _, err := reg.Fetch(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, fetched)
	})
}

func TestRegistry_FetchWithBundle_CacheRemoteArtifact(t *testing.T) {
	ctx := context.Background()

	t.Run("caches remote fetch with bundle into local catalog", func(t *testing.T) {
		reg, local := newTestRegistry(t)
		reg.SetCacheRemoteArtifacts(true)

		// Set up a "remote" catalog with an artifact + bundle
		remote := newTestCatalog(t)
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "bundled-sol",
			Version: semver.MustParse("1.0.0"),
		}
		content := []byte("name: bundled-sol\nversion: 1.0.0")
		bundle := []byte("fake-tar-data")
		_, err := remote.Store(ctx, ref, content, bundle, nil, false)
		require.NoError(t, err)

		reg.AddCatalog(remote)

		// FetchWithBundle should succeed from remote
		fetched, fetchedBundle, info, err := reg.FetchWithBundle(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, fetched)
		assert.Equal(t, bundle, fetchedBundle)
		assert.Equal(t, "bundled-sol", info.Reference.Name)

		// The artifact should now be cached locally
		localContent, localBundle, _, err := local.FetchWithBundle(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, content, localContent)
		assert.Equal(t, bundle, localBundle)
	})
}

func TestRegistry_SetCacheRemoteArtifacts(t *testing.T) {
	reg, _ := newTestRegistry(t)

	// Default should be false
	assert.False(t, reg.cacheRemoteArtifacts)

	reg.SetCacheRemoteArtifacts(true)
	assert.True(t, reg.cacheRemoteArtifacts)

	reg.SetCacheRemoteArtifacts(false)
	assert.False(t, reg.cacheRemoteArtifacts)
}

func TestRegistry_Local(t *testing.T) {
	reg, local := newTestRegistry(t)
	assert.Same(t, local, reg.Local())
}

func TestRegistry_Catalogs(t *testing.T) {
	reg, local := newTestRegistry(t)

	catalogs := reg.Catalogs()
	require.Len(t, catalogs, 1)
	assert.Equal(t, local.Name(), catalogs[0].Name())

	remote := newTestCatalog(t)
	reg.AddCatalog(remote)

	catalogs = reg.Catalogs()
	assert.Len(t, catalogs, 2)
}

func TestRegistry_Resolve_Found(t *testing.T) {
	ctx := context.Background()
	reg, local := newTestRegistry(t)

	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := local.Store(ctx, ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	info, err := reg.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "my-sol", info.Reference.Name)
}

func TestRegistry_Resolve_NotFound(t *testing.T) {
	ctx := context.Background()
	reg, _ := newTestRegistry(t)

	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "nonexistent",
		Version: semver.MustParse("1.0.0"),
	}

	_, err := reg.Resolve(ctx, ref)
	require.Error(t, err)
	assert.True(t, IsArtifactNotFoundError(err))
}

func TestRegistry_List(t *testing.T) {
	ctx := context.Background()
	reg, local := newTestRegistry(t)

	ref1 := Reference{Kind: ArtifactKindSolution, Name: "sol-a", Version: semver.MustParse("1.0.0")}
	ref2 := Reference{Kind: ArtifactKindSolution, Name: "sol-b", Version: semver.MustParse("2.0.0")}
	_, err := local.Store(ctx, ref1, []byte("content-a"), nil, nil, false)
	require.NoError(t, err)
	_, err = local.Store(ctx, ref2, []byte("content-b"), nil, nil, false)
	require.NoError(t, err)

	results, err := reg.List(ctx, ArtifactKindSolution, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 2)
}

func TestNewRegistry(t *testing.T) {
	reg, err := NewRegistry(logr.Discard())
	require.NoError(t, err)
	require.NotNil(t, reg)
	assert.NotNil(t, reg.Local())
}
