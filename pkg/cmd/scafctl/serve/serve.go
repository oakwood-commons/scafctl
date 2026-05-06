// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/api/endpoints"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// Options holds the options for the serve command.
type Options struct {
	Host       string
	Port       int
	TLSCert    string
	TLSKey     string
	EnableTLS  bool
	APIVersion string
	CliParams  *settings.Run
	IOStreams  *terminal.IOStreams
}

// CommandServe creates the `scafctl serve` command.
func CommandServe(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &Options{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the REST API server",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Start the scafctl REST API server.

			The API server exposes all major scafctl features as REST endpoints:
			solutions, providers, catalogs, schemas, eval, config, and more.

			The server uses chi for routing and Huma for OpenAPI-compliant
			endpoint registration, with support for Entra OIDC authentication,
			Prometheus metrics, OpenTelemetry tracing, and audit logging.

			Health probes at /health, /health/live, and /health/ready bypass
			authentication for orchestrator integration.

			OpenAPI documentation is served at /{version}/docs and the spec
			at /{version}/openapi.json.
		`), settings.CliBinaryName, cliParams.BinaryName),
		Example: strings.ReplaceAll(heredoc.Doc(`
			# Start the API server with defaults (port 8080)
			scafctl serve

			# Start on a custom port
			scafctl serve --port 9090

			# Start with TLS
			scafctl serve --enable-tls --tls-cert cert.pem --tls-key key.pem

			# Export OpenAPI spec without starting the server
			scafctl serve openapi --format yaml --output openapi.yaml
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), opts)
		},
	}

	cmd.Flags().StringVar(&opts.Host, "host", "", "Host to bind to (default from config or 127.0.0.1)")
	cmd.Flags().IntVar(&opts.Port, "port", 0, "Port to listen on (default from config or 8080)")
	cmd.Flags().StringVar(&opts.TLSCert, "tls-cert", "", "Path to TLS certificate")
	cmd.Flags().StringVar(&opts.TLSKey, "tls-key", "", "Path to TLS private key")
	cmd.Flags().BoolVar(&opts.EnableTLS, "enable-tls", false, "Enable TLS")
	cmd.Flags().StringVar(&opts.APIVersion, "api-version", "", "API version prefix (default from config or v1)")

	cmd.AddCommand(CommandOpenAPI(cliParams, ioStreams))

	return cmd
}

func runServe(ctx context.Context, opts *Options) error {
	lgr := logger.FromContext(ctx)
	cfg := config.FromContext(ctx)
	authReg := auth.RegistryFromContext(ctx)

	// Apply CLI flag overrides to config
	if cfg == nil {
		cfg = &config.Config{}
	}
	if opts.Host != "" {
		cfg.APIServer.Host = opts.Host
	}
	if opts.Port > 0 {
		cfg.APIServer.Port = opts.Port
	}
	if opts.EnableTLS {
		cfg.APIServer.TLS.Enabled = true
	}
	if opts.TLSCert != "" {
		cfg.APIServer.TLS.Cert = opts.TLSCert
	}
	if opts.TLSKey != "" {
		cfg.APIServer.TLS.Key = opts.TLSKey
	}
	if opts.APIVersion != "" {
		cfg.APIServer.APIVersion = opts.APIVersion
	}

	// Load provider registry
	reg, err := builtin.DefaultRegistry(ctx)
	if err != nil {
		return fmt.Errorf("initializing provider registry: %w", err)
	}

	// Build official provider registry for auto-resolution
	var officialReg *official.Registry
	if cfg.Settings.DisableOfficialProviders {
		lgr.V(1).Info("official provider auto-resolution disabled by settings")
	} else {
		officialReg = official.NewRegistry()
	}

	// Build plugin fetcher for auto-fetching plugin binaries from catalogs
	var pluginFetcher *plugin.Fetcher
	if fetcher, fetchErr := prepare.BuildPluginFetcherWithConfig(ctx, prepare.PluginFetcherOverrides{
		AllowedCatalogs: cfg.APIServer.Plugins.AllowedCatalogs,
	}); fetchErr == nil {
		pluginFetcher = fetcher
	} else {
		lgr.V(1).Info("plugin fetcher not available; official providers will not be pre-loaded", "error", fetchErr)
	}

	// Pre-load all official providers into the registry at server startup.
	// This avoids per-request gRPC spawn overhead and ensures the extracted
	// providers (exec, directory, git, etc.) are available for all endpoints.
	var pluginClients []*plugin.Client
	if officialReg != nil && pluginFetcher != nil {
		clients, preloadErr := preloadOfficialProviders(ctx, reg, officialReg, pluginFetcher)
		if preloadErr != nil {
			lgr.V(0).Info("warning: some official providers could not be pre-loaded", "error", preloadErr)
		}
		pluginClients = clients
	}

	// Create plugin pool for managing external plugin lifecycle.
	// Pre-loaded official providers are adopted into the pool so their
	// lifecycle (shutdown) is managed consistently.
	pluginPool := buildPluginPool(ctx, cfg, pluginFetcher, reg, lgr, pluginClients)

	// Build server options
	serverOpts := []api.ServerOption{
		api.WithServerLogger(*lgr),
		api.WithServerConfig(cfg),
		api.WithServerRegistry(reg),
		api.WithServerContext(ctx),
		api.WithServerVersion(settings.VersionInformation.BuildVersion),
		api.WithServerPluginPool(pluginPool),
	}
	if authReg != nil {
		serverOpts = append(serverOpts, api.WithServerAuthRegistry(authReg))
	}
	if pluginFetcher != nil {
		serverOpts = append(serverOpts, api.WithServerPluginFetcher(pluginFetcher))
	}
	if officialReg != nil {
		serverOpts = append(serverOpts, api.WithServerOfficialProviders(officialReg))
	}

	// Create server
	srv, err := api.NewServer(serverOpts...)
	if err != nil {
		return fmt.Errorf("creating API server: %w", err)
	}

	// Setup middleware (two-layer: global + API)
	// Use the server's own cancellable context so the rate-limit cleanup
	// goroutine is stopped when the server shuts down, not when the outer
	// cobra command context is eventually cancelled.
	apiRouter, err := api.SetupMiddleware(srv.Context(), srv.Router(), &cfg.APIServer, *lgr)
	if err != nil {
		return fmt.Errorf("setting up middleware: %w", err)
	}
	srv.SetAPIRouter(apiRouter)

	// Initialize Huma API
	srv.InitAPI()

	// Register all endpoints
	handlerCtx := srv.HandlerCtx()
	endpoints.RegisterAll(srv.API(), srv.Router(), handlerCtx)

	// Start server (blocks until SIGINT/SIGTERM)
	return srv.Start()
}

// preloadOfficialProviders fetches all official providers and registers them
// into the provider registry. This is called at server startup so that
// extracted providers (exec, directory, git, etc.) are immediately available
// for all API endpoints without per-request gRPC spawn overhead.
//
// Returns the plugin clients that must be killed on server shutdown.
// Logs warnings for providers that fail to load but does not fail the
// server startup.
func preloadOfficialProviders(
	ctx context.Context,
	reg *provider.Registry,
	officialReg *official.Registry,
	fetcher *plugin.Fetcher,
) ([]*plugin.Client, error) {
	lgr := logger.FromContext(ctx)

	// Build plugin dependencies for all official providers
	providers := officialReg.Names()
	deps := make([]solution.PluginDependency, 0, len(providers))
	for _, name := range providers {
		p, ok := officialReg.Get(name)
		if !ok {
			continue
		}
		// Skip providers already in the registry (e.g., builtins)
		if reg.Has(name) {
			if lgr != nil {
				lgr.V(1).Info("official provider already registered, skipping pre-load", "provider", name)
			}
			continue
		}
		deps = append(deps, p.ToPluginDependency())
	}

	if len(deps) == 0 {
		return nil, nil
	}

	if fetcher == nil {
		if lgr != nil {
			lgr.V(1).Info("plugin fetcher not available; cannot pre-load official providers")
		}
		return nil, nil
	}

	if lgr != nil {
		names := make([]string, len(deps))
		for i, d := range deps {
			names[i] = d.Name
		}
		lgr.V(0).Info("pre-loading official providers for API server", "providers", names)
	}

	fetchResults, fetchErr := fetcher.FetchPlugins(ctx, deps, nil)
	if fetchErr != nil {
		return nil, fmt.Errorf("fetching official providers: %w", fetchErr)
	}

	clients, regErr := plugin.RegisterFetchedPlugins(ctx, reg, fetchResults, nil)
	if regErr != nil {
		return nil, fmt.Errorf("registering official providers: %w", regErr)
	}

	if lgr != nil {
		lgr.V(0).Info("pre-loaded official providers", "count", len(clients))
	}

	return clients, nil
}

// buildPluginPool creates a Pool configured from the API server settings and
// adopts any pre-loaded official plugin clients into it.
func buildPluginPool(ctx context.Context, cfg *config.Config, fetcher *plugin.Fetcher, reg *provider.Registry, lgr *logr.Logger, preloaded []*plugin.Client) *plugin.Pool {
	poolOpts := []plugin.PoolOption{
		plugin.WithIdleTimeout(5 * time.Minute),
		plugin.WithMaxPlugins(50),
		plugin.WithDisableExternal(!cfg.APIServer.Plugins.AllowExternal),
	}
	if len(cfg.APIServer.Plugins.AllowedPlugins) > 0 {
		poolOpts = append(poolOpts, plugin.WithAllowedPlugins(cfg.APIServer.Plugins.AllowedPlugins))
	}
	pool := plugin.NewPool(fetcher, reg, *lgr, poolOpts...)
	for _, c := range preloaded {
		// Query the client for provider names it registered so the pool can
		// unregister them on eviction/death.
		var providers []string
		if names, err := c.GetProviders(ctx); err == nil {
			providers = names
		}
		pool.Adopt(c.Name(), c, solution.PluginDependency{
			Name: c.Name(),
			Kind: solution.PluginKindProvider,
		}, providers)
	}
	return pool
}
