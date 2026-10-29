// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
)

func TestRemoteCatalog_CanonicalID(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	assert.Equal(t, cat.canonicalID(), cat.CanonicalID())
}

func TestRemoteCatalog_ResolveByTag(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	t.Cleanup(ts.Close) // parallel subtests run after this func returns

	ctx := t.Context()
	ref := testRef("demo", "")
	v1, err := cat.Store(ctx, testRef("demo", "1.0.0"), []byte("v1"), nil, nil, false)
	require.NoError(t, err)
	v2, err := cat.Store(ctx, testRef("demo", "2.0.0"), []byte("v2"), nil, nil, false)
	require.NoError(t, err)

	tests := []struct {
		name       string
		tag        string
		wantDigest string
	}{
		{name: "by version", tag: "1.0.0", wantDigest: v1.Digest},
		{name: "other version", tag: "2.0.0", wantDigest: v2.Digest},
		{name: "by digest", tag: v1.Digest, wantDigest: v1.Digest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			desc, err := cat.resolveByTag(ctx, ref, tt.tag)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDigest, desc.Digest.String())
			assert.NotEmpty(t, desc.MediaType)
			assert.Positive(t, desc.Size)
		})
	}
}

func TestRemoteCatalog_ResolveByTag_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	t.Cleanup(ts.Close) // parallel subtests run after this func returns

	ctx := t.Context()
	_, err := cat.Store(ctx, testRef("demo", "1.0.0"), []byte("v1"), nil, nil, false)
	require.NoError(t, err)

	tests := []struct {
		name string
		ref  Reference
		tag  string
	}{
		{name: "unknown tag", ref: testRef("demo", ""), tag: "9.9.9"},
		{name: "unknown repository", ref: testRef("missing", ""), tag: "1.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := cat.resolveByTag(ctx, tt.ref, tt.tag)
			require.Error(t, err)
			assert.True(t, IsNotFound(err), "got %v", err)
		})
	}
}

func TestRemoteCatalog_CopyToStorage(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("demo", "1.0.0")
	payload := []byte("name: demo\n")
	_, err := cat.Store(ctx, ref, payload, nil, nil, false)
	require.NoError(t, err)

	desc, err := cat.resolveByTag(ctx, ref, "1.0.0")
	require.NoError(t, err)

	const dstTag = "provider/demo:1.0.0"
	store := memory.New()
	require.NoError(t, cat.copyToStorage(ctx, ref, desc, dstTag, store))

	tagged, err := store.Resolve(ctx, dstTag)
	require.NoError(t, err)
	assert.Equal(t, desc.Digest, tagged.Digest)

	// The full graph (manifest + layers) must be present locally.
	successors, err := content.Successors(ctx, store, tagged)
	require.NoError(t, err)
	require.NotEmpty(t, successors)
	for _, s := range successors {
		exists, err := store.Exists(ctx, s)
		require.NoError(t, err)
		assert.True(t, exists, "missing blob %s", s.Digest)
	}
}

// Not parallel: it lowers the package-level maxLayerSize.
func TestRemoteCatalog_CopyToStorage_LayerSizeLimit(t *testing.T) {
	const limit = 2048

	tests := []struct {
		name    string
		payload []byte
		wantErr bool
	}{
		{name: "under limit", payload: []byte("name: demo\n")},
		{name: "over limit", payload: bytes.Repeat([]byte("x"), 4*limit), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, ts := newTestRemoteCatalog(t)
			defer ts.Close()

			ctx := t.Context()
			ref := testRef("demo", "1.0.0")
			_, err := cat.Store(ctx, ref, tt.payload, nil, nil, false)
			require.NoError(t, err)

			original := maxLayerSize
			maxLayerSize = limit
			defer func() { maxLayerSize = original }()

			store := memory.New()
			_, err = cat.pull(ctx, ref, store, "staged")
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrLayerTooLarge)

			_, err = store.Resolve(ctx, "staged")
			assert.Error(t, err, "an oversized artifact must not be staged")
		})
	}
}

func TestRemoteCatalog_CopyToStorage_NoTag(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("demo", "1.0.0")
	_, err := cat.Store(ctx, ref, []byte("x"), nil, nil, false)
	require.NoError(t, err)

	desc, err := cat.resolveByTag(ctx, ref, "1.0.0")
	require.NoError(t, err)

	store := memory.New()
	require.NoError(t, cat.copyToStorage(ctx, ref, desc, "", store))

	// oras.Copy reuses the source tag when the destination ref is empty.
	tagged, err := store.Resolve(ctx, "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, desc.Digest, tagged.Digest)
}

func TestRemoteCatalog_CopyToStorage_TagMoved(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("demo", "1.0.0")
	_, err := cat.Store(ctx, ref, []byte("x"), nil, nil, false)
	require.NoError(t, err)

	// Simulate the tag moving after resolve: desc no longer matches what 1.0.0 points at.
	desc, err := cat.resolveByTag(ctx, ref, "1.0.0")
	require.NoError(t, err)
	desc.Digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

	err = cat.copyToStorage(ctx, ref, desc, "x", memory.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tag moved")
}

func TestRemoteCatalog_CopyToStorage_MissingTag(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	_, err := cat.Store(ctx, testRef("demo", "1.0.0"), []byte("x"), nil, nil, false)
	require.NoError(t, err)

	desc, err := cat.resolveByTag(ctx, testRef("demo", "1.0.0"), "1.0.0")
	require.NoError(t, err)

	err = cat.copyToStorage(ctx, testRef("demo", "9.9.9"), desc, "x", memory.New())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "copying demo:9.9.9")
}

func TestRemoteCatalog_Pull(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	ref := testRef("demo", "1.0.0")
	stored, err := cat.Store(ctx, ref, []byte("name: demo\n"), nil, nil, false)
	require.NoError(t, err)

	const dstTag = "staged"
	store := memory.New()
	desc, err := cat.pull(ctx, ref, store, dstTag)
	require.NoError(t, err)
	assert.Equal(t, stored.Digest, desc.Digest.String())

	tagged, err := store.Resolve(ctx, dstTag)
	require.NoError(t, err)
	assert.Equal(t, desc.Digest, tagged.Digest)
}

func TestRemoteCatalog_Pull_NotFound(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	_, err := cat.Store(ctx, testRef("demo", "1.0.0"), []byte("x"), nil, nil, false)
	require.NoError(t, err)

	store := memory.New()
	_, err = cat.pull(ctx, testRef("demo", "9.9.9"), store, "staged")
	require.Error(t, err)
	assert.True(t, IsNotFound(err), "got %v", err)

	_, err = store.Resolve(ctx, "staged")
	assert.Error(t, err, "nothing should be copied on resolve failure")
}
