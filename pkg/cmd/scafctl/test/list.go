// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
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
	Output          string
	IncludeBuiltins bool
	Filter          []string
	Tag             []string
	Solution        []string
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
		SilenceUsage: true,
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams

			return runList(ctx, opts)
		},
	}

	// Register flags
	cCmd.Flags().StringVarP(&opts.File, "file", "f", "", "Path to the solution file")
	cCmd.Flags().StringVar(&opts.TestsPath, "tests-path", "", "Path to directory containing solution files with tests")
	cCmd.Flags().StringVarP(&opts.Output, "output", "o", "table", "Output format: table, json, yaml, quiet")
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

	// Validate input
	if opts.File == "" && opts.TestsPath == "" {
		err := fmt.Errorf("either --file (-f) or --tests-path must be specified")
		if w != nil {
			w.Errorf("%s", err)
		}
		return exitcode.WithCode(err, exitcode.InvalidInput)
	}

	// Discover solutions
	testsPath := opts.TestsPath
	if testsPath == "" {
		testsPath = opts.File
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
	format, _ := kvx.ParseOutputFormat(opts.Output)
	outputOpts := kvx.NewOutputOptions(opts.IOStreams)
	outputOpts.Format = format
	outputOpts.Ctx = ctx

	return soltesting.ReportList(solutions, outputOpts, opts.IncludeBuiltins)
}
