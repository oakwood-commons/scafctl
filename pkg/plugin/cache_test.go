// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCache_PutAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	data := []byte("#!/bin/bash\necho hello")
	name := "test-plugin"
	version := "1.0.0"
	platform := "linux/amd64"

	// Put
	path, err := cache.Put(name, version, platform, data)
	require.NoError(t, err)
	assert.Contains(t, path, name)
	assert.Contains(t, path, version)
	assert.Contains(t, path, "linux-amd64")

	// Verify file exists and is executable
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.Mode()&0o111 != 0, "file should be executable")

	// Get without digest
	gotPath, ok := cache.Get(name, version, platform, "")
	assert.True(t, ok)
	assert.Equal(t, path, gotPath)

	// Get with correct digest
	digest, err := cache.Digest(name, version, platform)
	require.NoError(t, err)
	assert.Contains(t, digest, "sha256:")

	gotPath, ok = cache.Get(name, version, platform, digest)
	assert.True(t, ok)
	assert.Equal(t, path, gotPath)

	// Get with wrong digest
	_, ok = cache.Get(name, version, platform, "sha256:wrong")
	assert.False(t, ok)
}

func TestCache_GetMissing(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	_, ok := cache.Get("nonexistent", "1.0.0", "linux/amd64", "")
	assert.False(t, ok)
}

func TestCache_List(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	// Empty cache
	items, err := cache.List()
	require.NoError(t, err)
	assert.Empty(t, items)

	// Add some plugins
	_, err = cache.Put("plugin-a", "1.0.0", "linux/amd64", []byte("binary-a"))
	require.NoError(t, err)
	_, err = cache.Put("plugin-b", "2.0.0", "darwin/arm64", []byte("binary-b"))
	require.NoError(t, err)

	items, err = cache.List()
	require.NoError(t, err)
	assert.Len(t, items, 2)

	// Verify items contain expected data
	names := make(map[string]bool)
	for _, item := range items {
		names[item.Name] = true
		assert.NotEmpty(t, item.Path)
		assert.True(t, item.Size > 0)
	}
	assert.True(t, names["plugin-a"])
	assert.True(t, names["plugin-b"])
}

func TestCache_ListEmptyDir(t *testing.T) {
	// Non-existent directory
	cache := NewCache("/nonexistent/path/that/does/not/exist")
	items, err := cache.List()
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestCache_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	_, err := cache.Put("test-plugin", "1.0.0", "linux/amd64", []byte("binary"))
	require.NoError(t, err)

	// Verify it exists
	_, ok := cache.Get("test-plugin", "1.0.0", "linux/amd64", "")
	assert.True(t, ok)

	// Remove
	err = cache.Remove("test-plugin", "1.0.0", "linux/amd64")
	require.NoError(t, err)

	// Verify it's gone
	_, ok = cache.Get("test-plugin", "1.0.0", "linux/amd64", "")
	assert.False(t, ok)
}

func TestCache_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	// Write should be atomic (no .tmp file left)
	_, err := cache.Put("test-plugin", "1.0.0", "linux/amd64", []byte("binary"))
	require.NoError(t, err)

	dir := filepath.Join(tmpDir, "test-plugin", "1.0.0", "linux-amd64")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".tmp", "no temp files should remain")
	}
}

func TestCache_DefaultDir(t *testing.T) {
	cache := NewCache("")
	assert.NotEmpty(t, cache.Dir())
}

func TestCache_GetLatestCached_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	_, _, ok := cache.GetLatestCached("nonexistent", "linux/amd64")
	assert.False(t, ok)
}

func TestCache_GetLatestCached_NonExecutable(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	// Create a version directory with a non-executable file.
	dir := filepath.Join(tmpDir, "myplugin", "1.0.0", "linux-amd64")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "myplugin"), []byte("bin"), 0o644))

	_, _, ok := cache.GetLatestCached("myplugin", "linux/amd64")
	assert.False(t, ok)
}

func TestCache_GetLatestCached_PicksHighestSemver(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	// Create versions: 0.9.0, 0.10.0, 1.0.0
	for _, v := range []string{"0.9.0", "0.10.0", "1.0.0"} {
		_, err := cache.Put("myplugin", v, "linux/amd64", []byte("binary-"+v))
		require.NoError(t, err)
	}

	path, version, ok := cache.GetLatestCached("myplugin", "linux/amd64")
	require.True(t, ok)
	assert.Equal(t, "1.0.0", version)
	assert.Contains(t, path, "1.0.0")
}

func TestCache_GetLatestCached_SemverBeatsLexicographic(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	// 0.10.0 > 0.9.0 semver, but 0.9.0 > 0.10.0 lexicographically.
	for _, v := range []string{"0.9.0", "0.10.0"} {
		_, err := cache.Put("myplugin", v, "linux/amd64", []byte("binary-"+v))
		require.NoError(t, err)
	}

	_, version, ok := cache.GetLatestCached("myplugin", "linux/amd64")
	require.True(t, ok)
	assert.Equal(t, "0.10.0", version, "should pick 0.10.0 (semver) not 0.9.0 (lexicographic)")
}
