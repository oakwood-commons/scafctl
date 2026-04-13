// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandInspect creates the top-level 'inspect' command group.
func CommandInspect(cliParams *settings.Run, ioStreams *terminal.IOStreams, binaryName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "inspect",
		Short:        "Inspect the structure and metadata of resources",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Inspect the structure and metadata of resources with full kvx output support.

			Provides structured views of solution metadata, resolvers, actions,
			parameters, and more. Supports all kvx output formats including
			table, JSON, YAML, tree, mermaid, and interactive mode.
		`),
		Example: heredoc.Docf(`
			# Inspect a solution from a file
			$ %[1]s inspect solution -f ./my-solution.yaml

			# Inspect from catalog with JSON output
			$ %[1]s inspect solution my-app -o json

			# Interactive TUI for exploring solution structure
			$ %[1]s inspect solution -f ./my-solution.yaml -i
		`, binaryName),
	}

	cmd.AddCommand(CommandInspectSolution(cliParams, ioStreams, binaryName))

	return cmd
}
