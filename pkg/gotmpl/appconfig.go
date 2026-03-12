// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"context"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// GoTemplateConfigInput holds the configuration values for Go template initialization.
// This mirrors config.GoTemplateConfig but avoids circular dependencies.
type GoTemplateConfigInput struct {
	// CacheSize is the maximum number of compiled templates to cache
	CacheSize int `json:"cacheSize" yaml:"cacheSize" doc:"Maximum number of compiled templates to cache" maximum:"100000" example:"10000"`
	// EnableMetrics enables template cache metrics collection
	EnableMetrics bool `json:"enableMetrics" yaml:"enableMetrics" doc:"Enable template cache metrics collection"`
}

var (
	// appConfigMu protects the app config initialization state
	appConfigMu sync.Mutex
	// appConfigInitialized tracks whether InitFromAppConfig has been called
	appConfigInitialized bool

	// appConfigCache stores the configured cache for InitFromAppConfig
	appConfigCache *TemplateCache
)

// InitFromAppConfig initializes the Go template subsystem with application configuration.
// This should be called once during application startup, before any Go templates
// are evaluated.
//
// This function:
//   - Creates a new template cache with the specified size
//   - Registers the cache as the default cache via SetCacheFactory
//
// The function is idempotent - subsequent calls after the first are no-ops.
//
// Example:
//
//	gotmpl.InitFromAppConfig(ctx, gotmpl.GoTemplateConfigInput{
//	    CacheSize:     10000,
//	    EnableMetrics: true,
//	})
func InitFromAppConfig(ctx context.Context, cfg GoTemplateConfigInput) {
	appConfigMu.Lock()
	defer appConfigMu.Unlock()
	if !appConfigInitialized {
		initFromAppConfigInternal(ctx, cfg)
		appConfigInitialized = true
	}
}

// initFromAppConfigInternal performs the actual initialization.
// This is separated to allow testing with ResetAppConfigForTesting().
func initFromAppConfigInternal(ctx context.Context, cfg GoTemplateConfigInput) {
	lgr := logger.FromContext(ctx)

	// Use default values if not specified
	cacheSize := cfg.CacheSize
	if cacheSize <= 0 {
		cacheSize = DefaultTemplateCacheSize
	}

	// Create the cache with the configured size
	appConfigCache = NewTemplateCache(cacheSize)

	// Register the cache factory so GetDefaultCache() uses our configured cache
	SetCacheFactory(func() *TemplateCache {
		return appConfigCache
	})

	lgr.V(1).Info("initialized Go template cache from app config",
		"cacheSize", cacheSize,
		"enableMetrics", cfg.EnableMetrics)
}

// ResetAppConfigForTesting resets the app config state for testing purposes.
// This should only be called from tests.
func ResetAppConfigForTesting() {
	appConfigMu.Lock()
	appConfigInitialized = false
	appConfigCache = nil
	appConfigMu.Unlock()
	// Also reset the cache factory and default cache
	cacheFactoryMu.Lock()
	cacheFactoryInitialized = false
	cacheFactory = nil
	cacheFactoryMu.Unlock()
	defaultTemplateCacheMu.Lock()
	defaultTemplateCacheInitialized = false
	defaultTemplateCache = nil
	defaultTemplateCacheMu.Unlock()
}

// GetAppConfigCache returns the cache created by InitFromAppConfig.
// Returns nil if InitFromAppConfig has not been called.
func GetAppConfigCache() *TemplateCache {
	return appConfigCache
}
