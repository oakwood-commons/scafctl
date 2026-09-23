// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
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
// The cfg parameter can be nil, in which case scafctl defaults are used --
// including the same default deny-private policy and disabled proxy routing
// an explicit, all-fields-unset configuration gets below.
func NewClientFromAppConfig(cfg *config.HTTPClientConfig, logger logr.Logger) *Client {
	return NewClient(httpClientConfigFromAppConfig(cfg, logger))
}

// httpClientConfigFromAppConfig builds the ClientConfig that
// NewClientFromAppConfig hands to NewClient. Split out from
// NewClientFromAppConfig so tests can assert on the resolved IPPolicy and
// Transport directly, rather than reaching through the client's wrapped
// transport chain (OTel, retry, cache) to find them.
func httpClientConfigFromAppConfig(cfg *config.HTTPClientConfig, logger logr.Logger) *ClientConfig {
	clientCfg := DefaultConfig()
	clientCfg.Logger = logger

	if cfg == nil {
		// nil is the documented secure default, so it must get the same
		// protections as an explicit, all-fields-unset configuration: the
		// default deny-private policy (PolicyFromAppConfig(nil)'s result)
		// and a transport that never routes through an ambient proxy
		// (TrustedProxy(nil) is false). Returning before these were set left
		// http.DefaultTransport's environment-based proxy selection in force
		// for exactly the callers -- such as hostname inventory fetches --
		// with no trustedProxy opt-in to have relied on it.
		clientCfg.IPPolicy = &IPPolicy{}
		clientCfg.Transport = ProxyAwareTransport(false)
		return clientCfg
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

	// Which destination addresses this client may reach. Enforced upstream at
	// dial time, against the resolved address.
	//
	// A malformed entry fails closed: the client keeps the default deny-all
	// policy rather than starting with weaker protection than the operator
	// asked for. Config validation rejects malformed entries at startup, so
	// reaching this branch means validation was bypassed.
	policy, policyErr := PolicyFromAppConfig(cfg)
	if policyErr != nil {
		if logger.GetSink() != nil {
			logger.Error(policyErr, "invalid address allowlist; denying all private addresses",
				"field", AllowedPrivateCIDRsKey)
		}
		policy = &IPPolicy{}
	}
	clientCfg.IPPolicy = policy

	// A proxied request is dialed to the proxy, not the target, so it never
	// reaches the dial-time check the rest of this policy relies on. Disable
	// proxy routing by default (TrustedProxy unset/false) rather than accept
	// that gap silently; see ProxyAwareTransport for the full reasoning.
	if clientCfg.Transport == nil {
		clientCfg.Transport = ProxyAwareTransport(TrustedProxy(cfg))
	}

	if cfg.MaxResponseBodySize > 0 {
		clientCfg.MaxResponseBodySize = cfg.MaxResponseBodySize
	}

	return clientCfg
}
