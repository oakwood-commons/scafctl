// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
)

// memSource seeds an in-memory target with builder under tag and returns it as
// the fake "remote" the copy helpers read from. No network is involved.
func memSource(t *testing.T, builder ArtifactBuilder, tag string) oras.ReadOnlyTarget {
	t.Helper()
	src := memory.New()
	desc, err := builder.Push(t.Context(), src)
	require.NoError(t, err)
	require.NoError(t, src.Tag(t.Context(), desc, tag))
	return src
}

func TestCopyOptions(t *testing.T) {
	t.Parallel()

	t.Run("no callback leaves oras defaults", func(t *testing.T) {
		t.Parallel()
		opts := copyOptions(CopyOptions{})
		assert.Nil(t, opts.OnCopySkipped)
		assert.Nil(t, opts.PostCopy)
	})

	t.Run("callback is wired to both hooks and invoked", func(t *testing.T) {
		t.Parallel()
		var calls int
		cb := func(ocispec.Descriptor) { calls++ }
		opts := copyOptions(CopyOptions{OnProgress: cb})
		require.NotNil(t, opts.OnCopySkipped)
		require.NotNil(t, opts.PostCopy)
		require.NoError(t, opts.OnCopySkipped(context.Background(), ocispec.Descriptor{}))
		require.NoError(t, opts.PostCopy(context.Background(), ocispec.Descriptor{}))
		assert.Equal(t, 2, calls)
	})
}

func TestLocalCatalog_storeV2(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
	builder := NewManifestBuilder(ref).
		Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte("name: demo")})

	info, err := cat.storeArtifact(ctx, builder, false)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", info.Reference.Version.String())
	assert.Equal(t, LocalCatalogName, info.Catalog)
	assert.NotEmpty(t, info.Digest)
	assert.True(t, info.Reference.Built)
	assert.Equal(t, "demo", info.Annotations[AnnotationArtifactName],
		"the returned info carries the tagged descriptor's annotations")
	assert.NotContains(t, info.Annotations, AnnotationSourceCanonical)

	resolved, err := cat.store.Resolve(ctx, "solution/demo:1.0.0")
	require.NoError(t, err)
	assert.Equal(t, info.Digest, resolved.Digest.String())

	// Exists without force is rejected.
	_, err = cat.storeArtifact(ctx, builder, false)
	var existsErr *ArtifactExistsError
	require.ErrorAs(t, err, &existsErr)

	// Force overwrites.
	info2, err := cat.storeArtifact(ctx, NewManifestBuilder(ref).
		Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte("name: demo v2")}), true)
	require.NoError(t, err)
	assert.NotEqual(t, info.Digest, info2.Digest)
}

func TestLocalCatalog_storeArtifact_InvalidRef(t *testing.T) {
	cat := newTestCatalog(t)

	_, err := cat.storeArtifact(t.Context(), remoteSolutionBuilder(Reference{Kind: ArtifactKindSolution}), false)
	require.Error(t, err)
}

func TestLocalCatalog_IngestVerbatim(t *testing.T) {
	ctx := context.Background()
	cat := newTestCatalog(t)

	localRef := localRefOf("ghcr.io/myorg/scafctl")
	source := memSource(t, remoteSolutionBuilder(solutionRef("demo", "1.0.0")), "1.0.0")

	info, err := cat.IngestVerbatim(ctx, source, "1.0.0", localRef, oras.DefaultCopyOptions)
	require.NoError(t, err)
	assert.Equal(t, LocalCatalogName, info.Catalog)
	assert.Equal(t, "ghcr.io/myorg/scafctl", info.Annotations[AnnotationSourceCanonical])
	assert.Equal(t, "demo", info.Annotations[AnnotationArtifactName])

	// The local tag is derived from the reference.
	wantTag, err := tagForLocalRef(localRef)
	require.NoError(t, err)
	resolved, err := cat.store.Resolve(ctx, wantTag)
	require.NoError(t, err)
	assert.Equal(t, info.Digest, resolved.Digest.String())

	// Verbatim: digest and content are preserved unchanged (no rebuild).
	content, _, err := cat.Fetch(ctx, localRef)
	require.NoError(t, err)
	assert.Equal(t, "name: demo", string(content))
}

func TestLocalCatalog_IngestVerbatim_InvalidRef(t *testing.T) {
	cat := newTestCatalog(t)
	source := memSource(t, remoteSolutionBuilder(solutionRef("demo", "1.0.0")), "1.0.0")

	_, err := cat.IngestVerbatim(t.Context(), source, "1.0.0", Reference{Kind: ArtifactKindSolution}, oras.DefaultCopyOptions)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tagging local reference")
}

func TestRemoteCatalog_copyVerbatim(t *testing.T) {
	ctx := t.Context()
	cat := newRemoteTestCatalog(t)

	local := newTestCatalog(t)
	ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
	srcTag := cat.tagForRef(ref) // "1.0.0"
	source := memSource(t, remoteSolutionBuilder(ref), srcTag)

	info, err := cat.copyVerbatim(ctx, source, srcTag, ref, local, CopyOptions{})
	require.NoError(t, err)
	assert.Equal(t, "demo", info.Reference.Name)
	assert.False(t, info.Reference.Built)
	assert.Equal(t, "ghcr.io/myorg/scafctl", info.Reference.Remote)
	assert.Equal(t, LocalCatalogName, info.Catalog)

	// Stored under the remote-namespaced local tag, resolvable from the raw store.
	localRef := cat.remoteRefToLocalRef(ref)
	tagged, err := tagForLocalRef(localRef)
	require.NoError(t, err)
	_, err = local.store.Resolve(ctx, tagged)
	require.NoError(t, err)
}

func TestRemoteCatalog_copyRenamed(t *testing.T) {
	ctx := t.Context()
	cat := newRemoteTestCatalog(t)

	local := newTestCatalog(t)
	ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
	srcTag := cat.tagForRef(ref)
	source := memSource(t, remoteSolutionBuilder(ref), srcTag)

	info, err := cat.copyRenamed(ctx, source, srcTag, ref, local, CopyOptions{TargetName: "renamed@2.0.0"})
	require.NoError(t, err)
	assert.Equal(t, "renamed", info.Reference.Name)
	assert.Equal(t, "2.0.0", info.Reference.Version.String())
	assert.True(t, info.Reference.Built, "a renamed artifact is rebuilt, so it is locally built")
	assert.Equal(t, "renamed", info.Annotations[AnnotationArtifactName])
	assert.NotContains(t, info.Annotations, AnnotationSourceCanonical)

	// Stored under the plain local tag for the renamed identity.
	resolved, err := local.store.Resolve(ctx, "solution/renamed:2.0.0")
	require.NoError(t, err)
	assert.Equal(t, info.Digest, resolved.Digest.String())
}

func TestRemoteCatalog_copyRenamed_InvalidTarget(t *testing.T) {
	t.Parallel()

	cat := newRemoteTestCatalog(t)
	ctx := t.Context()
	local := newTestCatalog(t)
	ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
	srcTag := cat.tagForRef(ref)
	source := memSource(t, remoteSolutionBuilder(ref), srcTag)

	tests := []struct {
		name   string
		target string
	}{
		{name: "bare name", target: "just-name"},
		{name: "digest pin", target: "demo@sha256:abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := cat.copyRenamed(ctx, source, srcTag, ref, local, CopyOptions{TargetName: tt.target})
			require.Error(t, err)
			var ire *InvalidReferenceError
			require.ErrorAs(t, err, &ire)
		})
	}
}

func TestRemoteCatalog_CopyToLocal_Guard(t *testing.T) {
	t.Parallel()

	cat := newRemoteTestCatalog(t)
	ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
	_, err := cat.CopyToLocal(context.Background(), ref, nil, CopyOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil local catalog")
}

func TestLocalCatalog_ExportVerbatim(t *testing.T) {
	ctx := t.Context()
	cat := newTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
	stored, err := cat.storeArtifact(ctx, remoteSolutionBuilder(ref), false)
	require.NoError(t, err)

	srcTag, err := tagForLocalRef(ref)
	require.NoError(t, err)

	dst := memory.New()
	desc, err := cat.ExportVerbatim(ctx, srcTag, dst, "9.9.9", oras.DefaultCopyOptions)
	require.NoError(t, err)

	// Verbatim export preserves the digest.
	assert.Equal(t, stored.Digest, desc.Digest.String())
	resolved, err := dst.Resolve(ctx, "9.9.9")
	require.NoError(t, err)
	assert.Equal(t, desc.Digest, resolved.Digest)
}

func TestLocalCatalog_ExportRebuilt(t *testing.T) {
	ctx := t.Context()
	cat := newTestCatalog(t)

	ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
	stored, err := cat.storeArtifact(ctx, remoteSolutionBuilder(ref), false)
	require.NoError(t, err)

	srcTag, err := tagForLocalRef(ref)
	require.NoError(t, err)

	target := Reference{Kind: ArtifactKindSolution, Name: "renamed", Version: semver.MustParse("2.0.0")}
	dst := memory.New()
	desc, err := cat.ExportRebuilt(ctx, srcTag, dst, "2.0.0", target, nil)
	require.NoError(t, err)

	// A rebuild mints a new digest, and the destination holds it under the tag.
	assert.NotEqual(t, stored.Digest, desc.Digest.String())
	resolved, err := dst.Resolve(ctx, "2.0.0")
	require.NoError(t, err)
	assert.Equal(t, desc.Digest, resolved.Digest)
}

func TestRemoteCatalog_publishedInfo(t *testing.T) {
	t.Parallel()

	cat := newRemoteTestCatalog(t)
	dig := digest.FromString("x")
	desc := ocispec.Descriptor{Digest: dig, Size: 3}

	info := cat.publishedInfo(
		Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")},
		"1.0.0",
		desc,
	)

	assert.Equal(t, "ghcr.io/myorg/scafctl", info.Canonical)
	assert.Equal(t, "1.0.0", info.Tag)
	assert.Equal(t, "ghcr.io/myorg/scafctl", info.Reference.Remote)
	assert.False(t, info.Reference.Built)
	assert.Equal(t, "ghcr.io/myorg/scafctl/solutions/demo@"+dig.String(), info.ImageRef)
	assert.Equal(t, "ghcr.io", info.Catalog)
}

func TestRemoteCatalog_CopyFromLocal_Guard(t *testing.T) {
	t.Parallel()

	cat := newRemoteTestCatalog(t)
	ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0")}
	_, err := cat.CopyFromLocal(context.Background(), ref, nil, CopyOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil local catalog")
}

func newRemoteTestCatalog(t *testing.T) *RemoteCatalog {
	t.Helper()
	cat, err := NewRemoteCatalog(RemoteCatalogConfig{
		Registry:   "ghcr.io",
		Repository: "myorg/scafctl",
		Logger:     logr.Discard(),
	})
	require.NoError(t, err)
	return cat
}

func localRefOf(remote string) Reference {
	return Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0"), Remote: remote}
}

// tagForLocalRefWithCanonical mirrors what copyVerbatim stores under:
// tagForLocalRef over a ref whose Remote is the base canonical identity.
