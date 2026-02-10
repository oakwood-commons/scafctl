// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"testing"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgramCache_GetDetailedStats_Empty(t *testing.T) {
	cache := NewProgramCache(10)

	stats := cache.GetDetailedStats(0)
	assert.Equal(t, 0, stats.Size)
	assert.Equal(t, uint64(0), stats.TotalAccesses)
	assert.Empty(t, stats.TopExpressions)
}

func TestProgramCache_GetDetailedStats_Basic(t *testing.T) {
	cache := NewProgramCache(10)

	// Add and access some expressions
	expressions := []string{
		"1 + 2",
		"x * 2",
		"name == 'Alice'",
	}

	for _, exprStr := range expressions {
		expr := Expression(exprStr)
		compiled, err := expr.Compile([]cel.EnvOption{cel.Variable("x", cel.IntType), cel.Variable("name", cel.StringType)})
		require.NoError(t, err)

		key := "key-" + exprStr
		cache.Put(key, compiled.Program, exprStr)
	}

	// Access expressions different amounts
	cache.Get("key-1 + 2")           // 1 hit
	cache.Get("key-1 + 2")           // 2 hits
	cache.Get("key-x * 2")           // 1 hit
	cache.Get("key-name == 'Alice'") // 1 hit
	cache.Get("key-name == 'Alice'") // 2 hits
	cache.Get("key-name == 'Alice'") // 3 hits

	stats := cache.GetDetailedStats(0) // Get all
	assert.Equal(t, 3, stats.Size)
	assert.Equal(t, uint64(6), stats.Hits)
	assert.Equal(t, uint64(6), stats.TotalAccesses)
	assert.Len(t, stats.TopExpressions, 3)

	// Should be sorted by hits (descending)
	assert.Equal(t, "name == 'Alice'", stats.TopExpressions[0].Expression)
	assert.Equal(t, uint64(3), stats.TopExpressions[0].Hits)

	assert.Equal(t, "1 + 2", stats.TopExpressions[1].Expression)
	assert.Equal(t, uint64(2), stats.TopExpressions[1].Hits)

	assert.Equal(t, "x * 2", stats.TopExpressions[2].Expression)
	assert.Equal(t, uint64(1), stats.TopExpressions[2].Hits)
}

func TestProgramCache_GetDetailedStats_TopN(t *testing.T) {
	cache := NewProgramCache(10)

	// Add 5 expressions with different access counts
	for i := 0; i < 5; i++ {
		exprStr := Expression(string(rune('a'+i)) + " + 1")
		compiled, err := exprStr.Compile([]cel.EnvOption{
			cel.Variable(string(rune('a'+i)), cel.IntType),
		})
		require.NoError(t, err)

		key := "key-" + string(rune('a'+i))
		cache.Put(key, compiled.Program, string(rune('a'+i))+" + 1")
	}

	// Access with increasing hits: a=1, b=2, c=3, d=4, e=5
	for i := 0; i < 5; i++ {
		key := "key-" + string(rune('a'+i))
		for j := 0; j <= i; j++ {
			cache.Get(key)
		}
	}

	// Get top 3
	stats := cache.GetDetailedStats(3)
	assert.Len(t, stats.TopExpressions, 3)

	// Should be e(5), d(4), c(3)
	assert.Equal(t, "e + 1", stats.TopExpressions[0].Expression)
	assert.Equal(t, uint64(5), stats.TopExpressions[0].Hits)

	assert.Equal(t, "d + 1", stats.TopExpressions[1].Expression)
	assert.Equal(t, uint64(4), stats.TopExpressions[1].Hits)

	assert.Equal(t, "c + 1", stats.TopExpressions[2].Expression)
	assert.Equal(t, uint64(3), stats.TopExpressions[2].Hits)

	// Get top 1
	stats = cache.GetDetailedStats(1)
	assert.Len(t, stats.TopExpressions, 1)
	assert.Equal(t, "e + 1", stats.TopExpressions[0].Expression)
}

func TestProgramCache_GetDetailedStats_LastAccess(t *testing.T) {
	cache := NewProgramCache(10)

	expr := Expression("1 + 2")
	compiled, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)

	cache.Put("key", compiled.Program, string(expr))

	// First access
	time1 := time.Now()
	cache.Get("key")

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Second access
	time2 := time.Now()
	cache.Get("key")

	stats := cache.GetDetailedStats(0)
	require.Len(t, stats.TopExpressions, 1)

	// LastAccess should be after time1 and around time2
	assert.True(t, stats.TopExpressions[0].LastAccess.After(time1))
	assert.True(t, stats.TopExpressions[0].LastAccess.Sub(time2) < 100*time.Millisecond)
}

func TestProgramCache_GetDetailedStats_MetricsLimit(t *testing.T) {
	cache := NewProgramCache(2000) // Large cache
	cache.metricsLimit = 10        // But limit metrics to 10

	// Add 15 expressions
	for i := 0; i < 15; i++ {
		exprStr := Expression(string(rune('a'+i)) + " + 1")
		compiled, err := exprStr.Compile([]cel.EnvOption{
			cel.Variable(string(rune('a'+i)), cel.IntType),
		})
		require.NoError(t, err)

		key := "key-" + string(rune('a'+i))
		cache.Put(key, compiled.Program, string(rune('a'+i))+" + 1")

		// Access each one to trigger metrics tracking
		cache.Get(key)
	}

	stats := cache.GetDetailedStats(0)

	// Should only track 10 expressions (the limit)
	assert.LessOrEqual(t, len(stats.TopExpressions), 10)
}

func TestProgramCache_ExpressionMetrics_Eviction(t *testing.T) {
	cache := NewProgramCache(10)
	cache.metricsLimit = 3 // Only track 3 expressions

	// Add 5 expressions
	expressions := []string{"a + 1", "b + 1", "c + 1", "d + 1", "e + 1"}
	vars := []string{"a", "b", "c", "d", "e"}

	for i, exprStr := range expressions {
		expr := Expression(exprStr)
		compiled, err := expr.Compile([]cel.EnvOption{
			cel.Variable(vars[i], cel.IntType),
		})
		require.NoError(t, err)

		cache.Put("key-"+vars[i], compiled.Program, exprStr)
	}

	// Access all 5 expressions once each
	// This will cause metrics eviction since we can only track 3
	for _, varName := range vars {
		cache.Get("key-" + varName)
	}

	stats := cache.GetDetailedStats(0)

	// Should only have 3 tracked expressions (the limit)
	assert.Equal(t, 3, len(stats.TopExpressions))

	// All tracked expressions should have at least 1 hit
	for _, stat := range stats.TopExpressions {
		assert.GreaterOrEqual(t, stat.Hits, uint64(1))
	}
}

func TestProgramCache_ClearWithStats_ResetsMetrics(t *testing.T) {
	cache := NewProgramCache(10)

	// Add expressions and generate metrics
	expr := Expression("1 + 2")
	compiled, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)

	cache.Put("key", compiled.Program, string(expr))
	cache.Get("key")

	stats := cache.GetDetailedStats(0)
	assert.Len(t, stats.TopExpressions, 1)

	// Clear with stats should reset metrics
	cache.ClearWithStats()

	stats = cache.GetDetailedStats(0)
	assert.Empty(t, stats.TopExpressions)
	assert.Equal(t, uint64(0), stats.TotalAccesses)
}

func TestProgramCache_ResetStats_ResetsMetrics(t *testing.T) {
	cache := NewProgramCache(10)

	// Add expressions and generate metrics
	expr := Expression("1 + 2")
	compiled, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)

	cache.Put("key", compiled.Program, string(expr))
	cache.Get("key")

	stats := cache.GetDetailedStats(0)
	assert.Len(t, stats.TopExpressions, 1)

	// ResetStats should also reset expression metrics
	cache.ResetStats()

	stats = cache.GetDetailedStats(0)
	assert.Empty(t, stats.TopExpressions)
	assert.Equal(t, uint64(0), stats.TotalAccesses)
}

func TestProgramCache_Stats_TotalAccesses(t *testing.T) {
	cache := NewProgramCache(10)

	expr := Expression("1 + 2")
	compiled, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)

	cache.Put("key", compiled.Program, string(expr))

	// 3 hits + 2 misses = 5 total accesses
	cache.Get("key")
	cache.Get("key")
	cache.Get("key")
	cache.Get("nonexistent")
	cache.Get("another-miss")

	stats := cache.Stats()
	assert.Equal(t, uint64(5), stats.TotalAccesses)
	assert.Equal(t, uint64(3), stats.Hits)
	assert.Equal(t, uint64(2), stats.Misses)
}
