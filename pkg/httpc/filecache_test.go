package httpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileCache(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")

	cache, err := NewFileCache(tmpDir, 5*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, cache)

	// Verify directory was created
	info, err := os.Stat(tmpDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewFileCache_HomeDirExpansion(t *testing.T) {
	// Use a subdirectory in temp to avoid polluting actual home
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	defer os.Setenv("HOME", origHome)

	cache, err := NewFileCache("~/test-cache", 5*time.Minute)
	require.NoError(t, err)
	require.NotNil(t, cache)

	expectedDir := filepath.Join(tmpDir, "test-cache")
	assert.Equal(t, expectedDir, cache.dir)

	// Verify directory was created
	info, err := os.Stat(expectedDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFileCache_SetAndGet(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	cache, err := NewFileCache(tmpDir, 5*time.Minute)
	require.NoError(t, err)

	ctx := context.Background()
	key := "test-key"
	data := []byte("test data")

	// Set data
	err = cache.Set(ctx, key, data, 0)
	require.NoError(t, err)

	// Get data
	retrieved, err := cache.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

func TestFileCache_GetNonExistent(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	cache, err := NewFileCache(tmpDir, 5*time.Minute)
	require.NoError(t, err)

	ctx := context.Background()

	// Try to get non-existent key - should return nil, nil (cache miss)
	data, err := cache.Get(ctx, "non-existent-key")
	require.NoError(t, err)
	assert.Nil(t, data)
}

func TestFileCache_Del(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	cache, err := NewFileCache(tmpDir, 5*time.Minute)
	require.NoError(t, err)

	ctx := context.Background()
	key := "test-key"
	data := []byte("test data")

	// Set data
	err = cache.Set(ctx, key, data, 0)
	require.NoError(t, err)

	// Delete data
	err = cache.Del(ctx, key)
	require.NoError(t, err)

	// Verify it's gone
	retrieved, err := cache.Get(ctx, key)
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestFileCache_Expiration(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	cache, err := NewFileCache(tmpDir, 100*time.Millisecond)
	require.NoError(t, err)

	ctx := context.Background()
	key := "test-key"
	data := []byte("test data")

	// Set data
	err = cache.Set(ctx, key, data, 0)
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(200 * time.Millisecond)

	// Try to get expired data - should return nil, nil (expired)
	retrieved, err := cache.Get(ctx, key)
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestFileCache_Clear(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	cache, err := NewFileCache(tmpDir, 5*time.Minute)
	require.NoError(t, err)

	ctx := context.Background()

	// Add multiple entries
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		data := []byte("data " + key)
		err = cache.Set(ctx, key, data, 0)
		require.NoError(t, err)
	}

	// Clear cache
	err = cache.Clear()
	require.NoError(t, err)

	// Verify all entries are gone
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		retrieved, err := cache.Get(ctx, key)
		require.NoError(t, err)
		assert.Nil(t, retrieved)
	}
}

func TestFileCache_CleanExpired(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	cache, err := NewFileCache(tmpDir, 100*time.Millisecond)
	require.NoError(t, err)

	ctx := context.Background()

	// Add entries
	err = cache.Set(ctx, "key1", []byte("data1"), 0)
	require.NoError(t, err)

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Add another entry
	err = cache.Set(ctx, "key2", []byte("data2"), 0)
	require.NoError(t, err)

	// Wait for first entry to expire
	time.Sleep(100 * time.Millisecond)

	// Clean expired entries
	err = cache.CleanExpired()
	require.NoError(t, err)

	// First entry should be gone
	retrieved, err := cache.Get(ctx, "key1")
	require.NoError(t, err)
	assert.Nil(t, retrieved)

	// Second entry might still be there depending on timing
	// We won't assert on it since it's time-sensitive
}

func TestFileCache_KeyToFilename(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	cache, err := NewFileCache(tmpDir, 5*time.Minute)
	require.NoError(t, err)

	// Test that different keys produce different filenames
	filename1 := cache.keyToFilename("key1")
	filename2 := cache.keyToFilename("key2")
	assert.NotEqual(t, filename1, filename2)

	// Test that same key produces same filename
	filename1Again := cache.keyToFilename("key1")
	assert.Equal(t, filename1, filename1Again)

	// Test that filenames are in the cache directory
	assert.Contains(t, filename1, tmpDir)
}
