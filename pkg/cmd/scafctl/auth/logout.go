package auth

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
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
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			w := writer.MustFromContext(ctx)
			handlerName := args[0]

			// Validate handler name
			if !IsSupportedHandler(handlerName) {
				return fmt.Errorf("unknown auth handler: %s (supported: %v)", handlerName, SupportedHandlers())
			}

			handler, err := getEntraHandler(ctx)
			if err != nil {
				return fmt.Errorf("failed to initialize auth handler: %w", err)
			}

			// Check if authenticated first
			status, err := handler.Status(ctx)
			if err != nil {
				return fmt.Errorf("failed to check auth status: %w", err)
			}

			if !status.Authenticated {
				w.Info("Not currently authenticated with Entra ID.")
				return nil
			}

			if err := handler.Logout(ctx); err != nil {
				return fmt.Errorf("logout failed: %w", err)
			}

			w.Success("Successfully logged out from Entra ID.")
			return nil
		},
	}

	return cmd
}
