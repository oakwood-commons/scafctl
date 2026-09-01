// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog/mediatypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Realistic media-type layer keys exercise sanitization of '/', '.', and '+'.
const (
	mtBundle = mediatypes.SolutionBundle
	mtLock   = mediatypes.SolutionLock
)

func TestArtifactCache_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	bundle := []byte("bundle data")

	err := c.Put("solution", "my-solution", "1.0.0", "sha256:abc123", content, map[string][]byte{
		mtBundle: bundle,
	})
	require.NoError(t, err)

	got, layers, ok, err := c.Get("solution", "my-solution", "1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, content, got)
	assert.Equal(t, bundle, layers[mtBundle])
}

func TestArtifactCache_PutAndGet_LockLayer(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	lock := []byte(`{"version":1}`)

	err := c.Put("solution", "locked-solution", "1.0.0", "sha256:lock", content, map[string][]byte{
		mtLock: lock,
	})
	require.NoError(t, err)

	got, layers, ok, err := c.Get("solution", "locked-solution", "1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, content, got)
	assert.Equal(t, lock, layers[mtLock])
	assert.NotContains(t, layers, mtBundle)
}

func TestArtifactCache_PutAndGet_BundleAndLock(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	bundle := []byte("bundle data")
	lock := []byte(`{"version":1}`)

	err := c.Put("solution", "full-solution", "1.0.0", "sha256:full", content, map[string][]byte{
		mtBundle: bundle,
		mtLock:   lock,
	})
	require.NoError(t, err)

	got, layers, ok, err := c.Get("solution", "full-solution", "1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, content, got)
	assert.Len(t, layers, 2)
	assert.Equal(t, bundle, layers[mtBundle])
	assert.Equal(t, lock, layers[mtLock])
}

func TestArtifactCache_Put_EmptyLayerSkipped(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	// A present key with empty bytes must not create a layer file.
	err := c.Put("solution", "empty-layer", "1.0.0", "sha256:e", content, map[string][]byte{
		mtBundle: {},
		mtLock:   []byte("lock"),
	})
	require.NoError(t, err)

	_, layers, ok, err := c.Get("solution", "empty-layer", "1.0.0")
	require.NoError(t, err)
	require.True(t, ok)
	assert.NotContains(t, layers, mtBundle)
	assert.Equal(t, []byte("lock"), layers[mtLock])
}

func TestArtifactCache_Put_RemovesStaleLayers(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")

	// First put with both bundle and lock.
	require.NoError(t, c.Put("solution", "shrinking", "1.0.0", "sha256:1", content, map[string][]byte{
		mtBundle: []byte("bundle"),
		mtLock:   []byte("lock"),
	}))

	// Re-put with only content (no layers) must clear the previously stored layers.
	require.NoError(t, c.Put("solution", "shrinking", "1.0.0", "sha256:2", content, nil))

	_, layers, ok, err := c.Get("solution", "shrinking", "1.0.0")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, layers)
}

func TestArtifactCache_GetMiss(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	got, layers, ok, err := c.Get("solution", "missing", "1.0.0")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, got)
	assert.Nil(t, layers)
}

func TestArtifactCache_TTLExpiry(t *testing.T) {
	dir := t.TempDir()
	// Use a 1ms TTL so the entry expires almost immediately
	c := cache.NewArtifactCache(dir, 1*time.Millisecond)

	content := []byte("solution: {}")
	err := c.Put("solution", "my-solution", "1.0.0", "sha256:abc123", content, nil)
	require.NoError(t, err)

	// Wait for the TTL to expire
	time.Sleep(10 * time.Millisecond)

	got, _, ok, err := c.Get("solution", "my-solution", "1.0.0")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, got)
}

func TestArtifactCache_ZeroTTL_NeverExpires(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, 0) // zero TTL = never expire

	content := []byte("solution: {}")
	err := c.Put("solution", "my-solution", "2.0.0", "sha256:def456", content, nil)
	require.NoError(t, err)

	got, _, ok, err := c.Get("solution", "my-solution", "2.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, content, got)
}

func TestArtifactCache_NoLayers(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	err := c.Put("solution", "my-solution", "1.0.0", "sha256:abc123", content, nil)
	require.NoError(t, err)

	got, layers, ok, err := c.Get("solution", "my-solution", "1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, content, got)
	assert.Empty(t, layers)
}

func TestArtifactCache_Invalidate(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	err := c.Put("solution", "my-solution", "1.0.0", "sha256:abc123", content, nil)
	require.NoError(t, err)

	// Verify it's there
	_, _, ok, err := c.Get("solution", "my-solution", "1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)

	// Invalidate
	err = c.Invalidate("solution", "my-solution", "1.0.0")
	require.NoError(t, err)

	// Should be gone
	_, _, ok, err = c.Get("solution", "my-solution", "1.0.0")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestArtifactCache_Invalidate_NonExistent(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	// Should not error on non-existent entry
	err := c.Invalidate("solution", "nonexistent", "1.0.0")
	assert.NoError(t, err)
}

func TestArtifactCache_CorruptMeta(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	// Store valid entry first
	content := []byte("solution: {}")
	err := c.Put("solution", "my-solution", "1.0.0", "sha256:abc123", content, nil)
	require.NoError(t, err)

	// Corrupt the meta file to trigger corrupt-meta handling
	entryDir := dir + "/solution/my-solution@1.0.0"
	err = os.WriteFile(entryDir+"/meta.json", []byte("not-valid-json"), 0o600)
	require.NoError(t, err)

	// Should be treated as a cache miss
	_, _, ok, err := c.Get("solution", "my-solution", "1.0.0")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestArtifactCache_MissingLayerFile_TreatedAsMiss(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	require.NoError(t, c.Put("solution", "torn", "1.0.0", "sha256:t", content, map[string][]byte{
		mtLock: []byte("lock"),
	}))

	// Delete the layers directory out from under the entry, leaving meta listing
	// a layer that no longer exists on disk.
	require.NoError(t, os.RemoveAll(dir+"/solution/torn@1.0.0/layers"))

	_, layers, ok, err := c.Get("solution", "torn", "1.0.0")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, layers)
}

func TestArtifactCache_SpecialCharsInName(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	// Names with special characters should be sanitized
	content := []byte("solution: {}")
	err := c.Put("solution", "my/solution:v2", "1.0.0", "sha256:abc123", content, nil)
	require.NoError(t, err)

	got, _, ok, err := c.Get("solution", "my/solution:v2", "1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, content, got)
}

func TestArtifactCache_DirAndTTL(t *testing.T) {
	dir := t.TempDir()
	ttl := 5 * time.Minute
	c := cache.NewArtifactCache(dir, ttl)

	assert.Equal(t, dir, c.Dir())
	assert.Equal(t, ttl, c.TTL())
}

func BenchmarkArtifactCache_PutGet(b *testing.B) {
	dir := b.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {name: bench}")
	layers := map[string][]byte{mtBundle: []byte("bundle")}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = c.Put("solution", "bench-solution", "1.0.0", "sha256:bench", content, layers)
		_, _, _, _ = c.Get("solution", "bench-solution", "1.0.0")
	}
}

func TestInvalidateArtifact(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	// Store an entry
	content := []byte("solution: {}")
	require.NoError(t, c.Put("solution", "my-app", "2.0.0", "sha256:def456", content, nil))

	// Verify it's there
	_, _, ok, err := c.Get("solution", "my-app", "2.0.0")
	require.NoError(t, err)
	assert.True(t, ok)

	// Invalidate via convenience function
	err = cache.InvalidateArtifact(dir, time.Hour, "solution", "my-app", "2.0.0")
	require.NoError(t, err)

	// Should be gone
	_, _, ok, err = c.Get("solution", "my-app", "2.0.0")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestInvalidateArtifact_NonExistent(t *testing.T) {
	dir := t.TempDir()

	// Should not error for non-existent entry
	err := cache.InvalidateArtifact(dir, time.Hour, "solution", "nonexistent", "1.0.0")
	assert.NoError(t, err)
}

func TestArtifactCache_Get_ContentReadError(t *testing.T) {
	// Simulate a readable meta but unreadable content file by creating a directory
	// at the path where the content file should be, causing a read error.
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, 0)

	// Put a valid entry first
	err := c.Put("solution", "error-sol", "1.0.0", "sha256:abc", []byte("content"), nil)
	require.NoError(t, err)

	// Replace content file with a directory (unreadable as file)
	entryDir := dir + "/solution/error-sol@1.0.0"
	contentPath := entryDir + "/content"
	require.NoError(t, os.Remove(contentPath))
	require.NoError(t, os.MkdirAll(contentPath, 0o700))

	_, _, ok, err := c.Get("solution", "error-sol", "1.0.0")
	// Should get an error reading the content directory as a file
	assert.Error(t, err)
	assert.False(t, ok)
}

func TestArtifactCache_Put_WithBundle(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, 0)

	content := []byte("solution content")
	bundleData := []byte("bundle data")

	err := c.Put("solution", "with-bundle", "1.0.0", "sha256:xyz", content, map[string][]byte{
		mtBundle: bundleData,
	})
	require.NoError(t, err)

	gotContent, layers, ok, err := c.Get("solution", "with-bundle", "1.0.0")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, content, gotContent)
	assert.Equal(t, bundleData, layers[mtBundle])

	// The bundle layer must be persisted at the legacy top-level bundle.tar.gz
	// path (not under layers/) for on-disk compatibility with prior versions.
	bundleOnDisk, readErr := os.ReadFile(filepath.Join(dir, "solution", "with-bundle@1.0.0", "bundle.tar.gz"))
	require.NoError(t, readErr)
	assert.Equal(t, bundleData, bundleOnDisk)
}

func TestArtifactCache_Invalidate_OsRemoveAllError(t *testing.T) {
	// Can't easily simulate os.RemoveAll error on macOS without root;
	// just verify that invalidating a non-existent entry works.
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, 0)
	err := c.Invalidate("solution", "nonexistent", "9.9.9")
	assert.NoError(t, err)
}

func BenchmarkInvalidateArtifact(b *testing.B) {
	dir := b.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	for b.Loop() {
		_ = c.Put("solution", "bench", "1.0.0", "sha256:bench", []byte("data"), nil)
		_ = cache.InvalidateArtifact(dir, time.Hour, "solution", "bench", "1.0.0")
	}
}
