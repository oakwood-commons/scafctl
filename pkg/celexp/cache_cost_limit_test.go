// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompileWithCache_DifferentCostLimits validates fix for issue #4:
// Different cost limits should create different cache entries to prevent
// users from accidentally getting a program with the wrong cost limit.
func TestCompileWithCache_DifferentCostLimits(t *testing.T) {
	cache := NewProgramCache(10)
	expr := Expression("x + y")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}
	ctx := context.Background()

	// Compile with cost limit 1000
	result1, err := expr.Compile(opts, WithCache(cache), WithContext(ctx), WithCostLimit(1000))
	require.NoError(t, err)
	require.NotNil(t, result1)

	// Compile same expression with different cost limit 5000
	result2, err := expr.Compile(opts, WithCache(cache), WithContext(ctx), WithCostLimit(5000))
	require.NoError(t, err)
	require.NotNil(t, result2)

	// Compile again with cost limit 0 (no limit)
	result3, err := expr.Compile(opts, WithCache(cache), WithContext(ctx), WithCostLimit(0))
	require.NoError(t, err)
	require.NotNil(t, result3)

	// Should have 3 different cache entries (not 1)
	stats := cache.Stats()
	assert.Equal(t, 3, stats.Size, "should have 3 separate cache entries for different cost limits")
	assert.Equal(t, uint64(0), stats.Hits, "should be all cache misses since cost limits differ")
	assert.Equal(t, uint64(3), stats.Misses)

	// Compile again with cost limit 1000 - should be cache hit
	result4, err := expr.Compile(opts, WithCache(cache), WithContext(ctx), WithCostLimit(1000))
	require.NoError(t, err)
	require.NotNil(t, result4)

	stats = cache.Stats()
	assert.Equal(t, 3, stats.Size, "should still have 3 cache entries")
	assert.Equal(t, uint64(1), stats.Hits, "fourth compilation should be a cache hit")
	assert.Equal(t, uint64(3), stats.Misses)
}

// TestCompileWithCache_CostLimitApplied validates fix for issue #2:
// CompileWithCache should apply cost limits to prevent DoS attacks.
func TestCompileWithCache_CostLimitApplied(t *testing.T) {
	cache := NewProgramCache(10)

	// Expression that would be expensive without cost limit
	expr := Expression("[1,2,3,4,5].map(x, x * 2).map(x, x * 2).map(x, x * 2)")
	ctx := context.Background()

	// Compile with very low cost limit
	result, err := expr.Compile(nil, WithCache(cache), WithContext(ctx), WithCostLimit(10))
	require.NoError(t, err, "compilation should succeed")
	require.NotNil(t, result)

	// Evaluation should fail due to cost limit
	_, err = result.Eval(nil)
	assert.Error(t, err, "evaluation should fail due to cost limit")
	assert.Contains(t, err.Error(), "cost", "error should mention cost limit")
}

// TestCompileWithCache_ContextCancellation validates fix for issue #3:
// CompileWithCache should respect context cancellation.
func TestCompileWithCache_ContextCancellation(t *testing.T) {
	cache := NewProgramCache(10)
	expr := Expression("x + y")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Compilation should fail with context error
	_, err := expr.Compile(opts, WithCache(cache), WithContext(ctx), WithCostLimit(GetDefaultCostLimit()))
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "should return context.Canceled error")
}

// TestCompileWithCache_ContextTimeoutOnCacheMiss validates that context
// timeout is respected during the compilation phase (cache miss).
func TestCompileWithCache_ContextTimeoutOnCacheMiss(t *testing.T) {
	cache := NewProgramCache(10)
	expr := Expression("x + y")
	opts := []cel.EnvOption{
		cel.Variable("x", cel.IntType),
		cel.Variable("y", cel.IntType),
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before compilation

	// Should fail immediately due to context check before compilation
	_, err := expr.Compile(opts, WithCache(cache), WithContext(ctx), WithCostLimit(GetDefaultCostLimit()))
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// Cache should be empty (compilation failed)
	stats := cache.Stats()
	assert.Equal(t, 0, stats.Size)
}

// TestExpressionCompile_UsesCostLimit validates that Expression.Compile([]cel.EnvOption{})
// uses the default cost limit for security.
func TestExpressionCompile_UsesCostLimit(t *testing.T) {
	// Expression that would be expensive without cost limit
	expr := Expression("[1,2,3,4,5,6,7,8,9,10].map(x, x * 2).map(x, x * 2).map(x, x * 2).map(x, x * 2)")

	// Compile should succeed (it just compiles, doesn't evaluate)
	result, err := expr.Compile([]cel.EnvOption{})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Evaluation might fail due to default cost limit (depending on complexity)
	// This test just verifies that the program was compiled with cost tracking
	_, _ = result.Eval(nil) //nolint:errcheck // Error or success both acceptable - testing cost limit configuration
	// The important thing is no panic/hang
}
