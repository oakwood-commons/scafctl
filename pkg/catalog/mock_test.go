// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPopulatedMockSource(t *testing.T) PluginSource {
	t.Helper()
	src := NewMockPluginSource("mock")
	m, ok := src.(*mockPluginSource)
	require.True(t, ok)
	m.addArtifact(testRef("p", "1.0.0"), []byte("v1"))
	m.addArtifact(testRef("p", "1.2.0"), []byte("v12"))
	m.addArtifact(testRef("p", "2.0.0"), []byte("v2"))
	return src
}

func TestMockPluginSource_Find(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
		wantErr error
	}{
		{name: "empty is latest", version: "", want: "2.0.0"},
		{name: "latest keyword", version: "latest", want: "2.0.0"},
		{name: "exact version", version: "1.0.0", want: "1.0.0"},
		{name: "constraint picks newest match", version: "^1.0.0", want: "1.2.0"},
		{name: "no match is not-found", version: "^3.0.0", wantErr: ErrArtifactNotFound},
		{name: "invalid constraint", version: "not a version", wantErr: ErrInvalidQuery},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := newPopulatedMockSource(t)
			info, err := src.Find(context.Background(), ArtifactQuery{Name: "p", Kind: ArtifactKindProvider, Version: tc.version})
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, info.Reference.Version.String())
			assert.Equal(t, "mock", info.Catalog)
		})
	}
}

func TestMockPluginSource_FindWithResolveFunc(t *testing.T) {
	t.Parallel()

	var got Reference
	src := NewMockPluginSource("mock", WithResolveFunc(func(_ context.Context, ref Reference) (ArtifactInfo, error) {
		got = ref
		return ArtifactInfo{Reference: ref, Catalog: "mock"}, nil
	}))

	_, err := src.Find(context.Background(), ArtifactQuery{Name: "p", Kind: ArtifactKindProvider, Version: "1.2.3"})
	require.NoError(t, err)
	assert.Equal(t, semver.MustParse("1.2.3"), got.Version, "an exact version is pinned")

	_, err = src.Find(context.Background(), ArtifactQuery{Name: "p", Kind: ArtifactKindProvider, Version: "^1.0.0"})
	require.NoError(t, err)
	assert.Nil(t, got.Version, "a constraint is left to the resolve func")
}

func TestMockPluginSource_FetchAndDigest(t *testing.T) {
	t.Parallel()
	src := newPopulatedMockSource(t)
	ref := testRef("p", "1.0.0")

	data, info, err := src.FetchByPlatform(context.Background(), ref, "linux/amd64")
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), data)
	assert.Equal(t, "mock", info.Catalog)

	digest, err := src.ResolveContentDigest(context.Background(), ref, "linux/amd64", MediaTypeProviderBinary)
	require.NoError(t, err)
	assert.Equal(t, info.Digest, digest.ContentDigest)

	_, err = src.ResolveContentDigest(context.Background(), testRef("missing", "1.0.0"), "linux/amd64", MediaTypeProviderBinary)
	require.ErrorIs(t, err, ErrArtifactNotFound)

	platforms, err := src.ListPlatforms(context.Background(), ref)
	require.NoError(t, err)
	assert.Nil(t, platforms)

	_, err = src.ListPlatforms(context.Background(), testRef("missing", "1.0.0"))
	require.ErrorIs(t, err, ErrArtifactNotFound)

	custom := NewMockPluginSource("custom", WithListPlatformsFunc(func(_ context.Context, _ Reference) ([]string, error) {
		return []string{"linux/amd64", "darwin/arm64"}, nil
	}))
	platforms, err = custom.ListPlatforms(context.Background(), ref)
	require.NoError(t, err)
	assert.Equal(t, []string{"linux/amd64", "darwin/arm64"}, platforms)
}
