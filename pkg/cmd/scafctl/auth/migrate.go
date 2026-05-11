// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	authmigrate "github.com/oakwood-commons/scafctl/pkg/auth/migrate"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// buildPluginFetchFunc creates a FetchFunc that builds a catalog chain and
// fetches plugins. This is the production wiring; tests can supply their own.
func buildPluginFetchFunc(binaryName string) authmigrate.FetchFunc {
	return func(ctx context.Context, deps []solution.PluginDependency) ([]plugin.FetchResult, error) {
		appCfg := config.FromContext(ctx)
		var chainLogger logr.Logger
		lgr := logger.FromContext(ctx)
		if lgr != nil {
			chainLogger = *lgr
		} else {
			chainLogger = logr.Discard()
		}

		chain, err := catalog.BuildCatalogChain(appCfg, auth.RegistryFromContext(ctx), chainLogger)
		if err != nil {
			return nil, fmt.Errorf("building catalog chain: %w", err)
		}

		fetcher := plugin.NewFetcher(plugin.FetcherConfig{
			Catalog:    chain,
			BinaryName: binaryName,
			Logger:     chainLogger,
		})

		return fetcher.FetchPlugins(ctx, deps, nil)
	}
}

// CommandMigrate creates the 'auth migrate' command for proactive pre-Stage-B migration.
func CommandMigrate(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Pre-install official auth handler plugins from catalog",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Pre-install all official auth handler plugins from the catalog
			and validate token migration readiness.

			This command:
			  1. Pre-installs all official auth handler plugins from the catalog
			  2. Validates token migration (checks cached tokens are accessible)
			  3. Reports pass/fail per handler with actionable guidance

			Run this before upgrading to a release that removes built-in
			auth handlers. It is safe to run multiple times.

			Examples:
			  # Migrate all official auth handlers to plugins
			  scafctl auth migrate
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrate(cmd.Context(), buildPluginFetchFunc(cliParams.BinaryName))
		},
	}

	return cmd
}

// runMigrate is the testable core of the migrate command. It accepts a
// FetchFunc so tests can inject a mock without hitting the catalog.
func runMigrate(ctx context.Context, fetchFn authmigrate.FetchFunc) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return fmt.Errorf("writer not initialized in context")
	}

	officialReg := authofficial.RegistryFromContext(ctx)
	if officialReg == nil {
		w.Errorf("Official auth handler registry not available")
		return exitcode.WithCode(
			fmt.Errorf("official auth handler registry not in context"),
			exitcode.GeneralError,
		)
	}

	authReg := auth.RegistryFromContext(ctx)

	w.Infof("Migrating auth handlers to plugins...")
	w.Plainln("")

	results := authmigrate.Handlers(ctx, officialReg, authReg, fetchFn)

	// Print results.
	failCount := 0
	for _, r := range results {
		w.Infof("  %s:", r.Name)
		w.Infof("    Plugin: %s", r.PluginSource)
		w.Infof("    Tokens: %s", r.TokenMessage)
		if r.IsReady() {
			w.Successf("    Status: %s", r.StatusString())
		} else {
			w.Errorf("    Status: %s", r.StatusString())
			if r.ErrorMessage != "" {
				w.Errorf("    Error: %s", r.ErrorMessage)
			}
			failCount++
		}
		w.Plainln("")
	}

	if failCount > 0 {
		w.Errorf("Migration incomplete. %d handler(s) failed.", failCount)
		return exitcode.WithCode(
			fmt.Errorf("migration incomplete: %d handler(s) failed", failCount),
			exitcode.GeneralError,
		)
	}

	w.Successf("Migration complete. All handlers ready for plugin mode.")
	return nil
}
