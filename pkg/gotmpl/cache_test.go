// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"text/template"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTemplateCache(t *testing.T) {
	t.Run("valid size", func(t *testing.T) {
		cache := NewTemplateCache(50)
		assert.NotNil(t, cache)
		assert.Equal(t, 50, cache.maxSize)
	})

	t.Run("zero size defaults to 100", func(t *testing.T) {
		cache := NewTemplateCache(0)
		assert.NotNil(t, cache)
		assert.Equal(t, 100, cache.maxSize)
	})

	t.Run("negative size defaults to 100", func(t *testing.T) {
		cache := NewTemplateCache(-10)
		assert.NotNil(t, cache)
		assert.Equal(t, 100, cache.maxSize)
	})
}

func TestTemplateCache_GetPut(t *testing.T) {
	cache := NewTemplateCache(10)

	// Parse a simple template
	tmpl, err := template.New("test").Parse("Hello, {{.Name}}!")
	require.NoError(t, err)

	key := "test-key"

	// Should not exist initially
	_, found := cache.Get(key)
	assert.False(t, found)

	// Put template
	cache.Put(key, tmpl, "test")

	// Should exist now
	retrieved, found := cache.Get(key)
	assert.True(t, found)
	assert.NotNil(t, retrieved)

	// Verify the cached template works correctly
	var buf1, buf2 [1]byte
	_ = buf1
	_ = buf2

	data := map[string]string{"Name": "World"}
	result := executeTemplate(t, retrieved, data)
	assert.Equal(t, "Hello, World!", result)
}

func TestTemplateCache_GetReturnsClone(t *testing.T) {
	cache := NewTemplateCache(10)

	tmpl, err := template.New("test").Parse("Value: {{.Val}}")
	require.NoError(t, err)

	cache.Put("key", tmpl, "test")

	// Get two clones and execute with different data
	clone1, ok := cache.Get("key")
	require.True(t, ok)

	clone2, ok := cache.Get("key")
	require.True(t, ok)

	result1 := executeTemplate(t, clone1, map[string]string{"Val": "A"})
	result2 := executeTemplate(t, clone2, map[string]string{"Val": "B"})

	assert.Equal(t, "Value: A", result1)
	assert.Equal(t, "Value: B", result2)
}

func TestTemplateCache_LRUEviction(t *testing.T) {
	cache := NewTemplateCache(3)

	// Add 3 templates
	for i := 0; i < 3; i++ {
		tmpl, err := template.New(fmt.Sprintf("tmpl-%d", i)).Parse(fmt.Sprintf("Template %d", i))
		require.NoError(t, err)
		cache.Put(fmt.Sprintf("key-%d", i), tmpl, fmt.Sprintf("tmpl-%d", i))
	}

	stats := cache.Stats()
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, uint64(0), stats.Evictions)

	// Add one more - should evict the oldest (key-0)
	tmpl, err := template.New("tmpl-3").Parse("Template 3")
	require.NoError(t, err)
	cache.Put("key-3", tmpl, "tmpl-3")

	stats = cache.Stats()
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, uint64(1), stats.Evictions)

	// 'key-0' should be evicted
	_, found := cache.Get("key-0")
	assert.False(t, found)

	// 'key-1', 'key-2', 'key-3' should exist
	_, found = cache.Get("key-1")
	assert.True(t, found)
	_, found = cache.Get("key-2")
	assert.True(t, found)
	_, found = cache.Get("key-3")
	assert.True(t, found)
}

func TestTemplateCache_LRUAccess(t *testing.T) {
	cache := NewTemplateCache(3)

	// Add 3 templates
	for i := 0; i < 3; i++ {
		tmpl, err := template.New(fmt.Sprintf("tmpl-%d", i)).Parse(fmt.Sprintf("Template %d", i))
		require.NoError(t, err)
		cache.Put(fmt.Sprintf("key-%d", i), tmpl, fmt.Sprintf("tmpl-%d", i))
	}

	// Access key-0 to make it recently used
	_, found := cache.Get("key-0")
	assert.True(t, found)

	// Add key-3 - should evict key-1 (least recently used), not key-0
	tmpl, err := template.New("tmpl-3").Parse("Template 3")
	require.NoError(t, err)
	cache.Put("key-3", tmpl, "tmpl-3")

	_, found = cache.Get("key-0")
	assert.True(t, found, "key-0 should not be evicted since it was recently accessed")

	_, found = cache.Get("key-1")
	assert.False(t, found, "key-1 should be evicted as least recently used")
}

func TestTemplateCache_PutUpdate(t *testing.T) {
	cache := NewTemplateCache(10)

	tmpl1, err := template.New("v1").Parse("Version 1")
	require.NoError(t, err)
	cache.Put("key", tmpl1, "v1")

	tmpl2, err := template.New("v2").Parse("Version 2")
	require.NoError(t, err)
	cache.Put("key", tmpl2, "v2")

	// Should still have size 1
	stats := cache.Stats()
	assert.Equal(t, 1, stats.Size)

	// Should return updated template
	retrieved, found := cache.Get("key")
	assert.True(t, found)
	result := executeTemplate(t, retrieved, nil)
	assert.Equal(t, "Version 2", result)
}

func TestTemplateCache_Stats(t *testing.T) {
	cache := NewTemplateCache(10)

	tmpl, err := template.New("test").Parse("Hello")
	require.NoError(t, err)
	cache.Put("key", tmpl, "test")

	// Generate some hits and misses
	cache.Get("key")         // hit
	cache.Get("key")         // hit
	cache.Get("nonexistent") // miss

	stats := cache.Stats()
	assert.Equal(t, 1, stats.Size)
	assert.Equal(t, 10, stats.MaxSize)
	assert.Equal(t, uint64(2), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)
	assert.Equal(t, uint64(3), stats.TotalAccesses)
	assert.InDelta(t, 66.67, stats.HitRate, 0.1)
}

func TestTemplateCache_GetDetailedStats(t *testing.T) {
	cache := NewTemplateCache(10)

	tmpl1, err := template.New("tmpl-a").Parse("A")
	require.NoError(t, err)
	cache.Put("key-a", tmpl1, "tmpl-a")

	tmpl2, err := template.New("tmpl-b").Parse("B")
	require.NoError(t, err)
	cache.Put("key-b", tmpl2, "tmpl-b")

	// Access key-a 3 times, key-b 1 time
	cache.Get("key-a")
	cache.Get("key-a")
	cache.Get("key-a")
	cache.Get("key-b")

	stats := cache.GetDetailedStats(0)
	assert.Len(t, stats.TopTemplates, 2)
	assert.Equal(t, "tmpl-a", stats.TopTemplates[0].TemplateName) // Most hits first
	assert.Equal(t, uint64(3), stats.TopTemplates[0].Hits)
	assert.Equal(t, "tmpl-b", stats.TopTemplates[1].TemplateName)
	assert.Equal(t, uint64(1), stats.TopTemplates[1].Hits)

	// With topN
	statsTop1 := cache.GetDetailedStats(1)
	assert.Len(t, statsTop1.TopTemplates, 1)
	assert.Equal(t, "tmpl-a", statsTop1.TopTemplates[0].TemplateName)
}

func TestTemplateCache_Clear(t *testing.T) {
	cache := NewTemplateCache(10)

	tmpl, err := template.New("test").Parse("Hello")
	require.NoError(t, err)
	cache.Put("key", tmpl, "test")
	cache.Get("key") // hit

	cache.Clear()

	stats := cache.Stats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, uint64(1), stats.Hits, "Hits should be preserved after Clear()")

	_, found := cache.Get("key")
	assert.False(t, found)
}

func TestTemplateCache_ClearWithStats(t *testing.T) {
	cache := NewTemplateCache(10)

	tmpl, err := template.New("test").Parse("Hello")
	require.NoError(t, err)
	cache.Put("key", tmpl, "test")
	cache.Get("key")

	cache.ClearWithStats()

	stats := cache.Stats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(0), stats.Misses)
	assert.Equal(t, uint64(0), stats.Evictions)
}

func TestTemplateCache_ResetStats(t *testing.T) {
	cache := NewTemplateCache(10)

	tmpl, err := template.New("test").Parse("Hello")
	require.NoError(t, err)
	cache.Put("key", tmpl, "test")
	cache.Get("key")

	cache.ResetStats()

	stats := cache.Stats()
	assert.Equal(t, 1, stats.Size, "Cache entries should be preserved")
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(0), stats.Misses)
}

func TestTemplateCache_ConcurrentAccess(t *testing.T) {
	cache := NewTemplateCache(100)

	// Pre-populate with templates
	for i := 0; i < 10; i++ {
		tmpl, err := template.New(fmt.Sprintf("tmpl-%d", i)).Parse(fmt.Sprintf("Template %d", i))
		require.NoError(t, err)
		cache.Put(fmt.Sprintf("key-%d", i), tmpl, fmt.Sprintf("tmpl-%d", i))
	}

	var wg sync.WaitGroup
	const goroutines = 50
	const opsPerGoroutine = 100

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d", j%10)
				if j%3 == 0 {
					// Read
					tmpl, found := cache.Get(key)
					if found {
						_ = executeTemplate(t, tmpl, nil)
					}
				} else {
					// Write
					tmpl, err := template.New(fmt.Sprintf("concurrent-%d-%d", id, j)).Parse(fmt.Sprintf("C %d %d", id, j))
					if err == nil {
						cache.Put(fmt.Sprintf("ckey-%d-%d", id, j), tmpl, fmt.Sprintf("c-%d-%d", id, j))
					}
				}
			}
		}(g)
	}

	wg.Wait()

	stats := cache.Stats()
	assert.Greater(t, stats.TotalAccesses, uint64(0))
}

func TestGenerateTemplateCacheKey(t *testing.T) {
	t.Run("same inputs produce same key", func(t *testing.T) {
		key1 := generateTemplateCacheKey("Hello {{.Name}}", "{{", "}}", MissingKeyDefault, []string{"upper", "lower"})
		key2 := generateTemplateCacheKey("Hello {{.Name}}", "{{", "}}", MissingKeyDefault, []string{"upper", "lower"})
		assert.Equal(t, key1, key2)
	})

	t.Run("different content produces different key", func(t *testing.T) {
		key1 := generateTemplateCacheKey("Hello {{.Name}}", "{{", "}}", MissingKeyDefault, nil)
		key2 := generateTemplateCacheKey("Goodbye {{.Name}}", "{{", "}}", MissingKeyDefault, nil)
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different delimiters produce different key", func(t *testing.T) {
		key1 := generateTemplateCacheKey("Hello", "{{", "}}", MissingKeyDefault, nil)
		key2 := generateTemplateCacheKey("Hello", "<<", ">>", MissingKeyDefault, nil)
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different missingKey produces different key", func(t *testing.T) {
		key1 := generateTemplateCacheKey("Hello", "{{", "}}", MissingKeyDefault, nil)
		key2 := generateTemplateCacheKey("Hello", "{{", "}}", MissingKeyError, nil)
		assert.NotEqual(t, key1, key2)
	})

	t.Run("different func maps produce different key", func(t *testing.T) {
		key1 := generateTemplateCacheKey("Hello", "{{", "}}", MissingKeyDefault, []string{"upper"})
		key2 := generateTemplateCacheKey("Hello", "{{", "}}", MissingKeyDefault, []string{"lower"})
		assert.NotEqual(t, key1, key2)
	})

	t.Run("func map order does not matter", func(t *testing.T) {
		key1 := generateTemplateCacheKey("Hello", "{{", "}}", MissingKeyDefault, []string{"upper", "lower"})
		key2 := generateTemplateCacheKey("Hello", "{{", "}}", MissingKeyDefault, []string{"lower", "upper"})
		assert.Equal(t, key1, key2)
	})
}

func TestTemplateCache_MetricsLimit(t *testing.T) {
	cache := NewTemplateCache(10000)
	cache.metricsLimit = 5 // Lower limit for testing

	// Add and access more than metricsLimit unique templates
	for i := 0; i < 10; i++ {
		tmpl, err := template.New(fmt.Sprintf("tmpl-%d", i)).Parse(fmt.Sprintf("T%d", i))
		require.NoError(t, err)
		key := fmt.Sprintf("key-%d", i)
		cache.Put(key, tmpl, fmt.Sprintf("tmpl-%d", i))
		cache.Get(key)
	}

	// Should not exceed the metrics limit
	cache.mu.RLock()
	assert.LessOrEqual(t, len(cache.tmplHits), cache.metricsLimit+1)
	cache.mu.RUnlock()
}

func TestTemplateCache_HitRateZeroAccesses(t *testing.T) {
	cache := NewTemplateCache(10)

	stats := cache.Stats()
	assert.Equal(t, 0.0, stats.HitRate)
}

func TestService_ExecuteWithCache(t *testing.T) {
	cache := NewTemplateCache(100)
	svc := NewServiceWithCache(nil, cache)
	ctx := context.Background()

	opts := TemplateOptions{
		Content: "Hello, {{.Name}}!",
		Name:    "test-template",
		Data:    map[string]string{"Name": "World"},
	}

	// First call should be a cache miss
	result1, err := svc.Execute(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", result1.Output)

	stats := cache.Stats()
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)
	assert.Equal(t, 1, stats.Size)

	// Second call with same content should be a cache hit
	opts.Data = map[string]string{"Name": "Go"}
	result2, err := svc.Execute(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, "Hello, Go!", result2.Output)

	stats = cache.Stats()
	assert.Equal(t, uint64(1), stats.Hits)
	assert.Equal(t, uint64(1), stats.Misses)
}

func TestService_ExecuteCacheDifferentContent(t *testing.T) {
	cache := NewTemplateCache(100)
	svc := NewServiceWithCache(nil, cache)
	ctx := context.Background()

	// Two different template contents should create two cache entries
	result1, err := svc.Execute(ctx, TemplateOptions{
		Content: "Hello, {{.Name}}!",
		Name:    "tmpl-1",
		Data:    map[string]string{"Name": "A"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Hello, A!", result1.Output)

	result2, err := svc.Execute(ctx, TemplateOptions{
		Content: "Goodbye, {{.Name}}!",
		Name:    "tmpl-2",
		Data:    map[string]string{"Name": "B"},
	})
	require.NoError(t, err)
	assert.Equal(t, "Goodbye, B!", result2.Output)

	stats := cache.Stats()
	assert.Equal(t, 2, stats.Size)
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(2), stats.Misses)
}

func TestDefaultCache(t *testing.T) {
	// Save and restore default cache state
	ResetDefaultCache()
	defer ResetDefaultCache()

	cache := GetDefaultCache()
	assert.NotNil(t, cache)

	// Should return same instance
	cache2 := GetDefaultCache()
	assert.Same(t, cache, cache2)
}

func TestSetDefaultCacheSize(t *testing.T) {
	// Save and restore
	originalSize := DefaultTemplateCacheSize
	defer func() { DefaultTemplateCacheSize = originalSize }()

	SetDefaultCacheSize(500)
	assert.Equal(t, 500, DefaultTemplateCacheSize)
}

// executeTemplate is a test helper to execute a template and return the output.
func executeTemplate(t *testing.T, tmpl *template.Template, data any) string {
	t.Helper()
	var writer strings.Builder
	err := tmpl.Execute(&writer, data)
	require.NoError(t, err)
	return writer.String()
}
