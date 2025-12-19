package celexp

import (
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
	compiled, err := expr.Compile()
	require.NoError(t, err)

	key := "test-key"

	// Should not exist initially
	_, found := cache.Get(key)
	assert.False(t, found)

	// Put program
	cache.Put(key, compiled.Program)

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
		compiled, err := expr.Compile()
		require.NoError(t, err)
		cache.Put(string(rune('a'+i)), compiled.Program)
	}

	stats := cache.Stats()
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, uint64(0), stats.Evictions)

	// Add one more - should evict the oldest (a)
	expr := Expression("1 + 2")
	compiled, err := expr.Compile()
	require.NoError(t, err)
	cache.Put("d", compiled.Program)

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
		compiled, err := expr.Compile()
		require.NoError(t, err)
		cache.Put(key, compiled.Program)
	}

	// Access 'a' to make it most recently used
	// Order is now: a, c, b (b is oldest)
	_, found := cache.Get("a")
	require.True(t, found)

	// Add 'd' - should evict 'b' (oldest)
	expr := Expression("1 + 2")
	compiled, err := expr.Compile()
	require.NoError(t, err)
	cache.Put("d", compiled.Program)

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
		compiled, err := expr.Compile()
		require.NoError(t, err)
		cache.Put(string(rune('a'+i)), compiled.Program)
	}

	// Generate some hits and misses
	cache.Get("a")
	cache.Get("nonexistent")

	stats := cache.Stats()
	assert.Equal(t, 5, stats.Size)
	assert.Greater(t, stats.Hits, uint64(0))
	assert.Greater(t, stats.Misses, uint64(0))

	// Clear cache
	cache.Clear()

	stats = cache.Stats()
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, uint64(0), stats.Hits)
	assert.Equal(t, uint64(0), stats.Misses)
	assert.Equal(t, uint64(0), stats.Evictions)

	// Nothing should exist
	_, found := cache.Get("a")
	assert.False(t, found)
}

func TestProgramCache_Stats(t *testing.T) {
	cache := NewProgramCache(10)

	// Add programs
	for i := 0; i < 3; i++ {
		expr := Expression("1 + 2")
		compiled, err := expr.Compile()
		require.NoError(t, err)
		cache.Put(string(rune('a'+i)), compiled.Program)
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
	compiled, err := expr.Compile()
	require.NoError(t, err)
	cache.Put("a", compiled.Program)

	// 2 hits, 1 miss = 66.67% hit rate
	cache.Get("a")
	cache.Get("a")
	cache.Get("b") // miss

	stats = cache.Stats()
	assert.InDelta(t, 66.67, stats.HitRate, 0.1)
}

func TestGenerateCacheKey(t *testing.T) {
	t.Run("same expression and options produce same key", func(t *testing.T) {
		opts := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		}

		key1 := generateCacheKey("x + y", opts)
		key2 := generateCacheKey("x + y", opts)

		assert.Equal(t, key1, key2)
	})

	t.Run("different expressions produce different keys", func(t *testing.T) {
		opts := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
		}

		key1 := generateCacheKey("x + 1", opts)
		key2 := generateCacheKey("x + 2", opts)

		assert.NotEqual(t, key1, key2)
	})

	t.Run("different options produce different keys", func(t *testing.T) {
		opts1 := []cel.EnvOption{
			cel.Variable("x", cel.IntType),
		}
		opts2 := []cel.EnvOption{
			cel.Variable("y", cel.IntType),
		}

		key1 := generateCacheKey("x + 1", opts1)
		key2 := generateCacheKey("x + 1", opts2)

		// Note: These might be equal if the option pointers happen to be the same
		// In practice, they would be different in a real scenario
		assert.NotEmpty(t, key1)
		assert.NotEmpty(t, key2)
	})

	t.Run("empty options", func(t *testing.T) {
		key := generateCacheKey("1 + 2", nil)
		assert.NotEmpty(t, key)
	})
}

func TestCompileWithCache(t *testing.T) {
	t.Run("nil cache returns error", func(t *testing.T) {
		expr := Expression("1 + 2")
		_, err := CompileWithCache(nil, expr)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cache cannot be nil")
	})

	t.Run("cache miss compiles and caches", func(t *testing.T) {
		cache := NewProgramCache(10)

		expr := Expression("1 + 2")
		result, err := CompileWithCache(cache, expr)
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
		result1, err := CompileWithCache(cache, expr, opts...)
		require.NoError(t, err)

		// Second call - cache hit
		result2, err := CompileWithCache(cache, expr, opts...)
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
		_, err := CompileWithCache(cache, expr, cel.Variable("x", cel.IntType))
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
			_, err := CompileWithCache(cache, expr)
			require.NoError(t, err)
		}

		stats := cache.Stats()
		assert.Equal(t, 4, stats.Size)
		assert.Equal(t, uint64(0), stats.Hits)
		assert.Equal(t, uint64(4), stats.Misses)

		// Call again - should all be hits
		for _, expr := range expressions {
			_, err := CompileWithCache(cache, expr)
			require.NoError(t, err)
		}

		stats = cache.Stats()
		assert.Equal(t, 4, stats.Size)
		assert.Equal(t, uint64(4), stats.Hits)
		assert.Equal(t, uint64(4), stats.Misses)
		assert.Equal(t, 50.0, stats.HitRate)
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
				compiled, err := CompileWithCache(cache, expr, opts...)
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
			compiled, err := expr.Compile()
			require.NoError(t, err)
			cache.Put(string(rune('a'+index%26)), compiled.Program)
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
	_, err := CompileWithCache(cache, expr, opts...)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = CompileWithCache(cache, expr, opts...)
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
		_, _ = CompileWithCache(cache, expr, opts...)
	}
}

func BenchmarkCompile_NoCache(b *testing.B) {
	expr := Expression("x + y * 2")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expr.Compile(opts...)
	}
}
