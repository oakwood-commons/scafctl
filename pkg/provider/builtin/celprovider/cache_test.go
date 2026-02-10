// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/env"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCelProvider_Execute_CacheUtilization verifies that the CEL provider
// uses the global cache and that repeated evaluations hit the cache
func TestCelProvider_Execute_CacheUtilization(t *testing.T) {
	// Register factories (normally done at app startup)
	celexp.SetEnvFactory(env.New)
	celexp.SetCacheFactory(env.GlobalCache)

	cache := env.GlobalCache()
	require.NotNil(t, cache, "Global cache should be initialized")

	// Clear cache and reset stats
	cache.ClearWithStats()

	p := NewCelProvider()
	expression := "_.x * 2 + _.y"

	ctx1 := provider.WithResolverContext(context.Background(), map[string]any{
		"x": 10,
		"y": 5,
	})

	inputs := map[string]any{
		"expression": expression,
	}

	// First execution - should be a cache miss
	result1, err := p.Execute(ctx1, inputs)
	require.NoError(t, err)
	require.NotNil(t, result1)
	assert.Equal(t, int64(25), result1.Data)

	stats1 := cache.Stats()
	assert.Equal(t, uint64(1), stats1.Misses, "First execution should be a cache miss")
	assert.Equal(t, uint64(0), stats1.Hits, "First execution should not have cache hits")

	// Second execution with same expression but different values - should be a cache hit
	ctx2 := provider.WithResolverContext(context.Background(), map[string]any{
		"x": 20,
		"y": 10,
	})

	result2, err := p.Execute(ctx2, inputs)
	require.NoError(t, err)
	require.NotNil(t, result2)
	assert.Equal(t, int64(50), result2.Data)

	stats2 := cache.Stats()
	assert.Equal(t, uint64(1), stats2.Misses, "Should still have only 1 cache miss")
	assert.Equal(t, uint64(1), stats2.Hits, "Second execution should be a cache hit")

	// Third execution - another cache hit
	ctx3 := provider.WithResolverContext(context.Background(), map[string]any{
		"x": 100,
		"y": 200,
	})

	result3, err := p.Execute(ctx3, inputs)
	require.NoError(t, err)
	require.NotNil(t, result3)
	assert.Equal(t, int64(400), result3.Data)

	stats3 := cache.Stats()
	assert.Equal(t, uint64(1), stats3.Misses, "Should still have only 1 cache miss")
	assert.Equal(t, uint64(2), stats3.Hits, "Third execution should be another cache hit")
}

// TestCelProvider_Execute_CacheDifferentExpressions verifies that different
// expressions result in different cache entries
func TestCelProvider_Execute_CacheDifferentExpressions(t *testing.T) {
	// Register factories (normally done at app startup)
	celexp.SetEnvFactory(env.New)
	celexp.SetCacheFactory(env.GlobalCache)

	cache := env.GlobalCache()
	require.NotNil(t, cache, "Global cache should be initialized")

	// Clear cache and reset stats
	cache.ClearWithStats()

	p := NewCelProvider()

	ctx := provider.WithResolverContext(context.Background(), map[string]any{"x": 10})

	// First expression
	inputs1 := map[string]any{"expression": "_.x * 2"}
	result1, err := p.Execute(ctx, inputs1)
	require.NoError(t, err)
	assert.Equal(t, int64(20), result1.Data)

	stats1 := cache.Stats()
	assert.Equal(t, uint64(1), stats1.Misses, "First expression should be a cache miss")

	// Different expression - should be another cache miss
	inputs2 := map[string]any{"expression": "_.x + 5"}
	result2, err := p.Execute(ctx, inputs2)
	require.NoError(t, err)
	assert.Equal(t, int64(15), result2.Data)

	stats2 := cache.Stats()
	assert.Equal(t, uint64(2), stats2.Misses, "Different expression should be another cache miss")

	// Repeat first expression - should be a cache hit
	result3, err := p.Execute(ctx, inputs1)
	require.NoError(t, err)
	assert.Equal(t, int64(20), result3.Data)

	stats3 := cache.Stats()
	assert.Equal(t, uint64(2), stats3.Misses, "Should still have 2 cache misses")
	assert.Equal(t, uint64(1), stats3.Hits, "Repeated expression should be a cache hit")
}
