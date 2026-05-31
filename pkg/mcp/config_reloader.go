// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// configReloader provides cached, TTL-based config reloading from disk.
// It avoids re-reading the config file on every MCP request while still
// picking up changes (e.g. auth profile switches) within the TTL window.
type configReloader struct {
	mu       sync.Mutex
	cached   *config.Config
	loadedAt time.Time
	ttl      time.Duration
	logger   logr.Logger
}

// newConfigReloader creates a reloader that caches config for the given TTL.
// An initial config snapshot may be provided; if nil, the first call to
// Config() will load from disk.
func newConfigReloader(initial *config.Config, ttl time.Duration, logger logr.Logger) *configReloader {
	r := &configReloader{
		cached: initial,
		ttl:    ttl,
		logger: logger,
	}
	if initial != nil {
		r.loadedAt = time.Now()
	}
	return r
}

// Config returns the current configuration. If the cached value is older
// than the TTL, the config is re-read from disk. If the disk read fails,
// the stale cached value is returned (never returns nil if a prior load
// succeeded).
func (r *configReloader) Config() *config.Config {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cached != nil && time.Since(r.loadedAt) < r.ttl {
		return r.cached
	}

	// Reset the global singleton first so Global() re-reads from disk
	// instead of returning its cached in-memory value.
	config.ResetGlobal()

	cfg, err := config.Global()
	if err != nil {
		r.logger.V(1).Info("config reload failed, using cached value", "error", err)
		return r.cached
	}

	r.cached = cfg
	r.loadedAt = time.Now()
	return r.cached
}
