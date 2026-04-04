// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package newcmd provides commands for creating new scafctl resources.
package newcmd

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandNew creates the 'new' command group.
func CommandNew(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:   "new",
		Short: fmt.Sprintf("Create new %s resources", path),
		Long: `Create new solutions, templates, and other scafctl resources from scratch.

Generates well-structured YAML scaffolds with best practices built in.`,
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cCmd.Use)

	cCmd.AddCommand(CommandSolution(cliParams, ioStreams, cmdPath))

	return cCmd
}
