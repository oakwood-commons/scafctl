// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitFromAppConfig(t *testing.T) {
	t.Run("creates cache with configured size", func(t *testing.T) {
		ResetAppConfigForTesting()
		t.Cleanup(ResetAppConfigForTesting)

		lgr := logger.GetWithOptions(logger.Options{Level: logger.LogLevelNone})
		ctx := logger.WithLogger(context.Background(), lgr)

		InitFromAppConfig(ctx, GoTemplateConfigInput{
			CacheSize:     500,
			EnableMetrics: true,
		})

		cache := GetAppConfigCache()
		require.NotNil(t, cache)
		assert.Equal(t, 500, cache.maxSize)
	})

	t.Run("idempotent - second call is no-op", func(t *testing.T) {
		ResetAppConfigForTesting()
		t.Cleanup(ResetAppConfigForTesting)

		lgr := logger.GetWithOptions(logger.Options{Level: logger.LogLevelNone})
		ctx := logger.WithLogger(context.Background(), lgr)

		InitFromAppConfig(ctx, GoTemplateConfigInput{
			CacheSize: 500,
		})

		// Second call with different size should be no-op
		InitFromAppConfig(ctx, GoTemplateConfigInput{
			CacheSize: 999,
		})

		cache := GetAppConfigCache()
		require.NotNil(t, cache)
		assert.Equal(t, 500, cache.maxSize, "second call should not change cache size")
	})

	t.Run("uses default size when not specified", func(t *testing.T) {
		ResetAppConfigForTesting()
		t.Cleanup(ResetAppConfigForTesting)

		lgr := logger.GetWithOptions(logger.Options{Level: logger.LogLevelNone})
		ctx := logger.WithLogger(context.Background(), lgr)

		InitFromAppConfig(ctx, GoTemplateConfigInput{
			CacheSize: 0, // Should use default
		})

		cache := GetAppConfigCache()
		require.NotNil(t, cache)
		assert.Equal(t, DefaultTemplateCacheSize, cache.maxSize)
	})

	t.Run("registers cache factory for GetDefaultCache", func(t *testing.T) {
		ResetAppConfigForTesting()
		t.Cleanup(ResetAppConfigForTesting)

		lgr := logger.GetWithOptions(logger.Options{Level: logger.LogLevelNone})
		ctx := logger.WithLogger(context.Background(), lgr)

		InitFromAppConfig(ctx, GoTemplateConfigInput{
			CacheSize: 750,
		})

		// GetDefaultCache should return the app-config cache
		defaultCache := GetDefaultCache()
		appCache := GetAppConfigCache()
		assert.Same(t, appCache, defaultCache, "GetDefaultCache should return the app-configured cache")
	})
}

func TestGetAppConfigCache_BeforeInit(t *testing.T) {
	ResetAppConfigForTesting()
	t.Cleanup(ResetAppConfigForTesting)

	cache := GetAppConfigCache()
	assert.Nil(t, cache, "should be nil before InitFromAppConfig is called")
}

func TestResetAppConfigForTesting(t *testing.T) {
	lgr := logger.GetWithOptions(logger.Options{Level: logger.LogLevelNone})
	ctx := logger.WithLogger(context.Background(), lgr)

	InitFromAppConfig(ctx, GoTemplateConfigInput{
		CacheSize: 500,
	})

	ResetAppConfigForTesting()

	assert.Nil(t, GetAppConfigCache(), "cache should be nil after reset")

	// Should be able to init again after reset
	InitFromAppConfig(ctx, GoTemplateConfigInput{
		CacheSize: 250,
	})

	cache := GetAppConfigCache()
	require.NotNil(t, cache)
	assert.Equal(t, 250, cache.maxSize)

	// Cleanup
	ResetAppConfigForTesting()
}
