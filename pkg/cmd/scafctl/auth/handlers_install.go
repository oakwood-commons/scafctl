// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	authpkg "github.com/oakwood-commons/scafctl/pkg/auth"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	solprepare "github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// commandHandlersInstall creates the 'auth handlers install' command.
func commandHandlersInstall(cliParams *settings.Run, ioStreams *terminal.IOStreams) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "install <handler>",
		Short: "Download and cache an auth handler plugin",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Download an official auth handler plugin from the catalog and cache it
			locally. This pre-fetches the handler without requiring authentication,
			allowing you to inspect its capabilities (flows, display name) via
			'scafctl auth handlers'.

			Official auth handlers are normally auto-fetched on first use (e.g.,
			during 'scafctl auth login <handler>'). This command lets you fetch
			them ahead of time.

			Use --force to bypass the local cache and pull the latest version from
			the catalog, even if a version is already cached.

			Examples:
			  # Install the GitHub auth handler
			  scafctl auth handlers install github

			  # Install the Entra ID (Azure AD) auth handler
			  scafctl auth handlers install entra

			  # Upgrade to the latest version
			  scafctl auth handlers install entra --force
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			reg := authofficial.RegistryFromContext(cmd.Context())
			if reg == nil {
				reg = authofficial.NewRegistry()
			}
			return reg.Names(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			name := args[0]

			officialReg := authofficial.RegistryFromContext(ctx)
			if officialReg == nil {
				return fmt.Errorf("official auth handler registry not available")
			}

			entry, ok := officialReg.Get(name)
			if !ok {
				return fmt.Errorf("unknown auth handler %q; available: %s", name, strings.Join(officialReg.Names(), ", "))
			}

			// Check if already cached on disk (skip with --force).
			cacheKey := plugin.PluginCacheKey(name, solution.PluginKindAuthHandler)
			cache := plugin.NewCache(settings.PluginCacheDirFor(cliParams.BinaryName))
			if !force {
				if _, _, found := cache.GetLatestCached(cacheKey, plugin.CurrentPlatform()); found {
					w := writer.FromContext(ctx)
					if w != nil {
						w.Successf("Auth handler %q is already cached. Use --force to re-download.", name)
					}
					return nil
				}
			}

			w := writer.FromContext(ctx)
			if w != nil {
				if force {
					w.Infof("Fetching latest auth handler %q from catalog...", name)
				} else {
					w.Infof("Downloading auth handler %q from catalog...", name)
				}
			}

			dep := entry.ToPluginDependency()

			fetcher, err := solprepare.BuildPluginFetcherWithConfig(ctx, solprepare.PluginFetcherOverrides{
				NoCache: force,
			})
			if err != nil {
				return fmt.Errorf("building plugin fetcher: %w", err)
			}

			results, err := fetcher.FetchPlugins(ctx, []solution.PluginDependency{dep}, nil)
			if err != nil {
				return fmt.Errorf("downloading auth handler %q: %w", name, err)
			}

			if len(results) == 0 {
				return fmt.Errorf("no results returned for auth handler %q", name)
			}

			result := results[0]

			// Register the handler so we can report its capabilities.
			authReg := authpkg.RegistryFromContext(ctx)
			if authReg != nil {
				clients, regErr := plugin.RegisterFetchedAuthHandlerPlugins(ctx, authReg, results, nil)
				// Kill plugin processes — we only needed to inspect capabilities.
				for _, c := range clients {
					c.Kill()
				}
				if regErr != nil {
					// Non-fatal: the handler is cached, just can't inspect capabilities now.
					if w != nil {
						w.Warningf("Handler cached but could not load for inspection: %v", regErr)
					}
				}
			}

			if w != nil {
				if result.FromCache {
					w.Successf("Auth handler %q already cached (version %s). Use --force to upgrade.", name, result.Version)
				} else {
					w.Successf("Auth handler %q installed (version %s).", name, result.Version)
				}
			}

			_ = ioStreams

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Bypass cache and fetch the latest version from catalog")

	return cmd
}
