// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package env

import (
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
)

const (
	// GlobalCacheSize is the size of the global AST cache shared across all CEL evaluations.
	// This cache significantly improves performance by reusing compiled CEL programs.
	GlobalCacheSize = 10000
)

var (
	// globalCache is the singleton program cache shared across all CEL evaluations.
	// It caches compiled CEL programs (ASTs) for reuse across multiple evaluations.
	globalCache *celexp.ProgramCache

	// globalCacheMu protects the global cache initialization state.
	globalCacheMu sync.Mutex
	// globalCacheInitialized tracks whether the global cache has been created.
	globalCacheInitialized bool
)

// GlobalCache returns the global singleton program cache used for AST caching.
// This cache is shared across all CEL evaluations and significantly improves performance
// by reusing compiled CEL programs.
//
// The cache is initialized on first call with GlobalCacheSize entries.
// This function is thread-safe and can be called concurrently.
//
// Example usage:
//
//	cache := env.GlobalCache()
//	if cache != nil {
//	    stats := cache.Stats()
//	    fmt.Printf("Cache hit rate: %.2f%%\n", stats.HitRate)
//	}
func GlobalCache() *celexp.ProgramCache {
	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()
	if !globalCacheInitialized {
		globalCache = celexp.NewProgramCache(GlobalCacheSize)
		globalCacheInitialized = true
	}
	return globalCache
}

// resetGlobalCacheForTesting resets the global cache state for testing.
// This is safe for use in tests as it acquires the mutex before resetting.
// WARNING: This should only be called from tests.
func resetGlobalCacheForTesting() {
	globalCacheMu.Lock()
	defer globalCacheMu.Unlock()
	globalCache = nil
	globalCacheInitialized = false
}
