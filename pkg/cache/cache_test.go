// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{100, "100 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, FormatBytes(tt.bytes))
		})
	}
}

func TestClearDirectory_NonExistent(t *testing.T) {
	t.Parallel()

	files, bytes, err := ClearDirectory("/nonexistent/path", "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), files)
	assert.Equal(t, int64(0), bytes)
}

func TestClearDirectory_EmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files, bytes, err := ClearDirectory(dir, "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), files)
	assert.Equal(t, int64(0), bytes)
}

func TestClearDirectory_WithFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// Create test files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file2.txt"), []byte("world!"), 0o644))

	files, bytes, err := ClearDirectory(dir, "")
	require.NoError(t, err)
	assert.Equal(t, int64(2), files)
	assert.Equal(t, int64(11), bytes)

	// Directory should still exist but be empty
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestClearDirectory_WithPattern(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cache1.json"), []byte("data1"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cache2.json"), []byte("data2"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep"), 0o644))

	files, _, err := ClearDirectory(dir, "*.json")
	require.NoError(t, err)
	assert.Equal(t, int64(2), files)

	// keep.txt should still exist
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "keep.txt", entries[0].Name())
}

func TestGetCacheInfo_NonExistent(t *testing.T) {
	t.Parallel()

	info := GetCacheInfo("Test", "/nonexistent/path", "test cache")
	assert.Equal(t, "Test", info.Name)
	assert.Equal(t, "/nonexistent/path", info.Path)
	assert.Equal(t, "test cache", info.Description)
	assert.Equal(t, int64(0), info.Size)
	assert.Equal(t, int64(0), info.FileCount)
	assert.Equal(t, "0 B", info.SizeHuman)
}

func TestGetCacheInfo_WithFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f1"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f2"), []byte("world"), 0o644))

	info := GetCacheInfo("Test", dir, "test cache")
	assert.Equal(t, "Test", info.Name)
	assert.Equal(t, int64(10), info.Size)
	assert.Equal(t, int64(2), info.FileCount)
	assert.Equal(t, "10 B", info.SizeHuman)
}

func TestValidKinds(t *testing.T) {
	t.Parallel()

	assert.Contains(t, ValidKinds, "all")
	assert.Contains(t, ValidKinds, "http")
	assert.Contains(t, ValidKinds, "build")
	assert.Contains(t, ValidKinds, "artifact")
	assert.Len(t, ValidKinds, 4)
}
