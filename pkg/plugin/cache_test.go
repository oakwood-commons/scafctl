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

func TestSortAndDedupeLatest_DefaultKeepsOnlyLatestPerNamePlatform(t *testing.T) {
	cached := []CachedPlugin{
		{Name: "myplugin", Version: "0.9.0", Platform: "linux/amd64"},
		{Name: "myplugin", Version: "0.10.0", Platform: "linux/amd64"},
		{Name: "myplugin", Version: "1.0.0", Platform: "linux/amd64"},
	}

	result := SortAndDedupeLatest(cached, false)
	require.Len(t, result, 1)
	assert.Equal(t, "1.0.0", result[0].Version)
}

func TestSortAndDedupeLatest_AllVersionsReturnsEveryEntrySorted(t *testing.T) {
	cached := []CachedPlugin{
		{Name: "myplugin", Version: "0.9.0", Platform: "linux/amd64"},
		{Name: "myplugin", Version: "1.0.0", Platform: "linux/amd64"},
	}

	result := SortAndDedupeLatest(cached, true)
	require.Len(t, result, 2)
	// Sorted descending by semver within the same name.
	assert.Equal(t, "1.0.0", result[0].Version)
	assert.Equal(t, "0.9.0", result[1].Version)
}

func TestSortAndDedupeLatest_SemverBeatsLexicographic(t *testing.T) {
	cached := []CachedPlugin{
		{Name: "myplugin", Version: "0.9.0", Platform: "linux/amd64"},
		{Name: "myplugin", Version: "0.10.0", Platform: "linux/amd64"},
	}

	result := SortAndDedupeLatest(cached, false)
	require.Len(t, result, 1)
	assert.Equal(t, "0.10.0", result[0].Version, "should pick 0.10.0 (semver) not 0.9.0 (lexicographic)")
}

func TestSortAndDedupeLatest_ValidSemverAlwaysBeatsNonSemver(t *testing.T) {
	// "zzz-not-a-version" sorts lexically above all valid semver strings,
	// but a valid semver version must always be preferred as "latest".
	cached := []CachedPlugin{
		{Name: "myplugin", Version: "1.0.0", Platform: "linux/amd64"},
		{Name: "myplugin", Version: "9.0.0", Platform: "linux/amd64"},
		{Name: "myplugin", Version: "zzz-not-a-version", Platform: "linux/amd64"},
	}

	result := SortAndDedupeLatest(cached, false)
	require.Len(t, result, 1)
	assert.Equal(t, "9.0.0", result[0].Version)
}

func TestSortAndDedupeLatest_DedupeKeyIncludesPlatform(t *testing.T) {
	cached := []CachedPlugin{
		{Name: "myplugin", Version: "1.0.0", Platform: "linux/amd64"},
		{Name: "myplugin", Version: "1.0.0", Platform: "darwin/arm64"},
	}

	result := SortAndDedupeLatest(cached, false)
	assert.Len(t, result, 2, "same version across different platforms must both be retained")
}

func TestSortAndDedupeLatest_EmptyInput(t *testing.T) {
	result := SortAndDedupeLatest(nil, false)
	assert.Empty(t, result)

	result = SortAndDedupeLatest([]CachedPlugin{}, true)
	assert.Empty(t, result)
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

// registryHashA/B are valid 16-hex registry-hash directory names used to
// exercise the nested (catalog-installed) cache layout.
const (
	registryHashA = "0123456789abcdef"
	registryHashB = "fedcba9876543210"
)

func TestCache_ResolveVersion_FlatLayout(t *testing.T) {
	cache := NewCache(t.TempDir())
	name, version, platform := "flat-plugin", "1.2.3", "linux/amd64"

	want, err := cache.Put(name, version, platform, []byte("flat-binary"))
	require.NoError(t, err)

	got, ok, err := cache.ResolveVersion(name, version, platform)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, want, got)
}

// TestCache_ResolveVersion_NestedLayout is the core #598 regression: a
// catalog-installed plugin lives under <name>/<registryHash>/<version>/... and
// must resolve even though the caller does not supply the registry hash. The
// old Get(name, version, platform, "") lookup missed this.
func TestCache_ResolveVersion_NestedLayout(t *testing.T) {
	cache := NewCache(t.TempDir())
	name, version, platform := "catalog-plugin", "2.0.0", "linux/amd64"

	want, err := cache.Put(name, version, platform, []byte("nested-binary"), WithRegistryHash(registryHashA))
	require.NoError(t, err)

	// The flat lookup must miss it (proves the layout difference).
	_, ok := cache.Get(name, version, platform, "")
	assert.False(t, ok, "flat Get should not find a nested plugin")

	// ResolveVersion must find it across the registry-hash layout.
	got, ok, err := cache.ResolveVersion(name, version, platform)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, want, got)
}

func TestCache_ResolveVersion_Missing(t *testing.T) {
	cache := NewCache(t.TempDir())

	got, ok, err := cache.ResolveVersion("nope", "9.9.9", "linux/amd64")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, got)
}

// TestCache_ResolveVersion_HexLookingVersion guards against misclassifying a
// flat cache entry whose version happens to look like a registry hash (16
// lowercase hex). Such a version must still resolve from the flat layout
// rather than being treated as a registry-hash directory.
func TestCache_ResolveVersion_HexLookingVersion(t *testing.T) {
	cache := NewCache(t.TempDir())
	name, platform := "hex-plugin", "linux/amd64"
	version := "0123456789abcdef" // 16 lowercase hex, matches registryHashPattern

	want, err := cache.Put(name, version, platform, []byte("hex-version-binary"))
	require.NoError(t, err)

	got, ok, err := cache.ResolveVersion(name, version, platform)
	require.NoError(t, err)
	assert.True(t, ok, "flat entry with a hash-looking version must resolve")
	assert.Equal(t, want, got)
}

func TestCache_ResolveVersion_WrongPlatform(t *testing.T) {
	cache := NewCache(t.TempDir())
	name, version := "plat-plugin", "1.0.0"

	_, err := cache.Put(name, version, "linux/amd64", []byte("bin"), WithRegistryHash(registryHashA))
	require.NoError(t, err)

	_, ok, err := cache.ResolveVersion(name, version, "darwin/arm64")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestCache_ResolveVersion_WindowsTargetOnNonWindowsHost verifies that a
// windows/amd64 cache entry resolves on a non-Windows host even though its
// binary lacks the Unix executable bit — the exec-bit check must key off the
// target platform, not the host GOOS.
func TestCache_ResolveVersion_WindowsTargetOnNonWindowsHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("exercises non-Windows host handling of a windows target platform")
	}
	cache := NewCache(t.TempDir())
	name, version, platform := "win-plugin", "1.0.0", "windows/amd64"

	want, err := cache.Put(name, version, platform, []byte("MZ fake-windows-binary"))
	require.NoError(t, err)

	// Strip the executable bit to mimic a Windows binary stored on a Unix host.
	require.NoError(t, os.Chmod(want, 0o644))

	got, ok, err := cache.ResolveVersion(name, version, platform)
	require.NoError(t, err)
	assert.True(t, ok, "windows target binary must resolve despite missing Unix exec bit")
	assert.Equal(t, want, got)
}

func TestCache_ResolveVersion_DuplicateIdenticalAcrossLayouts(t *testing.T) {
	cache := NewCache(t.TempDir())
	name, version, platform := "dup-plugin", "1.0.0", "linux/amd64"
	data := []byte("identical-binary")

	_, err := cache.Put(name, version, platform, data)
	require.NoError(t, err)
	_, err = cache.Put(name, version, platform, data, WithRegistryHash(registryHashA))
	require.NoError(t, err)

	// Same content in two layouts is unambiguous — resolves without error.
	got, ok, err := cache.ResolveVersion(name, version, platform)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.NotEmpty(t, got)
}

func TestCache_ResolveVersion_AmbiguousDifferingContent(t *testing.T) {
	cache := NewCache(t.TempDir())
	name, version, platform := "ambiguous-plugin", "1.0.0", "linux/amd64"

	_, err := cache.Put(name, version, platform, []byte("binary-from-registry-a"), WithRegistryHash(registryHashA))
	require.NoError(t, err)
	_, err = cache.Put(name, version, platform, []byte("binary-from-registry-b"), WithRegistryHash(registryHashB))
	require.NoError(t, err)

	// Two registries hold the same version with different content — ambiguous.
	got, ok, err := cache.ResolveVersion(name, version, platform)
	require.Error(t, err)
	assert.False(t, ok)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "differing binaries")
	// The message must point at an action the user can actually take.
	assert.Contains(t, err.Error(), "remove the duplicate cache entries")
}
