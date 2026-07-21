// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package extract provides the polymorphic top-level `extract` command verb,
// with subcommands for extracting files from different scafctl artifact kinds.
package extract

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// Subcommand names for the extract verb.
const (
	subBundle = "bundle"
)

// CommandExtract creates the top-level `extract` command group. It is a
// polymorphic verb whose subcommands extract files from different scafctl
// artifact kinds.
func CommandExtract(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	cmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:          "extract",
		Short:        "Extract files from an artifact (bundles)",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Extract files from an artifact.

			Choose a subcommand for the kind of artifact you want to extract from:
			  - bundle: extract files from a bundled solution artifact
		`),
		Example: heredoc.Docf(`
			# Extract all files from a bundled solution
			$ %[1]s extract bundle my-solution@1.0.0

			# Extract only files needed by a resolver
			$ %[1]s extract bundle my-solution@1.0.0 --resolver mainTfTemplate
		`, binaryName),
	})

	cmd.AddCommand(CommandExtractBundle(cliParams, ioStreams, binaryName))

	return cmd
}
