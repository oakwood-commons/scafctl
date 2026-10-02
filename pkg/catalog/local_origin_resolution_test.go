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

// originInfo builds an ArtifactInfo carrying only the fields the origin
// precedence ladder inspects: name, version, digest, and origin.
func originInfo(origin, version, dgst string) ArtifactInfo {
	ref := Reference{Kind: ArtifactKindSolution, Name: "x", Origin: origin}
	if version != "" {
		ref.Version = semver.MustParse(version)
	}
	ref.Digest = dgst
	return ArtifactInfo{Reference: ref, Digest: dgst}
}

func TestMatchesForRef(t *testing.T) {
	t.Parallel()

	v1 := originInfo("local", "1.0.0", "")
	v2 := originInfo("local", "2.0.0", "")
	d1 := originInfo("local", "1.0.0", "sha256:aaa")
	d2 := originInfo("local", "1.0.0", "sha256:bbb")
	all := []ArtifactInfo{v1, v2, d1, d2}

	t.Run("version filter takes precedence over digest", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x", Version: semver.MustParse("2.0.0")}
		got := matchesForRef(ref, all)
		require.Len(t, got, 1)
		assert.Equal(t, "2.0.0", got[0].Reference.Version.String())
	})

	t.Run("digest filter when no version", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x", Digest: "sha256:bbb"}
		got := matchesForRef(ref, all)
		require.Len(t, got, 1)
		assert.Equal(t, "sha256:bbb", got[0].Digest)
	})

	t.Run("bare name matches everything", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x"}
		got := matchesForRef(ref, all)
		assert.Len(t, got, len(all))
	})

	t.Run("no match returns empty", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x", Version: semver.MustParse("9.9.9")}
		assert.Empty(t, matchesForRef(ref, all))
	})
}

func TestSelectByOrigin(t *testing.T) {
	t.Parallel()

	source := originInfo("ghcr.io/source", "1.0.0", "")
	other := originInfo("ghcr.io/other", "1.0.0", "")
	local := originInfo(LocalOrigin, "1.0.0", "")

	t.Run("empty returns -1 without error", func(t *testing.T) {
		t.Parallel()
		idx, err := selectByOrigin(Reference{}, nil)
		require.NoError(t, err)
		assert.Equal(t, -1, idx)
	})

	t.Run("single match returns it", func(t *testing.T) {
		t.Parallel()
		idx, err := selectByOrigin(Reference{}, []ArtifactInfo{source})
		require.NoError(t, err)
		assert.Equal(t, 0, idx)
	})

	t.Run("origin-qualified reference selects exact origin", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x", Origin: "ghcr.io/other"}
		idx, err := selectByOrigin(ref, []ArtifactInfo{source, other})
		require.NoError(t, err)
		assert.Equal(t, 1, idx)
	})

	t.Run("origin-qualified reference with no exact origin returns -1", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x", Origin: "ghcr.io/missing"}
		idx, err := selectByOrigin(ref, []ArtifactInfo{source, other})
		require.NoError(t, err)
		assert.Equal(t, -1, idx)
	})

	t.Run("origin-qualified reference with single wrong-origin candidate returns -1", func(t *testing.T) {
		t.Parallel()
		// A lone candidate must still be rejected when its origin differs from
		// the requested one: the single-match shortcut must not short-circuit
		// origin-exact matching (otherwise Resolve/Delete would act on the wrong
		// artifact).
		ref := Reference{Name: "x", Origin: "ghcr.io/missing"}
		idx, err := selectByOrigin(ref, []ArtifactInfo{source})
		require.NoError(t, err)
		assert.Equal(t, -1, idx)
	})

	t.Run("locally-built copy wins over remote copies", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x"}
		idx, err := selectByOrigin(ref, []ArtifactInfo{source, local, other})
		require.NoError(t, err)
		assert.Equal(t, 1, idx, "the local build (index 1) should win")
	})

	t.Run("LocalOrigin-qualified reference selects the local build exactly", func(t *testing.T) {
		t.Parallel()
		// An explicit LocalOrigin reference is exact: it must select the local
		// build and nothing else.
		ref := Reference{Name: "x", Origin: LocalOrigin}
		idx, err := selectByOrigin(ref, []ArtifactInfo{source, local, other})
		require.NoError(t, err)
		assert.Equal(t, 1, idx)
	})

	t.Run("LocalOrigin-qualified reference with only a remote copy returns -1", func(t *testing.T) {
		t.Parallel()
		// The reserved LocalOrigin is a non-empty (exact) origin: a lone remote
		// candidate must not be selected via the single-match shortcut, or
		// Resolve/Delete would act on the wrong artifact.
		ref := Reference{Name: "x", Origin: LocalOrigin}
		idx, err := selectByOrigin(ref, []ArtifactInfo{source})
		require.NoError(t, err)
		assert.Equal(t, -1, idx)
	})

	t.Run("single remote origin is unambiguous", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x"}
		idx, err := selectByOrigin(ref, []ArtifactInfo{source, source})
		require.NoError(t, err)
		assert.Equal(t, 0, idx)
	})

	t.Run("multiple remote origins are ambiguous", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Name: "x", Version: semver.MustParse("1.0.0")}
		idx, err := selectByOrigin(ref, []ArtifactInfo{source, other})
		assert.Equal(t, -1, idx)
		require.Error(t, err)
		assert.True(t, IsAmbiguousReference(err))

		var ambErr *AmbiguousReferenceError
		require.ErrorAs(t, err, &ambErr)
		assert.Len(t, ambErr.Candidates, 2)
		// The message must list both origin-qualified tags so the user can copy one.
		msg := ambErr.Error()
		assert.Contains(t, msg, "ghcr.io/source/solutions/x:1.0.0")
		assert.Contains(t, msg, "ghcr.io/other/solutions/x:1.0.0")
	})
}

// storePulled stores an artifact as if it had been pulled from a remote origin,
// producing an origin-qualified local tag and origin provenance annotations.
func storePulled(t *testing.T, cat *LocalCatalog, ctx context.Context, name, version, origin string) { //nolint:unparam
	t.Helper()
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    name,
		Version: semver.MustParse(version),
		Origin:  origin,
	}
	annotations := map[string]string{
		AnnotationOrigin:          "pulled from " + origin,
		AnnotationSourceCanonical: origin,
	}
	_, err := cat.Store(ctx, ref, []byte("name: "+name), nil, annotations, false)
	require.NoError(t, err)
}

func TestLocalCatalog_Resolve_MultiOriginAmbiguity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")
	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/other")

	t.Run("short reference is ambiguous", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
		_, err := cat.Resolve(ctx, ref)
		require.Error(t, err)
		assert.True(t, IsAmbiguousReference(err), "expected ambiguous reference error, got %v", err)
	})

	t.Run("origin-qualified reference resolves deterministically", func(t *testing.T) {
		t.Parallel()
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
			Origin:  "ghcr.io/source",
		}
		info, err := cat.Resolve(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/source", info.Reference.Origin)
	})
}

func TestLocalCatalog_Resolve_LocalBuildWinsOverRemotes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")
	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/other")

	// A locally-built copy of the same name+version.
	localRef := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
	_, err := cat.Store(ctx, localRef, []byte("name: my-solution"), nil, nil, false)
	require.NoError(t, err)

	// The short reference now resolves to the local build rather than erroring.
	info, err := cat.Resolve(ctx, localRef)
	require.NoError(t, err)
	assert.Equal(t, LocalOrigin, info.Reference.Origin)
}

func TestLocalCatalog_Exists_MultiOriginIsNotAmbiguous(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")
	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/other")

	// Existence is origin-agnostic: a matching copy from any origin means yes,
	// with no ambiguity error.
	ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.True(t, exists)
}

// TestLocalCatalog_LocalOriginIsExact verifies that the reserved LocalOrigin is
// treated as an exact origin (not as the unqualified/short form): when only a
// remote-origin copy is present, a LocalOrigin-qualified reference must neither
// exist nor resolve, while the unqualified reference still does.
func TestLocalCatalog_LocalOriginIsExact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	// Only a remote-origin copy exists; there is no local build.
	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")

	localRef := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-solution",
		Version: semver.MustParse("1.0.0"),
		Origin:  LocalOrigin,
	}
	shortRef := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-solution",
		Version: semver.MustParse("1.0.0"),
	}

	t.Run("Exists is false for LocalOrigin when only a remote copy exists", func(t *testing.T) {
		t.Parallel()
		exists, err := cat.Exists(ctx, localRef)
		require.NoError(t, err)
		assert.False(t, exists, "a LocalOrigin reference must not match a remote-origin copy")

		exists, err = cat.Exists(ctx, shortRef)
		require.NoError(t, err)
		assert.True(t, exists, "the unqualified reference still matches the remote copy")
	})

	t.Run("Resolve returns not-found for LocalOrigin when only a remote copy exists", func(t *testing.T) {
		t.Parallel()
		_, err := cat.Resolve(ctx, localRef)
		require.Error(t, err)
		assert.True(t, IsArtifactNotFoundError(err), "expected not-found, got %v", err)

		// The unqualified reference resolves to the remote copy.
		info, err := cat.Resolve(ctx, shortRef)
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/source", info.Reference.Origin)
	})
}

func TestLocalCatalog_Delete_MultiOriginAmbiguity(t *testing.T) {
	// This test intentionally does NOT call t.Parallel(): its subtests share and
	// mutate the same catalog and must run serially in declaration order (see
	// the comment on the subtests below), so neither the parent nor the subtests
	// may run in parallel.
	ctx := context.Background()
	cat := newTestCatalog(t)

	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")
	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/other")

	// These subtests share and mutate the same catalog, so they must run
	// serially in declaration order: the ambiguity assertion depends on both
	// copies still being present, which the origin-qualified delete below would
	// otherwise race and break. Do NOT add t.Parallel() here.
	t.Run("short reference refuses to delete non-deterministically", func(t *testing.T) {
		ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
		err := cat.Delete(ctx, ref)
		require.Error(t, err)
		assert.True(t, IsAmbiguousReference(err), "expected ambiguous reference error, got %v", err)

		// Both copies must still exist -- nothing was deleted.
		exists, err := cat.Exists(ctx, ref)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("origin-qualified reference deletes exactly one copy", func(t *testing.T) {
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
			Origin:  "ghcr.io/source",
		}
		require.NoError(t, cat.Delete(ctx, ref))

		// The other origin's copy survives and now resolves unambiguously.
		remaining := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "my-solution",
			Version: semver.MustParse("1.0.0"),
			Origin:  "ghcr.io/other",
		}
		info, err := cat.Resolve(ctx, remaining)
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/other", info.Reference.Origin)
	})
}

// TestLocalCatalog_Delete_OriginQualified_PersistsAcrossInstances mirrors the
// CLI, where each command opens a fresh catalog on the same path. A pulled copy
// stored by one instance must be gone from a subsequent instance's List after
// an origin-qualified Delete by a third instance -- i.e. the Untag persists to
// the on-disk index, not just the in-memory store.
func TestLocalCatalog_Delete_OriginQualified_PersistsAcrossInstances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()

	writer, err := NewLocalCatalogAt(dir, logr.Discard())
	require.NoError(t, err)
	storePulled(t, writer, ctx, "my-solution", "1.0.0", "ghcr.io/source")

	deleter, err := NewLocalCatalogAt(dir, logr.Discard())
	require.NoError(t, err)
	ref := Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-solution",
		Version: semver.MustParse("1.0.0"),
		Origin:  "ghcr.io/source",
	}
	require.NoError(t, deleter.Delete(ctx, ref))

	reader, err := NewLocalCatalogAt(dir, logr.Discard())
	require.NoError(t, err)
	infos, err := reader.List(ctx, ArtifactKindSolution, "my-solution")
	require.NoError(t, err)
	assert.Empty(t, infos, "the pulled copy should be gone from a fresh catalog instance after delete")
}

// TestLocalCatalog_StoreDedup_CoexistsWithPulledCopy verifies the origin-exact
// duplicate guard: a locally-built dedup artifact must be storable even when a
// pulled copy of the same name+version (from a remote origin) already exists.
// The two live under distinct tags -- the local build under the canonical local
// tag, the pulled copy under its origin-qualified tag -- so StoreDedup must not
// reject the build as a spurious duplicate.
func TestLocalCatalog_StoreDedup_CoexistsWithPulledCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	// A pulled copy from a remote origin occupies an origin-qualified tag.
	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")

	// A local dedup build of the same name+version (Origin unset -> local).
	localRef := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
	manifestJSON := []byte(`{"version":2,"root":".","files":[]}`)
	_, err := cat.StoreDedup(ctx, localRef, []byte("name: my-solution"), manifestJSON, nil, nil, nil, false)
	require.NoError(t, err, "local dedup build must coexist with a pulled copy of the same name+version")

	// Both copies are present.
	infos, err := cat.List(ctx, ArtifactKindSolution, "my-solution")
	require.NoError(t, err)
	assert.Len(t, infos, 2, "the local build and the pulled copy must both be stored")

	// The short reference resolves to the local build (it wins over remotes).
	local, err := cat.Resolve(ctx, localRef)
	require.NoError(t, err)
	assert.Equal(t, LocalOrigin, local.Reference.Origin)

	// The pulled copy still resolves via its origin-qualified reference.
	pulled, err := cat.Resolve(ctx, Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-solution",
		Version: semver.MustParse("1.0.0"),
		Origin:  "ghcr.io/source",
	})
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/source", pulled.Reference.Origin)
}

// TestLocalCatalog_Load_CoexistsWithPulledCopy verifies the same origin-exact
// guard for Load: importing a locally-exported artifact must succeed even when a
// pulled copy of the same name+version already exists. Load always writes the
// canonical local tag, so it can only genuinely clobber a local copy -- not the
// pulled copy under its origin-qualified tag.
func TestLocalCatalog_Load_CoexistsWithPulledCopy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Destination catalog already holds a pulled copy from a remote origin.
	dst := newTestCatalog(t)
	storePulled(t, dst, ctx, "my-solution", "1.0.0", "ghcr.io/source")

	// Export a locally-built copy of the same name+version from a source catalog.
	src := newTestCatalog(t)
	localRef := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
	_, err := src.Store(ctx, localRef, []byte("name: my-solution"), nil, nil, false)
	require.NoError(t, err)
	tarPath := t.TempDir() + "/my-solution.tar"
	_, err = src.Save(ctx, "my-solution", "1.0.0", tarPath)
	require.NoError(t, err)

	// Loading into the destination must not collide with the pulled copy.
	_, err = dst.Load(ctx, tarPath, false)
	require.NoError(t, err, "loading a local copy must coexist with a pulled copy of the same name+version")

	infos, err := dst.List(ctx, ArtifactKindSolution, "my-solution")
	require.NoError(t, err)
	assert.Len(t, infos, 2, "the loaded local copy and the pulled copy must both be stored")
}

// TestLocalCatalog_Store_NormalizesOriginFromCanonical covers the cache-write
// path (storeLocally / cacheArtifact): callers leave ref.Origin empty but set
// AnnotationSourceCanonical to the remote origin. Store must normalize the
// origin from that annotation so the pulled copy lands under its
// origin-qualified tag instead of clobbering the bare local tag (which would
// overwrite a local build or another origin's cached copy).
func TestLocalCatalog_Store_NormalizesOriginFromCanonical(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	// Simulate a cache write: ref.Origin empty, canonical names the origin.
	ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
	annotations := map[string]string{
		AnnotationOrigin:          "auto-cached from my-remote",
		AnnotationSourceCanonical: "ghcr.io/source",
	}
	info, err := cat.Store(ctx, ref, []byte("name: my-solution"), nil, annotations, false)
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/source", info.Reference.Origin, "origin must be normalized from the canonical source")

	// A subsequent local build of the same name+version must not be rejected as
	// a duplicate and must not clobber the pulled copy.
	localRef := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
	_, err = cat.Store(ctx, localRef, []byte("name: my-solution"), nil, nil, false)
	require.NoError(t, err, "a local build must coexist with the origin-qualified cached copy")

	infos, err := cat.List(ctx, ArtifactKindSolution, "my-solution")
	require.NoError(t, err)
	assert.Len(t, infos, 2, "the cached copy and the local build must both be stored")

	// The pulled copy still resolves via its origin-qualified reference.
	pulled, err := cat.Resolve(ctx, Reference{
		Kind:    ArtifactKindSolution,
		Name:    "my-solution",
		Version: semver.MustParse("1.0.0"),
		Origin:  "ghcr.io/source",
	})
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/source", pulled.Reference.Origin)
}

// TestLocalCatalog_Resolve_NoVersion_MultiOriginAmbiguity covers the
// unversioned ("latest") resolution path: origin precedence must apply among
// the artifacts sharing the highest version, so an unqualified request across
// two origins is ambiguous and an origin-qualified request resolves its own
// origin -- rather than silently returning whichever copy sorted first.
func TestLocalCatalog_Resolve_NoVersion_MultiOriginAmbiguity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")
	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/other")

	t.Run("unversioned short reference is ambiguous across origins", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution"}
		_, err := cat.Resolve(ctx, ref)
		require.Error(t, err)
		assert.True(t, IsAmbiguousReference(err), "expected ambiguous reference error, got %v", err)
	})

	t.Run("unversioned origin-qualified reference resolves deterministically", func(t *testing.T) {
		t.Parallel()
		ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "ghcr.io/other"}
		info, err := cat.Resolve(ctx, ref)
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io/other", info.Reference.Origin)
	})
}

// TestLocalCatalog_Resolve_NoVersion_LocalBuildWins verifies the unversioned
// path honors the "local build shadows remotes" precedence: a bare "latest"
// request resolves to the local build even when remote copies of the same
// version are present.
func TestLocalCatalog_Resolve_NoVersion_LocalBuildWins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")
	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/other")

	localRef := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Version: semver.MustParse("1.0.0")}
	_, err := cat.Store(ctx, localRef, []byte("name: my-solution"), nil, nil, false)
	require.NoError(t, err)

	info, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "my-solution"})
	require.NoError(t, err)
	assert.Equal(t, LocalOrigin, info.Reference.Origin)
}

// TestLocalCatalog_Resolve_NoVersion_OriginQualifiedFiltersBeforeLatest covers
// the cross-origin "latest" bug: origin A holds only 1.0.0 while origin B holds
// a higher 2.0.0. An origin-qualified no-version request for A must resolve A's
// 1.0.0 rather than computing "latest" across all origins (2.0.0 from B) and
// then failing origin selection with a spurious not-found.
func TestLocalCatalog_Resolve_NoVersion_OriginQualifiedFiltersBeforeLatest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newTestCatalog(t)

	storePulled(t, cat, ctx, "my-solution", "1.0.0", "ghcr.io/source")
	storePulled(t, cat, ctx, "my-solution", "2.0.0", "ghcr.io/other")

	ref := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "ghcr.io/source"}
	info, err := cat.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/source", info.Reference.Origin)
	require.NotNil(t, info.Reference.Version)
	assert.Equal(t, "1.0.0", info.Reference.Version.String())

	// The other origin's higher version still resolves via its own qualifier.
	other := Reference{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "ghcr.io/other"}
	otherInfo, err := cat.Resolve(ctx, other)
	require.NoError(t, err)
	require.NotNil(t, otherInfo.Reference.Version)
	assert.Equal(t, "2.0.0", otherInfo.Reference.Version.String())
}
