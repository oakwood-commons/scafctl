package httpc

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileCache(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")

	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)
	require.NotNil(t, cache)

	// Verify directory was created
	info, err := os.Stat(tmpDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestNewFileCache_HomeDirExpansion(t *testing.T) {
	// This test verifies that tilde expansion works
	// We can't easily mock os.UserHomeDir(), so we just verify it expands correctly
	config := &FileCacheConfig{
		Dir:    "~/test-cache-scafctl",
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)
	require.NotNil(t, cache)
	defer os.RemoveAll(cache.dir) // Clean up

	// Get the actual home directory for comparison
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	expectedDir := filepath.Join(homeDir, "test-cache-scafctl")
	assert.Equal(t, expectedDir, cache.dir)

	// Verify directory was created
	info, err := os.Stat(expectedDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFileCache_SetAndGet(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
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
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Try to get non-existent key - should return nil, nil (cache miss)
	data, err := cache.Get(ctx, "non-existent-key")
	require.NoError(t, err)
	// Cache miss returns nil, nil
	assert.Nil(t, data)
}

func TestFileCache_Del(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
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

	// Verify it's gone - should return nil, nil (cache miss)
	retrieved, err := cache.Get(ctx, key)
	require.NoError(t, err)
	// Cache miss returns nil, nil
	assert.Nil(t, retrieved)
}

func TestFileCache_Expiration(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    100 * time.Millisecond,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()
	key := "test-key"
	data := []byte("test data")

	// Set data
	err = cache.Set(ctx, key, data, 0)
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(200 * time.Millisecond)

	// Try to get expired data - should return nil, nil (cache miss)
	retrieved, err := cache.Get(ctx, key)
	require.NoError(t, err)
	// Cache miss returns nil, nil
	assert.Nil(t, retrieved)
}

func TestFileCache_Clear(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
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

	// Verify all entries are gone - should return nil, nil
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		retrieved, err := cache.Get(ctx, key)
		require.NoError(t, err)
		// Cache miss returns nil, nil
		assert.Nil(t, retrieved)
	}
}

func TestFileCache_CleanExpired(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    100 * time.Millisecond,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
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
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
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

func TestFileCache_Stats(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Initial stats should be zero
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(0), hits)
	assert.Equal(t, uint64(0), misses)

	// Set some data
	err = cache.Set(ctx, "key1", []byte("data1"), 0)
	require.NoError(t, err)

	// Get existing key (should increment hits)
	data, err := cache.Get(ctx, "key1")
	require.NoError(t, err)
	assert.NotNil(t, data)

	hits, misses = cache.Stats()
	assert.Equal(t, uint64(1), hits)
	assert.Equal(t, uint64(0), misses)

	// Get non-existent key (should increment misses)
	data, err = cache.Get(ctx, "non-existent")
	require.NoError(t, err)
	// Cache miss returns nil, nil
	assert.Nil(t, data)

	hits, misses = cache.Stats()
	assert.Equal(t, uint64(1), hits)
	assert.Equal(t, uint64(1), misses)

	// Multiple hits
	for i := 0; i < 5; i++ {
		data, err = cache.Get(ctx, "key1")
		require.NoError(t, err)
		assert.NotNil(t, data)
	}

	hits, misses = cache.Stats()
	assert.Equal(t, uint64(6), hits)
	assert.Equal(t, uint64(1), misses)

	// Multiple misses
	for i := 0; i < 3; i++ {
		key := "miss-" + string(rune('a'+i))
		data, err = cache.Get(ctx, key)
		require.NoError(t, err)
		// Cache miss returns nil, nil
		assert.Nil(t, data)
	}

	hits, misses = cache.Stats()
	assert.Equal(t, uint64(6), hits)
	assert.Equal(t, uint64(4), misses)
}

func TestFileCache_StatsWithExpiration(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    100 * time.Millisecond,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Set data
	err = cache.Set(ctx, "key1", []byte("data1"), 0)
	require.NoError(t, err)

	// Get before expiration (hit)
	data, err := cache.Get(ctx, "key1")
	require.NoError(t, err)
	assert.NotNil(t, data)

	// Wait for expiration
	time.Sleep(200 * time.Millisecond)

	// Get after expiration (miss)
	data, err = cache.Get(ctx, "key1")
	require.NoError(t, err)
	// Cache miss returns nil, nil
	assert.Nil(t, data)

	// Stats should show 1 hit and 1 miss
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(1), hits)
	assert.Equal(t, uint64(1), misses)
}

func TestFileCache_ConcurrentStats(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Set some data
	err = cache.Set(ctx, "key1", []byte("data1"), 0)
	require.NoError(t, err)

	// Concurrent reads (all hits)
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			data, err := cache.Get(ctx, "key1")
			assert.NoError(t, err)
			assert.NotNil(t, data)
			done <- true
		}()
	}

	// Concurrent misses
	for i := 0; i < 5; i++ {
		go func(idx int) {
			key := "miss-" + string(rune('a'+idx))
			data, err := cache.Get(ctx, key)
			assert.NoError(t, err)
			// Cache miss returns nil, nil
			assert.Nil(t, data)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 15; i++ {
		<-done
	}

	// Stats should be atomic (10 hits, 5 misses)
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(10), hits)
	assert.Equal(t, uint64(5), misses)
}

func TestFileCache_ConcurrentWriteRead(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:       tmpDir,
		TTL:       5 * time.Minute,
		MaxSize:   1024 * 1024, // 1MB
		Logger:    logr.Discard(),
		KeyPrefix: "test:",
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()
	numGoroutines := 20
	numOperations := 50

	done := make(chan bool, numGoroutines)

	// Concurrent writes and reads with different keys
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				key := filepath.Join("key", string(rune('a'+(id+j)%26)))
				data := []byte{byte('A' + (id+j)%26)}

				// Alternate between Set and Get
				if j%2 == 0 {
					_ = cache.Set(ctx, key, data, 0)
				} else {
					_, _ = cache.Get(ctx, key)
				}
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify stats were updated (no panics or race conditions)
	hits, misses := cache.Stats()
	assert.True(t, hits+misses > 0, "Stats should be updated")
}

func TestFileCache_ConcurrentSetSameKey(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:       tmpDir,
		TTL:       5 * time.Minute,
		MaxSize:   1024 * 1024,
		Logger:    logr.Discard(),
		KeyPrefix: "test:",
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()
	key := "shared-key"
	numGoroutines := 30

	done := make(chan bool, numGoroutines)

	// Multiple goroutines writing to same key
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			data := []byte{byte('A' + id%26)}
			_ = cache.Set(ctx, key, data, 0)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify final value exists and is valid
	data, err := cache.Get(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, data)
	require.Len(t, data, 1)
	assert.True(t, data[0] >= 'A' && data[0] <= 'Z')
}

func TestFileCache_ConcurrentDeleteAndRead(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:       tmpDir,
		TTL:       5 * time.Minute,
		Logger:    logr.Discard(),
		KeyPrefix: "test:",
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Pre-populate cache with multiple keys
	for i := 0; i < 20; i++ {
		key := filepath.Join("key", string(rune('a'+i)))
		data := []byte{byte('A' + i)}
		_ = cache.Set(ctx, key, data, 0)
	}

	numGoroutines := 40
	done := make(chan bool, numGoroutines)

	// Half delete, half read
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			key := filepath.Join("key", string(rune('a'+(id%20))))
			if id%2 == 0 {
				_ = cache.Del(ctx, key)
			} else {
				_, _ = cache.Get(ctx, key)
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Should complete without panics or race conditions
	hits, misses := cache.Stats()
	assert.True(t, hits+misses >= 20, "Stats should reflect read operations")
}
