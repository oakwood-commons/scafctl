// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package examples provides commands for browsing and retrieving embedded scafctl examples.
package examples

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandExamples creates the 'examples' command group.
func CommandExamples(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:          "examples",
		Aliases:      []string{"ex"},
		Short:        fmt.Sprintf("Browse and retrieve %s example configurations", path),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cCmd.Use)

	cCmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandGet(cliParams, ioStreams, cmdPath))

	return cCmd
}
