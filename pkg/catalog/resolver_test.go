// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
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

// TestSolutionResolver_EmbedderCatalog verifies the full catalog resolution
// round-trip when the catalog lives at a non-default path (simulating an
// embedder like "cldctl" that sets paths.SetAppName("cldctl")).
//
// This catches regressions where catalog resolution depends on hardcoded
// "scafctl" paths or tag formats.
func TestSolutionResolver_EmbedderCatalog(t *testing.T) {
	ctx := context.Background()

	// Simulate an embedder's catalog at a custom XDG path
	embedderCatalogDir := t.TempDir()
	cat, err := NewLocalCatalogAt(embedderCatalogDir, logr.Discard())
	require.NoError(t, err)

	content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: starter-kit
  version: 0.0.1
spec:
  resolvers: {}
`)

	ref, err := ParseReference(ArtifactKindSolution, "starter-kit@0.0.1")
	require.NoError(t, err)
	_, err = cat.Store(ctx, ref, content, nil, nil, false)
	require.NoError(t, err)

	resolver := NewSolutionResolver(cat, logr.Discard())

	t.Run("bare name resolves", func(t *testing.T) {
		got, err := resolver.FetchSolution(ctx, "starter-kit")
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("name@version resolves", func(t *testing.T) {
		got, err := resolver.FetchSolution(ctx, "starter-kit@0.0.1")
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("FetchSolutionWithBundle resolves", func(t *testing.T) {
		got, bundle, err := resolver.FetchSolutionWithBundle(ctx, "starter-kit@0.0.1")
		require.NoError(t, err)
		assert.Equal(t, content, got)
		assert.Nil(t, bundle)
	})
}

// TestSolutionResolver_EmbedderCatalog_MismatchedTag verifies that the resolver
// can fetch solutions whose OCI tags don't match the canonical kind/name:version
// format. This happens when an embedder's "catalog pull" stored an artifact
// before the kind was properly set (tag: "/name:version" instead of
// "solution/name:version").
func TestSolutionResolver_EmbedderCatalog_MismatchedTag(t *testing.T) {
	ctx := context.Background()

	embedderCatalogDir := t.TempDir()
	cat, err := NewLocalCatalogAt(embedderCatalogDir, logr.Discard())
	require.NoError(t, err)

	content := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: starter-kit
  version: 0.0.1
spec:
  resolvers: {}
`)

	// Store with correct kind so annotations are right
	ref, err := ParseReference(ArtifactKindSolution, "starter-kit@0.0.1")
	require.NoError(t, err)
	_, err = cat.Store(ctx, ref, content, nil, nil, false)
	require.NoError(t, err)

	// Re-tag with wrong format to simulate the pre-fix pull bug
	correctTag := "solution/starter-kit:0.0.1"
	wrongTag := "/starter-kit:0.0.1"
	desc, err := cat.store.Resolve(ctx, correctTag)
	require.NoError(t, err)
	require.NoError(t, cat.store.Tag(ctx, desc, wrongTag))
	require.NoError(t, cat.store.Untag(ctx, correctTag))

	resolver := NewSolutionResolver(cat, logr.Discard())

	t.Run("bare name resolves despite mismatched tag", func(t *testing.T) {
		got, err := resolver.FetchSolution(ctx, "starter-kit")
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("name@version resolves despite mismatched tag", func(t *testing.T) {
		got, err := resolver.FetchSolution(ctx, "starter-kit@0.0.1")
		require.NoError(t, err)
		assert.Equal(t, content, got)
	})

	t.Run("FetchSolutionWithBundle resolves despite mismatched tag", func(t *testing.T) {
		got, bundle, err := resolver.FetchSolutionWithBundle(ctx, "starter-kit@0.0.1")
		require.NoError(t, err)
		assert.Equal(t, content, got)
		assert.Nil(t, bundle)
	})
}

func TestSolutionResolver_RemoteFallback_FetchSolution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	remote := newMockCatalog("remote-reg")

	ref := Reference{Kind: ArtifactKindSolution, Name: "my-sol", Version: semver.MustParse("1.0.0")}
	remote.addArtifact(ref, []byte("remote-content"), nil)

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	// Should fetch from remote and auto-store locally.
	content, err := resolver.FetchSolution(ctx, "my-sol@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("remote-content"), content)

	// Verify it was stored in local catalog.
	_, ok := local.artifacts[ref.String()]
	assert.True(t, ok, "artifact should be auto-stored in local catalog")
}

func TestSolutionResolver_RemoteFallback_SetsOriginAnnotation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	var storedAnnotations map[string]string
	local := newMockCatalog("local")
	local.storeFunc = func(_ context.Context, _ Reference, _, _ []byte, annotations map[string]string, _ bool) (ArtifactInfo, error) {
		storedAnnotations = annotations
		return ArtifactInfo{}, nil
	}
	remote := newMockCatalog("remote-reg")

	ref := Reference{Kind: ArtifactKindSolution, Name: "origin-sol", Version: semver.MustParse("1.0.0")}
	remote.addArtifact(ref, []byte("remote-data"), nil)

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	_, err := resolver.FetchSolution(ctx, "origin-sol@1.0.0")
	require.NoError(t, err)
	require.NotNil(t, storedAnnotations)
	assert.Equal(t, "auto-cached from remote-reg", storedAnnotations[AnnotationOrigin])
}

func TestSolutionResolver_RemoteFallback_FetchSolutionWithBundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	remote := newMockCatalog("remote-reg")

	ref := Reference{Kind: ArtifactKindSolution, Name: "bundled-sol", Version: semver.MustParse("2.0.0")}
	remote.artifacts[ref.String()] = mockArtifact{
		content:    []byte("sol-content"),
		bundleData: []byte("bundle-tar"),
		info:       ArtifactInfo{Reference: ref, Catalog: "remote-reg"},
	}

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	content, bundle, err := resolver.FetchSolutionWithBundle(ctx, "bundled-sol@2.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("sol-content"), content)
	assert.Equal(t, []byte("bundle-tar"), bundle)
}

func TestSolutionResolver_RemoteFallback_LocalHitSkipsRemote(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	remote := newMockCatalog("remote-reg")

	ref := Reference{Kind: ArtifactKindSolution, Name: "local-sol", Version: semver.MustParse("1.0.0")}
	local.addArtifact(ref, []byte("local-content"), nil)
	remote.addArtifact(ref, []byte("remote-content"), nil)

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	content, err := resolver.FetchSolution(ctx, "local-sol@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("local-content"), content, "should use local, not remote")
}

func TestSolutionResolver_RemoteFallback_AllMiss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	remote := newMockCatalog("remote-reg")

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	_, err := resolver.FetchSolution(ctx, "nonexistent@1.0.0")
	require.Error(t, err)
	assert.True(t, IsNotFound(err), "should return a typed not-found error")

	var notFoundErr *ArtifactNotFoundError
	require.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "local", notFoundErr.Catalog)
}

func TestSolutionResolver_RemoteFallback_AuthErrorContinuesToNext(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	authErr := fmt.Errorf("401 unauthorized: bad credentials")
	remote1 := newMockCatalog("broken-auth")
	remote1.fetchFunc = func(_ context.Context, _ Reference) ([]byte, ArtifactInfo, error) {
		return nil, ArtifactInfo{}, authErr
	}
	remote2 := newMockCatalog("good-reg")
	ref := Reference{Kind: ArtifactKindSolution, Name: "my-sol", Version: semver.MustParse("1.0.0")}
	remote2.addArtifact(ref, []byte("from-good-reg"), nil)

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote1, remote2}),
	)

	content, err := resolver.FetchSolution(ctx, "my-sol@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("from-good-reg"), content)
}

func TestSolutionResolver_RemoteFallback_AuthErrorReportedWhenAllFail(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	remote1 := newMockCatalog("broken-auth")
	remote1.fetchFunc = func(_ context.Context, _ Reference) ([]byte, ArtifactInfo, error) {
		return nil, ArtifactInfo{}, fmt.Errorf("401 unauthorized: bad credentials")
	}
	remote2 := newMockCatalog("also-broken")
	remote2.fetchFunc = func(_ context.Context, _ Reference) ([]byte, ArtifactInfo, error) {
		return nil, ArtifactInfo{}, fmt.Errorf("403 forbidden")
	}

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote1, remote2}),
	)

	_, err := resolver.FetchSolution(ctx, "my-sol@1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
	assert.False(t, IsNotFound(err), "auth errors should not be masked as not-found")
}

func TestSolutionResolver_RemoteFallback_AuthErrorContinuesToNext_WithBundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	remote1 := newMockCatalog("broken-auth")
	remote1.fetchWithBundleFunc = func(_ context.Context, _ Reference) ([]byte, []byte, ArtifactInfo, error) {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("403 forbidden")
	}
	remote2 := newMockCatalog("good-reg")
	ref := Reference{Kind: ArtifactKindSolution, Name: "my-sol", Version: semver.MustParse("1.0.0")}
	remote2.addArtifact(ref, []byte("from-good-reg"), nil)

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote1, remote2}),
	)

	content, _, err := resolver.FetchSolutionWithBundle(ctx, "my-sol@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("from-good-reg"), content)
}

func TestSolutionResolver_RemoteFallback_AuthErrorReportedWhenAllFail_WithBundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	remote1 := newMockCatalog("broken-auth")
	remote1.fetchWithBundleFunc = func(_ context.Context, _ Reference) ([]byte, []byte, ArtifactInfo, error) {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("401 unauthorized")
	}
	remote2 := newMockCatalog("also-broken")
	remote2.fetchWithBundleFunc = func(_ context.Context, _ Reference) ([]byte, []byte, ArtifactInfo, error) {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("403 forbidden")
	}

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote1, remote2}),
	)

	_, _, err := resolver.FetchSolutionWithBundle(ctx, "my-sol@1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unauthorized")
	assert.False(t, IsNotFound(err), "auth errors should not be masked as not-found")
}

func TestSolutionResolver_RemoteFallback_MultipleRemotes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	remote1 := newMockCatalog("reg-1")
	remote2 := newMockCatalog("reg-2")

	ref := Reference{Kind: ArtifactKindSolution, Name: "only-in-reg2", Version: semver.MustParse("1.0.0")}
	remote2.addArtifact(ref, []byte("from-reg2"), nil)

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote1, remote2}),
	)

	content, err := resolver.FetchSolution(ctx, "only-in-reg2@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, []byte("from-reg2"), content)
}

func TestSolutionResolver_LocalNonNotFoundError_NoFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	local.fetchFunc = func(_ context.Context, _ Reference) ([]byte, ArtifactInfo, error) {
		return nil, ArtifactInfo{}, fmt.Errorf("corrupted OCI layout")
	}

	remote := newMockCatalog("remote-reg")
	ref := Reference{Kind: ArtifactKindSolution, Name: "my-sol", Version: semver.MustParse("1.0.0")}
	remote.addArtifact(ref, []byte("remote-content"), nil)

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	_, err := resolver.FetchSolution(ctx, "my-sol@1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "corrupted OCI layout")
	assert.False(t, IsNotFound(err), "non-not-found errors should not be masked")
}

func TestSolutionResolver_LocalNonNotFoundError_NoFallback_WithBundle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	local := newMockCatalog("local")
	local.fetchWithBundleFunc = func(_ context.Context, _ Reference) ([]byte, []byte, ArtifactInfo, error) {
		return nil, nil, ArtifactInfo{}, fmt.Errorf("corrupted OCI layout")
	}

	remote := newMockCatalog("remote-reg")
	ref := Reference{Kind: ArtifactKindSolution, Name: "my-sol", Version: semver.MustParse("1.0.0")}
	remote.addArtifact(ref, []byte("remote-content"), nil)

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	_, _, err := resolver.FetchSolutionWithBundle(ctx, "my-sol@1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "corrupted OCI layout")
	assert.False(t, IsNotFound(err), "non-not-found errors should not be masked")
}

func TestIsNewerVersion(t *testing.T) {
	v1 := semver.MustParse("1.0.0")
	v2 := semver.MustParse("2.0.0")

	tests := []struct {
		name          string
		remoteVersion *semver.Version
		localVersion  *semver.Version
		want          bool
	}{
		{name: "remote newer", remoteVersion: v2, localVersion: v1, want: true},
		{name: "remote same", remoteVersion: v1, localVersion: v1, want: false},
		{name: "remote older", remoteVersion: v1, localVersion: v2, want: false},
		{name: "remote nil", remoteVersion: nil, localVersion: v1, want: false},
		{name: "local nil", remoteVersion: v1, localVersion: nil, want: true},
		{name: "both nil", remoteVersion: nil, localVersion: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := ArtifactInfo{Reference: Reference{Version: tt.remoteVersion}}
			local := ArtifactInfo{Reference: Reference{Version: tt.localVersion}}
			assert.Equal(t, tt.want, isNewerVersion(remote, local))
		})
	}
}

func TestFetchSolution_LatestChecksRemote(t *testing.T) {
	ctx := context.Background()

	// Local has v1.0.0
	local := newMockCatalog("local")
	localRef := Reference{Kind: ArtifactKindSolution, Name: "my-app", Version: semver.MustParse("1.0.0")}
	local.addArtifact(localRef, []byte("v1-content"), nil)
	// Make local return v1 for unversioned fetch
	local.fetchFunc = func(_ context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
		return []byte("v1-content"), ArtifactInfo{
			Reference: localRef,
			Digest:    "sha256:local-v1",
			Catalog:   "local",
		}, nil
	}

	// Remote has v2.0.0
	remoteRef := Reference{Kind: ArtifactKindSolution, Name: "my-app", Version: semver.MustParse("2.0.0")}
	remote := newMockCatalog("remote-reg")
	remote.resolveFunc = func(_ context.Context, _ Reference) (ArtifactInfo, error) {
		return ArtifactInfo{
			Reference: remoteRef,
			Digest:    "sha256:remote-v2",
			Catalog:   "remote-reg",
		}, nil
	}
	remote.fetchFunc = func(_ context.Context, _ Reference) ([]byte, ArtifactInfo, error) {
		return []byte("v2-content"), ArtifactInfo{
			Reference: remoteRef,
			Digest:    "sha256:remote-v2",
			Catalog:   "remote-reg",
		}, nil
	}

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	// Unversioned fetch should get v2 from remote
	content, err := resolver.FetchSolution(ctx, "my-app")
	require.NoError(t, err)
	assert.Equal(t, "v2-content", string(content))
	assert.Equal(t, "remote-reg", resolver.LastResolvedCatalog())
}

func TestFetchSolution_LatestUsesLocalWhenRemoteUnavailable(t *testing.T) {
	ctx := context.Background()

	local := newMockCatalog("local")
	localRef := Reference{Kind: ArtifactKindSolution, Name: "my-app", Version: semver.MustParse("1.0.0")}
	local.fetchFunc = func(_ context.Context, _ Reference) ([]byte, ArtifactInfo, error) {
		return []byte("v1-content"), ArtifactInfo{
			Reference: localRef,
			Catalog:   "local",
		}, nil
	}

	remote := newMockCatalog("remote-reg")
	remote.resolveFunc = func(_ context.Context, _ Reference) (ArtifactInfo, error) {
		return ArtifactInfo{}, fmt.Errorf("network error")
	}

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	// Should fall back to local gracefully
	content, err := resolver.FetchSolution(ctx, "my-app")
	require.NoError(t, err)
	assert.Equal(t, "v1-content", string(content))
}

func TestFetchSolution_PinnedVersionSkipsRemoteCheck(t *testing.T) {
	ctx := context.Background()

	local := newMockCatalog("local")
	localRef := Reference{Kind: ArtifactKindSolution, Name: "my-app", Version: semver.MustParse("1.0.0")}
	local.addArtifact(localRef, []byte("v1-content"), nil)

	// Remote has v2 but should never be consulted for a pinned version
	remote := newMockCatalog("remote-reg")
	remote.resolveFunc = func(_ context.Context, _ Reference) (ArtifactInfo, error) {
		t.Error("remote.Resolve should not be called for pinned version")
		return ArtifactInfo{}, nil
	}

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	content, err := resolver.FetchSolution(ctx, "my-app@1.0.0")
	require.NoError(t, err)
	assert.Equal(t, "v1-content", string(content))
}

func TestFetchSolution_LatestNoUpgradeWhenSameVersion(t *testing.T) {
	ctx := context.Background()

	v1 := semver.MustParse("1.0.0")
	local := newMockCatalog("local")
	local.fetchFunc = func(_ context.Context, _ Reference) ([]byte, ArtifactInfo, error) {
		return []byte("v1-content"), ArtifactInfo{
			Reference: Reference{Kind: ArtifactKindSolution, Name: "my-app", Version: v1},
			Catalog:   "local",
		}, nil
	}

	remote := newMockCatalog("remote-reg")
	remote.resolveFunc = func(_ context.Context, _ Reference) (ArtifactInfo, error) {
		return ArtifactInfo{
			Reference: Reference{Kind: ArtifactKindSolution, Name: "my-app", Version: v1},
		}, nil
	}
	remote.fetchFunc = func(_ context.Context, _ Reference) ([]byte, ArtifactInfo, error) {
		t.Error("remote.Fetch should not be called when versions match")
		return nil, ArtifactInfo{}, nil
	}

	resolver := NewSolutionResolver(local, logr.Discard(),
		WithResolverRemoteCatalogs([]Catalog{remote}),
	)

	content, err := resolver.FetchSolution(ctx, "my-app")
	require.NoError(t, err)
	assert.Equal(t, "v1-content", string(content))
}
