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
		Short:        "Compare solutions (solution diff)",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Structurally compare two solutions.

			Currently this group provides a single command, 'solution diff', which
			reports what changed between two solution files or catalog versions
			(resolvers, actions, parameters, and metadata).

			Related: use 'inspect solution' to examine a single solution, and
			'inspect solution --usage' to see how to run one.
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
