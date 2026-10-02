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

// Tests for local identity: a locally built copy and copies pulled from
// different origins are distinct local identities even when they share a
// name and version.

func newIdentityTestCatalog(t *testing.T) *LocalCatalog {
	t.Helper()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	return cat
}

// storeLegacyPulled stores a pulled copy at a bare (unqualified) tag, the shape
// produced by renamed pulls and pulls made before tags were origin-qualified.
// The copy's source canonical is testPushOrigin.
func storeLegacyPulled(t *testing.T, cat *LocalCatalog, ref Reference) string {
	t.Helper()
	info, err := cat.Store(context.Background(), ref, []byte("legacy "+ref.Name), nil,
		map[string]string{AnnotationSourceCanonical: testPushOrigin}, false)
	require.NoError(t, err)
	return info.Digest
}

// stableAliasTag is the stored tag of the "stable" alias for a solution copy.
func stableAliasTag(name, origin string) string {
	return Reference{Kind: ArtifactKindSolution, Name: name, Origin: origin}.LocalTag() + ":stable"
}

func TestLocalCatalog_DeleteResolved_RemovesWholeIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "foo", Version: semver.MustParse("1.0.0")}
	_, err := cat.Store(ctx, ref, []byte("built"), nil, nil, false)
	require.NoError(t, err)
	for _, alias := range []string{"stable", "prod"} {
		_, err = cat.Tag(ctx, ref, alias)
		require.NoError(t, err)
	}
	pulledDigest := storeForPush(t, cat, ArtifactKindSolution, "foo", "1.0.0", testPushOrigin, nil)

	sel, err := ParseExactSelector("foo@1.0.0", "", "built")
	require.NoError(t, err)
	info, err := cat.ResolveExact(ctx, sel)
	require.NoError(t, err)

	removed, err := cat.DeleteResolved(ctx, info)
	require.NoError(t, err)
	assert.Equal(t, []string{"1.0.0", "prod", "stable"}, removed)

	_, err = cat.ResolveExact(ctx, sel)
	assert.True(t, IsNotFound(err), "no alias may keep the deleted version resolvable: %v", err)

	pulled := ref
	pulled.Origin = testPushOrigin
	got, err := cat.Resolve(ctx, pulled)
	require.NoError(t, err, "another origin's copy at the same version must survive")
	assert.Equal(t, pulledDigest, got.Digest)
}

// TestLocalCatalog_SameVersionRebuild covers a --force rebuild of a version
// that has an alias: the alias keeps the old digest (a stale entry at the same
// version), which must neither make the version ambiguous nor be deleted with
// the new copy.
func TestLocalCatalog_SameVersionRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "foo", Version: semver.MustParse("1.0.0")}
	old, err := cat.Store(ctx, ref, []byte("old"), nil, nil, false)
	require.NoError(t, err)
	_, err = cat.Tag(ctx, ref, "stable")
	require.NoError(t, err)
	rebuilt, err := cat.Store(ctx, ref, []byte("new"), nil, nil, true)
	require.NoError(t, err)
	require.NotEqual(t, old.Digest, rebuilt.Digest)

	sel, err := ParseExactSelector("foo@1.0.0", "", "")
	require.NoError(t, err)

	t.Run("exact resolve prefers the version tag", func(t *testing.T) {
		t.Parallel()
		info, err := cat.ResolveExact(ctx, sel)
		require.NoError(t, err)
		assert.Equal(t, rebuilt.Digest, info.Digest)
		assert.Equal(t, "1.0.0", info.Tag)
	})

	t.Run("push resolve prefers the version tag", func(t *testing.T) {
		t.Parallel()
		pushSel, err := ParsePushSelector("foo@1.0.0", "", "")
		require.NoError(t, err)
		info, err := cat.ResolveForPush(ctx, pushSel)
		require.NoError(t, err)
		assert.Equal(t, rebuilt.Digest, info.Digest)
	})

	t.Run("push resolve of latest prefers the version tag", func(t *testing.T) {
		t.Parallel()
		pushSel, err := ParsePushSelector("foo", "", "")
		require.NoError(t, err)
		info, err := cat.ResolveForPush(ctx, pushSel)
		require.NoError(t, err)
		assert.Equal(t, rebuilt.Digest, info.Digest)
	})

	t.Run("the stale alias stays selectable by digest", func(t *testing.T) {
		t.Parallel()
		digestSel, err := ParseExactSelector("foo@"+old.Digest, "", "")
		require.NoError(t, err)
		info, err := cat.ResolveExact(ctx, digestSel)
		require.NoError(t, err)
		assert.Equal(t, "stable", info.Tag)
	})
}

func TestLocalCatalog_SameVersionRebuild_TagAfterRebuild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)

	// build -> tag stable -> rebuild --force -> tag prod (review N1 repro).
	ref := Reference{Kind: ArtifactKindSolution, Name: "foo", Version: semver.MustParse("1.0.0")}
	_, err := cat.Store(ctx, ref, []byte("old"), nil, nil, false)
	require.NoError(t, err)
	_, err = cat.Tag(ctx, ref, "stable")
	require.NoError(t, err)
	rebuilt, err := cat.Store(ctx, ref, []byte("new"), nil, nil, true)
	require.NoError(t, err)

	sel, err := ParseExactSelector("foo@1.0.0", "", "")
	require.NoError(t, err)
	info, err := cat.ResolveExact(ctx, sel)
	require.NoError(t, err, "the stale alias must not make the version ambiguous")
	_, err = cat.TagResolved(ctx, info, "prod")
	require.NoError(t, err)

	prod, err := cat.ResolveExact(ctx, ExactSelector{Kind: ArtifactKindSolution, Name: "foo", Digest: rebuilt.Digest})
	require.NoError(t, err)
	assert.Equal(t, rebuilt.Digest, prod.Digest)
	removed, err := cat.DeleteResolved(ctx, prod)
	require.NoError(t, err)
	assert.Equal(t, []string{"1.0.0", "prod"}, removed, "prod is an alias of the rebuilt copy")
}

func TestLocalCatalog_SameVersionRebuild_DeleteThenStaleAlias(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "foo", Version: semver.MustParse("1.0.0")}
	old, err := cat.Store(ctx, ref, []byte("old"), nil, nil, false)
	require.NoError(t, err)
	_, err = cat.Tag(ctx, ref, "stable")
	require.NoError(t, err)
	_, err = cat.Store(ctx, ref, []byte("new"), nil, nil, true)
	require.NoError(t, err)

	sel, err := ParseExactSelector("foo@1.0.0", "", "")
	require.NoError(t, err)
	info, err := cat.ResolveExact(ctx, sel)
	require.NoError(t, err)
	removed, err := cat.DeleteResolved(ctx, info)
	require.NoError(t, err)
	assert.Equal(t, []string{"1.0.0"}, removed, "the alias holds a different digest and is not part of this identity")

	// Only the stale alias is left at 1.0.0; it is now what the version resolves to.
	left, err := cat.ResolveExact(ctx, sel)
	require.NoError(t, err)
	assert.Equal(t, old.Digest, left.Digest)
	removed, err = cat.DeleteResolved(ctx, left)
	require.NoError(t, err)
	assert.Equal(t, []string{"stable"}, removed)
}

func TestLocalCatalog_DeleteResolved_NotFound(t *testing.T) {
	t.Parallel()
	cat := newIdentityTestCatalog(t)
	storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", "", nil)

	_, err := cat.DeleteResolved(context.Background(), ArtifactInfo{
		Reference: Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")},
		Digest:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	})
	require.Error(t, err)
	assert.True(t, IsNotFound(err))
}

func TestLocalCatalog_Delete_AnnotationFallbackRespectsOrigin(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)
	storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)

	other := Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0"), Origin: testPushOtherOrigin}
	err := cat.Delete(ctx, other)
	require.Error(t, err)
	assert.True(t, IsNotFound(err), "a copy from another origin must never be deleted")

	pulled := other
	pulled.Origin = testPushOrigin
	exists, err := cat.Exists(ctx, pulled)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestLocalCatalog_Delete_BareRefIsBuiltOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)
	storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)
	legacy := Reference{Kind: ArtifactKindSolution, Name: "legacy", Version: semver.MustParse("1.0.0")}
	storeLegacyPulled(t, cat, legacy)

	built := Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}
	for _, ref := range []Reference{built, legacy} {
		err := cat.Delete(ctx, ref)
		assert.True(t, IsNotFound(err), "a bare ref must never delete a pulled copy of %s: %v", ref.Name, err)
	}

	_, err := cat.Store(ctx, built, []byte("built"), nil, nil, false)
	require.NoError(t, err)
	require.NoError(t, cat.Delete(ctx, built))

	pulled := built
	pulled.Origin = testPushOrigin
	exists, err := cat.Exists(ctx, pulled)
	require.NoError(t, err)
	assert.True(t, exists, "deleting the built copy must leave the pulled copy")
}

func TestLocalCatalog_Store_MigratesLegacyPull(t *testing.T) {
	t.Parallel()
	for name, force := range map[string]bool{"without force": false, "with force": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			cat := newIdentityTestCatalog(t)
			bare := Reference{Kind: ArtifactKindSolution, Name: "legacy", Version: semver.MustParse("1.0.0")}
			pulledDigest := storeLegacyPulled(t, cat, bare)

			built, err := cat.Store(ctx, bare, []byte("built"), nil, nil, force)
			require.NoError(t, err, "a legacy pull at the bare tag must not block the built copy")

			qualified := bare
			qualified.Origin = testPushOrigin
			got, err := cat.Resolve(ctx, qualified)
			require.NoError(t, err, "the pulled copy must survive under its origin-qualified tag")
			assert.Equal(t, pulledDigest, got.Digest)

			sel, err := ParseExactSelector("legacy@1.0.0", "", "built")
			require.NoError(t, err)
			info, err := cat.ResolveExact(ctx, sel)
			require.NoError(t, err)
			assert.Equal(t, built.Digest, info.Digest)
		})
	}
}

func TestLocalCatalog_StoreMultiPlatform_MigratesLegacyPull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)
	bare := Reference{Kind: ArtifactKindProvider, Name: "legacy", Version: semver.MustParse("1.0.0")}
	pulledDigest := storeLegacyPulled(t, cat, bare)

	binaries := []PlatformBinary{{Platform: "linux/amd64", Data: []byte("bin")}}
	_, err := cat.StoreMultiPlatform(ctx, bare, binaries, nil, true)
	require.NoError(t, err)

	qualified := bare
	qualified.Origin = testPushOrigin
	got, err := cat.Resolve(ctx, qualified)
	require.NoError(t, err)
	assert.Equal(t, pulledDigest, got.Digest)
}

func TestLocalCatalog_LegacyBareTaggedPull(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)

	bare := Reference{Kind: ArtifactKindProvider, Name: "legacy", Version: semver.MustParse("1.0.0")}
	digest := storeLegacyPulled(t, cat, bare)
	qualified := bare
	qualified.Origin = testPushOrigin

	t.Run("bare resolve reports the source origin", func(t *testing.T) {
		t.Parallel()
		info, err := cat.Resolve(ctx, bare)
		require.NoError(t, err)
		assert.Equal(t, digest, info.Digest)
		assert.Equal(t, testPushOrigin, info.Reference.Origin)
		assert.Equal(t, testPushOrigin, info.Canonical)
	})

	t.Run("platform listing finds the bare-tagged copy", func(t *testing.T) {
		t.Parallel()
		platforms, err := cat.ListPlatforms(ctx, qualified)
		require.NoError(t, err)
		assert.Nil(t, platforms)
	})

	t.Run("platform fetch finds the bare-tagged copy", func(t *testing.T) {
		t.Parallel()
		content, _, err := cat.FetchByPlatform(ctx, qualified, "linux/amd64")
		require.NoError(t, err)
		assert.Equal(t, []byte("legacy legacy"), content)
	})
}

func TestLocalCatalog_Store_ExistenceIsPerIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)
	storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)

	bare := Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}

	exists, err := cat.Exists(ctx, bare)
	require.NoError(t, err)
	assert.True(t, exists, "a bare Exists sees a copy from any origin")

	_, err = cat.Store(ctx, bare, []byte("built"), nil, nil, false)
	require.NoError(t, err, "a pulled copy must not block storing the built copy")

	qualified := bare
	qualified.Origin = testPushOrigin
	_, err = cat.Store(ctx, qualified, []byte("again"), nil, map[string]string{AnnotationSourceCanonical: testPushOrigin}, false)
	var existsErr *ArtifactExistsError
	require.ErrorAs(t, err, &existsErr, "the same identity must still be protected")
}

func TestLocalCatalog_StoreMultiPlatform_ExistenceIsPerIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)
	storeForPush(t, cat, ArtifactKindProvider, "plug", "1.0.0", testPushOrigin, nil)

	bare := Reference{Kind: ArtifactKindProvider, Name: "plug", Version: semver.MustParse("1.0.0")}
	binaries := []PlatformBinary{{Platform: "linux/amd64", Data: []byte("bin")}}
	_, err := cat.StoreMultiPlatform(ctx, bare, binaries, nil, false)
	require.NoError(t, err, "a pulled copy must not block storing the built copy")

	_, err = cat.StoreMultiPlatform(ctx, bare, binaries, nil, false)
	var existsErr *ArtifactExistsError
	require.ErrorAs(t, err, &existsErr)
}

func TestLocalCatalog_TagResolved_IsOriginScoped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)

	digests := map[string]string{
		testPushOrigin:      storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil),
		testPushOtherOrigin: storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOtherOrigin, nil),
	}

	for origin := range digests {
		sel, err := ParseExactSelector("app@1.0.0", "", origin)
		require.NoError(t, err)
		info, err := cat.ResolveExact(ctx, sel)
		require.NoError(t, err)
		old, err := cat.TagResolved(ctx, info, "stable")
		require.NoError(t, err)
		assert.Empty(t, old)
	}

	for origin, digest := range digests {
		desc, err := cat.store.Resolve(ctx, stableAliasTag("app", origin))
		require.NoError(t, err, "alias for %s must live in its origin namespace", origin)
		assert.Equal(t, digest, desc.Digest.String())
		assert.Equal(t, origin, desc.Annotations[AnnotationSourceCanonical], "alias must keep the copy's provenance")
	}

	_, err := cat.store.Resolve(ctx, stableAliasTag("app", ""))
	require.Error(t, err, "the built namespace must be untouched")

	// Moving the alias within one origin reports the previous version.
	storeForPush(t, cat, ArtifactKindSolution, "app", "1.1.0", testPushOrigin, nil)
	sel, err := ParseExactSelector("app@1.1.0", "", testPushOrigin)
	require.NoError(t, err)
	info, err := cat.ResolveExact(ctx, sel)
	require.NoError(t, err)
	old, err := cat.TagResolved(ctx, info, "stable")
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", old)
}

func TestLocalCatalog_Tag_PulledOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cat := newIdentityTestCatalog(t)
	digest := storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)

	old, err := cat.Tag(ctx, Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}, "stable")
	require.NoError(t, err)
	assert.Empty(t, old)

	desc, err := cat.store.Resolve(ctx, stableAliasTag("app", testPushOrigin))
	require.NoError(t, err)
	assert.Equal(t, digest, desc.Digest.String())
}

func TestLocalCatalog_ResolveExact_SameDigestDifferentOrigins(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newPushFixture(t)

	sel, err := ParseExactSelector("mirror@1.0.0", "", "")
	require.NoError(t, err)
	_, err = f.cat.ResolveExact(ctx, sel)
	var amb *AmbiguousArtifactError
	require.ErrorAs(t, err, &amb, "identical content from two origins is two local identities")
	require.Len(t, amb.Candidates, 2)

	assert.ElementsMatch(t, []string{
		"mycli catalog delete mirror@1.0.0 --kind solution --origin " + OriginBuilt,
		"mycli catalog delete mirror@1.0.0 --kind solution --origin " + testPushOrigin,
	}, amb.DeleteHints("mycli"))
	assert.ElementsMatch(t, []string{
		"mycli catalog tag mirror@1.0.0 stable --kind solution --origin " + OriginBuilt,
		"mycli catalog tag mirror@1.0.0 stable --kind solution --origin " + testPushOrigin,
	}, amb.TagHints("mycli", "stable"))

	sel, err = ParseExactSelector("mirror@1.0.0", "", OriginBuilt)
	require.NoError(t, err)
	info, err := f.cat.ResolveExact(ctx, sel)
	require.NoError(t, err)
	assert.Equal(t, f.mirrorSharedDgst, info.Digest)
	assert.Empty(t, info.Canonical)
}

func TestAmbiguousKindError_TagHints(t *testing.T) {
	t.Parallel()
	e := &AmbiguousKindError{Name: "exec", Kinds: []ArtifactKind{ArtifactKindSolution, ArtifactKindProvider}, Selector: "@1.0.0"}
	assert.Equal(t, []string{
		"mycli catalog tag exec@1.0.0 stable --kind solution",
		"mycli catalog tag exec@1.0.0 stable --kind provider",
	}, e.TagHints("mycli", "stable"))
}
