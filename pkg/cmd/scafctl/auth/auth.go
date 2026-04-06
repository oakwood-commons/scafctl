// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"strings"

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
		Long: strings.ReplaceAll(heredoc.Doc(`
			Manage authentication for scafctl.

			Authentication handlers manage identity verification and token acquisition
			for accessing protected resources. Handlers are loaded from the auth registry
			and can be extended via plugins.

			Each handler declares its capabilities, which determine which flags and
			features are available. For example, some handlers support per-request
			scopes while others fix scopes at login time.

			Use 'scafctl auth login <handler>' to authenticate.
			Use 'scafctl auth status' to check current authentication status.
			Use 'scafctl auth list' to show cached refresh and access token metadata.
			Use 'scafctl auth logout <handler>' to clear credentials.
			Use 'scafctl auth token <handler>' to retrieve a token value (for debugging).
			Use 'scafctl auth diagnose' to run health checks and troubleshoot issues.
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandDiagnose(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandLogin(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandLogout(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandStatus(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandToken(cliParams, ioStreams, cmdPath))

	return cmd
}
