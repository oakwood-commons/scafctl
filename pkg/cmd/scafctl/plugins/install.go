// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// InstallOptions holds options for the install command.
type InstallOptions struct {
	CliParams *settings.Run
	IOStreams *terminal.IOStreams
	File      string
	Platform  string
	CacheDir  string
	NoCache   bool
	Kind      string
	Version   string
	DryRun    bool
}

// CommandInstall creates the install subcommand.
func CommandInstall(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &InstallOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "install [plugin-name ...]",
		Short:        "Pre-fetch plugin binaries by name or from a solution",
		SilenceUsage: true,
		Long: strings.ReplaceAll(heredoc.Doc(`
			Pre-fetch and cache plugin binaries. Plugins can be specified
			by name (standalone mode) or loaded from a solution's
			bundle.plugins section (solution mode).

			In standalone mode, plugin names are resolved against the
			official provider and auth handler registries, then fetched
			from the configured catalog.

			Examples:
			  # Install a single plugin by name
			  scafctl plugins install github

			  # Install multiple plugins
			  scafctl plugins install github exec env

			  # Install a specific version
			  scafctl plugins install github --version ">=0.3.0"

			  # Install an auth handler plugin
			  scafctl plugins install github --kind auth-handler

			  # Install plugins from a solution file
			  scafctl plugins install -f solution.yaml

			  # Install for a different platform (e.g., for CI)
			  scafctl plugins install -f solution.yaml --platform linux/amd64
		`), settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx := writer.WithWriter(cmd.Context(), w)

			lgr := logger.FromContext(cmd.Context())
			if lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			return runInstall(ctx, opts, args)
		},
	}

	cmd.Flags().StringVarP(&opts.File, "file", "f", "", "Path to solution file (auto-discovered if not provided)")
	cmd.Flags().StringVar(&opts.Platform, "platform", "", "Target platform (default: current, e.g., linux/amd64)")
	cmd.Flags().StringVar(&opts.CacheDir, "cache-dir", "", fmt.Sprintf("Plugin cache directory (default: $XDG_CACHE_HOME/%s/plugins/)", path))
	cmd.Flags().BoolVar(&opts.NoCache, "no-cache", false, "Bypass the plugin cache and fetch directly from the catalog")
	cmd.Flags().StringVar(&opts.Kind, "kind", "provider", "Plugin kind: provider or auth-handler")
	cmd.Flags().StringVar(&opts.Version, "version", "", "Version constraint for standalone plugins (e.g., \">=0.3.0\", \"latest\")")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be installed without fetching")

	return cmd
}

func runInstall(ctx context.Context, opts *InstallOptions, args []string) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}
	lgr := logger.FromContext(ctx)

	// Determine plugins: from positional args (standalone) or solution file.
	var plugins []solution.PluginDependency
	var lockPlugins []bundler.LockPlugin

	if len(args) > 0 {
		// Standalone mode: build dependencies from plugin names.
		w.Verbosef("Standalone mode: resolving %d plugin name(s)", len(args))
		resolved, err := resolveStandalonePlugins(ctx, args, opts.Kind, opts.Version)
		if err != nil {
			w.Errorf("%v", err)
			return exitcode.WithCode(err, exitcode.InvalidInput)
		}
		plugins = resolved
	} else {
		// Solution mode: load from file.
		w.Verbosef("Solution mode: loading plugins from file")
		var err error
		plugins, lockPlugins, err = loadPluginsFromSolution(ctx, opts)
		if err != nil {
			return err
		}
	}

	if len(plugins) == 0 {
		return nil
	}

	for _, p := range plugins {
		w.Verbosef("  Plugin: %s (kind=%s, version=%s)", p.Name, p.Kind, p.Version)
	}

	if opts.DryRun {
		w.Infof("Dry run: would install %d plugin(s):", len(plugins))
		for _, p := range plugins {
			ver := p.Version
			if ver == "" {
				ver = "latest"
			}
			w.Infof("  %s (%s) %s", p.Name, p.Kind, ver)
		}
		return nil
	}

	// Build catalog chain from config
	appCfg := config.FromContext(ctx)
	var chainLogger logr.Logger
	if lgr != nil {
		chainLogger = *lgr
	} else {
		chainLogger = logr.Discard()
	}

	chain, err := catalog.BuildCatalogChain(appCfg, auth.RegistryFromContext(ctx), chainLogger)
	if err != nil {
		w.Errorf("failed to build catalog chain: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Create the fetcher
	fetcher := plugin.NewFetcher(plugin.FetcherConfig{
		Catalog:    chain,
		Cache:      plugin.NewCache(opts.CacheDir),
		Platform:   opts.Platform,
		NoCache:    opts.NoCache,
		BinaryName: settings.BinaryNameFromContext(ctx),
		Logger:     chainLogger,
	})

	// Fetch all plugins
	w.Infof("Installing %d plugin(s)...", len(plugins))

	results, err := fetcher.FetchPlugins(ctx, plugins, lockPlugins)
	if err != nil {
		w.Errorf("failed to install plugins: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Report results
	for _, r := range results {
		src := "catalog"
		if r.FromCache {
			src = "cache (already installed)"
		}
		w.Successf("  %s@%s (%s) → %s", r.Name, r.Version, src, r.Path)
	}

	// Invalidate all cached provider descriptors when plugins are freshly installed.
	// We invalidate everything because plugin names (used in catalog/solution) may
	// differ from provider names (used as cache keys by MCP handlers), making
	// targeted invalidation unreliable. The cache self-heals on next MCP access.
	descCache := plugin.NewDescriptorCache("", 0)
	hasNew := false
	for _, r := range results {
		if !r.FromCache {
			hasNew = true
			break
		}
	}
	if hasNew {
		descCache.InvalidateAll()
	}

	w.Successf("Installed %d plugin(s).", len(results))
	return nil
}

func loadSolution(path string) (*solution.Solution, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading solution from %s: %w", path, err)
	}
	var sol solution.Solution
	if err := sol.LoadFromBytes(data); err != nil {
		return nil, fmt.Errorf("parsing solution from %s: %w", path, err)
	}
	return &sol, nil
}

// resolveStandalonePlugins converts positional plugin name arguments into
// PluginDependency entries by looking up each name in the official provider
// and auth handler registries from context (falling back to default registries).
// Names not found in any registry are still accepted -- catalog resolution
// will determine if they exist.
func resolveStandalonePlugins(ctx context.Context, names []string, kind, version string) ([]solution.PluginDependency, error) {
	pluginKind := solution.PluginKind(kind)
	if !pluginKind.IsValid() {
		return nil, fmt.Errorf("invalid plugin kind %q (valid: provider, auth-handler)", kind)
	}

	provReg := official.RegistryFromContext(ctx)
	if provReg == nil {
		provReg = official.NewRegistry()
	}
	authReg := authofficial.RegistryFromContext(ctx)
	if authReg == nil {
		authReg = authofficial.NewRegistry()
	}
	deps := make([]solution.PluginDependency, 0, len(names))

	for _, name := range names {
		// Parse optional name@version syntax.
		pluginName, pluginVersion := parseNameVersion(name)
		if version != "" {
			pluginVersion = version // --version flag overrides inline version
		}

		var dep solution.PluginDependency

		switch pluginKind {
		case solution.PluginKindProvider:
			if p, ok := provReg.Get(pluginName); ok {
				dep = p.ToPluginDependency()
				if pluginVersion != "" {
					dep.Version = pluginVersion
				}
			} else {
				dep = solution.PluginDependency{
					Name:    pluginName,
					Kind:    solution.PluginKindProvider,
					Version: pluginVersion,
				}
			}
		case solution.PluginKindAuthHandler:
			if h, ok := authReg.Get(pluginName); ok {
				dep = h.ToPluginDependency()
				if pluginVersion != "" {
					dep.Version = pluginVersion
				}
			} else {
				dep = solution.PluginDependency{
					Name:    pluginName,
					Kind:    solution.PluginKindAuthHandler,
					Version: pluginVersion,
				}
			}
		}

		deps = append(deps, dep)
	}

	return deps, nil
}

// parseNameVersion splits "name@version" into (name, version).
// If no "@" is present, returns (input, "").
func parseNameVersion(s string) (string, string) {
	if idx := strings.LastIndex(s, "@"); idx > 0 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

// loadPluginsFromSolution loads plugin dependencies from a solution file.
func loadPluginsFromSolution(ctx context.Context, opts *InstallOptions) ([]solution.PluginDependency, []bundler.LockPlugin, error) {
	w := writer.FromContext(ctx)

	filePath := opts.File
	if filePath == "" {
		filePath = get.NewGetterFromContext(ctx).FindSolution()
	}
	if filePath == "" {
		err := fmt.Errorf("no plugin names or solution file provided; use positional args or --file (-f)")
		w.Errorf("%s", err)
		return nil, nil, exitcode.WithCode(err, exitcode.InvalidInput)
	}

	sol, err := loadSolution(filePath)
	if err != nil {
		w.Errorf("failed to load solution: %v", err)
		return nil, nil, exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if len(sol.Bundle.Plugins) == 0 {
		w.Infof("No plugins declared in solution — nothing to install.")
		return nil, nil, nil
	}

	lockFile, _ := bundler.LoadLockFile(filepath.Join(filepath.Dir(filePath), bundler.DefaultLockFileName))
	var lockPlugins []bundler.LockPlugin
	if lockFile != nil {
		lockPlugins = lockFile.Plugins
	}

	return sol.Bundle.Plugins, lockPlugins, nil
}
