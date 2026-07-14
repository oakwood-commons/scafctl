// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package state provides commands for managing scafctl state files.
package state

import (
	"fmt"
	"strings"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandState creates the 'state' command group.
func CommandState(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "state",
		Short: "Manage solution state files",
		Long: strings.ReplaceAll(heredoc.Doc(`
			View and manage persisted state files.

			State files store resolver values across solution executions. They are
			created automatically when a solution has a state block with a file backend.

			The --path flag specifies the state file relative to the current working
			directory. Use an absolute path to reference files in other locations.
		`), settings.CliBinaryName, cliParams.BinaryName),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cmd.Use)
	cmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandShow(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandGet(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandSet(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandDelete(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandClear(cliParams, ioStreams, cmdPath))
	cmd.AddCommand(CommandFingerprints(cliParams, ioStreams, cmdPath))

	return cmd
}
