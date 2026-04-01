// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/config"
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
	cfg := &config.Config{
		APIServer: config.APIServerConfig{Host: "127.0.0.1", Port: 0},
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

func BenchmarkParseTimeoutOrDefault(b *testing.B) {
	for b.Loop() {
		parseTimeoutOrDefault("30s", "60s")
	}
}
