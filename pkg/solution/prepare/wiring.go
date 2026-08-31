// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
)

// CLIWiring holds the neutral, CLI-agnostic inputs needed to assemble the
// []Option passed to Solution. It deliberately contains only plain scalars and
// interfaces (no *settings.Run or command-specific option structs) so that any
// embedder or command can populate it. Context-sourced dependencies (config,
// auth registry, official registry, plugin fetcher, auth client options) are
// read from ctx by OptionsFromContext rather than carried here.
type CLIWiring struct {
	// File is the already-resolved solution path (or "-" for stdin). It is used
	// only to decide whether to load an adjacent lock file; auto-discovery of an
	// empty File is the caller's responsibility.
	File string
	// Getter is an optional custom solution getter (dependency injection).
	Getter get.Interface
	// Registry is an optional custom provider registry (dependency injection).
	Registry *provider.Registry
	// Stdin is the reader used for stdin-based solution loading (File == "-").
	Stdin io.Reader
	// NoCache disables artifact caching when loading from the catalog.
	NoCache bool
	// Strict disables auto-resolution of official providers.
	Strict bool
	// DiscoveryMode controls which file names auto-discovery searches for.
	DiscoveryMode settings.DiscoveryMode
	// LockMode is the raw --lock-mode flag value (strict, constrained,
	// bestEffort). Empty leaves the mode unset (prepare applies its default).
	LockMode string
	// MetricsOut, when non-nil, enables metrics collection to this writer.
	MetricsOut io.Writer
	// PluginConfig, when non-nil, is sent to plugin providers after
	// registration via ConfigureProvider.
	PluginConfig *plugin.ProviderConfig
	// DebugLogging enables plugin client debug logging.
	DebugLogging bool
}

// OptionsFromContext assembles the []Option for Solution from the neutral
// CLIWiring plus dependencies read from ctx (config, auth registry, official
// registry, plugin fetcher, auth client options). It is the single source of
// truth for how CLI commands (run, render, ...) wire a solution for execution,
// keeping their preparation behavior identical.
func OptionsFromContext(ctx context.Context, w CLIWiring) ([]Option, error) {
	var opts []Option

	if w.Getter != nil {
		opts = append(opts, WithGetter(w.Getter))
	}
	if w.NoCache {
		opts = append(opts, WithNoCache())
	}
	if w.Registry != nil {
		opts = append(opts, WithRegistry(w.Registry))
	}
	if w.Stdin != nil {
		opts = append(opts, WithStdin(w.Stdin))
	}
	if w.MetricsOut != nil {
		opts = append(opts, WithMetrics(w.MetricsOut))
	}

	// Plugin client configuration (config, debug logging, gRPC message size).
	if w.PluginConfig != nil {
		opts = append(opts, WithPluginConfig(w.PluginConfig))
	}
	if w.DebugLogging {
		opts = append(opts, WithClientOptions(plugin.WithDebugLogging()))
	}
	if cfg := config.FromContext(ctx); cfg != nil && cfg.Plugins.GRPCMaxMessageSize > 0 {
		opts = append(opts, WithClientOptions(plugin.WithGRPCMaxMessageSize(cfg.Plugins.GRPCMaxMessageSize)))
	}

	// Wire auth host deps so that plugin providers can request auth tokens
	// from the host process via gRPC HostService.
	if authOpts := plugin.AuthClientOptsFromContext(ctx); len(authOpts) > 0 {
		opts = append(opts, WithClientOptions(authOpts...))
	}

	if w.DiscoveryMode != settings.DiscoveryModeDefault {
		opts = append(opts, WithDiscoveryMode(w.DiscoveryMode))
	}

	// Wire plugin auto-fetch so that bundle.plugins declarations trigger
	// automatic download from configured catalogs.
	if fetcher, err := BuildPluginFetcher(ctx); err == nil {
		opts = append(opts, WithPluginFetcher(fetcher))
	}

	// Load lock file for reproducible plugin resolution. The lock file lives
	// alongside the solution file (e.g., solution.lock). Only attempt this for
	// local file paths — catalog names, remote refs, and URLs resolve to "."
	// via filepath.Dir, which would incorrectly load an unrelated lock file
	// from the current working directory.
	if w.File != "" && w.File != "-" && !get.IsCatalogReference(w.File) {
		lf, lockErr := loadAdjacentLockFile(w.File)
		if lockErr != nil {
			return nil, lockErr
		}
		if lf != nil {
			opts = append(opts, WithLockFile(lf))
			opts = append(opts, WithLockPlugins(lf.Plugins))
		}
	}

	// Wire lock mode for BuildProviderDependency.
	if w.LockMode != "" {
		parsed, parseErr := ParseLockMode(w.LockMode)
		if parseErr != nil {
			return nil, parseErr
		}
		opts = append(opts, WithLockMode(parsed))
	}

	// Pass auth registry so auth handler plugins can be registered.
	if authReg := auth.RegistryFromContext(ctx); authReg != nil {
		opts = append(opts, WithAuthRegistry(authReg))
	}

	// Wire official provider auto-resolution from context.
	if officialReg := official.RegistryFromContext(ctx); officialReg != nil {
		opts = append(opts, WithOfficialProviders(officialReg))
	}
	if w.Strict {
		opts = append(opts, WithStrict(true))
	}

	return opts, nil
}

// ParseLockMode parses the raw --lock-mode flag value into a LockMode constant.
func ParseLockMode(s string) (LockMode, error) {
	switch s {
	case "strict":
		return LockModeStrict, nil
	case "constrained":
		return LockModeConstrained, nil
	case "bestEffort":
		return LockModeBestEffort, nil
	default:
		return 0, fmt.Errorf("invalid --lock-mode %q; must be one of: strict, constrained, bestEffort", s)
	}
}

// loadAdjacentLockFile loads a lock file adjacent to the solution file. It
// returns (nil, nil) when no lock file exists (a benign no-op), but propagates
// any other error -- an unreadable, malformed, or unsupported-version lock --
// so its pins are never silently dropped (which would degrade strict mode to a
// misleading missing-lock error and best-effort mode to unpinned execution).
func loadAdjacentLockFile(solutionPath string) (*bundler.LockFile, error) {
	lockPath := filepath.Join(filepath.Dir(solutionPath), bundler.DefaultLockFileName)
	return bundler.LoadLockFile(lockPath)
}
