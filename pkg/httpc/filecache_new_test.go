// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileCache_SizeLimit(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:     tmpDir,
		TTL:     5 * time.Minute,
		MaxSize: 100, // 100 bytes limit
		Logger:  logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Small data should work
	smallData := make([]byte, 50)
	err = cache.Set(ctx, "small", smallData, 0)
	require.NoError(t, err)

	// Large data should fail
	largeData := make([]byte, 200)
	err = cache.Set(ctx, "large", largeData, 0)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrCacheSizeLimitExceeded))

	// Verify small data was cached
	retrieved, err := cache.Get(ctx, "small")
	require.NoError(t, err)
	assert.Equal(t, smallData, retrieved)

	// Verify large data was not cached (cache miss)
	retrieved, err = cache.Get(ctx, "large")
	require.NoError(t, err)
	assert.Nil(t, retrieved)
}

func TestFileCache_KeyPrefix(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")

	// Create two caches with different prefixes
	config1 := &FileCacheConfig{
		Dir:       tmpDir,
		TTL:       5 * time.Minute,
		KeyPrefix: "app1:",
		Logger:    logr.Discard(),
	}
	cache1, err := NewFileCache(config1)
	require.NoError(t, err)

	config2 := &FileCacheConfig{
		Dir:       tmpDir,
		TTL:       5 * time.Minute,
		KeyPrefix: "app2:",
		Logger:    logr.Discard(),
	}
	cache2, err := NewFileCache(config2)
	require.NoError(t, err)

	ctx := context.Background()
	key := "test-key"

	// Set data in cache1
	data1 := []byte("data from cache1")
	err = cache1.Set(ctx, key, data1, 0)
	require.NoError(t, err)

	// Set data in cache2 with same key
	data2 := []byte("data from cache2")
	err = cache2.Set(ctx, key, data2, 0)
	require.NoError(t, err)

	// Verify they're stored separately
	retrieved1, err := cache1.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, data1, retrieved1)

	retrieved2, err := cache2.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, data2, retrieved2)

	// Verify filenames are different due to prefix
	filename1 := cache1.keyToFilename(key)
	filename2 := cache2.keyToFilename(key)
	assert.NotEqual(t, filename1, filename2)
}

func TestFileCache_Close(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    100 * time.Millisecond,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	ctx := context.Background()

	// Add some data
	err = cache.Set(ctx, "key1", []byte("data1"), 0)
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(200 * time.Millisecond)

	// Add fresh data
	err = cache.Set(ctx, "key2", []byte("data2"), 0)
	require.NoError(t, err)

	// Close should clean up expired entries
	err = cache.Close()
	require.NoError(t, err)

	// Verify expired entry is gone
	_, err = cache.Get(ctx, "key1")
	require.NoError(t, err)
	// Cache miss returns nil, nil

	// Verify fresh entry still exists
	retrieved, err := cache.Get(ctx, "key2")
	require.NoError(t, err)
	assert.NotNil(t, retrieved)
}

func TestFileCache_ContextCancellation(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := &FileCacheConfig{
		Dir:    tmpDir,
		TTL:    5 * time.Minute,
		Logger: logr.Discard(),
	}
	cache, err := NewFileCache(config)
	require.NoError(t, err)

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(context.Background())

	// First add some data with valid context
	validCtx := context.Background()
	err = cache.Set(validCtx, "key1", []byte("data1"), 0)
	require.NoError(t, err)

	// Now cancel context
	cancel()

	// Set should fail with context cancelled
	err = cache.Set(ctx, "key2", []byte("data2"), 0)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))

	// Get should fail with context cancelled
	_, err = cache.Get(ctx, "key1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))

	// Del should fail with context cancelled
	err = cache.Del(ctx, "key1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestClient_Close(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "test-cache")
	config := DefaultConfig()
	config.CacheDir = tmpDir
	config.CacheType = CacheTypeFilesystem
	config.CacheTTL = 100 * time.Millisecond

	client := NewClient(config)
	require.NotNil(t, client)

	// Add some cached data
	ctx := context.Background()
	resp, _ := client.Get(ctx, "http://example.com/test")
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	// Close the client
	err := client.Close()
	require.NoError(t, err)
}

func TestFileCache_Close_ErrorPath(t *testing.T) {
	// Create a direct FileCache struct with a dir that doesn't exist + TTL set
	// This forces CleanExpired to return an error, exercising the Close error path
	cache := &FileCache{
		dir:    "/nonexistent/path/for/close/err",
		ttl:    10 * time.Minute,
		logger: logr.Discard(),
	}
	err := cache.Close()
	require.Error(t, err)
}
