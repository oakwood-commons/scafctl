// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package env

import (
	"sync"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobalCache_Initialization(t *testing.T) {
	// Reset global state before test
	resetGlobalState()

	// GlobalCache should initialize on first call
	cache := GlobalCache()
	require.NotNil(t, cache, "GlobalCache should return a valid cache")
}

func TestGlobalCache_SingletonBehavior(t *testing.T) {
	// Reset global state before test
	resetGlobalState()

	// Call GlobalCache multiple times
	cache1 := GlobalCache()
	cache2 := GlobalCache()

	// Should return the exact same instance (pointer equality)
	assert.Same(t, cache1, cache2, "GlobalCache() should return the same cache instance")
}

func TestGlobalCache_ConcurrentAccess(t *testing.T) {
	// Reset global state before test
	resetGlobalState()

	const goroutines = 100
	var wg sync.WaitGroup
	cachePtrs := make([]*celexp.ProgramCache, goroutines)

	// Launch multiple goroutines calling GlobalCache concurrently
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cachePtrs[idx] = GlobalCache()
		}(i)
	}

	wg.Wait()

	// All caches should be the same instance
	firstCache := cachePtrs[0]
	require.NotNil(t, firstCache)
	for i, cache := range cachePtrs {
		assert.Same(t, firstCache, cache, "Cache at index %d should be the same instance", i)
	}
}

func BenchmarkGlobalCache_FirstCall(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Reset global state for each iteration to measure initialization time
		resetGlobalState()
		_ = GlobalCache()
	}
}

func BenchmarkGlobalCache_SubsequentCalls(b *testing.B) {
	// Initialize once before benchmark
	resetGlobalState()
	_ = GlobalCache()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = GlobalCache()
	}
}

func BenchmarkGlobalCache_ConcurrentAccess(b *testing.B) {
	// Initialize once before benchmark
	resetGlobalState()
	_ = GlobalCache()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = GlobalCache()
		}
	})
}

// resetGlobalState resets the global state for testing.
// WARNING: This is only safe in tests and should never be used in production code.
func resetGlobalState() {
	globalCache = nil
	globalCacheOnce = sync.Once{}
}
