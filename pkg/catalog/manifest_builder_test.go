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

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"
)

type recordingPusher struct {
	pushed []ocispec.Descriptor
	blobs  map[digest.Digest][]byte
	errFor func(ocispec.Descriptor) error
}

func (p *recordingPusher) Push(_ context.Context, desc ocispec.Descriptor, r io.Reader) error {
	if p.errFor != nil {
		if err := p.errFor(desc); err != nil {
			return err
		}
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if p.blobs == nil {
		p.blobs = map[digest.Digest][]byte{}
	}
	p.pushed = append(p.pushed, desc)
	p.blobs[desc.Digest] = data
	return nil
}

// dedupPusher is a recordingPusher that also answers blob-existence queries,
// which is what lets the builder skip layers the destination already stores.
type dedupPusher struct {
	recordingPusher
	existing    map[digest.Digest]bool
	existsErr   error
	existsCalls []digest.Digest
}

func (p *dedupPusher) Exists(_ context.Context, desc ocispec.Descriptor) (bool, error) {
	p.existsCalls = append(p.existsCalls, desc.Digest)
	if p.existsErr != nil {
		return false, p.existsErr
	}
	return p.existing[desc.Digest], nil
}

// countingCloser reports whether the layer reader was closed after the push.
type countingCloser struct {
	io.Reader
	closed int
}

func (c *countingCloser) Close() error {
	c.closed++
	return nil
}

func TestManifestBuilder_Manifest(t *testing.T) {
	content := Layer{MediaType: MediaTypeSolutionContent, Data: []byte("name: demo")}
	bundle := Layer{MediaType: MediaTypeSolutionBundle, Data: []byte("tar")}
	ref := solutionRef("demo", "1.0.0")

	tests := []struct {
		name    string
		builder *ManifestBuilder
		wantErr string
	}{
		{
			name:    "no layers",
			builder: NewManifestBuilder(ref),
			wantErr: "no layers",
		},
		{
			name:    "no version",
			builder: NewManifestBuilder(Reference{Kind: ArtifactKindSolution, Name: "demo"}).Layers(content),
			wantErr: "building manifest",
		},
		{
			name:    "no name",
			builder: NewManifestBuilder(Reference{Kind: ArtifactKindSolution}).Layers(content),
			wantErr: "building manifest",
		},
		{
			name:    "invalid kind",
			builder: NewManifestBuilder(Reference{Kind: "bogus", Name: "demo", Version: ref.Version}).Layers(content),
			wantErr: "building manifest",
		},
		{
			name: "full chain",
			builder: NewManifestBuilder(ref).
				Layers(content, bundle).
				Annotations(map[string]string{
					AnnotationTitle:           "demo",
					AnnotationSourceCanonical: "example.com/solutions/demo",
					AnnotationOrigin:          "example.com",
				}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := tt.builder.Manifest()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, 2, m.SchemaVersion)
			assert.Equal(t, ocispec.MediaTypeImageManifest, m.MediaType)
			assert.Equal(t, MediaTypeSolutionConfig, m.Config.MediaType)
			require.Len(t, m.Layers, 2)
			assert.Equal(t, content.Descriptor(), m.Layers[0])
			assert.Equal(t, bundle.Descriptor(), m.Layers[1])
			assert.Equal(t, "demo", m.Annotations[AnnotationTitle])
			assert.NotContains(t, m.Annotations, AnnotationSourceCanonical)
			assert.NotContains(t, m.Annotations, AnnotationOrigin)
			assert.NotContains(t, m.Annotations, AnnotationPlatform)
		})
	}
}

func TestManifestBuilder_InvalidRefError(t *testing.T) {
	_, err := NewManifestBuilder(Reference{Kind: ArtifactKindSolution, Name: "demo"}).
		Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte("x")}).
		Manifest()

	var builderErr *BuilderError
	require.ErrorAs(t, err, &builderErr)
	var refErr *InvalidReferenceError
	require.ErrorAs(t, err, &refErr)
}

func TestManifestBuilder_Identity(t *testing.T) {
	ref := solutionRef("demo", "1.0.0")
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	build := func() *ManifestBuilder {
		return NewManifestBuilder(ref).
			CreatedAt(createdAt).
			Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte("demo")}).
			Annotations(map[string]string{
				AnnotationArtifactName: "spoofed",
				AnnotationVersion:      "9.9.9",
				AnnotationArtifactType: ArtifactKindProvider.String(),
				AnnotationTitle:        "keep me",
			})
	}

	assert.Equal(t, ref, build().Reference())

	m, err := build().Manifest()
	require.NoError(t, err)
	assert.Equal(t, "demo", m.Annotations[AnnotationArtifactName], "the ref wins over caller metadata")
	assert.Equal(t, "1.0.0", m.Annotations[AnnotationVersion])
	assert.Equal(t, ArtifactKindSolution.String(), m.Annotations[AnnotationArtifactType])
	assert.Equal(t, formatCreated(createdAt), m.Annotations[AnnotationCreated])
	assert.Equal(t, "keep me", m.Annotations[AnnotationTitle])

	t.Run("config carries the manifest annotations", func(t *testing.T) {
		p := &recordingPusher{}
		_, err := build().Push(context.Background(), p)
		require.NoError(t, err)

		var cfg artifactConfig
		require.NoError(t, json.Unmarshal(p.blobs[m.Config.Digest], &cfg))
		assert.Equal(t, m.Annotations, cfg.Annotations)
		assert.Equal(t, "demo", cfg.Name)
		assert.Equal(t, "1.0.0", cfg.Version)
		assert.Equal(t, formatCreated(createdAt), cfg.CreatedAt)
	})

	t.Run("a fixed creation time gives reproducible digests", func(t *testing.T) {
		a, err := build().Push(context.Background(), &recordingPusher{})
		require.NoError(t, err)
		b, err := build().Push(context.Background(), &recordingPusher{})
		require.NoError(t, err)
		assert.Equal(t, a.Digest, b.Digest)
	})

	t.Run("repeated calls on one builder are stable", func(t *testing.T) {
		b := NewManifestBuilder(ref).Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte("demo")})
		first, err := b.Manifest()
		require.NoError(t, err)
		second, err := b.Manifest()
		require.NoError(t, err)
		assert.Equal(t, first, second)
	})
}

func TestManifestBuilder_AnnotationTransformer(t *testing.T) {
	annotations := map[string]string{
		AnnotationTitle:       "demo",
		AnnotationDescription: "drop me",
	}
	build := func() *ManifestBuilder {
		return NewManifestBuilder(solutionRef("demo", "1.0.0")).
			Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte("demo")}).
			Annotations(annotations)
	}

	manifest, err := build().Manifest()
	require.NoError(t, err)
	assert.Equal(t, "drop me", manifest.Annotations[AnnotationDescription])

	filtered, err := build().AnnotationTransformer(DropAnnotations(AnnotationDescription)).Manifest()
	require.NoError(t, err)
	assert.NotContains(t, filtered.Annotations, AnnotationDescription)
	assert.Equal(t, "demo", filtered.Annotations[AnnotationTitle])

	injected, err := build().AnnotationTransformer(func(m map[string]string) map[string]string {
		m[AnnotationSourceCanonical] = "example.com/demo"
		m[AnnotationOrigin] = "example.com"
		return m
	}).Manifest()
	require.NoError(t, err)
	assert.NotContains(t, injected.Annotations, AnnotationSourceCanonical,
		"provenance is stripped after the transformer runs")
	assert.NotContains(t, injected.Annotations, AnnotationOrigin)

	reset, err := build().
		AnnotationTransformer(dropAllAnnotations).
		AnnotationTransformer(nil).
		Manifest()
	require.NoError(t, err)
	assert.Equal(t, "drop me", reset.Annotations[AnnotationDescription])

	annotations[AnnotationTitle] = "changed"
	assert.Equal(t, "demo", reset.Annotations[AnnotationTitle])
}

func TestManifestBuilder_Push(t *testing.T) {
	content := Layer{MediaType: MediaTypeSolutionContent, Data: []byte("name: demo")}
	ref := solutionRef("demo", "1.0.0")
	newBuilder := func() *ManifestBuilder { return NewManifestBuilder(ref).Layers(content) }
	pushErr := errors.New("boom")

	tests := []struct {
		name      string
		errFor    func(ocispec.Descriptor) error
		builder   *ManifestBuilder
		wantErr   string
		wantOrder []string
	}{
		{
			name:      "pushes layers then config then manifest",
			builder:   newBuilder(),
			wantOrder: []string{MediaTypeSolutionContent, MediaTypeSolutionConfig, ocispec.MediaTypeImageManifest},
		},
		{
			name: "already exists is not an error",
			errFor: func(d ocispec.Descriptor) error {
				if d.MediaType == MediaTypeSolutionContent {
					return errdef.ErrAlreadyExists
				}
				return nil
			},
			builder:   newBuilder(),
			wantOrder: []string{MediaTypeSolutionConfig, ocispec.MediaTypeImageManifest},
		},
		{
			name:    "invalid chain",
			builder: NewManifestBuilder(ref),
			wantErr: "no layers",
		},
		{
			name: "layer push fails",
			errFor: func(d ocispec.Descriptor) error {
				if d.MediaType == MediaTypeSolutionContent {
					return pushErr
				}
				return nil
			},
			builder: newBuilder(),
			wantErr: "pushing layer 0",
		},
		{
			name: "config push fails",
			errFor: func(d ocispec.Descriptor) error {
				if d.MediaType == MediaTypeSolutionConfig {
					return pushErr
				}
				return nil
			},
			builder: newBuilder(),
			wantErr: "pushing config",
		},
		{
			name: "manifest push fails",
			errFor: func(d ocispec.Descriptor) error {
				if d.MediaType == ocispec.MediaTypeImageManifest {
					return pushErr
				}
				return nil
			},
			builder: newBuilder(),
			wantErr: "pushing manifest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &recordingPusher{errFor: tt.errFor}
			desc, err := tt.builder.Push(context.Background(), p)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			order := make([]string, len(p.pushed))
			for i, d := range p.pushed {
				order[i] = d.MediaType
			}
			assert.Equal(t, tt.wantOrder, order)

			want, err := tt.builder.Manifest()
			require.NoError(t, err)

			assert.Equal(t, ocispec.MediaTypeImageManifest, desc.MediaType)
			raw, ok := p.blobs[desc.Digest]
			require.True(t, ok)
			var m ocispec.Manifest
			require.NoError(t, json.Unmarshal(raw, &m))
			assert.Equal(t, content.Descriptor(), m.Layers[0])
			assert.Equal(t, want.Config.Digest, m.Config.Digest)
		})
	}
}

func TestBlobExists(t *testing.T) {
	ctx := context.Background()
	desc := ocispec.Descriptor{Digest: digest.FromString("x"), Size: 1}

	t.Run("pusher without existence checks reports absent", func(t *testing.T) {
		exists, err := blobExists(ctx, &recordingPusher{}, desc)
		require.NoError(t, err)
		assert.False(t, exists, "falling back to a transfer is the safe answer")
	})

	t.Run("checker answer is returned", func(t *testing.T) {
		p := &dedupPusher{existing: map[digest.Digest]bool{desc.Digest: true}}
		exists, err := blobExists(ctx, p, desc)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("checker failure is wrapped", func(t *testing.T) {
		boom := errors.New("boom")
		p := &dedupPusher{existsErr: boom}
		_, err := blobExists(ctx, p, desc)
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "checking for blob")
	})
}

func TestManifestBuilder_LayerRef(t *testing.T) {
	ctx := context.Background()
	payload := []byte("payload")
	layerDesc := ocispec.Descriptor{
		MediaType:   MediaTypeSolutionContent,
		Digest:      digest.FromBytes(payload),
		Size:        int64(len(payload)),
		Annotations: map[string]string{ocispec.AnnotationTitle: "content.yaml"},
	}
	build := func(open func(context.Context) (io.ReadCloser, error)) *ManifestBuilder {
		return NewManifestBuilder(solutionRef("demo", "1.0.0")).
			LayerRef(layerDesc, open)
	}

	t.Run("preserves the source descriptor verbatim", func(t *testing.T) {
		manifest, err := build(nil).Manifest()
		require.NoError(t, err)
		require.Len(t, manifest.Layers, 1)
		assert.Equal(t, layerDesc, manifest.Layers[0],
			"layer annotations must survive a referenced layer")
	})

	t.Run("skips the transfer when the destination has the blob", func(t *testing.T) {
		p := &dedupPusher{existing: map[digest.Digest]bool{layerDesc.Digest: true}}
		open := func(context.Context) (io.ReadCloser, error) {
			t.Fatal("open must not be called for a blob the destination already stores")
			return nil, nil
		}

		desc, err := build(open).Push(ctx, p)
		require.NoError(t, err)

		assert.Equal(t, []digest.Digest{layerDesc.Digest}, p.existsCalls)
		for _, pushed := range p.pushed {
			assert.NotEqual(t, layerDesc.Digest, pushed.Digest, "the layer must not be transferred")
		}
		// The manifest still references the layer it did not transfer.
		var manifest ocispec.Manifest
		require.NoError(t, json.Unmarshal(p.blobs[desc.Digest], &manifest))
		assert.Equal(t, layerDesc.Digest, manifest.Layers[0].Digest)
	})

	t.Run("transfers and closes the reader when the blob is missing", func(t *testing.T) {
		p := &dedupPusher{}
		reader := &countingCloser{Reader: bytes.NewReader(payload)}
		opened := 0
		open := func(context.Context) (io.ReadCloser, error) {
			opened++
			return reader, nil
		}

		_, err := build(open).Push(ctx, p)
		require.NoError(t, err)

		assert.Equal(t, 1, opened)
		assert.Equal(t, 1, reader.closed, "the layer reader must be closed")
		assert.Equal(t, payload, p.blobs[layerDesc.Digest])
	})

	t.Run("already exists on push is not an error", func(t *testing.T) {
		p := &dedupPusher{}
		p.errFor = func(d ocispec.Descriptor) error {
			if d.Digest == layerDesc.Digest {
				return errdef.ErrAlreadyExists
			}
			return nil
		}
		open := func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(payload)), nil
		}

		_, err := build(open).Push(ctx, p)
		require.NoError(t, err)
	})

	t.Run("open failure is attributed to the layer", func(t *testing.T) {
		boom := errors.New("boom")
		open := func(context.Context) (io.ReadCloser, error) { return nil, boom }

		_, err := build(open).Push(ctx, &dedupPusher{})
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "pushing layer 0")
	})

	t.Run("existence check failure is attributed to the layer", func(t *testing.T) {
		boom := errors.New("boom")
		open := func(context.Context) (io.ReadCloser, error) {
			t.Fatal("open must not be called when the existence check fails")
			return nil, nil
		}

		_, err := build(open).Push(ctx, &dedupPusher{existsErr: boom})
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "pushing layer 0")
	})
}
