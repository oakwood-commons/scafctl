// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allowlistMock is a minimal Catalog implementation for testing the allowlist wrapper.
type allowlistMock struct {
	name string
}

func (m *allowlistMock) Name() string { return m.name }
func (m *allowlistMock) Store(_ context.Context, _ Reference, _, _ []byte, _ map[string]string, _ bool, _ ...Layer) (ArtifactInfo, error) {
	return ArtifactInfo{}, nil
}

func (m *allowlistMock) Fetch(_ context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	return []byte("data"), ArtifactInfo{Reference: ref}, nil
}

func (m *allowlistMock) FetchWithBundle(_ context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	return []byte("data"), []byte("bundle"), ArtifactInfo{Reference: ref}, nil
}

func (m *allowlistMock) FetchWithLayer(_ context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	layers := make(map[string][]byte, len(mediaTypes))
	for _, mt := range mediaTypes {
		layers[mt] = []byte("layer")
	}
	return []byte("data"), layers, ArtifactInfo{Reference: ref}, nil
}

func (m *allowlistMock) Resolve(_ context.Context, ref Reference) (ArtifactInfo, error) {
	return ArtifactInfo{Reference: ref}, nil
}

func (m *allowlistMock) List(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
	return []ArtifactInfo{{Reference: Reference{Name: "listed"}}}, nil
}

func (m *allowlistMock) Exists(_ context.Context, _ Reference) (bool, error) {
	return true, nil
}

func (m *allowlistMock) Delete(_ context.Context, _ Reference) error {
	return nil
}

func TestAllowlistCatalog_NilAllowlistPermitsAll(t *testing.T) {
	inner := &allowlistMock{name: "test-catalog"}
	cat := NewAllowlistCatalog(inner, nil)

	ctx := context.Background()
	ref := Reference{Name: "anything", Kind: ArtifactKindProvider}

	info, err := cat.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "anything", info.Reference.Name)

	data, _, err := cat.Fetch(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), data)

	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestAllowlistCatalog_EmptySliceDeniesAll(t *testing.T) {
	inner := &allowlistMock{name: "test-catalog"}
	cat := NewAllowlistCatalog(inner, []string{})

	ctx := context.Background()
	ref := Reference{Name: "anything", Kind: ArtifactKindProvider}

	_, err := cat.Resolve(ctx, ref)
	require.Error(t, err)
	var notFound *ArtifactNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestAllowlistCatalog_AllowedArtifactPassesThrough(t *testing.T) {
	inner := &allowlistMock{name: "official"}
	cat := NewAllowlistCatalog(inner, []string{"exec", "git"})

	ctx := context.Background()

	// Resolve allowed
	info, err := cat.Resolve(ctx, Reference{Name: "exec", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.Equal(t, "exec", info.Reference.Name)

	// Fetch allowed
	data, _, err := cat.Fetch(ctx, Reference{Name: "git", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), data)

	// FetchWithBundle allowed
	content, bundle, _, err := cat.FetchWithBundle(ctx, Reference{Name: "exec", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), content)
	assert.Equal(t, []byte("bundle"), bundle)

	// FetchWithLayer allowed
	layerContent, layers, _, err := cat.FetchWithLayer(ctx, Reference{Name: "exec", Kind: ArtifactKindProvider}, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), layerContent)
	assert.Equal(t, []byte("layer"), layers[MediaTypeSolutionLock])

	// Exists allowed
	exists, err := cat.Exists(ctx, Reference{Name: "exec", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestAllowlistCatalog_RejectedArtifactReturnsNotFound(t *testing.T) {
	inner := &allowlistMock{name: "official"}
	cat := NewAllowlistCatalog(inner, []string{"exec", "git"})

	ctx := context.Background()
	ref := Reference{Name: "malicious-plugin", Kind: ArtifactKindProvider}

	// Resolve rejected
	_, err := cat.Resolve(ctx, ref)
	require.Error(t, err)
	assert.True(t, IsNotFound(err))
	assert.Contains(t, err.Error(), "official")

	// Fetch rejected
	_, _, err = cat.Fetch(ctx, ref)
	require.Error(t, err)
	assert.True(t, IsNotFound(err))

	// FetchWithBundle rejected
	_, _, _, err = cat.FetchWithBundle(ctx, ref) //nolint:dogsled // testing error path only
	require.Error(t, err)
	assert.True(t, IsNotFound(err))

	// FetchWithLayer rejected
	_, _, _, err = cat.FetchWithLayer(ctx, ref, MediaTypeSolutionLock) //nolint:dogsled // testing error path only
	require.Error(t, err)
	assert.True(t, IsNotFound(err))

	// Exists rejected
	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestAllowlistCatalog_ListNotRestricted(t *testing.T) {
	inner := &allowlistMock{name: "official"}
	cat := NewAllowlistCatalog(inner, []string{"exec"})

	ctx := context.Background()
	results, err := cat.List(ctx, ArtifactKindProvider, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "listed", results[0].Reference.Name)
}

func TestAllowlistCatalog_NameDelegatesToInner(t *testing.T) {
	inner := &allowlistMock{name: "my-catalog"}
	cat := NewAllowlistCatalog(inner, []string{"exec"})

	assert.Equal(t, "my-catalog", cat.Name())
}

func TestAllowlistCatalog_InnerReturnsWrapped(t *testing.T) {
	inner := &allowlistMock{name: "inner"}
	cat := NewAllowlistCatalog(inner, nil)

	assert.Equal(t, inner, cat.Inner())
}

func TestAllowlistCatalog_StoreDelegatesToInner(t *testing.T) {
	inner := &allowlistMock{name: "test"}
	cat := NewAllowlistCatalog(inner, []string{"exec"})

	// Store is not gated by allowlist (writes are unrestricted)
	ctx := context.Background()
	ref := Reference{Name: "not-in-allowlist", Kind: ArtifactKindProvider}
	_, err := cat.Store(ctx, ref, []byte("content"), nil, nil, false)
	assert.NoError(t, err)
}

func TestAllowlistCatalog_DeleteDelegatesToInner(t *testing.T) {
	inner := &allowlistMock{name: "test"}
	cat := NewAllowlistCatalog(inner, []string{"exec"})

	ctx := context.Background()
	ref := Reference{Name: "not-in-allowlist", Kind: ArtifactKindProvider}
	err := cat.Delete(ctx, ref)
	assert.NoError(t, err)
}

// allowlistSourceOps exercises every PluginSource operation for name and
// returns the serving catalog.
var allowlistSourceOps = map[string]func(s PluginSource, name string) (string, error){
	"FetchByPlatform": func(s PluginSource, name string) (string, error) {
		_, info, err := s.FetchByPlatform(context.Background(), testRef(name, "1.0.0"), "linux/amd64")
		return info.Catalog, err
	},
	"ListPlatforms": func(s PluginSource, name string) (string, error) {
		platforms, err := s.ListPlatforms(context.Background(), testRef(name, "1.0.0"))
		if len(platforms) > 0 {
			return platforms[0], err
		}
		return "", err
	},
	"ResolveContentDigest": func(s PluginSource, name string) (string, error) {
		info, err := s.ResolveContentDigest(context.Background(), testRef(name, "1.0.0"), "linux/amd64", MediaTypeProviderBinary)
		return info.Catalog, err
	},
	"Find": func(s PluginSource, name string) (string, error) {
		info, err := s.Find(context.Background(), ArtifactQuery{Name: name, Kind: ArtifactKindProvider})
		return info.Catalog, err
	},
}

func TestAllowlistPluginSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		allowed []string
		plugin  string
		want    bool
	}{
		{name: "nil allowlist permits all", allowed: nil, plugin: "anything", want: true},
		{name: "empty allowlist denies all", allowed: []string{}, plugin: "exec", want: false},
		{name: "listed plugin passes", allowed: []string{"exec"}, plugin: "exec", want: true},
		{name: "unlisted plugin is not found", allowed: []string{"exec"}, plugin: "git", want: false},
	}

	for opName, op := range allowlistSourceOps {
		for _, tc := range tests {
			t.Run(opName+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				inner := okSource("inner")
				src := newAllowlistPluginSource(inner, tc.allowed)

				got, err := op(src, tc.plugin)
				if tc.want {
					require.NoError(t, err)
					assert.Equal(t, "inner", got)
					assert.Equal(t, 1, inner.calls)
					return
				}
				require.ErrorIs(t, err, ErrArtifactNotFound, "a denied plugin must look not-found so the chain falls through")
				var nf *ArtifactNotFoundError
				require.ErrorAs(t, err, &nf)
				assert.Equal(t, "inner", nf.Catalog)
				assert.Equal(t, tc.plugin, nf.Reference.Name)
				assert.Zero(t, inner.calls, "a denied plugin must not reach the inner source")
			})
		}
	}
}

func TestAllowlistPluginSource_NameAndUnwrap(t *testing.T) {
	t.Parallel()
	inner := okSource("inner")
	src := newAllowlistPluginSource(inner, nil)

	assert.Equal(t, "inner", src.Name())
	assert.Same(t, inner, src.unwrapPluginSource())
}

type countingCatalogMock struct {
	allowlistMock
	calls int
}

func (m *countingCatalogMock) Fetch(ctx context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	m.calls++
	return m.allowlistMock.Fetch(ctx, ref)
}

func (m *countingCatalogMock) FetchWithBundle(ctx context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	m.calls++
	return m.allowlistMock.FetchWithBundle(ctx, ref)
}

func (m *countingCatalogMock) FetchWithLayer(ctx context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	m.calls++
	return m.allowlistMock.FetchWithLayer(ctx, ref, mediaTypes...)
}

func (m *countingCatalogMock) Resolve(ctx context.Context, ref Reference) (ArtifactInfo, error) {
	m.calls++
	return m.allowlistMock.Resolve(ctx, ref)
}

func (m *countingCatalogMock) List(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
	m.calls++
	return []ArtifactInfo{
		{Reference: Reference{Name: "sol-1", Kind: ArtifactKindSolution}},
		{Reference: Reference{Name: "prov-1", Kind: ArtifactKindProvider}},
	}, nil
}

func (m *countingCatalogMock) Exists(ctx context.Context, ref Reference) (bool, error) {
	m.calls++
	return m.allowlistMock.Exists(ctx, ref)
}

func (m *countingCatalogMock) FetchByPlatform(_ context.Context, ref Reference, _ string) ([]byte, ArtifactInfo, error) {
	m.calls++
	return []byte("bin"), ArtifactInfo{Reference: ref, Catalog: m.name}, nil
}

func (m *countingCatalogMock) ListPlatforms(_ context.Context, _ Reference) ([]string, error) {
	m.calls++
	return []string{"linux/amd64"}, nil
}

func (m *countingCatalogMock) ResolveContentDigest(_ context.Context, ref Reference, _, _ string) (ContentDigestInfo, error) {
	m.calls++
	return ContentDigestInfo{ArtifactInfo: ArtifactInfo{Reference: ref, Catalog: m.name}, ContentDigest: "sha256:abc"}, nil
}

func TestKindFilterCatalog(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("empty allowedKinds permits all kinds", func(t *testing.T) {
		t.Parallel()
		inner := &countingCatalogMock{allowlistMock: allowlistMock{name: "corp"}}
		cat := NewKindFilterCatalog(inner, nil)

		assert.Equal(t, "corp", cat.Name())
		assert.Same(t, inner, cat.Inner())

		for _, kind := range []ArtifactKind{ArtifactKindSolution, ArtifactKindProvider, ArtifactKindAuthHandler} {
			ref := Reference{Name: "item", Kind: kind}
			_, err := cat.Resolve(ctx, ref)
			require.NoError(t, err)
		}
		assert.Equal(t, 3, inner.calls)
	})

	t.Run("solution-only catalog rejects plugin kinds with zero inner calls", func(t *testing.T) {
		t.Parallel()
		inner := &countingCatalogMock{allowlistMock: allowlistMock{name: "corp"}}
		cat := NewKindFilterCatalog(inner, []ArtifactKind{ArtifactKindSolution})

		provRef := Reference{Name: "exec", Kind: ArtifactKindProvider}
		_, err := cat.Resolve(ctx, provRef)
		require.ErrorIs(t, err, ErrArtifactNotFound)

		_, _, err = cat.Fetch(ctx, provRef)
		require.ErrorIs(t, err, ErrArtifactNotFound)

		_, _, _, err = cat.FetchWithBundle(ctx, provRef) //nolint:dogsled // testing error path
		require.ErrorIs(t, err, ErrArtifactNotFound)

		_, _, _, err = cat.FetchWithLayer(ctx, provRef, MediaTypeSolutionLock) //nolint:dogsled // testing error path
		require.ErrorIs(t, err, ErrArtifactNotFound)

		_, _, err = cat.FetchByPlatform(ctx, provRef, "linux/amd64")
		require.ErrorIs(t, err, ErrArtifactNotFound)

		_, err = cat.ListPlatforms(ctx, provRef)
		require.ErrorIs(t, err, ErrArtifactNotFound)

		_, err = cat.ResolveContentDigest(ctx, provRef, "linux/amd64", MediaTypeProviderBinary)
		require.ErrorIs(t, err, ErrArtifactNotFound)

		exists, err := cat.Exists(ctx, provRef)
		require.NoError(t, err)
		assert.False(t, exists)

		listed, err := cat.List(ctx, ArtifactKindProvider, "")
		require.NoError(t, err)
		assert.Empty(t, listed)

		assert.Zero(t, inner.calls, "unserved kind must never touch inner catalog")

		// Served kind passes through.
		solRef := Reference{Name: "starter", Kind: ArtifactKindSolution}
		info, err := cat.Resolve(ctx, solRef)
		require.NoError(t, err)
		assert.Equal(t, "starter", info.Reference.Name)

		// Unscoped List filters returned kinds.
		allListed, err := cat.List(ctx, "", "")
		require.NoError(t, err)
		require.Len(t, allListed, 1)
		assert.Equal(t, ArtifactKindSolution, allListed[0].Reference.Kind)

		// Store and Delete pass through unrestricted.
		_, err = cat.Store(ctx, provRef, []byte("x"), nil, nil, false)
		require.NoError(t, err)
		require.NoError(t, cat.Delete(ctx, provRef))

		// Kindless Reference probes only allowed kinds (solution) and never unserved kinds.
		kindlessRef := Reference{Name: "starter"}
		resolvedKindless, err := cat.Resolve(ctx, kindlessRef)
		require.NoError(t, err)
		assert.Equal(t, "starter", resolvedKindless.Reference.Name)
		_, fetchedKindless, err := cat.Fetch(ctx, kindlessRef)
		require.NoError(t, err)
		assert.Equal(t, "starter", fetchedKindless.Reference.Name)
		_, _, bundledKindless, err := cat.FetchWithBundle(ctx, kindlessRef)
		require.NoError(t, err)
		assert.Equal(t, "starter", bundledKindless.Reference.Name)
		_, _, layeredKindless, err := cat.FetchWithLayer(ctx, kindlessRef, MediaTypeSolutionLock)
		require.NoError(t, err)
		assert.Equal(t, "starter", layeredKindless.Reference.Name)
		existsKindless, err := cat.Exists(ctx, kindlessRef)
		require.NoError(t, err)
		assert.True(t, existsKindless)

		// Non-nil empty slice denies all kinds (fail-closed, matching NewAllowlistCatalog).
		denyAll := NewKindFilterCatalog(inner, []ArtifactKind{})
		_, err = denyAll.Resolve(ctx, solRef)
		require.ErrorIs(t, err, ErrArtifactNotFound)
		_, err = denyAll.Resolve(ctx, kindlessRef)
		require.ErrorIs(t, err, ErrArtifactNotFound)
		emptyListed, err := denyAll.List(ctx, "", "")
		require.NoError(t, err)
		assert.Empty(t, emptyListed)

		// Served platform-aware methods pass through when kind is allowed.
		provCat := NewKindFilterCatalog(inner, []ArtifactKind{ArtifactKindProvider})
		_, _, err = provCat.FetchByPlatform(ctx, provRef, "linux/amd64")
		require.NoError(t, err)
		_, err = provCat.ListPlatforms(ctx, provRef)
		require.NoError(t, err)
		_, err = provCat.ResolveContentDigest(ctx, provRef, "linux/amd64", MediaTypeProviderBinary)
		require.NoError(t, err)
	})
}

func BenchmarkKindFilterPluginSource(b *testing.B) {
	inner := okSource("providers-only")
	src := newKindFilterPluginSource(inner, []ArtifactKind{ArtifactKindProvider})
	ctx := b.Context()
	query := ArtifactQuery{Name: "my-plugin", Kind: ArtifactKindAuthHandler}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = src.Find(ctx, query)
	}
}

func TestKindFilterPluginSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	refWithKind := func(kind ArtifactKind) Reference {
		r := testRef("my-plugin", "1.0.0")
		r.Kind = kind
		return r
	}

	ops := map[string]func(s PluginSource, kind ArtifactKind) (string, error){
		"FetchByPlatform": func(s PluginSource, kind ArtifactKind) (string, error) {
			_, info, err := s.FetchByPlatform(ctx, refWithKind(kind), "linux/amd64")
			return info.Catalog, err
		},
		"ListPlatforms": func(s PluginSource, kind ArtifactKind) (string, error) {
			platforms, err := s.ListPlatforms(ctx, refWithKind(kind))
			if len(platforms) > 0 {
				return platforms[0], err
			}
			return "", err
		},
		"ResolveContentDigest": func(s PluginSource, kind ArtifactKind) (string, error) {
			info, err := s.ResolveContentDigest(ctx, refWithKind(kind), "linux/amd64", MediaTypeProviderBinary)
			return info.Catalog, err
		},
		"Find": func(s PluginSource, kind ArtifactKind) (string, error) {
			info, err := s.Find(ctx, ArtifactQuery{Name: "my-plugin", Kind: kind})
			return info.Catalog, err
		},
	}

	for opName, op := range ops {
		t.Run(opName, func(t *testing.T) {
			t.Parallel()

			inner := okSource("providers-only")
			src := newKindFilterPluginSource(inner, []ArtifactKind{ArtifactKindProvider})
			assert.Equal(t, "providers-only", src.Name())
			assert.Same(t, inner, src.unwrapPluginSource())

			// Unserved kind returns ArtifactNotFoundError with zero inner calls.
			_, err := op(src, ArtifactKindAuthHandler)
			require.ErrorIs(t, err, ErrArtifactNotFound)
			assert.Zero(t, inner.calls)

			// Served kind reaches inner source.
			got, err := op(src, ArtifactKindProvider)
			require.NoError(t, err)
			assert.Equal(t, "providers-only", got)
			assert.Equal(t, 1, inner.calls)
		})
	}
}
