// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewServer_Defaults(t *testing.T) {
	srv, err := NewServer()
	require.NoError(t, err)
	assert.NotNil(t, srv.Router())
	assert.NotEmpty(t, srv.Version())
	assert.False(t, srv.IsShuttingDown())
	assert.NotZero(t, srv.StartTime())
}

func TestNewServer_WithOptions(t *testing.T) {
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			Host: "127.0.0.1",
			Port: 9090,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := NewServer(
		WithServerConfig(cfg),
		WithServerVersion("test-v1"),
		WithServerContext(ctx),
	)
	require.NoError(t, err)
	assert.Equal(t, "test-v1", srv.Version())
	assert.Equal(t, cfg, srv.Config())
}

// TestNewServer_AttachesAppConfigToRequestContext proves withAppConfig actually
// reaches a real request, not just that it is registered.
//
// The CLI and MCP server have always attached the app config to their
// contexts; the API server never did. Without this, config.FromContext
// returned nil inside every API handler, so config-driven behavior --
// including the httpClient.allowPrivateIPs SSRF setting this PR's guard
// depends on -- silently fell back to defaults and was unconfigurable in API
// mode. Registering the middleware is not enough to prove that; this drives
// an actual request through the router and reads the context inside a real
// handler.
func TestNewServer_AttachesAppConfigToRequestContext(t *testing.T) {
	allowPrivateIPs := true
	cfg := &config.Config{
		HTTPClient: config.HTTPClientConfig{AllowPrivateIPs: &allowPrivateIPs},
	}
	srv, err := NewServer(WithServerConfig(cfg))
	require.NoError(t, err)

	var gotFromContext *config.Config
	srv.Router().Get("/probe", func(_ http.ResponseWriter, r *http.Request) {
		gotFromContext = config.FromContext(r.Context())
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/probe", nil)
	srv.Router().ServeHTTP(httptest.NewRecorder(), req)

	require.NotNil(t, gotFromContext, "config.FromContext must not be nil inside a handler")
	require.NotNil(t, gotFromContext.HTTPClient.AllowPrivateIPs)
	assert.True(t, *gotFromContext.HTTPClient.AllowPrivateIPs,
		"the exact config instance passed to NewServer must be reachable from request context")
}

func TestServer_SetAPIRouter(t *testing.T) {
	srv, err := NewServer()

	require.NoError(t, err)
	assert.Equal(t, srv.Router(), srv.APIRouter())
	srv.SetAPIRouter(srv.Router())
	assert.NotNil(t, srv.APIRouter())
}

func TestServer_HandlerCtx(t *testing.T) {
	cfg := &config.Config{}
	srv, err := NewServer(WithServerConfig(cfg))
	require.NoError(t, err)
	hctx := srv.HandlerCtx()
	assert.NotNil(t, hctx)
	assert.Equal(t, cfg, hctx.Config)
	assert.False(t, hctx.ShuttingDown())
}

func TestServer_HandlerCtx_WithPluginFields(t *testing.T) {
	cfg := &config.Config{}
	officialReg := official.NewRegistry()
	srv, err := NewServer(
		WithServerConfig(cfg),
		WithServerOfficialProviders(officialReg),
	)
	require.NoError(t, err)
	hctx := srv.HandlerCtx()
	assert.NotNil(t, hctx)
	assert.Equal(t, officialReg, hctx.OfficialProviders)
	assert.NotNil(t, hctx.ServerContext, "ServerContext should be set from server's context")
}

func TestServer_WithPluginClients_Shutdown(t *testing.T) {
	// Verify that Shutdown doesn't panic with nil or empty plugin clients.
	srv, err := NewServer(WithServerPluginClients(nil))
	require.NoError(t, err)
	err = srv.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.True(t, srv.IsShuttingDown())
}

func TestServer_WithPluginPool_Shutdown(t *testing.T) {
	reg := provider.NewRegistry()
	pool := plugin.NewPool(context.Background(), nil, reg, logr.Discard(), plugin.WithIdleTimeout(0))

	srv, err := NewServer(WithServerPluginPool(pool))
	require.NoError(t, err)

	err = srv.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.True(t, srv.IsShuttingDown())
}

func TestServer_HandlerCtx_PluginPool(t *testing.T) {
	reg := provider.NewRegistry()
	pool := plugin.NewPool(context.Background(), nil, reg, logr.Discard(), plugin.WithIdleTimeout(0))
	defer pool.Shutdown()

	srv, err := NewServer(WithServerPluginPool(pool), WithServerRegistry(reg))
	require.NoError(t, err)

	hctx := srv.HandlerCtx()
	assert.Equal(t, pool, hctx.PluginPool)
}

func TestServer_Shutdown(t *testing.T) {
	srv, err := NewServer()
	require.NoError(t, err)
	assert.False(t, srv.IsShuttingDown())
	err = srv.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.True(t, srv.IsShuttingDown())
}

func TestParseTimeoutOrDefault(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		defaultValue string
		expected     time.Duration
	}{
		{"valid value", "5s", "10s", 5 * time.Second},
		{"empty uses default", "", "10s", 10 * time.Second},
		{"invalid uses default", "invalid", "10s", 10 * time.Second},
		{"both invalid zero", "invalid", "also-invalid", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTimeoutOrDefault(tt.value, tt.defaultValue)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsLoopbackHost pins the predicate that decides whether the server warns
// about being exposed without authentication. A false positive here silences a
// real exposure warning, so unresolvable and wildcard hosts must NOT be treated
// as loopback.
func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		expected bool
	}{
		{"empty means the loopback default", "", true},
		{"localhost name", "localhost", true},
		{"localhost uppercase", "LocalHost", true},
		{"ipv4 loopback", "127.0.0.1", true},
		{"ipv4 loopback range", "127.0.0.53", true},
		{"ipv6 loopback", "::1", true},
		{"ipv6 loopback bracketed", "[::1]", true},
		{"ipv4 wildcard binds all interfaces", "0.0.0.0", false},
		{"ipv6 wildcard binds all interfaces", "::", false},
		{"private lan address", "192.168.1.10", false},
		{"public address", "203.0.113.7", false},
		{"unresolvable hostname errs toward warning", "api.example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isLoopbackHost(tt.host))
		})
	}
}

// TestServer_AppliesResourceLimits verifies the http.Server is constructed with
// the idle and header bounds, including that an explicit config value wins over
// the default.
func TestServer_AppliesResourceLimits(t *testing.T) {
	t.Run("defaults are applied when unset", func(t *testing.T) {
		srv, err := NewServer(WithServerConfig(&config.Config{}))
		require.NoError(t, err)

		addr := srv.buildHTTPServer()

		require.NotNil(t, srv.httpSrv)
		expectedIdle, _ := time.ParseDuration(settings.DefaultAPIIdleTimeout)
		assert.Equal(t, expectedIdle, srv.httpSrv.IdleTimeout)
		assert.Equal(t, settings.DefaultAPIMaxHeaderBytes, srv.httpSrv.MaxHeaderBytes)
		assert.Equal(t, "127.0.0.1:8080", addr, "default bind must stay loopback")
	})

	t.Run("explicit config overrides defaults", func(t *testing.T) {
		srv, err := NewServer(WithServerConfig(&config.Config{
			APIServer: config.APIServerConfig{
				IdleTimeout:    "45s",
				MaxHeaderBytes: 4096,
			},
		}))
		require.NoError(t, err)

		srv.buildHTTPServer()

		require.NotNil(t, srv.httpSrv)
		assert.Equal(t, 45*time.Second, srv.httpSrv.IdleTimeout)
		assert.Equal(t, 4096, srv.httpSrv.MaxHeaderBytes)
	})

	t.Run("invalid idle timeout falls back to the default", func(t *testing.T) {
		srv, err := NewServer(WithServerConfig(&config.Config{
			APIServer: config.APIServerConfig{IdleTimeout: "not-a-duration"},
		}))
		require.NoError(t, err)

		srv.buildHTTPServer()

		expectedIdle, _ := time.ParseDuration(settings.DefaultAPIIdleTimeout)
		assert.Equal(t, expectedIdle, srv.httpSrv.IdleTimeout)
	})

	t.Run("default idle timeout does not exceed the read timeout", func(t *testing.T) {
		// Regression test. net/http falls back to ReadTimeout when IdleTimeout is
		// zero (Server.idleTimeout), so a default LARGER than ReadTimeout would
		// widen the idle window rather than bound it -- the opposite of the
		// intent. Keep the default at or below the request timeout.
		srv, err := NewServer(WithServerConfig(&config.Config{}))
		require.NoError(t, err)

		srv.buildHTTPServer()

		assert.LessOrEqual(t, srv.httpSrv.IdleTimeout, srv.httpSrv.ReadTimeout,
			"default IdleTimeout must not exceed ReadTimeout, or it loosens the idle bound")
	})
}

// TestNewServer_ValidatesAPIConfig asserts the advertised apiServer bounds hold
// on the embedder path too. config.Manager.Load is the only other caller of
// APIServerConfig.Validate, so without a check in NewServer an embedder passing
// a hand-built config through WithServerConfig could start a server with an
// arbitrarily large per-connection header buffer, or with a host allowlist that
// was requested but is unusable.
func TestNewServer_ValidatesAPIConfig(t *testing.T) {
	tests := []struct {
		name    string
		apiCfg  config.APIServerConfig
		wantErr string
	}{
		{"zero value is valid", config.APIServerConfig{}, ""},
		{"in-bounds config is valid", config.APIServerConfig{
			MaxHeaderBytes: settings.MaxAPIMaxHeaderBytes,
			AllowedHosts:   []string{"api.example.com"},
		}, ""},
		{"header cap is enforced", config.APIServerConfig{
			MaxHeaderBytes: settings.MaxAPIMaxHeaderBytes + 1,
		}, "maxHeaderBytes"},
		{"negative header bytes rejected", config.APIServerConfig{
			MaxHeaderBytes: -1,
		}, "maxHeaderBytes"},
		{"unusable allowlist rejected", config.APIServerConfig{
			AllowedHosts: []string{" "},
		}, "allowedHosts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, err := NewServer(WithServerConfig(&config.Config{APIServer: tt.apiCfg}))
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.NotNil(t, srv)
				return
			}
			require.Error(t, err)
			assert.Nil(t, srv, "no server may be returned alongside a validation error")
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestServer_Start_InvalidTLS(t *testing.T) {
	cfg := &config.Config{
		APIServer: config.APIServerConfig{
			TLS: config.APITLSConfig{Enabled: true},
		},
	}
	srv, err := NewServer(WithServerConfig(cfg))
	require.NoError(t, err)
	err = srv.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TLS enabled but cert or key path is empty")
}

func TestServer_InitAPI(t *testing.T) {
	srv, err := NewServer()
	require.NoError(t, err)
	srv.InitAPI()
	assert.NotNil(t, srv.API())
}

func TestServer_Start_PortZero(t *testing.T) {
	// Allocate a free port so the test doesn't collide with port 8080.
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	freePort := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cfg := &config.Config{
		APIServer: config.APIServerConfig{Host: "127.0.0.1", Port: freePort},
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv, err := NewServer(WithServerConfig(cfg), WithServerContext(ctx))
	require.NoError(t, err)
	srv.InitAPI()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down in time")
	}
}

func BenchmarkNewServer(b *testing.B) {
	cfg := &config.Config{}
	for b.Loop() {
		_, _ = NewServer(WithServerConfig(cfg))
	}
}

func TestWithServerLogger(t *testing.T) {
	lgr := logr.Discard()
	srv, err := NewServer(WithServerLogger(lgr))
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

func TestWithServerRegistry(t *testing.T) {
	reg := provider.NewRegistry()
	srv, err := NewServer(WithServerRegistry(reg))
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

func TestWithServerAuthRegistry(t *testing.T) {
	reg := auth.NewRegistry()
	srv, err := NewServer(WithServerAuthRegistry(reg))
	require.NoError(t, err)
	assert.NotNil(t, srv)
}

func TestServer_Context(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := NewServer(WithServerContext(ctx))
	require.NoError(t, err)
	assert.NotNil(t, srv.Context())
}

func BenchmarkParseTimeoutOrDefault(b *testing.B) {
	for b.Loop() {
		parseTimeoutOrDefault("30s", "60s")
	}
}
