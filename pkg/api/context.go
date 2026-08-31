// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// PluginPool is the minimal surface the API server needs from a plugin pool:
// acquiring the external plugins a request depends on (holding a reference for
// the duration of the work) and shutting the pool down on server stop. Both
// *plugin.Pool and *plugin.VersionPool satisfy it, so the server can be wired
// with either implementation.
type PluginPool interface {
	// EnsureAndAcquire loads every provider dependency and takes a reference on
	// each ready entry, returning a release function that drops all acquired
	// references and must be called when the caller is done (typically via
	// defer).
	EnsureAndAcquire(ctx context.Context, deps []solution.PluginDependency) (release func(), err error)
	// Shutdown kills all managed plugin processes.
	Shutdown()
}

var (
	_ PluginPool = (*plugin.Pool)(nil)
	_ PluginPool = (*plugin.VersionPool)(nil)
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
	PluginPool PluginPool

	// CompositeRegistry is the shared builtin+external provider registry used
	// by solution endpoints to build a request-scoped registry via
	// prepare.ScopeSolutionProviders. Nil when only the legacy
	// ProviderRegistry surface is wired.
	CompositeRegistry *provider.CompositeRegistry

	// CatalogIndex is the config-derived catalog topology. It maps a normalized
	// OCI origin ("registry" or "registry/repository") to the configured
	// catalog's alias so handlers can bind a fully-qualified provider
	// reference's raw registry to a concrete configured catalog. It is built
	// once at server start from the configured catalogs; its lookups miss for
	// every origin when no remote catalogs are configured.
	CatalogIndex *catalogindex.Index
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
