// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package secrets provides commands for managing scafctl secrets.
package secrets

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandSecrets creates the 'secrets' command.
func CommandSecrets(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "secrets",
		Aliases: []string{"secret"},
		Short:   "Manage encrypted secrets",
		Long: heredoc.Doc(`
			Securely manage encrypted secrets for authentication and configuration.

			Secrets are encrypted with AES-256-GCM and stored in XDG-compliant locations:
			  - Linux:   ~/.local/share/scafctl/secrets/
			  - macOS:   ~/.local/share/scafctl/secrets/
			  - Windows: %LOCALAPPDATA%\scafctl\secrets\

			The master encryption key is stored in your OS keychain for security.

			Reserved namespace:
			  Secret names starting with "scafctl." are reserved for internal use
			  and cannot be managed through these commands.
		`),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandGet(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandSet(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandDelete(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandExists(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandExport(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandImport(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandRotate(cliParams, ioStreams, cmdPath))

	return cmd
}
