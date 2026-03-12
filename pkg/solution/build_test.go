// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveArtifactName(t *testing.T) {
	t.Run("explicit name takes priority", func(t *testing.T) {
		name, err := ResolveArtifactName("my-app", "from-metadata", "/path/to/file.yaml")
		require.NoError(t, err)
		assert.Equal(t, "my-app", name)
	})

	t.Run("falls back to metadata name", func(t *testing.T) {
		name, err := ResolveArtifactName("", "from-metadata", "/path/to/file.yaml")
		require.NoError(t, err)
		assert.Equal(t, "from-metadata", name)
	})

	t.Run("falls back to filename", func(t *testing.T) {
		name, err := ResolveArtifactName("", "", "/path/to/my-solution.yaml")
		require.NoError(t, err)
		assert.Equal(t, "my-solution", name)
	})

	t.Run("filename with nested path", func(t *testing.T) {
		name, err := ResolveArtifactName("", "", "solutions/gcp-basic.yml")
		require.NoError(t, err)
		assert.Equal(t, "gcp-basic", name)
	})

	t.Run("invalid name format", func(t *testing.T) {
		_, err := ResolveArtifactName("INVALID_NAME", "", "file.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid name")
	})

	t.Run("all empty falls back to filename base", func(t *testing.T) {
		// "file" is a valid catalog name
		name, err := ResolveArtifactName("", "", "file.yaml")
		require.NoError(t, err)
		assert.Equal(t, "file", name)
	})
}

func TestResolveArtifactVersion(t *testing.T) {
	v1 := semver.MustParse("1.0.0")
	v2 := semver.MustParse("2.0.0")

	t.Run("explicit version takes priority", func(t *testing.T) {
		v, overrides, err := ResolveArtifactVersion("2.0.0", v1)
		require.NoError(t, err)
		assert.Equal(t, v2, v)
		assert.True(t, overrides, "should indicate it overrides metadata version")
	})

	t.Run("explicit version same as metadata", func(t *testing.T) {
		v, overrides, err := ResolveArtifactVersion("1.0.0", v1)
		require.NoError(t, err)
		assert.Equal(t, v1, v)
		assert.False(t, overrides, "should not indicate override when versions match")
	})

	t.Run("explicit version no metadata", func(t *testing.T) {
		v, overrides, err := ResolveArtifactVersion("1.0.0", nil)
		require.NoError(t, err)
		assert.Equal(t, v1, v)
		assert.False(t, overrides)
	})

	t.Run("falls back to metadata version", func(t *testing.T) {
		v, overrides, err := ResolveArtifactVersion("", v1)
		require.NoError(t, err)
		assert.Equal(t, v1, v)
		assert.False(t, overrides)
	})

	t.Run("invalid explicit version", func(t *testing.T) {
		_, _, err := ResolveArtifactVersion("not-a-version", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid version")
	})

	t.Run("no version available", func(t *testing.T) {
		_, _, err := ResolveArtifactVersion("", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no version")
	})
}

func BenchmarkResolveArtifactName(b *testing.B) {
	for b.Loop() {
		_, _ = ResolveArtifactName("", "my-solution", "/path/to/file.yaml")
	}
}

func BenchmarkResolveArtifactVersion(b *testing.B) {
	metadata := semver.MustParse("1.0.0")
	for b.Loop() {
		_, _, _ = ResolveArtifactVersion("2.0.0", metadata)
	}
}
