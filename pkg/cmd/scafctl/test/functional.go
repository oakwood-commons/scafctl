// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// FunctionalOptions holds configuration for the test functional command.
type FunctionalOptions struct {
	IOStreams       *terminal.IOStreams
	CliParams       *settings.Run
	File            string
	TestsPath       string
	Output          string
	ReportFile      string
	UpdateSnapshots bool
	Sequential      bool
	Concurrency     int
	SkipBuiltins    bool
	TestTimeout     time.Duration
	Timeout         time.Duration
	Filter          []string
	Tag             []string
	Solution        []string
	DryRun          bool
	FailFast        bool
	Verbose         bool
	KeepSandbox     bool
	NoProgress      bool
}

// CommandFunctional creates the 'test functional' subcommand.
func CommandFunctional(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &FunctionalOptions{}

	cCmd := &cobra.Command{
		Use:     "functional",
		Aliases: []string{"func", "fn"},
		Short:   "Run functional tests against solutions",
		Long: `Run functional tests defined in solution YAML files.

Tests execute scafctl commands in isolated sandboxes and validate output
using CEL expressions, regex matching, substring checks, and golden-file
snapshots.

Examples:
  # Run all tests in a solution
  scafctl test functional -f ./solution.yaml

  # Run tests from a directory
  scafctl test functional --tests-path ./solutions/

  # Run with filters
  scafctl test functional -f ./solution.yaml --filter "render-*" --tag smoke

  # Update snapshots
  scafctl test functional -f ./solution.yaml --update-snapshots

  # Run sequentially with verbose output
  scafctl test functional -f ./solution.yaml --sequential -v

  # Generate JUnit report
  scafctl test functional -f ./solution.yaml --report-file results.xml

  # Dry run (validate only)
  scafctl test functional -f ./solution.yaml --dry-run`,
		SilenceUsage: true,
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams

			return runFunctional(ctx, opts)
		},
	}

	// Register flags
	cCmd.Flags().StringVarP(&opts.File, "file", "f", "", "Path to the solution file")
	cCmd.Flags().StringVar(&opts.TestsPath, "tests-path", "", "Path to directory containing solution files with tests")
	cCmd.Flags().StringVarP(&opts.Output, "output", "o", "table", "Output format: table, json, yaml, quiet")
	cCmd.Flags().StringVar(&opts.ReportFile, "report-file", "", "Path to write JUnit XML report")
	cCmd.Flags().BoolVar(&opts.UpdateSnapshots, "update-snapshots", false, "Update golden files instead of comparing")
	cCmd.Flags().BoolVar(&opts.Sequential, "sequential", false, "Run tests sequentially (no concurrency)")
	cCmd.Flags().IntVarP(&opts.Concurrency, "concurrency", "j", 1, "Maximum number of tests to run in parallel")
	cCmd.Flags().BoolVar(&opts.SkipBuiltins, "skip-builtins", false, "Skip builtin tests (parse, lint, resolve-defaults, render-defaults)")
	cCmd.Flags().DurationVar(&opts.TestTimeout, "test-timeout", 0, "Default timeout per test (e.g., 30s, 5m)")
	cCmd.Flags().DurationVar(&opts.Timeout, "timeout", 0, "Global timeout for all tests (e.g., 10m)")
	cCmd.Flags().StringArrayVar(&opts.Filter, "filter", nil, "Filter tests by name glob pattern (can be repeated)")
	cCmd.Flags().StringArrayVar(&opts.Tag, "tag", nil, "Filter tests by tag (can be repeated)")
	cCmd.Flags().StringArrayVar(&opts.Solution, "solution", nil, "Filter by solution name glob pattern (can be repeated)")
	cCmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Validate tests without executing commands")
	cCmd.Flags().BoolVar(&opts.FailFast, "fail-fast", false, "Stop remaining tests on first failure")
	cCmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Enable verbose output (assertion counts, details)")
	cCmd.Flags().BoolVar(&opts.KeepSandbox, "keep-sandbox", false, "Keep sandbox directories after test execution")
	cCmd.Flags().BoolVar(&opts.NoProgress, "no-progress", false, "Disable live progress output during test execution")

	return cCmd
}

// runFunctional implements the test functional command logic.
func runFunctional(ctx context.Context, opts *FunctionalOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		// Fallback: create writer if not in context (e.g., direct invocation)
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

	if len(solutions) == 0 {
		if w != nil {
			w.Info("No solutions with tests found.")
		}
		return nil
	}

	// Build runner
	concurrency := opts.Concurrency
	if opts.Sequential {
		concurrency = 1
	}

	skipBuiltins := soltesting.SkipBuiltinsValue{}
	if opts.SkipBuiltins {
		skipBuiltins.All = true
	}

	// Apply skip builtins to each solution's test config
	if opts.SkipBuiltins {
		for i := range solutions {
			if solutions[i].TestConfig == nil {
				solutions[i].TestConfig = &soltesting.TestConfig{}
			}
			solutions[i].TestConfig.SkipBuiltins = skipBuiltins
		}
	}

	// Resolve the binary path for subprocess execution.
	// Each test runs as a child process for true parallelism and env isolation.
	binaryPath, err := os.Executable()
	if err != nil {
		if w != nil {
			w.Errorf("failed to resolve executable path: %s", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	runner := &soltesting.Runner{
		BinaryPath:      binaryPath,
		Concurrency:     concurrency,
		FailFast:        opts.FailFast,
		UpdateSnapshots: opts.UpdateSnapshots,
		Verbose:         opts.Verbose,
		KeepSandbox:     opts.KeepSandbox,
		TestTimeout:     opts.TestTimeout,
		GlobalTimeout:   opts.Timeout,
		DryRun:          opts.DryRun,
		IOStreams:       opts.IOStreams,
		Filter: soltesting.FilterOptions{
			NamePatterns:     opts.Filter,
			Tags:             opts.Tag,
			SolutionPatterns: opts.Solution,
		},
	}

	// Set up progress reporting unless explicitly disabled or output format
	// is non-table (json/yaml/quiet — progress would corrupt structured output).
	if !opts.NoProgress && !opts.DryRun {
		format, _ := kvx.ParseOutputFormat(opts.Output)
		if kvx.IsTableFormat(format) || opts.Output == "" {
			if kvx.IsTerminal(opts.IOStreams.ErrOut) {
				runner.Progress = NewMPBTestProgress(opts.IOStreams.ErrOut)
			} else {
				runner.Progress = NewLineTestProgress(opts.IOStreams.ErrOut)
			}
		}
	}

	// Execute tests
	start := time.Now()
	results, err := runner.Run(ctx, solutions)
	elapsed := time.Since(start)

	// Wait for progress output to flush before writing the report
	if runner.Progress != nil {
		runner.Progress.Wait()
	}

	if err != nil {
		if w != nil {
			w.Errorf("test execution failed: %s", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Report results
	format, _ := kvx.ParseOutputFormat(opts.Output)
	outputOpts := kvx.NewOutputOptions(opts.IOStreams)
	outputOpts.Format = format
	outputOpts.Ctx = ctx

	if err := soltesting.ReportResults(results, outputOpts, opts.Verbose, elapsed); err != nil {
		if w != nil {
			w.Errorf("reporting failed: %s", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	// Write JUnit report if requested
	if opts.ReportFile != "" {
		if err := soltesting.WriteJUnitReport(results, opts.ReportFile); err != nil {
			if w != nil {
				w.Errorf("JUnit report failed: %s", err)
			}
			return exitcode.WithCode(err, exitcode.GeneralError)
		}
	}

	// Determine exit code from results
	summary := soltesting.Summarize(results)
	if summary.Failed > 0 || summary.Errors > 0 {
		return exitcode.WithCode(
			fmt.Errorf("%d failed, %d errors", summary.Failed, summary.Errors),
			exitcode.TestFailed,
		)
	}

	return nil
}
