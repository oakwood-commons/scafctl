// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	pkgfilepath "github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ActionOptions holds configuration for the run action command.
type ActionOptions struct {
	BinaryName string
	sharedResolverOptions

	// Names is the list of action names to execute (positional args).
	// Only the specified actions and their transitive dependsOn dependencies
	// are executed. Finally actions always run.
	Names []string

	// Action execution options
	ActionTimeout        time.Duration
	MaxActionConcurrency int
	DryRun               bool
	Verbose              bool
	ShowExecution        bool

	// File conflict strategy
	OnConflict string
	Force      bool
	Backup     bool

	// DynamicArgs are resolver parameters from positional key=value syntax.
	DynamicArgs []string
}

// CommandAction creates the 'run action' subcommand.
func CommandAction(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &ActionOptions{}

	cfg := runCommandConfig{
		cliParams: cliParams,
		ioStreams: ioStreams,
		path:      path,
		runner:    options,
		getOutputFn: func() string {
			return options.Output
		},
		setIOStreamFn: func(ios *terminal.IOStreams, cli *settings.Run) {
			options.BinaryName = cli.BinaryName
			options.IOStreams = ios
			options.CliParams = cli
			options.Verbose = cli.Verbose
		},
	}

	cCmd := &cobra.Command{
		Use:     "action [action-name...] [key=value...]",
		Aliases: []string{"act", "a", "actions"},
		Short:   "Run specific actions from a solution by name",
		Long: strings.ReplaceAll(`Execute specific actions from a solution's workflow by name.

This command loads a solution, runs its resolvers, then executes only the
selected actions and their transitive dependsOn dependencies. Finally actions
always run regardless of the filter.

This is equivalent to 'scafctl run solution --action <name>' but with a more
ergonomic CLI: action names are positional arguments, matching the pattern
used by 'scafctl run resolver'.

SOLUTION SOURCE:
  Solutions can be loaded from:
  - Local file or catalog: Use -f flag (e.g., -f ./solution.yaml or -f my-app@1.2.3)
  - URL: Provide an HTTP(S) URL via -f/--file (also detected as a positional arg)
  - Auto-discovery: If no source is specified, searches for solution.yaml in current directory

ACTION SELECTION:
  Pass action names as positional arguments to execute only those actions
  and their transitive dependsOn dependencies. When no names are provided,
  all actions in the workflow are executed (equivalent to 'scafctl run solution').

  Finally actions always execute regardless of the filter.

`+ResolverParametersHelp+`

EXIT CODES:
  0  Success
  1  Resolver execution failed
  2  Validation failed
  3  Invalid solution (no workflow, cycle, or parse error)
  4  File not found
  6  Action/workflow execution failed

Examples:
  # Run the 'lint' action (auto-discovery)
  scafctl run action lint

  # Run multiple actions and their deps
  scafctl run action lint test

  # Run from a specific file
  scafctl run action lint -f ./taskfile.yaml

  # Run from catalog
  scafctl run action lint -f my-app

  # Run with resolver parameters
  scafctl run action build -f ./solution.yaml version=1.0.0

  # Dry run
  scafctl run action lint --dry-run`, settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.ArbitraryArgs,
		PreRun: func(cCmd *cobra.Command, args []string) {
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
			// Split positional args: bare words are action names,
			// args containing '=' or starting with '@' are parameters.
			// Unlike run solution/resolver, bare names are always treated as action
			// names -- use -f/--file for solution file or catalog references.
			// Only URLs (http(s)://, oci://) are auto-detected as solution refs.
			fileExplicit := options.flagsChanged["file"]
			for _, arg := range args {
				switch {
				case !fileExplicit && options.File == "" && pkgfilepath.IsURL(arg):
					options.File = arg
					fileExplicit = true
				case strings.Contains(arg, "=") || strings.HasPrefix(arg, "@"):
					options.DynamicArgs = append(options.DynamicArgs, arg)
				default:
					options.Names = append(options.Names, arg)
				}
			}
		},
		RunE:         makeRunEFunc(cfg, "action"),
		SilenceUsage: true,
	}

	// Shared resolver flags
	addSharedResolverFlags(cCmd, &options.sharedResolverOptions)

	// Action execution flags
	cCmd.Flags().DurationVar(&options.ActionTimeout, "action-timeout", settings.DefaultActionTimeout, "Default timeout per action")
	cCmd.Flags().IntVar(&options.MaxActionConcurrency, "max-action-concurrency", 0, "Maximum concurrent actions (0=unlimited)")
	cCmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Validate and show what would be executed without running")
	cCmd.Flags().BoolVar(&options.ShowExecution, "show-execution", false, "Include __execution metadata in output")

	// File conflict strategy flags
	cCmd.Flags().StringVar(&options.OnConflict, "on-conflict", "", "Conflict strategy for file writes (error|overwrite|skip|skip-unchanged|append) (default: error)")
	cCmd.Flags().BoolVar(&options.Force, "force", false, "Overwrite existing files (shorthand for --on-conflict skip-unchanged)")
	cCmd.Flags().BoolVar(&options.Backup, "backup", false, "Create .bak backups before mutating existing files")

	return cCmd
}

// getEffectiveActionConfig returns action config values, using app config
// as defaults when CLI flags weren't explicitly set.
func (o *ActionOptions) getEffectiveActionConfig(ctx context.Context) config.ActionConfigValues {
	result := config.ActionConfigValues{
		DefaultTimeout: o.ActionTimeout,
		GracePeriod:    settings.DefaultGracePeriod,
		MaxConcurrency: o.MaxActionConcurrency,
	}

	cfg := config.FromContext(ctx)
	if cfg == nil {
		return result
	}

	configValues, err := cfg.Action.ToActionValues()
	if err != nil {
		lgr := logger.FromContext(ctx)
		lgr.V(1).Info("failed to parse action config, using CLI defaults", "error", err)
		return result
	}

	if o.flagsChanged != nil {
		if !o.flagsChanged["action-timeout"] {
			result.DefaultTimeout = configValues.DefaultTimeout
		}
		if !o.flagsChanged["max-action-concurrency"] {
			result.MaxConcurrency = configValues.MaxConcurrency
		}
		if !o.flagsChanged["output-dir"] {
			result.OutputDir = configValues.OutputDir
		}
	}

	result.GracePeriod = configValues.GracePeriod
	return result
}

// Run executes the selected actions from the solution.
func (o *ActionOptions) Run(ctx context.Context) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

	lgr := logger.FromContext(ctx)

	// Apply config default for output-dir
	if o.flagsChanged != nil && !o.flagsChanged["output-dir"] {
		actionCfg := o.getEffectiveActionConfig(ctx)
		if actionCfg.OutputDir != "" {
			o.OutputDir = actionCfg.OutputDir
		}
	}

	// --force shorthand
	if o.Force {
		o.OnConflict = "skip-unchanged"
	}

	if o.OnConflict != "" {
		if !fileprovider.ConflictStrategy(o.OnConflict).IsValid() {
			return o.exitWithCode(ctx, fmt.Errorf("invalid --on-conflict value %q (valid: error, overwrite, skip, skip-unchanged, append)", o.OnConflict), exitcode.InvalidInput)
		}
	}

	lgr.V(1).Info("running action",
		"file", o.File,
		"names", o.Names,
		"dryRun", o.DryRun)

	absOutputDir, err := o.resolveOutputDir(ctx, o.DryRun)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}

	// Detect @- / -f - conflict early
	if o.File == "-" && flags.ContainsStdinRef(o.ResolverParams) {
		return o.exitWithCode(ctx,
			fmt.Errorf("cannot use both -f - and @-: stdin can only be read once"),
			exitcode.InvalidInput)
	}

	originalCwd, err := provider.GetWorkingDirectory(ctx)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to get working directory: %w", err), exitcode.GeneralError)
	}

	sol, reg, cleanup, err := o.prepareSolutionForExecution(ctx)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.FileNotFound)
	}
	defer cleanup()

	// Set the solution directory for path resolution.
	// Only applied when --base-dir is explicitly provided.
	if o.BaseDir != "" {
		absBaseDir, baseDirErr := filepath.Abs(o.BaseDir)
		if baseDirErr != nil {
			return o.exitWithCode(ctx, fmt.Errorf("--base-dir: %w", baseDirErr), exitcode.InvalidInput)
		}
		ctx = provider.WithSolutionDirectory(ctx, absBaseDir)
	}

	actionAdapter := &actionRegistryAdapter{registry: reg}

	if !sol.Spec.HasWorkflow() {
		return o.exitWithCode(ctx,
			fmt.Errorf("solution %q has no workflow defined; use '%s run resolver' to execute resolvers without actions", sol.Metadata.Name, o.BinaryName),
			exitcode.InvalidInput)
	}

	if err := action.ValidateWorkflow(sol.Spec.Workflow, actionAdapter); err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("workflow validation failed: %w", err), exitcode.ValidationFailed)
	}

	// Apply action filter
	workflow := sol.Spec.Workflow
	if len(o.Names) > 0 {
		filtered, filterErr := action.FilterWorkflowActions(workflow, o.Names)
		if filterErr != nil {
			return o.exitWithCode(ctx, filterErr, exitcode.InvalidInput)
		}
		workflow = filtered
		lgr.V(1).Info("action filter applied", "targets", o.Names, "remaining", len(workflow.Actions))
	}

	// Parse resolver parameters
	allParams := make([]string, 0, len(o.ResolverParams)+len(o.DynamicArgs))
	allParams = append(allParams, o.ResolverParams...)
	allParams = append(allParams, o.DynamicArgs...)

	var stdinReader io.Reader
	if o.IOStreams != nil {
		stdinReader = o.IOStreams.In
	}
	params, err := flags.ParseResolverFlagsWithStdin(allParams, stdinReader)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to parse resolver parameters: %w", err), exitcode.ValidationFailed)
	}

	lgr.V(1).Info("parsed parameters", "count", len(params))

	// Validate parameter keys
	if len(params) > 0 {
		paramKeys := extractParameterKeys(sol.Spec.ResolversToSlice())
		if len(paramKeys) > 0 {
			if err := flags.ValidateInputKeys(params, paramKeys, "solution"); err != nil {
				return o.exitWithCode(ctx, err, exitcode.InvalidInput)
			}
		}
	}

	// Inject CLI overrides into context
	actionCtx := ctx
	if absOutputDir != "" {
		actionCtx = provider.WithOutputDirectory(actionCtx, absOutputDir)
	}
	if o.OnConflict != "" {
		actionCtx = provider.WithConflictStrategy(actionCtx, o.OnConflict)
	}
	if o.Backup {
		actionCtx = provider.WithBackup(actionCtx, true)
	}
	actionCtx = provider.WithWorkingDirectory(actionCtx, originalCwd)

	// Dry run — execute resolvers with ctx (solution-dir aware, no working-dir
	// override) so resolver paths resolve relative to the solution file.
	if o.DryRun {
		return o.executeDryRun(ctx, sol, reg, params, workflow)
	}

	// Execute resolvers
	resolvers := sol.Spec.ResolversToSlice()
	start := time.Now()
	resolverData, resolverCtx, err := o.executeResolvers(ctx, sol, resolvers, params, reg)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.GeneralError)
	}
	resolverElapsed := time.Since(start)

	// Build action graph from filtered workflow
	graph, err := action.BuildGraph(ctx, workflow, resolverData, nil)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to build action graph: %w", err), exitcode.InvalidInput)
	}

	lgr.V(1).Info("action graph built",
		"totalActions", len(graph.Actions),
		"mainPhases", len(graph.ExecutionOrder),
		"finallyPhases", len(graph.FinallyOrder))

	var actionProgressCallback action.ProgressCallback
	if o.Progress {
		actionProgressCallback = NewActionProgressCallback(writer.FromContext(ctx))
	}

	actionCfg := o.getEffectiveActionConfig(ctx)
	resolverExecutionData := execute.BuildExecutionData(resolverCtx, resolvers, resolverElapsed)

	actionExecutor := action.NewExecutor(
		action.WithRegistry(actionAdapter),
		action.WithResolverData(resolverData),
		action.WithExecutionData(resolverExecutionData),
		action.WithProgressCallback(actionProgressCallback),
		action.WithDefaultTimeout(actionCfg.DefaultTimeout),
		action.WithGracePeriod(actionCfg.GracePeriod),
		action.WithMaxConcurrency(actionCfg.MaxConcurrency),
		action.WithIOStreams(o.getActionIOStreams()),
		action.WithCwd(originalCwd),
	)

	result, err := actionExecutor.Execute(actionCtx, workflow)
	if err != nil && result != nil && result.FinalStatus != action.ExecutionPartialSuccess {
		return o.exitWithCode(ctx, fmt.Errorf("action execution failed: %w", err), exitcode.ActionFailed)
	}

	var executionData map[string]any
	if o.ShowExecution {
		executionData = resolverExecutionData
	}
	return o.writeActionOutput(ctx, result, executionData)
}

// exitWithCode logs an error and returns it wrapped with the exit code.
func (o *ActionOptions) exitWithCode(ctx context.Context, err error, code int) error {
	w := writer.FromContext(ctx)
	if w != nil {
		w.Error(err.Error())
	}
	return exitcode.WithCode(err, code)
}

// executeDryRun delegates to SolutionOptions.executeDryRun which runs
// resolvers (side-effect-free) and produces a structured WhatIf report.
func (o *ActionOptions) executeDryRun(ctx context.Context, sol *solution.Solution, reg *provider.Registry, params map[string]any, workflow *action.Workflow) error {
	s := &SolutionOptions{
		sharedResolverOptions: o.sharedResolverOptions,
		Verbose:               o.Verbose,
		ShowExecution:         o.ShowExecution,
	}
	return s.executeDryRun(ctx, sol, reg, params, workflow)
}

// resolveOutputDir validates and creates the output directory.
func (o *ActionOptions) resolveOutputDir(ctx context.Context, dryRun bool) (string, error) {
	// Delegate to SolutionOptions which owns the canonical implementation.
	s := &SolutionOptions{sharedResolverOptions: o.sharedResolverOptions}
	return s.resolveOutputDir(ctx, dryRun)
}

// writeActionOutput writes the action execution results using the shared implementation.
func (o *ActionOptions) writeActionOutput(ctx context.Context, result *action.ExecutionResult, executionData map[string]any) error {
	s := &SolutionOptions{
		sharedResolverOptions: o.sharedResolverOptions,
		Verbose:               o.Verbose,
		ShowExecution:         o.ShowExecution,
	}
	return s.writeActionOutput(ctx, result, executionData)
}

// getActionIOStreams returns an IOStreams for the action executor.
func (o *ActionOptions) getActionIOStreams() *provider.IOStreams {
	if o.IOStreams == nil {
		return nil
	}
	return &provider.IOStreams{
		Out:    o.IOStreams.Out,
		ErrOut: o.IOStreams.ErrOut,
	}
}
