// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
)

func mustVersion(s string) *semver.Version {
	v, err := semver.NewVersion(s)
	if err != nil {
		panic("bad test version: " + err.Error())
	}
	return v
}

func newTestLocalCatalog(t *testing.T) *LocalCatalog {
	t.Helper()
	dir := t.TempDir()
	cat, err := NewLocalCatalogAt(dir, logr.Discard())
	require.NoError(t, err)
	return cat
}

func testPluginRef(name, version string) Reference {
	v, err := semver.NewVersion(version)
	if err != nil {
		panic("bad test version: " + err.Error())
	}
	return Reference{
		Kind:    ArtifactKindProvider,
		Name:    name,
		Version: v,
	}
}

func TestLocalCatalog_StoreMultiPlatform_RoundTrip(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("multi-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-amd64-binary")},
		{Platform: "darwin/arm64", Data: []byte("darwin-arm64-binary")},
	}

	info, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)
	assert.Equal(t, "multi-provider", info.Reference.Name)
	assert.Equal(t, "1.0.0", info.Reference.Version.String())
	assert.NotEmpty(t, info.Digest)

	// Fetch linux/amd64
	data, fetchInfo, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("linux-amd64-binary"), data)
	assert.Equal(t, "linux/amd64", fetchInfo.Annotations[AnnotationPlatform])

	// Fetch darwin/arm64
	data, fetchInfo, err = cat.FetchByPlatform(ctx, ref, "darwin/arm64")
	require.NoError(t, err)
	assert.Equal(t, []byte("darwin-arm64-binary"), data)
	assert.Equal(t, "darwin/arm64", fetchInfo.Annotations[AnnotationPlatform])
}

func TestLocalCatalog_StoreMultiPlatform_PlatformNotFound(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("single-plat", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-binary")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	// Try to fetch a platform that doesn't exist
	_, _, err = cat.FetchByPlatform(ctx, ref, "windows/amd64")
	require.Error(t, err)
	assert.True(t, IsPlatformNotFound(err))
}

func TestLocalCatalog_StoreMultiPlatform_AlreadyExists(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("dup-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("binary")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	// Second store should fail (no force)
	_, err = cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.Error(t, err)
	assert.True(t, IsExists(err))
}

func TestLocalCatalog_StoreMultiPlatform_Force(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("force-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("binary-v1")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	// Force overwrite
	binaries[0].Data = []byte("binary-v2")
	_, err = cat.StoreMultiPlatform(ctx, ref, binaries, nil, true)
	require.NoError(t, err)

	// Verify v2 data
	data, _, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("binary-v2"), data)
}

func TestLocalCatalog_StoreMultiPlatform_EmptyBinaries(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("empty-provider", "1.0.0")

	_, err := cat.StoreMultiPlatform(ctx, ref, nil, nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one platform binary")
}

func TestLocalCatalog_StoreMultiPlatform_InvalidPlatform(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("bad-plat-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "invalid", Data: []byte("binary")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid platform")
}

func TestLocalCatalog_StoreMultiPlatform_FiveplatformRoundTrip(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("all-plats-provider", "2.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-amd64")},
		{Platform: "linux/arm64", Data: []byte("linux-arm64")},
		{Platform: "darwin/amd64", Data: []byte("darwin-amd64")},
		{Platform: "darwin/arm64", Data: []byte("darwin-arm64")},
		{Platform: "windows/amd64", Data: []byte("windows-amd64")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	// Verify each platform
	for _, pb := range binaries {
		data, info, err := cat.FetchByPlatform(ctx, ref, pb.Platform)
		require.NoError(t, err, "platform %s", pb.Platform)
		assert.Equal(t, pb.Data, data, "platform %s", pb.Platform)
		assert.Equal(t, pb.Platform, info.Annotations[AnnotationPlatform])
	}
}

func TestLocalCatalog_ListPlatforms(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("list-plats", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux")},
		{Platform: "darwin/arm64", Data: []byte("darwin")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	platforms, err := cat.ListPlatforms(ctx, ref)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"linux/amd64", "darwin/arm64"}, platforms)
}

func TestLocalCatalog_ListPlatforms_SinglePlatform(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "single-provider",
		Version: mustVersion("1.0.0"),
	}

	// Store a normal single-platform artifact
	_, err := cat.Store(ctx, ref, []byte("binary"), nil, nil, false)
	require.NoError(t, err)

	// ListPlatforms should return nil for single-platform
	platforms, err := cat.ListPlatforms(ctx, ref)
	require.NoError(t, err)
	assert.Nil(t, platforms)
}

func TestLocalCatalog_FetchByPlatform_SinglePlatformFallback(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "single-provider",
		Version: mustVersion("1.0.0"),
	}

	// Store a normal single-platform artifact
	_, err := cat.Store(ctx, ref, []byte("single-binary"), nil, nil, false)
	require.NoError(t, err)

	// FetchByPlatform should return the content for any platform since
	// it's a single-platform manifest (no image index)
	data, _, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("single-binary"), data)
}

func TestLocalCatalog_StoreMultiPlatform_WithAnnotations(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := testPluginRef("annotated-provider", "1.0.0")

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("binary")},
	}

	annotations := map[string]string{
		AnnotationDescription: "A test provider",
		AnnotationAuthors:     "test-author",
	}

	info, err := cat.StoreMultiPlatform(ctx, ref, binaries, annotations, false)
	require.NoError(t, err)
	assert.Equal(t, "A test provider", info.Annotations[AnnotationDescription])
	assert.Equal(t, "test-author", info.Annotations[AnnotationAuthors])
}

func TestLocalCatalog_StoreMultiPlatform_AuthHandler(t *testing.T) {
	cat := newTestLocalCatalog(t)
	ctx := context.Background()
	ref := Reference{
		Kind:    ArtifactKindAuthHandler,
		Name:    "multi-auth",
		Version: mustVersion("1.0.0"),
	}

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("auth-linux")},
		{Platform: "darwin/arm64", Data: []byte("auth-darwin")},
	}

	_, err := cat.StoreMultiPlatform(ctx, ref, binaries, nil, false)
	require.NoError(t, err)

	data, _, err := cat.FetchByPlatform(ctx, ref, "darwin/arm64")
	require.NoError(t, err)
	assert.Equal(t, []byte("auth-darwin"), data)
}

func TestVerifyBlob(t *testing.T) {
	t.Parallel()
	data := []byte("blob-content")
	good := ocispec.Descriptor{Digest: digest.FromBytes(data)}

	tests := []struct {
		name    string
		desc    ocispec.Descriptor
		wantErr error
	}{
		{name: "matching digest", desc: good},
		{name: "digest mismatch", desc: ocispec.Descriptor{Digest: digest.FromString("other")}, wantErr: content.ErrMismatchedDigest},
		{name: "invalid digest", desc: ocispec.Descriptor{Digest: "sha256:nothex"}, wantErr: digest.ErrDigestInvalidLength},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := verifyBlob(tt.desc, data)
			if tt.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestLocalCatalog_FetchByPlatform_ManifestDigest(t *testing.T) {
	t.Parallel()
	cat := newTestLocalCatalog(t)
	ctx := t.Context()

	multi := testPluginRef("multi", "1.0.0")
	index, err := cat.StoreMultiPlatform(ctx, multi, []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux")},
		{Platform: "darwin/arm64", Data: []byte("darwin")},
	}, nil, false)
	require.NoError(t, err)

	_, linux, err := cat.FetchByPlatform(ctx, multi, "linux/amd64")
	require.NoError(t, err)
	_, darwin, err := cat.FetchByPlatform(ctx, multi, "darwin/arm64")
	require.NoError(t, err)

	// An index records the platform manifest, not the index itself.
	assert.NotEmpty(t, linux.ManifestDigest)
	assert.NotEqual(t, index.Digest, linux.ManifestDigest)
	assert.NotEqual(t, linux.ManifestDigest, darwin.ManifestDigest)
	assert.Empty(t, linux.ImageRef, "a local build has no registry image")

	single := testPluginRef("single", "1.0.0")
	stored, err := cat.Store(ctx, single, []byte("single"), nil, nil, false)
	require.NoError(t, err)
	_, info, err := cat.FetchByPlatform(ctx, single, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, stored.Digest, info.ManifestDigest)
}

func TestLocalCatalog_FetchByPlatform_RejectsTamperedBlob(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		tampered []byte
		wantErr  error
	}{
		{name: "same length", tampered: []byte("tampered"), wantErr: content.ErrMismatchedDigest},
		{name: "longer", tampered: []byte("original-plus-more"), wantErr: content.ErrTrailingData},
		{name: "shorter", tampered: []byte("orig"), wantErr: io.ErrUnexpectedEOF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cat := newTestLocalCatalog(t)
			ctx := t.Context()
			ref := testPluginRef("tampered", "1.0.0")
			binary := []byte("original")
			_, err := cat.StoreMultiPlatform(ctx, ref, []PlatformBinary{{Platform: "linux/amd64", Data: binary}}, nil, false)
			require.NoError(t, err)

			// Replace the binary's blob in place.
			blob := filepath.Join(cat.Path(), "blobs", "sha256", digest.FromBytes(binary).Encoded())
			require.FileExists(t, blob)
			require.NoError(t, os.Remove(blob))
			require.NoError(t, os.WriteFile(blob, tt.tampered, 0o600))

			_, _, err = cat.FetchByPlatform(ctx, ref, "linux/amd64")
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// storeCorruptPlugin stores provider name@version in cat and overwrites its
// manifest blob with same-length garbage, so reading it fails verification.
func storeCorruptPlugin(t *testing.T, cat *LocalCatalog, name, version string) {
	t.Helper()
	info, err := cat.StoreMultiPlatform(t.Context(), testPluginRef(name, version),
		[]PlatformBinary{{Platform: "linux/amd64", Data: []byte(name)}}, nil, false)
	require.NoError(t, err)

	blob := filepath.Join(cat.Path(), "blobs", "sha256", digest.Digest(info.Digest).Encoded())
	data, err := os.ReadFile(blob)
	require.NoError(t, err)
	for i := range data {
		data[i] = 'x'
	}
	require.NoError(t, os.Remove(blob))
	require.NoError(t, os.WriteFile(blob, data, 0o600))
}

func TestLocalCatalog_Find_CorruptCandidateIsAnError(t *testing.T) {
	t.Parallel()
	cat := newTestLocalCatalog(t)
	ctx := t.Context()
	storeCorruptPlugin(t, cat, "p", "1.0.0")
	_, err := cat.StoreMultiPlatform(ctx, testPluginRef("q", "1.0.0"),
		[]PlatformBinary{{Platform: "linux/amd64", Data: []byte("q")}}, nil, false)
	require.NoError(t, err)

	tests := []struct {
		name         string
		query        ArtifactQuery
		wantNotFound bool
	}{
		{name: "latest of the corrupt name", query: ArtifactQuery{Name: "p", Kind: ArtifactKindProvider}},
		{name: "corrupt version", query: ArtifactQuery{Name: "p", Kind: ArtifactKindProvider, Version: "1.0.0"}},
		{name: "built source", query: ArtifactQuery{Name: "p", Kind: ArtifactKindProvider, Source: SourceSelector{Kind: SourceBuilt}}},
		{name: "another version is not blocked", query: ArtifactQuery{Name: "p", Kind: ArtifactKindProvider, Version: "2.0.0"}, wantNotFound: true},
		{name: "pulled source is not blocked by a build", query: ArtifactQuery{Name: "p", Kind: ArtifactKindProvider, Source: SourceSelector{Kind: SourceRemote, Remote: "ghcr.io/org"}}, wantNotFound: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := cat.Find(ctx, tt.query)
			require.Error(t, err)
			assert.Equal(t, tt.wantNotFound, IsNotFound(err), "err: %v", err)
			if !tt.wantNotFound {
				assert.ErrorIs(t, err, content.ErrMismatchedDigest)
			}
		})
	}

	t.Run("other names still resolve", func(t *testing.T) {
		t.Parallel()
		info, err := cat.Find(ctx, ArtifactQuery{Name: "q", Kind: ArtifactKindProvider})
		require.NoError(t, err)
		assert.Equal(t, "q", info.Reference.Name)
	})
}

func TestIsCandidateTag(t *testing.T) {
	t.Parallel()
	query := func(version string, source SourceSelector) ArtifactQuery {
		return ArtifactQuery{Name: "p", Kind: ArtifactKindProvider, Version: version, Source: source}
	}
	built := SourceSelector{Kind: SourceBuilt}
	remote := SourceSelector{Kind: SourceRemote, Remote: "127.0.0.1:5000/org"}

	tests := []struct {
		name  string
		tag   string
		query ArtifactQuery
		want  bool
	}{
		{name: "built tag", tag: "provider/p:1.0.0", query: query("", SourceSelector{}), want: true},
		{name: "pulled tag", tag: "ghcr.io/org/providers/p:1.0.0", query: query("", SourceSelector{}), want: true},
		{name: "pulled tag with port", tag: "127.0.0.1:5000/org/providers/p:1.0.0", query: query("", remote), want: true},
		{name: "digest tag", tag: "provider/p@sha256:abc", query: query("1.0.0", SourceSelector{}), want: true},
		{name: "other name", tag: "provider/pp:1.0.0", query: query("", SourceSelector{}), want: false},
		{name: "other kind", tag: "solution/p:1.0.0", query: query("", SourceSelector{}), want: false},
		{name: "other version", tag: "provider/p:2.0.0", query: query("1.0.0", SourceSelector{}), want: false},
		{name: "constraint match", tag: "provider/p:1.2.0", query: query("^1.0.0", SourceSelector{}), want: true},
		{name: "built source skips pulled", tag: "ghcr.io/org/providers/p:1.0.0", query: query("", built), want: false},
		{name: "remote source skips built", tag: "provider/p:1.0.0", query: query("", remote), want: false},
		{name: "remote source skips other remote", tag: "ghcr.io/org/providers/p:1.0.0", query: query("", remote), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isCandidateTag(tt.tag, tt.query))
		})
	}
}
