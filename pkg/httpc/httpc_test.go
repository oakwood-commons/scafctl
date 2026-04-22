// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package httpc

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	assert.False(t, cfg.AllowPrivateIPs, "AllowPrivateIPs should not be mutated")
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
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestPrivateIPsAllowed_NoConfig(t *testing.T) {
	ctx := context.Background()
	assert.False(t, PrivateIPsAllowed(ctx), "should deny when no config in context")
}

func TestPrivateIPsAllowed_NilField(t *testing.T) {
	cfg := &config.Config{}
	ctx := config.WithConfig(context.Background(), cfg)
	assert.False(t, PrivateIPsAllowed(ctx), "should deny when AllowPrivateIPs is nil")
}

func TestPrivateIPsAllowed_True(t *testing.T) {
	allow := true
	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow},
	}
	ctx := config.WithConfig(context.Background(), cfg)
	assert.True(t, PrivateIPsAllowed(ctx))
}

func TestPrivateIPsAllowed_False(t *testing.T) {
	deny := false
	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &deny},
	}
	ctx := config.WithConfig(context.Background(), cfg)
	assert.False(t, PrivateIPsAllowed(ctx))
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

func TestSSRFSafeRedirectPolicy_BlocksPrivateRedirect(t *testing.T) {
	// A public server that redirects to a private IP address.
	privateTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer privateTarget.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Redirect to 127.0.0.1 (the private target).
		http.Redirect(w, r, privateTarget.URL+"/secret", http.StatusFound)
	}))
	defer redirector.Close()

	client := NewClient(&ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
		RetryMax:    0,
	})

	// Context with private IPs disallowed (default, no config in context).
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, redirector.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err, "redirect to private IP should be blocked")
	assert.Contains(t, err.Error(), "private", "error should mention private IP validation")
}

func TestSSRFSafeRedirectPolicy_AllowsPrivateRedirectWhenPermitted(t *testing.T) {
	privateTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	defer privateTarget.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, privateTarget.URL+"/internal", http.StatusFound)
	}))
	defer redirector.Close()

	client := NewClient(&ClientConfig{
		Timeout:     5 * time.Second,
		EnableCache: false,
		RetryMax:    0,
	})

	// Context with private IPs allowed.
	allow := true
	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow},
	}
	ctx := config.WithConfig(context.Background(), cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, redirector.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSSRFSafeRedirectPolicy_EnforcesMaxRedirects(t *testing.T) {
	// Server that always redirects to itself.
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
	})

	// Even with private IPs allowed, max redirect limit should be enforced.
	allow := true
	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allow},
	}
	ctx := config.WithConfig(context.Background(), cfg)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	require.NoError(t, err)

	resp, err := client.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	require.Error(t, err, "should hit max redirect limit")
	assert.Contains(t, err.Error(), "3 redirect", "error should mention redirect count")
}
