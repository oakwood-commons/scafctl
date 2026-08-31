// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindLayerByMediaType(t *testing.T) {
	t.Parallel()

	m := ocispec.Manifest{
		Layers: []ocispec.Descriptor{
			{MediaType: MediaTypeSolutionContent},
			{MediaType: MediaTypeSolutionBundle},
			{MediaType: MediaTypeSolutionLock},
		},
	}

	desc, ok := findLayerByMediaType(m, MediaTypeSolutionLock)
	require.True(t, ok)
	assert.Equal(t, MediaTypeSolutionLock, desc.MediaType)

	_, ok = findLayerByMediaType(m, "application/vnd.does.not.exist")
	assert.False(t, ok)

	_, ok = findLayerByMediaType(ocispec.Manifest{}, MediaTypeSolutionLock)
	assert.False(t, ok)
}

func TestStore_AppendsLockLayer(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("lock-store", "1.0.0")
	ref.Kind = ArtifactKindSolution

	content := []byte("apiVersion: scafctl.io/v1\nkind: Solution\n")
	lockData := []byte(`{"version":1,"plugins":[{"name":"p","kind":"provider"}]}`)

	info, err := cat.Store(ctx, ref, content, nil, nil, false,
		Layer{MediaType: MediaTypeSolutionLock, Data: lockData})
	require.NoError(t, err)

	manifest, err := cat.fetchManifestByDigest(ctx, info.Digest)
	require.NoError(t, err)

	// Layer 0 is always content; the lock is appended after it.
	require.Len(t, manifest.Layers, 2)
	assert.Equal(t, MediaTypeForKind(ref.Kind), manifest.Layers[0].MediaType)

	lockDesc, ok := findLayerByMediaType(manifest, MediaTypeSolutionLock)
	require.True(t, ok, "lock layer must be present")

	got, err := cat.fetchBlob(ctx, lockDesc)
	require.NoError(t, err)
	assert.Equal(t, lockData, got)
}

func TestStore_OmitsEmptyLockLayer(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("lock-empty", "1.0.0")
	ref.Kind = ArtifactKindSolution

	content := []byte("apiVersion: scafctl.io/v1\nkind: Solution\n")

	info, err := cat.Store(ctx, ref, content, nil, nil, false,
		Layer{MediaType: MediaTypeSolutionLock, Data: nil})
	require.NoError(t, err)

	manifest, err := cat.fetchManifestByDigest(ctx, info.Digest)
	require.NoError(t, err)

	// Only the content layer should exist; the empty lock is skipped.
	require.Len(t, manifest.Layers, 1)
	_, ok := findLayerByMediaType(manifest, MediaTypeSolutionLock)
	assert.False(t, ok, "empty lock layer must not be stored")
}

func TestStore_LockLayerAfterBundle(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("lock-after-bundle", "1.0.0")
	ref.Kind = ArtifactKindSolution

	content := []byte("solution")
	bundleData := []byte("bundle-tar-bytes")
	lockData := []byte(`{"version":1}`)

	info, err := cat.Store(ctx, ref, content, bundleData, nil, false,
		Layer{MediaType: MediaTypeSolutionLock, Data: lockData})
	require.NoError(t, err)

	manifest, err := cat.fetchManifestByDigest(ctx, info.Digest)
	require.NoError(t, err)

	// content, bundle, lock — lock is last, bundle keeps its layer[1] slot.
	require.Len(t, manifest.Layers, 3)
	assert.Equal(t, MediaTypeSolutionBundle, manifest.Layers[1].MediaType)
	assert.Equal(t, MediaTypeSolutionLock, manifest.Layers[len(manifest.Layers)-1].MediaType)

	// The bundle is still reachable via the media-type switch in FetchWithBundle.
	gotContent, gotBundle, _, err := cat.FetchWithBundle(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, content, gotContent)
	assert.Equal(t, bundleData, gotBundle)
}

func TestStoreDedup_AppendsLockLayerLast(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := context.Background()

	ref := testRef("lock-dedup", "1.0.0")
	ref.Kind = ArtifactKindSolution

	solutionYAML := []byte("apiVersion: scafctl.io/v1\nkind: Solution\n")

	// A small-files tar at layer 2, referenced by the bundle manifest.
	var smallTarBuf bytes.Buffer
	tw := tar.NewWriter(&smallTarBuf)
	fileContent := []byte("hello from bundle")
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: "data/info.txt",
		Size: int64(len(fileContent)),
		Mode: 0o644,
	}))
	_, err := tw.Write(fileContent)
	require.NoError(t, err)
	require.NoError(t, tw.Close())

	manifestJSON := []byte(fmt.Sprintf(`{
		"version": 2,
		"root": ".",
		"files": [
			{"path": "data/info.txt", "size": %d, "digest": "sha256:abc", "layer": 2}
		]
	}`, len(fileContent)))

	lockData := []byte(`{"version":1,"plugins":[{"name":"p","kind":"provider"}]}`)

	info, err := cat.StoreDedup(ctx, ref, solutionYAML, manifestJSON, smallTarBuf.Bytes(), nil, nil, false,
		Layer{MediaType: MediaTypeSolutionLock, Data: lockData})
	require.NoError(t, err)

	manifest, err := cat.fetchManifestByDigest(ctx, info.Digest)
	require.NoError(t, err)

	// Layers: 0=content, 1=bundle manifest, 2=small tar, 3=lock (appended last).
	require.Len(t, manifest.Layers, 4)
	assert.Equal(t, MediaTypeSolutionBundleManifest, manifest.Layers[1].MediaType)
	assert.Equal(t, MediaTypeSolutionBundleSmallTar, manifest.Layers[2].MediaType)
	assert.Equal(t, MediaTypeSolutionLock, manifest.Layers[3].MediaType)

	// The dedup file blob indices (f.Layer == 2) remain valid: reassembly works.
	_, bundleData, _, err := cat.FetchWithBundle(ctx, ref)
	require.NoError(t, err)
	extracted, err := extractFileFromTar(bundleData, "data/info.txt")
	require.NoError(t, err)
	assert.Equal(t, fileContent, extracted)

	// And the lock is retrievable by media type.
	lockDesc, ok := findLayerByMediaType(manifest, MediaTypeSolutionLock)
	require.True(t, ok)
	got, err := cat.fetchBlob(ctx, lockDesc)
	require.NoError(t, err)
	assert.Equal(t, lockData, got)
}
