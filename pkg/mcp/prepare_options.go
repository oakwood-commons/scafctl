// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
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
