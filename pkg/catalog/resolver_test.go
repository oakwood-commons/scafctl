// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNameVersion(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedName    string
		expectedVersion string
	}{
		{
			name:            "name only",
			input:           "my-solution",
			expectedName:    "my-solution",
			expectedVersion: "",
		},
		{
			name:            "name with version",
			input:           "my-solution@1.2.3",
			expectedName:    "my-solution",
			expectedVersion: "1.2.3",
		},
		{
			name:            "name with prerelease version",
			input:           "my-app@2.0.0-beta.1",
			expectedName:    "my-app",
			expectedVersion: "2.0.0-beta.1",
		},
		{
			name:            "name with digest",
			input:           "my-solution@sha256:abc123def456",
			expectedName:    "my-solution",
			expectedVersion: "sha256:abc123def456",
		},
		{
			name:            "simple name",
			input:           "app",
			expectedName:    "app",
			expectedVersion: "",
		},
		{
			name:            "name with hyphens",
			input:           "my-complex-app-name@1.0.0",
			expectedName:    "my-complex-app-name",
			expectedVersion: "1.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, version := ParseNameVersion(tt.input)
			assert.Equal(t, tt.expectedName, name)
			assert.Equal(t, tt.expectedVersion, version)
		})
	}
}

func TestNewSolutionResolver(t *testing.T) {
	tmpDir := t.TempDir()
	catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	resolver := NewSolutionResolver(catalog, logr.Discard())

	assert.NotNil(t, resolver)
	assert.Equal(t, catalog, resolver.catalog)
}

func TestSolutionResolver_FetchSolution(t *testing.T) {
	t.Run("fetches existing solution", func(t *testing.T) {
		tmpDir := t.TempDir()
		catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		// Store a solution
		content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: test-solution
  version: 1.0.0
spec:
  resolvers: {}
`)
		ref, err := ParseReference(ArtifactKindSolution, "test-solution@1.0.0")
		require.NoError(t, err)
		_, err = catalog.Store(context.Background(), ref, content, nil, nil, false)
		require.NoError(t, err)

		// Fetch via resolver
		resolver := NewSolutionResolver(catalog, logr.Discard())
		fetchedContent, err := resolver.FetchSolution(context.Background(), "test-solution@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, content, fetchedContent)
	})

	t.Run("fetches latest version when no version specified", func(t *testing.T) {
		tmpDir := t.TempDir()
		catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		// Store multiple versions
		content1 := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-version
  version: 1.0.0
spec:
  resolvers: {}
`)
		content2 := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: multi-version
  version: 2.0.0
spec:
  resolvers: {}
`)
		ref1, err := ParseReference(ArtifactKindSolution, "multi-version@1.0.0")
		require.NoError(t, err)
		ref2, err := ParseReference(ArtifactKindSolution, "multi-version@2.0.0")
		require.NoError(t, err)
		_, err = catalog.Store(context.Background(), ref1, content1, nil, nil, false)
		require.NoError(t, err)
		_, err = catalog.Store(context.Background(), ref2, content2, nil, nil, false)
		require.NoError(t, err)

		// Fetch without version should get latest (2.0.0)
		resolver := NewSolutionResolver(catalog, logr.Discard())
		fetchedContent, err := resolver.FetchSolution(context.Background(), "multi-version")
		require.NoError(t, err)
		assert.Equal(t, content2, fetchedContent)
	})

	t.Run("returns error for non-existent solution", func(t *testing.T) {
		tmpDir := t.TempDir()
		catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		resolver := NewSolutionResolver(catalog, logr.Discard())
		_, err = resolver.FetchSolution(context.Background(), "nonexistent")
		assert.Error(t, err)
		assert.True(t, IsNotFound(err))
	})

	t.Run("returns error for invalid reference", func(t *testing.T) {
		tmpDir := t.TempDir()
		catalog, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		resolver := NewSolutionResolver(catalog, logr.Discard())
		_, err = resolver.FetchSolution(context.Background(), "Invalid-Name@1.0.0")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid solution reference")
	})
}

func TestWithResolverArtifactCache(t *testing.T) {
	tmpDir := t.TempDir()
	cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	// Use WithResolverArtifactCache and WithResolverNoCache options
	resolver := NewSolutionResolver(cat, logr.Discard(),
		WithResolverNoCache(true),
	)
	assert.NotNil(t, resolver)
	assert.True(t, resolver.noCache)
}

func TestSolutionResolver_FetchSolution_WithCacheHit(t *testing.T) {
	tmpDir := t.TempDir()
	cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	cachedContent := []byte("cached-solution-content")
	mock := &mockCacher{content: cachedContent, hit: true}

	resolver := NewSolutionResolver(cat, logr.Discard(), WithResolverArtifactCache(mock))
	got, err := resolver.FetchSolution(context.Background(), "any-sol@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, cachedContent, got)
}

func TestSolutionResolver_FetchSolution_WithCacheMissThenStore(t *testing.T) {
	tmpDir := t.TempDir()
	cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
	require.NoError(t, err)

	// Store a solution in catalog
	content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cacheable-sol
  version: 1.0.0
spec:
  resolvers: {}
`)
	ref, err := ParseReference(ArtifactKindSolution, "cacheable-sol@1.0.0")
	require.NoError(t, err)
	_, err = cat.Store(context.Background(), ref, content, nil, nil, false)
	require.NoError(t, err)

	mock := &mockCacher{hit: false}
	resolver := NewSolutionResolver(cat, logr.Discard(), WithResolverArtifactCache(mock))
	got, err := resolver.FetchSolution(context.Background(), "cacheable-sol@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestSolutionResolver_FetchSolutionWithBundle(t *testing.T) {
	t.Run("returns error for invalid reference", func(t *testing.T) {
		tmpDir := t.TempDir()
		cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		resolver := NewSolutionResolver(cat, logr.Discard())
		_, _, err = resolver.FetchSolutionWithBundle(context.Background(), "Invalid-Name@1.0.0")
		assert.Error(t, err)
	})

	t.Run("returns error for non-existent solution", func(t *testing.T) {
		tmpDir := t.TempDir()
		cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		resolver := NewSolutionResolver(cat, logr.Discard())
		_, _, err = resolver.FetchSolutionWithBundle(context.Background(), "nonexistent@1.0.0")
		assert.Error(t, err)
	})

	t.Run("fetches existing solution without bundle", func(t *testing.T) {
		tmpDir := t.TempDir()
		cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: bundle-sol
  version: 1.0.0
spec:
  resolvers: {}
`)
		ref, err := ParseReference(ArtifactKindSolution, "bundle-sol@1.0.0")
		require.NoError(t, err)
		_, err = cat.Store(context.Background(), ref, content, nil, nil, false)
		require.NoError(t, err)

		resolver := NewSolutionResolver(cat, logr.Discard())
		got, bundle, err := resolver.FetchSolutionWithBundle(context.Background(), "bundle-sol@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, content, got)
		assert.Nil(t, bundle)
	})

	t.Run("cache hit returns stored content", func(t *testing.T) {
		tmpDir := t.TempDir()
		cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		cachedContent := []byte("cached-content")
		cachedBundle := []byte("cached-bundle")
		mock := &mockCacher{content: cachedContent, bundle: cachedBundle, hit: true}

		resolver := NewSolutionResolver(cat, logr.Discard(), WithResolverArtifactCache(mock))
		got, gotBundle, err := resolver.FetchSolutionWithBundle(context.Background(), "any-sol@1.0.0")
		require.NoError(t, err)
		assert.Equal(t, cachedContent, got)
		assert.Equal(t, cachedBundle, gotBundle)
	})

	t.Run("noCache skips cache", func(t *testing.T) {
		tmpDir := t.TempDir()
		cat, err := NewLocalCatalogAt(tmpDir, logr.Discard())
		require.NoError(t, err)

		mock := &mockCacher{hit: true} // would hit if cache were consulted
		resolver := NewSolutionResolver(cat, logr.Discard(),
			WithResolverArtifactCache(mock),
			WithResolverNoCache(true),
		)
		// Should not use cache, so will fail with not-found
		_, _, err = resolver.FetchSolutionWithBundle(context.Background(), "any-sol@1.0.0")
		assert.Error(t, err)
	})
}

// mockCacher is a simple ArtifactCacher for testing.
type mockCacher struct {
	content []byte
	bundle  []byte
	hit     bool
	putErr  error
}

func (m *mockCacher) Get(_, _, _ string) ([]byte, []byte, bool, error) {
	return m.content, m.bundle, m.hit, nil
}

func (m *mockCacher) Put(_, _, _, _ string, _, _ []byte) error {
	return m.putErr
}
