// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRemoteSolutionResolver(t *testing.T) {
	t.Parallel()

	t.Run("sets all fields from config", func(t *testing.T) {
		t.Parallel()
		handlerFunc := func(registry string) auth.Handler { return nil }
		resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
			CredentialStore: nil,
			AuthHandlerFunc: handlerFunc,
			Insecure:        true,
			Logger:          logr.Discard(),
		})

		require.NotNil(t, resolver)
		assert.True(t, resolver.insecure)
		assert.Nil(t, resolver.credStore)
		assert.NotNil(t, resolver.authHandlerFunc)
	})

	t.Run("nil auth handler func is accepted", func(t *testing.T) {
		t.Parallel()
		resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
			Logger: logr.Discard(),
		})

		require.NotNil(t, resolver)
		assert.Nil(t, resolver.authHandlerFunc)
	})
}

func TestRemoteSolutionResolver_FetchRemoteSolution_InvalidRef(t *testing.T) {
	t.Parallel()

	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		Logger: logr.Discard(),
	})

	tests := []struct {
		name string
		ref  string
	}{
		{"empty ref", ""},
		{"whitespace only", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, bundleData, err := resolver.FetchRemoteSolution(t.Context(), tt.ref)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid remote reference")
			assert.Nil(t, content)
			assert.Nil(t, bundleData)
		})
	}
}

func TestRemoteSolutionResolver_FetchRemoteSolution_DefaultsToSolutionKind(t *testing.T) {
	t.Parallel()

	// We cannot easily test the full fetch path without a real registry.
	// Instead, verify the function parses the ref correctly by using a
	// reference with an explicit kind path segment so it reaches
	// NewRemoteCatalog. We test with a canceled context to avoid network calls.
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		Insecure: true,
		Logger:   logr.Discard(),
	})

	// Cancel immediately so no network I/O occurs.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit@1.0.0")
	require.Error(t, err)
	// Error should NOT be about parsing — it should be a fetch/context error
	assert.NotContains(t, err.Error(), "invalid remote reference")
}

func TestRemoteSolutionResolver_FetchRemoteSolution_WithAuthHandlerFunc(t *testing.T) {
	t.Parallel()

	called := false
	mockHandler := auth.NewMockHandler("github")
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		AuthHandlerFunc: func(registry string) auth.Handler {
			called = true
			assert.Equal(t, "localhost:9999", registry)
			return mockHandler
		},
		Insecure: true,
		Logger:   logr.Discard(),
	})

	// Cancel immediately so no network I/O occurs.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit@1.0.0")
	require.Error(t, err)
	assert.True(t, called, "auth handler func should have been called")
}

// remoteMockCacher is a test double for ArtifactCacher used by remote resolver tests.
type remoteMockCacher struct {
	getContent []byte
	getBundle  []byte
	getLayers  map[string][]byte
	hit        bool
	getErr     error
	getKind    string
	getName    string
	getVersion string
	putErr     error
	putCalled  bool
	putKind    string
	putName    string
	putVersion string
	putDigest  string
	putContent []byte
	putLayers  map[string][]byte
}

func (m *remoteMockCacher) Get(kind, name, version string) ([]byte, map[string][]byte, bool, error) {
	m.getKind = kind
	m.getName = name
	m.getVersion = version
	layers := m.getLayers
	if layers == nil && len(m.getBundle) > 0 {
		layers = map[string][]byte{MediaTypeSolutionBundle: m.getBundle}
	}
	return m.getContent, layers, m.hit, m.getErr
}

func (m *remoteMockCacher) Put(kind, name, version, digest string, content []byte, layers map[string][]byte) error {
	m.putCalled = true
	m.putKind = kind
	m.putName = name
	m.putVersion = version
	m.putDigest = digest
	m.putContent = content
	m.putLayers = layers
	return m.putErr
}

func TestRemoteSolutionResolver_FetchRemoteSolution_CacheHit(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{
		getContent: []byte("cached-content"),
		getBundle:  []byte("cached-bundle"),
		hit:        true,
	}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	content, bundle, err := resolver.FetchRemoteSolution(t.Context(), "localhost:9999/myorg/starter-kit@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("cached-content"), content)
	assert.Equal(t, []byte("cached-bundle"), bundle)
	assert.False(t, mc.putCalled, "should not write to cache on hit")

	// Verify correct cache key was passed.
	assert.Equal(t, "solution", mc.getKind)
	assert.Equal(t, "localhost:9999/myorg/starter-kit", mc.getName)
	assert.Equal(t, "1.0.0", mc.getVersion)
}

func TestRemoteSolutionResolver_FetchRemoteSolution_CacheMiss(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{hit: false}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	// Cancel immediately so no network I/O occurs — we just verify the cache
	// was checked and the fetch was attempted (not short-circuited).
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit@1.0.0")
	require.Error(t, err, "should fail due to canceled context, not cache")
	assert.NotContains(t, err.Error(), "invalid remote reference")

	// Verify correct cache key was passed to Get.
	assert.Equal(t, "solution", mc.getKind)
	assert.Equal(t, "localhost:9999/myorg/starter-kit", mc.getName)
	assert.Equal(t, "1.0.0", mc.getVersion)
	assert.False(t, mc.putCalled, "Put should not be called when fetch fails")
}

func TestRemoteSolutionResolver_FetchRemoteSolution_NoCacheBypass(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{
		getContent: []byte("should-not-return"),
		hit:        true,
	}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		NoCache:       true,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	// Even though cache would hit, noCache bypasses it entirely.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit@1.0.0")
	require.Error(t, err, "should attempt fetch (not return cached), then fail on canceled ctx")
}

func TestRemoteSolutionResolver_FetchRemoteSolution_NilCacheBackwardCompat(t *testing.T) {
	t.Parallel()

	// No ArtifactCache set — should behave exactly as before (no panic, no cache).
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		Insecure: true,
		Logger:   logr.Discard(),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit@1.0.0")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "invalid remote reference")
}

func TestRemoteSolutionResolver_FetchRemoteSolution_CacheGetError(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{
		getErr: fmt.Errorf("disk full"),
		hit:    false,
	}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	// Cache Get error is logged and ignored — fetch proceeds normally.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit@1.0.0")
	require.Error(t, err, "should fail on fetch, not on cache error")
	assert.NotContains(t, err.Error(), "disk full")
}

func TestRemoteSolutionResolver_FetchRemoteSolution_CacheEmptyTagNormalized(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{hit: false}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	// Use a ref with no explicit tag — should normalize cache version to "latest".
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolution(ctx, "localhost:9999/myorg/starter-kit")
	require.Error(t, err, "should fail due to canceled context")

	assert.Equal(t, "latest", mc.getVersion, "empty tag should be normalized to 'latest'")
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_InvalidRef(t *testing.T) {
	t.Parallel()

	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		Logger: logr.Discard(),
	})

	content, layers, err := resolver.FetchRemoteSolutionWithLayers(t.Context(), "   ", MediaTypeSolutionBundle)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid remote reference")
	assert.Nil(t, content)
	assert.Nil(t, layers)
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_CacheHitAllLayers(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{
		getContent: []byte("cached-content"),
		getLayers: map[string][]byte{
			MediaTypeSolutionBundle: []byte("cached-bundle"),
			MediaTypeSolutionLock:   []byte("cached-lock"),
		},
		hit: true,
	}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	// All requested layers are present in cache — served without any network I/O.
	content, layers, err := resolver.FetchRemoteSolutionWithLayers(
		t.Context(), "localhost:9999/myorg/starter-kit@1.0.0",
		MediaTypeSolutionBundle, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, []byte("cached-content"), content)
	assert.Equal(t, []byte("cached-bundle"), layers[MediaTypeSolutionBundle])
	assert.Equal(t, []byte("cached-lock"), layers[MediaTypeSolutionLock])
	assert.False(t, mc.putCalled, "should not write to cache on hit")

	assert.Equal(t, "solution", mc.getKind)
	assert.Equal(t, "localhost:9999/myorg/starter-kit", mc.getName)
	assert.Equal(t, "1.0.0", mc.getVersion)
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_CacheHitFiltersToRequested(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{
		getContent: []byte("cached-content"),
		getLayers: map[string][]byte{
			MediaTypeSolutionBundle: []byte("cached-bundle"),
			MediaTypeSolutionLock:   []byte("cached-lock"),
		},
		hit: true,
	}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	// Request only the bundle — the returned map must exclude the lock even
	// though it is present in the cache entry.
	content, layers, err := resolver.FetchRemoteSolutionWithLayers(
		t.Context(), "localhost:9999/myorg/starter-kit@1.0.0", MediaTypeSolutionBundle)
	require.NoError(t, err)
	assert.Equal(t, []byte("cached-content"), content)
	assert.Equal(t, []byte("cached-bundle"), layers[MediaTypeSolutionBundle])
	_, hasLock := layers[MediaTypeSolutionLock]
	assert.False(t, hasLock, "unrequested layer must be filtered out")
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_CacheHitMissingRequestedLayer(t *testing.T) {
	t.Parallel()

	// Cache holds only the bundle, but the caller also wants the lock. The hit
	// is not usable, so the resolver must fall through to the registry.
	mc := &remoteMockCacher{
		getContent: []byte("cached-content"),
		getLayers:  map[string][]byte{MediaTypeSolutionBundle: []byte("cached-bundle")},
		hit:        true,
	}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolutionWithLayers(
		ctx, "localhost:9999/myorg/starter-kit@1.0.0",
		MediaTypeSolutionBundle, MediaTypeSolutionLock)
	require.Error(t, err, "should fall through to fetch and fail on canceled ctx")
	assert.NotContains(t, err.Error(), "invalid remote reference")
	assert.False(t, mc.putCalled, "Put should not be called when fetch fails")
	assert.Equal(t, "1.0.0", mc.getVersion, "cache Get should have been consulted")
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_CacheMiss(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{hit: false}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolutionWithLayers(
		ctx, "localhost:9999/myorg/starter-kit@1.0.0", MediaTypeSolutionBundle)
	require.Error(t, err, "should fail due to canceled context, not cache")
	assert.NotContains(t, err.Error(), "invalid remote reference")

	assert.Equal(t, "solution", mc.getKind)
	assert.Equal(t, "localhost:9999/myorg/starter-kit", mc.getName)
	assert.Equal(t, "1.0.0", mc.getVersion)
	assert.False(t, mc.putCalled, "Put should not be called when fetch fails")
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_NoCacheBypass(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{
		getContent: []byte("should-not-return"),
		getLayers:  map[string][]byte{MediaTypeSolutionBundle: []byte("b")},
		hit:        true,
	}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		NoCache:       true,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolutionWithLayers(
		ctx, "localhost:9999/myorg/starter-kit@1.0.0", MediaTypeSolutionBundle)
	require.Error(t, err, "noCache should bypass cache and attempt fetch")
	assert.Empty(t, mc.getKind, "cache Get must not be consulted when noCache is set")
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_NilCache(t *testing.T) {
	t.Parallel()

	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		Insecure: true,
		Logger:   logr.Discard(),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolutionWithLayers(
		ctx, "localhost:9999/myorg/starter-kit@1.0.0", MediaTypeSolutionBundle)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "invalid remote reference")
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_CacheGetErrorIgnored(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{
		getErr: fmt.Errorf("disk full"),
		hit:    false,
	}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolutionWithLayers(
		ctx, "localhost:9999/myorg/starter-kit@1.0.0", MediaTypeSolutionBundle)
	require.Error(t, err, "should fail on fetch, not on cache error")
	assert.NotContains(t, err.Error(), "disk full")
}

func TestRemoteSolutionResolver_FetchRemoteSolutionWithLayers_EmptyTagNormalized(t *testing.T) {
	t.Parallel()

	mc := &remoteMockCacher{hit: false}
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		ArtifactCache: mc,
		Insecure:      true,
		Logger:        logr.Discard(),
	})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := resolver.FetchRemoteSolutionWithLayers(
		ctx, "localhost:9999/myorg/starter-kit", MediaTypeSolutionBundle)
	require.Error(t, err, "should fail due to canceled context")
	assert.Equal(t, "latest", mc.getVersion, "empty tag should be normalized to 'latest'")
}

func TestHasAllLayers(t *testing.T) {
	t.Parallel()

	layers := map[string][]byte{
		MediaTypeSolutionBundle: []byte("b"),
		MediaTypeSolutionLock:   []byte("l"),
	}

	assert.True(t, hasAllLayers(layers, nil), "empty request is trivially satisfied")
	assert.True(t, hasAllLayers(layers, []string{MediaTypeSolutionBundle}))
	assert.True(t, hasAllLayers(layers, []string{MediaTypeSolutionBundle, MediaTypeSolutionLock}))
	assert.False(t, hasAllLayers(layers, []string{"application/vnd.scafctl.solution.other"}))
	assert.False(t, hasAllLayers(map[string][]byte{MediaTypeSolutionBundle: {}}, []string{MediaTypeSolutionBundle}),
		"empty-byte layer counts as absent")
}

func TestSelectLayers(t *testing.T) {
	t.Parallel()

	layers := map[string][]byte{
		MediaTypeSolutionBundle: []byte("b"),
		MediaTypeSolutionLock:   []byte("l"),
	}

	assert.Nil(t, selectLayers(layers, nil), "no requested media types returns nil")

	got := selectLayers(layers, []string{MediaTypeSolutionBundle})
	assert.Equal(t, map[string][]byte{MediaTypeSolutionBundle: []byte("b")}, got)

	// Requested-but-absent media types are simply omitted.
	got = selectLayers(layers, []string{MediaTypeSolutionBundle, "application/vnd.scafctl.solution.other"})
	assert.Equal(t, map[string][]byte{MediaTypeSolutionBundle: []byte("b")}, got)
}
