// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePluginKind(t *testing.T) {
	tests := []struct {
		input   string
		want    ArtifactKind
		wantErr bool
	}{
		{"provider", ArtifactKindProvider, false},
		{"auth-handler", ArtifactKindAuthHandler, false},
		{"solution", "", true},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ValidatePluginKind(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestReadPlatformBinaries(t *testing.T) {
	// Create temp binaries
	dir := t.TempDir()
	linuxBin := filepath.Join(dir, "plugin-linux")
	darwinBin := filepath.Join(dir, "plugin-darwin")

	require.NoError(t, os.WriteFile(linuxBin, []byte("linux-binary-data"), 0o755))
	require.NoError(t, os.WriteFile(darwinBin, []byte("darwin-binary-data"), 0o755))

	t.Run("valid platforms", func(t *testing.T) {
		result, err := ReadPlatformBinaries(map[string]string{
			"linux/amd64":  linuxBin,
			"darwin/arm64": darwinBin,
		})
		require.NoError(t, err)
		assert.Len(t, result, 2)

		// Verify data is loaded
		for _, pb := range result {
			assert.NotEmpty(t, pb.Data)
			assert.NotEmpty(t, pb.Platform)
		}
	})

	t.Run("unsupported platform", func(t *testing.T) {
		_, err := ReadPlatformBinaries(map[string]string{
			"solaris/sparc": linuxBin,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported platform")
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := ReadPlatformBinaries(map[string]string{
			"linux/amd64": filepath.Join(dir, "nonexistent"),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("directory instead of file", func(t *testing.T) {
		_, err := ReadPlatformBinaries(map[string]string{
			"linux/amd64": dir,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "directory")
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := ReadPlatformBinaries(map[string]string{
			"linux/amd64": "",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "non-empty")
	})

	t.Run("empty map", func(t *testing.T) {
		_, err := ReadPlatformBinaries(map[string]string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no platform binaries")
	})

	t.Run("empty binary", func(t *testing.T) {
		emptyFile := filepath.Join(dir, "empty")
		require.NoError(t, os.WriteFile(emptyFile, nil, 0o755))

		_, err := ReadPlatformBinaries(map[string]string{
			"linux/amd64": emptyFile,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})
}

func BenchmarkReadPlatformBinaries(b *testing.B) {
	dir := b.TempDir()
	binFile := filepath.Join(dir, "test-binary")
	_ = os.WriteFile(binFile, make([]byte, 1024), 0o755)

	paths := map[string]string{"linux/amd64": binFile}

	for b.Loop() {
		_, _ = ReadPlatformBinaries(paths)
	}
}

func BenchmarkValidatePluginKind(b *testing.B) {
	for b.Loop() {
		_, _ = ValidatePluginKind("provider")
	}
}
