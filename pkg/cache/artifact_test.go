// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache_test

import (
	"os"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactCache_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	bundle := []byte("bundle data")

	err := c.Put("solution", "my-solution", "1.0.0", "sha256:abc123", content, bundle)
	require.NoError(t, err)

	got, gotBundle, ok, err := c.Get("solution", "my-solution", "1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, content, got)
	assert.Equal(t, bundle, gotBundle)
}

func TestArtifactCache_GetMiss(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	got, gotBundle, ok, err := c.Get("solution", "missing", "1.0.0")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Nil(t, got)
	assert.Nil(t, gotBundle)
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

func TestArtifactCache_NoBundle(t *testing.T) {
	dir := t.TempDir()
	c := cache.NewArtifactCache(dir, time.Hour)

	content := []byte("solution: {}")
	err := c.Put("solution", "my-solution", "1.0.0", "sha256:abc123", content, nil)
	require.NoError(t, err)

	got, gotBundle, ok, err := c.Get("solution", "my-solution", "1.0.0")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, content, got)
	assert.Nil(t, gotBundle)
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
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = c.Put("solution", "bench-solution", "1.0.0", "sha256:bench", content, nil)
		_, _, _, _ = c.Get("solution", "bench-solution", "1.0.0")
	}
}
