// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package prepare provides a standalone function for loading and preparing
// a solution for execution. It decouples solution preparation from CLI-specific
// types, making it reusable by both CLI commands and the MCP server.
package prepare

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

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
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
)

// Option configures the PrepareSolution function.
type Option func(*prepareConfig)

type prepareConfig struct {
	getter        get.Interface
	registry      *provider.Registry
	authRegistry  *auth.Registry
	stdin         io.Reader
	showMetrics   bool
	metricsOut    io.Writer
	pluginFetcher *plugin.Fetcher
	lockPlugins   []bundler.LockPlugin
	noCache       bool
	pluginCfg     *plugin.ProviderConfig
	clientOpts    []plugin.ClientOption
	discoveryMode settings.DiscoveryMode
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

	// Register the solution provider
	if !reg.Has(solutionprovider.ProviderName) {
		solProvider := solutionprovider.New(
			solutionprovider.WithLoader(getter),
			solutionprovider.WithRegistry(reg),
		)
		if err := reg.Register(solProvider); err != nil {
			cleanup()
			return nil, fmt.Errorf("registering solution provider: %w", err)
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
