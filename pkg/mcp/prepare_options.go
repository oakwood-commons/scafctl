// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
)

// prepareOptions builds the base set of prepare.Option values used by every
// MCP tool that loads a solution. It mirrors the CLI's `run` wiring so that
// solutions referencing official plugin providers (e.g. directory, identity,
// github) are auto-fetched and resolved the same way from the MCP server as
// from the CLI. Any extra options are appended last so callers can add
// tool-specific behavior (e.g. dry-run, discovery mode).
//
// All wiring is best-effort: when a dependency is unavailable the related
// option is simply omitted, preserving the previous behavior for solutions
// that do not use plugins.
func (s *Server) prepareOptions(ctx context.Context, extra ...prepare.Option) []prepare.Option {
	opts := []prepare.Option{
		prepare.WithRegistry(s.registry),
	}

	// In server mode, delegate provider plugin lifecycle to the shared pool so
	// plugin processes survive across tool calls. Without this, prepare would
	// fetch, register, and kill plugin clients on every call, leaving dead-backed
	// provider wrappers in the shared registry that break subsequent calls with
	// "grpc: the client connection is closing".
	if s.pluginPool != nil {
		opts = append(opts, prepare.WithPluginPool(s.pluginPool))
	}

	// Wire plugin auto-fetch so that bundle.plugins declarations and official
	// plugin providers trigger automatic download from configured catalogs.
	if fetcher, err := prepare.BuildPluginFetcher(ctx); err == nil {
		opts = append(opts, prepare.WithPluginFetcher(fetcher))
	}

	// Wire official provider auto-resolution from context.
	if officialReg := official.RegistryFromContext(ctx); officialReg != nil {
		opts = append(opts, prepare.WithOfficialProviders(officialReg))
	}

	// Pass the auth registry so auth handler plugins can be registered.
	if authReg := auth.RegistryFromContext(ctx); authReg != nil {
		opts = append(opts, prepare.WithAuthRegistry(authReg))
	}

	// Wire auth host deps so that plugin providers can request auth tokens
	// from the host process via the gRPC HostService.
	if authOpts := plugin.AuthClientOptsFromContext(ctx); len(authOpts) > 0 {
		opts = append(opts, prepare.WithClientOptions(authOpts...))
	}

	return append(opts, extra...)
}

// prepareSolution loads path (a local file, URL, catalog or registry
// reference; empty auto-discovers against ctx's working directory) through a
// load.Router and prepares it for execution with prepareOptions. Stdin is
// deliberately not enabled: in stdio mode it carries the MCP protocol, so a
// "-" path fails with load.ErrSourceNotEnabled. The returned Result.Cleanup
// must be deferred.
func (s *Server) prepareSolution(ctx context.Context, path string) (*prepare.Result, error) {
	c, err := source.Classify(path)
	if err != nil {
		return nil, err
	}
	router := load.NewRouterFromContext(ctx, *logger.FromContext(ctx))
	return prepare.SolutionFromLoader(ctx, router, c, s.prepareOptions(ctx)...)
}

// localSolution returns ref as a solution file or directory on the local
// filesystem for tools that read solution files (see load.Local): a local
// path is resolved against ctx's working directory, and a URL, registry, or
// catalog reference is fetched, with its bundle, to a temporary directory.
// On failure it returns the structured tool error to send instead. The
// caller must Close the result.
func localSolution(ctx context.Context, ref string) (*load.LocalSolution, *mcp.CallToolResult) {
	router := load.NewRouterFromContext(ctx, *logger.FromContext(ctx))
	local, err := load.Local(ctx, router, ref, load.DiscoveryRiskLow)
	switch {
	case errors.Is(err, load.ErrFetch):
		return nil, newStructuredError(ErrCodeLoadFailed, err.Error(),
			WithField("path"),
			WithSuggestion("Check the reference exists and the catalog or registry is reachable"),
			WithRelatedTools("catalog_list"),
		)
	case errors.Is(err, load.ErrNoSolutionFound):
		return nil, newStructuredError(ErrCodeNotFound, err.Error(),
			WithField("path"),
			WithSuggestion("Provide the path to a solution file or directory"),
		)
	case err != nil:
		return nil, newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("path"),
			WithSuggestion("Provide a solution file, directory, URL, or catalog reference"),
		)
	}

	// load.Local returns a local path that does not exist for the caller to
	// report.
	if _, err := os.Stat(local.Path); err != nil {
		_ = local.Close()
		return nil, newStructuredError(ErrCodeNotFound, fmt.Sprintf("path not found: %v", err),
			WithField("path"),
			WithSuggestion("Check the path exists and is accessible"),
		)
	}
	return local, nil
}
