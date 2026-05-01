// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package prepare provides a standalone function for loading and preparing
// a solution for execution. It decouples solution preparation from CLI-specific
// types, making it reusable by both CLI commands and the MCP server.
package prepare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/solutionprovider"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
)

// Option configures the PrepareSolution function.
type Option func(*prepareConfig)

type prepareConfig struct {
	getter            get.Interface
	registry          *provider.Registry
	authRegistry      *auth.Registry
	stdin             io.Reader
	showMetrics       bool
	metricsOut        io.Writer
	pluginFetcher     *plugin.Fetcher
	lockPlugins       []bundler.LockPlugin
	noCache           bool
	pluginCfg         *plugin.ProviderConfig
	clientOpts        []plugin.ClientOption
	discoveryMode     settings.DiscoveryMode
	officialProviders *official.Registry
	strict            bool
}

// WithGetter provides a custom solution getter. If not set, one is created
// from context (with catalog resolution support).
func WithGetter(g get.Interface) Option {
	return func(c *prepareConfig) {
		c.getter = g
	}
}

// WithRegistry provides a custom provider registry. If not set,
// builtin.DefaultRegistry is used.
func WithRegistry(r *provider.Registry) Option {
	return func(c *prepareConfig) {
		c.registry = r
	}
}

// WithStdin provides a reader for stdin-based solution loading (path == "-").
func WithStdin(r io.Reader) Option {
	return func(c *prepareConfig) {
		c.stdin = r
	}
}

// WithMetrics enables metrics collection and specifies where to write metrics output.
func WithMetrics(out io.Writer) Option {
	return func(c *prepareConfig) {
		c.showMetrics = true
		c.metricsOut = out
	}
}

// WithAuthRegistry provides an auth handler registry for registering
// auth handler plugins. If not set, auth handler plugin loading is skipped.
func WithAuthRegistry(r *auth.Registry) Option {
	return func(c *prepareConfig) {
		c.authRegistry = r
	}
}

// WithPluginFetcher provides a plugin fetcher for auto-fetching plugin
// binaries from catalogs at runtime. If not set, plugin auto-fetching
// is skipped (plugins must be available via --plugin-dir).
func WithPluginFetcher(f *plugin.Fetcher) Option {
	return func(c *prepareConfig) {
		c.pluginFetcher = f
	}
}

// WithLockPlugins provides lock file plugin entries for reproducible
// plugin resolution. When provided, pinned versions and digests are
// used instead of resolving constraints against catalogs.
func WithLockPlugins(plugins []bundler.LockPlugin) Option {
	return func(c *prepareConfig) {
		c.lockPlugins = plugins
	}
}

// WithNoCache disables artifact caching when loading solutions from the catalog.
// When set, the catalog is always queried directly, bypassing the filesystem cache.
func WithNoCache() Option {
	return func(c *prepareConfig) {
		c.noCache = true
	}
}

// WithPluginConfig provides configuration that is sent to plugin providers
// after registration via ConfigureProvider. If not set, plugins use defaults.
func WithPluginConfig(cfg *plugin.ProviderConfig) Option {
	return func(c *prepareConfig) {
		c.pluginCfg = cfg
	}
}

// WithClientOptions provides options for plugin client creation, such as
// host-side dependencies (secrets, auth) for callback services.
func WithClientOptions(opts ...plugin.ClientOption) Option {
	return func(c *prepareConfig) {
		c.clientOpts = append(c.clientOpts, opts...)
	}
}

// WithDiscoveryMode sets the discovery mode used when auto-discovering
// solution files. See settings.DiscoveryMode for available modes.
func WithDiscoveryMode(mode settings.DiscoveryMode) Option {
	return func(c *prepareConfig) {
		c.discoveryMode = mode
	}
}

// WithOfficialProviders provides an official provider registry for
// auto-resolving missing providers at runtime. When set (and strict is
// false), providers not found in the registry are checked against the
// official list and auto-fetched via the plugin fetcher.
func WithOfficialProviders(r *official.Registry) Option {
	return func(c *prepareConfig) {
		c.officialProviders = r
	}
}

// WithStrict disables auto-resolution of official providers. When strict
// is true, missing providers produce an error instructing the user to
// declare them explicitly in bundle.plugins.
func WithStrict(strict bool) Option {
	return func(c *prepareConfig) {
		c.strict = strict
	}
}

// Result holds the output of PrepareSolution.
type Result struct {
	// Solution is the loaded and prepared solution.
	Solution *solution.Solution `json:"solution" yaml:"solution" doc:"The loaded solution"`
	// Registry is the provider registry with all providers registered,
	// including the solution provider.
	Registry *provider.Registry `json:"-" yaml:"-"`
	// SolutionDir is the directory containing the solution file, resolved to
	// an absolute path. Empty when loaded from stdin or a catalog reference.
	// Callers can use this to set provider.WithSolutionDirectory for relative
	// path resolution during execution.
	SolutionDir string `json:"solutionDir,omitempty" yaml:"solutionDir,omitempty" doc:"Directory containing the solution file"`
	// Cleanup must be deferred by the caller. It handles temp directory
	// removal, working directory restoration, and metrics output.
	Cleanup func() `json:"-" yaml:"-"`
	// DiscoveredFrom holds metadata about how the solution file was discovered.
	// Only populated when auto-discovery is used (path was empty).
	DiscoveredFrom get.DiscoveryResult `json:"-" yaml:"-"`
}

// Solution loads a solution from the given path, extracts any bundle,
// merges plugin defaults, sets up the provider registry, and registers the
// solution provider. The returned Result.Cleanup function must be deferred.
//
// This function is the standalone equivalent of the CLI's
// sharedResolverOptions.prepareSolutionForExecution method, decoupled from
// CLI-specific types so it can be used by the MCP server and other callers.
func Solution(ctx context.Context, path string, opts ...Option) (*Result, error) {
	cfg := &prepareConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	lgr := logger.FromContext(ctx)

	// Enable metrics collection if requested
	if cfg.showMetrics {
		provider.GlobalMetrics.Enable()
	}

	// Get or create the solution getter
	getter := cfg.getter
	if getter == nil {
		getter = NewDefaultGetter(ctx, cfg.noCache)
	}

	// When discovery mode is set and no explicit path is provided,
	// perform discovery up front so we can capture the result metadata.
	var discoveredFrom get.DiscoveryResult
	if path == "" && cfg.discoveryMode != settings.DiscoveryModeDefault {
		if g, ok := getter.(*get.Getter); ok {
			g.SetDiscoveryMode(cfg.discoveryMode)
			path = g.FindSolution()
			discoveredFrom = g.LastDiscoveryResult()
		}
	}

	// Load the solution (with bundle if available)
	sol, bundleDir, err := loadSolutionWithBundle(ctx, getter, path, cfg.stdin)
	if err != nil {
		return nil, err
	}

	// Determine the solution directory for relative path resolution.
	// For file-based loading: use the file's parent directory.
	// For bundles (including catalog bundles): use the bundle extraction directory.
	// For stdin or unbundled catalog references: leave empty (falls back to CWD).
	//
	// Prefer sol.GetPath() over the original `path` argument because when
	// auto-discovery is performed inside GetWithBundle (path == ""), the
	// original path stays empty while the solution object carries the resolved
	// file path set by FromLocalFileSystem. Fall back to `path` for callers
	// that use custom getters (e.g. test mocks) that don't call SetPath.
	var solutionDir string
	resolvedPath := sol.GetPath()
	if resolvedPath == "" {
		resolvedPath = path
	}
	isCatalogRef := strings.HasPrefix(resolvedPath, "catalog:")
	switch {
	case bundleDir != "":
		solutionDir = bundleDir
	case resolvedPath != "" && resolvedPath != "-" && !isCatalogRef:
		absPath, absErr := provider.AbsFromContext(ctx, resolvedPath)
		if absErr == nil {
			solutionDir = filepath.Dir(absPath)
		}
	case isCatalogRef:
		// Unbundled catalog solutions have no local directory tree.
		// Providers that reference relative paths (e.g. directory provider)
		// will resolve against CWD, which is likely not the intended base.
		lgr.V(1).Info("solution loaded from catalog without bundle; relative paths in providers will resolve against CWD. "+
			"If this is unintended, run 'catalog pull' first to extract files locally.",
			"path", sol.GetPath())
	}

	// Build cleanup function
	cleanup := func() {
		if cfg.showMetrics && cfg.metricsOut != nil {
			writeMetrics(cfg.metricsOut)
		}
		if bundleDir != "" {
			os.RemoveAll(bundleDir)
		}
	}

	// Change to bundle directory if needed
	if bundleDir != "" {
		originalDir, wdErr := os.Getwd()
		if wdErr != nil {
			cleanup()
			return nil, fmt.Errorf("failed to get working directory: %w", wdErr)
		}
		if chErr := os.Chdir(bundleDir); chErr != nil {
			cleanup()
			return nil, fmt.Errorf("failed to change to bundle directory: %w", chErr)
		}
		origCleanup := cleanup
		cleanup = func() {
			_ = os.Chdir(originalDir)
			origCleanup()
		}
		if lgr != nil {
			lgr.V(1).Info("using bundle extraction directory as working directory", "dir", bundleDir)
		}
	}

	if lgr != nil {
		lgr.V(1).Info("loaded solution",
			"name", sol.Metadata.Name,
			"version", sol.Metadata.Version,
			"hasResolvers", sol.Spec.HasResolvers(),
			"hasWorkflow", sol.Spec.HasWorkflow())
	}

	// Merge plugin defaults into provider inputs before DAG construction
	if len(sol.Bundle.Plugins) > 0 {
		bundler.MergePluginDefaults(sol)
		if lgr != nil {
			lgr.V(1).Info("merged plugin defaults", "pluginCount", len(sol.Bundle.Plugins))
		}
	}

	// Set up provider registry
	reg := cfg.registry
	if reg == nil {
		var regErr error
		reg, regErr = builtin.DefaultRegistry(ctx)
		if regErr != nil {
			if lgr != nil {
				lgr.V(0).Info("warning: failed to register some providers", "error", regErr)
			}
			reg = provider.GetGlobalRegistry()
		}
	}

	// Inject host metadata into plugin config so providers like "metadata"
	// can return runtime information about the scafctl process.
	injectHostMetadataSettings(cfg.pluginCfg, sol)

	// Inject httpClient settings (e.g. allowPrivateIPs) from the app config
	// so external plugins can apply the same SSRF policy as the host.
	injectHTTPClientSettings(ctx, cfg.pluginCfg)

	// Auto-fetch and register plugin binaries from catalogs
	var pluginClients []*plugin.Client
	if len(sol.Bundle.Plugins) > 0 && cfg.pluginFetcher != nil {
		fetchResults, fetchErr := cfg.pluginFetcher.FetchPlugins(ctx, sol.Bundle.Plugins, cfg.lockPlugins)
		if fetchErr != nil {
			cleanup()
			return nil, fmt.Errorf("auto-fetching plugins: %w", fetchErr)
		}

		clients, regErr := plugin.RegisterFetchedPlugins(ctx, reg, fetchResults, cfg.pluginCfg, cfg.clientOpts...)
		if regErr != nil {
			cleanup()
			return nil, fmt.Errorf("registering fetched plugins: %w", regErr)
		}
		pluginClients = clients

		// Register auth handler plugins if auth registry is available
		if cfg.authRegistry != nil {
			authClients, authRegErr := plugin.RegisterFetchedAuthHandlerPlugins(ctx, cfg.authRegistry, fetchResults, cfg.pluginCfg, cfg.clientOpts...)
			if authRegErr != nil {
				cleanup()
				return nil, fmt.Errorf("registering fetched auth handler plugins: %w", authRegErr)
			}
			// Add auth handler client cleanup
			origCleanup2 := cleanup
			cleanup = func() {
				for _, c := range authClients {
					c.Kill()
				}
				origCleanup2()
			}
		}

		if lgr != nil {
			for _, r := range fetchResults {
				src := "catalog"
				if r.FromCache {
					src = "cache"
				}
				lgr.V(1).Info("plugin loaded",
					"name", r.Name,
					"version", r.Version,
					"source", src)
			}
		}

		// Add plugin cleanup to the cleanup chain
		origCleanup := cleanup
		cleanup = func() {
			for _, c := range pluginClients {
				c.Kill()
			}
			origCleanup()
		}
	}

	// Auto-resolve official providers that are referenced in the solution
	// but not already registered. This runs after the explicit plugin fetch
	// so that declared bundle.plugins always take precedence.
	officialClients, officialErr := autoResolveOfficialProviders(ctx, sol, reg, cfg)
	if officialErr != nil {
		cleanup()
		return nil, officialErr
	}
	if len(officialClients) > 0 {
		origCleanup := cleanup
		cleanup = func() {
			for _, c := range officialClients {
				c.Kill()
			}
			origCleanup()
		}
	}

	// Register the solution provider
	if !reg.Has(solutionprovider.ProviderName) {
		solOpts := []solutionprovider.Option{
			solutionprovider.WithLoader(getter),
			solutionprovider.WithRegistry(reg),
		}
		if cfg.officialProviders != nil {
			solOpts = append(solOpts, solutionprovider.WithOfficialProviders(cfg.officialProviders))
		}
		if cfg.pluginFetcher != nil {
			solOpts = append(solOpts, solutionprovider.WithPluginFetcher(cfg.pluginFetcher))
		}
		if cfg.pluginCfg != nil {
			solOpts = append(solOpts, solutionprovider.WithPluginConfig(cfg.pluginCfg))
		}
		if len(cfg.clientOpts) > 0 {
			solOpts = append(solOpts, solutionprovider.WithClientOptions(cfg.clientOpts...))
		}
		solProvider := solutionprovider.New(solOpts...)
		if err := reg.Register(solProvider); err != nil {
			cleanup()
			return nil, fmt.Errorf("registering solution provider: %w", err)
		}
		// Add solution provider cleanup to kill child plugin clients.
		origCleanup := cleanup
		cleanup = func() {
			solProvider.Close()
			origCleanup()
		}
	}

	return &Result{
		Solution:       sol,
		Registry:       reg,
		SolutionDir:    solutionDir,
		Cleanup:        cleanup,
		DiscoveredFrom: discoveredFrom,
	}, nil
}

// NewDefaultGetter creates a default solution getter with catalog and remote resolution support.
// When noCache is true, the artifact cache is disabled so the catalog is always queried directly.
func NewDefaultGetter(ctx context.Context, noCache bool) get.Interface {
	lgr := logger.FromContext(ctx)

	var getterOpts []get.Option
	if lgr != nil {
		getterOpts = append(getterOpts, get.WithLogger(*lgr))

		localCatalog, err := catalog.NewLocalCatalog(*lgr)
		if err == nil {
			// Build SolutionResolverOptions with optional artifact cache
			resolverOpts := []catalog.SolutionResolverOption{
				catalog.WithResolverNoCache(noCache),
				catalog.WithResolverRemoteCatalogs(catalog.RemoteCatalogsFromContext(ctx, *lgr)),
			}
			if !noCache {
				artifactCache := cache.NewArtifactCache(paths.ArtifactCacheDir(), settings.DefaultArtifactCacheTTL)
				resolverOpts = append(resolverOpts, catalog.WithResolverArtifactCache(artifactCache))
			}
			catResolver := catalog.NewSolutionResolver(localCatalog, *lgr, resolverOpts...)
			getterOpts = append(getterOpts, get.WithCatalogResolver(catResolver))
		} else {
			lgr.V(1).Info("catalog not available for solution resolution", "error", err)
		}

		// Wire up remote resolver for Docker-style OCI references
		credStore, credErr := catalog.NewCredentialStore(*lgr)
		if credErr != nil {
			lgr.V(1).Info("credential store not available for remote resolution", "error", credErr)
		}
		remoteResolver := catalog.NewRemoteSolutionResolver(catalog.RemoteSolutionResolverConfig{
			CredentialStore: credStore,
			AuthHandlerFunc: func(registry string) auth.Handler {
				cfg := config.FromContext(ctx)

				// Check catalog config for an explicit authProvider matching this registry.
				if cfg != nil {
					for _, cat := range cfg.Catalogs {
						if cat.URL == "" || cat.AuthProvider == "" {
							continue
						}
						host, _ := catalog.ParseCatalogURL(cat.URL)
						if host == registry {
							if h, err := auth.GetHandler(ctx, cat.AuthProvider); err == nil {
								return h
							}
						}
					}
				}

				// Fall back to inference from registry host.
				var customHandlers []config.CustomOAuth2Config
				if cfg != nil {
					customHandlers = cfg.Auth.CustomOAuth2
				}
				handlerName := catalog.InferAuthHandler(registry, customHandlers)
				if handlerName == "" {
					return nil
				}
				h, err := auth.GetHandler(ctx, handlerName)
				if err != nil {
					return nil
				}
				return h
			},
			AuthScopeFunc: func(registry string) string {
				cfg := config.FromContext(ctx)
				if cfg == nil {
					return ""
				}
				for _, cat := range cfg.Catalogs {
					if cat.URL == "" || cat.AuthScope == "" {
						continue
					}
					host, _ := catalog.ParseCatalogURL(cat.URL)
					if host == registry {
						return cat.AuthScope
					}
				}
				return ""
			},
			Logger: *lgr,
		})
		getterOpts = append(getterOpts, get.WithRemoteResolver(remoteResolver))
	}

	return get.NewGetterFromContext(ctx, getterOpts...)
}

// loadSolutionWithBundle loads a solution and extracts its bundle if present.
func loadSolutionWithBundle(ctx context.Context, getter get.Interface, path string, stdin io.Reader) (*solution.Solution, string, error) {
	lgr := logger.FromContext(ctx)

	// Handle stdin
	if path == "-" {
		if stdin == nil {
			return nil, "", fmt.Errorf("stdin requested but no reader provided")
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read from stdin: %w", err)
		}

		var sol solution.Solution
		if err := sol.LoadFromBytes(data); err != nil {
			return nil, "", fmt.Errorf("failed to parse solution from stdin: %w", err)
		}
		return &sol, "", nil
	}

	// Use GetWithBundle for catalog solutions to extract bundle
	sol, bundleData, err := getter.GetWithBundle(ctx, path)
	if err != nil {
		return nil, "", err
	}

	// If there's bundle data, extract it to a temp directory
	if len(bundleData) > 0 {
		if lgr != nil {
			lgr.V(1).Info("extracting solution bundle", "size", len(bundleData))
		}
		tmpDir, err := os.MkdirTemp("", paths.AppName()+"-bundle-*")
		if err != nil {
			return nil, "", fmt.Errorf("failed to create temp directory for bundle: %w", err)
		}

		// Write the solution YAML to the temp dir so relative paths work
		solYAML, err := sol.ToYAML()
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", fmt.Errorf("failed to serialize solution: %w", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "solution.yaml"), solYAML, 0o600); err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", fmt.Errorf("failed to write solution to temp dir: %w", err)
		}

		// Extract bundle tar
		manifest, err := bundler.ExtractBundleTar(bundleData, tmpDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", fmt.Errorf("failed to extract bundle: %w", err)
		}

		if lgr != nil {
			lgr.V(1).Info("extracted bundle",
				"files", len(manifest.Files),
				"dir", tmpDir)
		}

		return sol, tmpDir, nil
	}

	return sol, "", nil
}

// writeMetrics writes provider execution metrics to the given writer.
func writeMetrics(out io.Writer) {
	allMetrics := provider.GlobalMetrics.GetAllMetrics()
	if len(allMetrics) == 0 {
		return
	}

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Provider Execution Metrics:")
	fmt.Fprintln(out, strings.Repeat("-", 80))
	fmt.Fprintf(out, "%-25s %8s %8s %8s %12s %12s\n",
		"Provider", "Total", "Success", "Failure", "Avg Duration", "Success %")
	fmt.Fprintln(out, strings.Repeat("-", 80))

	// Sort provider names for consistent output
	names := make([]string, 0, len(allMetrics))
	for name := range allMetrics {
		names = append(names, name)
	}
	slices.Sort(names)

	for _, name := range names {
		m := allMetrics[name]
		avgDuration := m.AverageDuration()
		successRate := m.SuccessRate()
		fmt.Fprintf(out, "%-25s %8d %8d %8d %12s %11.1f%%\n",
			name,
			m.ExecutionCount,
			m.SuccessCount,
			m.FailureCount,
			avgDuration.Round(time.Millisecond),
			successRate)
	}
	fmt.Fprintln(out, strings.Repeat("-", 80))
}

// autoResolveOfficialProviders scans the solution's resolvers for provider
// references that are not already registered. For each missing provider
// found in the official registry, a synthetic PluginDependency is created
// and fetched via the plugin fetcher.
//
// When strict is true, this function returns an error listing the missing
// official providers instead of auto-fetching them.
//
// Returns the plugin clients that were created (caller must add to cleanup)
// or nil when no providers were auto-resolved.
func autoResolveOfficialProviders(
	ctx context.Context,
	sol *solution.Solution,
	reg *provider.Registry,
	cfg *prepareConfig,
) ([]*plugin.Client, error) {
	if cfg.officialProviders == nil || cfg.officialProviders.Len() == 0 {
		return nil, nil
	}

	lgr := logger.FromContext(ctx)

	// Collect provider names referenced in the solution's resolvers.
	missing := missingOfficialProviders(sol, reg, cfg.officialProviders)
	if len(missing) == 0 {
		return nil, nil
	}

	// In strict mode, refuse to auto-resolve and return an actionable error.
	if cfg.strict {
		names := make([]string, len(missing))
		for i, p := range missing {
			names[i] = p.Name
		}
		return nil, fmt.Errorf(
			"strict mode: providers %v are official but not declared in bundle.plugins; "+
				"add them explicitly or disable strict mode",
			names,
		)
	}

	if cfg.pluginFetcher == nil {
		if lgr != nil {
			lgr.V(1).Info("official providers need auto-resolution but no plugin fetcher available")
		}
		return nil, nil
	}

	// Build synthetic plugin dependencies for each missing official provider.
	deps := make([]solution.PluginDependency, len(missing))
	for i, p := range missing {
		deps[i] = p.ToPluginDependency()
	}

	if lgr != nil {
		names := make([]string, len(missing))
		for i, p := range missing {
			names[i] = p.Name
		}
		lgr.V(0).Info("auto-resolving official providers", "providers", names)
	}

	fetchResults, fetchErr := cfg.pluginFetcher.FetchPlugins(ctx, deps, cfg.lockPlugins)
	if fetchErr != nil {
		return nil, fmt.Errorf("auto-fetching official providers: %w", fetchErr)
	}

	clients, regErr := plugin.RegisterFetchedPlugins(ctx, reg, fetchResults, cfg.pluginCfg, cfg.clientOpts...)
	if regErr != nil {
		return nil, fmt.Errorf("registering auto-resolved official providers: %w", regErr)
	}

	return clients, nil
}

// missingOfficialProviders returns the subset of official providers that are
// referenced by solution resolvers or workflow actions but not present in the
// provider registry.
func missingOfficialProviders(
	sol *solution.Solution,
	reg *provider.Registry,
	officialReg *official.Registry,
) []official.Provider {
	var missing []official.Provider
	for _, name := range sol.Spec.ReferencedProviderNames() {
		if reg.Has(name) {
			continue
		}
		if p, ok := officialReg.Get(name); ok {
			missing = append(missing, p)
		}
	}
	return missing
}

// BuildPluginFetcher creates a plugin.Fetcher from the context's config and
// auth registry. Returns an error when the catalog chain cannot be built.
// Callers should treat errors as non-fatal: plugin auto-fetch is simply disabled.
func BuildPluginFetcher(ctx context.Context) (*plugin.Fetcher, error) {
	lgr := logger.FromContext(ctx)
	var fetcherLogger logr.Logger
	if lgr != nil {
		fetcherLogger = *lgr
	} else {
		fetcherLogger = logr.Discard()
	}
	appCfg := config.FromContext(ctx)
	authReg := auth.RegistryFromContext(ctx)
	catalogChain, err := catalog.BuildCatalogChain(appCfg, authReg, fetcherLogger)
	if err != nil {
		fetcherLogger.V(1).Info("catalog chain not available, plugin auto-fetch disabled", "error", err)
		return nil, fmt.Errorf("building catalog chain: %w", err)
	}
	return plugin.NewFetcher(plugin.FetcherConfig{
		Catalog:    catalogChain,
		BinaryName: settings.BinaryNameFromContext(ctx),
		Logger:     fetcherLogger,
	}), nil
}

// ResolveOfficialProviders fetches any official providers referenced by the
// solution that are missing from the registry. It reads the official provider
// registry from context and builds a plugin fetcher on demand.
// Returns the plugin clients created (caller must defer Kill on each), or nil
// when no providers needed resolution or fetching failed non-fatally.
func ResolveOfficialProviders(ctx context.Context, sol *solution.Solution, reg *provider.Registry) ([]*plugin.Client, error) {
	officialReg := official.RegistryFromContext(ctx)
	if officialReg == nil || officialReg.Len() == 0 {
		return nil, nil
	}
	missing := missingOfficialProviders(sol, reg, officialReg)
	if len(missing) == 0 {
		return nil, nil
	}
	lgr := logger.FromContext(ctx)
	fetcher, err := BuildPluginFetcher(ctx)
	if err != nil {
		if lgr != nil {
			lgr.V(1).Info("plugin fetcher not available for official provider auto-resolution", "error", err)
		}
		return nil, nil
	}
	deps := make([]solution.PluginDependency, len(missing))
	for i, p := range missing {
		deps[i] = p.ToPluginDependency()
	}
	if lgr != nil {
		names := make([]string, len(missing))
		for i, p := range missing {
			names[i] = p.Name
		}
		lgr.V(0).Info("auto-resolving official providers", "providers", names)
	}
	results, err := fetcher.FetchPlugins(ctx, deps, nil)
	if err != nil {
		return nil, fmt.Errorf("auto-fetching official providers: %w", err)
	}
	clients, err := plugin.RegisterFetchedPlugins(ctx, reg, results, nil)
	if err != nil {
		return nil, fmt.Errorf("registering auto-resolved official providers: %w", err)
	}
	return clients, nil
}

// injectHostMetadataSettings populates cfg.Settings["metadata"] with host runtime
// information so that the metadata plugin (and any other plugin that cares) can
// return version, entrypoint, args, and solution metadata to callers.
//
// NOTE: os.Args is included in the serialized settings. This means command-line
// arguments (potentially including sensitive values) are visible to all plugins
// over the local gRPC socket. This is acceptable because plugins are first-party
// trusted binaries, but users should avoid passing secrets via CLI flags when
// running untrusted plugin binaries.
func injectHostMetadataSettings(cfg *plugin.ProviderConfig, sol *solution.Solution) {
	if cfg == nil {
		return
	}

	// Determine entrypoint from binary name heuristic.
	entrypoint := "cli"
	if cfg.BinaryName == "" {
		entrypoint = "unknown"
	}

	type solutionMeta struct {
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		DisplayName string   `json:"displayName"`
		Description string   `json:"description"`
		Category    string   `json:"category"`
		Tags        []string `json:"tags"`
	}

	solMeta := solutionMeta{}
	if sol != nil && sol.Metadata.Name != "" {
		solMeta.Name = sol.Metadata.Name
		solMeta.DisplayName = sol.Metadata.DisplayName
		solMeta.Description = sol.Metadata.Description
		solMeta.Category = sol.Metadata.Category
		solMeta.Tags = sol.Metadata.Tags
		if sol.Metadata.Version != nil {
			solMeta.Version = sol.Metadata.Version.String()
		}
	}

	type hostMetadata struct {
		BuildVersion string       `json:"buildVersion"`
		Commit       string       `json:"commit"`
		BuildTime    string       `json:"buildTime"`
		Entrypoint   string       `json:"entrypoint"`
		Command      string       `json:"command"`
		Args         []string     `json:"args"`
		Solution     solutionMeta `json:"solution"`
	}

	meta := hostMetadata{
		BuildVersion: settings.VersionInformation.BuildVersion,
		Commit:       settings.VersionInformation.Commit,
		BuildTime:    settings.VersionInformation.BuildTime,
		Entrypoint:   entrypoint,
		Command:      strings.Join(os.Args, " "),
		Args:         os.Args,
		Solution:     solMeta,
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		return
	}

	if cfg.Settings == nil {
		cfg.Settings = make(map[string]json.RawMessage)
	}
	cfg.Settings["metadata"] = json.RawMessage(raw)
}

// injectHTTPClientSettings propagates httpClient configuration (e.g.
// allowPrivateIPs) from the app config to ProviderConfig.Settings["httpClient"]
// so external plugins can apply the same network policies as the host.
func injectHTTPClientSettings(ctx context.Context, cfg *plugin.ProviderConfig) {
	if cfg == nil {
		return
	}

	appCfg := config.FromContext(ctx)
	if appCfg == nil {
		return
	}

	// Only inject if there's something to communicate.
	if appCfg.HTTPClient.AllowPrivateIPs == nil {
		return
	}

	type httpClientSettings struct {
		AllowPrivateIPs bool `json:"allowPrivateIPs"`
	}

	raw, err := json.Marshal(httpClientSettings{
		AllowPrivateIPs: *appCfg.HTTPClient.AllowPrivateIPs,
	})
	if err != nil {
		return
	}

	if cfg.Settings == nil {
		cfg.Settings = make(map[string]json.RawMessage)
	}
	cfg.Settings["httpClient"] = json.RawMessage(raw)
}
