// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"fmt"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// CommandTest creates the 'test' command that runs and manages functional tests.
// When invoked without a subcommand, it defaults to running functional tests
// (equivalent to 'test functional').
func CommandTest(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &FunctionalOptions{}

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
%s commands against them and checking the output. Tests are defined
inline in solution YAML under spec.testing.cases or in separate test files
under a tests/ directory.`, path, path),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		PreRun: func(_ *cobra.Command, args []string) {
			opts.positionalPathErr = nil
			bin := settings.CliBinaryName
			if cliParams != nil && cliParams.BinaryName != "" {
				bin = cliParams.BinaryName
			}
			cmdUsage := bin + " test"
			if _, err := source.ValidateFileFlagPositional(opts.File, cmdUsage); err != nil {
				opts.positionalPathErr = err
				return
			}
			if len(args) > 0 {
				if _, err := source.ValidatePositionalRef(args[0], opts.File, cmdUsage); err != nil {
					opts.positionalPathErr = err
				} else {
					opts.File = args[0]
				}
			}
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, "test")
			ctx := settings.IntoContext(cmd.Context(), cliParams)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.AppName = cliParams.BinaryName
			opts.Verbose = cliParams.Verbose

			if writer.FromContext(ctx) == nil {
				ctx = writer.WithWriter(ctx, writer.New(ioStreams, cliParams))
			}
			return runFunctional(ctx, opts)
		},
	}

	registerFunctionalFlags(cCmd, opts)

	cmdPath := filepath.Join(path, cCmd.Name())
	cCmd.AddCommand(CommandFunctional(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandInit(cliParams, ioStreams, cmdPath))
	cCmd.AddCommand(CommandList(cliParams, ioStreams, cmdPath))

	return cCmd
}
