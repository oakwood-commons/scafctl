// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/dryrun"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// ValidOutputTypes defines the supported output formats
var ValidOutputTypes = kvx.BaseOutputFormats()

// SolutionOptions holds configuration for the run solution command
type SolutionOptions struct {
	sharedResolverOptions

	// Action execution options
	ActionTimeout        time.Duration
	MaxActionConcurrency int
	DryRun               bool

	// Verbose includes MaterializedInputs in dry-run reports.
	Verbose bool

	// ShowExecution enables __execution metadata in output
	ShowExecution bool

	// OnConflict is the default conflict strategy for file writes.
	// When set, it is injected into the execution context so file providers
	// use it as their default instead of the built-in "skip-unchanged".
	OnConflict string

	// Backup enables .bak backup creation before mutating existing files.
	Backup bool
}

// CommandSolution creates the 'run solution' subcommand
func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &SolutionOptions{}

	cfg := runCommandConfig{
		cliParams: cliParams,
		ioStreams: ioStreams,
		path:      path,
		runner:    options,
		getOutputFn: func() string {
			return options.Output
		},
		setIOStreamFn: func(ios *terminal.IOStreams, cli *settings.Run) {
			options.IOStreams = ios
			options.CliParams = cli
		},
	}

	cCmd := &cobra.Command{
		Use:     "solution [name[@version]]",
		Aliases: []string{"sol", "s", "solutions"},
		Short:   "Run a solution by executing resolvers and actions",
		Long: `Execute a solution by running resolvers and then actions in dependency order.

Solutions can be loaded from:
- Local catalog: Use the solution name (e.g., "my-app" or "my-app@1.2.3")
- Local file: Use -f flag or provide a path with separators (e.g., "./solution.yaml")
- URL: Use -f flag with an HTTP(S) URL
- Auto-discovery: If no source is specified, searches for solution.yaml in current directory

The solution MUST define a workflow with actions. If no workflow is defined,
the command will error and suggest using 'scafctl run resolver' instead.

The execution proceeds in two phases:
1. RESOLVER PHASE: All resolvers execute in dependency order (concurrent within phases)
2. ACTION PHASE: Actions execute using resolved data

To execute resolvers only without actions (for debugging/inspection), use:
  scafctl run resolver

RESOLVER PARAMETERS:
  Parameters can be passed using -r/--resolver flag in several formats:
    key=value         Simple key-value pair
    @file.yaml        Load parameters from YAML file  
    @file.json        Load parameters from JSON file
    key=val1,val2     Multiple values become an array

EXECUTION ORDER:
  1. Parse and validate solution (must have workflow)
  2. Execute resolvers in dependency phases (concurrent within phases)
  3. Build action graph and execute actions
  4. Finally section actions always execute (even if main actions fail)

OUTPUT FORMATS:
  table    Bordered table view (default when terminal)
  json     JSON output (for piping/scripting)
  yaml     YAML output (for piping/scripting)
  quiet    Suppress output (exit code only)

INTERACTIVE MODE:
  Use -i/--interactive to launch a TUI for exploring results:
  - Navigate with arrow keys
  - Search across keys and values
  - Filter with CEL expressions
  - Copy values and paths

CEL EXPRESSIONS:
  Use -e/--expression to filter or transform the output:
    -e '_.database'                    Select specific resolver result
    -e '_.items.filter(x, x.enabled)'  Filter arrays
    -e 'size(_.results)'               Compute values

EXIT CODES:
  0  Success
  1  Resolver execution failed
  2  Validation failed
  3  Invalid solution (no workflow, cycle, or parse error)
  4  File not found
  6  Action/workflow execution failed

Examples:
  # Run solution from catalog by name (latest version)
  scafctl run solution my-app

  # Run specific version from catalog
  scafctl run solution my-app@1.2.3

  # Run solution from file
  scafctl run solution -f ./my-solution.yaml

  # Run with parameters
  scafctl run solution -r env=prod -r region=us-east1

  # Dry run (validate and show what would execute)
  scafctl run solution -f ./my-solution.yaml --dry-run

  # Explore resolver results interactively
  scafctl run solution -f ./my-solution.yaml -i

  # JSON output for piping
  scafctl run solution -f ./my-solution.yaml -o json | jq .

  # Limit concurrent actions
  scafctl run solution --max-action-concurrency=2

  # Show progress during execution
  scafctl run solution --progress

  # Include resolver execution metadata in output
  scafctl run solution --show-execution -f ./my-solution.yaml -o json`,
		Args: cobra.MaximumNArgs(1),
		PreRun: func(cCmd *cobra.Command, args []string) {
			// Track which flags were explicitly set by the user
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
			// If a positional argument is provided (solution name), use it as the file
			// unless -f/--file was explicitly set
			if len(args) > 0 && options.File == "" {
				options.File = args[0]
			}
		},
		RunE:         makeRunEFunc(cfg, "solution"),
		SilenceUsage: true,
	}

	// Shared resolver flags
	addSharedResolverFlags(cCmd, &options.sharedResolverOptions)

	// Action execution flags
	cCmd.Flags().DurationVar(&options.ActionTimeout, "action-timeout", settings.DefaultActionTimeout, "Default timeout per action")
	cCmd.Flags().IntVar(&options.MaxActionConcurrency, "max-action-concurrency", 0, "Maximum concurrent actions (0=unlimited)")
	cCmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Validate and show what would be executed without running")
	cCmd.Flags().BoolVar(&options.Verbose, "verbose", false, "When combined with --dry-run, include materialized inputs in report")
	cCmd.Flags().BoolVar(&options.ShowExecution, "show-execution", false, "Include __execution metadata in output (phases, timing, dependencies, providers)")

	// File conflict strategy flags
	cCmd.Flags().StringVar(&options.OnConflict, "on-conflict", "", "Conflict strategy for file writes (error|overwrite|skip|skip-unchanged|append)")
	cCmd.Flags().BoolVar(&options.Backup, "backup", false, "Create .bak backups before mutating existing files")

	return cCmd
}

// getEffectiveActionConfig returns action config values, using app config
// as defaults when CLI flags weren't explicitly set.
func (o *SolutionOptions) getEffectiveActionConfig(ctx context.Context) config.ActionConfigValues {
	// Start with CLI flag values
	result := config.ActionConfigValues{
		DefaultTimeout: o.ActionTimeout,
		GracePeriod:    settings.DefaultGracePeriod, // Not a CLI flag
		MaxConcurrency: o.MaxActionConcurrency,
	}

	// If config is available, use its values for non-changed flags
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return result
	}

	// Parse config values
	configValues, err := cfg.Action.ToActionValues()
	if err != nil {
		lgr := logger.FromContext(ctx)
		lgr.V(1).Info("failed to parse action config, using CLI defaults", "error", err)
		return result
	}

	// Override with config values for flags that weren't explicitly set.
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

	// GracePeriod always comes from config (no CLI flag)
	result.GracePeriod = configValues.GracePeriod

	return result
}

// Run executes the solution
func (o *SolutionOptions) Run(ctx context.Context) error {
	lgr := logger.FromContext(ctx)

	// Apply config default for output-dir when the CLI flag wasn't explicitly set
	if o.flagsChanged != nil && !o.flagsChanged["output-dir"] {
		actionCfg := o.getEffectiveActionConfig(ctx)
		if actionCfg.OutputDir != "" {
			o.OutputDir = actionCfg.OutputDir
		}
	}

	// Validate --on-conflict flag value if provided
	if o.OnConflict != "" {
		if !fileprovider.ConflictStrategy(o.OnConflict).IsValid() {
			return o.exitWithCode(ctx, fmt.Errorf("invalid --on-conflict value %q (valid: error, overwrite, skip, skip-unchanged, append)", o.OnConflict), exitcode.InvalidInput)
		}
	}

	lgr.V(1).Info("running solution",
		"file", o.File,
		"output", o.Output,
		"resolveAll", o.ResolveAll,
		"progress", o.Progress,
		"dryRun", o.DryRun,
		"showMetrics", o.ShowMetrics,
		"outputDir", o.OutputDir,
		"onConflict", o.OnConflict,
		"backup", o.Backup)

	// Validate and prepare output directory before execution (fail-fast).
	// In dry-run mode, resolve the path without creating the directory.
	absOutputDir, err := o.resolveOutputDir(ctx, o.DryRun)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}

	// Capture the original working directory before prepareSolutionForExecution,
	// which may os.Chdir into a bundle extraction directory. This ensures __cwd
	// in action expressions reflects the user's actual working directory.
	originalCwd, err := provider.GetWorkingDirectory(ctx)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to get working directory: %w", err), exitcode.GeneralError)
	}

	// Prepare solution: load, set up registry, handle bundles
	sol, reg, cleanup, err := o.prepareSolutionForExecution(ctx)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.FileNotFound)
	}
	defer cleanup()

	actionAdapter := &actionRegistryAdapter{registry: reg}

	// Require a workflow — run solution is for executing actions.
	// Use 'scafctl run resolver' for resolver-only solutions.
	if !sol.Spec.HasWorkflow() {
		return o.exitWithCode(ctx,
			fmt.Errorf("solution %q has no workflow defined; use 'scafctl run resolver' to execute resolvers without actions", sol.Metadata.Name),
			exitcode.InvalidInput)
	}

	// Validate the workflow
	if err := action.ValidateWorkflow(sol.Spec.Workflow, actionAdapter); err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("workflow validation failed: %w", err), exitcode.ValidationFailed)
	}

	// Parse resolver parameters
	params, err := flags.ParseResolverFlags(o.ResolverParams)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to parse resolver parameters: %w", err), exitcode.ValidationFailed)
	}

	lgr.V(1).Info("parsed parameters", "count", len(params))

	// Validate parameter keys against parameter provider 'key' inputs (early typo detection)
	if len(params) > 0 {
		paramKeys := extractParameterKeys(sol.Spec.ResolversToSlice())
		if len(paramKeys) > 0 {
			if err := flags.ValidateInputKeys(params, paramKeys, "solution"); err != nil {
				return o.exitWithCode(ctx, err, exitcode.InvalidInput)
			}
		}
	}

	// Inject CLI overrides into context before dry-run or live execution,
	// so both paths honour --output-dir, --on-conflict, and --backup.
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

	// Ensure actions resolve relative paths against the caller's original working
	// directory rather than the process CWD, which may have been changed to a
	// temporary bundle extraction directory for catalog solutions. This aligns
	// catalog runs with local -f behaviour: files land in the caller's CWD unless
	// --output-dir is explicitly specified (which takes precedence in ResolvePath).
	actionCtx = provider.WithWorkingDirectory(actionCtx, originalCwd)

	// Dry run — execute resolvers in dry-run mode and show structured report
	if o.DryRun {
		return o.executeDryRun(actionCtx, sol, reg, params)
	}

	// Warn if --verbose is used without --dry-run
	if o.Verbose {
		w := writer.FromContext(ctx)
		if w != nil {
			w.Warningf("--verbose has no effect without --dry-run")
		}
	}

	// Execute resolvers if present
	resolvers := sol.Spec.ResolversToSlice()

	// Track timing for execution metadata
	start := time.Now()

	resolverData, resolverCtx, err := o.executeResolvers(ctx, sol, resolvers, params, reg)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.GeneralError)
	}

	resolverElapsed := time.Since(start)

	// Build action graph
	graph, err := action.BuildGraph(ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to build action graph: %w", err), exitcode.InvalidInput)
	}

	lgr.V(1).Info("action graph built",
		"totalActions", len(graph.Actions),
		"mainPhases", len(graph.ExecutionOrder),
		"finallyPhases", len(graph.FinallyOrder))

	// Set up action progress callback if enabled
	var actionProgressCallback action.ProgressCallback
	if o.Progress {
		actionProgressCallback = NewActionProgressCallback(writer.FromContext(ctx))
	}

	// Get effective action config (CLI flags override app config)
	actionCfg := o.getEffectiveActionConfig(ctx)

	actionExecutor := action.NewExecutor(
		action.WithRegistry(actionAdapter),
		action.WithResolverData(resolverData),
		action.WithProgressCallback(actionProgressCallback),
		action.WithDefaultTimeout(actionCfg.DefaultTimeout),
		action.WithGracePeriod(actionCfg.GracePeriod),
		action.WithMaxConcurrency(actionCfg.MaxConcurrency),
		action.WithIOStreams(o.getActionIOStreams()),
		action.WithCwd(originalCwd),
	)

	result, err := actionExecutor.Execute(actionCtx, sol.Spec.Workflow)
	if err != nil && result != nil && result.FinalStatus != action.ExecutionPartialSuccess {
		return o.exitWithCode(ctx, fmt.Errorf("action execution failed: %w", err), exitcode.ActionFailed)
	}

	// Build and write output
	var executionData map[string]any
	if o.ShowExecution {
		executionData = execute.BuildExecutionData(resolverCtx, resolvers, resolverElapsed)
	}
	return o.writeActionOutput(ctx, result, executionData)
}

// executeDryRun executes resolvers normally (they are side-effect-free) and
// produces a structured WhatIf report showing what actions would do.
func (o *SolutionOptions) executeDryRun(ctx context.Context, sol *solution.Solution, reg *provider.Registry, params map[string]any) error {
	// Execute resolvers normally — resolver providers are side-effect-free,
	// so we get real data for WhatIf message generation.
	var resolverData map[string]any
	if sol.Spec.HasResolvers() {
		cfg := ResolverExecutionConfigFromContext(ctx)
		result, err := ExecuteResolvers(ctx, sol, params, reg, cfg)
		if err != nil {
			// Non-fatal — report will include warnings
			resolverData = make(map[string]any)
		} else {
			resolverData = result.Data
		}
	}

	report, err := dryrun.Generate(ctx, sol, dryrun.Options{
		Registry:     reg,
		ResolverData: resolverData,
		Verbose:      o.Verbose,
	})
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("dry-run failed: %w", err), exitcode.GeneralError)
	}

	return o.writeDryRunOutput(ctx, report)
}

// writeDryRunOutput renders a dry-run report in the requested output format.
func (o *SolutionOptions) writeDryRunOutput(ctx context.Context, report *dryrun.Report) error {
	w := writer.FromContext(ctx)
	switch o.Output {
	case "json":
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal dry-run report: %w", err)
		}
		if w != nil {
			w.Plainln(string(data))
		}
		return nil
	case "yaml":
		data, err := yaml.Marshal(report)
		if err != nil {
			return fmt.Errorf("failed to marshal dry-run report: %w", err)
		}
		if w != nil {
			w.Plain(string(data))
		}
		return nil
	case "quiet":
		return nil
	default:
		return o.writeDryRunTable(ctx, report)
	}
}

// writeDryRunTable renders a human-readable WhatIf dry-run report to the terminal.
func (o *SolutionOptions) writeDryRunTable(ctx context.Context, report *dryrun.Report) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}

	// Header
	version := ""
	if report.Version != "" {
		version = fmt.Sprintf(" (v%s)", report.Version)
	}
	w.Plainlnf("=== DRY RUN: What would happen ===")
	w.Plainln("")
	w.Plainlnf("Solution: %s%s", report.Solution, version)

	// Action plan grouped by phase
	if len(report.ActionPlan) > 0 {
		w.Plainln("")
		currentPhase := -1
		for _, act := range report.ActionPlan {
			if act.Phase != currentPhase {
				currentPhase = act.Phase
				w.Plainlnf("Phase %d:", currentPhase)
			}
			w.Plainlnf("  What if: [%s] %s", act.Name, act.WhatIf)
			if act.When != "" {
				w.Plainlnf("    (when: %s)", act.When)
			}
			if len(act.Dependencies) > 0 {
				w.Plainlnf("    (depends on: %s)", strings.Join(act.Dependencies, ", "))
			}
			if len(act.CrossSectionRefs) > 0 {
				w.Plainlnf("    (reads from: %s)", strings.Join(act.CrossSectionRefs, ", "))
			}
			if len(act.DeferredInputs) > 0 {
				for k, v := range act.DeferredInputs {
					w.Plainlnf("    (deferred: %s = %s)", k, v)
				}
			}
			if len(act.MaterializedInputs) > 0 {
				w.Plainlnf("    Inputs:")
				for k, v := range act.MaterializedInputs {
					w.Plainlnf("      %s: %v", k, v)
				}
			}
		}
	} else if !report.HasWorkflow {
		w.Plainln("")
		w.Plainln("No workflow defined.")
	}

	// Warnings
	if len(report.Warnings) > 0 {
		w.Plainln("")
		w.Plainln("Warnings:")
		for _, wn := range report.Warnings {
			w.Plainlnf("  - %s", wn)
		}
	}

	return nil
}

// getActionIOStreams returns the provider IO streams for action execution.
// For the default/table output format, IO streams are provided so providers can
// stream output directly to the terminal. For json/yaml/quiet/test, no streams are
// provided so all output is captured and serialized.
func (o *SolutionOptions) getActionIOStreams() *provider.IOStreams {
	switch o.Output {
	case "json", "yaml", "quiet", "test":
		// Structured output modes: don't stream, capture everything for serialization
		return nil
	default:
		// Default/table: stream output directly to terminal
		return &provider.IOStreams{
			Out:    o.IOStreams.Out,
			ErrOut: o.IOStreams.ErrOut,
		}
	}
}

// resolveOutputDir validates and prepares the output directory.
// Returns the absolute path if --output-dir was set, or empty string if not.
// When dryRun is false, creates the directory if it doesn't exist.
// When dryRun is true, only resolves to an absolute path without creating it.
func (o *SolutionOptions) resolveOutputDir(ctx context.Context, dryRun bool) (string, error) {
	if o.OutputDir == "" {
		return "", nil
	}

	absDir, err := provider.AbsFromContext(ctx, o.OutputDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve output directory %q: %w", o.OutputDir, err)
	}

	if !dryRun {
		if err := os.MkdirAll(absDir, 0o755); err != nil {
			return "", fmt.Errorf("failed to create output directory %q: %w", absDir, err)
		}
	}

	return absDir, nil
}

// writeActionOutput writes the action execution results.
// For the default/table output format, output is minimal since providers that support
// streaming (e.g., exec) already wrote their output directly to the terminal.
// For json/yaml, the full execution envelope is serialized.
func (o *SolutionOptions) writeActionOutput(ctx context.Context, result *action.ExecutionResult, executionData map[string]any) error {
	if o.Output == "quiet" {
		return nil
	}

	switch o.Output {
	case "auto", "table", "list", "":
		return o.writeActionOutputDefault(ctx, result)
	case "json":
		return o.writeActionOutputStructured(ctx, result, executionData, "json")
	case "yaml":
		return o.writeActionOutputStructured(ctx, result, executionData, "yaml")
	case "test":
		return o.writeActionTestOutput(ctx, result, executionData)
	default:
		return fmt.Errorf("unsupported output format: %s", o.Output)
	}
}

// writeActionOutputDefault writes the default/table output for action execution.
// Actions that already streamed to the terminal are skipped. For non-streamed actions,
// any stdout/stderr from the results is printed. Failed/skipped actions show a status line.
func (o *SolutionOptions) writeActionOutputDefault(ctx context.Context, result *action.ExecutionResult) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}
	for name, ar := range result.Actions {
		// Skip actions that already streamed their output to the terminal
		if ar.Streamed {
			// If the action failed despite streaming, show the error
			if ar.Status == action.StatusFailed || ar.Status == action.StatusTimeout {
				w.Errorf("Error [%s]: %s", name, ar.Error)
			}
			continue
		}

		// For non-streamed actions, show results based on status
		switch ar.Status {
		case action.StatusSucceeded:
			// Print stdout if available in results
			if results, ok := ar.Results.(map[string]any); ok {
				if stdout, ok := results["stdout"].(string); ok && stdout != "" {
					w.Plain(stdout)
				}
			}
		case action.StatusFailed, action.StatusTimeout:
			// Show stderr if available, then error
			if results, ok := ar.Results.(map[string]any); ok {
				if stderr, ok := results["stderr"].(string); ok && stderr != "" {
					w.WarnStderrf("%s", stderr)
				}
			}
			w.Errorf("Error [%s]: %s", name, ar.Error)
		case action.StatusSkipped:
			w.WarnStderrf("Skipped [%s]: %s", name, ar.SkipReason)
		case action.StatusPending, action.StatusRunning, action.StatusCancelled:
			// These statuses should not appear in final results; ignore.
		}
	}

	return nil
}

// writeActionOutputStructured writes action results as JSON or YAML (the full execution envelope).
func (o *SolutionOptions) writeActionOutputStructured(ctx context.Context, result *action.ExecutionResult, executionData map[string]any, format string) error {
	outputData := action.BuildOutputData(result, executionData)

	var data []byte
	var marshalErr error

	switch format {
	case "yaml":
		data, marshalErr = yaml.Marshal(outputData)
	case "json":
		data, marshalErr = json.MarshalIndent(outputData, "", "  ")
	}

	if marshalErr != nil {
		return fmt.Errorf("failed to marshal output: %w", marshalErr)
	}

	if w := writer.FromContext(ctx); w != nil {
		w.Plainln(string(data))
	}
	return nil
}

// writeActionTestOutput generates a functional test definition from the action execution
// result and writes test YAML to stdout. A snapshot golden file is written to testdata/.
func (o *SolutionOptions) writeActionTestOutput(ctx context.Context, result *action.ExecutionResult, executionData map[string]any) error {
	// Full output (including __execution) for the snapshot.
	fullData := action.BuildOutputData(result, executionData)

	// Assertion data excludes __execution to avoid volatile timing assertions.
	assertionData := action.BuildOutputData(result, nil)

	rawJSON, err := json.MarshalIndent(fullData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal output for test generation: %w", err)
	}

	testArgs := make([]string, 0, len(o.ResolverParams)*2)
	for _, param := range o.ResolverParams {
		testArgs = append(testArgs, "-r", param)
	}

	snapshotDir := "testdata"
	if o.File != "" && o.File != "-" {
		snapshotDir = filepath.Join(filepath.Dir(o.File), "testdata")
	}

	genResult, err := soltesting.Generate(&soltesting.GenerateInput{
		Command:     []string{"run", "solution"},
		Args:        testArgs,
		TestName:    o.TestName,
		SnapshotDir: snapshotDir,
		Data:        assertionData,
		RawJSON:     rawJSON,
	})
	if err != nil {
		return fmt.Errorf("failed to generate test: %w", err)
	}

	yamlData, err := soltesting.GenerateToYAML(genResult)
	if err != nil {
		return fmt.Errorf("failed to marshal test YAML: %w", err)
	}

	if w := writer.FromContext(ctx); w != nil {
		w.Plain(string(yamlData))
		if genResult.SnapshotWritten {
			w.WarnStderrf("Snapshot written: %s", genResult.SnapshotPath)
		}
	}
	return nil
}

// actionRegistryAdapter adapts provider.Registry to action.RegistryInterface
type actionRegistryAdapter struct {
	registry *provider.Registry
}

// Get returns a provider by name (for action.RegistryInterface - returns bool)
func (r *actionRegistryAdapter) Get(name string) (provider.Provider, bool) {
	return r.registry.Get(name)
}

// Has checks if a provider exists (for action.RegistryInterface)
func (r *actionRegistryAdapter) Has(name string) bool {
	_, ok := r.registry.Get(name)
	return ok
}

// ActionProgressCallback implements action.ProgressCallback for CLI output
type ActionProgressCallback struct {
	w *writer.Writer
}

// NewActionProgressCallback creates a new action progress callback
func NewActionProgressCallback(w *writer.Writer) *ActionProgressCallback {
	return &ActionProgressCallback{w: w}
}

func (a *ActionProgressCallback) OnActionStart(actionName string) {
	a.w.Infof("[ACTION] Starting: %s", actionName)
}

func (a *ActionProgressCallback) OnActionComplete(actionName string, _ any) {
	a.w.Successf("[ACTION] Completed: %s ✓", actionName)
}

func (a *ActionProgressCallback) OnActionFailed(actionName string, err error) {
	a.w.Errorf("[ACTION] Failed: %s ✗ (%v)", actionName, err)
}

func (a *ActionProgressCallback) OnActionSkipped(actionName, reason string) {
	a.w.Warningf("[ACTION] Skipped: %s (%s)", actionName, reason)
}

func (a *ActionProgressCallback) OnActionTimeout(actionName string, timeout time.Duration) {
	a.w.Errorf("[ACTION] Timeout: %s (after %v)", actionName, timeout)
}

func (a *ActionProgressCallback) OnActionCancelled(actionName string) {
	a.w.Warningf("[ACTION] Cancelled: %s", actionName)
}

func (a *ActionProgressCallback) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	a.w.Warningf("[ACTION] Retry %d/%d for %s: %v", attempt, maxAttempts, actionName, err)
}

func (a *ActionProgressCallback) OnForEachProgress(actionName string, completed, total int) {
	a.w.Infof("[ACTION] %s: %d/%d iterations complete", actionName, completed, total)
}

func (a *ActionProgressCallback) OnPhaseStart(phase int, actionNames []string) {
	a.w.Infof("[PHASE] Starting phase %d: %s", phase, strings.Join(actionNames, ", "))
}

func (a *ActionProgressCallback) OnPhaseComplete(phase int) {
	a.w.Successf("[PHASE] Completed phase %d", phase)
}

func (a *ActionProgressCallback) OnFinallyStart() {
	a.w.Infof("[FINALLY] Starting finally section")
}

func (a *ActionProgressCallback) OnFinallyComplete() {
	a.w.Successf("[FINALLY] Completed finally section")
}
