// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/terminal/format"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePlatformFlags(t *testing.T) {
	t.Run("valid single platform", func(t *testing.T) {
		// Create a temp binary
		dir := t.TempDir()
		bin := filepath.Join(dir, "my-plugin")
		require.NoError(t, os.WriteFile(bin, []byte("binary"), 0o755))

		result, err := parsePlatformFlags([]string{"linux/amd64=" + bin})
		require.NoError(t, err)
		assert.Len(t, result, 1)
		assert.Equal(t, bin, result["linux/amd64"])
	})

	t.Run("valid multiple platforms", func(t *testing.T) {
		dir := t.TempDir()
		linBin := filepath.Join(dir, "linux-bin")
		darBin := filepath.Join(dir, "darwin-bin")
		require.NoError(t, os.WriteFile(linBin, []byte("linux"), 0o755))
		require.NoError(t, os.WriteFile(darBin, []byte("darwin"), 0o755))

		result, err := parsePlatformFlags([]string{
			"linux/amd64=" + linBin,
			"darwin/arm64=" + darBin,
		})
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("missing equals sign", func(t *testing.T) {
		_, err := parsePlatformFlags([]string{"linux/amd64"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --platform format")
	})

	t.Run("empty platform", func(t *testing.T) {
		_, err := parsePlatformFlags([]string{"=./bin"})
		require.Error(t, err)
	})

	t.Run("empty path", func(t *testing.T) {
		_, err := parsePlatformFlags([]string{"linux/amd64="})
		require.Error(t, err)
	})

	t.Run("unsupported platform passes through", func(t *testing.T) {
		// parsePlatformFlags no longer validates platform names;
		// that is handled by catalog.ReadPlatformBinaries.
		result, err := parsePlatformFlags([]string{"freebsd/amd64=./bin"})
		require.NoError(t, err)
		assert.Equal(t, "./bin", result["freebsd/amd64"])
	})

	t.Run("duplicate platform", func(t *testing.T) {
		_, err := parsePlatformFlags([]string{
			"linux/amd64=./bin1",
			"linux/amd64=./bin2",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate platform")
	})

	t.Run("nonexistent path passes through", func(t *testing.T) {
		// parsePlatformFlags no longer stats files;
		// that is handled by catalog.ReadPlatformBinaries.
		result, err := parsePlatformFlags([]string{"linux/amd64=/nonexistent/path"})
		require.NoError(t, err)
		assert.Equal(t, "/nonexistent/path", result["linux/amd64"])
	})

	t.Run("directory path passes through", func(t *testing.T) {
		// parsePlatformFlags no longer checks if path is a directory;
		// that is handled by catalog.ReadPlatformBinaries.
		dir := t.TempDir()
		result, err := parsePlatformFlags([]string{"linux/amd64=" + dir})
		require.NoError(t, err)
		assert.Equal(t, dir, result["linux/amd64"])
	})
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, format.Bytes(tt.input))
	}
}
