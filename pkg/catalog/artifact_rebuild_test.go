// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

// blobFetcher is an in-memory content.Fetcher that records which digests were
// actually read, so tests can assert that deduplicated layers are never
// transferred.
type blobFetcher struct {
	blobs   map[digest.Digest][]byte
	fetched []digest.Digest
	err     error
}

func newBlobFetcher() *blobFetcher {
	return &blobFetcher{blobs: map[digest.Digest][]byte{}}
}

func (f *blobFetcher) add(mediaType string, data []byte) ocispec.Descriptor {
	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}
	f.blobs[desc.Digest] = data
	return desc
}

func (f *blobFetcher) addJSON(t *testing.T, mediaType string, v any) ocispec.Descriptor {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return f.add(mediaType, data)
}

func (f *blobFetcher) Fetch(_ context.Context, desc ocispec.Descriptor) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.blobs[desc.Digest]
	if !ok {
		return nil, errdef.ErrNotFound
	}
	f.fetched = append(f.fetched, desc.Digest)
	return io.NopCloser(bytes.NewReader(data)), nil
}

func solutionRef(name, version string) Reference {
	return Reference{
		Kind:    ArtifactKindSolution,
		Name:    name,
		Version: semver.MustParse(version),
	}
}

func TestNewArtifactConfig(t *testing.T) {
	annotations := map[string]string{AnnotationArtifactName: "demo"}
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)

	t.Run("single platform omits platform", func(t *testing.T) {
		data, err := newArtifactConfig(solutionRef("demo", "1.2.3"), "", createdAt, annotations)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))
		assert.Equal(t, ArtifactKindSolution.String(), raw["kind"])
		assert.Equal(t, "demo", raw["name"])
		assert.Equal(t, "1.2.3", raw["version"])
		assert.NotContains(t, raw, "platform")
		assert.Equal(t, "2025-01-02T03:04:05Z", raw["createdAt"])

		var cfg artifactConfig
		require.NoError(t, json.Unmarshal(data, &cfg))
		assert.Equal(t, annotations, cfg.Annotations)
	})

	t.Run("multi platform records platform", func(t *testing.T) {
		data, err := newArtifactConfig(solutionRef("demo", "1.2.3"), "linux/amd64", createdAt, nil)
		require.NoError(t, err)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(data, &raw))
		assert.Equal(t, "linux/amd64", raw["platform"])
		assert.NotContains(t, raw, "annotations")
	})

	t.Run("identity is part of the content", func(t *testing.T) {
		a, err := newArtifactConfig(solutionRef("demo", "1.0.0"), "", createdAt, nil)
		require.NoError(t, err)
		b, err := newArtifactConfig(solutionRef("renamed", "1.0.0"), "", createdAt, nil)
		require.NoError(t, err)
		assert.NotEqual(t, digest.FromBytes(a), digest.FromBytes(b))
	})

	t.Run("same inputs give the same bytes", func(t *testing.T) {
		a, err := newArtifactConfig(solutionRef("demo", "1.0.0"), "", createdAt, annotations)
		require.NoError(t, err)
		b, err := newArtifactConfig(solutionRef("demo", "1.0.0"), "", createdAt, annotations)
		require.NoError(t, err)
		assert.Equal(t, a, b)
	})
}

func TestFetchJSON(t *testing.T) {
	fetcher := newBlobFetcher()
	desc := fetcher.addJSON(t, ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
	})
	garbage := fetcher.add(ocispec.MediaTypeImageManifest, []byte("not json"))

	t.Run("decodes a verified blob", func(t *testing.T) {
		var manifest ocispec.Manifest
		require.NoError(t, fetchJSON(context.Background(), fetcher, desc, &manifest))
		assert.Equal(t, 2, manifest.SchemaVersion)
	})

	t.Run("digest mismatch is rejected", func(t *testing.T) {
		tampered := desc
		tampered.Digest = digest.FromString("something else")

		var manifest ocispec.Manifest
		err := fetchJSON(context.Background(), fetcher, tampered, &manifest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetching")
	})

	t.Run("size mismatch is rejected", func(t *testing.T) {
		tampered := desc
		tampered.Size++

		var manifest ocispec.Manifest
		err := fetchJSON(context.Background(), fetcher, tampered, &manifest)
		require.Error(t, err)
	})

	t.Run("malformed json is reported as a decode failure", func(t *testing.T) {
		var manifest ocispec.Manifest
		err := fetchJSON(context.Background(), fetcher, garbage, &manifest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "decoding")
	})

	t.Run("fetch failure is wrapped", func(t *testing.T) {
		boom := errors.New("boom")
		failing := &blobFetcher{blobs: fetcher.blobs, err: boom}

		var manifest ocispec.Manifest
		err := fetchJSON(context.Background(), failing, desc, &manifest)
		require.ErrorIs(t, err, boom)
	})
}

func TestReportProgress(t *testing.T) {
	desc := ocispec.Descriptor{Digest: digest.FromString("x")}

	assert.NotPanics(t, func() { reportProgress(nil, desc) })

	var seen []ocispec.Descriptor
	reportProgress(func(d ocispec.Descriptor) { seen = append(seen, d) }, desc)
	require.Len(t, seen, 1)
	assert.Equal(t, desc, seen[0])
}

func TestOpenLayer(t *testing.T) {
	ctx := context.Background()

	t.Run("fetches and reports progress", func(t *testing.T) {
		fetcher := newBlobFetcher()
		desc := fetcher.add(MediaTypeSolutionContent, []byte("payload"))

		var progress []digest.Digest
		rc, err := openLayer(fetcher, desc, func(d ocispec.Descriptor) {
			progress = append(progress, d.Digest)
		})(ctx)
		require.NoError(t, err)
		defer rc.Close()

		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, []byte("payload"), data)
		assert.Equal(t, []digest.Digest{desc.Digest}, progress)
	})

	t.Run("negative size is rejected before any transfer", func(t *testing.T) {
		fetcher := newBlobFetcher()
		desc := fetcher.add(MediaTypeSolutionContent, []byte("payload"))
		desc.Size = -1

		_, err := openLayer(fetcher, desc, nil)(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid layer size")
		assert.Empty(t, fetcher.fetched, "the size bound must be checked before fetching")
	})

	t.Run("oversized layer is rejected before any transfer", func(t *testing.T) {
		fetcher := newBlobFetcher()
		desc := fetcher.add(MediaTypeSolutionContent, []byte("payload"))
		desc.Size = maxLayerSize + 1

		_, err := openLayer(fetcher, desc, nil)(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
		assert.Empty(t, fetcher.fetched, "the size bound must be checked before fetching")
	})

	t.Run("fetch failure is wrapped", func(t *testing.T) {
		boom := errors.New("boom")
		fetcher := &blobFetcher{blobs: map[digest.Digest][]byte{}, err: boom}
		desc := ocispec.Descriptor{MediaType: MediaTypeSolutionContent, Digest: digest.FromString("x"), Size: 1}

		_, err := openLayer(fetcher, desc, nil)(ctx)
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "fetching layer")
	})
}

// sourceManifest seeds a fetcher with a single-platform artifact that claims a
// remote origin and an old identity, and returns its manifest descriptor.
func sourceManifest(t *testing.T, fetcher *blobFetcher, payload []byte) (ocispec.Descriptor, ocispec.Descriptor, ocispec.Descriptor) {
	t.Helper()

	layer := fetcher.add(MediaTypeSolutionContent, payload)
	config := fetcher.add(MediaTypeSolutionConfig, []byte(`{"kind":"solution","name":"old","version":"1.0.0"}`))
	manifest := fetcher.addJSON(t, ocispec.MediaTypeImageManifest, ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    config,
		Layers:    []ocispec.Descriptor{layer},
		Annotations: map[string]string{
			AnnotationArtifactName:    "old",
			AnnotationArtifactType:    ArtifactKindSolution.String(),
			AnnotationVersion:         "1.0.0",
			AnnotationSourceCanonical: "example.com/solutions/old",
			AnnotationTitle:           "keep me",
		},
	})
	return manifest, config, layer
}

func TestRebuildManifest(t *testing.T) {
	ctx := context.Background()
	target := solutionRef("new", "2.0.0")

	t.Run("mints a new config and rewrites identity", func(t *testing.T) {
		fetcher := newBlobFetcher()
		manifestDesc, configDesc, layerDesc := sourceManifest(t, fetcher, []byte("payload"))

		builder, err := rebuildManifest(ctx, fetcher, manifestDesc, target, nil)
		require.NoError(t, err)

		rebuilt, err := builder.Manifest()
		require.NoError(t, err)

		require.Len(t, rebuilt.Layers, 1)
		assert.Equal(t, layerDesc, rebuilt.Layers[0])

		assert.NotEqual(t, configDesc.Digest, rebuilt.Config.Digest)
		assert.Equal(t, ConfigMediaTypeForKind(ArtifactKindSolution), rebuilt.Config.MediaType)

		var cfg artifactConfig
		require.NoError(t, json.Unmarshal(fetchPushedConfig(ctx, t, builder), &cfg))
		assert.Equal(t, "new", cfg.Name)
		assert.Equal(t, "2.0.0", cfg.Version)
		assert.Empty(t, cfg.Platform)
		assert.Equal(t, rebuilt.Annotations, cfg.Annotations,
			"the config carries the same annotations as the manifest")

		assert.Equal(t, "new", rebuilt.Annotations[AnnotationArtifactName])
		assert.Equal(t, "2.0.0", rebuilt.Annotations[AnnotationVersion])
		assert.NotContains(t, rebuilt.Annotations, AnnotationSourceCanonical)
		assert.Equal(t, "keep me", rebuilt.Annotations[AnnotationTitle])
	})

	t.Run("layers the destination already holds are never downloaded", func(t *testing.T) {
		fetcher := newBlobFetcher()
		manifestDesc, _, layerDesc := sourceManifest(t, fetcher, []byte("payload"))

		builder, err := rebuildManifest(ctx, fetcher, manifestDesc, target, nil)
		require.NoError(t, err)

		fetcher.fetched = nil

		p := &dedupPusher{existing: map[digest.Digest]bool{layerDesc.Digest: true}}
		_, err = builder.Push(ctx, p)
		require.NoError(t, err)
		assert.Empty(t, fetcher.fetched, "an already-stored layer must not be transferred")
	})

	t.Run("missing layers are downloaded on push", func(t *testing.T) {
		fetcher := newBlobFetcher()
		manifestDesc, _, layerDesc := sourceManifest(t, fetcher, []byte("payload"))

		builder, err := rebuildManifest(ctx, fetcher, manifestDesc, target, nil)
		require.NoError(t, err)
		fetcher.fetched = nil

		p := &dedupPusher{}
		_, err = builder.Push(ctx, p)
		require.NoError(t, err)
		assert.Equal(t, []digest.Digest{layerDesc.Digest}, fetcher.fetched)
		assert.Equal(t, []byte("payload"), p.blobs[layerDesc.Digest])
	})

	t.Run("unreadable manifest is an error", func(t *testing.T) {
		fetcher := newBlobFetcher()
		missing := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageManifest,
			Digest:    digest.FromString("absent"),
			Size:      1,
		}

		_, err := rebuildManifest(ctx, fetcher, missing, target, nil)
		require.Error(t, err)
	})
}

// fetchPushedConfig pushes the builder into a throwaway pusher and returns the
// config blob it wrote, which is the only way to read a minted config.
func fetchPushedConfig(ctx context.Context, t *testing.T, builder *ManifestBuilder) []byte {
	t.Helper()

	manifest, err := builder.Manifest()
	require.NoError(t, err)

	p := &dedupPusher{}
	_, err = builder.Push(ctx, p)
	require.NoError(t, err)

	data, ok := p.blobs[manifest.Config.Digest]
	require.True(t, ok, "config blob must be pushed")
	return data
}

func TestRebuildIndex(t *testing.T) {
	ctx := context.Background()
	target := solutionRef("new", "2.0.0")

	linux := &ocispec.Platform{OS: "linux", Architecture: "amd64"}
	darwin := &ocispec.Platform{OS: "darwin", Architecture: "arm64"}

	seedIndex := func(t *testing.T, entries func(*blobFetcher) []ocispec.Descriptor) (*blobFetcher, ocispec.Descriptor) {
		t.Helper()
		fetcher := newBlobFetcher()
		desc := fetcher.addJSON(t, ocispec.MediaTypeImageIndex, ocispec.Index{
			Versioned:   specs.Versioned{SchemaVersion: 2},
			MediaType:   ocispec.MediaTypeImageIndex,
			Manifests:   entries(fetcher),
			Annotations: map[string]string{AnnotationSourceCanonical: "example.com/solutions/old"},
		})
		return fetcher, desc
	}

	platformEntry := func(t *testing.T, fetcher *blobFetcher, payload []byte, platform *ocispec.Platform) ocispec.Descriptor {
		t.Helper()
		entry, _, _ := sourceManifest(t, fetcher, payload)
		entry.Platform = platform
		return entry
	}

	t.Run("rebuilds every platform under the new identity", func(t *testing.T) {
		fetcher, desc := seedIndex(t, func(f *blobFetcher) []ocispec.Descriptor {
			return []ocispec.Descriptor{
				platformEntry(t, f, []byte("amd64"), linux),
				platformEntry(t, f, []byte("arm64"), darwin),
			}
		})

		builder, err := rebuildIndex(ctx, fetcher, desc, target, nil)
		require.NoError(t, err)

		index, err := builder.Index()
		require.NoError(t, err)
		require.Len(t, index.Manifests, 2)
		assert.Equal(t, "linux", index.Manifests[0].Platform.OS)
		assert.Equal(t, "darwin", index.Manifests[1].Platform.OS)

		assert.Equal(t, "new", index.Annotations[AnnotationArtifactName])
		assert.NotContains(t, index.Annotations, AnnotationSourceCanonical)
		for _, entry := range index.Manifests {
			assert.Equal(t, "new", entry.Annotations[AnnotationArtifactName])
			assert.NotContains(t, entry.Annotations, AnnotationSourceCanonical)
		}

		p := &dedupPusher{}
		_, err = builder.Push(ctx, p)
		require.NoError(t, err)
		for i, want := range []string{"linux/amd64", "darwin/arm64"} {
			var manifest ocispec.Manifest
			require.NoError(t, json.Unmarshal(p.blobs[index.Manifests[i].Digest], &manifest))
			var cfg artifactConfig
			require.NoError(t, json.Unmarshal(p.blobs[manifest.Config.Digest], &cfg))
			assert.Equal(t, want, cfg.Platform, "each platform config records its platform")
			assert.Equal(t, "new", cfg.Name)
		}
	})

	t.Run("empty index is rejected", func(t *testing.T) {
		fetcher, desc := seedIndex(t, func(*blobFetcher) []ocispec.Descriptor { return nil })

		_, err := rebuildIndex(ctx, fetcher, desc, target, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no manifests")
	})

	t.Run("non-manifest entry is rejected", func(t *testing.T) {
		fetcher, desc := seedIndex(t, func(f *blobFetcher) []ocispec.Descriptor {
			entry := platformEntry(t, f, []byte("amd64"), linux)
			entry.MediaType = ocispec.MediaTypeImageIndex
			return []ocispec.Descriptor{entry}
		})

		_, err := rebuildIndex(ctx, fetcher, desc, target, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported media type")
	})

	t.Run("entry without a platform is rejected", func(t *testing.T) {
		fetcher, desc := seedIndex(t, func(f *blobFetcher) []ocispec.Descriptor {
			return []ocispec.Descriptor{platformEntry(t, f, []byte("amd64"), nil)}
		})

		_, err := rebuildIndex(ctx, fetcher, desc, target, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "has no platform")
	})

	t.Run("platform failure names the platform", func(t *testing.T) {
		fetcher, desc := seedIndex(t, func(f *blobFetcher) []ocispec.Descriptor {
			entry := platformEntry(t, f, []byte("amd64"), linux)
			entry.Digest = digest.FromString("absent")
			return []ocispec.Descriptor{entry}
		})

		_, err := rebuildIndex(ctx, fetcher, desc, target, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "platform linux/amd64")
	})

	t.Run("unreadable index is an error", func(t *testing.T) {
		fetcher := newBlobFetcher()
		missing := ocispec.Descriptor{
			MediaType: ocispec.MediaTypeImageIndex,
			Digest:    digest.FromString("absent"),
			Size:      1,
		}

		_, err := rebuildIndex(ctx, fetcher, missing, target, nil)
		require.Error(t, err)
	})
}

func TestRebuildArtifact(t *testing.T) {
	ctx := context.Background()
	target := solutionRef("new", "2.0.0")

	t.Run("image manifest yields a manifest chain", func(t *testing.T) {
		fetcher := newBlobFetcher()
		desc, _, _ := sourceManifest(t, fetcher, []byte("payload"))

		builder, err := rebuildArtifact(ctx, fetcher, desc, target, nil)
		require.NoError(t, err)
		assert.IsType(t, &ManifestBuilder{}, builder)
	})

	t.Run("image index yields an index chain", func(t *testing.T) {
		fetcher := newBlobFetcher()
		entry, _, _ := sourceManifest(t, fetcher, []byte("amd64"))
		entry.Platform = &ocispec.Platform{OS: "linux", Architecture: "amd64"}
		desc := fetcher.addJSON(t, ocispec.MediaTypeImageIndex, ocispec.Index{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispec.MediaTypeImageIndex,
			Manifests: []ocispec.Descriptor{entry},
		})

		builder, err := rebuildArtifact(ctx, fetcher, desc, target, nil)
		require.NoError(t, err)
		assert.IsType(t, &IndexBuilder{}, builder)
	})

	t.Run("target without a version is rejected", func(t *testing.T) {
		fetcher := newBlobFetcher()
		desc, _, _ := sourceManifest(t, fetcher, []byte("payload"))

		_, err := rebuildArtifact(ctx, fetcher, desc, Reference{Kind: ArtifactKindSolution, Name: "new"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no version")
	})

	t.Run("unsupported media type is rejected", func(t *testing.T) {
		fetcher := newBlobFetcher()
		desc := fetcher.add("application/vnd.example.unknown", []byte("x"))

		_, err := rebuildArtifact(ctx, fetcher, desc, target, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported media type")
	})
}

// remoteSolutionBuilder returns a single-layer solution builder for ref,
// carrying unrelated metadata that every pipeline must preserve.
func remoteSolutionBuilder(ref Reference) *ManifestBuilder {
	return NewManifestBuilder(ref).
		Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte("name: demo")}).
		Annotations(map[string]string{AnnotationTitle: "keep me"})
}
