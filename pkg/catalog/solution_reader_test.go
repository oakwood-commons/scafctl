// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"
)

func TestReadSolutionArtifact(t *testing.T) {
	t.Parallel()

	doc := []byte("apiVersion: scafctl.io/v1\nkind: Solution\n")
	bundleTar := buildTarBlob(t, map[string][]byte{"data/info.txt": []byte("hello")})
	fileContent := []byte("deduplicated")
	dedupManifest := []byte(fmt.Sprintf(`{"version": 2, "root": ".", "files": [{"path": "data/big.bin", "size": %d, "digest": "sha256:abc", "layer": 2}]}`, len(fileContent)))
	lock := []byte(`{"version":1}`)

	tests := []struct {
		name       string
		layers     []Layer
		wantBundle bool
		wantFile   string
		wantData   []byte
	}{
		{
			name:   "document only",
			layers: []Layer{{MediaType: MediaTypeSolutionContent, Data: doc}},
		},
		{
			name: "tar bundle",
			layers: []Layer{
				{MediaType: MediaTypeSolutionContent, Data: doc},
				{MediaType: MediaTypeSolutionBundle, Data: bundleTar},
			},
			wantBundle: true, wantFile: "data/info.txt", wantData: []byte("hello"),
		},
		{
			name: "deduplicated bundle with trailing lock",
			layers: []Layer{
				{MediaType: MediaTypeSolutionContent, Data: doc},
				{MediaType: MediaTypeSolutionBundleManifest, Data: dedupManifest},
				{MediaType: MediaTypeSolutionBundleBlob, Data: fileContent},
				{MediaType: MediaTypeSolutionLock, Data: lock},
			},
			wantBundle: true, wantFile: "data/big.bin", wantData: fileContent,
		},
		{
			name: "tar bundle after lock layer",
			layers: []Layer{
				{MediaType: MediaTypeSolutionContent, Data: doc},
				{MediaType: MediaTypeSolutionLock, Data: lock},
				{MediaType: MediaTypeSolutionBundle, Data: bundleTar},
			},
			wantBundle: true, wantFile: "data/info.txt", wantData: []byte("hello"),
		},
		{
			name: "lock without bundle",
			layers: []Layer{
				{MediaType: MediaTypeSolutionContent, Data: doc},
				{MediaType: MediaTypeSolutionLock, Data: lock},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := memory.New()
			ref := Reference{Kind: ArtifactKindSolution, Name: "demo", Version: semver.MustParse("1.0.0"), Built: true}
			desc, err := NewManifestBuilder(ref).Layers(tt.layers...).Push(ctx, store)
			require.NoError(t, err)

			gotDoc, gotBundle, err := ReadSolutionArtifact(ctx, store, desc)

			require.NoError(t, err)
			assert.Equal(t, doc, gotDoc)
			if !tt.wantBundle {
				assert.Nil(t, gotBundle)
				return
			}
			extracted, err := extractFileFromTar(gotBundle, tt.wantFile)
			require.NoError(t, err)
			assert.Equal(t, tt.wantData, extracted)
		})
	}
}

func TestReadSolutionArtifact_Errors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	store := memory.New()
	notJSON := []byte("not json")
	// A non-manifest media type: the store parses manifests on push.
	notJSONDesc := makeDesc(MediaTypeSolutionContent, notJSON)
	require.NoError(t, store.Push(ctx, notJSONDesc, bytes.NewReader(notJSON)))

	emptyManifest := []byte(`{"schemaVersion":2,"layers":[]}`)
	emptyDesc := makeDesc(ocispec.MediaTypeImageManifest, emptyManifest)
	require.NoError(t, store.Push(ctx, emptyDesc, bytes.NewReader(emptyManifest)))

	missingLayer := []byte(fmt.Sprintf(`{"schemaVersion":2,"layers":[{"mediaType":%q,"digest":%q,"size":3}]}`, MediaTypeSolutionContent, digest.FromString("abc")))
	missingLayerDesc := makeDesc(ocispec.MediaTypeImageManifest, missingLayer)
	require.NoError(t, store.Push(ctx, missingLayerDesc, bytes.NewReader(missingLayer)))

	tests := []struct {
		name    string
		desc    ocispec.Descriptor
		wantErr string
	}{
		{name: "missing manifest", desc: makeDesc(ocispec.MediaTypeImageManifest, []byte("absent")), wantErr: "failed to fetch manifest"},
		{name: "malformed manifest", desc: notJSONDesc, wantErr: "failed to unmarshal manifest"},
		{name: "no layers", desc: emptyDesc, wantErr: "manifest has no content layers"},
		{name: "missing content layer", desc: missingLayerDesc, wantErr: "failed to fetch content"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := ReadSolutionArtifact(ctx, store, tt.desc)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
