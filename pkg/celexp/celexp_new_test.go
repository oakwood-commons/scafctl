// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultCache tests the package-level default cache functionality
func TestDefaultCache(t *testing.T) {
	t.Run("GetDefaultCache returns same instance", func(t *testing.T) {
		cache1 := GetDefaultCache()
		cache2 := GetDefaultCache()
		assert.Same(t, cache1, cache2, "should return same cache instance")
	})

	t.Run("default cache is used by Compile", func(t *testing.T) {
		// Get the default cache
		cache := GetDefaultCache()
		initialStats := cache.Stats()

		expr := Expression("1 + 2")
		result1, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)
		require.NotNil(t, result1)

		stats1 := cache.Stats()
		assert.GreaterOrEqual(t, stats1.Size, initialStats.Size, "cache should have entries")

		// Second compilation should hit cache
		result2, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)
		require.NotNil(t, result2)

		stats2 := cache.Stats()
		assert.GreaterOrEqual(t, stats2.Hits, initialStats.Hits+1, "should have cache hit")
	})

	t.Run("SetDefaultCacheSize before initialization", func(t *testing.T) {
		// This test verifies the function works
		// In real usage, SetDefaultCacheSize would be called before any Compile()
		// Note: Once initialized, this has no effect, but doesn't error
		SetDefaultCacheSize(500)
		// Function works without error (though may not have effect if already initialized)
	})
}

// TestCompileWithDefaultCache tests that Compile uses caching by default
func TestCompileWithDefaultCache(t *testing.T) {
	t.Run("Compile caches by default", func(t *testing.T) {
		expr := Expression("x * 2")
		opts := []cel.EnvOption{cel.Variable("x", cel.IntType)}

		// First compilation
		result1, err := expr.Compile(opts)
		require.NoError(t, err)
		val1, err := EvalAs[int64](result1, map[string]any{"x": int64(5)})
		require.NoError(t, err)
		assert.Equal(t, int64(10), val1)

		// Second compilation should hit cache (same options)
		result2, err := expr.Compile(opts)
		require.NoError(t, err)
		val2, err := EvalAs[int64](result2, map[string]any{"x": int64(7)})
		require.NoError(t, err)
		assert.Equal(t, int64(14), val2)
	})
}

// TestTypeConversionHelpers tests the new type-specific evaluation methods
func TestTypeConversionHelpers(t *testing.T) {
	t.Run("EvalAs[string]", func(t *testing.T) {
		expr := Expression("'hello' + ' ' + 'world'")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		str, err := EvalAs[string](result, nil)
		require.NoError(t, err)
		assert.Equal(t, "hello world", str)
	})

	t.Run("EvalAs[string] with wrong type", func(t *testing.T) {
		expr := Expression("42")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[string](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not string")
	})

	t.Run("EvalAs[bool]", func(t *testing.T) {
		expr := Expression("x > 10")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType)})
		require.NoError(t, err)

		b, err := EvalAs[bool](result, map[string]any{"x": int64(15)})
		require.NoError(t, err)
		assert.True(t, b)

		b, err = EvalAs[bool](result, map[string]any{"x": int64(5)})
		require.NoError(t, err)
		assert.False(t, b)
	})

	t.Run("EvalAs[bool] with wrong type", func(t *testing.T) {
		expr := Expression("'true'")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[bool](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not bool")
	})

	t.Run("EvalAs[int64]", func(t *testing.T) {
		expr := Expression("x + y")
		result, err := expr.Compile([]cel.EnvOption{
			cel.Variable("x", cel.IntType),
			cel.Variable("y", cel.IntType),
		})
		require.NoError(t, err)

		val, err := EvalAs[int64](result, map[string]any{"x": int64(10), "y": int64(20)})
		require.NoError(t, err)
		assert.Equal(t, int64(30), val)
	})

	t.Run("EvalAs[int64] with wrong type", func(t *testing.T) {
		expr := Expression("3.14")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[int64](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not int64")
	})

	t.Run("EvalAs[float64]", func(t *testing.T) {
		expr := Expression("x * 1.5")
		result, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.DoubleType)})
		require.NoError(t, err)

		val, err := EvalAs[float64](result, map[string]any{"x": 10.0})
		require.NoError(t, err)
		assert.Equal(t, 15.0, val)
	})

	t.Run("EvalAs[float64] with wrong type", func(t *testing.T) {
		expr := Expression("42")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[float64](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not float64")
	})

	t.Run("EvalAs[[]any]", func(t *testing.T) {
		expr := Expression("[1, 2, 3, 4]")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		list, err := EvalAs[[]any](result, nil)
		require.NoError(t, err)
		assert.Len(t, list, 4)
		assert.Equal(t, int64(1), list[0])
		assert.Equal(t, int64(4), list[3])
	})

	t.Run("EvalAs[[]any] with wrong type", func(t *testing.T) {
		expr := Expression("'not a list'")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[[]any](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not []interface {}")
	})

	t.Run("EvalAs[map[string]any]", func(t *testing.T) {
		expr := Expression("{\"name\": \"John\", \"age\": 30}")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		m, err := EvalAs[map[string]any](result, nil)
		require.NoError(t, err)
		assert.Equal(t, "John", m["name"])
		assert.Equal(t, int64(30), m["age"])
	})

	t.Run("EvalAs[map[string]any] with wrong type", func(t *testing.T) {
		expr := Expression("'not a map'")
		result, err := expr.Compile([]cel.EnvOption{})
		require.NoError(t, err)

		_, err = EvalAs[map[string]any](result, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not map[string]interface {}")
	})
}

// TestCostLimitProtection tests that cost limits prevent expensive expressions
func TestCostLimitProtection(t *testing.T) {
	t.Run("very low cost limit triggers error", func(t *testing.T) {
		// Create an expression that will exceed a very low cost limit
		expr := Expression("[1,2,3,4,5].map(x, x * 2)")
		lowCost := uint64(1) // Extremely low limit

		result, err := expr.Compile(
			[]cel.EnvOption{},
			WithCostLimit(lowCost),
		)
		require.NoError(t, err)

		// Evaluation should fail due to cost limit
		_, err = result.Eval(nil)
		assert.Error(t, err)
		// The error contains "operation cancelled" when cost limit exceeded
		assert.Contains(t, err.Error(), "cost limit")
	})

	t.Run("reasonable cost limit allows normal expressions", func(t *testing.T) {
		expr := Expression("[1,2,3].map(x, x * 2)")
		reasonableCost := uint64(10000)

		result, err := expr.Compile(
			[]cel.EnvOption{},
			WithCostLimit(reasonableCost),
		)
		require.NoError(t, err)

		list, err := EvalAs[[]any](result, nil)
		require.NoError(t, err)
		assert.Len(t, list, 3)
	})
}
