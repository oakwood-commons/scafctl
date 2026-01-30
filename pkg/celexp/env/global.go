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

	// globalCacheOnce ensures the global cache is initialized exactly once.
	globalCacheOnce sync.Once
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
	globalCacheOnce.Do(func() {
		globalCache = celexp.NewProgramCache(GlobalCacheSize)
	})
	return globalCache
}
