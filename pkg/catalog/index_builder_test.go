// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func providerRef(name, version string) Reference {
	return Reference{Kind: ArtifactKindProvider, Name: name, Version: semver.MustParse(version)}
}

// platformManifestBuilder returns a per-platform child with a placeholder
// identity; the index replaces it with its own when the index is built.
func platformManifestBuilder(data []byte) *ManifestBuilder {
	return NewManifestBuilder(providerRef("placeholder", "0.0.1")).
		Layers(Layer{MediaType: MediaTypeProviderBinary, Data: data})
}

func TestIndexBuilder_Index(t *testing.T) {
	ref := providerRef("demo", "1.0.0")

	tests := []struct {
		name    string
		builder *IndexBuilder
		wantErr string
	}{
		{name: "no platforms", builder: NewIndexBuilder(ref), wantErr: "no platforms"},
		{
			name:    "invalid ref",
			builder: NewIndexBuilder(Reference{Kind: ArtifactKindProvider, Name: "demo"}),
			wantErr: "building index",
		},
		{
			name:    "invalid platform",
			builder: NewIndexBuilder(ref).Platform("bogus", platformManifestBuilder([]byte("x"))),
			wantErr: "invalid platform",
		},
		{
			name:    "nil manifest",
			builder: NewIndexBuilder(ref).Platform("linux/amd64", nil),
			wantErr: "nil manifest",
		},
		{
			name:    "invalid manifest",
			builder: NewIndexBuilder(ref).Platform("linux/amd64", NewManifestBuilder(ref)),
			wantErr: "platform linux/amd64",
		},
		{
			name: "two platforms",
			builder: NewIndexBuilder(ref).
				Platform("linux/amd64", platformManifestBuilder([]byte("amd64"))).
				Platform("darwin/arm64", platformManifestBuilder([]byte("arm64"))).
				Annotations(map[string]string{
					AnnotationTitle:           "demo",
					AnnotationSourceCanonical: "example.com/demo",
					AnnotationOrigin:          "example.com",
				}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idx, err := tt.builder.Index()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, ocispec.MediaTypeImageIndex, idx.MediaType)
			require.Len(t, idx.Manifests, 2)
			assert.Equal(t, "linux", idx.Manifests[0].Platform.OS)
			assert.Equal(t, "amd64", idx.Manifests[0].Platform.Architecture)
			assert.Equal(t, "darwin", idx.Manifests[1].Platform.OS)
			assert.Equal(t, ocispec.MediaTypeImageManifest, idx.Manifests[0].MediaType)
			assert.Equal(t, "demo", idx.Annotations[AnnotationTitle])
			assert.Equal(t, "demo", idx.Annotations[AnnotationArtifactName])
			assert.Equal(t, ArtifactKindProvider.String(), idx.Annotations[AnnotationArtifactType])
			assert.Equal(t, "1.0.0", idx.Annotations[AnnotationVersion])
			assert.NotContains(t, idx.Annotations, AnnotationSourceCanonical)
			assert.NotContains(t, idx.Annotations, AnnotationOrigin)
		})
	}
}

func TestIndexBuilder_StampsChildren(t *testing.T) {
	ref := providerRef("demo", "1.0.0")
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	child := platformManifestBuilder([]byte("amd64"))

	builder := NewIndexBuilder(ref).
		CreatedAt(createdAt).
		Platform("linux/amd64", child).
		Platform("darwin/arm64", platformManifestBuilder([]byte("arm64")))

	idx, err := builder.Index()
	require.NoError(t, err)

	for i, want := range []string{"linux/amd64", "darwin/arm64"} {
		entry := idx.Manifests[i]
		assert.Equal(t, want, entry.Annotations[AnnotationPlatform])
		assert.Equal(t, "demo", entry.Annotations[AnnotationArtifactName],
			"children carry the index identity, not their own")
		assert.Equal(t, "1.0.0", entry.Annotations[AnnotationVersion])
		assert.Equal(t, formatCreated(createdAt), entry.Annotations[AnnotationCreated])
	}
	assert.Equal(t, formatCreated(createdAt), idx.Annotations[AnnotationCreated])

	assert.Equal(t, "placeholder", child.Reference().Name, "the caller's child builder is not mutated")
	assert.Empty(t, child.platform)

	p := &recordingPusher{}
	_, err = builder.Push(context.Background(), p)
	require.NoError(t, err)
	var manifest ocispec.Manifest
	require.NoError(t, json.Unmarshal(p.blobs[idx.Manifests[0].Digest], &manifest))
	assert.Equal(t, MediaTypeProviderConfig, manifest.Config.MediaType)
	var cfg artifactConfig
	require.NoError(t, json.Unmarshal(p.blobs[manifest.Config.Digest], &cfg))
	assert.Equal(t, "linux/amd64", cfg.Platform)
	assert.Equal(t, "demo", cfg.Name)
}

func TestIndexBuilder_Reproducible(t *testing.T) {
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	build := func() *IndexBuilder {
		return NewIndexBuilder(providerRef("demo", "1.0.0")).
			CreatedAt(createdAt).
			Platform("linux/amd64", platformManifestBuilder([]byte("amd64")))
	}

	a, err := build().Push(context.Background(), &recordingPusher{})
	require.NoError(t, err)
	b, err := build().Push(context.Background(), &recordingPusher{})
	require.NoError(t, err)
	assert.Equal(t, a.Digest, b.Digest)
}

func TestIndexBuilder_AnnotationTransformer(t *testing.T) {
	ref := providerRef("demo", "1.0.0")
	annotations := map[string]string{
		AnnotationTitle:       "demo",
		AnnotationDescription: "drop me",
	}
	build := func() *IndexBuilder {
		return NewIndexBuilder(ref).
			Platform("linux/amd64", platformManifestBuilder([]byte("amd64"))).
			Annotations(annotations)
	}

	index, err := build().Index()
	require.NoError(t, err)
	assert.Equal(t, "drop me", index.Annotations[AnnotationDescription])
	assert.Equal(t, ref, build().Reference())

	filtered, err := build().AnnotationTransformer(DropAnnotations(AnnotationDescription)).Index()
	require.NoError(t, err)
	assert.NotContains(t, filtered.Annotations, AnnotationDescription)
	assert.Equal(t, "demo", filtered.Annotations[AnnotationTitle])

	identity, err := build().AnnotationTransformer(dropAllAnnotations).Index()
	require.NoError(t, err)
	assert.Equal(t, "demo", identity.Annotations[AnnotationArtifactName],
		"identity is stamped after the transformer, so it cannot be filtered out")
	assert.NotContains(t, identity.Annotations, AnnotationTitle)

	reset, err := build().
		AnnotationTransformer(dropAllAnnotations).
		AnnotationTransformer(nil).
		Index()
	require.NoError(t, err)
	assert.Equal(t, "drop me", reset.Annotations[AnnotationDescription])
}

func TestIndexBuilder_Push(t *testing.T) {
	build := func() *IndexBuilder {
		return NewIndexBuilder(providerRef("demo", "1.0.0")).
			Platform("linux/amd64", platformManifestBuilder([]byte("amd64"))).
			Platform("darwin/arm64", platformManifestBuilder([]byte("arm64")))
	}

	t.Run("pushes manifests then index", func(t *testing.T) {
		p := &recordingPusher{}
		desc, err := build().Push(context.Background(), p)
		require.NoError(t, err)

		// 2 platforms x (layer, config, manifest) + index
		require.Len(t, p.pushed, 7)
		assert.Equal(t, ocispec.MediaTypeImageIndex, p.pushed[6].MediaType)
		assert.Equal(t, ocispec.MediaTypeImageIndex, desc.MediaType)

		var idx ocispec.Index
		require.NoError(t, json.Unmarshal(p.blobs[desc.Digest], &idx))
		require.Len(t, idx.Manifests, 2)
		for _, m := range idx.Manifests {
			_, ok := p.blobs[m.Digest]
			assert.True(t, ok, "platform manifest must be pushed before the index")
		}
	})

	t.Run("invalid chain", func(t *testing.T) {
		_, err := NewIndexBuilder(providerRef("demo", "1.0.0")).Push(context.Background(), &recordingPusher{})
		require.Error(t, err)
	})

	t.Run("platform push fails", func(t *testing.T) {
		p := &recordingPusher{errFor: func(ocispec.Descriptor) error { return errors.New("boom") }}
		_, err := build().Push(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pushing platform linux/amd64")
	})

	t.Run("index push fails", func(t *testing.T) {
		p := &recordingPusher{errFor: func(d ocispec.Descriptor) error {
			if d.MediaType == ocispec.MediaTypeImageIndex {
				return errors.New("boom")
			}
			return nil
		}}
		_, err := build().Push(context.Background(), p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pushing image index")
	})
}
