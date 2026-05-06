// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"net/http"
	"net/http/httptest"
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

func boolPtr(b bool) *bool { return &b }
