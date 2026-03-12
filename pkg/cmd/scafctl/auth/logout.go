// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/input"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandLogout creates the 'auth logout' command.
func CommandLogout(cliParams *settings.Run, ioStreams *terminal.IOStreams, _ string) *cobra.Command {
	var (
		all    bool
		force  bool
		dryRun bool
		yes    bool
	)

	cmd := &cobra.Command{
		Use:   "logout [handler]",
		Short: "Clear authentication credentials",
		Long: heredoc.Doc(`
			Clear stored authentication credentials for an auth handler.

			This removes the stored refresh token, clears any cached access tokens,
			and removes metadata.

			Use --all to log out from every registered handler at once.
			Use --force to clear credentials even when not currently authenticated.

			Examples:
			  # Logout from Entra ID
			  scafctl auth logout entra

			  # Logout from GitHub
			  scafctl auth logout github

			  # Logout from GCP
			  scafctl auth logout gcp

			  # Logout from all registered handlers at once
			  scafctl auth logout --all

			  # Force clear credentials even if not currently logged in
			  scafctl auth logout entra --force

			  # Force clear all handlers' credentials
			  scafctl auth logout --all --force
			  # Show what would be removed without actually removing anything
			  scafctl auth logout entra --dry-run

			  # Dry-run across all handlers
		  scafctl auth logout --all --dry-run

		  # Log out from all handlers, skipping the confirmation prompt
		  scafctl auth logout --all --yes	`),
		SilenceUsage: true,
		Args: func(_ *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return fmt.Errorf("cannot specify a handler name when --all is set")
			}
			if !all && len(args) != 1 {
				return fmt.Errorf("accepts 1 arg(s) (handler name), received %d — or use --all to log out from all handlers", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.FromContext(ctx)
			if w == nil {
				return fmt.Errorf("writer not initialized in context")
			}

			var handlerNames []string
			if all {
				handlerNames = listHandlers(ctx)
				if len(handlerNames) == 0 {
					err := fmt.Errorf("no auth handlers registered")
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				// Require confirmation for --all unless --yes or --force is set.
				in := input.New(ioStreams, cliParams)
				confirmed, err := in.Confirm(input.NewConfirmOptions().
					WithPrompt(fmt.Sprintf("Log out from all %d handler(s) %v?", len(handlerNames), handlerNames)).
					WithSkipCondition(yes || force))
				if err != nil {
					w.Errorf("failed to read confirmation: %v", err)
					return exitcode.WithCode(err, exitcode.GeneralError)
				}
				if !confirmed {
					w.Info("Logout cancelled.")
					return nil
				}
			} else {
				handlerName := args[0]
				if err := validateHandlerName(ctx, handlerName); err != nil {
					w.Errorf("%v", err)
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
				handlerNames = []string{handlerName}
			}

			hadError := false
			for _, handlerName := range handlerNames {
				handler, err := getHandler(ctx, handlerName)
				if err != nil {
					w.Errorf("Failed to initialize auth handler %s: %v", handlerName, err)
					hadError = true
					continue
				}

				if !force {
					// Check if authenticated first; skip logout if not.
					status, err := handler.Status(ctx)
					if err != nil {
						w.Errorf("Failed to check auth status for %s: %v", handlerName, err)
						hadError = true
						continue
					}
					if !status.Authenticated {
						w.Infof("Not currently authenticated with %s.", handler.DisplayName())
						continue
					}
				}

				if dryRun {
					w.Infof("[dry-run] Would log out from %s (cached tokens and refresh token would be removed).", handler.DisplayName())
					continue
				}

				if err := handler.Logout(ctx); err != nil {
					w.Errorf("Logout failed for %s: %v", handlerName, err)
					hadError = true
					continue
				}

				w.Successf("Successfully logged out from %s.", handler.DisplayName())
			}

			if hadError {
				err := fmt.Errorf("one or more logout operations failed")
				return exitcode.WithCode(err, exitcode.GeneralError)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Log out from all registered auth handlers")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Clear stored credentials even if not currently authenticated")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be removed without actually removing credentials")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt when using --all")
	return cmd
}
