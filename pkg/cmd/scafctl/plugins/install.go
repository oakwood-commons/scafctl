// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
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
}

// CommandInstall creates the install subcommand.
func CommandInstall(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	opts := &InstallOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	cmd := &cobra.Command{
		Use:          "install",
		Short:        "Pre-fetch plugin binaries declared in a solution",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Pre-fetch and cache plugin binaries declared in a solution's
			bundle.plugins section.

			This command resolves plugin version constraints against configured
			catalogs, downloads the binaries, and stores them in the local
			plugin cache. Subsequent 'scafctl run solution' invocations will
			use the cached binaries without network access.

			Examples:
			  # Install plugins for a solution
			  scafctl plugins install -f solution.yaml

			  # Install for a different platform (e.g., for CI)
			  scafctl plugins install -f solution.yaml --platform linux/amd64

			  # Use a custom cache directory
			  scafctl plugins install -f solution.yaml --cache-dir /tmp/plugins
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			w := writer.FromContext(cmd.Context())
			if w == nil {
				w = writer.New(ioStreams, cliParams)
			}
			ctx := writer.WithWriter(cmd.Context(), w)

			lgr := logger.FromContext(cmd.Context())
			if lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			return runInstall(ctx, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.File, "file", "f", "", "Path to solution file (auto-discovered if not provided)")
	cmd.Flags().StringVar(&opts.Platform, "platform", "", "Target platform (default: current, e.g., linux/amd64)")
	cmd.Flags().StringVar(&opts.CacheDir, "cache-dir", "", "Plugin cache directory (default: $XDG_CACHE_HOME/scafctl/plugins/)")

	return cmd
}

func runInstall(ctx context.Context, opts *InstallOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}
	lgr := logger.FromContext(ctx)

	// Auto-discover solution file if not provided
	filePath := opts.File
	if filePath == "" {
		filePath = get.NewGetter().FindSolution()
	}
	if filePath == "" {
		err := fmt.Errorf("no solution path provided and no solution file found in default locations; use --file (-f)")
		w.Errorf("%s", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Load the solution
	sol, err := loadSolution(filePath)
	if err != nil {
		w.Errorf("failed to load solution: %v", err)
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	if len(sol.Bundle.Plugins) == 0 {
		w.Infof("No plugins declared in solution — nothing to install.")
		return nil
	}

	// Load lock file if available
	lockFile, _ := bundler.LoadLockFile(filepath.Join(filepath.Dir(filePath), bundler.DefaultLockFileName))
	var lockPlugins []bundler.LockPlugin
	if lockFile != nil {
		lockPlugins = lockFile.Plugins
	}

	// Build catalog chain from config
	appCfg := config.FromContext(ctx)
	var chainLogger logr.Logger
	if lgr != nil {
		chainLogger = *lgr
	} else {
		chainLogger = logr.Discard()
	}

	chain, err := catalog.BuildCatalogChain(appCfg, chainLogger)
	if err != nil {
		w.Errorf("failed to build catalog chain: %v", err)
		return exitcode.WithCode(err, exitcode.CatalogError)
	}

	// Create the fetcher
	fetcher := plugin.NewFetcher(plugin.FetcherConfig{
		Catalog:  chain,
		Cache:    plugin.NewCache(opts.CacheDir),
		Platform: opts.Platform,
		Logger:   chainLogger,
	})

	// Fetch all plugins
	w.Infof("Installing %d plugin(s)...", len(sol.Bundle.Plugins))

	results, err := fetcher.FetchPlugins(ctx, sol.Bundle.Plugins, lockPlugins)
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
