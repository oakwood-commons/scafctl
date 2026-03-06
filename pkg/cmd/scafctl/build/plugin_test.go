// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package build

import (
	"os"
	"path/filepath"
	"testing"

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

	t.Run("unsupported platform", func(t *testing.T) {
		dir := t.TempDir()
		bin := filepath.Join(dir, "bin")
		require.NoError(t, os.WriteFile(bin, []byte("binary"), 0o755))

		_, err := parsePlatformFlags([]string{"freebsd/amd64=" + bin})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported platform")
	})

	t.Run("duplicate platform", func(t *testing.T) {
		dir := t.TempDir()
		bin := filepath.Join(dir, "bin")
		require.NoError(t, os.WriteFile(bin, []byte("binary"), 0o755))

		_, err := parsePlatformFlags([]string{
			"linux/amd64=" + bin,
			"linux/amd64=" + bin,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate platform")
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := parsePlatformFlags([]string{"linux/amd64=/nonexistent/path"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "binary not found")
	})

	t.Run("path is directory", func(t *testing.T) {
		dir := t.TempDir()
		_, err := parsePlatformFlags([]string{"linux/amd64=" + dir})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "is a directory")
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
		assert.Equal(t, tt.want, formatBytes(tt.input))
	}
}
