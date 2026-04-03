// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandSolution creates the solution command group.
func CommandSolution(cliParams *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "solution",
		Aliases:      []string{"sol"},
		Short:        "Inspect and compare solution files",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Inspect and compare solution files.

			This command group provides tools for working with solution files,
			including structural comparison to understand what changed between
			versions.
		`),
		Example: heredoc.Docf(`
			# Compare two solution files
			$ %s solution diff -f v1.yaml -f v2.yaml

			# Compare with JSON output
			$ %s solution diff -f v1.yaml -f v2.yaml -o json

			# Compare catalog versions
			$ %s solution diff my-app@1.0.0 my-app@2.0.0
		`, binaryName, binaryName, binaryName),
	}

	cmd.AddCommand(CommandDiff(cliParams, ioStreams, binaryName))

	return cmd
}
