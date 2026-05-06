// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// storeMultiPlatformInFake pushes a multi-platform OCI image index into the
// fake registry backing a test RemoteCatalog.  Each platform binary gets its
// own manifest and content layer, all tied together by an OCI image index
// tagged at the requested ref.
func storeMultiPlatformInFake(t *testing.T, cat *RemoteCatalog, ref Reference, binaries []PlatformBinary) {
	t.Helper()
	ctx := t.Context()

	repo, err := cat.getRepository(ref)
	require.NoError(t, err)

	platDescs := make([]ocispec.Descriptor, 0, len(binaries))

	for _, pb := range binaries {
		ociPlat, err := PlatformToOCI(pb.Platform)
		require.NoError(t, err)

		// Push binary content layer.
		contentDigest := digest.FromBytes(pb.Data)
		contentDesc := ocispec.Descriptor{
			MediaType: MediaTypeProviderBinary,
			Digest:    contentDigest,
			Size:      int64(len(pb.Data)),
		}
		require.NoError(t, repo.Push(ctx, contentDesc, bytes.NewReader(pb.Data)))

		// Push a small config blob.
		cfgData := []byte(`{}`)
		cfgDesc := ocispec.Descriptor{
			MediaType: "application/vnd.scafctl.provider.config.v1+json",
			Digest:    digest.FromBytes(cfgData),
			Size:      int64(len(cfgData)),
		}
		require.NoError(t, repo.Push(ctx, cfgDesc, bytes.NewReader(cfgData)))

		// Build the platform-specific manifest.
		manifest := ocispec.Manifest{
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    cfgDesc,
			Layers:    []ocispec.Descriptor{contentDesc},
			Annotations: map[string]string{
				AnnotationArtifactName: ref.Name,
				AnnotationPlatform:     pb.Platform,
			},
		}
		manifest.SchemaVersion = 2
		manifestData, err := json.Marshal(manifest)
		require.NoError(t, err)

		manifestDesc := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    digest.FromBytes(manifestData),
			Size:      int64(len(manifestData)),
			Platform:  ociPlat,
			Annotations: map[string]string{
				AnnotationPlatform: pb.Platform,
			},
		}
		require.NoError(t, repo.Push(ctx, manifestDesc, bytes.NewReader(manifestData)))

		platDescs = append(platDescs, manifestDesc)
	}

	// Build the OCI image index.
	index := ocispec.Index{
		MediaType: ocispec.MediaTypeImageIndex,
		Manifests: platDescs,
		Annotations: map[string]string{
			AnnotationArtifactName: ref.Name,
			AnnotationArtifactType: ArtifactKindProvider.String(),
		},
	}
	index.SchemaVersion = 2
	indexData, err := json.Marshal(index)
	require.NoError(t, err)

	indexDesc := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageIndex,
		Digest:    digest.FromBytes(indexData),
		Size:      int64(len(indexData)),
	}
	require.NoError(t, repo.Push(ctx, indexDesc, bytes.NewReader(indexData)))

	tag := cat.tagForRef(ref)
	require.NoError(t, repo.Tag(ctx, indexDesc, tag))
}

func TestRemoteCatalog_FetchByPlatform_RoundTrip(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "multi-provider",
		Version: semver.MustParse("2.0.0"),
	}

	binaries := []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-amd64-binary")},
		{Platform: "darwin/arm64", Data: []byte("darwin-arm64-binary")},
	}

	storeMultiPlatformInFake(t, cat, ref, binaries)

	ctx := t.Context()

	// Fetch linux/amd64
	data, info, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("linux-amd64-binary"), data)
	assert.Equal(t, "linux/amd64", info.Annotations[AnnotationPlatform])
	assert.NotEmpty(t, info.Digest)
	// Digest should be the layer content digest, not the index digest.
	expectedDigest := digest.FromBytes([]byte("linux-amd64-binary")).String()
	assert.Equal(t, expectedDigest, info.Digest)

	// Fetch darwin/arm64
	data, info, err = cat.FetchByPlatform(ctx, ref, "darwin/arm64")
	require.NoError(t, err)
	assert.Equal(t, []byte("darwin-arm64-binary"), data)
	assert.Equal(t, "darwin/arm64", info.Annotations[AnnotationPlatform])
}

func TestRemoteCatalog_FetchByPlatform_PlatformNotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "linux-only",
		Version: semver.MustParse("1.0.0"),
	}

	storeMultiPlatformInFake(t, cat, ref, []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-binary")},
	})

	_, _, err := cat.FetchByPlatform(t.Context(), ref, "darwin/arm64")
	require.Error(t, err)
	assert.True(t, IsPlatformNotFound(err), "expected PlatformNotFoundError, got %T: %v", err, err)
}

func TestRemoteCatalog_FetchByPlatform_SinglePlatformManifest(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "single-plat",
		Version: semver.MustParse("1.0.0"),
	}

	// Store as a normal single-platform manifest (via Store).
	_, err := cat.Store(ctx, ref, []byte("single-binary"), nil, nil, false)
	require.NoError(t, err)

	// FetchByPlatform should still work (single-platform fallback).
	data, _, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("single-binary"), data)
}

func TestRemoteCatalog_ListPlatforms_MultiPlatform(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "multi-plat",
		Version: semver.MustParse("1.0.0"),
	}

	storeMultiPlatformInFake(t, cat, ref, []PlatformBinary{
		{Platform: "linux/amd64", Data: []byte("linux-amd64")},
		{Platform: "linux/arm64", Data: []byte("linux-arm64")},
		{Platform: "darwin/arm64", Data: []byte("darwin-arm64")},
	})

	platforms, err := cat.ListPlatforms(t.Context(), ref)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"linux/amd64", "linux/arm64", "darwin/arm64"}, platforms)
}

func TestRemoteCatalog_ListPlatforms_SinglePlatform(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "single-plat",
		Version: semver.MustParse("1.0.0"),
	}

	_, err := cat.Store(ctx, ref, []byte("single-binary"), nil, nil, false)
	require.NoError(t, err)

	platforms, err := cat.ListPlatforms(ctx, ref)
	require.NoError(t, err)
	assert.Nil(t, platforms, "single-platform artifact should return nil platforms")
}

// Verify that RemoteCatalog implements PlatformAwareCatalog at compile time.
func TestRemoteCatalog_ImplementsPlatformAwareCatalog(t *testing.T) {
	var _ PlatformAwareCatalog = (*RemoteCatalog)(nil)
}

func TestRemoteCatalog_FetchByPlatform_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "nonexistent",
		Version: semver.MustParse("1.0.0"),
	}
	_, _, err := cat.FetchByPlatform(t.Context(), ref, "linux/amd64")
	require.Error(t, err)
}

func TestRemoteCatalog_ListPlatforms_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "nonexistent",
		Version: semver.MustParse("1.0.0"),
	}
	_, err := cat.ListPlatforms(t.Context(), ref)
	require.Error(t, err)
}

func TestRemoteCatalog_FetchByPlatform_SinglePlatform(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := Reference{
		Kind:    ArtifactKindProvider,
		Name:    "single-fetch",
		Version: semver.MustParse("1.0.0"),
	}

	_, err := cat.Store(ctx, ref, []byte("single-binary"), nil, nil, false)
	require.NoError(t, err)

	data, info, err := cat.FetchByPlatform(ctx, ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("single-binary"), data)
	assert.NotEmpty(t, info.Digest)
}
