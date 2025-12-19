package celexp

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"

	"github.com/google/cel-go/cel"
)

// ProgramCache is a thread-safe LRU cache for compiled CEL programs.
// It caches programs by a hash of the expression and environment options,
// allowing reuse of expensive compilation operations.
type ProgramCache struct {
	mu        sync.RWMutex
	cache     map[string]*cacheEntry
	lru       *list.List
	maxSize   int
	hits      uint64
	misses    uint64
	evictions uint64
}

// cacheEntry represents a cached program with its LRU element
type cacheEntry struct {
	program cel.Program
	element *list.Element
}

// cacheKey is used for LRU tracking
type cacheKey struct {
	key string
}

// NewProgramCache creates a new program cache with the specified maximum size.
// When the cache reaches maxSize, the least recently used entry will be evicted.
// A maxSize of 0 or negative value defaults to 100.
func NewProgramCache(maxSize int) *ProgramCache {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &ProgramCache{
		cache:   make(map[string]*cacheEntry),
		lru:     list.New(),
		maxSize: maxSize,
	}
}

// Get retrieves a program from the cache if it exists.
// Returns the program and true if found, nil and false otherwise.
func (c *ProgramCache) Get(key string) (cel.Program, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, found := c.cache[key]
	if !found {
		c.misses++
		return nil, false
	}

	// Move to front (most recently used)
	c.lru.MoveToFront(entry.element)
	c.hits++
	return entry.program, true
}

// Put adds a program to the cache. If the cache is full, it evicts
// the least recently used entry before adding the new one.
func (c *ProgramCache) Put(key string, program cel.Program) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if entry, found := c.cache[key]; found {
		entry.program = program
		c.lru.MoveToFront(entry.element)
		return
	}

	// Evict if necessary
	if len(c.cache) >= c.maxSize {
		c.evictOldest()
	}

	// Add new entry
	element := c.lru.PushFront(cacheKey{key: key})
	c.cache[key] = &cacheEntry{
		program: program,
		element: element,
	}
}

// evictOldest removes the least recently used entry from the cache.
// Must be called with the lock held.
func (c *ProgramCache) evictOldest() {
	element := c.lru.Back()
	if element != nil {
		c.lru.Remove(element)
		if ck, ok := element.Value.(cacheKey); ok {
			delete(c.cache, ck.key)
			c.evictions++
		}
	}
}

// Clear removes all entries from the cache.
func (c *ProgramCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*cacheEntry)
	c.lru = list.New()
	c.hits = 0
	c.misses = 0
	c.evictions = 0
}

// Stats returns cache statistics.
func (c *ProgramCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Size:      len(c.cache),
		MaxSize:   c.maxSize,
		Hits:      c.hits,
		Misses:    c.misses,
		Evictions: c.evictions,
		HitRate:   c.hitRate(),
	}
}

// hitRate calculates the cache hit rate as a percentage.
// Must be called with the lock held.
func (c *ProgramCache) hitRate() float64 {
	total := c.hits + c.misses
	if total == 0 {
		return 0.0
	}
	return float64(c.hits) / float64(total) * 100.0
}

// CacheStats contains cache performance statistics.
type CacheStats struct {
	Size      int     `json:"size"`
	MaxSize   int     `json:"max_size"`
	Hits      uint64  `json:"hits"`
	Misses    uint64  `json:"misses"`
	Evictions uint64  `json:"evictions"`
	HitRate   float64 `json:"hit_rate"` // Percentage
}

// generateCacheKey creates a unique cache key from an expression and environment options.
// The key is a SHA-256 hash of the expression and the string representation of options.
func generateCacheKey(expression string, opts []cel.EnvOption) string {
	h := sha256.New()

	// Hash the expression
	h.Write([]byte(expression))

	// Hash the options (convert to string representation and sort for consistency)
	optStrings := make([]string, len(opts))
	for i, opt := range opts {
		// Use the address of the option as a unique identifier
		// This works because cel.EnvOption functions are typically created once
		optStrings[i] = fmt.Sprintf("%p", opt)
	}
	sort.Strings(optStrings) // Sort for consistent ordering

	for _, s := range optStrings {
		h.Write([]byte(s))
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// CompileWithCache compiles a CEL expression using the provided cache.
// If the program is already cached, it returns the cached version.
// Otherwise, it compiles the expression and caches the result.
//
// This function is thread-safe and can be called concurrently.
//
// Example usage:
//
//	cache := celexp.NewProgramCache(100)
//	expr := celexp.CelExpression("x + y")
//	result, err := celexp.CompileWithCache(cache, expr,
//	    cel.Variable("x", cel.IntType),
//	    cel.Variable("y", cel.IntType))
//	if err != nil {
//	    return err
//	}
//	value, err := result.Eval(map[string]any{"x": 10, "y": 20})
func CompileWithCache(cache *ProgramCache, expression Expression, opts ...cel.EnvOption) (*CompileResult, error) {
	if cache == nil {
		return nil, fmt.Errorf("cache cannot be nil")
	}

	// Generate cache key
	key := generateCacheKey(string(expression), opts)

	// Try to get from cache
	if prog, found := cache.Get(key); found {
		return &CompileResult{
			Program:    prog,
			Expression: expression,
		}, nil
	}

	// Cache miss - compile the program
	result, err := expression.Compile(opts...)
	if err != nil {
		return nil, err
	}

	// Store in cache
	cache.Put(key, result.Program)

	return result, nil
}
