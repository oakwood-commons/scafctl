// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
)

// CommandTest creates the 'test' command that runs and manages functional tests.
func CommandTest(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:     "test",
		Aliases: []string{"t"},
		Short:   "Run and manage functional tests",
		Long: fmt.Sprintf(`Run and manage functional tests for %s solutions.

SUBCOMMANDS:
  functional  Run functional tests against solutions
  init        Generate a starter test suite from a solution
  list        List available tests without executing them

Functional tests validate that solutions behave correctly by executing
scafctl commands against them and checking the output. Tests are defined
inline in solution YAML under spec.tests or in separate test files
under a tests/ directory.`, settings.CliBinaryName),
		SilenceUsage: true,
	}

	cmdPath := fmt.Sprintf("%s/%s", path, cCmd.Use)
	cCmd.AddCommand(CommandFunctional(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandInit(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))

	return cCmd
}
