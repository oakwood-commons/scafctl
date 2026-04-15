// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// ListOptions holds configuration for the test list command.
type ListOptions struct {
	IOStreams       *terminal.IOStreams
	CliParams       *settings.Run
	File            string
	TestsPath       string
	IncludeBuiltins bool
	Filter          []string
	Tag             []string
	Solution        []string

	// kvx output integration
	flags.KvxOutputFlags
}

// CommandList creates the 'test list' subcommand.
func CommandList(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &ListOptions{}

	cCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls", "l"},
		Short:   "List available tests without executing them",
		Long: `List all functional tests defined in solution files.

Shows test names, commands, tags, and skip status without running any tests.
Useful for discovering available tests and verifying test configuration.

Examples:
  # Auto-discover solution in current directory
  scafctl test list

  # List all tests in a solution
  scafctl test list -f ./solution.yaml

  # List tests from a directory
  scafctl test list --tests-path ./solutions/

  # Include builtin tests
  scafctl test list -f ./solution.yaml --include-builtins

  # Filter by tag
  scafctl test list -f ./solution.yaml --tag smoke

  # Output as JSON
  scafctl test list -f ./solution.yaml -o json`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.AppName = cliParams.BinaryName

			return runList(ctx, opts)
		},
	}

	// Register flags
	cCmd.Flags().StringVarP(&opts.File, "file", "f", "", "Solution file path (auto-discovered if not provided)")
	cCmd.Flags().StringVar(&opts.TestsPath, "tests-path", "", "Path to directory containing solution files with tests")
	flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)
	cCmd.Flags().BoolVar(&opts.IncludeBuiltins, "include-builtins", false, "Include builtin tests in the listing")
	cCmd.Flags().StringArrayVar(&opts.Filter, "filter", nil, "Filter tests by name glob pattern (can be repeated)")
	cCmd.Flags().StringArrayVar(&opts.Tag, "tag", nil, "Filter tests by tag (can be repeated)")
	cCmd.Flags().StringArrayVar(&opts.Solution, "solution", nil, "Filter by solution name glob pattern (can be repeated)")

	return cCmd
}

// runList implements the test list command logic.
func runList(ctx context.Context, opts *ListOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		w = writer.New(opts.IOStreams, opts.CliParams)
	}

	// Determine the path to discover solutions from.
	// Priority: --tests-path > -f > auto-discover
	testsPath := opts.TestsPath
	if testsPath == "" {
		testsPath = opts.File
	}
	if testsPath == "" {
		testsPath = get.NewGetterFromContext(ctx).FindSolution()
	}
	if testsPath == "" {
		err := fmt.Errorf("no solution path provided and no solution file found in default locations; use --file (-f) or --tests-path")
		if w != nil {
			w.Errorf("%s", err)
		}
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	solutions, err := soltesting.DiscoverSolutions(testsPath)
	if err != nil {
		if w != nil {
			w.Errorf("discovery failed: %s", err)
		}
		return exitcode.WithCode(err, exitcode.FileNotFound)
	}

	// Apply filters
	filterOpts := soltesting.FilterOptions{
		NamePatterns:     opts.Filter,
		Tags:             opts.Tag,
		SolutionPatterns: opts.Solution,
	}
	solutions = soltesting.FilterTests(solutions, filterOpts)

	if len(solutions) == 0 {
		if w != nil {
			w.Info("No solutions with tests found.")
		}
		return nil
	}

	// Report
	outputOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
		kvx.WithIOStreams(opts.IOStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(opts.CliParams.NoColor),
	)

	return soltesting.ReportList(solutions, outputOpts, opts.IncludeBuiltins)
}
