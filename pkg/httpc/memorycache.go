// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/metrics"
	"ivan.dev/httpcache"
)

// metricsMemoryCache wraps an httpcache.Cache to track hits and misses.
//
// Thread-Safety: metricsMemoryCache is safe for concurrent use by multiple goroutines.
// The underlying base cache (httpcache.MemoryCache) is thread-safe, and hit/miss
// statistics are tracked using atomic operations. All methods can be called concurrently
// without additional synchronization.
type metricsMemoryCache struct {
	base   httpcache.Cache
	hits   uint64
	misses uint64
}

// newMetricsMemoryCache creates a new memory cache wrapper with metrics tracking
func newMetricsMemoryCache(base httpcache.Cache) *metricsMemoryCache {
	return &metricsMemoryCache{
		base: base,
	}
}

// Set stores data in the cache with the given key
func (m *metricsMemoryCache) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	return m.base.Set(ctx, key, data, ttl)
}

// Get retrieves data from the cache for the given key
// Returns (nil, nil) for cache misses - this is not an error, it's standard cache behavior
func (m *metricsMemoryCache) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := m.base.Get(ctx, key)

	// Track cache hit/miss based on data and error
	// Cache miss cases:
	// 1. Error returned (cache library returns error for miss)
	// 2. Nil data returned (cache library returns nil for miss)
	// 3. Empty byte slice (edge case, treated as miss)
	if err != nil || data == nil || len(data) == 0 {
		// Cache miss
		atomic.AddUint64(&m.misses, 1)
		if metrics.HTTPClientCacheMisses != nil {
			metrics.HTTPClientCacheMisses.Add(ctx, 1)
		}
		// Return nil, nil for httpcache compatibility (ignore base cache error)
		return nil, nil //nolint:nilerr // httpcache expects (nil, nil) for cache misses
	}

	// Cache hit
	atomic.AddUint64(&m.hits, 1)
	if metrics.HTTPClientCacheHits != nil {
		metrics.HTTPClientCacheHits.Add(ctx, 1)
	}

	return data, nil
}

// Del removes data from the cache for the given key
func (m *metricsMemoryCache) Del(ctx context.Context, key string) error {
	return m.base.Del(ctx, key)
}

// Stats returns the cache hit and miss statistics
func (m *metricsMemoryCache) Stats() (hits, misses uint64) {
	return atomic.LoadUint64(&m.hits), atomic.LoadUint64(&m.misses)
}
