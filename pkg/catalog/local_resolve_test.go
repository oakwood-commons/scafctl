// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tagAsPulled re-tags an existing built artifact as a pulled copy from
// canonical, sharing the same content digest (a mirror). It mirrors the
// technique used by newPushFixture and lets tests exercise identical content
// coexisting under multiple origins.
func tagAsPulled(t testing.TB, cat *LocalCatalog, builtRef Reference, canonical string) {
	t.Helper()
	ctx := context.Background()
	desc, err := cat.store.Resolve(ctx, builtRef.LocalTag())
	require.NoError(t, err)

	anns := make(map[string]string, len(desc.Annotations)+2)
	for k, v := range desc.Annotations {
		anns[k] = v
	}
	anns[AnnotationSourceCanonical] = canonical
	anns[AnnotationOrigin] = "pulled from " + canonical
	desc.Annotations = anns

	pulledRef := builtRef
	pulledRef.Origin = canonical
	require.NoError(t, cat.store.Tag(ctx, desc, pulledRef.LocalTag()))
}

// TestResolve_PrefersBuiltOverPulled exercises selectResolved's prefer-built
// branch via the no-version "latest" path (where the tag fast path does not
// short-circuit): a built copy wins over a same-version pulled copy with
// different content.
func TestResolve_PrefersBuiltOverPulled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	builtDigest := storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", "", nil)
	pulledDigest := storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)
	require.NotEqual(t, builtDigest, pulledDigest, "built and pulled must differ to prove preference")

	info, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "app"})
	require.NoError(t, err)
	assert.Equal(t, builtDigest, info.Digest)
	assert.Empty(t, info.Canonical)
	assert.Empty(t, info.Reference.Origin, "built copy resolves to an unqualified reference")
}

// TestResolve_AmbiguousAcrossRemotes: two pulled copies from different origins
// with different content and no built copy is ambiguous.
func TestResolve_AmbiguousAcrossRemotes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	a := storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)
	b := storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOtherOrigin, nil)
	require.NotEqual(t, a, b)

	_, err = cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")})
	require.Error(t, err)
	assert.True(t, IsAmbiguous(err), "want ErrAmbiguousArtifact, got %v", err)

	var ambErr *AmbiguousLocalArtifactError
	require.True(t, errors.As(err, &ambErr))
	assert.ElementsMatch(t, []string{testPushOrigin, testPushOtherOrigin}, ambErr.Origins)
}

// TestResolve_CollapsesIdenticalDigests: identical content mirrored across two
// origins resolves to that single copy (no ambiguity).
func TestResolve_CollapsesIdenticalDigests(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	// Store one copy tagged as a pull from testPushOrigin, then mirror the same
	// descriptor under testPushOtherOrigin. Remove the accidental built tag so
	// only the two pulled copies exist.
	shared := storeForPush(t, cat, ArtifactKindSolution, "mirror", "1.0.0", testPushOrigin, nil)
	pulledRef := Reference{Kind: ArtifactKindSolution, Name: "mirror", Version: semver.MustParse("1.0.0"), Origin: testPushOrigin}
	tagAsPulled(t, cat, pulledRef, testPushOtherOrigin)

	info, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "mirror", Version: semver.MustParse("1.0.0")})
	require.NoError(t, err)
	assert.Equal(t, shared, info.Digest)
}

// TestResolve_DigestPinNeverAmbiguous: pinning by digest resolves to exactly
// that content even when other origins have different content at the same
// version.
func TestResolve_DigestPinNeverAmbiguous(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	a := storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)
	b := storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOtherOrigin, nil)
	require.NotEqual(t, a, b)

	info, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "app", Digest: b})
	require.NoError(t, err)
	assert.Equal(t, b, info.Digest)
	assert.Equal(t, testPushOtherOrigin, info.Canonical)
}

// TestResolve_OriginQualifiedFastPath: an origin-qualified reference resolves
// directly to that origin's copy.
func TestResolve_OriginQualifiedFastPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)
	storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOtherOrigin, nil)

	info, err := cat.Resolve(ctx, Reference{
		Kind:    ArtifactKindSolution,
		Name:    "app",
		Version: semver.MustParse("1.0.0"),
		Origin:  testPushOtherOrigin,
	})
	require.NoError(t, err)
	assert.Equal(t, testPushOtherOrigin, info.Canonical)
	assert.Equal(t, testPushOtherOrigin, info.Reference.Origin)
}

// TestResolve_OriginQualifiedNotFound: asking for an origin that lacks the
// artifact never returns another origin's copy.
func TestResolve_OriginQualifiedNotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)

	_, err = cat.Resolve(ctx, Reference{
		Kind:    ArtifactKindSolution,
		Name:    "app",
		Version: semver.MustParse("1.0.0"),
		Origin:  "ghcr.io/absent",
	})
	require.Error(t, err)
	var nf *ArtifactNotFoundError
	assert.True(t, errors.As(err, &nf), "want ArtifactNotFoundError, got %v", err)
}

// TestResolve_LatestPrefersHigherRemoteVersion: version dominates over
// built-preference; a higher pulled version is not shadowed by a lower build.
func TestResolve_LatestPrefersHigherRemoteVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	storeForPush(t, cat, ArtifactKindSolution, "app", "1.1.0", "", nil)
	higher := storeForPush(t, cat, ArtifactKindSolution, "app", "1.2.0", testPushOrigin, nil)

	info, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "app"})
	require.NoError(t, err)
	assert.Equal(t, "1.2.0", info.Reference.Version.String())
	assert.Equal(t, higher, info.Digest)
	assert.Equal(t, testPushOrigin, info.Reference.Origin)
}

// TestResolve_StampsResolvedOrigin: a single pulled copy resolves to a
// fully-qualified reference carrying its origin.
func TestResolve_StampsResolvedOrigin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	storeForPush(t, cat, ArtifactKindSolution, "solo", "1.0.0", testPushOrigin, nil)

	info, err := cat.Resolve(ctx, Reference{Kind: ArtifactKindSolution, Name: "solo", Version: semver.MustParse("1.0.0")})
	require.NoError(t, err)
	assert.Equal(t, testPushOrigin, info.Reference.Origin)
}

// TestSave_PulledOnlyArtifact regresses the bug where Save reconstructed a bare
// tag and could not find a pulled-only artifact.
func TestSave_PulledOnlyArtifact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	digest := storeForPush(t, cat, ArtifactKindSolution, "ponly", "1.0.0", testPushOrigin, nil)

	out := filepath.Join(t.TempDir(), "ponly.tar")
	res, err := cat.Save(ctx, "ponly", "1.0.0", out)
	require.NoError(t, err)
	assert.Equal(t, digest, res.Digest)
}

// TestResolveContentDigest_PulledOnlyArtifact regresses the same bug for
// ResolveContentDigest.
func TestResolveContentDigest_PulledOnlyArtifact(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	storeForPush(t, cat, ArtifactKindSolution, "ponly", "1.0.0", testPushOrigin, nil)

	got, err := cat.ResolveContentDigest(ctx,
		Reference{Kind: ArtifactKindSolution, Name: "ponly", Version: semver.MustParse("1.0.0")},
		"", MediaTypeForKind(ArtifactKindSolution))
	require.NoError(t, err)
	assert.NotEmpty(t, got.ContentDigest)
	assert.Equal(t, testPushOrigin, got.Reference.Origin)
}

// TestExists_OriginAware: an origin-qualified existence check is precise -- it
// never reports a copy from a different origin as existing. A bare (unqualified)
// reference still matches any origin.
func TestExists_OriginAware(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)

	storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)

	bareExists, err := cat.Exists(ctx, Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")})
	require.NoError(t, err)
	assert.True(t, bareExists, "an unqualified reference matches any origin")

	wrongOrigin, err := cat.Exists(ctx, Reference{
		Kind:    ArtifactKindSolution,
		Name:    "app",
		Version: semver.MustParse("1.0.0"),
		Origin:  "ghcr.io/absent",
	})
	require.NoError(t, err)
	assert.False(t, wrongOrigin, "an origin-qualified reference must not match another origin")

	pulledExists, err := cat.Exists(ctx, Reference{
		Kind:    ArtifactKindSolution,
		Name:    "app",
		Version: semver.MustParse("1.0.0"),
		Origin:  testPushOrigin,
	})
	require.NoError(t, err)
	assert.True(t, pulledExists)
}
