// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalCatalog_ResolveAnyKind(t *testing.T) {
	t.Parallel()

	v1 := semver.MustParse("1.0.0")
	v2 := semver.MustParse("2.0.0")
	const remoteOrigin = "ghcr.io/example-org/catalog"

	t.Run("not found when catalog is empty", func(t *testing.T) {
		t.Parallel()
		cat := newTestCatalog(t)

		_, err := cat.ResolveAnyKind(t.Context(), Reference{Name: "missing", Version: v1})
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
	})

	t.Run("unique match per kind and open version", func(t *testing.T) {
		t.Parallel()
		for _, kind := range []ArtifactKind{ArtifactKindSolution, ArtifactKindProvider, ArtifactKindAuthHandler} {
			t.Run(string(kind), func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				cat := newTestCatalog(t)

				_, err := cat.Store(ctx, Reference{Kind: kind, Name: "my-art", Version: v1, Built: true}, []byte("v1"), nil, nil, false)
				require.NoError(t, err)
				_, err = cat.Store(ctx, Reference{Kind: kind, Name: "my-art", Version: v2, Built: true}, []byte("v2"), nil, nil, false)
				require.NoError(t, err)

				info, err := cat.ResolveAnyKind(ctx, Reference{Name: "my-art"})
				require.NoError(t, err)
				assert.Equal(t, kind, info.Reference.Kind)
				assert.Equal(t, "my-art", info.Reference.Name)
				assert.Equal(t, "2.0.0", info.Reference.Version.String())
			})
		}
	})

	t.Run("explicit kind delegates directly to single-kind resolve", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		cat := newTestCatalog(t)

		_, err := cat.Store(ctx, Reference{Kind: ArtifactKindSolution, Name: "shared", Version: v1, Built: true}, []byte("sol"), nil, nil, false)
		require.NoError(t, err)
		_, err = cat.Store(ctx, Reference{Kind: ArtifactKindProvider, Name: "shared", Version: v1, Built: true}, []byte("prov"), nil, nil, false)
		require.NoError(t, err)

		info, err := cat.ResolveAnyKind(ctx, Reference{Kind: ArtifactKindProvider, Name: "shared", Version: v1})
		require.NoError(t, err)
		assert.Equal(t, ArtifactKindProvider, info.Reference.Kind)
	})

	t.Run("single kind with multiple origins preserves AmbiguousArtifactError", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		cat := newTestCatalog(t)

		_, err := cat.Store(ctx, Reference{Kind: ArtifactKindSolution, Name: "dup-origin", Version: v1, Built: true}, []byte("built"), nil, nil, false)
		require.NoError(t, err)
		_, err = cat.Store(ctx, Reference{Kind: ArtifactKindSolution, Name: "dup-origin", Version: v1, Remote: remoteOrigin}, []byte("remote"), nil, map[string]string{
			AnnotationSourceCanonical: remoteOrigin,
		}, false)
		require.NoError(t, err)

		_, err = cat.ResolveAnyKind(ctx, Reference{Name: "dup-origin", Version: v1})
		require.Error(t, err)

		var ambOrigin *AmbiguousArtifactError
		require.ErrorAs(t, err, &ambOrigin, "single-kind multi-origin must preserve *AmbiguousArtifactError")
		var ambKind *AmbiguousKindError
		assert.NotErrorAs(t, err, &ambKind)
		assert.Len(t, ambOrigin.Candidates(), 2)
	})

	t.Run("multiple kinds returns AmbiguousKindError in canonical order", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		cat := newTestCatalog(t)

		_, err := cat.Store(ctx, Reference{Kind: ArtifactKindSolution, Name: "shared", Version: v1, Built: true}, []byte("sol"), nil, nil, false)
		require.NoError(t, err)
		_, err = cat.Store(ctx, Reference{Kind: ArtifactKindProvider, Name: "shared", Version: v1, Built: true}, []byte("prov"), nil, nil, false)
		require.NoError(t, err)

		_, err = cat.ResolveAnyKind(ctx, Reference{Name: "shared", Version: v1})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrAmbiguousArtifact)

		var ambKind *AmbiguousKindError
		require.ErrorAs(t, err, &ambKind)
		assert.Equal(t, []ArtifactKind{ArtifactKindSolution, ArtifactKindProvider}, ambKind.Kinds)
		assert.Contains(t, ambKind.Error(), "matches multiple kinds (solution, provider)")
	})

	t.Run("multiple kinds where one kind also has multiple origins still reports AmbiguousKindError", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		cat := newTestCatalog(t)

		_, err := cat.Store(ctx, Reference{Kind: ArtifactKindSolution, Name: "shared", Version: v1, Built: true}, []byte("sol-built"), nil, nil, false)
		require.NoError(t, err)
		_, err = cat.Store(ctx, Reference{Kind: ArtifactKindSolution, Name: "shared", Version: v1, Remote: remoteOrigin}, []byte("sol-remote"), nil, map[string]string{
			AnnotationSourceCanonical: remoteOrigin,
		}, false)
		require.NoError(t, err)
		_, err = cat.Store(ctx, Reference{Kind: ArtifactKindAuthHandler, Name: "shared", Version: v1, Built: true}, []byte("auth"), nil, nil, false)
		require.NoError(t, err)

		_, err = cat.ResolveAnyKind(ctx, Reference{Name: "shared", Version: v1})
		require.Error(t, err)

		var ambKind *AmbiguousKindError
		require.ErrorAs(t, err, &ambKind)
		assert.Equal(t, []ArtifactKind{ArtifactKindSolution, ArtifactKindAuthHandler}, ambKind.Kinds)
	})

	t.Run("version narrows across kinds when only one kind holds requested version", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		cat := newTestCatalog(t)

		_, err := cat.Store(ctx, Reference{Kind: ArtifactKindSolution, Name: "shared", Version: v1, Built: true}, []byte("sol"), nil, nil, false)
		require.NoError(t, err)
		_, err = cat.Store(ctx, Reference{Kind: ArtifactKindProvider, Name: "shared", Version: v2, Built: true}, []byte("prov"), nil, nil, false)
		require.NoError(t, err)

		info, err := cat.ResolveAnyKind(ctx, Reference{Name: "shared", Version: v2})
		require.NoError(t, err)
		assert.Equal(t, ArtifactKindProvider, info.Reference.Kind)
		assert.Equal(t, "2.0.0", info.Reference.Version.String())
	})

	t.Run("invalid empty name returns ErrInvalidQuery", func(t *testing.T) {
		t.Parallel()
		cat := newTestCatalog(t)

		_, err := cat.ResolveAnyKind(t.Context(), Reference{})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidQuery)
	})
}

func BenchmarkLocalCatalog_ResolveAnyKind(b *testing.B) {
	ctx := b.Context()
	cat, err := NewLocalCatalogAt(b.TempDir(), logr.Discard())
	if err != nil {
		b.Fatal(err)
	}
	v1 := semver.MustParse("1.0.0")
	if _, err := cat.Store(ctx, Reference{Kind: ArtifactKindProvider, Name: "bench-plugin", Version: v1, Built: true}, []byte("bin"), nil, nil, false); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = cat.ResolveAnyKind(ctx, Reference{Name: "bench-plugin", Version: v1})
	}
}
