// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package test

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
)

// FunctionalOptions holds configuration for the test functional command.
type FunctionalOptions struct {
	IOStreams         *terminal.IOStreams
	CliParams         *settings.Run
	File              string
	TestsPath         string
	ReportFile        string
	UpdateSnapshots   bool
	Sequential        bool
	Concurrency       int
	SkipBuiltins      bool
	TestTimeout       time.Duration
	Timeout           time.Duration
	Filter            []string
	Tag               []string
	Solution          []string
	DryRun            bool
	FailFast          bool
	Verbose           bool
	KeepSandbox       bool
	NoProgress        bool
	Watch             bool
	positionalPathErr error

	// kvx output integration
	flags.KvxOutputFlags
}

// CommandFunctional creates the 'test functional' subcommand.
func CommandFunctional(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	opts := &FunctionalOptions{}

	cCmd := &cobra.Command{
		Use:     "functional [reference]",
		Aliases: []string{"func", "fn"},
		Short:   "Run functional tests against solutions",
		Long: fmt.Sprintf(`Run functional tests defined in solution YAML files.

Tests execute %[1]s commands in isolated sandboxes and validate output
using CEL expressions, regex matching, substring checks, and golden-file
snapshots.

Examples:
  # Auto-discover solution in current directory
  %[1]s test functional

  # Run all tests in a solution
  %[1]s test functional -f ./solution.yaml

  # Run tests from a catalog or remote registry reference
  %[1]s test functional my-solution@1.0.0
  %[1]s test functional ghcr.io/myorg/solutions/my-solution@1.0.0

  # Run tests from a directory
  %[1]s test functional --tests-path ./solutions/

  # Run with filters
  %[1]s test functional -f ./solution.yaml --filter "render-*" --tag smoke

  # Update snapshots
  %[1]s test functional -f ./solution.yaml --update-snapshots

  # Run sequentially with verbose output
  %[1]s test functional -f ./solution.yaml --sequential -v

  # Generate JUnit report
  %[1]s test functional -f ./solution.yaml --report-file results.xml

  # Dry run (validate only)
  %[1]s test functional -f ./solution.yaml --dry-run

  # Watch mode - re-run on file changes
  %[1]s test functional -f ./solution.yaml --watch
  %[1]s test functional -f ./solution.yaml --watch --tag smoke`, cliParams.BinaryName),
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		PreRun: func(_ *cobra.Command, args []string) {
			if len(args) > 0 {
				if err := get.ValidatePositionalRef(args[0], opts.File, cliParams.BinaryName+" test functional"); err != nil {
					opts.positionalPathErr = err
				} else {
					opts.File = args[0]
				}
			}
		},
		RunE: func(cCmd *cobra.Command, _ []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Name())
			ctx := settings.IntoContext(cCmd.Context(), cliParams)

			opts.IOStreams = ioStreams
			opts.CliParams = cliParams
			opts.AppName = cliParams.BinaryName
			opts.Verbose = cliParams.Verbose

			return runFunctional(ctx, opts)
		},
	}

	// Register flags
	cCmd.Flags().StringVarP(&opts.File, "file", "f", "", "Solution file path (auto-discovered if not provided)")
	cCmd.Flags().StringVar(&opts.TestsPath, "tests-path", "", "Path to directory containing solution files with tests")
	// Add kvx output flags (-o, -i, -e) but not -w (conflicts with --watch)
	flags.AddKvxOutputFlags(cCmd, &opts.Output, &opts.Interactive, &opts.Expression)
	// Add --where without -w short form to avoid conflict with --watch
	cCmd.Flags().StringVar(&opts.Where, "where", "", "Per-item CEL boolean filter for list data (e.g., '_.enabled')")
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
	cCmd.Flags().BoolVar(&opts.KeepSandbox, "keep-sandbox", false, "Keep sandbox directories after test execution")
	cCmd.Flags().BoolVar(&opts.NoProgress, "no-progress", false, "Disable live progress output during test execution")
	cCmd.Flags().BoolVarP(&opts.Watch, "watch", "w", false, "Watch solution files for changes and re-run affected tests")

	return cCmd
}

// runFunctional implements the test functional command logic.
func runFunctional(ctx context.Context, opts *FunctionalOptions) error {
	w := writer.FromContext(ctx)
	if w == nil {
		// Fallback: create writer if not in context (e.g., direct invocation)
		w = writer.New(opts.IOStreams, opts.CliParams)
	}

	if opts.positionalPathErr != nil {
		w.Errorf("%s", opts.positionalPathErr)
		return exitcode.WithCode(opts.positionalPathErr, exitcode.InvalidInput)
	}

	// Determine the path to discover solutions from.
	// Priority: --tests-path > -f/positional arg > auto-discover
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

	// If the path is a catalog/remote reference (and not a local file/directory),
	// fetch the solution and its bundle to a temp directory.
	if get.IsCatalogReference(testsPath) && !fileExists(testsPath) {
		w.Infof("Fetching %s...", testsPath)
		getter := prepare.NewDefaultGetter(ctx, false)
		sol, bundleData, getErr := getter.GetWithBundle(ctx, testsPath)
		if getErr != nil {
			w.Errorf("failed to fetch solution %q: %v", testsPath, getErr)
			return exitcode.WithCode(getErr, exitcode.CatalogError)
		}
		solName := sol.Metadata.Name
		if sol.Metadata.Version != nil {
			solName += "@" + sol.Metadata.Version.String()
		}
		w.Infof("Resolved %s", solName)

		// Create a temp directory for the solution and its bundled files.
		tmpDir, tmpErr := os.MkdirTemp("", paths.AppName()+"-test-*")
		if tmpErr != nil {
			return fmt.Errorf("failed to create temp directory: %w", tmpErr)
		}
		defer os.RemoveAll(tmpDir)

		// Extract bundle tar if present (contains templates/, static files, etc.)
		if len(bundleData) > 0 {
			if _, extractErr := bundler.ExtractBundleTar(bundleData, tmpDir); extractErr != nil {
				return fmt.Errorf("failed to extract solution bundle: %w", extractErr)
			}
		}

		// Write the solution YAML into the temp directory.
		rawContent := sol.RawContent()
		if len(rawContent) == 0 {
			var marshalErr error
			rawContent, marshalErr = sol.ToYAML()
			if marshalErr != nil {
				return fmt.Errorf("failed to marshal solution: %w", marshalErr)
			}
		}
		solFile := filepath.Join(tmpDir, "solution.yaml")
		if writeErr := os.WriteFile(solFile, rawContent, 0o600); writeErr != nil {
			return fmt.Errorf("failed to write solution file: %w", writeErr)
		}
		testsPath = solFile
	}

	// Watch mode — delegate to the watcher loop.
	if opts.Watch {
		return runWatchMode(ctx, opts, w, testsPath)
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
			if solutions[i].Config == nil {
				solutions[i].Config = &soltesting.TestConfig{}
			}
			solutions[i].Config.SkipBuiltins = skipBuiltins
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
		BinaryName:      settings.BinaryNameFromContext(ctx),
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
	// is non-visual (json/yaml/quiet — progress would corrupt structured output).
	if !opts.NoProgress && !opts.DryRun {
		format, _ := kvx.ParseOutputFormat(opts.Output)
		if kvx.IsKvxFormat(format) {
			if kvx.IsTerminal(opts.IOStreams.ErrOut) {
				runner.Progress = NewMPBTestProgress(opts.IOStreams.ErrOut)
			} else {
				runner.Progress = NewLineTestProgress(w)
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
	outputOpts := flags.ToKvxOutputOptions(&opts.KvxOutputFlags,
		kvx.WithIOStreams(opts.IOStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(opts.CliParams.NoColor),
	)

	// When stdout is not a terminal (e.g. redirected to a file), include
	// per-test rows in the report even if progress was shown on stderr.
	reportProgress := runner.Progress
	if !kvx.IsTerminal(opts.IOStreams.Out) {
		reportProgress = nil
	}

	if err := soltesting.ReportResults(results, outputOpts, opts.Verbose, elapsed, reportProgress); err != nil {
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

// runWatchMode starts the file watcher and re-runs tests on changes.
// It blocks until Ctrl-C (SIGINT/SIGTERM) is received.
func runWatchMode(ctx context.Context, opts *FunctionalOptions, w *writer.Writer, testsPath string) error {
	// Set up signal handling for clean shutdown.
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	concurrency := opts.Concurrency
	if opts.Sequential {
		concurrency = 1
	}

	skipBuiltins := soltesting.SkipBuiltinsValue{}
	if opts.SkipBuiltins {
		skipBuiltins.All = true
	}

	binaryPath, err := os.Executable()
	if err != nil {
		if w != nil {
			w.Errorf("failed to resolve executable path: %s", err)
		}
		return exitcode.WithCode(err, exitcode.GeneralError)
	}

	runner := &soltesting.Runner{
		BinaryPath:      binaryPath,
		BinaryName:      settings.BinaryNameFromContext(ctx),
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

	// Unused in watch mode — skip builtins are applied per-run via
	// the solution's Config, which DiscoverSolutions populates.
	_ = skipBuiltins

	isTTY := kvx.IsTerminal(opts.IOStreams.ErrOut)

	watcher := &soltesting.Watcher{
		Runner:    runner,
		TestsPath: testsPath,
		Options: soltesting.WatchOptions{
			OnRunStart: func(triggerFile string) {
				// Set up progress for each run (mpb instances are single-use).
				if !opts.NoProgress && !opts.DryRun {
					format, _ := kvx.ParseOutputFormat(opts.Output)
					if kvx.IsKvxFormat(format) {
						if isTTY {
							runner.Progress = NewMPBTestProgress(opts.IOStreams.ErrOut)
						} else {
							runner.Progress = NewLineTestProgress(w)
						}
					}
				}

				if w != nil {
					if isTTY {
						// ANSI clear screen + cursor home for clean re-display.
						w.Plain("\033[2J\033[H")
					}
					w.Infof("[watch] %s — running tests...", triggerFile)
				}
			},
			OnRunComplete: func(results []soltesting.TestResult, elapsed time.Duration, runErr error) {
				if runErr != nil {
					if w != nil {
						w.Errorf("[watch] run error: %s", runErr)
					}
					return
				}

				if len(results) == 0 {
					if w != nil {
						w.Info("[watch] no tests found")
					}
					return
				}

				// Report results.
				format, _ := kvx.ParseOutputFormat(opts.Output)
				outputOpts := kvx.NewOutputOptions(opts.IOStreams)
				outputOpts.Format = format
				outputOpts.Ctx = ctx

				// When stdout is not a terminal, include per-test rows
				// in the report even if progress was shown on stderr.
				watchReportProgress := runner.Progress
				if !kvx.IsTerminal(opts.IOStreams.Out) {
					watchReportProgress = nil
				}

				if reportErr := soltesting.ReportResults(results, outputOpts, opts.Verbose, elapsed, watchReportProgress); reportErr != nil {
					if w != nil {
						w.Errorf("[watch] reporting failed: %s", reportErr)
					}
				}

				summary := soltesting.Summarize(results)
				if w != nil {
					w.Infof("[watch] waiting for file changes... (Ctrl-C to exit)")
					_ = summary // summary already printed by ReportResults
				}
			},
		},
	}

	if w != nil {
		w.Infof("[watch] watching %s for changes...", testsPath)
	}

	err = watcher.Watch(ctx)
	if err != nil && ctx.Err() != nil {
		// Context cancelled via signal — this is a clean exit.
		if w != nil {
			w.Plainln("")
			w.Info("[watch] stopped")
		}
		return nil
	}
	return err
}

// fileExists returns true if the given path exists on the local filesystem.
// Returns false only for non-existence; other errors (e.g. permission) are
// treated as existence to avoid misrouting inaccessible local paths.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return !os.IsNotExist(err)
}
