// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package httpc is a thin adapter over the standalone github.com/oakwood-commons/httpc
// library, adding scafctl-specific defaults (XDG cache paths, app-name-based cache key prefix)
// and bridging application-level concerns (OTel metrics, config.FromContext, etc.).
//
// All consumer code continues to import "github.com/oakwood-commons/scafctl/pkg/httpc"
// with no changes required.
package httpc

import (
	"time"

	"github.com/hashicorp/go-retryablehttp"

	upstream "github.com/oakwood-commons/httpc"

	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// Type aliases re-export upstream types so consumers need no import changes.
type (
	Client               = upstream.Client
	ClientConfig         = upstream.ClientConfig
	CacheType            = upstream.CacheType
	CircuitBreakerConfig = upstream.CircuitBreakerConfig
	FileCacheConfig      = upstream.FileCacheConfig
	FileCache            = upstream.FileCache
	CacheStats           = upstream.CacheStats
	IPPolicy             = upstream.IPPolicy
	Metrics              = upstream.Metrics
	NoopMetrics          = upstream.NoopMetrics
	RequestHook          = upstream.RequestHook
	ResponseHook         = upstream.ResponseHook
)

// Cache type constants.
const (
	CacheTypeMemory     = upstream.CacheTypeMemory
	CacheTypeFilesystem = upstream.CacheTypeFilesystem
)

// Sentinel errors.
var (
	ErrCircuitBreakerOpen        = upstream.ErrCircuitBreakerOpen
	ErrCacheSizeLimitExceeded    = upstream.ErrCacheSizeLimitExceeded
	ErrDecompressionBombDetected = upstream.ErrDecompressionBombDetected
	ErrResponseBodyTooLarge      = upstream.ErrResponseBodyTooLarge
)

// Constant re-exports.
const DefaultMaxRedirects = upstream.DefaultMaxRedirects

// Function re-exports that have no scafctl-specific behaviour.
var (
	NewFileCache                = upstream.NewFileCache
	DefaultCircuitBreakerConfig = upstream.DefaultCircuitBreakerConfig
	ValidateURLNotPrivate       = upstream.ValidateURLNotPrivate
)

// NewClient creates a new HTTP client with scafctl-specific defaults injected.
// When cfg is nil, DefaultConfig() is used. nil Metrics, empty CacheDir and
// empty CacheKeyPrefix are filled with scafctl defaults.
//
// The caller's config is not mutated; a shallow copy is made internally.
//
// Which destination addresses the client may reach is decided by cfg.IPPolicy,
// enforced by the upstream transport as each connection is dialed -- after DNS
// resolution, against the address actually being connected to. A nil IPPolicy
// denies private, loopback, and link-local addresses. Build one from
// application configuration with PolicyFromAppConfig.
//
// Because the check runs at dial time it also covers redirect hops and
// hostnames that resolve to private addresses, neither of which a pre-flight
// URL check can catch.
func NewClient(cfg *ClientConfig) *Client {
	var local ClientConfig
	if cfg != nil {
		local = *cfg
	} else {
		local = *DefaultConfig()
	}
	if local.Metrics == nil {
		local.Metrics = &OTelMetrics{}
	}
	if local.CacheDir == "" {
		local.CacheDir = paths.HTTPCacheDir()
	}
	if local.CacheKeyPrefix == "" {
		local.CacheKeyPrefix = settings.HTTPCacheKeyPrefixFor(paths.AppName())
	}

	return upstream.NewClient(&local)
}

// BuildStatusCodeCheckRetry returns a retryablehttp.CheckRetry function
// that retries on the given HTTP status codes.
func BuildStatusCodeCheckRetry(statusCodes []int) retryablehttp.CheckRetry {
	return upstream.BuildStatusCodeCheckRetry(statusCodes)
}

// BuildNamedBackoff returns a retryablehttp.Backoff function for the named strategy.
func BuildNamedBackoff(strategy string, initialWait, maxWait time.Duration) retryablehttp.Backoff {
	return upstream.BuildNamedBackoff(strategy, initialWait, maxWait)
}

// DefaultConfig returns a ClientConfig with scafctl-specific defaults:
// XDG-based cache directory, app-name-based cache key prefix, and OTel metrics adapter.
func DefaultConfig() *ClientConfig {
	cfg := upstream.DefaultConfig()
	cfg.CacheDir = paths.HTTPCacheDir()
	cfg.CacheKeyPrefix = settings.HTTPCacheKeyPrefixFor(paths.AppName())
	cfg.Metrics = &OTelMetrics{}
	return cfg
}
