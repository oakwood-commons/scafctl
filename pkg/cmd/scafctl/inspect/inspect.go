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
		Short:        "Inspect a specific resource instance (add --usage for how to run it)",
		SilenceUsage: true,
		Long: heredoc.Doc(`
			Inspect the structure and metadata of a specific resource instance,
			with full kvx output support (table, JSON, YAML, tree, mermaid, and
			interactive mode).

			Which command do I want?
			  inspect solution          Structure of a specific solution file
			                            (resolvers, actions, dependencies, run command)
			  inspect solution --usage  How to consume it: parameters (with defaults
			                            and allowed values) and the command per action
			  explain <kind>            Schema of a kind (valid fields), not a file
			  get                       List what exists, or show one by name
		`),
		Example: heredoc.Docf(`
			# How do I run this solution? (usage view)
			$ %[1]s inspect solution -f ./my-solution.yaml --usage

			# Inspect a solution's structure from a file
			$ %[1]s inspect solution -f ./my-solution.yaml

			# From catalog with JSON output
			$ %[1]s inspect solution my-app -o json

			# Interactive TUI for exploring solution structure
			$ %[1]s inspect solution -f ./my-solution.yaml -i
		`, binaryName),
	}

	cmd.AddCommand(CommandInspectSolution(cliParams, ioStreams, binaryName))

	return cmd
}
