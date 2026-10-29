// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package prepare

import (
	"context"
	"fmt"
	"io"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// CLIWiring holds the neutral, CLI-agnostic inputs needed to assemble the
// []Option passed to SolutionFromLoader. It deliberately contains only plain
// scalars and interfaces (no *settings.Run or command-specific option structs)
// so that any embedder or command can populate it. Context-sourced
// dependencies (config, auth registry, official registry, plugin fetcher, auth
// client options) are read from ctx by OptionsFromContext rather than carried
// here. Source-loading concerns (stdin, cache, discovery mode) belong to the
// load.Router, not here.
type CLIWiring struct {
	// Registry is an optional custom provider registry (dependency injection).
	Registry *provider.Registry
	// Strict disables auto-resolution of official providers.
	Strict bool
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

// NewCLIWiring returns a CLIWiring with the fields derived from CLI settings
// filled in: the plugin provider config and plugin debug logging. Callers set
// the command-specific fields. params may be nil.
func NewCLIWiring(params *settings.Run) CLIWiring {
	var w CLIWiring
	if params != nil {
		w.PluginConfig = &plugin.ProviderConfig{
			Quiet:      params.IsQuiet,
			NoColor:    params.NoColor,
			BinaryName: params.BinaryName,
		}
		w.DebugLogging = logger.IsDebugLevel(params.MinLogLevel)
	}
	return w
}

// OptionsFromContext assembles the []Option for SolutionFromLoader from the
// neutral CLIWiring plus dependencies read from ctx (config, auth registry,
// official registry, plugin fetcher, auth client options). It is the single
// source of truth for how CLI commands (run, render, ...) wire a solution for
// execution, keeping their preparation behavior identical.
func OptionsFromContext(ctx context.Context, w CLIWiring) ([]Option, error) {
	var opts []Option

	if w.Registry != nil {
		opts = append(opts, WithRegistry(w.Registry))
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

	// Wire plugin auto-fetch so that bundle.plugins declarations trigger
	// automatic download from configured catalogs. A build failure is
	// non-fatal: auto-fetch is simply disabled (see BuildPluginFetcher).
	if fetcher, err := BuildPluginFetcher(ctx); err == nil {
		opts = append(opts, WithPluginFetcher(fetcher))
	}

	// The adjacent solution.lock of a local or auto-discovered solution is read
	// by the load package (MaterializedSolution.Lock) and applied by
	// SolutionFromLoader, so it is not wired here.

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
