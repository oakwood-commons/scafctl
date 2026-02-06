package auth

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandAuth creates the 'auth' command group.
func CommandAuth(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "auth",
		Aliases: []string{"authenticate"},
		Short:   "Manage authentication",
		Long: heredoc.Doc(`
			Manage authentication for scafctl.

			Authentication handlers manage identity verification and token acquisition
			for accessing protected resources. scafctl supports the following auth handlers:

			- entra: Microsoft Entra ID (formerly Azure AD)

			Use 'scafctl auth login <handler>' to authenticate.
			Use 'scafctl auth status' to check current authentication status.
			Use 'scafctl auth logout <handler>' to clear credentials.
			Use 'scafctl auth token <handler>' to display a token (for debugging).
		`),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandLogin(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandLogout(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandStatus(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandToken(cliParams, ioStreams, cmdPath))

	return cmd
}
