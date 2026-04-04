// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package config provides commands for managing scafctl configuration.
package config

import (
	"fmt"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandConfig creates the 'config' command.
func CommandConfig(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:     "config",
		Aliases: []string{"cfg"},
		Short:   fmt.Sprintf("Manage %s configuration", path),
		Long: heredoc.Doc(`
			View and manage scafctl configuration.

			Configuration follows the XDG Base Directory Specification:
			  - Linux:   ~/.config/scafctl/config.yaml
			  - macOS:   ~/.config/scafctl/config.yaml
			  - Windows: %LOCALAPPDATA%\scafctl\config.yaml

			Use --config flag to specify an alternate location.

			Environment variables with SCAFCTL_ prefix override config file values.
			For nested keys, use underscores (e.g., SCAFCTL_SETTINGS_NOCOLOR).

			Use 'scafctl config paths' to see all resolved paths.
		`),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cCmd.Use)
	cCmd.AddCommand(CommandInit(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandView(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandShow(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandGet(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandSet(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandUnset(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandValidate(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandAddCatalog(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandRemoveCatalog(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandUseCatalog(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandSchema(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandPaths(cliParams, ioStreams, cmdPath))

	return cCmd
}
