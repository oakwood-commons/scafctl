// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"time"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// parseDurationOr parses s as a time.Duration, returning fallback when s is
// empty or unparseable. Parse failures are logged with the given field name.
func parseDurationOr(s string, fallback time.Duration, logger logr.Logger, field string) time.Duration {
	if s == "" {
		return fallback
	}
	if logger.GetSink() == nil {
		logger = logr.Discard()
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		logger.Error(err, "invalid duration, using default", "field", field, "value", s)
		return fallback
	}
	return d
}

// NewClientFromAppConfig creates an httpc.Client from a scafctl config.HTTPClientConfig.
// It uses scafctl-specific defaults (XDG cache dir, app-name-based prefix, OTel metrics)
// as the base, then overlays the string-based config values.
//
// The cfg parameter can be nil, in which case scafctl defaults are used.
func NewClientFromAppConfig(cfg *config.HTTPClientConfig, logger logr.Logger) *Client {
	clientCfg := DefaultConfig()
	clientCfg.Logger = logger

	if cfg == nil {
		return NewClient(clientCfg)
	}

	clientCfg.Timeout = parseDurationOr(cfg.Timeout, clientCfg.Timeout, logger, "timeout")
	if cfg.RetryMax > 0 {
		clientCfg.RetryMax = cfg.RetryMax
	}
	clientCfg.RetryWaitMin = parseDurationOr(cfg.RetryWaitMin, clientCfg.RetryWaitMin, logger, "retryWaitMin")
	clientCfg.RetryWaitMax = parseDurationOr(cfg.RetryWaitMax, clientCfg.RetryWaitMax, logger, "retryWaitMax")

	if cfg.EnableCache != nil {
		clientCfg.EnableCache = *cfg.EnableCache
	}
	if cfg.CacheType != "" {
		clientCfg.CacheType = CacheType(cfg.CacheType)
	}
	if cfg.CacheDir != "" {
		clientCfg.CacheDir = cfg.CacheDir
	}
	clientCfg.CacheTTL = parseDurationOr(cfg.CacheTTL, clientCfg.CacheTTL, logger, "cacheTTL")
	if cfg.CacheKeyPrefix != "" {
		clientCfg.CacheKeyPrefix = cfg.CacheKeyPrefix
	}
	if cfg.MaxCacheFileSize > 0 {
		clientCfg.MaxCacheFileSize = cfg.MaxCacheFileSize
	}
	if cfg.MemoryCacheSize > 0 {
		clientCfg.MemoryCacheSize = cfg.MemoryCacheSize
	}

	if cfg.EnableCircuitBreaker != nil {
		clientCfg.EnableCircuitBreaker = *cfg.EnableCircuitBreaker
	}
	if cfg.CircuitBreakerMaxFailures > 0 || cfg.CircuitBreakerOpenTimeout != "" || cfg.CircuitBreakerHalfOpenMaxRequests > 0 {
		clientCfg.CircuitBreakerConfig = DefaultCircuitBreakerConfig()
		if cfg.CircuitBreakerMaxFailures > 0 {
			clientCfg.CircuitBreakerConfig.MaxFailures = cfg.CircuitBreakerMaxFailures
		}
		clientCfg.CircuitBreakerConfig.OpenTimeout = parseDurationOr(
			cfg.CircuitBreakerOpenTimeout,
			clientCfg.CircuitBreakerConfig.OpenTimeout,
			logger, "circuitBreakerOpenTimeout",
		)
		if cfg.CircuitBreakerHalfOpenMaxRequests > 0 {
			clientCfg.CircuitBreakerConfig.HalfOpenMaxRequests = cfg.CircuitBreakerHalfOpenMaxRequests
		}
	}

	if cfg.EnableCompression != nil {
		clientCfg.EnableCompression = *cfg.EnableCompression
	}
	// NOTE: cfg.AllowPrivateIPs from app config is consumed only by
	// PrivateIPsAllowed(ctx) at the application layer; it is NOT forwarded to
	// the upstream transport, which is always set to AllowPrivateIPs=true by
	// NewClient() to avoid double-gating.
	if cfg.MaxResponseBodySize > 0 {
		clientCfg.MaxResponseBodySize = cfg.MaxResponseBodySize
	}

	return NewClient(clientCfg)
}

// PrivateIPsAllowed returns true when the application config stored in ctx permits
// HTTP requests to private/loopback/link-local IP addresses.
// Returns false (deny) when no config is present -- secure by default.
func PrivateIPsAllowed(ctx context.Context) bool {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return false
	}
	if cfg.HTTPClient.AllowPrivateIPs != nil {
		return *cfg.HTTPClient.AllowPrivateIPs
	}
	return false
}
