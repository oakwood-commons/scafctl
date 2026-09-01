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
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/cache"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/catalogindex"
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
	getter               get.Interface
	registry             *provider.Registry
	authRegistry         *auth.Registry
	stdin                io.Reader
	showMetrics          bool
	metricsOut           io.Writer
	pluginFetcher        *plugin.Fetcher
	lockPlugins          []bundler.LockPlugin
	noCache              bool
	pluginCfg            *plugin.ProviderConfig
	clientOpts           []plugin.ClientOption
	discoveryMode        settings.DiscoveryMode
	officialProviders    *official.Registry
	officialAuthHandlers *authofficial.Registry
	strict               bool
	pluginPool           *plugin.Pool
	lockFile             *bundler.LockFile
	lockMode             LockMode
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

// WithOfficialAuthHandlers provides an official auth handler registry for
// auto-resolving missing auth handlers at runtime. When set (and strict is
// false), auth handlers referenced by the identity provider that are not in
// the auth registry are checked against the official list and auto-fetched
// via the plugin fetcher.
func WithOfficialAuthHandlers(r *authofficial.Registry) Option {
	return func(c *prepareConfig) {
		c.officialAuthHandlers = r
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

// WithLockFile provides the parsed lock file for reproducible, version-pinned
// plugin resolution in the versioned registry pipeline. BuildProviderDependency
// uses this to look up pinned versions and digests by plugin name.
func WithLockFile(lf *bundler.LockFile) Option {
	return func(c *prepareConfig) {
		c.lockFile = lf
	}
}

// WithLockMode sets the lock mode that controls how BuildProviderDependency
// handles version constraints. See LockMode constants for available modes.
func WithLockMode(mode LockMode) Option {
	return func(c *prepareConfig) {
		c.lockMode = mode
	}
}

// WithPluginPool delegates provider plugin lifecycle to a shared, long-lived
// plugin pool instead of fetching, registering, and killing plugin processes
// per call. This is the correct mode for long-lived servers (MCP, HTTP API):
// the pool registers provider wrappers into the shared registry exactly once
// and keeps the processes alive across requests, while each prepared solution
// merely acquires a reference for its lifetime and releases (never kills) it on
// cleanup. This prevents registry poisoning where a per-request Kill would leave
// dead-backed provider wrappers in a registry shared by future requests.
//
// The pool MUST have been constructed with the same provider registry passed
// via WithRegistry. When nil, prepare falls back to the per-call
// fetch/register/kill behavior suitable for one-shot CLI invocations.
func WithPluginPool(p *plugin.Pool) Option {
	return func(c *prepareConfig) {
		c.pluginPool = p
	}
}

type providerLookup interface {
	DescriptorLookup() provider.DescriptorLookup
	Get(name string) (provider.Provider, bool)
	Has(name string) bool
}

// Result holds the output of PrepareSolution.
type Result struct {
	// Solution is the loaded and prepared solution.
	Solution *solution.Solution `json:"solution" yaml:"solution" doc:"The loaded solution"`
	// Registry is the provider registry with all providers registered,
	// including the solution provider. In per-call mode this is a
	// ExecutionRegistry backed by a CompositeRegistry; in pool mode or when no
	// plugins are involved it may be the flat builtin *provider.Registry.
	// Callers that only need lookup (Get/Has/DescriptorLookup) should use
	// this field. Callers that need mutation (Register) should use the flat
	// registry from ProviderCtx instead.
	Registry providerLookup `json:"-" yaml:"-"`
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
	// ProviderCtx enriches a context with per-execution provider settings and
	// solution provider dependencies (loader, registry, plugin deps, client
	// tracker). Callers must apply this to the execution context before running
	// resolvers or actions so that per-solution provider settings reach plugins
	// on every execution and the solution provider can resolve sub-solutions.
	// Always non-nil.
	ProviderCtx func(ctx context.Context) context.Context `json:"-" yaml:"-"`
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

	// Load the solution (with bundle and lock if available)
	sol, bundleDir, lockData, err := loadSolutionWithBundleAndLock(ctx, getter, path, cfg.stdin)
	if err != nil {
		return nil, err
	}

	// Parse the OCI lock layer (when present) and apply the appropriate lock
	// mode default based on the solution source.
	isCatalogOrRemote := path != "" && path != "-" && get.IsCatalogReference(path)
	if err := applyOCILockLayer(cfg, lockData); err != nil {
		return nil, err
	}
	applyDefaultLockMode(cfg, path, isCatalogOrRemote, lgr)

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

	// Provider plugin lifecycle. In pool mode (long-lived servers), the shared
	// pool owns provider processes: it registers wrappers into the shared
	// registry once and keeps them alive across requests. We merely acquire a
	// reference for this prepared solution and release (never kill) on cleanup,
	// which prevents registry poisoning. In per-call mode (one-shot CLI), we
	// classify, fetch, and register the plugins declared in bundle.plugins into
	// a versioned composite registry, and build a ExecutionRegistry that routes
	// lookups to the correct catalog+version. Referenced providers that are
	// neither built-in nor declared in bundle.plugins are rejected (no implicit
	// official-provider auto-resolution on this path).
	//
	// resultRegistry is the registry exposed via Result.Registry for downstream
	// lookup (Get/Has/DescriptorLookup). In pool mode it is the flat builtin
	// registry; in per-call mode it is the ExecutionRegistry.
	var resultRegistry providerLookup = reg
	if cfg.pluginPool != nil {
		deps := providerPoolDeps(sol, cfg)
		release, ensErr := cfg.pluginPool.EnsureAndAcquire(ctx, deps)
		if ensErr != nil {
			cleanup()
			return nil, fmt.Errorf("ensuring provider plugins via pool: %w", ensErr)
		}
		origCleanup := cleanup
		cleanup = func() {
			release()
			origCleanup()
		}

		// Auth handler plugins are not pool-managed. Register any declared in
		// the bundle into the shared auth registry, but do NOT kill them per
		// request -- the long-lived server owns their lifetime, and killing them
		// would poison the shared auth registry for subsequent requests.
		if cfg.authRegistry != nil && cfg.pluginFetcher != nil && len(sol.Bundle.Plugins) > 0 {
			if _, ahErr := registerBundleAuthHandlers(ctx, sol, cfg); ahErr != nil {
				cleanup()
				return nil, ahErr
			}
		}
	} else {
		composite := provider.NewCompositeRegistryFromBase(reg)
		// Per-call mode: classify, fetch, and register the plugins declared in
		// bundle.plugins into a versioned composite registry, and build a
		// ExecutionRegistry that routes lookups to the correct catalog+version.
		// Undeclared external providers are rejected by validateExternalProviders.
		execReg, vCleanup, vErr := buildExecutionRegistryCLI(ctx, sol, composite, catalogindex.FromContext(ctx), cfg)
		if vErr != nil {
			cleanup()
			return nil, vErr
		}

		// Chain the execution-registry cleanup (kills the versioned clients
		// started during registration) into the cleanup chain.
		origCleanup := cleanup
		cleanup = func() {
			vCleanup()
			origCleanup()
		}

		// Register auth handler plugins declared in the bundle. The
		// buildExecutionRegistryCLI path only fetches provider-kind plugins, so
		// auth handlers must be fetched separately.
		if bundleHasAuthHandlers(sol) {
			if cfg.authRegistry == nil {
				cleanup()
				return nil, fmt.Errorf("bundle declares auth handler plugins but no auth registry is configured")
			}
			if cfg.pluginFetcher == nil {
				cleanup()
				return nil, fmt.Errorf("bundle declares auth handler plugins but no plugin fetcher is available")
			}
			authClients, authRegErr := registerBundleAuthHandlers(ctx, sol, cfg)
			if authRegErr != nil {
				cleanup()
				return nil, authRegErr
			}
			if len(authClients) > 0 {
				origCleanup := cleanup
				cleanup = func() {
					for _, c := range authClients {
						c.Kill()
					}
					origCleanup()
				}
			}
		}

		resultRegistry = execReg
	}

	// Auto-resolve official auth handlers that are referenced in the solution
	// (via identity provider) but not already in the auth registry.
	officialAuthClients, officialAuthErr := autoResolveOfficialAuthHandlers(ctx, sol, cfg.authRegistry, cfg)
	if officialAuthErr != nil {
		cleanup()
		return nil, officialAuthErr
	}
	// In pool mode, auth handler clients are owned by the long-lived server and
	// must not be killed per request (doing so would poison the shared auth
	// registry). In per-call mode, they are killed on cleanup.
	if len(officialAuthClients) > 0 && cfg.pluginPool == nil {
		origCleanup := cleanup
		cleanup = func() {
			for _, c := range officialAuthClients {
				c.Kill()
			}
			origCleanup()
		}
	}

	// Build a context enrichment function that injects per-execution provider
	// settings and (when present) solution provider dependencies via context.
	// This avoids mutating the shared singleton in DefaultRegistry and keeps
	// per-execution state isolated.
	//
	// The per-solution settings blob is attached unconditionally -- it must not
	// be gated on the solution provider being registered, since it carries the
	// per-solution metadata that pooled plugins (e.g. the metadata provider)
	// rely on for every execution.
	execSettings := buildPerSolutionSettings(hostEntrypoint(cfg), sol)

	hasSolutionProvider := reg.Has(solutionprovider.ProviderName)
	var tracker *solutionprovider.ChildClientTracker
	if hasSolutionProvider {
		tracker = solutionprovider.NewChildClientTracker()
	}

	providerCtx := func(ctx context.Context) context.Context {
		if len(execSettings) > 0 {
			ctx = provider.WithExecutionSettings(ctx, execSettings)
		}
		if hasSolutionProvider {
			ctx = solutionprovider.WithLoaderCtx(ctx, getter)
			ctx = solutionprovider.WithProviderRegistry(ctx, reg)
			ctx = solutionprovider.WithChildClientTracker(ctx, tracker)
			if cfg.officialProviders != nil {
				ctx = solutionprovider.WithOfficialProvidersCtx(ctx, cfg.officialProviders)
			}
			if cfg.pluginFetcher != nil {
				ctx = solutionprovider.WithPluginFetcherCtx(ctx, cfg.pluginFetcher)
			}
			if cfg.pluginCfg != nil {
				ctx = solutionprovider.WithPluginConfigCtx(ctx, cfg.pluginCfg)
			}
			if len(cfg.clientOpts) > 0 {
				ctx = solutionprovider.WithClientOptionsCtx(ctx, cfg.clientOpts)
			}
		}
		return ctx
	}

	if hasSolutionProvider {
		// Add tracker cleanup to kill child plugin clients for this run only.
		origCleanup := cleanup
		cleanup = func() {
			tracker.Close()
			origCleanup()
		}
	}

	return &Result{
		Solution:       sol,
		Registry:       resultRegistry,
		SolutionDir:    solutionDir,
		Cleanup:        cleanup,
		DiscoveredFrom: discoveredFrom,
		ProviderCtx:    providerCtx,
	}, nil
}

// NewDefaultGetter creates a default solution getter with catalog and remote resolution support.
// When noCache is true, the artifact cache is disabled so the catalog is always queried directly.
func NewDefaultGetter(ctx context.Context, noCache bool) get.Interface {
	lgr := logger.FromContext(ctx)

	var getterOpts []get.Option
	if lgr != nil {
		getterOpts = append(getterOpts, get.WithLogger(*lgr))

		// Create shared artifact cache for both catalog and remote resolvers.
		var artifactCache catalog.ArtifactCacher
		if !noCache {
			artifactCache = cache.NewArtifactCache(paths.ArtifactCacheDir(), settings.DefaultArtifactCacheTTL)
		}

		localCatalog, err := catalog.NewLocalCatalog(*lgr)
		if err == nil {
			// Build SolutionResolverOptions with optional artifact cache
			resolverOpts := []catalog.SolutionResolverOption{
				catalog.WithResolverNoCache(noCache),
				catalog.WithResolverRemoteCatalogs(catalog.RemoteCatalogsFromContext(ctx, *lgr)),
			}
			if artifactCache != nil {
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
				var handlerName string
				if cfg != nil {
					for _, cat := range cfg.Catalogs {
						if cat.URL == "" || cat.AuthProvider == "" {
							continue
						}
						host, _ := catalog.ParseCatalogURL(cat.URL)
						if host == registry {
							handlerName = cat.AuthProvider
							break
						}
					}
				}

				// Fall back to inference from registry host.
				if handlerName == "" {
					var customHandlers []config.CustomOAuth2Config
					if cfg != nil {
						customHandlers = cfg.Auth.CustomOAuth2
					}
					handlerName = catalog.InferAuthHandler(registry, customHandlers)
				}

				if handlerName == "" {
					return nil
				}
				handler, err := auth.GetHandler(ctx, handlerName)
				if err != nil {
					lgr.V(1).Info("failed to resolve auth handler for registry", "handler", handlerName, "registry", registry, "error", err)
					return nil
				}
				return handler
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
			Logger:        *lgr,
			ArtifactCache: artifactCache,
			NoCache:       noCache,
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

// loadSolutionWithBundleAndLock loads a solution, its bundle, and its lock
// layer in a single call. Returns the solution, the extracted bundle directory
// (empty when no bundle), the raw lock layer bytes (nil when absent), and any
// error. Delegates bundle extraction to loadSolutionWithBundle internally.
func loadSolutionWithBundleAndLock(ctx context.Context, getter get.Interface, path string, stdin io.Reader) (*solution.Solution, string, []byte, error) {
	lgr := logger.FromContext(ctx)

	// stdin has no OCI layers — delegate to the plain bundle loader.
	if path == "-" {
		sol, bundleDir, err := loadSolutionWithBundle(ctx, getter, path, stdin)
		return sol, bundleDir, nil, err
	}

	// Use GetWithLayers to fetch solution, bundle, and lock in one call.
	sol, layers, err := getter.GetWithLayers(ctx, path, catalog.MediaTypeSolutionBundle, catalog.MediaTypeSolutionLock)
	if err != nil {
		return nil, "", nil, err
	}
	bundleData := layers[catalog.MediaTypeSolutionBundle]
	lockData := layers[catalog.MediaTypeSolutionLock]

	// If there's bundle data, extract it to a temp directory
	if len(bundleData) > 0 {
		if lgr != nil {
			lgr.V(1).Info("extracting solution bundle", "size", len(bundleData))
		}
		tmpDir, err := os.MkdirTemp("", paths.AppName()+"-bundle-*")
		if err != nil {
			return nil, "", nil, fmt.Errorf("failed to create temp directory for bundle: %w", err)
		}

		solYAML, err := sol.ToYAML()
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", nil, fmt.Errorf("failed to serialize solution: %w", err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "solution.yaml"), solYAML, 0o600); err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", nil, fmt.Errorf("failed to write solution to temp dir: %w", err)
		}

		manifest, err := bundler.ExtractBundleTar(bundleData, tmpDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, "", nil, fmt.Errorf("failed to extract bundle: %w", err)
		}

		if lgr != nil {
			lgr.V(1).Info("extracted bundle",
				"files", len(manifest.Files),
				"dir", tmpDir)
		}

		return sol, tmpDir, lockData, nil
	}

	return sol, "", lockData, nil
}

// applyOCILockLayer parses raw OCI lock layer bytes into cfg when the caller
// did not supply a lock file explicitly. No-op when lockData is nil or cfg
// already has a caller-supplied lock (WithLockFile takes precedence). A
// present-but-invalid lock layer -- unparseable JSON or an unsupported format
// version -- is returned as an error rather than silently ignored: dropping it
// would downgrade a packaged, locked artifact to apparently-unlocked and let
// source defaulting select best-effort and run its plugins unpinned.
func applyOCILockLayer(cfg *prepareConfig, lockData []byte) error {
	if cfg.lockFile != nil || len(lockData) == 0 {
		return nil
	}
	var lf bundler.LockFile
	if err := json.Unmarshal(lockData, &lf); err != nil {
		return fmt.Errorf("parsing embedded lock layer: %w", err)
	}
	if lf.Version != bundler.LockFileVersion {
		return fmt.Errorf("embedded lock layer has unsupported version %d (expected %d)", lf.Version, bundler.LockFileVersion)
	}
	cfg.lockFile = &lf
	cfg.lockPlugins = lf.Plugins
	return nil
}

// applyDefaultLockMode sets cfg.lockMode when the caller did not supply one
// via WithLockMode. The default depends on the solution source:
//
//   - Local file / stdin: BestEffort -- dev-iteration friendly, lock hints
//     are advisory only.
//   - Catalog/remote with lock layer: Strict -- exact pins from the embedded
//     lock; fetches by digest so catalog yanking is not a concern.
//   - Catalog/remote without lock layer: BestEffort + warning -- old artifact
//     predating the lock feature; plugins resolve unpinned.
func applyDefaultLockMode(cfg *prepareConfig, path string, isCatalogOrRemote bool, lgr *logr.Logger) {
	if cfg.lockMode != 0 {
		return // caller explicitly set a mode via WithLockMode
	}
	switch {
	case !isCatalogOrRemote:
		cfg.lockMode = LockModeBestEffort
	case cfg.lockFile != nil:
		cfg.lockMode = LockModeStrict
	default:
		cfg.lockMode = LockModeBestEffort
		if lgr != nil {
			lgr.V(0).Info("solution has no embedded lock layer; external plugin versions are unpinned; re-package to embed a lock if it uses external providers", "path", path)
		}
	}
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

// providerPoolDeps computes the provider plugin dependencies to acquire from
// the shared pool for a solution: all provider-kind bundle plugins plus every
// referenced official provider. Official providers are included whether or not
// they are already registered so that pool-managed entries get a reference on
// each request (preventing idle eviction of an in-use provider); the pool
// deduplicates already-loaded entries internally. Note that a provider already
// present in the shared registry but not tracked by the pool (e.g. a builtin or
// one registered outside the pool) returns early from ensureOne without taking
// a reference. Builtin providers referenced by the solution are intentionally
// omitted -- they live outside the pool and are never evicted.
func providerPoolDeps(sol *solution.Solution, cfg *prepareConfig) []solution.PluginDependency {
	var deps []solution.PluginDependency
	seen := make(map[string]bool)
	for _, p := range sol.Bundle.Plugins {
		if p.Kind == solution.PluginKindProvider {
			deps = append(deps, p)
			seen[p.LocalName()] = true
		}
	}
	if cfg.officialProviders != nil {
		for _, name := range sol.Spec.ReferencedProviderNames() {
			if seen[name] {
				continue
			}
			if op, ok := cfg.officialProviders.Get(name); ok {
				deps = append(deps, op.ToPluginDependency())
				seen[name] = true
			}
		}
	}
	return deps
}

// bundleHasAuthHandlers reports whether the solution's bundle declares at least
// one auth-handler-kind plugin dependency.
func bundleHasAuthHandlers(sol *solution.Solution) bool {
	for _, p := range sol.Bundle.Plugins {
		if p.Kind == solution.PluginKindAuthHandler {
			return true
		}
	}
	return false
}

// registerBundleAuthHandlers fetches and registers any auth-handler-kind
// plugins declared in the solution bundle into the shared auth registry. It is
// used in pool mode, where the returned clients are owned by the long-lived
// server and are intentionally NOT killed per request. Returns nil when the
// bundle declares no auth handler plugins.
func registerBundleAuthHandlers(ctx context.Context, sol *solution.Solution, cfg *prepareConfig) ([]*plugin.AuthHandlerClient, error) {
	var authDeps []solution.PluginDependency
	for _, p := range sol.Bundle.Plugins {
		if p.Kind == solution.PluginKindAuthHandler {
			authDeps = append(authDeps, p)
		}
	}
	if len(authDeps) == 0 {
		return nil, nil
	}
	fetchResults, err := cfg.pluginFetcher.FetchPlugins(ctx, authDeps, cfg.lockPlugins)
	if err != nil {
		return nil, fmt.Errorf("auto-fetching auth handler plugins: %w", err)
	}
	clients, err := plugin.RegisterFetchedAuthHandlerPlugins(ctx, cfg.authRegistry, fetchResults, cfg.pluginCfg, cfg.clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering fetched auth handler plugins: %w", err)
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

// autoResolveOfficialAuthHandlers scans the solution's resolvers for auth
// handler references (via the identity provider) that are not already
// registered. For each missing handler found in the official auth handler
// registry, a synthetic PluginDependency is created and fetched.
//
// When strict is true, this function returns an error listing the missing
// official auth handlers instead of auto-fetching them.
//
// Returns the auth handler plugin clients that were created (caller must add
// to cleanup) or nil when no auth handlers were auto-resolved.
func autoResolveOfficialAuthHandlers(
	ctx context.Context,
	sol *solution.Solution,
	authReg *auth.Registry,
	cfg *prepareConfig,
) ([]*plugin.AuthHandlerClient, error) {
	// Default to the registry from context if not explicitly provided via option.
	officialReg := cfg.officialAuthHandlers
	if officialReg == nil {
		officialReg = authofficial.RegistryFromContext(ctx)
	}
	if officialReg == nil || officialReg.Len() == 0 {
		return nil, nil
	}

	lgr := logger.FromContext(ctx)

	// Collect auth handler names referenced in the solution's resolvers.
	missing := missingOfficialAuthHandlers(sol, authReg, officialReg)
	if len(missing) == 0 {
		return nil, nil
	}

	// In strict mode, refuse to auto-resolve and return an actionable error.
	if cfg.strict {
		entries := make([]string, len(missing))
		for i, h := range missing {
			if h.CatalogRef != h.Name {
				entries[i] = fmt.Sprintf("%s (catalogRef: %s)", h.Name, h.CatalogRef)
			} else {
				entries[i] = h.Name
			}
		}
		return nil, fmt.Errorf(
			"strict mode: auth handlers %v are official but not declared in bundle.plugins; "+
				"add their catalog references explicitly or disable strict mode",
			entries,
		)
	}

	if cfg.pluginFetcher == nil {
		if lgr != nil {
			lgr.V(1).Info("official auth handlers need auto-resolution but no plugin fetcher available")
		}
		return nil, nil
	}

	// Build synthetic plugin dependencies for each missing official auth handler.
	deps := make([]solution.PluginDependency, len(missing))
	for i, h := range missing {
		deps[i] = h.ToPluginDependency()
	}

	if lgr != nil {
		names := make([]string, len(missing))
		for i, h := range missing {
			names[i] = h.Name
		}
		lgr.V(0).Info("auto-resolving official auth handlers", "handlers", names)
	}

	fetchResults, fetchErr := cfg.pluginFetcher.FetchPlugins(ctx, deps, cfg.lockPlugins)
	if fetchErr != nil {
		return nil, fmt.Errorf("auto-fetching official auth handlers: %w", fetchErr)
	}

	if authReg == nil {
		return nil, nil
	}

	authClients, regErr := plugin.RegisterFetchedAuthHandlerPlugins(ctx, authReg, fetchResults, cfg.pluginCfg, cfg.clientOpts...)
	if regErr != nil {
		return nil, fmt.Errorf("registering auto-resolved official auth handlers: %w", regErr)
	}

	return authClients, nil
}

// missingOfficialAuthHandlers returns the subset of official auth handlers
// that are referenced by solution resolvers (via identity provider) but not
// present in the auth registry.
func missingOfficialAuthHandlers(
	sol *solution.Solution,
	authReg *auth.Registry,
	officialReg *authofficial.Registry,
) []authofficial.AuthHandler {
	var missing []authofficial.AuthHandler
	for _, name := range sol.Spec.ReferencedAuthHandlerNames() {
		if authReg != nil && authReg.Has(name) {
			continue
		}
		if h, ok := officialReg.Get(name); ok {
			missing = append(missing, h)
		}
	}
	return missing
}

// BuildPluginFetcher creates a plugin.Fetcher from the context's config and
// auth registry. Returns an error when the catalog chain cannot be built.
// Callers should treat errors as non-fatal: plugin auto-fetch is simply disabled.
func BuildPluginFetcher(ctx context.Context) (*plugin.Fetcher, error) {
	return BuildPluginFetcherWithConfig(ctx, PluginFetcherOverrides{})
}

// PluginFetcherOverrides holds the subset of FetcherConfig fields that
// BuildPluginFetcherWithConfig can override. Other fields (Catalog,
// BinaryName, Logger, Cache) are always derived from context.
type PluginFetcherOverrides struct {
	// AllowedCatalogs restricts which catalog names plugins may be fetched from
	// (Fetcher-level post-resolve check, belt-and-suspenders).
	AllowedCatalogs []string
	// ChainAllowedCatalogs restricts which catalogs are included in the chain
	// during construction. Catalogs not in this list are excluded entirely.
	ChainAllowedCatalogs []string
	// PerCatalogArtifacts restricts which artifact names each catalog may serve.
	// Keys are catalog names, values describe the policy for that catalog.
	// Each matching catalog is wrapped with an AllowlistCatalog decorator.
	PerCatalogArtifacts map[string]catalog.PluginPolicy
	// Cache overrides the local plugin cache. When nil, a default unbounded
	// cache is created inside the Fetcher.
	Cache *plugin.Cache
	// Platform overrides the target platform. If empty, auto-detected.
	Platform string
	// NoCache bypasses the local cache when true.
	NoCache bool
	// SignaturePolicy overrides the signature verification policy derived
	// from config. When non-nil, takes precedence over config values.
	SignaturePolicy *plugin.SignaturePolicy
}

// BuildPluginFetcherWithConfig creates a plugin.Fetcher from the context's
// config and auth registry, applying optional overrides for policy fields.
// Catalog, BinaryName, Logger, and Cache are always derived from context.
func BuildPluginFetcherWithConfig(ctx context.Context, override PluginFetcherOverrides) (*plugin.Fetcher, error) {
	lgr := logger.FromContext(ctx)
	var fetcherLogger logr.Logger
	if lgr != nil {
		fetcherLogger = *lgr
	} else {
		fetcherLogger = logr.Discard()
	}
	appCfg := config.FromContext(ctx)
	authReg := auth.RegistryFromContext(ctx)

	var chainOpts []catalog.ChainCatalogOption
	if override.ChainAllowedCatalogs != nil {
		chainOpts = append(chainOpts, catalog.WithAllowedCatalogs(override.ChainAllowedCatalogs))
	}
	if override.PerCatalogArtifacts != nil {
		chainOpts = append(chainOpts, catalog.WithPerCatalogArtifacts(override.PerCatalogArtifacts))
	}

	catalogChain, err := catalog.BuildCatalogChain(appCfg, authReg, fetcherLogger, chainOpts...)
	if err != nil {
		fetcherLogger.V(1).Info("catalog chain not available, plugin auto-fetch disabled", "error", err)
		return nil, fmt.Errorf("building catalog chain: %w", err)
	}

	cfg := plugin.FetcherConfig{
		Catalog:             catalogChain,
		Cache:               override.Cache,
		BinaryName:          settings.BinaryNameFromContext(ctx),
		Logger:              fetcherLogger,
		AllowedCatalogs:     override.AllowedCatalogs,
		PerCatalogArtifacts: override.PerCatalogArtifacts,
		SignaturePolicy:     override.SignaturePolicy,
	}
	if cfg.SignaturePolicy == nil {
		cfg.SignaturePolicy = plugin.SignaturePolicyFromContext(ctx)
	}
	if cfg.SignaturePolicy == nil {
		cfg.SignaturePolicy = signaturePolicyFromConfig(appCfg, fetcherLogger)
	}
	if err := cfg.SignaturePolicy.Validate(); err != nil {
		return nil, fmt.Errorf("plugin signature policy: %w", err)
	}
	if override.Platform != "" {
		cfg.Platform = override.Platform
	}
	if override.NoCache {
		cfg.NoCache = true
	}
	return plugin.NewFetcher(cfg), nil
}

// signaturePolicyFromConfig converts the app configuration's plugin signature
// settings into a SignaturePolicy using the shared plugin.SignaturePolicyFromRaw
// helper. Returns nil when the config is nil, mode is "off", empty, or invalid.
func signaturePolicyFromConfig(appCfg *config.Config, lgr logr.Logger) *plugin.SignaturePolicy {
	if appCfg == nil {
		return nil
	}
	sigCfg := appCfg.Plugins.Signatures
	policy, err := plugin.SignaturePolicyFromRaw(sigCfg.Mode, sigCfg.TrustedIssuers, sigCfg.TrustedIdentities)
	if err != nil {
		lgr.Info("invalid plugin signature mode in config, defaulting to off",
			"mode", sigCfg.Mode, "error", err)
		return nil
	}
	return policy
}

// ResolveOfficialProviders fetches any official providers referenced by the
// solution that are missing from the registry. It reads the official provider
// registry from context and builds a plugin fetcher on demand.
// Returns the plugin clients created (caller must defer Kill on each), or nil
// when no providers needed resolution or fetching failed non-fatally.
func ResolveOfficialProviders(ctx context.Context, sol *solution.Solution, reg *provider.Registry, clientOpts ...plugin.ClientOption) ([]*plugin.Client, error) {
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
	clients, err := plugin.RegisterFetchedPlugins(ctx, reg, results, nil, clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("registering auto-resolved official providers: %w", err)
	}
	return clients, nil
}

// ResolveOfficialAuthHandlers fetches all official auth handlers that are not
// already registered in the auth registry. Unlike the solution-level
// autoResolveOfficialAuthHandlers (which only resolves handlers referenced in a
// solution), this function resolves ALL missing official handlers to support
// direct CLI commands like "scafctl auth login github".
//
// It reads the official auth handler registry and auth registry from context,
// builds a plugin fetcher on demand, and uses the provided FetchCooldown to
// skip recently-failed fetches.
//
// Returns the auth handler plugin clients created (caller must defer Kill on
// each), or nil when no handlers needed resolution or fetching was not possible.
func ResolveOfficialAuthHandlers(
	ctx context.Context,
	authReg *auth.Registry,
	cooldown *plugin.FetchCooldown,
	pluginCfg *plugin.ProviderConfig,
	clientOpts ...plugin.ClientOption,
) ([]*plugin.AuthHandlerClient, error) {
	officialReg := authofficial.RegistryFromContext(ctx)
	if officialReg == nil || officialReg.Len() == 0 {
		return nil, nil
	}
	if authReg == nil {
		return nil, nil
	}

	// Collect handlers that are official but not yet registered.
	var missing []authofficial.AuthHandler
	for _, name := range officialReg.Names() {
		if authReg.Has(name) {
			continue
		}
		h, _ := officialReg.Get(name)
		if cooldown != nil && cooldown.OnCooldown(name) {
			continue
		}
		missing = append(missing, h)
	}
	if len(missing) == 0 {
		return nil, nil
	}

	lgr := logger.FromContext(ctx)
	fetcher, err := BuildPluginFetcher(ctx)
	if err != nil {
		if lgr != nil {
			lgr.V(1).Info("plugin fetcher not available for official auth handler auto-resolution", "error", err)
		}
		return nil, nil
	}

	deps := make([]solution.PluginDependency, len(missing))
	for i, h := range missing {
		deps[i] = h.ToPluginDependency()
	}
	if lgr != nil {
		names := make([]string, len(missing))
		for i, h := range missing {
			names[i] = h.Name
		}
		lgr.V(0).Info("auto-resolving official auth handlers", "handlers", names)
	}

	results, fetchErr := fetcher.FetchPlugins(ctx, deps, nil)
	if fetchErr != nil {
		// Record cooldown for all missing handlers on fetch failure.
		if cooldown != nil {
			for _, h := range missing {
				_ = cooldown.RecordFailure(h.Name)
			}
		}
		if lgr != nil {
			lgr.V(0).Info("auto-fetch of official auth handlers failed", "error", fetchErr)
		}
		return nil, nil // Best-effort -- don't fail CLI on fetch errors.
	}

	authClients, regErr := plugin.RegisterFetchedAuthHandlerPlugins(ctx, authReg, results, pluginCfg, clientOpts...)
	if regErr != nil {
		if lgr != nil {
			lgr.V(0).Info("registering auto-resolved official auth handlers failed", "error", regErr)
		}
		return nil, nil
	}

	return authClients, nil
}

// Entrypoint values reported by the metadata provider's "entrypoint" field.
// They describe how the host process was invoked. The host derives the value
// and delivers it to plugins via ProviderConfig.Settings["metadata"].
const (
	// EntrypointCLI is a one-shot command-line invocation (scafctl run ...).
	EntrypointCLI = "cli"
	// EntrypointAPI is a long-lived HTTP API server (scafctl serve).
	EntrypointAPI = "api"
	// EntrypointMCP is a long-lived MCP server (scafctl mcp serve).
	EntrypointMCP = "mcp"
	// EntrypointUnknown is used when the host could not be classified.
	EntrypointUnknown = "unknown"
)

// hostMetadataSettingsKey is the ProviderConfig.Settings key under which host
// runtime metadata is delivered to plugins.
const hostMetadataSettingsKey = "metadata"

// buildHostMetadataSettings serializes host runtime metadata (build info,
// entrypoint, command, args) plus the given solution metadata into the JSON
// blob delivered under Settings["metadata"]. A nil solution yields an empty
// solution object, which is the correct shape for pool-mode hosts that serve
// many solutions (per-solution metadata is delivered per-execution instead).
//
// An empty entrypoint is normalized to EntrypointUnknown so the serialized
// metadata never reports a blank/invalid entrypoint, regardless of caller.
func buildHostMetadataSettings(entrypoint string, sol *solution.Solution) (json.RawMessage, error) {
	if entrypoint == "" {
		entrypoint = EntrypointUnknown
	}

	type solutionMeta struct {
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		DisplayName string   `json:"displayName"`
		Description string   `json:"description"`
		Category    string   `json:"category"`
		Tags        []string `json:"tags"`
		Source      string   `json:"source,omitempty"`
	}

	// Tags is documented as string[], so default to an empty slice; a nil
	// slice marshals to JSON null, which breaks consumers expecting an array
	// (this happens in pool mode and whenever the solution has no tags).
	solMeta := solutionMeta{Tags: []string{}}
	if sol != nil && sol.Metadata.Name != "" {
		solMeta.Name = sol.Metadata.Name
		solMeta.DisplayName = sol.Metadata.DisplayName
		solMeta.Description = sol.Metadata.Description
		solMeta.Category = sol.Metadata.Category
		if sol.Metadata.Tags != nil {
			solMeta.Tags = sol.Metadata.Tags
		}
		// Report the solution's runtime provenance (local path, else the
		// author-declared metadata.source) so the metadata provider surfaces
		// the same origin the proto SolutionMeta.Source carries.
		solMeta.Source = sol.Provenance()
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

	return json.Marshal(meta)
}

// injectHostMetadataSettings populates cfg.Settings["metadata"] with host runtime
// information so that the metadata plugin (and any other plugin that cares) can
// return version, entrypoint, args, and solution metadata to callers.
//
// This is the per-call (one-shot) path: the plugin process is spawned for a
// single solution, so both host-static and per-solution metadata are delivered
// together via the one-time ConfigureProvider call. Pool-mode hosts must use
// HostStaticProviderConfig instead (see its doc for why).
//
// NOTE: os.Args is included in the serialized settings. This means command-line
// arguments (potentially including sensitive values) are visible to every plugin
// that receives this config over the local gRPC socket. For built-in and other
// first-party plugins this is acceptable, but scafctl can be configured to allow
// external (third-party/untrusted) plugins (disabled by default; see the API
// server's AllowExternal / DisableExternal controls). Operators who enable
// external plugins are trusting those binaries with the host command line, so
// avoid passing secrets via CLI flags in that configuration.
func injectHostMetadataSettings(cfg *plugin.ProviderConfig, sol *solution.Solution) {
	if cfg == nil {
		return
	}

	// Determine entrypoint from binary name heuristic.
	entrypoint := EntrypointCLI
	if cfg.BinaryName == "" {
		entrypoint = EntrypointUnknown
	}

	settings := buildPerSolutionSettings(entrypoint, sol)
	if settings == nil {
		return
	}

	if cfg.Settings == nil {
		cfg.Settings = make(map[string]json.RawMessage)
	}
	for k, v := range settings {
		cfg.Settings[k] = v
	}
}

// buildPerSolutionSettings builds the settings map delivered to plugins on every
// provider execution via ExecuteProviderRequest.settings. It returns the
// complete host metadata blob (host-static fields plus the per-solution
// sub-object) under the "metadata" key.
//
// The blob must be complete rather than solution-only because the SDK merges
// execute-time settings over configure-time settings at the key level: the
// execute-time "metadata" entry fully replaces the configure-time one. Pool-mode
// hosts configure pooled plugins once with an empty-solution blob, so this
// per-execution blob is what carries correct per-solution values (name, version,
// source, ...) to those plugins. Callers pass the entrypoint that matches the
// configure-time blob (see hostEntrypoint) so host-static fields -- crucially
// the entrypoint -- are preserved through the override.
//
// Returns nil when the blob cannot be marshaled, matching the fail-soft
// behavior of the configure-time injection path.
func buildPerSolutionSettings(entrypoint string, sol *solution.Solution) map[string]json.RawMessage {
	raw, err := buildHostMetadataSettings(entrypoint, sol)
	if err != nil {
		return nil
	}
	return map[string]json.RawMessage{hostMetadataSettingsKey: raw}
}

// hostEntrypoint resolves the host entrypoint (cli/api/mcp) used to build the
// per-execution metadata blob. In pool mode the authoritative value is the
// entrypoint the pool baked into its host-static baseConfig at load time, so it
// is read back from there to guarantee the per-execution blob preserves it
// through the key-level settings override. In per-call mode (no pool) it falls
// back to the binary-name heuristic used by injectHostMetadataSettings.
func hostEntrypoint(cfg *prepareConfig) string {
	if cfg.pluginPool != nil {
		base := cfg.pluginPool.BaseProviderConfig()
		if raw, ok := base.Settings[hostMetadataSettingsKey]; ok {
			var meta struct {
				Entrypoint string `json:"entrypoint"`
			}
			if err := json.Unmarshal(raw, &meta); err == nil && meta.Entrypoint != "" {
				return meta.Entrypoint
			}
		}
		// A pool is a long-lived server, never the one-shot CLI path, so fall
		// back to Unknown rather than the CLI heuristic when the base config
		// could not supply an entrypoint.
		return EntrypointUnknown
	}

	if cfg.pluginCfg != nil && cfg.pluginCfg.BinaryName != "" {
		return EntrypointCLI
	}
	return EntrypointUnknown
}

// HostStaticProviderConfig builds a ProviderConfig that carries host-static
// metadata (build version/commit/time, entrypoint, command, args) for delivery
// to pooled plugins at load time.
//
// Pool-mode hosts (long-lived MCP/API servers) start a plugin process once and
// share it across many solutions and concurrent requests, so the one-time
// ConfigureProvider call cannot safely carry per-solution metadata. The
// solution object is therefore intentionally left empty here; per-solution
// metadata is delivered per-execution via ExecuteProviderRequest.SolutionMetadata.
//
// Without this, pooled plugins would only ever receive an empty ProviderConfig,
// causing the metadata provider to report entrypoint "unknown" and empty
// version/command fields under pool-mode hosts.
func HostStaticProviderConfig(binaryName, entrypoint string) plugin.ProviderConfig {
	cfg := plugin.ProviderConfig{BinaryName: binaryName}

	raw, err := buildHostMetadataSettings(entrypoint, nil)
	if err != nil {
		return cfg
	}
	cfg.Settings = map[string]json.RawMessage{hostMetadataSettingsKey: raw}
	return cfg
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
