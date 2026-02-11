// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// Validate validates the entire configuration.
// Returns an error if any configuration value is invalid.
func (c *Config) Validate() error {
	// Warn about missing or outdated version (but don't fail)
	// This is handled by the caller if needed

	// Validate global HTTP client config
	if err := c.HTTPClient.Validate(); err != nil {
		return fmt.Errorf("httpClient: %w", err)
	}

	// Validate CEL config
	if err := c.CEL.Validate(); err != nil {
		return fmt.Errorf("cel: %w", err)
	}

	// Validate resolver config
	if err := c.Resolver.Validate(); err != nil {
		return fmt.Errorf("resolver: %w", err)
	}

	// Validate action config
	if err := c.Action.Validate(); err != nil {
		return fmt.Errorf("action: %w", err)
	}

	// Validate each catalog
	for i, catalog := range c.Catalogs {
		if err := catalog.Validate(); err != nil {
			return fmt.Errorf("catalogs[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate validates a catalog configuration.
func (c *CatalogConfig) Validate() error {
	// Validate catalog type if specified
	if c.Type != "" && !IsValidCatalogType(c.Type) {
		return fmt.Errorf("type: invalid value %q, must be one of: %v", c.Type, ValidCatalogTypes())
	}

	// Validate per-catalog HTTP client config if present
	if c.HTTPClient != nil {
		if err := c.HTTPClient.Validate(); err != nil {
			return fmt.Errorf("httpClient: %w", err)
		}
	}

	return nil
}

// Validate validates the HTTP client configuration.
// Returns an error if any value is invalid.
func (h *HTTPClientConfig) Validate() error {
	// Validate duration fields
	durationFields := map[string]string{
		"timeout":                   h.Timeout,
		"retryWaitMin":              h.RetryWaitMin,
		"retryWaitMax":              h.RetryWaitMax,
		"cacheTTL":                  h.CacheTTL,
		"circuitBreakerOpenTimeout": h.CircuitBreakerOpenTimeout,
	}

	for field, value := range durationFields {
		if value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
			}
		}
	}

	// Validate cache type
	if h.CacheType != "" && !IsValidHTTPClientCacheType(h.CacheType) {
		return fmt.Errorf("cacheType: invalid value %q, must be one of: %v", h.CacheType, ValidHTTPClientCacheTypes())
	}

	// Validate numeric ranges
	if h.RetryMax < 0 {
		return fmt.Errorf("retryMax: must be non-negative, got %d", h.RetryMax)
	}
	if h.MemoryCacheSize < 0 {
		return fmt.Errorf("memoryCacheSize: must be non-negative, got %d", h.MemoryCacheSize)
	}
	if h.MaxCacheFileSize < 0 {
		return fmt.Errorf("maxCacheFileSize: must be non-negative, got %d", h.MaxCacheFileSize)
	}
	if h.CircuitBreakerMaxFailures < 0 {
		return fmt.Errorf("circuitBreakerMaxFailures: must be non-negative, got %d", h.CircuitBreakerMaxFailures)
	}
	if h.CircuitBreakerHalfOpenMaxRequests < 0 {
		return fmt.Errorf("circuitBreakerHalfOpenMaxRequests: must be non-negative, got %d", h.CircuitBreakerHalfOpenMaxRequests)
	}

	return nil
}

// Validate validates the CEL configuration.
// Returns an error if any value is invalid.
func (c *CELConfig) Validate() error {
	// Validate cache size
	if c.CacheSize < 0 {
		return fmt.Errorf("cacheSize: must be non-negative, got %d", c.CacheSize)
	}

	// Validate cost limit
	if c.CostLimit < 0 {
		return fmt.Errorf("costLimit: must be non-negative, got %d", c.CostLimit)
	}

	return nil
}

// Validate validates the resolver configuration.
// Returns an error if any value is invalid.
func (r *ResolverConfig) Validate() error {
	// Validate duration fields
	durationFields := map[string]string{
		"timeout":      r.Timeout,
		"phaseTimeout": r.PhaseTimeout,
	}

	for field, value := range durationFields {
		if value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
			}
		}
	}

	// Validate numeric ranges
	if r.MaxConcurrency < 0 {
		return fmt.Errorf("maxConcurrency: must be non-negative, got %d", r.MaxConcurrency)
	}
	if r.WarnValueSize < 0 {
		return fmt.Errorf("warnValueSize: must be non-negative, got %d", r.WarnValueSize)
	}
	if r.MaxValueSize < 0 {
		return fmt.Errorf("maxValueSize: must be non-negative, got %d", r.MaxValueSize)
	}

	return nil
}

// Validate validates the action configuration.
// Returns an error if any value is invalid.
func (a *ActionConfig) Validate() error {
	// Validate duration fields
	durationFields := map[string]string{
		"defaultTimeout": a.DefaultTimeout,
		"gracePeriod":    a.GracePeriod,
	}

	for field, value := range durationFields {
		if value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
			}
		}
	}

	// Validate numeric ranges
	if a.MaxConcurrency < 0 {
		return fmt.Errorf("maxConcurrency: must be non-negative, got %d", a.MaxConcurrency)
	}

	return nil
}

// Validate validates the logging configuration.
// Returns an error if any value is invalid.
func (l *LoggingConfig) Validate() error {
	// Validate log level (must be a recognized named level or numeric V-level)
	if l.Level != "" {
		if _, err := logger.ParseLogLevel(l.Level); err != nil {
			return fmt.Errorf("level: %w", err)
		}
	}

	// Validate format
	if l.Format != "" && l.Format != LoggingFormatJSON && l.Format != LoggingFormatText && l.Format != LoggingFormatConsole {
		return fmt.Errorf("format: must be %q, %q, or %q, got %q", LoggingFormatConsole, LoggingFormatJSON, LoggingFormatText, l.Format)
	}

	return nil
}

// CheckVersion checks if the config version is current and returns a warning message if not.
// Returns an empty string if the version is current or if version checking should be skipped.
func (c *Config) CheckVersion() string {
	if c.Version == 0 {
		return "config file has no version specified, consider adding 'version: 1'"
	}
	if c.Version < CurrentConfigVersion {
		return fmt.Sprintf("config file version %d is outdated, current version is %d", c.Version, CurrentConfigVersion)
	}
	if c.Version > CurrentConfigVersion {
		return fmt.Sprintf("config file version %d is newer than supported version %d, some features may not work", c.Version, CurrentConfigVersion)
	}
	return ""
}
