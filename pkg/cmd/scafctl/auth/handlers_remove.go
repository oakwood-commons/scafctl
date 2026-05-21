// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// commandHandlersRemove creates the 'auth handlers remove' command.
func commandHandlersRemove(cliParams *settings.Run, ioStreams *terminal.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <handler>",
		Aliases: []string{"uninstall", "delete"},
		Short:   "Remove a cached auth handler plugin",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Remove a cached auth handler plugin from the local plugin cache.
			This deletes all cached versions of the handler.

			The handler can be re-downloaded at any time using
			'scafctl auth handlers install <handler>' or automatically on next use.

			Note: this does not remove any stored credentials. Use
			'scafctl auth logout <handler>' to clear tokens first if needed.

			Examples:
			  # Remove the GitHub auth handler
			  scafctl auth handlers remove github

			  # Remove the Entra ID auth handler
			  scafctl auth handlers remove entra
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

			return removeAuthHandler(ctx, name, cliParams, plugin.NewCache(settings.PluginCacheDirFor(cliParams.BinaryName)))
		},
	}

	_ = ioStreams

	return cmd
}

// removeAuthHandler removes a cached auth handler plugin by name.
func removeAuthHandler(ctx context.Context, name string, cliParams *settings.Run, cache *plugin.Cache) error {
	// Validate the handler name to prevent path traversal.
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") || name == "." {
		return fmt.Errorf("invalid auth handler name %q: must not contain path separators or '..'", name)
	}

	cacheKey := plugin.PluginCacheKey(name, solution.PluginKindAuthHandler)

	// Check if the handler has any cached binaries.
	handlerDir := filepath.Join(cache.Dir(), cacheKey)
	if _, err := os.Stat(handlerDir); os.IsNotExist(err) {
		return fmt.Errorf("auth handler %q is not installed (no cached binaries found)", name)
	}

	// Remove the entire handler directory (all versions, all platforms).
	if err := os.RemoveAll(handlerDir); err != nil {
		return fmt.Errorf("removing cached auth handler %q: %w", name, err)
	}

	w := writer.FromContext(ctx)
	if w != nil {
		w.Successf("Auth handler %q removed from cache.", name)
		w.Infof("Stored credentials are not affected. Use '%s auth logout %s' to clear tokens.", cliParams.BinaryName, name)
	}

	return nil
}
