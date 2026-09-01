// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allowlistMock is a minimal Catalog implementation for testing the allowlist wrapper.
type allowlistMock struct {
	name string
}

func (m *allowlistMock) Name() string { return m.name }
func (m *allowlistMock) Store(_ context.Context, _ Reference, _, _ []byte, _ map[string]string, _ bool, _ ...Layer) (ArtifactInfo, error) {
	return ArtifactInfo{}, nil
}

func (m *allowlistMock) Fetch(_ context.Context, ref Reference) ([]byte, ArtifactInfo, error) {
	return []byte("data"), ArtifactInfo{Reference: ref}, nil
}

func (m *allowlistMock) FetchWithBundle(_ context.Context, ref Reference) ([]byte, []byte, ArtifactInfo, error) {
	return []byte("data"), []byte("bundle"), ArtifactInfo{Reference: ref}, nil
}

func (m *allowlistMock) FetchWithLayer(_ context.Context, ref Reference, mediaTypes ...string) ([]byte, map[string][]byte, ArtifactInfo, error) {
	layers := make(map[string][]byte, len(mediaTypes))
	for _, mt := range mediaTypes {
		layers[mt] = []byte("layer")
	}
	return []byte("data"), layers, ArtifactInfo{Reference: ref}, nil
}

func (m *allowlistMock) Resolve(_ context.Context, ref Reference) (ArtifactInfo, error) {
	return ArtifactInfo{Reference: ref}, nil
}

func (m *allowlistMock) List(_ context.Context, _ ArtifactKind, _ string) ([]ArtifactInfo, error) {
	return []ArtifactInfo{{Reference: Reference{Name: "listed"}}}, nil
}

func (m *allowlistMock) Exists(_ context.Context, _ Reference) (bool, error) {
	return true, nil
}

func (m *allowlistMock) Delete(_ context.Context, _ Reference) error {
	return nil
}

func TestAllowlistCatalog_NilAllowlistPermitsAll(t *testing.T) {
	inner := &allowlistMock{name: "test-catalog"}
	cat := NewAllowlistCatalog(inner, nil)

	ctx := context.Background()
	ref := Reference{Name: "anything", Kind: ArtifactKindProvider}

	info, err := cat.Resolve(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, "anything", info.Reference.Name)

	data, _, err := cat.Fetch(ctx, ref)
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), data)

	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestAllowlistCatalog_EmptySliceDeniesAll(t *testing.T) {
	inner := &allowlistMock{name: "test-catalog"}
	cat := NewAllowlistCatalog(inner, []string{})

	ctx := context.Background()
	ref := Reference{Name: "anything", Kind: ArtifactKindProvider}

	_, err := cat.Resolve(ctx, ref)
	require.Error(t, err)
	var notFound *ArtifactNotFoundError
	assert.ErrorAs(t, err, &notFound)
}

func TestAllowlistCatalog_AllowedArtifactPassesThrough(t *testing.T) {
	inner := &allowlistMock{name: "official"}
	cat := NewAllowlistCatalog(inner, []string{"exec", "git"})

	ctx := context.Background()

	// Resolve allowed
	info, err := cat.Resolve(ctx, Reference{Name: "exec", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.Equal(t, "exec", info.Reference.Name)

	// Fetch allowed
	data, _, err := cat.Fetch(ctx, Reference{Name: "git", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), data)

	// FetchWithBundle allowed
	content, bundle, _, err := cat.FetchWithBundle(ctx, Reference{Name: "exec", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), content)
	assert.Equal(t, []byte("bundle"), bundle)

	// FetchWithLayer allowed
	layerContent, layers, _, err := cat.FetchWithLayer(ctx, Reference{Name: "exec", Kind: ArtifactKindProvider}, MediaTypeSolutionLock)
	require.NoError(t, err)
	assert.Equal(t, []byte("data"), layerContent)
	assert.Equal(t, []byte("layer"), layers[MediaTypeSolutionLock])

	// Exists allowed
	exists, err := cat.Exists(ctx, Reference{Name: "exec", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestAllowlistCatalog_RejectedArtifactReturnsNotFound(t *testing.T) {
	inner := &allowlistMock{name: "official"}
	cat := NewAllowlistCatalog(inner, []string{"exec", "git"})

	ctx := context.Background()
	ref := Reference{Name: "malicious-plugin", Kind: ArtifactKindProvider}

	// Resolve rejected
	_, err := cat.Resolve(ctx, ref)
	require.Error(t, err)
	assert.True(t, IsNotFound(err))
	assert.Contains(t, err.Error(), "official")

	// Fetch rejected
	_, _, err = cat.Fetch(ctx, ref)
	require.Error(t, err)
	assert.True(t, IsNotFound(err))

	// FetchWithBundle rejected
	_, _, _, err = cat.FetchWithBundle(ctx, ref) //nolint:dogsled // testing error path only
	require.Error(t, err)
	assert.True(t, IsNotFound(err))

	// FetchWithLayer rejected
	_, _, _, err = cat.FetchWithLayer(ctx, ref, MediaTypeSolutionLock) //nolint:dogsled // testing error path only
	require.Error(t, err)
	assert.True(t, IsNotFound(err))

	// Exists rejected
	exists, err := cat.Exists(ctx, ref)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestAllowlistCatalog_ListNotRestricted(t *testing.T) {
	inner := &allowlistMock{name: "official"}
	cat := NewAllowlistCatalog(inner, []string{"exec"})

	ctx := context.Background()
	results, err := cat.List(ctx, ArtifactKindProvider, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "listed", results[0].Reference.Name)
}

func TestAllowlistCatalog_NameDelegatesToInner(t *testing.T) {
	inner := &allowlistMock{name: "my-catalog"}
	cat := NewAllowlistCatalog(inner, []string{"exec"})

	assert.Equal(t, "my-catalog", cat.Name())
}

func TestAllowlistCatalog_InnerReturnsWrapped(t *testing.T) {
	inner := &allowlistMock{name: "inner"}
	cat := NewAllowlistCatalog(inner, nil)

	assert.Equal(t, inner, cat.Inner())
}

func TestAllowlistCatalog_StoreDelegatesToInner(t *testing.T) {
	inner := &allowlistMock{name: "test"}
	cat := NewAllowlistCatalog(inner, []string{"exec"})

	// Store is not gated by allowlist (writes are unrestricted)
	ctx := context.Background()
	ref := Reference{Name: "not-in-allowlist", Kind: ArtifactKindProvider}
	_, err := cat.Store(ctx, ref, []byte("content"), nil, nil, false)
	assert.NoError(t, err)
}

func TestAllowlistCatalog_DeleteDelegatesToInner(t *testing.T) {
	inner := &allowlistMock{name: "test"}
	cat := NewAllowlistCatalog(inner, []string{"exec"})

	ctx := context.Background()
	ref := Reference{Name: "not-in-allowlist", Kind: ArtifactKindProvider}
	err := cat.Delete(ctx, ref)
	assert.NoError(t, err)
}
