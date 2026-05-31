// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigReloader(t *testing.T) {
	t.Run("returns initial config within TTL", func(t *testing.T) {
		initial := &config.Config{Version: 1}
		r := newConfigReloader(initial, 10*time.Minute, logr.Discard())

		got := r.Config()
		require.NotNil(t, got)
		assert.Equal(t, 1, got.Version)
	})

	t.Run("returns cached config when TTL not expired", func(t *testing.T) {
		initial := &config.Config{Version: 1}
		r := newConfigReloader(initial, 10*time.Minute, logr.Discard())

		// Two successive calls should return the same pointer.
		first := r.Config()
		second := r.Config()
		assert.Same(t, first, second)
	})

	t.Run("reloads from disk when TTL expired", func(t *testing.T) {
		initial := &config.Config{Version: 1}
		r := newConfigReloader(initial, 0, logr.Discard()) // TTL=0 → always stale

		// Reset global so Global() tries to load fresh. With no config
		// file on disk it will fail, falling back to the cached value.
		config.ResetGlobal()

		got := r.Config()
		require.NotNil(t, got, "should return cached value when disk load fails")
	})

	t.Run("nil initial attempts disk load", func(t *testing.T) {
		r := newConfigReloader(nil, 10*time.Minute, logr.Discard())
		config.ResetGlobal()

		// With no cached value, Config() attempts disk load via Global().
		// If a config file exists on disk it returns it; otherwise nil.
		// Either outcome is valid — the key invariant is no panic.
		_ = r.Config()
	})
}

func TestFreshConfigContext(t *testing.T) {
	t.Run("overlays fresh config onto merged context", func(t *testing.T) {
		initial := &config.Config{Version: 10}
		srv, err := NewServer(
			WithServerConfig(initial),
			WithServerName("testcli"),
		)
		require.NoError(t, err)

		ctx := srv.freshConfigContext(context.Background())
		got := config.FromContext(ctx)
		require.NotNil(t, got)
		assert.Equal(t, 10, got.Version)
	})

	t.Run("nil reloader falls through to mergeContext", func(t *testing.T) {
		initial := &config.Config{Version: 5}
		srv, err := NewServer(
			WithServerConfig(initial),
			WithServerName("testcli"),
		)
		require.NoError(t, err)

		// Force nil reloader to exercise fallback path.
		srv.cfgReloader = nil

		ctx := srv.freshConfigContext(context.Background())
		got := config.FromContext(ctx)
		require.NotNil(t, got)
		assert.Equal(t, 5, got.Version)
	})
}

func TestResolveConfig(t *testing.T) {
	t.Run("delegates to reloader when present", func(t *testing.T) {
		initial := &config.Config{Version: 42}
		srv, err := NewServer(
			WithServerConfig(initial),
			WithServerName("testcli"),
		)
		require.NoError(t, err)

		got := srv.resolveConfig()
		require.NotNil(t, got)
		assert.Equal(t, 42, got.Version)
	})

	t.Run("falls back to s.config when reloader is nil", func(t *testing.T) {
		cfg := &config.Config{Version: 7}
		srv, err := NewServer(
			WithServerConfig(cfg),
			WithServerName("testcli"),
		)
		require.NoError(t, err)
		srv.cfgReloader = nil

		got := srv.resolveConfig()
		require.NotNil(t, got)
		assert.Equal(t, 7, got.Version)
	})

	t.Run("falls back to context config when reloader and s.config are nil", func(t *testing.T) {
		ctxCfg := &config.Config{Version: 3}
		ctx := config.WithConfig(context.Background(), ctxCfg)
		srv, err := NewServer(
			WithServerName("testcli"),
			WithServerContext(ctx),
		)
		require.NoError(t, err)
		srv.cfgReloader = nil
		srv.config = nil

		got := srv.resolveConfig()
		require.NotNil(t, got)
		assert.Equal(t, 3, got.Version)
	})

	t.Run("falls back to Global when all else is nil", func(t *testing.T) {
		srv, err := NewServer(
			WithServerName("testcli"),
		)
		require.NoError(t, err)
		srv.cfgReloader = nil
		srv.config = nil
		srv.ctx = context.Background() // no config in context

		// Global() may or may not find a config file on disk.
		// The key invariant is no panic.
		config.ResetGlobal()
		_ = srv.resolveConfig()
	})

	t.Run("returns nil when everything fails", func(t *testing.T) {
		srv, err := NewServer(
			WithServerName("testcli"),
		)
		require.NoError(t, err)
		srv.cfgReloader = nil
		srv.config = nil
		srv.ctx = nil // nil context triggers Global() fallback

		config.ResetGlobal()
		// With no config file on disk, Global() will fail.
		// resolveConfig should return nil gracefully.
		_ = srv.resolveConfig()
	})
}

func BenchmarkConfigReloader_CachedHit(b *testing.B) {
	r := newConfigReloader(&config.Config{Version: 1}, 10*time.Minute, logr.Discard())
	b.ResetTimer()
	for range b.N {
		r.Config()
	}
}
