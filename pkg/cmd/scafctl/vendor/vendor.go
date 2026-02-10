// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package vendor provides CLI commands for managing vendored solution dependencies.
package vendor

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandVendor creates the vendor command group.
func CommandVendor(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "vendor",
		Short:        "Manage vendored solution dependencies",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Commands for managing vendored catalog dependencies
			used by solution bundles.
		`),
	}

	cmd.AddCommand(CommandUpdate(cliParams, ioStreams, path))

	return cmd
}
