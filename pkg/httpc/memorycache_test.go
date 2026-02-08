// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ivan.dev/httpcache"
)

func TestMemoryCacheWrapper_SetAndGet(t *testing.T) {
	baseCache := httpcache.MemoryCache(100, 1*time.Minute)
	cache := newMetricsMemoryCache(baseCache)

	ctx := context.Background()
	key := "test-key"
	value := []byte("test-value")

	// Set value
	err := cache.Set(ctx, key, value, 1*time.Minute)
	require.NoError(t, err)

	// Get value (should be a hit)
	data, err := cache.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, data)

	// Check stats
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(1), hits)
	assert.Equal(t, uint64(0), misses)
}

func TestMemoryCacheWrapper_Miss(t *testing.T) {
	baseCache := httpcache.MemoryCache(100, 1*time.Minute)
	cache := newMetricsMemoryCache(baseCache)

	ctx := context.Background()
	key := "non-existent-key"

	// Get non-existent value (should be a miss)
	// MemoryCache returns an error for cache misses
	data, err := cache.Get(ctx, key)
	assert.NoError(t, err)
	assert.Nil(t, data)

	// Check stats
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(0), hits)
	assert.Equal(t, uint64(1), misses)
}

func TestMemoryCacheWrapper_MultipleOperations(t *testing.T) {
	baseCache := httpcache.MemoryCache(100, 1*time.Minute)
	cache := newMetricsMemoryCache(baseCache)

	ctx := context.Background()

	// Set multiple values
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		value := []byte{byte('A' + i)}
		err := cache.Set(ctx, key, value, 1*time.Minute)
		require.NoError(t, err)
	}

	// Get all values (5 hits)
	for i := 0; i < 5; i++ {
		key := string(rune('a' + i))
		data, err := cache.Get(ctx, key)
		require.NoError(t, err)
		assert.NotNil(t, data)
	}

	// Try to get non-existent values (3 misses)
	for i := 0; i < 3; i++ {
		key := "miss-" + string(rune('a'+i))
		data, err := cache.Get(ctx, key)
		assert.NoError(t, err) // MemoryCache wrapper returns nil, nil for misses
		assert.Nil(t, data)
	}

	// Check stats
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(5), hits)
	assert.Equal(t, uint64(3), misses)
}

func TestMemoryCacheWrapper_Del(t *testing.T) {
	baseCache := httpcache.MemoryCache(100, 1*time.Minute)
	cache := newMetricsMemoryCache(baseCache)

	ctx := context.Background()
	key := "test-key"
	value := []byte("test-value")

	// Set value
	err := cache.Set(ctx, key, value, 1*time.Minute)
	require.NoError(t, err)

	// Get value (hit)
	data, err := cache.Get(ctx, key)
	require.NoError(t, err)
	assert.Equal(t, value, data)

	// Delete value
	err = cache.Del(ctx, key)
	require.NoError(t, err)

	// Try to get deleted value (miss)
	data, err = cache.Get(ctx, key)
	assert.NoError(t, err) // MemoryCache wrapper returns nil, nil for misses
	assert.Nil(t, data)

	// Check stats
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(1), hits)
	assert.Equal(t, uint64(1), misses)
}

func TestMemoryCacheWrapper_ConcurrentAccess(t *testing.T) {
	baseCache := httpcache.MemoryCache(100, 1*time.Minute)
	cache := newMetricsMemoryCache(baseCache)

	ctx := context.Background()
	key := "concurrent-key"
	value := []byte("concurrent-value")

	// Set value
	err := cache.Set(ctx, key, value, 1*time.Minute)
	require.NoError(t, err)

	// Concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() {
			data, err := cache.Get(ctx, key)
			assert.NoError(t, err)
			assert.Equal(t, value, data)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Check stats (should have 10 hits)
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(10), hits)
	assert.Equal(t, uint64(0), misses)
}

func TestMemoryCacheWrapper_ConcurrentWriteRead(t *testing.T) {
	baseCache := httpcache.MemoryCache(1000, 1*time.Minute)
	cache := newMetricsMemoryCache(baseCache)

	ctx := context.Background()
	numGoroutines := 50
	numOperations := 100

	done := make(chan bool, numGoroutines)

	// Concurrent writes and reads
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				key := string(rune('a' + (id+j)%26))
				value := []byte{byte('A' + (id+j)%26)}

				// Alternate between Set and Get
				if j%2 == 0 {
					_ = cache.Set(ctx, key, value, 1*time.Minute)
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

	// Verify stats were updated (hits + misses should equal number of Get operations)
	hits, misses := cache.Stats()
	totalGets := uint64(numGoroutines * numOperations / 2)
	assert.Equal(t, totalGets, hits+misses, "Total gets should equal hits + misses")
}

func TestMemoryCacheWrapper_ConcurrentSetSameKey(t *testing.T) {
	baseCache := httpcache.MemoryCache(100, 1*time.Minute)
	cache := newMetricsMemoryCache(baseCache)

	ctx := context.Background()
	key := "shared-key"
	numGoroutines := 20

	done := make(chan bool, numGoroutines)

	// Multiple goroutines writing to same key
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			value := []byte{byte('A' + id)}
			_ = cache.Set(ctx, key, value, 1*time.Minute)
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
	assert.True(t, data[0] >= 'A' && data[0] < 'A'+byte(numGoroutines))
}

func TestMemoryCacheWrapper_EmptyDataHandling(t *testing.T) {
	baseCache := httpcache.MemoryCache(100, 1*time.Minute)
	cache := newMetricsMemoryCache(baseCache)

	ctx := context.Background()

	// Test with empty byte slice
	err := cache.Set(ctx, "empty-key", []byte{}, 1*time.Minute)
	require.NoError(t, err)

	// Get should treat empty data as miss
	data, err := cache.Get(ctx, "empty-key")
	assert.NoError(t, err)
	assert.Nil(t, data)

	// Check stats - should be a miss
	hits, misses := cache.Stats()
	assert.Equal(t, uint64(0), hits)
	assert.Equal(t, uint64(1), misses)
}
