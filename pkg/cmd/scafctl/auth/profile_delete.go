// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/auth"
	appconfig "github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandProfileDelete creates the 'auth profile delete' command.
func CommandProfileDelete(cliParams *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <handler> <profile>",
		Short: "Delete an auth profile",
		Long: strings.ReplaceAll(heredoc.Doc(`
			Delete an authentication profile for a handler.

			This removes the profile entry from the config file and clears any
			stored credentials (tokens and refresh tokens) for the profile.

			If the deleted profile is the active profile, the active profile is
			reset to the unnamed built-in profile.

			Examples:
			  # Delete the staging profile for GitHub
			  scafctl auth profile delete github staging

			  # Force delete without confirmation
			  scafctl auth profile delete github staging --force
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			ctx := cmd.Context()
			switch len(args) {
			case 0:
				return listKnownHandlers(ctx), cobra.ShellCompDirectiveNoFileComp
			case 1:
				return auth.ListConfiguredProfiles(ctx, args[0]), cobra.ShellCompDirectiveNoFileComp
			default:
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			handlerName := args[0]
			profileName := args[1]

			// Validate handler name
			if err := validateHandlerName(ctx, handlerName); err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Normalize and reject "default" / "built-in"
			profileName = auth.NormalizeProfileName(profileName)
			if profileName == "" {
				err := fmt.Errorf("cannot delete the unnamed built-in profile")
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			if err := auth.ValidateProfileName(profileName); err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Load config
			var configPathOpt string
			if configFlag := cmd.Root().Flag("config"); configFlag != nil && configFlag.Value.String() != "" {
				configPathOpt = configFlag.Value.String()
			}
			mgr := appconfig.NewManager(configPathOpt)
			cfg, err := mgr.Load()
			if err != nil {
				err = fmt.Errorf("failed to load config: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Check if profile exists
			found, err := auth.DeleteProfile(cfg, handlerName, profileName)
			if err != nil {
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
			if !found {
				err = fmt.Errorf("profile %q not found for handler %s", profileName, handlerName)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Save config
			if err := mgr.Save(); err != nil {
				err = fmt.Errorf("failed to save config: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Try to clear credentials (best-effort)
			// Always attempt logout even if status check fails -- credentials may exist
			// even when status is unavailable or shows unauthenticated.
			ctx = auth.WithProfile(ctx, profileName)
			handler, handlerErr := getHandler(ctx, handlerName)
			if handlerErr == nil {
				if logoutErr := handler.Logout(ctx); logoutErr != nil && !force {
					w.Warningf("Profile removed from config but failed to clear credentials: %v", logoutErr)
				}
			} else if !force {
				w.Warningf("Profile removed from config but could not clear credentials: %v", handlerErr)
			}

			w.Successf("Deleted profile %q from %s.", profileName, handlerName)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Suppress warnings when credentials cannot be cleared")

	return cmd
}
