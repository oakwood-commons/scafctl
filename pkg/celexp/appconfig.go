// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"context"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// CELConfigInput holds the configuration values for CEL initialization.
// This mirrors config.CELConfig but avoids circular dependencies.
type CELConfigInput struct {
	// CacheSize is the maximum number of compiled programs to cache
	CacheSize int `json:"cacheSize" yaml:"cacheSize" doc:"Maximum number of compiled programs to cache" maximum:"100000" example:"10000"`
	// CostLimit is the cost limit for expression evaluation (0 = disabled)
	CostLimit int64 `json:"costLimit" yaml:"costLimit" doc:"Cost limit for expression evaluation (0 = disabled)" example:"1000000"`
	// UseASTBasedCaching enables AST-based cache key generation
	UseASTBasedCaching bool `json:"useASTBasedCaching" yaml:"useASTBasedCaching" doc:"Enable AST-based cache key generation"`
	// EnableMetrics enables expression metrics collection
	EnableMetrics bool `json:"enableMetrics" yaml:"enableMetrics" doc:"Enable expression metrics collection"`
}

var (
	// appConfigMu protects the app config initialization state
	appConfigMu sync.Mutex
	// appConfigInitialized tracks whether InitFromAppConfig has been called
	appConfigInitialized bool

	// appConfigCache stores the configured cache for InitFromAppConfig
	appConfigCache *ProgramCache
)

// InitFromAppConfig initializes the CEL subsystem with application configuration.
// This should be called once during application startup, before any CEL expressions
// are evaluated.
//
// This function:
//   - Creates a new program cache with the specified size and AST caching option
//   - Sets the default cost limit
//   - Registers the cache as the default cache factory
//
// The function is idempotent - subsequent calls after the first are no-ops.
//
// Example:
//
//	celexp.InitFromAppConfig(ctx, celexp.CELConfigInput{
//	    CacheSize:          10000,
//	    CostLimit:          1000000,
//	    UseASTBasedCaching: true,
//	    EnableMetrics:      true,
//	})
func InitFromAppConfig(ctx context.Context, cfg CELConfigInput) {
	appConfigMu.Lock()
	defer appConfigMu.Unlock()
	if !appConfigInitialized {
		initFromAppConfigInternal(ctx, cfg)
		appConfigInitialized = true
	}
}

// initFromAppConfigInternal performs the actual initialization.
// This is separated to allow testing with ResetForTesting().
func initFromAppConfigInternal(ctx context.Context, cfg CELConfigInput) {
	lgr := logger.FromContext(ctx)

	// Use default values if not specified
	cacheSize := cfg.CacheSize
	if cacheSize <= 0 {
		cacheSize = DefaultCacheSize
	}

	// Create the cache with the configured options
	var cacheOpts []CacheOption
	if cfg.UseASTBasedCaching {
		cacheOpts = append(cacheOpts, WithASTBasedCaching(true))
	}

	appConfigCache = NewProgramCache(cacheSize, cacheOpts...)

	// Set the default cost limit
	if cfg.CostLimit > 0 {
		SetDefaultCostLimit(uint64(cfg.CostLimit))
	} else if cfg.CostLimit == 0 {
		// Explicitly set to 0 means disable cost limiting
		SetDefaultCostLimit(0)
	}

	// Register the cache factory so GetDefaultCache() uses our configured cache
	SetCacheFactory(func() *ProgramCache {
		return appConfigCache
	})

	lgr.V(1).Info("initialized CEL from app config",
		"cacheSize", cacheSize,
		"costLimit", cfg.CostLimit,
		"useASTBasedCaching", cfg.UseASTBasedCaching,
		"enableMetrics", cfg.EnableMetrics)
}

// ResetForTesting resets the app config state for testing purposes.
// This should only be called from tests.
func ResetForTesting() {
	appConfigMu.Lock()
	appConfigInitialized = false
	appConfigCache = nil
	appConfigMu.Unlock()
	// Also reset the cache factory
	cacheFactoryMu.Lock()
	cacheFactoryInitialized = false
	cacheFactory = nil
	cacheFactoryMu.Unlock()
}

// GetAppConfigCache returns the cache created by InitFromAppConfig.
// Returns nil if InitFromAppConfig has not been called.
func GetAppConfigCache() *ProgramCache {
	return appConfigCache
}
