// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package diff provides the polymorphic top-level `diff` command verb, with
// subcommands for comparing solutions, bundles, and snapshots.
package diff

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// Subcommand names for the diff verb.
const (
	subSolution = "solution"
	subBundle   = "bundle"
	subSnapshot = "snapshot"
)

// CommandDiff creates the top-level `diff` command group. It is a polymorphic
// verb whose subcommands compare different scafctl artifact kinds.
func CommandDiff(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:          "diff",
		Short:        "Compare two artifacts (solutions, bundles, snapshots)",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Compare two artifacts and report their differences.

			Choose a subcommand for the kind of artifact you want to compare:
			  - solution: structurally compare two solution files or catalog versions
			  - bundle:   compare two bundled solution versions (files, vendored deps, plugins)
			  - snapshot: compare two resolver execution snapshots
		`),
		Example: heredoc.Docf(`
			# Compare two solution files
			$ %[1]s diff solution -f v1.yaml -f v2.yaml

			# Compare two bundled versions
			$ %[1]s diff bundle my-solution@1.0.0 my-solution@2.0.0

			# Compare two snapshots
			$ %[1]s diff snapshot before.json after.json
		`, binaryName),
	})

	cmd.AddCommand(CommandDiffSolution(cliParams, *ioStreams, binaryName))
	cmd.AddCommand(CommandDiffBundle(cliParams, ioStreams, binaryName))
	cmd.AddCommand(CommandDiffSnapshot(cliParams, *ioStreams, binaryName))

	return cmd
}
