// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandLogout creates the 'auth logout' command.
func CommandLogout(_ *settings.Run, _ *terminal.IOStreams, _ string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logout <handler>",
		Short: "Clear authentication credentials",
		Long: heredoc.Doc(`
			Clear stored authentication credentials for an auth handler.

			This removes the stored refresh token, clears any cached access tokens,
			and removes metadata.

			Supported handlers:
			- entra: Microsoft Entra ID

			Examples:
			  # Logout from Entra ID
			  scafctl auth logout entra
		`),
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			handlerName := args[0]

			// Validate handler name
			if !IsSupportedHandler(handlerName) {
				err := fmt.Errorf("unknown auth handler: %s (supported: %v)", handlerName, SupportedHandlers())
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			handler, err := getEntraHandler(ctx)
			if err != nil {
				err = fmt.Errorf("failed to initialize auth handler: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			// Check if authenticated first
			status, err := handler.Status(ctx)
			if err != nil {
				err = fmt.Errorf("failed to check auth status: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			if !status.Authenticated {
				w.Info("Not currently authenticated with Entra ID.")
				return nil
			}

			if err := handler.Logout(ctx); err != nil {
				err = fmt.Errorf("logout failed: %w", err)
				w.Errorf("%v", err)
				return exitcode.WithCode(err, exitcode.GeneralError)
			}

			w.Success("Successfully logged out from Entra ID.")
			return nil
		},
	}

	return cmd
}
