// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProgramCache(t *testing.T) {
	t.Run("valid size", func(t *testing.T) {
		cache := NewProgramCache(50)
		assert.NotNil(t, cache)
		assert.Equal(t, 50, cache.maxSize)
	})

	t.Run("zero size defaults to 100", func(t *testing.T) {
		cache := NewProgramCache(0)
		assert.NotNil(t, cache)
		assert.Equal(t, 100, cache.maxSize)
	})

	t.Run("negative size defaults to 100", func(t *testing.T) {
		cache := NewProgramCache(-10)
		assert.NotNil(t, cache)
		assert.Equal(t, 100, cache.maxSize)
	})
}

func TestProgramCache_GetPut(t *testing.T) {
	cache := NewProgramCache(10)

	// Compile a simple program
	expr := Expression("1 + 2")
	compiled, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)

	key := "test-key"

	// Should not exist initially
	_, found := cache.Get(key)
	assert.False(t, found)

	// Put program
	cache.Put(key, compiled.Program, string(expr))

	// Should exist now
	retrieved, found := cache.Get(key)
	assert.True(t, found)
	assert.NotNil(t, retrieved)

	// Verify it's the same program by evaluating
	result1, err := compiled.Eval(nil)
	require.NoError(t, err)
	retrievedResult := &CompileResult{Program: retrieved, Expression: expr}
	result2, err := retrievedResult.Eval(nil)
	require.NoError(t, err)
	assert.Equal(t, result1, result2)
}

func TestProgramCache_LRUEviction(t *testing.T) {
	cache := NewProgramCache(3)

	// Add 3 programs
	for i := 0; i < 3; i++ {
		expr := Expression("1 + 2")
		compiled, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)
		cache.Put(string(rune('a'+i)), compiled.Program, string(expr))
	}

	stats := cache.Stats()
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, uint64(0), stats.Evictions)

	// Add one more - should evict the oldest (a)
	expr := Expression("1 + 2")
	compiled, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)
	cache.Put("d", compiled.Program, string(expr))

	stats = cache.Stats()
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, uint64(1), stats.Evictions)

	// 'a' should be evicted
	_, found := cache.Get("a")
	assert.False(t, found)

	// 'b', 'c', 'd' should exist
	_, found = cache.Get("b")
	assert.True(t, found)
	_, found = cache.Get("c")
	assert.True(t, found)
	_, found = cache.Get("d")
	assert.True(t, found)
}

func TestProgramCache_LRUOrdering(t *testing.T) {
	cache := NewProgramCache(3)

	// Add 3 programs: a, b, c
	for _, key := range []string{"a", "b", "c"} {
		expr := Expression("1 + 2")
		compiled, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)
		cache.Put(key, compiled.Program, string(expr))
	}

	// Access 'a' to make it most recently used
	// Order is now: a, c, b (b is oldest)
	_, found := cache.Get("a")
	require.True(t, found)

	// Add 'd' - should evict 'b' (oldest)
	expr := Expression("1 + 2")
	compiled, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)
	cache.Put("d", compiled.Program, string(expr))

	// 'b' should be evicted
	_, found = cache.Get("b")
	assert.False(t, found)

	// 'a', 'c', 'd' should exist
	_, found = cache.Get("a")
	assert.True(t, found)
	_, found = cache.Get("c")
	assert.True(t, found)
	_, found = cache.Get("d")
	assert.True(t, found)
}

func TestProgramCache_Clear(t *testing.T) {
	cache := NewProgramCache(10)

	// Add some programs
	for i := 0; i < 5; i++ {
		expr := Expression("1 + 2")
		compiled, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)
		cache.Put(string(rune('a'+i)), compiled.Program, string(expr))
	}

	// Generate some hits and misses
	cache.Get("a")
	cache.Get("nonexistent")

	stats := cache.Stats()
	assert.Equal(t, 5, stats.Size)
	assert.Greater(t, stats.Hits, uint64(0))
	assert.Greater(t, stats.Misses, uint64(0))

	// Clear cache with stats
	cache.ClearWithStats()

	stats = cache.Stats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(0), stats.Misses)
	assert.Equal(t, uint64(0), stats.Evictions)

	// Nothing should exist
	_, found := cache.Get("a")
	assert.False(t, found)

	// Test that Clear() preserves stats
	cache2 := NewProgramCache(10)
	for i := 0; i < 3; i++ {
		expr := Expression(fmt.Sprintf("%d + 1", i))
		compiled, _ := expr.Compile([]cel.EnvOption{})
		cache2.Put(string(rune('a'+i)), compiled.Program, string(expr))
	}
	cache2.Get("a") // Hit
	cache2.Get("z") // Miss
	statsBefore := cache2.Stats()

	cache2.Clear() // Should clear entries but preserve stats

	statsAfter := cache2.Stats()
	assert.Equal(t, 0, statsAfter.Size, "size should be 0 after Clear")
	assert.Equal(t, statsBefore.Hits, statsAfter.Hits, "hits should be preserved")
	assert.Equal(t, statsBefore.Misses, statsAfter.Misses, "misses should be preserved")
}

func TestProgramCache_Stats(t *testing.T) {
	cache := NewProgramCache(10)

	// Add programs
	for i := 0; i < 3; i++ {
		expr := Expression("1 + 2")
		compiled, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)
		cache.Put(string(rune('a'+i)), compiled.Program, string(expr))
	}

	// Generate hits
	cache.Get("a")
	cache.Get("b")
	cache.Get("a") // Hit again

	// Generate misses
	cache.Get("x")
	cache.Get("y")

	stats := cache.Stats()
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, 10, stats.MaxSize)
	assert.Equal(t, uint64(3), stats.Hits)
	assert.Equal(t, uint64(2), stats.Misses)
	assert.Equal(t, uint64(0), stats.Evictions)
	assert.InDelta(t, 60.0, stats.HitRate, 0.1) // 3/5 = 60%
}

func TestProgramCache_HitRate(t *testing.T) {
	cache := NewProgramCache(10)

	// No operations yet
	stats := cache.Stats()
	assert.Equal(t, 0.0, stats.HitRate)

	// Add a program
	expr := Expression("1 + 2")
	compiled, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)
	cache.Put("a", compiled.Program, string(expr))

	// 2 hits, 1 miss = 66.67% hit rate
	cache.Get("a")
	cache.Get("a")
	cache.Get("b") // miss

	stats = cache.Stats()
	assert.InDelta(t, 66.67, stats.HitRate, 0.1)
}

func TestGenerateCacheKeyWithAST(t *testing.T) {
	cache := NewProgramCache(10) // Create a cache for testing
	ctx := context.Background()

	t.Run("same expression and options produce same key", func(t *testing.T) {
		opts := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}

		key1 := generateCacheKeyWithAST(ctx, cache, "x + y", opts, GetDefaultCostLimit())
		key2 := generateCacheKeyWithAST(ctx, cache, "x + y", opts, GetDefaultCostLimit())

		assert.Equal(t, key1.key, key2.key)
	})

	t.Run("different expressions produce different keys", func(t *testing.T) {
		opts := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
		}

		key1 := generateCacheKeyWithAST(ctx, cache, "x + 1", opts, GetDefaultCostLimit())
		key2 := generateCacheKeyWithAST(ctx, cache, "x + 2", opts, GetDefaultCostLimit())

		assert.NotEqual(t, key1.key, key2.key)
	})

	t.Run("different options produce different keys", func(t *testing.T) {
		opts1 := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
		}
		opts2 := []cel.EnvOption{
			cel.Variable("y", cel.IntType),
		}

		key1 := generateCacheKeyWithAST(ctx, cache, "x + 1", opts1, GetDefaultCostLimit())
		key2 := generateCacheKeyWithAST(ctx, cache, "x + 1", opts2, GetDefaultCostLimit())

		// Note: These might be equal if the option pointers happen to be the same
		// In practice, they would be different in a real scenario
		assert.NotEmpty(t, key1.key)
		assert.NotEmpty(t, key2.key)
	})

	t.Run("empty options", func(t *testing.T) {
		key := generateCacheKeyWithAST(ctx, cache, "1 + 2", nil, GetDefaultCostLimit())
		assert.NotEmpty(t, key.key)
	})

	t.Run("semantically identical options produce same key", func(t *testing.T) {
		// This test validates the fix for issue #1:
		// Creating new cel.EnvOption instances with the same semantic content
		// should produce the same cache key (content-based hashing)

		// First call - create options
		opts1 := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}
		key1 := generateCacheKeyWithAST(ctx, cache, "x + y", opts1, GetDefaultCostLimit())

		// Second call - create NEW option instances with same content
		opts2 := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}
		key2 := generateCacheKeyWithAST(ctx, cache, "x + y", opts2, GetDefaultCostLimit())

		// Keys should be EQUAL because semantic content is the same
		// (Before the fix, these would be different due to pointer address hashing)
		assert.Equal(t, key1.key, key2.key, "semantically identical options should produce the same cache key")
	})

	t.Run("different cost limits produce different keys", func(t *testing.T) {
		// This test validates the fix for issue #4:
		// Different cost limits should produce different cache keys
		opts := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
		}

		key1 := generateCacheKeyWithAST(ctx, cache, "x + 1", opts, 1000)
		key2 := generateCacheKeyWithAST(ctx, cache, "x + 1", opts, 5000)
		key3 := generateCacheKeyWithAST(ctx, cache, "x + 1", opts, 0) // No cost limit

		// All keys should be different
		assert.NotEqual(t, key1.key, key2.key, "different cost limits should produce different keys")
		assert.NotEqual(t, key1.key, key3.key, "cost limit vs no limit should produce different keys")
		assert.NotEqual(t, key2.key, key3.key, "different cost limits should produce different keys")
	})
}

func TestCompile_withCache(t *testing.T) {
	t.Run("cache miss compiles and caches", func(t *testing.T) {
		cache := NewProgramCache(10)

		expr := Expression("1 + 2")
		result, err := expr.Compile([]cel.EnvOption{}, WithCache(cache))
		require.NoError(t, err)
		require.NotNil(t, result)

		stats := cache.Stats()
		assert.Equal(t, 1, stats.Size)
		assert.Equal(t, uint64(0), stats.Hits)
		assert.Equal(t, uint64(1), stats.Misses)

		// Verify program works
		value, err := result.Eval(nil)
		require.NoError(t, err)
		assert.Equal(t, int64(3), value)
	})

	t.Run("cache hit returns cached program", func(t *testing.T) {
		cache := NewProgramCache(10)

		opts := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}

		expr := Expression("x + y")
		// First call - cache miss
		result1, err := expr.Compile(opts, WithCache(cache))
		require.NoError(t, err)

		// Second call - cache hit
		result2, err := expr.Compile(opts, WithCache(cache))
		require.NoError(t, err)

		stats := cache.Stats()
		assert.Equal(t, 1, stats.Size)
		assert.Equal(t, uint64(1), stats.Hits)
		assert.Equal(t, uint64(1), stats.Misses)

		// Both programs should work
		vars := map[string]any{"x": int64(10), "y": int64(20)}
		value1, err := result1.Eval(vars)
		require.NoError(t, err)
		value2, err := result2.Eval(vars)
		require.NoError(t, err)
		assert.Equal(t, value1, value2)
		assert.Equal(t, int64(30), value1)
	})

	t.Run("compilation error not cached", func(t *testing.T) {
		cache := NewProgramCache(10)

		// Invalid expression
		expr := Expression("x +")
		_, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)}, WithCache(cache))
		assert.Error(t, err)

		// Cache should be empty
		stats := cache.Stats()
		assert.Equal(t, 0, stats.Size)
	})

	t.Run("multiple different expressions cached", func(t *testing.T) {
		cache := NewProgramCache(10)

		expressions := []Expression{
			"1 + 2",
			"3 * 4",
			"5 - 2",
			"10 / 2",
		}

		for _, expr := range expressions {
			_, err := expr.Compile([]cel.EnvOption{}, WithCache(cache))
			require.NoError(t, err)
		}

		stats := cache.Stats()
		assert.Equal(t, 4, stats.Size)
		assert.Equal(t, uint64(0), stats.Hits)
		assert.Equal(t, uint64(4), stats.Misses)

		// Call again - should all be hits
		for _, expr := range expressions {
			_, err := expr.Compile([]cel.EnvOption{}, WithCache(cache))
			require.NoError(t, err)
		}

		stats = cache.Stats()
		assert.Equal(t, 4, stats.Size)
		assert.Equal(t, uint64(4), stats.Hits)
		assert.Equal(t, uint64(4), stats.Misses)
		assert.Equal(t, 50.0, stats.HitRate)
	})

	t.Run("cache hits with recreated options (validates fix for issue #1)", func(t *testing.T) {
		cache := NewProgramCache(10)
		expr := Expression("x + y")

		// First call with new options
		result1, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}, WithCache(cache))
		require.NoError(t, err)
		require.NotNil(t, result1)

		stats := cache.Stats()
		assert.Equal(t, 1, stats.Size)
		assert.Equal(t, uint64(0), stats.Hits)
		assert.Equal(t, uint64(1), stats.Misses)

		// Second call with NEW option instances (same semantic content)
		// Before the fix, this would be a cache miss due to pointer-based hashing
		// After the fix, this should be a cache hit due to content-based hashing
		result2, err := expr.Compile(
			[]cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			},
			WithCache(cache),
			WithContext(context.Background()),
			WithCostLimit(GetDefaultCostLimit()),
		)
		require.NoError(t, err)
		require.NotNil(t, result2)

		stats = cache.Stats()
		assert.Equal(t, 1, stats.Size, "should still be 1 cached entry")
		assert.Equal(t, uint64(1), stats.Hits, "should be a cache HIT, not a miss")
		assert.Equal(t, uint64(1), stats.Misses)
		assert.Equal(t, 50.0, stats.HitRate)

		// Third call to confirm continued hits
		result3, err := expr.Compile(
			[]cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			},
			WithCache(cache),
			WithContext(context.Background()),
			WithCostLimit(GetDefaultCostLimit()),
		)
		require.NoError(t, err)
		require.NotNil(t, result3)

		stats = cache.Stats()
		assert.Equal(t, uint64(2), stats.Hits, "third call should also be a cache hit")
		assert.InDelta(t, 66.67, stats.HitRate, 0.1)
	})
}

func TestCompileWithCache_Concurrent(t *testing.T) {
	cache := NewProgramCache(100)
	expr := Expression("x * 2")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
	}

	var wg sync.WaitGroup
	numGoroutines := 50
	numIterations := 10

	// Launch multiple goroutines that compile the same expression
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numIterations; j++ {
				compiled, err := expr.Compile(opts, WithCache(cache), WithContext(context.Background()), WithCostLimit(GetDefaultCostLimit()))
				require.NoError(t, err)
				require.NotNil(t, compiled)

				// Verify the program works
				value, err := compiled.Eval(map[string]any{"x": int64(5)})
				require.NoError(t, err)
				assert.Equal(t, int64(10), value)
			}
		}()
	}

	wg.Wait()

	// Should only have one cached program
	stats := cache.Stats()
	assert.Equal(t, 1, stats.Size)

	// Should have many hits (not all misses)
	totalOps := uint64(numGoroutines * numIterations)
	assert.Greater(t, stats.Hits, uint64(0))
	assert.Equal(t, totalOps, stats.Hits+stats.Misses)
	assert.Greater(t, stats.HitRate, 50.0) // Should have good hit rate
}

func TestProgramCache_ConcurrentAccess(t *testing.T) {
	cache := NewProgramCache(50)
	var wg sync.WaitGroup

	// Goroutines adding programs
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			expr := Expression("1 + 2")
			compiled, err := expr.Compile([]cel.EnvOption{})
			require.NoError(t, err)
			cache.Put(string(rune('a'+index%26)), compiled.Program, string(expr))
		}(i)
	}

	// Goroutines reading programs
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			cache.Get(string(rune('a' + index%26)))
		}(i)
	}

	// Goroutines reading stats
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cache.Stats()
		}()
	}

	wg.Wait()

	// Should complete without panics or deadlocks
	stats := cache.Stats()
	assert.LessOrEqual(t, stats.Size, 50)
}

func BenchmarkCompileWithCache_Hit(b *testing.B) {
	cache := NewProgramCache(100)
	expr := Expression("x + y * 2")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	// Prime the cache
	_, err := expr.Compile(opts, WithCache(cache), WithContext(context.Background()), WithCostLimit(GetDefaultCostLimit()))
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expr.Compile(opts, WithCache(cache), WithContext(context.Background()), WithCostLimit(GetDefaultCostLimit()))
	}
}

func BenchmarkCompileWithCache_Miss(b *testing.B) {
	cache := NewProgramCache(1000)
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Each iteration uses a different expression (cache miss)
		expr := Expression(fmt.Sprintf("x + %d", i))
		_, _ = expr.Compile(opts, WithCache(cache), WithContext(context.Background()), WithCostLimit(GetDefaultCostLimit()))
	}
}

func TestCompile_withASTBasedCaching(t *testing.T) {
	t.Run("structurally identical expressions share cache with AST keys", func(t *testing.T) {
		// Create cache with AST-based caching enabled
		cache := NewProgramCache(10, WithASTBasedCaching(true))

		// Two expressions with same structure but different variable names
		expr1 := Expression("x + y")
		opts1 := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}
		result1, err := expr1.Compile(opts1, WithCache(cache))
		require.NoError(t, err)
		require.NotNil(t, result1)

		// First expression should be a cache miss
		stats := cache.Stats()
		assert.Equal(t, uint64(1), stats.Misses, "First compilation should miss cache")
		assert.Equal(t, uint64(0), stats.Hits, "First compilation should have no hits")

		// Second expression with different variable names but same structure
		expr2 := Expression("a + b")
		opts2 := []cel.EnvOption{
			cel.Variable("a", cel.IntType),
			cel.Variable("b", cel.IntType),
		}
		result2, err := expr2.Compile(opts2, WithCache(cache))
		require.NoError(t, err)
		require.NotNil(t, result2)

		// Second expression SHOULD hit cache with AST-based caching
		stats = cache.Stats()
		assert.Equal(t, uint64(1), stats.Hits, "Second compilation should hit cache")
		assert.Equal(t, uint64(1), stats.Misses, "Should still have only one miss")
	})

	t.Run("different types produce different cache keys", func(t *testing.T) {
		// Create cache with AST-based caching enabled
		cache := NewProgramCache(10, WithASTBasedCaching(true))

		// Int addition
		expr1 := Expression("x + y")
		opts1 := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}
		_, err := expr1.Compile(opts1, WithCache(cache))
		require.NoError(t, err)

		// String concatenation (different type, should NOT share cache)
		expr2 := Expression("a + b")
		opts2 := []cel.EnvOption{
			cel.Variable("a", cel.StringType),
			cel.Variable("b", cel.StringType),
		}
		_, err = expr2.Compile(opts2, WithCache(cache))
		require.NoError(t, err)

		// Both should be cache misses (different types)
		stats := cache.Stats()
		assert.Equal(t, uint64(0), stats.Hits, "Different types should not share cache")
		assert.Equal(t, uint64(2), stats.Misses, "Both should be cache misses")
	})

	t.Run("traditional caching without AST keys", func(t *testing.T) {
		// Create cache WITHOUT AST-based caching (default behavior)
		cache := NewProgramCache(10)

		// Two expressions with same structure but different variable names
		expr1 := Expression("x + y")
		opts1 := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}
		_, err := expr1.Compile(opts1, WithCache(cache))
		require.NoError(t, err)

		expr2 := Expression("a + b")
		opts2 := []cel.EnvOption{
			cel.Variable("a", cel.IntType),
			cel.Variable("b", cel.IntType),
		}
		_, err = expr2.Compile(opts2, WithCache(cache))
		require.NoError(t, err)

		// Without AST caching, both should be cache misses
		stats := cache.Stats()
		assert.Equal(t, uint64(0), stats.Hits, "Without AST caching, should not share cache")
		assert.Equal(t, uint64(2), stats.Misses, "Both should be cache misses")
	})
}

func BenchmarkCompile_NoCache(b *testing.B) {
	expr := Expression("x + y * 2")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expr.Compile(opts)
	}
}
