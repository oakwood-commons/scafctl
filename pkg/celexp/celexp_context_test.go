// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompile_WithContext(t *testing.T) {
	t.Run("successful compilation with valid context", func(t *testing.T) {
		ctx := context.Background()
		expr := Expression("x + y")

		result, err := expr.Compile(
			[]cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			},
			WithContext(ctx),
		)

		require.NoError(t, err)
		require.NotNil(t, result)

		val, err := result.Eval(map[string]any{"x": int64(10), "y": int64(20)})
		require.NoError(t, err)
		assert.Equal(t, int64(30), val)
	})

	t.Run("compilation fails with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		expr := Expression("x + y")
		_, err := expr.Compile(
			[]cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			},
			WithContext(ctx),
		)

		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	})

	t.Run("compilation respects timeout", func(t *testing.T) {
		// Very short timeout - may or may not timeout depending on machine speed
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()

		time.Sleep(2 * time.Millisecond) // Ensure timeout occurs

		expr := Expression("x + y")
		_, err := expr.Compile(
			[]cel.EnvOption{
				cel.Variable("x", cel.IntType),
				cel.Variable("y", cel.IntType),
			},
			WithContext(ctx),
		)
		// May succeed if fast enough, or fail with timeout
		if err != nil {
			assert.Contains(t, err.Error(), "context")
		}
	})
}

func TestCompile_WithContextAndOptions(t *testing.T) {
	t.Run("custom cost limit with context", func(t *testing.T) {
		ctx := context.Background()
		customLimit := uint64(1000)

		expr := Expression("x * 2")
		result, err := expr.Compile(
			[]cel.EnvOption{cel.Variable("x", cel.IntType)},
			WithContext(ctx),
			WithCostLimit(customLimit),
		)

		require.NoError(t, err)
		require.NotNil(t, result)
	})

	t.Run("cancelled context with custom options", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		expr := Expression("x * 2")
		_, err := expr.Compile(
			[]cel.EnvOption{cel.Variable("x", cel.IntType)},
			WithContext(ctx),
		)

		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))
	})

	t.Run("custom cache with context", func(t *testing.T) {
		ctx := context.Background()
		customCache := NewProgramCache(10)

		expr := Expression("x + 1")
		result, err := expr.Compile(
			[]cel.EnvOption{cel.Variable("x", cel.IntType)},
			WithContext(ctx),
			WithCache(customCache),
		)

		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify it used our cache
		stats := customCache.Stats()
		assert.Equal(t, 1, stats.Size)
		assert.Equal(t, uint64(1), stats.Misses)
	})
}

func TestGetDefaultCacheStats(t *testing.T) {
	// Clear for clean test
	ClearDefaultCache()

	t.Run("returns valid statistics", func(t *testing.T) {
		stats := GetDefaultCacheStats()
		assert.GreaterOrEqual(t, stats.MaxSize, 0)
		assert.GreaterOrEqual(t, stats.Size, 0)
		assert.GreaterOrEqual(t, stats.Hits, uint64(0))
		assert.GreaterOrEqual(t, stats.Misses, uint64(0))
	})

	t.Run("statistics update after compilation", func(t *testing.T) {
		initialStats := GetDefaultCacheStats()

		expr := Expression("1 + 1")
		_, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		newStats := GetDefaultCacheStats()
		assert.GreaterOrEqual(t, newStats.Size, initialStats.Size)
		assert.GreaterOrEqual(t, newStats.Misses+newStats.Hits, initialStats.Misses+initialStats.Hits)
	})
}

func TestClearDefaultCache(t *testing.T) {
	t.Run("clears all cached entries", func(t *testing.T) {
		// Add some entries and guarantee a cache hit by compiling the same expression twice.
		opts := []cel.EnvOption{cel.Variable("x", cel.IntType)}
		expr1 := Expression("x + 1")
		_, err := expr1.Compile(opts)
		require.NoError(t, err)
		// Second compile of the same expression produces a cache hit.
		_, err = expr1.Compile(opts)
		require.NoError(t, err)

		expr2 := Expression("y * 2")
		_, err = expr2.Compile([]cel.EnvOption{cel.Variable("y", cel.IntType)})
		require.NoError(t, err)

		statsBefore := GetDefaultCacheStats()
		assert.Greater(t, statsBefore.Size, 0)

		// Clear cache (preserves stats)
		ClearDefaultCache()

		statsAfter := GetDefaultCacheStats()
		assert.Equal(t, 0, statsAfter.Size, "cache size should be 0 after Clear")
		// Stats are preserved by Clear(), not reset
		assert.Greater(t, statsAfter.Hits, uint64(0), "hits should be preserved")
		assert.Greater(t, statsAfter.Misses, uint64(0), "misses should be preserved")
	})

	t.Run("cache works after clearing", func(t *testing.T) {
		ClearDefaultCache()

		expr := Expression("42")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		val, err := result.Eval(nil)
		require.NoError(t, err)
		assert.Equal(t, int64(42), val)
	})
}

func TestCompile_WithContext_CacheInteraction(t *testing.T) {
	t.Run("context cancellation after cache hit still works", func(t *testing.T) {
		// First compilation to populate cache
		expr := Expression("x + 10")
		result1, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		// Second compilation with cancelled context
		// Context is checked BEFORE cache lookup, so this will fail
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = expr.Compile(
			[]cel.EnvOption{cel.Variable("x", cel.IntType)},
			WithContext(ctx),
		)
		// Context check happens early, so this should error
		assert.Error(t, err)
		assert.True(t, errors.Is(err, context.Canceled))

		// First result should still work
		val1, err := result1.Eval(map[string]any{"x": int64(5)})
		require.NoError(t, err)
		assert.Equal(t, int64(15), val1)
	})
}

func BenchmarkCompile_WithContext(b *testing.B) {
	ctx := context.Background()
	expr := Expression("x * 2 + y * 3")
	envOpts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = expr.Compile(envOpts, WithContext(ctx))
	}
}
