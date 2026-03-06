// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformToOCI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantOS   string
		wantArch string
		wantErr  bool
	}{
		{name: "linux/amd64", input: "linux/amd64", wantOS: "linux", wantArch: "amd64"},
		{name: "darwin/arm64", input: "darwin/arm64", wantOS: "darwin", wantArch: "arm64"},
		{name: "windows/amd64", input: "windows/amd64", wantOS: "windows", wantArch: "amd64"},
		{name: "invalid empty", input: "", wantErr: true},
		{name: "invalid no slash", input: "linux", wantErr: true},
		{name: "invalid trailing slash", input: "linux/", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := PlatformToOCI(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOS, p.OS)
			assert.Equal(t, tt.wantArch, p.Architecture)
		})
	}
}

func TestOCIPlatformString(t *testing.T) {
	tests := []struct {
		name  string
		input *ocispec.Platform
		want  string
	}{
		{
			name:  "linux/amd64",
			input: &ocispec.Platform{OS: "linux", Architecture: "amd64"},
			want:  "linux/amd64",
		},
		{
			name:  "darwin/arm64",
			input: &ocispec.Platform{OS: "darwin", Architecture: "arm64"},
			want:  "darwin/arm64",
		},
		{
			name:  "nil platform",
			input: nil,
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, OCIPlatformString(tt.input))
		})
	}
}

func TestMatchPlatform(t *testing.T) {
	index := &ocispec.Index{
		Manifests: []ocispec.Descriptor{
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Platform:  &ocispec.Platform{OS: "linux", Architecture: "amd64"},
			},
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Platform:  &ocispec.Platform{OS: "darwin", Architecture: "arm64"},
			},
			{
				MediaType: ocispec.MediaTypeImageManifest,
				Platform:  &ocispec.Platform{OS: "windows", Architecture: "amd64"},
			},
		},
	}

	t.Run("match linux/amd64", func(t *testing.T) {
		desc, err := MatchPlatform(index, "linux/amd64")
		require.NoError(t, err)
		assert.Equal(t, "linux", desc.Platform.OS)
		assert.Equal(t, "amd64", desc.Platform.Architecture)
	})

	t.Run("match darwin/arm64", func(t *testing.T) {
		desc, err := MatchPlatform(index, "darwin/arm64")
		require.NoError(t, err)
		assert.Equal(t, "darwin", desc.Platform.OS)
		assert.Equal(t, "arm64", desc.Platform.Architecture)
	})

	t.Run("no match linux/arm64", func(t *testing.T) {
		_, err := MatchPlatform(index, "linux/arm64")
		require.Error(t, err)
		assert.True(t, IsPlatformNotFound(err))
		var pnf *PlatformNotFoundError
		require.ErrorAs(t, err, &pnf)
		assert.Equal(t, "linux/arm64", pnf.Platform)
		assert.ElementsMatch(t, []string{"linux/amd64", "darwin/arm64", "windows/amd64"}, pnf.Available)
	})

	t.Run("nil index", func(t *testing.T) {
		_, err := MatchPlatform(nil, "linux/amd64")
		require.Error(t, err)
	})

	t.Run("invalid platform format", func(t *testing.T) {
		_, err := MatchPlatform(index, "invalid")
		require.Error(t, err)
	})
}

func TestIndexPlatforms(t *testing.T) {
	index := &ocispec.Index{
		Manifests: []ocispec.Descriptor{
			{Platform: &ocispec.Platform{OS: "linux", Architecture: "amd64"}},
			{Platform: &ocispec.Platform{OS: "darwin", Architecture: "arm64"}},
			{Platform: nil}, // no platform
		},
	}

	platforms := IndexPlatforms(index)
	assert.ElementsMatch(t, []string{"linux/amd64", "darwin/arm64"}, platforms)
}

func TestIndexPlatforms_Nil(t *testing.T) {
	assert.Nil(t, IndexPlatforms(nil))
}

func TestIsImageIndex(t *testing.T) {
	assert.True(t, IsImageIndex(ocispec.Descriptor{MediaType: ocispec.MediaTypeImageIndex}))
	assert.False(t, IsImageIndex(ocispec.Descriptor{MediaType: ocispec.MediaTypeImageManifest}))
	assert.False(t, IsImageIndex(ocispec.Descriptor{MediaType: ""}))
}

func TestIsSupportedPlatform(t *testing.T) {
	assert.True(t, IsSupportedPlatform("linux/amd64"))
	assert.True(t, IsSupportedPlatform("darwin/arm64"))
	assert.True(t, IsSupportedPlatform("windows/amd64"))
	assert.False(t, IsSupportedPlatform("freebsd/amd64"))
	assert.False(t, IsSupportedPlatform(""))
}

func TestSupportedPluginPlatforms(t *testing.T) {
	assert.Contains(t, SupportedPluginPlatforms, "linux/amd64")
	assert.Contains(t, SupportedPluginPlatforms, "linux/arm64")
	assert.Contains(t, SupportedPluginPlatforms, "darwin/amd64")
	assert.Contains(t, SupportedPluginPlatforms, "darwin/arm64")
	assert.Contains(t, SupportedPluginPlatforms, "windows/amd64")
	assert.Len(t, SupportedPluginPlatforms, 5)
}
