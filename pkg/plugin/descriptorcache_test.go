// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescriptorCache_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	cache := NewDescriptorCache(dir, 24*time.Hour)

	ver, _ := semver.NewVersion("2.0.0")
	desc := provider.Descriptor{
		Name:        "test-provider",
		DisplayName: "Test Provider",
		Description: "A test provider for unit tests",
		Version:     ver,
	}

	err := cache.Put("test-provider", desc)
	require.NoError(t, err)

	got := cache.Get("test-provider")
	require.NotNil(t, got)
	assert.Equal(t, "test-provider", got.Name)
	assert.Equal(t, "Test Provider", got.DisplayName)
	assert.Equal(t, "A test provider for unit tests", got.Description)
}

func TestDescriptorCache_GetMissing(t *testing.T) {
	dir := t.TempDir()
	cache := NewDescriptorCache(dir, 24*time.Hour)

	got := cache.Get("nonexistent")
	assert.Nil(t, got)
}

func TestDescriptorCache_TTLExpiry(t *testing.T) {
	dir := t.TempDir()
	// Use a very short TTL
	cache := NewDescriptorCache(dir, 1*time.Millisecond)

	ver, _ := semver.NewVersion("1.0.0")
	desc := provider.Descriptor{Name: "ephemeral", Version: ver}
	err := cache.Put("ephemeral", desc)
	require.NoError(t, err)

	// Wait for TTL to expire
	time.Sleep(5 * time.Millisecond)

	got := cache.Get("ephemeral")
	assert.Nil(t, got, "expected expired entry to return nil")
}

func TestDescriptorCache_ZeroTTL_NeverExpires(t *testing.T) {
	dir := t.TempDir()
	cache := NewDescriptorCache(dir, 0)

	ver, _ := semver.NewVersion("1.0.0")
	desc := provider.Descriptor{Name: "persistent", Version: ver}
	err := cache.Put("persistent", desc)
	require.NoError(t, err)

	got := cache.Get("persistent")
	require.NotNil(t, got)
	assert.Equal(t, "persistent", got.Name)
}

func TestDescriptorCache_Invalidate(t *testing.T) {
	dir := t.TempDir()
	cache := NewDescriptorCache(dir, 24*time.Hour)

	ver, _ := semver.NewVersion("1.0.0")
	desc := provider.Descriptor{Name: "doomed", Version: ver}
	err := cache.Put("doomed", desc)
	require.NoError(t, err)

	cache.Invalidate("doomed")

	got := cache.Get("doomed")
	assert.Nil(t, got, "expected invalidated entry to return nil")
}

func TestDescriptorCache_InvalidateAll(t *testing.T) {
	dir := t.TempDir()
	cache := NewDescriptorCache(dir, 24*time.Hour)

	ver, _ := semver.NewVersion("1.0.0")
	_ = cache.Put("a", provider.Descriptor{Name: "a", Version: ver})
	_ = cache.Put("b", provider.Descriptor{Name: "b", Version: ver})

	cache.InvalidateAll()

	assert.Nil(t, cache.Get("a"))
	assert.Nil(t, cache.Get("b"))
}

func TestDescriptorCache_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	cache := NewDescriptorCache(dir, 24*time.Hour)

	// Write garbage
	err := os.MkdirAll(dir, 0o755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("not json"), 0o644)
	require.NoError(t, err)

	got := cache.Get("corrupt")
	assert.Nil(t, got, "expected corrupt entry to return nil")
}

func TestDescriptorCache_DefaultDir(t *testing.T) {
	cache := NewDescriptorCache("", 24*time.Hour)
	assert.NotEmpty(t, cache.Dir())
	assert.Contains(t, cache.Dir(), "provider-schemas")
}

func TestDescriptorCache_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	cache := NewDescriptorCache(dir, 24*time.Hour)

	// Names with path separators should be rejected
	tests := []string{
		"../escape",
		"foo/bar",
		"foo\\bar",
		"..",
		"",
	}
	for _, name := range tests {
		err := cache.Put(name, provider.Descriptor{Name: name})
		assert.Error(t, err, "expected error for name %q", name)
		got := cache.Get(name)
		assert.Nil(t, got, "expected nil for name %q", name)
	}
}

func TestDescriptorCache_EmptyNameReturnsNil(t *testing.T) {
	dir := t.TempDir()
	cache := NewDescriptorCache(dir, 24*time.Hour)

	// Manually write a cache entry with empty Name
	entry := `{"cachedAt":"2099-01-01T00:00:00Z","descriptor":{"name":""}}`
	err := os.WriteFile(filepath.Join(dir, "empty-name.json"), []byte(entry), 0o600)
	require.NoError(t, err)

	// Get should reject it because Name is empty
	got := cache.Get("empty-name")
	assert.Nil(t, got, "expected nil for entry with empty descriptor name")
}
