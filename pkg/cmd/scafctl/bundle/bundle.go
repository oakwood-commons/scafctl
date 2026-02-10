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
		Short:        "Inspect and manage solution bundles",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Commands for inspecting, verifying, diffing, and extracting
			solution bundles built by 'scafctl build solution'.
		`),
	}

	cmd.AddCommand(CommandVerify(cliParams, ioStreams, path))
	cmd.AddCommand(CommandDiff(cliParams, ioStreams, path))
	cmd.AddCommand(CommandExtract(cliParams, ioStreams, path))

	return cmd
}
