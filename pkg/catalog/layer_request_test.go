// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"fmt"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"
)

const (
	testSolutionConfigJSON = `{"kind":"solution","name":"demo","version":"1.0.0"}`
	testPlatformAMD64      = "linux/amd64"
)

// layerManifest builds a manifest chain carrying the given layers.
func layerManifest(layers ...Layer) *ManifestBuilder {
	return NewManifestBuilder(solutionRef("demo", "1.0.0")).
		Layers(layers...)
}

// layerIndex starts an index chain under the same identity as layerManifest.
func layerIndex() *IndexBuilder {
	return NewIndexBuilder(solutionRef("demo", "1.0.0"))
}

// pushToMemory pushes the artifact into a fresh in-memory store.
func pushToMemory(t *testing.T, builder ArtifactBuilder) (*memory.Store, ocispec.Descriptor) {
	t.Helper()

	store := memory.New()
	desc, err := builder.Push(t.Context(), store)
	require.NoError(t, err)
	return store, desc
}

func binaryLayer(data string) Layer {
	return Layer{MediaType: MediaTypeProviderBinary, Data: []byte(data)}
}

func lockLayer(data string) Layer {
	return Layer{MediaType: MediaTypeSolutionLock, Data: []byte(data)}
}

func contentLayer(data string) Layer {
	return Layer{MediaType: MediaTypeSolutionContent, Data: []byte(data)}
}

func bundleLayer() Layer {
	return Layer{MediaType: MediaTypeSolutionBundle, Data: []byte("tar bytes")}
}

// unreadableSolution builds a selection whose layers have no content source,
// standing in for blobs that fail when opened.
func unreadableSolution(mediaTypes ...string) SolutionLayers {
	layers := make([]FetchedLayer, len(mediaTypes))
	for i, mediaType := range mediaTypes {
		layers[i] = FetchedLayer{Descriptor: ocispec.Descriptor{
			MediaType: mediaType,
			Digest:    digest.FromString(mediaType),
		}}
	}
	return SolutionLayers{layers: fetched(layers...)}
}

// fetched builds a FetchedLayers without a manifest, for helper tests that only
// exercise the selected layers.
func fetched(layers ...FetchedLayer) FetchedLayers {
	return FetchedLayers{selectedLayers: layers}
}

func TestFetchedLayers_Helpers(t *testing.T) {
	layers := fetched(
		FetchedLayer{Descriptor: ocispec.Descriptor{MediaType: MediaTypeProviderBinary}},
		FetchedLayer{Descriptor: ocispec.Descriptor{MediaType: MediaTypeSolutionLock}},
		FetchedLayer{Descriptor: ocispec.Descriptor{MediaType: MediaTypeProviderBinary, Digest: digest.FromString("other")}},
	)

	t.Run("ByMediaType", func(t *testing.T) {
		grouped := layers.ByMediaType()
		assert.Len(t, grouped, 2)
		assert.Len(t, grouped[MediaTypeProviderBinary], 2)
		assert.Len(t, grouped[MediaTypeSolutionLock], 1)
	})

	t.Run("Manifest keeps the unselected layers", func(t *testing.T) {
		manifest := ocispec.Manifest{Layers: []ocispec.Descriptor{
			{MediaType: MediaTypeSolutionContent},
			{MediaType: MediaTypeProviderBinary},
		}}
		withManifest := FetchedLayers{manifest: manifest, selectedLayers: layers.All()}

		assert.Equal(t, manifest, withManifest.Manifest())
	})
}

func TestFetchLayers(t *testing.T) {
	ctx := t.Context()

	t.Run("manifest request yields layers", func(t *testing.T) {
		store, root := pushToMemory(t, layerManifest(contentLayer("name: demo")))

		layers, err := FetchLayers(ctx, store, root, NewManifestLayers(MediaTypeSolutionContent))
		require.NoError(t, err)
		assert.Equal(t, 1, layers.Len())
	})

	t.Run("nil request", func(t *testing.T) {
		_, err := FetchLayers(ctx, memory.New(), ocispec.Descriptor{}, LayerRequest[FetchedLayers](nil))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nil request")
	})

	t.Run("request failure is surfaced", func(t *testing.T) {
		_, err := FetchLayers(ctx, memory.New(), ocispec.Descriptor{}, NewManifestLayers())
		require.Error(t, err)
	})
}

func TestFetchedLayer_OpenAndBytes(t *testing.T) {
	ctx := t.Context()
	data := []byte("layer payload")
	desc := ocispec.Descriptor{
		MediaType: MediaTypeSolutionContent,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}

	t.Run("no content source", func(t *testing.T) {
		_, err := FetchedLayer{Descriptor: desc}.Open(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no content source")

		_, err = FetchedLayer{Descriptor: desc}.Bytes(ctx)
		require.Error(t, err)
	})

	t.Run("open streams content", func(t *testing.T) {
		layer := FetchedLayer{Descriptor: desc, open: openLayer(fetcherReturning(data), desc, nil)}

		rc, err := layer.Open(ctx)
		require.NoError(t, err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("bytes verifies content", func(t *testing.T) {
		layer := FetchedLayer{Descriptor: desc, open: openLayer(fetcherReturning(data), desc, nil)}

		got, err := layer.Bytes(ctx)
		require.NoError(t, err)
		assert.Equal(t, data, got)
	})

	t.Run("bytes rejects tampered content", func(t *testing.T) {
		tampered := []byte("LAYER PAYLOAD") // same length, different digest
		layer := FetchedLayer{Descriptor: desc, open: openLayer(fetcherReturning(tampered), desc, nil)}

		_, err := layer.Bytes(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "verifying layer content")
	})

	t.Run("bytes surfaces fetch failure", func(t *testing.T) {
		fetcher := newBlobFetcher() // holds nothing, so every fetch is not-found
		layer := FetchedLayer{Descriptor: desc, open: openLayer(fetcher, desc, nil)}

		_, err := layer.Bytes(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "fetching layer")
	})

	t.Run("open enforces the size bound before fetching", func(t *testing.T) {
		original := maxLayerSize
		maxLayerSize = 4
		defer func() { maxLayerSize = original }()

		fetcher := newBlobFetcher()
		layer := FetchedLayer{Descriptor: desc, open: openLayer(fetcher, desc, nil)}

		_, err := layer.Open(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
		assert.Empty(t, fetcher.fetched, "an oversized layer must not be requested")
	})
}

func TestManifestLayers_Fetch(t *testing.T) {
	ctx := t.Context()

	store, root := pushToMemory(t, layerManifest(
		contentLayer("name: demo"),
		lockLayer("lock-data"),
		Layer{MediaType: MediaTypeSolutionBundle, Data: []byte("bundle-data")},
	))

	tests := []struct {
		name       string
		mediaTypes []string
		want       []string // media types in expected order
		wantErr    string
	}{
		{
			name:       "single media type",
			mediaTypes: []string{MediaTypeSolutionContent},
			want:       []string{MediaTypeSolutionContent},
		},
		{
			name:       "several media types follow manifest order, not request order",
			mediaTypes: []string{MediaTypeSolutionBundle, MediaTypeSolutionContent},
			want:       []string{MediaTypeSolutionContent, MediaTypeSolutionBundle},
		},
		{
			name:       "duplicate media types do not duplicate layers",
			mediaTypes: []string{MediaTypeSolutionLock, MediaTypeSolutionLock},
			want:       []string{MediaTypeSolutionLock},
		},
		{
			name:    "no media types",
			wantErr: "at least one media type",
		},
		{
			name:       "empty media type",
			mediaTypes: []string{""},
			wantErr:    "must not be empty",
		},
		{
			name:       "media type absent from the manifest",
			mediaTypes: []string{MediaTypeSolutionContent, MediaTypeProviderBinary},
			wantErr:    `no layer with media type "` + MediaTypeProviderBinary + `"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layers, err := NewManifestLayers(tt.mediaTypes...).Fetch(ctx, store, root)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			require.NoError(t, err)
			got := make([]string, layers.Len())
			for i, layer := range layers.All() {
				got[i] = layer.Descriptor.MediaType
			}
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("optional media type does not fail when absent", func(t *testing.T) {
		layers, err := NewManifestLayers(MediaTypeSolutionContent).
			Optional(MediaTypeProviderBinary).
			Fetch(ctx, store, root)
		require.NoError(t, err)
		require.Equal(t, 1, layers.Len())
		assert.Equal(t, MediaTypeSolutionContent, layers.All()[0].Descriptor.MediaType)
	})

	t.Run("optional media type included when present", func(t *testing.T) {
		layers, err := NewManifestLayers(MediaTypeSolutionContent).
			Optional(MediaTypeSolutionBundle).
			Fetch(ctx, store, root)
		require.NoError(t, err)
		require.Equal(t, 2, layers.Len())
		assert.Equal(t, MediaTypeSolutionContent, layers.All()[0].Descriptor.MediaType)
		assert.Equal(t, MediaTypeSolutionBundle, layers.All()[1].Descriptor.MediaType)
	})

	t.Run("only optional media types when absent returns empty slice", func(t *testing.T) {
		layers, err := NewManifestLayers().
			Optional(MediaTypeProviderBinary).
			Fetch(ctx, store, root)
		require.NoError(t, err)
		assert.Zero(t, layers.Len())
	})

	t.Run("content is read lazily and verified", func(t *testing.T) {
		fetcher := &blobFetcher{blobs: map[digest.Digest][]byte{}}
		contentDesc := fetcher.add(MediaTypeSolutionContent, []byte("name: demo"))
		rootDesc := fetcher.addJSON(t, ocispec.MediaTypeImageManifest, ocispec.Manifest{
			Versioned: specs.Versioned{SchemaVersion: 2},
			MediaType: ocispec.MediaTypeImageManifest,
			Config:    fetcher.add(MediaTypeSolutionConfig, []byte(testSolutionConfigJSON)),
			Layers:    []ocispec.Descriptor{contentDesc},
		})

		layers, err := NewManifestLayers(MediaTypeSolutionContent).Fetch(ctx, fetcher, rootDesc)
		require.NoError(t, err)
		require.Equal(t, 1, layers.Len())
		assert.NotContains(t, fetcher.fetched, contentDesc.Digest, "selection must not transfer layer content")

		got, err := layers.All()[0].Bytes(ctx)
		require.NoError(t, err)
		assert.Equal(t, "name: demo", string(got))
		assert.Contains(t, fetcher.fetched, contentDesc.Digest)
	})

	t.Run("progress is reported when a layer is opened", func(t *testing.T) {
		var seen []ocispec.Descriptor
		layers, err := NewManifestLayers(MediaTypeSolutionContent).
			OnProgress(func(d ocispec.Descriptor) { seen = append(seen, d) }).
			Fetch(ctx, store, root)
		require.NoError(t, err)
		assert.Empty(t, seen, "no progress before any layer is opened")

		_, err = layers.All()[0].Bytes(ctx)
		require.NoError(t, err)
		require.Len(t, seen, 1)
		assert.Equal(t, layers.All()[0].Descriptor.Digest, seen[0].Digest)
	})

	t.Run("an image index is rejected", func(t *testing.T) {
		indexStore, indexRoot := pushToMemory(t, layerIndex().
			Platform(testPlatformAMD64, layerManifest(binaryLayer("bin"))))

		_, err := NewManifestLayers(MediaTypeProviderBinary).Fetch(ctx, indexStore, indexRoot)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `expected "`+ocispec.MediaTypeImageManifest+`"`)
	})

	t.Run("missing manifest blob", func(t *testing.T) {
		_, err := NewManifestLayers(MediaTypeSolutionContent).Fetch(ctx, memory.New(), root)
		require.Error(t, err)
	})
}

func TestFetchedLayers_Find(t *testing.T) {
	t.Parallel()

	firstBinary := ocispec.Descriptor{MediaType: MediaTypeProviderBinary, Digest: digest.FromString("first")}
	secondBinary := ocispec.Descriptor{MediaType: MediaTypeProviderBinary, Digest: digest.FromString("second")}
	layers := fetched(
		FetchedLayer{Descriptor: firstBinary},
		FetchedLayer{Descriptor: ocispec.Descriptor{MediaType: MediaTypeSolutionLock}},
		FetchedLayer{Descriptor: secondBinary},
	)

	t.Run("returns the matching layer", func(t *testing.T) {
		t.Parallel()

		layer, ok := layers.Find(MediaTypeSolutionLock)
		require.True(t, ok)
		assert.Equal(t, MediaTypeSolutionLock, layer.Descriptor.MediaType)
	})

	t.Run("reports an unselected media type", func(t *testing.T) {
		t.Parallel()

		_, ok := layers.Find(MediaTypeSolutionBundle)
		assert.False(t, ok)
	})

	// A manifest may repeat a media type even though scafctl's own artifacts do
	// not, so the first match is the documented answer rather than an error.
	t.Run("returns the first of a repeated media type", func(t *testing.T) {
		t.Parallel()

		layer, ok := layers.Find(MediaTypeProviderBinary)
		require.True(t, ok)
		assert.Equal(t, firstBinary.Digest, layer.Descriptor.Digest)
	})

	t.Run("zero value finds nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := FetchedLayers{}.Find(MediaTypeSolutionContent)
		assert.False(t, ok)
	})
}

func TestFetchedLayers_Handle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	store, root := pushToMemory(t, layerManifest(
		contentLayer("solution: demo"),
		binaryLayer("binary bytes"),
	))

	// Select only the solution content, leaving the binary layer unselected.
	layers, err := FetchLayers(ctx, store, root, NewManifestLayers(MediaTypeSolutionContent))
	require.NoError(t, err)
	require.Equal(t, 1, layers.Len())

	_, selected := layers.Find(MediaTypeProviderBinary)
	require.False(t, selected)

	unselected, ok := findLayerByMediaType(layers.Manifest(), MediaTypeProviderBinary)
	require.True(t, ok)

	t.Run("opens a layer the request did not select", func(t *testing.T) {
		t.Parallel()
		layer, err := layers.handle(unselected)
		require.NoError(t, err)

		data, err := layer.Bytes(ctx)
		require.NoError(t, err)
		assert.Equal(t, "binary bytes", string(data))
	})

	// The lookup is what keeps a FetchedLayers from reaching content outside
	// the manifest it was selected from.
	t.Run("rejects a descriptor from outside the manifest", func(t *testing.T) {
		t.Parallel()
		foreign := unselected
		foreign.Digest = digest.FromString("some other artifact's blob")

		_, err := layers.handle(foreign)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in the manifest")
	})

	t.Run("zero value reaches nothing", func(t *testing.T) {
		t.Parallel()
		_, err := FetchedLayers{}.handle(unselected)
		require.Error(t, err)
	})
}

func TestFetchSolutionLayers(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("selects the content and every present optional part", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(
			contentLayer("solution: demo"),
			bundleLayer(),
			lockLayer("lock bytes"),
		))

		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)
		assert.Equal(t, 3, sol.Layers().Len())
	})

	t.Run("tolerates an artifact with no optional parts", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(contentLayer("solution: demo")))

		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)
		assert.Equal(t, 1, sol.Layers().Len())
	})

	t.Run("requires the solution content layer", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(lockLayer("lock bytes")))

		_, err := FetchSolutionLayers(ctx, store, root)
		require.Error(t, err)
		assert.Contains(t, err.Error(), MediaTypeSolutionContent)
	})
}

func TestSolutionLayers_Content(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("returns the solution file", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(contentLayer("solution: demo")))
		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)

		data, err := sol.Content(ctx)
		require.NoError(t, err)
		assert.Equal(t, "solution: demo", string(data))
	})

	// FetchSolutionLayers requires the content layer, so this is only reachable
	// through a zero value -- but absence must stay an error, not an empty read.
	t.Run("errors without a content layer", func(t *testing.T) {
		t.Parallel()

		_, err := SolutionLayers{}.Content(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), MediaTypeSolutionContent)
	})

	t.Run("wraps a read failure", func(t *testing.T) {
		t.Parallel()

		_, err := unreadableSolution(MediaTypeSolutionContent).Content(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading solution content")
	})
}

func TestSolutionLayers_Lock(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("returns the lock when present", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(
			contentLayer("solution: demo"),
			lockLayer("lock bytes"),
		))
		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)

		data, ok, err := sol.Lock(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "lock bytes", string(data))
	})

	t.Run("reports absence without erroring", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(contentLayer("solution: demo")))
		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)

		data, ok, err := sol.Lock(ctx)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Nil(t, data)
	})

	t.Run("wraps a read failure", func(t *testing.T) {
		t.Parallel()

		_, _, err := unreadableSolution(MediaTypeSolutionLock).Lock(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading solution lock")
	})
}

func TestSolutionLayers_Bundle(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	t.Run("returns a v1 bundle verbatim", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(
			contentLayer("solution: demo"),
			bundleLayer(),
		))
		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)

		data, ok, err := sol.Bundle(ctx)
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, "tar bytes", string(data))
	})

	t.Run("reassembles a v2 deduplicated bundle", func(t *testing.T) {
		t.Parallel()

		const fileContent = "hello world"
		// The file index addresses blobs by absolute manifest position, so the
		// blob must stay at layer 2 behind the content and manifest layers.
		bundleManifestJSON := fmt.Sprintf(`{
			"version": 2,
			"root": ".",
			"files": [
				{"path": "data/readme.txt", "size": %d, "digest": "sha256:abc", "layer": 2}
			]
		}`, len(fileContent))

		store, root := pushToMemory(t, layerManifest(
			contentLayer("solution: demo"),
			Layer{MediaType: MediaTypeSolutionBundleManifest, Data: []byte(bundleManifestJSON)},
			Layer{MediaType: MediaTypeSolutionBundleBlob, Data: []byte(fileContent)},
		))
		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)

		data, ok, err := sol.Bundle(ctx)
		require.NoError(t, err)
		require.True(t, ok)

		extracted, err := extractFileFromTar(data, "data/readme.txt")
		require.NoError(t, err)
		assert.Equal(t, fileContent, string(extracted))

		embedded, err := extractFileFromTar(data, ".scafctl/bundle-manifest.json")
		require.NoError(t, err)
		assert.Contains(t, string(embedded), `"version": 1`)
	})

	t.Run("surfaces a malformed v2 bundle manifest", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(
			contentLayer("solution: demo"),
			Layer{MediaType: MediaTypeSolutionBundleManifest, Data: []byte("not json")},
		))
		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)

		_, _, err = sol.Bundle(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reassembling deduplicated bundle")
	})

	t.Run("reports absence without erroring", func(t *testing.T) {
		t.Parallel()

		store, root := pushToMemory(t, layerManifest(contentLayer("solution: demo")))
		sol, err := FetchSolutionLayers(ctx, store, root)
		require.NoError(t, err)

		data, ok, err := sol.Bundle(ctx)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Nil(t, data)
	})

	t.Run("wraps a v1 read failure", func(t *testing.T) {
		t.Parallel()

		_, _, err := unreadableSolution(MediaTypeSolutionBundle).Bundle(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reading solution bundle")
	})

	// Reassembly reaches layers by position, so it asks for descriptors the
	// request never selected. A descriptor with no handle must surface as an
	// error from the lookup rather than a fetch against arbitrary content.
	t.Run("rejects a layer the selection has no handle for", func(t *testing.T) {
		t.Parallel()

		desc := makeDesc(MediaTypeSolutionBundleManifest, []byte(`{"version":2}`))
		sol := SolutionLayers{layers: FetchedLayers{
			manifest:       ocispec.Manifest{Layers: []ocispec.Descriptor{desc}},
			selectedLayers: []FetchedLayer{{Descriptor: desc}},
		}}

		_, _, err := sol.Bundle(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not in the manifest")
	})
}
