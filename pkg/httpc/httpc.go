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
	"fmt"
	"net/http"
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
// AllowPrivateIPs is set to true on the upstream client because scafctl handles
// SSRF protection via context-based checks (PrivateIPsAllowed + ValidateURLNotPrivate)
// at the call sites that need it (httpprovider, parameterprovider, etc.).
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
	// scafctl performs SSRF validation at the application layer via
	// PrivateIPsAllowed(ctx) and ValidateURLNotPrivate(url) before issuing
	// requests. Disable the upstream transport-level check to avoid double-gating
	// and to preserve the original context-aware behaviour.
	local.AllowPrivateIPs = true

	maxRedirects := local.MaxRedirects
	if maxRedirects <= 0 {
		maxRedirects = DefaultMaxRedirects
	}

	client := upstream.NewClient(&local)

	// Override the upstream CheckRedirect with a context-aware variant.
	// The upstream redirect policy is static (uses the AllowPrivateIPs config
	// field captured at construction), but scafctl needs per-request checks via
	// PrivateIPsAllowed(ctx). This prevents SSRF bypasses where a public URL
	// 302-redirects to a private/link-local address (e.g. 169.254.169.254).
	client.RetryableClient().HTTPClient.CheckRedirect = ssrfSafeRedirectPolicy(maxRedirects)

	return client
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

// ssrfSafeRedirectPolicy returns a CheckRedirect function that enforces both
// a maximum redirect count and context-aware SSRF validation on redirect targets.
func ssrfSafeRedirectPolicy(maxRedirects int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", len(via))
		}
		if !PrivateIPsAllowed(req.Context()) {
			if err := ValidateURLNotPrivate(req.URL.String()); err != nil {
				return err
			}
		}
		return nil
	}
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
