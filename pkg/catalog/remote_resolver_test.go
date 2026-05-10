// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	scafctlauth "github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRemoteSolutionResolver(t *testing.T) {
	t.Parallel()

	t.Run("sets all fields from config", func(t *testing.T) {
		t.Parallel()
		handlerFunc := func(registry string) scafctlauth.Handler { return nil }
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
	resolver := NewRemoteSolutionResolver(RemoteSolutionResolverConfig{
		AuthHandlerFunc: func(registry string) scafctlauth.Handler {
			called = true
			assert.Equal(t, "localhost:9999", registry)
			return nil
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
}

func (m *remoteMockCacher) Get(kind, name, version string) ([]byte, []byte, bool, error) {
	m.getKind = kind
	m.getName = name
	m.getVersion = version
	return m.getContent, m.getBundle, m.hit, m.getErr
}

func (m *remoteMockCacher) Put(kind, name, version, _ string, _, _ []byte) error {
	m.putCalled = true
	m.putKind = kind
	m.putName = name
	m.putVersion = version
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
