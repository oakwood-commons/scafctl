// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAuxLayerMediaType(t *testing.T) {
	t.Parallel()

	// Allowed auxiliary layers.
	assert.True(t, isAuxLayerMediaType(MediaTypeSolutionLock))
	assert.True(t, isAuxLayerMediaType(MediaTypeSolutionBundle))

	// Content and other non-aux layers are deliberately rejected — those are
	// served by Fetch / FetchWithBundle, not FetchWithLayer.
	rejected := []string{
		MediaTypeSolutionContent,
		MediaTypeProviderBinary,
		MediaTypeAuthHandlerBinary,
		MediaTypeSolutionBundleManifest,
		MediaTypeSolutionBundleBlob,
		MediaTypeSolutionBundleSmallTar,
		MediaTypeSolutionConfig,
		"application/vnd.does.not.exist",
		"",
	}
	for _, mt := range rejected {
		assert.Falsef(t, isAuxLayerMediaType(mt), "media type %q must not be an aux layer", mt)
	}
}

func TestLocalCatalog_FetchWithLayer_RoundTrip(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := t.Context()

	ref := testRef("fwl-roundtrip", "1.0.0")
	ref.Kind = ArtifactKindSolution

	content := []byte("apiVersion: scafctl.io/v1\nkind: Solution\n")
	lockData := []byte(`{"version":1,"plugins":[{"name":"p","kind":"provider"}]}`)

	_, err := cat.Store(ctx, ref, content, nil, nil, false,
		Layer{MediaType: MediaTypeSolutionLock, Data: lockData})
	require.NoError(t, err)

	gotContent, gotLayers, info, err := cat.FetchWithLayer(ctx, ref, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, content, gotContent)
	assert.Equal(t, lockData, gotLayers[MediaTypeSolutionLock])
	assert.Equal(t, "fwl-roundtrip", info.Reference.Name)
	assert.NotEmpty(t, info.Digest)
}

func TestLocalCatalog_FetchWithLayer_RoundTripWithBundle(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := t.Context()

	ref := testRef("fwl-bundle", "1.0.0")
	ref.Kind = ArtifactKindSolution

	content := []byte("solution")
	bundleData := []byte("bundle-tar-bytes")
	lockData := []byte(`{"version":1}`)

	_, err := cat.Store(ctx, ref, content, bundleData, nil, false,
		Layer{MediaType: MediaTypeSolutionLock, Data: lockData})
	require.NoError(t, err)

	// The lock is located by media type even though the bundle occupies
	// layer[1] — a fixed-index reader would break here.
	gotContent, gotLayers, _, err := cat.FetchWithLayer(ctx, ref, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, content, gotContent)
	assert.Equal(t, lockData, gotLayers[MediaTypeSolutionLock])
}

func TestLocalCatalog_FetchWithLayer_AbsentLayer(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := t.Context()

	ref := testRef("fwl-absent", "1.0.0")
	ref.Kind = ArtifactKindSolution

	content := []byte("solution-without-lock")

	// Store with no aux layer.
	_, err := cat.Store(ctx, ref, content, nil, nil, false)
	require.NoError(t, err)

	// Content still comes back; the absent aux layer is omitted from the map.
	gotContent, gotLayers, _, err := cat.FetchWithLayer(ctx, ref, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, content, gotContent)
	assert.Nil(t, gotLayers[MediaTypeSolutionLock])
}

func TestLocalCatalog_FetchWithLayer_RejectsNonAuxMediaType(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := t.Context()

	ref := testRef("fwl-reject", "1.0.0")
	ref.Kind = ArtifactKindSolution

	_, err := cat.Store(ctx, ref, []byte("solution"), nil, nil, false)
	require.NoError(t, err)

	// A completely unknown media type must be rejected before any fetch happens.
	_, _, _, err = cat.FetchWithLayer(ctx, ref, "application/vnd.unknown.type") //nolint:dogsled // testing error path only
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a fetchable auxiliary layer")
}

func TestLocalCatalog_FetchWithLayer_NotFound(t *testing.T) {
	t.Parallel()

	cat := newTestCatalog(t)
	ctx := t.Context()

	ref := testRef("fwl-missing", "9.9.9")
	ref.Kind = ArtifactKindSolution

	_, _, _, err := cat.FetchWithLayer(ctx, ref, MediaTypeSolutionLock) //nolint:dogsled // testing error path only
	require.Error(t, err)
	assert.True(t, IsNotFound(err), "expected not-found, got %v", err)
}

func TestRemoteCatalog_FetchWithLayer_RoundTrip(t *testing.T) {
	t.Parallel()

	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("fwl-remote", "1.0.0")
	ref.Kind = ArtifactKindSolution

	content := []byte("name: fwl-remote\n")
	lockData := []byte(`{"version":1,"plugins":[]}`)

	_, err := cat.Store(ctx, ref, content, nil, nil, false,
		Layer{MediaType: MediaTypeSolutionLock, Data: lockData})
	require.NoError(t, err)

	gotContent, gotLayers, info, err := cat.FetchWithLayer(ctx, ref, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, content, gotContent)
	assert.Equal(t, lockData, gotLayers[MediaTypeSolutionLock])
	assert.NotEmpty(t, info.Digest)
}

func TestRemoteCatalog_FetchWithLayer_AbsentLayer(t *testing.T) {
	t.Parallel()

	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("fwl-remote-absent", "1.0.0")
	ref.Kind = ArtifactKindSolution

	content := []byte("name: fwl-remote-absent\n")

	_, err := cat.Store(ctx, ref, content, nil, nil, false)
	require.NoError(t, err)

	gotContent, gotLayers, _, err := cat.FetchWithLayer(ctx, ref, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, content, gotContent)
	assert.Nil(t, gotLayers[MediaTypeSolutionLock])
}

func TestRemoteCatalog_FetchWithLayer_RejectsNonAuxMediaType(t *testing.T) {
	t.Parallel()

	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("fwl-remote-reject", "1.0.0")

	_, _, _, err := cat.FetchWithLayer(ctx, ref, MediaTypeSolutionContent) //nolint:dogsled // testing error path only
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a fetchable auxiliary layer")
}
