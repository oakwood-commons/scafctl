// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"container/list"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// templateMetric tracks access statistics for a specific template.
type templateMetric struct {
	templateName string
	hits         uint64
	lastAccess   time.Time
}

// TemplateCache is a thread-safe LRU cache for compiled Go templates.
// It caches parsed *template.Template objects by a hash of the template content
// and configuration, allowing reuse of expensive parse operations.
//
// The cache also tracks detailed template-level metrics for monitoring
// and debugging purposes.
type TemplateCache struct {
	mu        sync.RWMutex
	cache     map[string]*templateCacheEntry
	lru       *list.List
	maxSize   int
	hits      uint64
	misses    uint64
	evictions uint64

	// Template-level metrics (limited to top 1000 by default)
	tmplHits     map[string]*templateMetric
	metricsLimit int
}

// templateCacheEntry represents a cached template with its LRU element.
type templateCacheEntry struct {
	tmpl         *template.Template
	element      *list.Element
	templateName string // Store name for metrics
}

// templateCacheKey is used for LRU tracking.
type templateCacheKey struct {
	key string
}

// NewTemplateCache creates a new template cache with the specified maximum size.
// When the cache reaches maxSize, the least recently used entry will be evicted.
// A maxSize of 0 or negative value defaults to 100.
//
// Template-level metrics are tracked for the top 1000 most accessed templates
// to avoid unbounded memory growth.
func NewTemplateCache(maxSize int) *TemplateCache {
	if maxSize <= 0 {
		maxSize = 100
	}
	return &TemplateCache{
		cache:        make(map[string]*templateCacheEntry),
		lru:          list.New(),
		maxSize:      maxSize,
		tmplHits:     make(map[string]*templateMetric),
		metricsLimit: 1000,
	}
}

// Get retrieves a template from the cache if it exists.
// Returns a clone of the cached template and true if found, nil and false otherwise.
// The clone is safe for concurrent execution with different data.
func (c *TemplateCache) Get(key string) (*template.Template, bool) {
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

	// Update template metrics
	c.trackTemplateHit(entry.templateName)

	// Return a clone so callers can execute concurrently with different data
	// without affecting the cached template state.
	cloned, err := entry.tmpl.Clone()
	if err != nil {
		// Clone should not fail for correctly parsed templates,
		// but if it does, treat as a cache miss.
		c.hits-- // Undo the hit count
		c.misses++
		return nil, false
	}
	return cloned, true
}

// Put adds a template to the cache with its name for metrics tracking.
// If the cache is full, it evicts the least recently used entry before adding the new one.
func (c *TemplateCache) Put(key string, tmpl *template.Template, templateName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if entry, found := c.cache[key]; found {
		entry.tmpl = tmpl
		entry.templateName = templateName
		c.lru.MoveToFront(entry.element)
		return
	}

	// Evict if necessary
	if len(c.cache) >= c.maxSize {
		c.evictOldest()
	}

	// Add new entry
	element := c.lru.PushFront(templateCacheKey{key: key})
	c.cache[key] = &templateCacheEntry{
		tmpl:         tmpl,
		element:      element,
		templateName: templateName,
	}
}

// evictOldest removes the least recently used entry from the cache.
// Must be called with the lock held.
func (c *TemplateCache) evictOldest() {
	element := c.lru.Back()
	if element != nil {
		c.lru.Remove(element)
		if ck, ok := element.Value.(templateCacheKey); ok {
			delete(c.cache, ck.key)
			c.evictions++
		}
	}
}

// Clear removes all entries from the cache but preserves statistics.
// Use ClearWithStats() to also reset hit/miss counters.
func (c *TemplateCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*templateCacheEntry)
	c.lru = list.New()
}

// ClearWithStats removes all entries and resets all statistics to zero.
func (c *TemplateCache) ClearWithStats() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*templateCacheEntry)
	c.lru = list.New()
	c.hits = 0
	c.misses = 0
	c.evictions = 0
	c.tmplHits = make(map[string]*templateMetric)
}

// ResetStats resets cache statistics (hits, misses, evictions) without removing cached entries.
func (c *TemplateCache) ResetStats() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hits = 0
	c.misses = 0
	c.evictions = 0
	c.tmplHits = make(map[string]*templateMetric)
}

// Stats returns cache statistics.
func (c *TemplateCache) Stats() TemplateCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return TemplateCacheStats{
		Size:          len(c.cache),
		MaxSize:       c.maxSize,
		Hits:          c.hits,
		Misses:        c.misses,
		Evictions:     c.evictions,
		HitRate:       c.hitRate(),
		TotalAccesses: c.hits + c.misses,
	}
}

// GetDetailedStats returns cache statistics including template-level metrics.
// If topN is 0, returns all tracked templates. Otherwise returns the top N
// most accessed templates sorted by hit count (descending).
func (c *TemplateCache) GetDetailedStats(topN int) TemplateCacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := TemplateCacheStats{
		Size:          len(c.cache),
		MaxSize:       c.maxSize,
		Hits:          c.hits,
		Misses:        c.misses,
		Evictions:     c.evictions,
		HitRate:       c.hitRate(),
		TotalAccesses: c.hits + c.misses,
	}

	// Build template stats list
	tmplStats := make([]TemplateStat, 0, len(c.tmplHits))
	for _, metric := range c.tmplHits {
		tmplStats = append(tmplStats, TemplateStat{
			TemplateName: metric.templateName,
			Hits:         metric.hits,
			LastAccess:   metric.lastAccess,
		})
	}

	// Sort by hits (descending)
	sort.Slice(tmplStats, func(i, j int) bool {
		return tmplStats[i].Hits > tmplStats[j].Hits
	})

	// Limit to topN if specified
	if topN > 0 && topN < len(tmplStats) {
		tmplStats = tmplStats[:topN]
	}

	stats.TopTemplates = tmplStats
	return stats
}

// trackTemplateHit updates metrics for a template.
// Must be called with the write lock held.
func (c *TemplateCache) trackTemplateHit(templateName string) {
	metric, exists := c.tmplHits[templateName]
	if exists {
		metric.hits++
		metric.lastAccess = time.Now()
		return
	}

	// Check if we've hit the metrics limit
	if len(c.tmplHits) >= c.metricsLimit {
		c.evictLeastAccessedMetric()
	}

	// Add new metric
	c.tmplHits[templateName] = &templateMetric{
		templateName: templateName,
		hits:         1,
		lastAccess:   time.Now(),
	}
}

// evictLeastAccessedMetric removes the template metric with the lowest hit count.
// Must be called with the write lock held.
func (c *TemplateCache) evictLeastAccessedMetric() {
	if len(c.tmplHits) == 0 {
		return
	}

	minHits := ^uint64(0) // Max uint64
	var minName string

	for name, metric := range c.tmplHits {
		if metric.hits < minHits {
			minHits = metric.hits
			minName = name
		}
	}

	if minName != "" {
		delete(c.tmplHits, minName)
	}
}

// hitRate calculates the cache hit rate as a percentage.
// Must be called with the lock held.
func (c *TemplateCache) hitRate() float64 {
	total := c.hits + c.misses
	if total == 0 {
		return 0.0
	}
	return float64(c.hits) / float64(total) * 100.0
}

// TemplateCacheStats contains cache performance statistics.
type TemplateCacheStats struct {
	Size          int            `json:"size"`
	MaxSize       int            `json:"max_size"`
	Hits          uint64         `json:"hits"`
	Misses        uint64         `json:"misses"`
	Evictions     uint64         `json:"evictions"`
	HitRate       float64        `json:"hit_rate"` // Percentage
	TotalAccesses uint64         `json:"total_accesses"`
	TopTemplates  []TemplateStat `json:"top_templates,omitempty"`
}

// TemplateStat contains statistics for a specific template.
type TemplateStat struct {
	TemplateName string    `json:"template_name"`
	Hits         uint64    `json:"hits"`
	LastAccess   time.Time `json:"last_access"`
}

// generateTemplateCacheKey creates a unique cache key from template content and options.
// The key is based on a SHA-256 hash of: content, delimiters, missingKey option,
// and sorted function map keys. This ensures different configurations produce
// different keys while identical configurations share cache entries.
func generateTemplateCacheKey(content, leftDelim, rightDelim string, missingKey MissingKeyOption, funcMapKeys []string) string {
	h := sha256.New()

	// Hash template content
	h.Write([]byte(content))

	// Hash delimiters
	fmt.Fprintf(h, "delims:%s:%s", leftDelim, rightDelim)

	// Hash missing key option
	fmt.Fprintf(h, "missingkey:%s", missingKey)

	// Hash sorted function map keys for consistency
	sorted := make([]string, len(funcMapKeys))
	copy(sorted, funcMapKeys)
	sort.Strings(sorted)
	fmt.Fprintf(h, "funcs:%s", strings.Join(sorted, ","))

	return fmt.Sprintf("%x", h.Sum(nil))
}

// Package-level default cache (mirrors the CEL cache pattern)
var (
	defaultTemplateCache     *TemplateCache
	defaultTemplateCacheOnce sync.Once
	defaultTemplateCacheMu   sync.RWMutex

	// cacheFactory is an optional factory function that provides the default cache.
	// When set via SetCacheFactory, GetDefaultCache() delegates to this factory
	// instead of creating its own cache.
	cacheFactory     func() *TemplateCache
	cacheFactoryOnce sync.Once
	cacheFactoryMu   sync.RWMutex

	// DefaultTemplateCacheSize is the default size for the package-level cache.
	DefaultTemplateCacheSize = settings.DefaultGoTemplateCacheSize
)

// SetCacheFactory sets the factory function used to get the global template cache.
// This should be called once during application initialization (e.g. from
// InitFromAppConfig). It allows the default cache to be configured from
// application config without circular dependencies.
//
// This function is thread-safe and uses sync.Once to ensure it's only set once.
func SetCacheFactory(factory func() *TemplateCache) {
	cacheFactoryOnce.Do(func() {
		cacheFactoryMu.Lock()
		defer cacheFactoryMu.Unlock()
		cacheFactory = factory
	})
}

// getCacheFactory returns the current cache factory function.
// If no factory has been set, returns nil and the caller should use the default.
func getCacheFactory() func() *TemplateCache {
	cacheFactoryMu.RLock()
	defer cacheFactoryMu.RUnlock()
	return cacheFactory
}

// GetDefaultCache returns the package-level cache instance.
// If a cache factory has been registered via SetCacheFactory, that factory is used.
// Otherwise, the cache is lazily initialized on first access with
// DefaultTemplateCacheSize entries.
// All calls return the same cache instance (singleton pattern).
//
// Use this when you need to access cache statistics:
//
//	stats := gotmpl.GetDefaultCache().Stats()
//	fmt.Printf("Cache hit rate: %.1f%%\n", stats.HitRate)
func GetDefaultCache() *TemplateCache {
	// Check if a factory has been set (e.g. by InitFromAppConfig)
	if factory := getCacheFactory(); factory != nil {
		return factory()
	}

	defaultTemplateCacheMu.RLock()
	defer defaultTemplateCacheMu.RUnlock()

	defaultTemplateCacheOnce.Do(func() {
		defaultTemplateCache = NewTemplateCache(DefaultTemplateCacheSize)
	})
	return defaultTemplateCache
}

// ResetDefaultCache clears and recreates the default cache.
// This is intended for testing only.
//
// WARNING: This is not thread-safe with respect to ongoing template compilations.
// Only call this from test setup functions, not from production code.
func ResetDefaultCache() {
	defaultTemplateCacheMu.Lock()
	defer defaultTemplateCacheMu.Unlock()

	defaultTemplateCache = NewTemplateCache(DefaultTemplateCacheSize)
}

// SetDefaultCacheSize sets the size of the default cache.
// This must be called before the first call to GetDefaultCache() or Service.Execute().
// Once the cache is initialized, this function has no effect.
func SetDefaultCacheSize(size int) {
	DefaultTemplateCacheSize = size
}
