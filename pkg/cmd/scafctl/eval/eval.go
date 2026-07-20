// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package eval provides commands for evaluating and validating CEL expressions and Go templates.
package eval

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/cmd/cmdutil"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandEval creates the 'eval' command group.
func CommandEval(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := cmdutil.MakeHelpOnlyGroup(&cobra.Command{
		Use:     "eval",
		Aliases: []string{"e"},
		Short:   "Evaluate, validate, and analyze CEL expressions and Go templates",
		Long: `Work with CEL expressions and Go templates in isolation:

  eval cel        Evaluate a CEL expression
  eval template   Render a Go template
  eval validate   Check an expression or template for syntax errors
  eval refs       Extract the resolver references (_.name) an expression/template uses

Useful for testing and analyzing expressions and templates before using them
in solutions.`,
		SilenceUsage: true,
	})

	cmdPath := fmt.Sprintf("%s/%s", path, cCmd.Use)

	cCmd.AddCommand(CommandCEL(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandTemplate(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandValidate(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandRefs(cliParams, ioStreams, settings.SanitizeBinaryName(cliParams.BinaryName)))

	return cCmd
}
