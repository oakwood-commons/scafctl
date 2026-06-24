// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS != "windows" {
		assert.True(t, info.Mode()&0o111 != 0, "file should be executable")
	}

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

func TestCache_List_NotADirectory(t *testing.T) {
	// A file (not a directory) as the cache path should return an error.
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(tmpFile, []byte("x"), 0o644))

	cache := NewCache(tmpFile)
	items, err := cache.List()
	require.Error(t, err)
	assert.Nil(t, items)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestCache_BinaryPath_WindowsExeExtension(t *testing.T) {
	cache := NewCache("/tmp/plugins")

	// Windows platform should get .exe extension
	winPath := cache.binaryPath("myplugin", "1.0.0", "windows/amd64", "")
	assert.Equal(t, "myplugin.exe", filepath.Base(winPath))

	// Linux platform should NOT get .exe extension
	linuxPath := cache.binaryPath("myplugin", "1.0.0", "linux/amd64", "")
	assert.Equal(t, "myplugin", filepath.Base(linuxPath))

	// Darwin platform should NOT get .exe extension
	darwinPath := cache.binaryPath("myplugin", "1.0.0", "darwin/arm64", "")
	assert.Equal(t, "myplugin", filepath.Base(darwinPath))
}

func TestCache_PutAndGet_WindowsPlatform(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	data := []byte("MZ fake PE binary")
	name := "test-plugin"
	version := "1.0.0"
	platform := "windows/amd64"

	// Put
	path, err := cache.Put(name, version, platform, data)
	require.NoError(t, err)
	assert.Contains(t, path, "test-plugin.exe")
	assert.Contains(t, path, "windows-amd64")

	// Verify the file has the .exe extension on disk
	assert.Equal(t, "test-plugin.exe", filepath.Base(path))

	// Get should find it
	gotPath, ok := cache.Get(name, version, platform, "")
	assert.True(t, ok)
	assert.Equal(t, path, gotPath)
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

func TestCache_Put_ReplacesStaleExistingBinary(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	name := "test-plugin"
	version := "1.0.0"
	platform := "windows/amd64"

	first := []byte("stale-binary")
	second := []byte("fresh-binary")

	path, err := cache.Put(name, version, platform, first)
	require.NoError(t, err)

	// Simulate a stale cached file by replacing contents directly.
	require.NoError(t, os.WriteFile(path, []byte("corrupted"), 0o644))

	path2, err := cache.Put(name, version, platform, second)
	require.NoError(t, err)
	assert.Equal(t, path, path2)

	data, err := os.ReadFile(path2)
	require.NoError(t, err)
	assert.Equal(t, second, data)
}

func TestCache_Put_RenameRetryAfterConflictingPath(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	name := "test-plugin"
	version := "1.0.0"
	platform := "windows/amd64"

	binaryPath := cache.binaryPath(name, version, platform, "")
	require.NoError(t, os.MkdirAll(binaryPath, 0o755))

	data := []byte("fresh-binary")
	path, err := cache.Put(name, version, platform, data)
	require.NoError(t, err)
	assert.Equal(t, binaryPath, path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, content)
}

func TestCache_Put_RenameFailsWhenDestinationIsNonEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	name := "test-plugin"
	version := "1.0.0"
	platform := "windows/amd64"

	binaryPath := cache.binaryPath(name, version, platform, "")
	require.NoError(t, os.MkdirAll(binaryPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binaryPath, "keep.txt"), []byte("x"), 0o644))

	_, err := cache.Put(name, version, platform, []byte("fresh-binary"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "moving plugin binary into cache")
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
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support Unix file permission bits")
	}

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

func TestCache_Get_MigrationFallback(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	name := "test-plugin"
	version := "1.0.0"
	platform := "windows/amd64"
	data := []byte("MZ fake PE binary")

	// Simulate a legacy cache entry: binary at old path without .exe.
	legacyDir := filepath.Join(tmpDir, name, version, "windows-amd64")
	require.NoError(t, os.MkdirAll(legacyDir, 0o755))
	legacyPath := filepath.Join(legacyDir, name) // no .exe
	require.NoError(t, os.WriteFile(legacyPath, data, 0o755))

	// Get should find the legacy binary and rename it to the .exe path.
	gotPath, ok := cache.Get(name, version, platform, "")
	require.True(t, ok, "Get should succeed via migration fallback")
	assert.Contains(t, gotPath, "test-plugin.exe", "returned path should have .exe extension")

	// The legacy path should no longer exist.
	_, err := os.Stat(legacyPath)
	assert.True(t, os.IsNotExist(err), "legacy binary should be removed after migration")

	// The new path should exist.
	_, err = os.Stat(gotPath)
	require.NoError(t, err, "migrated binary should exist at new path")

	// Verify content is preserved.
	content, err := os.ReadFile(gotPath)
	require.NoError(t, err)
	assert.Equal(t, data, content)
}

func TestCache_Get_MigrationFallback_NotTriggeredForNonWindows(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	name := "test-plugin"
	version := "1.0.0"
	platform := "linux/amd64"

	// Create a binary at the legacy path (no .exe, same as normal linux path).
	// For linux, binaryPath == legacyBinaryPath, so this is just a normal Get.
	// If the binary doesn't exist at the expected path, Get returns false — no fallback.
	_, ok := cache.Get(name, version, platform, "")
	assert.False(t, ok, "Get should return false for missing linux binary")
}

func TestCache_GetLatestBinary(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)
	platform := CurrentPlatform()

	// Put two versions
	_, err := cache.Put("myplugin", "1.0.0", platform, []byte("v1"))
	require.NoError(t, err)
	_, err = cache.Put("myplugin", "2.0.0", platform, []byte("v2"))
	require.NoError(t, err)

	path, version, ok := cache.GetLatestBinary("myplugin")
	assert.True(t, ok)
	assert.Equal(t, "2.0.0", version)
	assert.Contains(t, path, "myplugin")
}

func TestCache_GetLatestBinary_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	_, _, ok := cache.GetLatestBinary("nonexistent")
	assert.False(t, ok)
}

func TestCache_ListCurrentPlatform(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)
	platform := CurrentPlatform()

	// Put plugins for current platform and a different one
	_, err := cache.Put("plugin-a", "1.0.0", platform, []byte("a"))
	require.NoError(t, err)
	_, err = cache.Put("plugin-b", "1.0.0", platform, []byte("b"))
	require.NoError(t, err)
	_, err = cache.Put("plugin-c", "1.0.0", "fake/arch", []byte("c"))
	require.NoError(t, err)

	// List should only return plugins for current platform
	result, err := cache.ListCurrentPlatform()
	require.NoError(t, err)
	assert.Len(t, result, 2)

	names := make(map[string]bool)
	for _, p := range result {
		names[p.Name] = true
	}
	assert.True(t, names["plugin-a"])
	assert.True(t, names["plugin-b"])
	assert.False(t, names["plugin-c"])
}

func TestCache_ListCurrentPlatform_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	result, err := cache.ListCurrentPlatform()
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestCache_ListCurrentPlatform_DeduplicatesVersions(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)
	platform := CurrentPlatform()

	// Put two versions of the same plugin
	_, err := cache.Put("myplugin", "1.0.0", platform, []byte("v1"))
	require.NoError(t, err)
	_, err = cache.Put("myplugin", "2.0.0", platform, []byte("v2"))
	require.NoError(t, err)

	result, err := cache.ListCurrentPlatform()
	require.NoError(t, err)
	assert.Len(t, result, 1, "should deduplicate by name")
	assert.Equal(t, "myplugin", result[0].Name)
}

func TestCache_ListCurrentPlatform_PicksLatestSemver(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)
	platform := CurrentPlatform()

	// Put versions in non-semver directory order (1.0.0, 10.0.0, 2.0.0).
	// Directory listing returns lexicographic order: 1.0.0, 10.0.0, 2.0.0.
	// The result must reflect proper semver comparison: 10.0.0.
	_, err := cache.Put("myplugin", "1.0.0", platform, []byte("v1"))
	require.NoError(t, err)
	_, err = cache.Put("myplugin", "10.0.0", platform, []byte("v10"))
	require.NoError(t, err)
	_, err = cache.Put("myplugin", "2.0.0", platform, []byte("v2"))
	require.NoError(t, err)

	result, err := cache.ListCurrentPlatform()
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "10.0.0", result[0].Version, "should pick latest by semver, not directory order")
}

// --- Managed-mode tests ---

func TestManagedCache_PutAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewManagedCache(tmpDir, 10*1024*1024)
	require.NoError(t, err)

	data := []byte("#!/bin/bash\necho managed")
	name := "managed-plugin"
	version := "2.0.0"
	platform := "linux/amd64"

	path, err := cache.Put(name, version, platform, data)
	require.NoError(t, err)
	assert.Contains(t, path, name)
	assert.Contains(t, path, version)

	// Get without digest
	gotPath, ok := cache.Get(name, version, platform, "")
	assert.True(t, ok)
	assert.Equal(t, path, gotPath)

	// Get with correct digest
	digest, err := cache.Digest(name, version, platform)
	require.NoError(t, err)
	gotPath, ok = cache.Get(name, version, platform, digest)
	assert.True(t, ok)
	assert.Equal(t, path, gotPath)

	// Get with wrong digest
	_, ok = cache.Get(name, version, platform, "sha256:wrong")
	assert.False(t, ok)
}

func TestManagedCache_Pin(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewManagedCache(tmpDir, 10*1024*1024)
	require.NoError(t, err)

	data := []byte("binary-content")
	name := "pin-plugin"
	version := "1.0.0"
	platform := "darwin/arm64"

	_, err = cache.Put(name, version, platform, data)
	require.NoError(t, err)

	path, release, ok := cache.Pin(name, version, platform)
	require.True(t, ok)
	assert.NotEmpty(t, path)
	assert.NotNil(t, release)

	// File exists at pinned path
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)

	// Release should not panic
	release()
}

func TestManagedCache_PinMissing(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewManagedCache(tmpDir, 10*1024*1024)
	require.NoError(t, err)

	_, _, ok := cache.Pin("nonexistent", "1.0.0", "linux/amd64")
	assert.False(t, ok)
}

func TestManagedCache_GetLatestCached(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewManagedCache(tmpDir, 10*1024*1024)
	require.NoError(t, err)

	platform := "linux/amd64"
	_, err = cache.Put("myplugin", "1.0.0", platform, []byte("v1"))
	require.NoError(t, err)
	_, err = cache.Put("myplugin", "2.5.0", platform, []byte("v2.5"))
	require.NoError(t, err)
	_, err = cache.Put("myplugin", "2.0.0", platform, []byte("v2"))
	require.NoError(t, err)

	path, version, ok := cache.GetLatestCached("myplugin", platform)
	require.True(t, ok)
	assert.Equal(t, "2.5.0", version)
	assert.Contains(t, path, "2.5.0")
}

func TestManagedCache_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	cache, err := NewManagedCache(tmpDir, 10*1024*1024)
	require.NoError(t, err)

	platform := "linux/amd64"
	path, err := cache.Put("rm-plugin", "1.0.0", platform, []byte("data"))
	require.NoError(t, err)

	// Exists before remove
	_, ok := cache.Get("rm-plugin", "1.0.0", platform, "")
	assert.True(t, ok)

	// Remove
	err = cache.Remove("rm-plugin", "1.0.0", platform)
	assert.NoError(t, err)

	// Gone after remove
	_, ok = cache.Get("rm-plugin", "1.0.0", platform, "")
	assert.False(t, ok)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestManagedCache_Eviction(t *testing.T) {
	tmpDir := t.TempDir()
	// Small budget: 100 bytes — each entry is ~14 bytes
	cache, err := NewManagedCache(tmpDir, 100)
	require.NoError(t, err)

	platform := "linux/amd64"
	// Write enough entries to trigger eviction
	for i := 0; i < 10; i++ {
		ver := fmt.Sprintf("1.0.%d", i)
		_, err := cache.Put("evict-plugin", ver, platform, []byte("binary-content"))
		require.NoError(t, err)
	}

	// Some early versions should have been evicted
	_, ok := cache.Get("evict-plugin", "1.0.0", platform, "")
	assert.False(t, ok, "earliest version should be evicted")

	// Latest should still exist
	_, ok = cache.Get("evict-plugin", "1.0.9", platform, "")
	assert.True(t, ok, "latest version should survive")
}

func TestManagedCache_WarmUp(t *testing.T) {
	tmpDir := t.TempDir()

	// Pre-populate the cache directory manually
	platform := "linux/amd64"
	platformKey := PlatformCacheKey(platform)
	for _, ver := range []string{"1.0.0", "2.0.0", "3.0.0"} {
		dir := filepath.Join(tmpDir, "warm-plugin", ver, platformKey)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "warm-plugin"), []byte("bin-"+ver), 0o755))
	}

	// Create cache and warm up
	cache, err := NewManagedCache(tmpDir, 10*1024*1024)
	require.NoError(t, err)
	require.NoError(t, cache.WarmUp())

	// Should find the latest version
	path, version, ok := cache.GetLatestCached("warm-plugin", platform)
	assert.True(t, ok)
	assert.Equal(t, "3.0.0", version)
	assert.Contains(t, path, "3.0.0")

	// Pin should work after warm-up
	pinPath, release, ok := cache.Pin("warm-plugin", "2.0.0", platform)
	assert.True(t, ok)
	assert.Contains(t, pinPath, "2.0.0")
	release()
}

func TestUnboundedCache_Pin(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	data := []byte("binary")
	platform := "linux/amd64"
	_, err := cache.Put("pin-test", "1.0.0", platform, data)
	require.NoError(t, err)

	// Pin should work (stat-based, no-op release)
	path, release, ok := cache.Pin("pin-test", "1.0.0", platform)
	assert.True(t, ok)
	assert.NotEmpty(t, path)
	assert.NotNil(t, release)
	release() // no-op, should not panic

	// Pin missing should return false
	_, _, ok = cache.Pin("nonexistent", "1.0.0", platform)
	assert.False(t, ok)
}

func TestManagedCache_WarmUpNoOp(t *testing.T) {
	tmpDir := t.TempDir()
	cache := NewCache(tmpDir)

	// WarmUp on unbounded cache is a no-op
	err := cache.WarmUp()
	assert.NoError(t, err)
}
