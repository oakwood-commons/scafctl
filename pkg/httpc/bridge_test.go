// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

func TestNewClientFromAppConfig_NilConfig(t *testing.T) {
	client := NewClientFromAppConfig(nil, logr.Discard())
	require.NotNil(t, client)
}

func TestNewClientFromAppConfig_BasicConfig(t *testing.T) {
	cfg := &config.HTTPClientConfig{
		Timeout:     "5s",
		RetryMax:    2,
		EnableCache: boolPtr(false),
	}
	client := NewClientFromAppConfig(cfg, logr.Discard())
	require.NotNil(t, client)
}

func TestNewClientFromAppConfig_InvalidTimeout(t *testing.T) {
	cfg := &config.HTTPClientConfig{
		Timeout: "not-a-duration",
	}
	// Should not panic; falls back to default timeout.
	client := NewClientFromAppConfig(cfg, logr.Discard())
	require.NotNil(t, client)
}

func TestNewClientFromAppConfig_AllFields(t *testing.T) {
	cfg := &config.HTTPClientConfig{
		Timeout:                           "10s",
		RetryMax:                          5,
		RetryWaitMin:                      "2s",
		RetryWaitMax:                      "60s",
		EnableCache:                       boolPtr(true),
		CacheType:                         "memory",
		CacheDir:                          "/tmp/test-cache",
		CacheTTL:                          "5m",
		CacheKeyPrefix:                    "test:",
		MaxCacheFileSize:                  1024,
		MemoryCacheSize:                   500,
		EnableCircuitBreaker:              boolPtr(true),
		CircuitBreakerMaxFailures:         10,
		CircuitBreakerOpenTimeout:         "1m",
		CircuitBreakerHalfOpenMaxRequests: 3,
		EnableCompression:                 boolPtr(false),
		AllowPrivateIPs:                   boolPtr(true),
		MaxResponseBodySize:               2048,
	}
	client := NewClientFromAppConfig(cfg, logr.Discard())
	require.NotNil(t, client)
}

func TestNewClientFromAppConfig_UsesScafctlDefaults(t *testing.T) {
	// Empty config should produce a client with scafctl cache defaults.
	cfg := &config.HTTPClientConfig{}
	client := NewClientFromAppConfig(cfg, logr.Discard())
	require.NotNil(t, client)

	// The underlying config should have scafctl-specific values.
	// We can verify indirectly: DefaultConfig() returns the expected values.
	defCfg := DefaultConfig()
	assert.Equal(t, paths.HTTPCacheDir(), defCfg.CacheDir)
	assert.Equal(t, settings.HTTPCacheKeyPrefixFor(paths.AppName()), defCfg.CacheKeyPrefix)
}

func TestNewClientFromAppConfig_MakesRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &config.HTTPClientConfig{
		Timeout:     "5s",
		EnableCache: boolPtr(false),
		// httptest binds loopback, which the default policy denies. Naming the
		// range here is what a user would do to reach an internal endpoint.
		AllowedPrivateCIDRs: config.PrivateCIDRList("127.0.0.0/8", "::1/128"),
	}
	client := NewClientFromAppConfig(cfg, logr.Discard())

	resp, err := client.Get(t.Context(), srv.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestNewClientFromAppConfig_InvalidDurations(t *testing.T) {
	cfg := &config.HTTPClientConfig{
		RetryWaitMin:              "bad",
		RetryWaitMax:              "bad",
		CacheTTL:                  "bad",
		CircuitBreakerOpenTimeout: "bad",
		// Ensure circuit breaker config block is entered.
		CircuitBreakerMaxFailures: 1,
	}
	// Should not panic; falls back to defaults for all invalid durations.
	client := NewClientFromAppConfig(cfg, logr.Discard())
	require.NotNil(t, client)
}

func TestNewClientFromAppConfig_NilSinkLogger(t *testing.T) {
	cfg := &config.HTTPClientConfig{
		Timeout: "bad", // triggers parseDurationOr error path
	}
	// Zero-value logr.Logger has a nil sink; must not panic.
	client := NewClientFromAppConfig(cfg, logr.Logger{})
	require.NotNil(t, client)
}

// A nil cfg is the documented secure default, so it must get the same
// protections a fully-specified-but-empty config gets: the default deny
// policy, and a transport that never routes through an ambient proxy.
// Before this test's fix, the nil path returned before either was set,
// silently inheriting http.DefaultTransport's environment-based proxy
// selection.
func TestHTTPClientConfigFromAppConfig_NilConfigAppliesSecureDefaults(t *testing.T) {
	clientCfg := httpClientConfigFromAppConfig(nil, logr.Discard())

	require.NotNil(t, clientCfg.IPPolicy,
		"nil config must get the default deny policy explicitly, not rely on upstream's own nil handling")
	assert.Equal(t, &IPPolicy{}, clientCfg.IPPolicy, "nil config must get the same zero-value deny policy as an empty one")

	require.NotNil(t, clientCfg.Transport, "nil config must not inherit http.DefaultTransport's proxy selection")
	transport, ok := clientCfg.Transport.(*http.Transport)
	require.True(t, ok, "the nil-config transport should be an *http.Transport")
	assert.Nil(t, transport.Proxy, "nil config must never route through an ambient HTTP_PROXY/HTTPS_PROXY")
}

// The transport wiring must follow TrustedProxy the same way for an explicit
// config as it does for the nil path above: untrusted (the default) disables
// proxy routing, trusted defers to normal environment-based proxy selection.
func TestHTTPClientConfigFromAppConfig_TransportFollowsTrustedProxy(t *testing.T) {
	t.Run("untrusted disables proxy routing", func(t *testing.T) {
		clientCfg := httpClientConfigFromAppConfig(&config.HTTPClientConfig{}, logr.Discard())

		require.NotNil(t, clientCfg.Transport)
		transport, ok := clientCfg.Transport.(*http.Transport)
		require.True(t, ok)
		assert.Nil(t, transport.Proxy)
	})

	t.Run("trusted restores environment proxy selection explicitly", func(t *testing.T) {
		trusted := true
		clientCfg := httpClientConfigFromAppConfig(&config.HTTPClientConfig{TrustedProxy: &trusted}, logr.Discard())

		require.NotNil(t, clientCfg.Transport,
			"a trusted proxy must be an explicit transport, not an omitted Transport that silently inherits one")
		transport, ok := clientCfg.Transport.(*http.Transport)
		require.True(t, ok)
		assert.Equal(t,
			reflect.ValueOf(http.ProxyFromEnvironment).Pointer(),
			reflect.ValueOf(transport.Proxy).Pointer(),
			"a trusted proxy explicitly restores http.DefaultTransport's environment-based proxy selection")
		assert.Nil(t, transport.DialContext,
			"the trusted transport carries no preinstalled dialer, so upstream installs its enforcing dialer for direct dials")
	})
}

func boolPtr(b bool) *bool { return &b }
