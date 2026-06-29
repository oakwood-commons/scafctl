// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/api/endpoints"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/builder"
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

	if err := configureAuthServerMode(ctx, authReg, cfg, lgr); err != nil {
		return fmt.Errorf("configuring auth server mode: %w", err)
	}

	// Parse plugin allowlist at startup boundary (validates "catalog/plugin" format).
	var (
		bareNames  []string
		perCatalog map[string]catalog.PluginPolicy
	)
	if len(cfg.APIServer.Plugins.AllowedPlugins) > 0 {
		parsed, parseErr := plugin.ParseAllowedPlugins(cfg.APIServer.Plugins.AllowedPlugins)
		if parseErr != nil {
			return fmt.Errorf("invalid plugin allowlist: %w", parseErr)
		}
		bareNames = plugin.BareNames(parsed)
		perCatalog = plugin.GroupByCatalog(parsed)
	}

	// AllowedCatalogs controls which catalogs participate in the chain.
	catalogNames := cfg.APIServer.Plugins.AllowedCatalogs

	// If a catalog is in allowedCatalogs but has no plugins assigned in
	// allowedPlugins, backfill it with a deny-all policy so
	// BuildCatalogChain wraps it with an empty allowlist.
	perCatalog = populateCatalogPolicies(catalogNames, perCatalog)

	// Build official provider registry for auto-resolution
	var officialReg *official.Registry
	if cfg.Settings.DisableOfficialProviders {
		lgr.V(1).Info("official provider auto-resolution disabled by settings")
	} else {
		officialReg = official.NewRegistry()
	}
	// Build plugin fetcher for auto-fetching plugin binaries from catalogs.
	// Chain construction respects catalog and per-catalog artifact allowlists.
	var pluginFetcher *plugin.Fetcher
	var pluginCache *plugin.Cache
	if !cfg.APIServer.Plugins.DisableDiskCache {
		cacheMaxSize, sizeErr := pluginCacheMaxSize(cfg)
		if sizeErr != nil {
			return fmt.Errorf("invalid plugin cache size: %w", sizeErr)
		}
		mc, cacheErr := plugin.NewManagedCache(settings.PluginCacheDirFor(opts.CliParams.BinaryName), cacheMaxSize)
		if cacheErr != nil {
			lgr.V(1).Info("managed plugin cache unavailable, falling back to unbounded", "error", cacheErr)
		} else {
			if warmErr := mc.WarmUp(); warmErr != nil {
				lgr.V(1).Info("plugin cache warm-up failed", "error", warmErr)
			}
			pluginCache = mc
		}
	}
	if fetcher, fetchErr := prepare.BuildPluginFetcherWithConfig(ctx, prepare.PluginFetcherOverrides{
		AllowedCatalogs:      catalogNames,
		ChainAllowedCatalogs: catalogNames,
		PerCatalogArtifacts:  perCatalog,
		Cache:                pluginCache,
		NoCache:              cfg.APIServer.Plugins.DisableDiskCache,
	}); fetchErr == nil {
		pluginFetcher = fetcher
	} else {
		lgr.V(1).Info("plugin fetcher not available; official providers will not be pre-loaded", "error", fetchErr)
	}

	// Pre-load official providers (filtered by per-catalog allowlist).
	var pluginClients []*plugin.Client
	if officialReg != nil && pluginFetcher != nil {
		var preloadOpts []PreLoadOption
		preloadOpts = configurePreloadOptions(perCatalog, preloadOpts, official.CatalogName)
		clients, preloadErr := preloadOfficialProviders(ctx, reg, officialReg, pluginFetcher, preloadOpts...)
		if preloadErr != nil {
			lgr.V(0).Info("warning: some official providers could not be pre-loaded", "error", preloadErr)
		}
		pluginClients = clients
	}

	// Create plugin pool (bare-name fast-reject for external plugins).
	pluginPool := buildPluginPool(ctx, cfg, pluginFetcher, reg, lgr, pluginClients, bareNames)

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

func populateCatalogPolicies(catalogNames []string, perCatalog map[string]catalog.PluginPolicy) map[string]catalog.PluginPolicy {
	if len(catalogNames) > 0 {
		if perCatalog == nil {
			perCatalog = make(map[string]catalog.PluginPolicy, len(catalogNames))
		}
		for _, name := range catalogNames {
			if _, exists := perCatalog[name]; !exists {
				perCatalog[name] = catalog.PluginPolicy{Plugins: []string{}}
			}
		}
	}
	return perCatalog
}

// pluginCacheMaxSize returns the configured plugin cache size in bytes.
// Falls back to settings.DefaultPluginCacheMaxSize when the config field is empty.
func pluginCacheMaxSize(cfg *config.Config) (int64, error) {
	raw := cfg.APIServer.Plugins.DiskCacheMaxSize
	if raw == "" {
		raw = settings.DefaultPluginCacheMaxSize
	}
	return builder.ParseByteSize(raw)
}

func configurePreloadOptions(perCatalog map[string]catalog.PluginPolicy, preloadOpts []PreLoadOption, catalogName string) []PreLoadOption {
	// When perCatalog is nil (no allowlist configured), no filter is applied.
	if perCatalog != nil {
		// An allowlist is configured; determine official catalog policy.
		policy, ok := perCatalog[catalogName]
		switch {
		case !ok:
			// catalog absent from policy map: deny all.
			preloadOpts = append(preloadOpts, WithAllowedPlugins([]string{}))
		case policy.AllowAll:
			// Wildcard ("catalog/*"): no filter needed.
		default:
			// Explicit list of allowed plugins.
			preloadOpts = append(preloadOpts, WithAllowedPlugins(policy.Plugins))
		}
	}
	return preloadOpts
}

type preLoadOptions struct {
	allowedPlugins map[string]bool
}

type PreLoadOption func(*preLoadOptions)

func WithAllowedPlugins(plugins []string) PreLoadOption {
	return func(opts *preLoadOptions) {
		if plugins == nil {
			// nil means "no filter" — leave allowedPlugins unchanged.
			return
		}
		// Non-nil (including empty) sets an explicit allowlist.
		// An empty slice means "deny all".
		if opts.allowedPlugins == nil {
			opts.allowedPlugins = make(map[string]bool, len(plugins))
		}
		for _, p := range plugins {
			opts.allowedPlugins[p] = true
		}
	}
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
	opts ...PreLoadOption,
) ([]*plugin.Client, error) {
	options := &preLoadOptions{}
	for _, opt := range opts {
		opt(options)
	}
	lgr := logger.FromContext(ctx)

	deps := buildPreloadDeps(officialReg, reg, options, lgr)

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
	start := time.Now()
	fetchResults, fetchErr := fetcher.FetchPlugins(ctx, deps, nil)
	lgr.V(1).Info("official provider fetch complete", "duration", time.Since(start).String(), "success", fetchErr == nil)
	// Release cache pins — once RegisterFetchedPlugins execs each binary,
	// the OS has it mapped in memory and the on-disk file can be evicted.
	// Placed before the error check so partial results are released on failure.
	defer func() {
		for i := range fetchResults {
			if fetchResults[i].Release != nil {
				fetchResults[i].Release()
			}
		}
	}()
	if fetchErr != nil {
		return nil, fmt.Errorf("fetching official providers: %w", fetchErr)
	}

	clientOpts := plugin.AuthClientOptsFromContext(ctx)

	clients, regErr := plugin.RegisterFetchedPlugins(ctx, reg, fetchResults, nil, clientOpts...)
	if regErr != nil {
		return nil, fmt.Errorf("registering official providers: %w", regErr)
	}

	if lgr != nil {
		lgr.V(0).Info("pre-loaded official providers", "count", len(clients))
	}

	return clients, nil
}

// buildPreloadDeps determines which official providers need to be fetched
// based on the allowlist and what's already registered.
func buildPreloadDeps(
	officialReg *official.Registry,
	reg *provider.Registry,
	options *preLoadOptions,
	lgr *logr.Logger,
) []solution.PluginDependency {
	providers := officialReg.Names()
	deps := make([]solution.PluginDependency, 0, len(providers))
	for _, name := range providers {
		if options.allowedPlugins != nil && !options.allowedPlugins[name] {
			if lgr != nil {
				lgr.V(1).Info("official provider not allowed, skipping pre-load", "provider", name)
			}
			continue
		}
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
	return deps
}

// buildPluginPool creates a Pool configured from the API server settings and
// adopts any pre-loaded official plugin clients into it.
func buildPluginPool(ctx context.Context, cfg *config.Config, fetcher *plugin.Fetcher, reg *provider.Registry, lgr *logr.Logger, preloaded []*plugin.Client, allowedPluginNames []string) *plugin.Pool {
	poolOpts := []plugin.PoolOption{
		plugin.WithIdleTimeout(5 * time.Minute),
		plugin.WithMaxPlugins(50),
		plugin.WithDisableExternal(!cfg.APIServer.Plugins.AllowExternal),
	}
	if len(allowedPluginNames) > 0 {
		poolOpts = append(poolOpts, plugin.WithAllowedPlugins(allowedPluginNames))
	}
	// Wire auth host dependencies so plugins can use host auth
	if authOpts := plugin.AuthClientOptsFromContext(ctx); len(authOpts) > 0 {
		poolOpts = append(poolOpts, plugin.WithClientOptions(authOpts...))
	}
	// Wire gRPC max message size from config
	if cfg.Plugins.GRPCMaxMessageSize > 0 {
		poolOpts = append(poolOpts, plugin.WithClientOptions(plugin.WithGRPCMaxMessageSize(cfg.Plugins.GRPCMaxMessageSize)))
	}
	pool := plugin.NewPool(ctx, fetcher, reg, *lgr, poolOpts...)
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

func authPluginSettings(cfg *config.Config, handlerName string) ([]byte, error) {
	raw, ok := cfg.APIServer.Auth.Handlers[handlerName]
	if !ok || raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal auth plugin settings for %q: %w", handlerName, err)
	}
	return data, nil
}

// activateAuthPluginServerMode iterates registered auth handlers and calls
// ActivateServerMode on each one that has server-mode settings configured
// in cfg.APIServer.Auth.Handlers. This switches plugins from CLI flows to
// server flows (client_credentials, WIF, OBO).
func activateAuthPluginServerMode(ctx context.Context, registry *auth.Registry, cfg *config.Config, lgr *logr.Logger) error {
	if len(cfg.APIServer.Auth.Handlers) == 0 {
		return nil
	}

	if registry == nil {
		return fmt.Errorf("auth registry not available for server-mode activation")
	}

	for handlerName := range cfg.APIServer.Auth.Handlers {
		handler, err := registry.Get(handlerName)
		if err != nil {
			return fmt.Errorf("auth handler %q not found in registry: %w", handlerName, err)
		}

		activator, ok := handler.(auth.ServerMode)
		if !ok {
			return fmt.Errorf("auth handler %q does not support server mode", handlerName)
		}

		settings, err := authPluginSettings(cfg, handlerName)
		if err != nil {
			return fmt.Errorf("auth handler %q settings: %w", handlerName, err)
		}
		if settings == nil {
			return fmt.Errorf("auth handler %q has no server mode settings configured", handlerName)
		}

		if err := activator.ActivateServerMode(ctx, settings); err != nil {
			return fmt.Errorf("activating server mode on auth handler %q: %w", handlerName, err)
		}
		lgr.V(0).Info("activated server mode on auth plugin", "handler", handlerName)
	}

	return nil
}

// disableNonServerModeHandlers disables all auth handlers that were not
// activated in server mode. This prevents CLI-only handlers (device-code,
// browser-based flows) from being invoked in the non-interactive API server.
func disableNonServerModeHandlers(registry *auth.Registry, cfg *config.Config, lgr *logr.Logger) {
	for _, name := range registry.List() {
		if _, configured := cfg.APIServer.Auth.Handlers[name]; configured {
			continue
		}
		if err := registry.Disable(name, "not configured for server mode"); err == nil {
			lgr.V(0).Info("disabled auth handler not configured for server mode", "handler", name)
		}
	}
}

func configureAuthServerMode(ctx context.Context, registry *auth.Registry, cfg *config.Config, lgr *logr.Logger) error {
	// Activate server mode on auth handler plugins for API-mode token acquisition.
	// This tells plugins to switch from CLI flows (device-code, PAT) to server flows
	// (client_credentials, WIF, OBO) using the opaque settings from config.
	if err := activateAuthPluginServerMode(ctx, registry, cfg, lgr); err != nil {
		return fmt.Errorf("activating auth plugin server mode: %w", err)
	}
	if registry != nil {
		// Disable auth handlers that were not activated in server mode.
		// In API mode, only handlers with explicit server-mode settings should
		// be usable — others would fall back to CLI flows (device-code) which
		// cannot work in a non-interactive server process.
		disableNonServerModeHandlers(registry, cfg, lgr)
	}

	return nil
}
