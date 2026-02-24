// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package eval provides commands for evaluating and validating CEL expressions and Go templates.
package eval

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandEval creates the 'eval' command group.
func CommandEval(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:   "eval",
		Short: "Evaluate CEL expressions and Go templates",
		Long: `Evaluate and validate CEL expressions and Go templates in isolation.

Useful for testing expressions and templates before using them in solutions.`,
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cCmd.Use)

	cCmd.AddCommand(CommandCEL(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandTemplate(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandValidate(cliParams, ioStreams, cmdPath))

	return cCmd
}
