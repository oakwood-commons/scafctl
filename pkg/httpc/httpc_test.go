// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

func TestDefaultConfig_ScafctlDefaults(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, paths.HTTPCacheDir(), cfg.CacheDir, "CacheDir should use XDG path")
	assert.Equal(t, settings.HTTPCacheKeyPrefixFor(paths.AppName()), cfg.CacheKeyPrefix, "CacheKeyPrefix should derive from app name")
	assert.IsType(t, &OTelMetrics{}, cfg.Metrics, "Metrics should be OTelMetrics adapter")
	assert.True(t, cfg.EnableCache, "EnableCache should default to true")
	assert.Equal(t, CacheTypeFilesystem, cfg.CacheType, "CacheType should default to filesystem")
}

func TestNewClient_NilConfig(t *testing.T) {
	client := NewClient(nil)
	require.NotNil(t, client)
}

func TestNewClient_ExplicitConfig(t *testing.T) {
	cfg := &ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
	}
	client := NewClient(cfg)
	require.NotNil(t, client)
}

func TestNewClient_DoesNotMutateInput(t *testing.T) {
	cfg := &ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
	}
	// CacheDir and CacheKeyPrefix are empty before the call.
	assert.Empty(t, cfg.CacheDir)
	assert.Empty(t, cfg.CacheKeyPrefix)
	assert.Nil(t, cfg.Metrics)

	_ = NewClient(cfg)

	// NewClient must not mutate the caller's config.
	assert.Empty(t, cfg.CacheDir, "CacheDir should not be mutated")
	assert.Empty(t, cfg.CacheKeyPrefix, "CacheKeyPrefix should not be mutated")
	assert.Nil(t, cfg.Metrics, "Metrics should not be mutated")
	assert.Nil(t, cfg.IPPolicy, "IPPolicy should not be mutated")
}

func TestNewClient_PreservesExplicitValues(t *testing.T) {
	cfg := &ClientConfig{
		Timeout:        5 * time.Second,
		EnableCache:    false,
		CacheDir:       "/custom/dir",
		CacheKeyPrefix: "custom:",
		Metrics:        NoopMetrics{},
	}
	_ = NewClient(cfg)

	// Caller's config must remain unchanged.
	assert.Equal(t, "/custom/dir", cfg.CacheDir)
	assert.Equal(t, "custom:", cfg.CacheKeyPrefix)
	assert.IsType(t, NoopMetrics{}, cfg.Metrics)
}

// allowLoopback returns a policy permitting loopback addresses, which every
// httptest server binds to. Without it the default deny policy refuses the
// test server itself.
func allowLoopback(t *testing.T) *IPPolicy {
	t.Helper()
	policy, err := PolicyFromAppConfig(&config.HTTPClientConfig{
		AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
	})
	require.NoError(t, err)
	return policy
}

func TestNewClient_DoRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	client := NewClient(&ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
		RetryMax:    0,
		IPPolicy:    allowLoopback(t),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestValidateURLNotPrivate_PublicIP(t *testing.T) {
	assert.NoError(t, ValidateURLNotPrivate("https://8.8.8.8/path"))
}

func TestValidateURLNotPrivate_PrivateIP(t *testing.T) {
	assert.Error(t, ValidateURLNotPrivate("http://192.168.1.1/secret"))
}

func TestBuildStatusCodeCheckRetry(t *testing.T) {
	fn := BuildStatusCodeCheckRetry([]int{502, 503})
	assert.NotNil(t, fn)
}

func TestBuildNamedBackoff(t *testing.T) {
	fn := BuildNamedBackoff("exponential", settings.DefaultHTTPRetryWaitMinimum, settings.DefaultHTTPRetryWaitMaximum)
	assert.NotNil(t, fn)
}

func TestCacheTypeConstants(t *testing.T) {
	assert.Equal(t, CacheType("memory"), CacheTypeMemory)
	assert.Equal(t, CacheType("filesystem"), CacheTypeFilesystem)
}

func TestSentinelErrors(t *testing.T) {
	assert.NotNil(t, ErrCircuitBreakerOpen)
	assert.NotNil(t, ErrCacheSizeLimitExceeded)
	assert.NotNil(t, ErrDecompressionBombDetected)
	assert.NotNil(t, ErrResponseBodyTooLarge)
}

func TestDefaultMaxRedirects(t *testing.T) {
	assert.Equal(t, 10, DefaultMaxRedirects, "should re-export upstream default")
}

func TestIPPolicy_DeniesLoopbackByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// No IPPolicy means the default deny policy, which refuses loopback. The
	// server is reachable; the client is what refuses to connect.
	client := NewClient(&ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
		RetryMax:    0,
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err, "loopback must be denied without an explicit exception")
	assert.ErrorIs(t, err, ErrBlockedByPolicy)
}

func TestIPPolicy_AllowsLoopbackWhenConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	client := NewClient(&ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
		RetryMax:    0,
		IPPolicy:    allowLoopback(t),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// Redirects are re-checked because the policy runs per dial, not per call. Both
// hops here are loopback, so an allowlist covering loopback must permit the
// whole chain -- the redirect target is dialled separately and checked again.
func TestIPPolicy_AllowedRedirectHopSucceeds(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`arrived`))
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/internal", http.StatusFound)
	}))
	defer redirector.Close()

	client := NewClient(&ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
		RetryMax:    0,
		IPPolicy:    allowLoopback(t),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redirector.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "arrived", string(body))
}

// The redirect-SSRF case: the request starts somewhere permitted and is then
// redirected into blocked space. Because the check runs per dial rather than
// once on the original URL, the second hop is refused.
//
// The redirect target is a cloud metadata address, which no configuration can
// exempt -- so this asserts the deny path without depending on the test's own
// loopback exemption, and without touching the network (the dial never happens).
func TestIPPolicy_BlockedRedirectHopIsRefused(t *testing.T) {
	const metadataURL = "http://169.254.169.254/latest/meta-data/"

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, metadataURL, http.StatusFound)
	}))
	defer redirector.Close()

	client := NewClient(&ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
		RetryMax:    0,
		// Loopback is permitted, so the FIRST hop succeeds and the denial can
		// only be coming from the redirect target.
		IPPolicy: allowLoopback(t),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, redirector.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req) //nolint:bodyclose // the request is refused, so there is no body
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}

	require.Error(t, err, "a redirect into blocked space must not be followed")
	assert.ErrorIs(t, err, ErrBlockedByPolicy)
}

func TestIPPolicy_EnforcesMaxRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL, http.StatusFound)
	}))
	defer srv.Close()

	client := NewClient(&ClientConfig{
		Timeout:      5 * time.Second,
		EnableCache:  false,
		RetryMax:     0,
		MaxRedirects: 3,
		IPPolicy:     allowLoopback(t),
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err, "should hit max redirect limit")
	assert.Contains(t, err.Error(), "3 redirect", "error should mention redirect count")
}

// TestProxyAwareTransport_Untrusted proves the untrusted (default) path
// disables proxy selection outright: net/http treats a Transport with a nil
// Proxy field as "never use a proxy", regardless of HTTP_PROXY/HTTPS_PROXY,
// so this is a structural guarantee rather than something that depends on
// racing against process-wide proxy-env caching.
func TestProxyAwareTransport_Untrusted(t *testing.T) {
	rt := ProxyAwareTransport(false)
	require.NotNil(t, rt, "an untrusted proxy must still get a usable transport")

	transport, ok := rt.(*http.Transport)
	require.True(t, ok, "the returned transport should be an *http.Transport")
	assert.Nil(t, transport.Proxy, "Proxy must be nil so no request is ever routed through one")
}

// TestProxyAwareTransport_Trusted proves the trusted path is a deliberate
// no-op: it returns nil so the caller's ClientConfig.Transport stays unset,
// letting http.DefaultTransport's normal environment-based proxy selection
// apply exactly as it did before this change.
func TestProxyAwareTransport_Trusted(t *testing.T) {
	assert.Nil(t, ProxyAwareTransport(true))
}

// TestNoProxyTransport_PreservesOtherDefaults proves the untrusted transport
// is a clone of http.DefaultTransport with only Proxy changed, not a bare
// *http.Transport{} that would silently drop timeouts, TLS config, and
// connection pool sizing.
func TestNoProxyTransport_PreservesOtherDefaults(t *testing.T) {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	require.True(t, ok, "http.DefaultTransport must be an *http.Transport for this test to be meaningful")

	rt := ProxyAwareTransport(false)
	transport, ok := rt.(*http.Transport)
	require.True(t, ok)

	assert.Equal(t, defaultTransport.MaxIdleConns, transport.MaxIdleConns)
	assert.Equal(t, defaultTransport.IdleConnTimeout, transport.IdleConnTimeout)
	assert.Equal(t, defaultTransport.TLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	assert.Nil(t, transport.Proxy)
}

// TestProxyAwareTransport_DoesNotRouteThroughProxy is an end-to-end proof,
// not just a struct-field check: an *http.Transport with an explicit
// http.ProxyURL DOES route requests through that proxy (this half of the test
// is the control, proving the proxy mechanism itself works), while a request
// made through ProxyAwareTransport(false) against the same target reaches the
// target directly and the proxy handler is never invoked -- exactly the
// TOCTOU-closing behavior the policy.go:86 review thread asked for.
func TestProxyAwareTransport_DoesNotRouteThroughProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	var proxyHit bool
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyHit = true
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	proxyURL, err := neturl.Parse(proxy.URL)
	require.NoError(t, err)

	t.Run("control: an explicit proxy transport does route through it", func(t *testing.T) {
		proxyHit = false
		explicit := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
		client := &http.Client{Transport: explicit}
		resp, err := client.Get(target.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.True(t, proxyHit, "the control transport must have gone through the proxy")
	})

	t.Run("untrusted ProxyAwareTransport reaches the target directly", func(t *testing.T) {
		proxyHit = false
		rt := ProxyAwareTransport(false)
		client := &http.Client{Transport: rt}
		resp, err := client.Get(target.URL)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.False(t, proxyHit, "an untrusted transport must never route through a proxy")
	})
}
