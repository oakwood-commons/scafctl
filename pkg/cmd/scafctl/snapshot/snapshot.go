// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package snapshot

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandSnapshot creates the snapshot command
func CommandSnapshot(cliParams *settings.Run, ioStreams terminal.IOStreams, binaryName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "snapshot",
		Aliases:      []string{"snap"},
		Short:        "Manage resolver execution snapshots",
		SilenceUsage: true,
		// Args: NoArgs + a RunE that shows help makes bare 'snapshot' print
		// help and exit 0, while an unknown subcommand (e.g. the hard-removed
		// 'snapshot diff') errors with "unknown command". A RunE is required:
		// cobra returns flag.ErrHelp (help, exit 0) for a non-runnable parent
		// *before* validating args, so NoArgs alone would never fire.
		Args: cobra.NoArgs,
		Long: heredoc.Doc(`
			Manage resolver execution snapshots for debugging, testing, and comparison.
			
			Snapshots capture the complete state of resolver execution including values,
			status, timing, errors, and metadata. They can be saved to files for later
			analysis, used in golden file testing, or compared to detect configuration drift.
			
			To create a snapshot, use: scafctl render solution --snapshot --snapshot-file=snapshot.json
		`),
		Example: heredoc.Docf(`
			# Create a snapshot (via render solution)
			$ %s render solution -f config.yaml --snapshot --snapshot-file=snapshot.json
			
			# Load and display a snapshot
			$ %s snapshot show snapshot.json
			
			# Compare two snapshots
			$ %s diff snapshot before.json after.json
		`, binaryName, binaryName, binaryName),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(CommandShow(cliParams, ioStreams, binaryName))

	return cmd
}
