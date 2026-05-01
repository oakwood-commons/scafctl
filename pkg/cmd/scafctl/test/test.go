// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandTest creates the 'test' command that runs and manages functional tests.
// When invoked without a subcommand, it defaults to running functional tests
// (equivalent to 'test functional').
func CommandTest(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	cCmd := &cobra.Command{
		Use:     "test [reference]",
		Aliases: []string{"t"},
		Short:   "Run functional tests (default) or manage test suites",
		Long: fmt.Sprintf(`Run and manage functional tests for %s solutions.

When invoked without a subcommand, runs functional tests (same as 'test functional').

SUBCOMMANDS:
  functional  Run functional tests against solutions
  init        Generate a starter test suite from a solution
  list        List available tests without executing them

Functional tests validate that solutions behave correctly by executing
scafctl commands against them and checking the output. Tests are defined
inline in solution YAML under spec.testing.cases or in separate test files
under a tests/ directory.`, path),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, "test")
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			opts := &FunctionalOptions{
				IOStreams: ioStreams,
				CliParams: cliParams,
				Verbose:   cliParams.Verbose,
			}
			opts.AppName = cliParams.BinaryName

			if len(args) > 0 {
				if err := get.ValidatePositionalRef(args[0], opts.File, cliParams.BinaryName+" test"); err != nil {
					opts.positionalPathErr = err
				} else {
					opts.File = args[0]
				}
			}

			ctx = writer.WithWriter(ctx, writer.New(ioStreams, cliParams))
			return runFunctional(ctx, opts)
		},
	}

	cmdPath := filepath.Join(path, cCmd.Name())
	cCmd.AddCommand(CommandFunctional(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandInit(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))

	return cCmd
}
