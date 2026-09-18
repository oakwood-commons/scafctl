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
//
// An omitted Transport is defaulted to proxy-disabled
// (ProxyAwareTransport(false)), the same posture every other
// policy-protected constructor in this package applies, so a bare
// NewClient(nil) cannot route through an ambient HTTP_PROXY/HTTPS_PROXY
// without an explicit opt-in. A caller who has marked a proxy as trusted
// passes ProxyAwareTransport(true) to restore environment-based proxy
// selection explicitly.
func NewClient(cfg *ClientConfig) *Client {
	return upstream.NewClient(localClientConfig(cfg))
}

// localClientConfig copies cfg (or the scafctl default when nil) and fills in
// scafctl-specific defaults. Split from NewClient so tests can assert on the
// resolved Transport and policy directly, rather than reaching through the
// client's wrapped transport chain (OTel, retry, cache) to find them.
func localClientConfig(cfg *ClientConfig) *ClientConfig {
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
	if local.Transport == nil {
		// An omitted Transport must not mean "http.DefaultTransport's
		// environment-based proxy selection": proxy routing is opt-in for
		// every policy-protected client, and a direct NewClient(nil) caller
		// has no trustedProxy setting of its own to have opted in with. Let
		// upstream see its own no-proxy default so an ambient proxy cannot
		// reintroduce the split-horizon/rebinding gap this package closes.
		// ProxyAwareTransport(true) restores environment proxying explicitly.
		local.Transport = ProxyAwareTransport(false)
	}

	return &local
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

// ProxyAwareTransport returns the *http.Transport a policy-protected client
// should dial through, given whether the caller has explicitly marked its
// configured proxy as trusted to enforce destination-address policy itself.
//
// The upstream client dials a proxy directly and validates the target only
// once, against a local DNS answer, before handing the request off -- the
// dial-time IP check that protects every other request never runs for that
// hop. When the proxy's own resolution can differ from that local answer
// (split-horizon DNS, a rebind between check and hand-off), an untrusted
// proxy can still connect somewhere the local check never saw.
//
// trustedProxy=false (the default posture; see config.HTTPClientConfig's
// TrustedProxy field) returns a transport with proxy selection disabled, so
// no HTTP_PROXY/HTTPS_PROXY environment variable is honoured and every
// request dials its target directly, where the ordinary dial-time check
// applies in full. trustedProxy=true returns a transport that explicitly
// restores environment-based proxy selection (ProxyFromEnvironment), so the
// caller opts in visibly rather than by omitting a Transport.
//
// Both transports leave dial-hook installation to the upstream library: they
// carry no dialer of their own, so a policy-protected client's dial is made
// by upstream's enforcing dialer, whose Control hook refuses a blocked
// address before the connection exists (see baseTransport).
func ProxyAwareTransport(trustedProxy bool) http.RoundTripper {
	if trustedProxy {
		return trustedProxyTransport()
	}
	return noProxyTransport()
}

// baseTransport returns a clone of http.DefaultTransport with its dial hooks
// cleared, for ProxyAwareTransport's two variants to specialize.
//
// Cloning (rather than building a bare *http.Transport{}) preserves every
// other default -- timeouts, TLS config, connection pool sizing -- but the
// dial hooks are cleared on purpose. Upstream treats a transport with a
// preinstalled dialer as a caller-supplied dialer that must run: it dials
// first and judges the peer address only afterwards (connect-then-check).
// With no preinstalled hook, upstream installs its own enforcing dialer,
// whose Control hook refuses a blocked address BEFORE the connection exists
// -- the pre-connect guarantee this package advertises. The only dialer
// settings the clear can lose are http.DefaultTransport's Dialer
// Timeout/KeepAlive (30s/30s), which are exactly upstream's enforcing
// dialer's own values, so a permitted request dials identically either way.
func baseTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		// http.DefaultTransport has been replaced with something that isn't a
		// *http.Transport (unusual, but possible). Fall back to a transport
		// built from scratch rather than panic; it still carries no proxy and
		// no dial hooks.
		base = &http.Transport{}
	}
	clone := base.Clone()
	// Clear every preinstalled dial hook so upstream owns the dial. The
	// deprecated Dial/DialTLS are cleared too: upstream reconstructs a
	// caller-dialer from either when the modern hooks are nil, and a replaced
	// http.DefaultTransport could have set them.
	clone.DialContext = nil    //nolint:staticcheck // assigning nil, not calling
	clone.DialTLSContext = nil //nolint:staticcheck // assigning nil, not calling
	clone.Dial = nil           //nolint:staticcheck // deprecated: cleared so it cannot select the caller-dialer path
	clone.DialTLS = nil        //nolint:staticcheck // deprecated: cleared so it cannot select the caller-dialer path
	return clone
}

// noProxyTransport returns a DefaultTransport clone that never routes through
// an ambient HTTP_PROXY/HTTPS_PROXY: exactly one thing differs from
// http.DefaultTransport, the Proxy field, which is nil -- net/http treats
// that as "never use a proxy" regardless of the environment.
func noProxyTransport() *http.Transport {
	clone := baseTransport()
	clone.Proxy = nil
	return clone
}

// trustedProxyTransport returns a DefaultTransport clone that explicitly
// restores environment-based proxy selection for callers that have marked
// their proxy as trusted. The dial hooks are cleared like the untrusted
// variant's: a request this transport dials directly (one no proxy was
// configured for) still gets upstream's pre-connect enforcing dialer, and a
// proxied dial is exempted by the upstream proxy path, which is what
// trustedProxy opted into.
func trustedProxyTransport() *http.Transport {
	clone := baseTransport()
	clone.Proxy = http.ProxyFromEnvironment
	return clone
}
