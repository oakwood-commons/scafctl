// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
)

// HandlerContext provides shared dependencies to all API handlers.
// All handlers access scafctl domain packages through this struct.
type HandlerContext struct {
	Config           *config.Config
	ProviderRegistry *provider.Registry
	AuthRegistry     *auth.Registry
	Logger           logr.Logger
	IsShuttingDown   *int32
	StartTime        time.Time

	// PluginFetcher enables auto-fetching of plugin binaries from catalogs
	// at request time. Used by the solution test endpoint (planned) to resolve
	// bundle.plugins declarations. Nil when plugin auto-fetch is not configured.
	PluginFetcher *plugin.Fetcher

	// OfficialProviders holds the registry of first-party extracted providers
	// for auto-resolution. Used by the solution validate endpoint (planned)
	// to check provider availability. Nil when official provider support is disabled.
	OfficialProviders *official.Registry

	// ServerContext carries the server's enriched context with config, auth,
	// and logger wired. Used by endpoints that call prepare.Solution() and
	// need these values in context. Reserved for solution test endpoint (planned).
	ServerContext context.Context

	// PluginPool manages shared, long-lived plugin processes with lazy
	// initialization and idle eviction. Used by solution endpoints to
	// ensure external plugins from bundle.plugins are available.
	PluginPool *plugin.Pool
}

// NewHandlerContext creates a new HandlerContext with the given dependencies.
func NewHandlerContext(
	cfg *config.Config,
	providerReg *provider.Registry,
	authReg *auth.Registry,
	logger logr.Logger,
	isShuttingDown *int32,
	startTime time.Time,
) *HandlerContext {
	return &HandlerContext{
		Config:           cfg,
		ProviderRegistry: providerReg,
		AuthRegistry:     authReg,
		Logger:           logger,
		IsShuttingDown:   isShuttingDown,
		StartTime:        startTime,
	}
}

// ShuttingDown returns true if the server is in graceful shutdown.
// Returns false when IsShuttingDown is nil (e.g., for export/spec contexts).
func (hc *HandlerContext) ShuttingDown() bool {
	if hc.IsShuttingDown == nil {
		return false
	}
	return atomic.LoadInt32(hc.IsShuttingDown) == 1
}
