// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package bundle provides CLI commands for inspecting, verifying, and
// extracting solution bundles built by 'scafctl build solution'.
package bundle

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandBundle creates the bundle command group.
func CommandBundle(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "bundle",
		Aliases:      []string{"bun"},
		Short:        "Inspect and manage solution bundles",
		SilenceUsage: true,
		// Args: NoArgs + a RunE that shows help makes bare 'bundle' print help
		// and exit 0, while an unknown subcommand (e.g. the hard-removed
		// 'bundle diff') errors with "unknown command". A RunE is required:
		// cobra returns flag.ErrHelp (help, exit 0) for a non-runnable parent
		// *before* validating args, so NoArgs alone would never fire.
		Args: cobra.NoArgs,
		Long: heredoc.Doc(`
			Commands for inspecting, verifying, and extracting
			solution bundles built by 'scafctl build solution'.
		`),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(CommandVerify(cliParams, ioStreams, path))
	cmd.AddCommand(CommandExtract(cliParams, ioStreams, path))

	return cmd
}
