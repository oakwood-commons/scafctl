// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/dryrun"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/fingerprint"
	"github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/state"
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
	BinaryName string
	sharedResolverOptions

	// Action execution options
	ActionTimeout        time.Duration
	MaxActionConcurrency int
	DryRun               bool

	// Verbose enables additional detail in output. In dry-run mode, includes
	// materialized inputs. In normal execution, shows condition-based skips.
	Verbose bool

	// ShowExecution enables __execution metadata in output
	ShowExecution bool

	// OnConflict is the default conflict strategy for file writes.
	// When set, it is injected into the execution context so file providers
	// use it as their default instead of the built-in "error".
	OnConflict string

	// Force overrides --on-conflict to "skip-unchanged", allowing re-runs of
	// solutions that produce identical files without errors.
	Force bool

	// SkipFingerprint disables fingerprint-based up-to-date checks.
	// When true, all actions execute regardless of whether sources have changed.
	SkipFingerprint bool

	// DetailedExitCode, when true, returns exitcode.PartialSuccess (12) instead
	// of 0 when the run ends in partial success (some continueOnError actions
	// failed, none failed hard). Defaults from settings.Run (embedder/config)
	// and is overridden by the --detailed-exit-code flag. When false, partial
	// success exits 0 (non-breaking).
	DetailedExitCode bool

	// Backup enables .bak backup creation before mutating existing files.
	Backup bool

	// ActionNames is the list of action names to execute selectively.
	// When set, only the specified actions and their transitive dependsOn
	// dependencies are executed. Finally actions always run.
	ActionNames []string

	// positionalPathErr is set in PreRun when the user passes a local file
	// path as a positional argument instead of using -f/--file.
	positionalPathErr error

	// validationWarn holds a non-fatal validation-only failure captured under
	// the "warn" policy. When set, its diagnostics and the resolved values
	// (validationWarnResolvers) are injected into the successful action output
	// envelope so the warning is visible in structured output. Reset per run.
	validationWarn          error
	validationWarnResolvers map[string]any
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
			options.BinaryName = cli.BinaryName
			options.IOStreams = ios
			options.CliParams = cli
			options.Verbose = cli.Verbose
		},
	}

	cCmd := &cobra.Command{
		Use:     "solution [name[@version]]",
		Aliases: []string{"sol", "s", "solutions"},
		Short:   "Run a solution by executing resolvers and actions",
		Long: strings.ReplaceAll(`Execute a solution by running resolvers and then actions in dependency order.

Solutions can be loaded from:
- Local catalog: Use the solution name as a positional argument (e.g., "my-app" or "my-app@1.2.3")
- Local file: Use -f/--file flag (e.g., -f ./solution.yaml)
- Stdin: Use -f - to read from stdin
- URL: Use an HTTP(S) URL either as a positional argument or with -f/--file
- Auto-discovery: If no source is specified, searches for solution.yaml in current directory

NOTE: Positional arguments accept catalog names, remote registry refs, and URLs.
Local file paths must use -f/--file.

The solution MUST define a workflow with actions. If no workflow is defined,
the command will error and suggest using 'scafctl run resolver' instead.

The execution proceeds in two phases:
1. RESOLVER PHASE: All resolvers execute in dependency order (concurrent within phases)
2. ACTION PHASE: Actions execute using resolved data

To execute resolvers only without actions (for debugging/inspection), use:
  scafctl run resolver

`+ResolverParametersFlagHelp+`

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

FAILURE OUTPUT (json/yaml):
  On failure the structured output still emits a parseable document so callers
  piping to jq/yq can detect and inspect the failure programmatically instead of
  receiving empty stdout. Setup and resolver-phase failures emit a
  {status: "failed", diagnostics: [...]} envelope; an action-execution failure
  emits the full action result envelope (status: "failed" with per-action error
  detail). Human formats (table/quiet) keep the prior stderr-only behavior.

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
  0   Success (also partial success unless --detailed-exit-code is set)
  1   Resolver execution failed
  2   Validation failed
  3   Invalid solution (no workflow, cycle, or parse error)
  4   File not found
  6   Action/workflow execution failed
  12  Partial success (only with --detailed-exit-code; some continueOnError actions failed)

Examples:
  # Run solution from catalog by name (latest version)
  scafctl run solution my-app

  # Run specific version from catalog
  scafctl run solution my-app@1.2.3

  # Run solution from file
  scafctl run solution -f ./my-solution.yaml

  # Run only specific actions (and their transitive dependencies)
  scafctl run solution -f ./my-solution.yaml --action lint
  scafctl run solution -f ./my-solution.yaml --action lint --action test

  # Run with parameters
  scafctl run solution -r env=prod -r region=us-east1

  # Load parameters from a YAML file
  scafctl run solution -f ./my-solution.yaml -r @params.yaml

  # Load parameters from stdin (pipe YAML or JSON)
  echo '{"env": "prod"}' | scafctl run solution -f ./my-solution.yaml -r @-

  # Pipe raw stdin into a single parameter
  echo hello | scafctl run solution -f ./my-solution.yaml -r message=@-

  # Read a file's raw content into a parameter
  scafctl run solution -f ./my-solution.yaml -r body=@content.txt

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
  scafctl run solution --show-execution -f ./my-solution.yaml -o json`, settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.MaximumNArgs(1),
		PreRun: func(cCmd *cobra.Command, args []string) {
			// Track which flags were explicitly set by the user
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
			// If a positional argument is provided, it must be a catalog/registry
			// reference. Local file paths require -f/--file. Providing both
			// -f/--file and a positional arg is also an error.
			if len(args) > 0 {
				if err := get.ValidatePositionalRef(args[0], options.File, cliParams.BinaryName+" run solution"); err != nil {
					options.positionalPathErr = err
				} else {
					options.File = args[0]
				}
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
	cCmd.Flags().BoolVar(&options.ShowExecution, "show-execution", false, "Include __execution metadata in output (phases, timing, dependencies, providers)")
	cCmd.Flags().StringSliceVar(&options.ActionNames, "action", nil, "Run only the named action(s) and their transitive dependencies (repeatable)")

	// File conflict strategy flags
	cCmd.Flags().StringVar(&options.OnConflict, "on-conflict", "", "Conflict strategy for file writes (error|overwrite|skip|skip-unchanged|append) (default: error)")
	cCmd.Flags().BoolVar(&options.Force, "force", false, "Skip unchanged files and write only new or modified content (shorthand for --on-conflict skip-unchanged)")
	cCmd.Flags().BoolVar(&options.SkipFingerprint, "skip-fingerprint", false, "Disable fingerprint-based up-to-date checks (re-run all actions)")
	cCmd.Flags().BoolVar(&options.Backup, "backup", false, "Create .bak backups before mutating existing files")

	// Detailed exit code: opt-in distinct exit code (12) on partial success.
	// Defaults from cliParams (embedder/config); the flag overrides it.
	cCmd.Flags().BoolVar(&options.DetailedExitCode, "detailed-exit-code", cliParams.DetailedExitCode, "Return a distinct exit code (12) when the run completes with partial success (some continueOnError actions failed); default off (partial success exits 0)")

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
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
	}

	// Reset per-run warn state so a previous non-fatal validation run cannot
	// leak diagnostics/resolvers into a later successful run when the same
	// options instance is reused in-process (e.g. an embedder re-executing).
	o.validationWarn = nil
	o.validationWarnResolvers = nil

	// Fail early if PreRun detected a local file path as positional arg
	if o.positionalPathErr != nil {
		return o.exitWithCode(ctx, o.positionalPathErr, exitcode.InvalidInput)
	}

	// Include pre-release versions in catalog resolution when --pre-release is set.
	// Must happen before resolveVersionConstraintForFile so --version constraints
	// also respect the flag.
	if o.PreRelease {
		ctx = catalog.WithIncludePreRelease(ctx)
	}

	// Resolve --version constraint before loading solution
	if err := o.resolveVersionConstraintForFile(ctx); err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}

	lgr := logger.FromContext(ctx)

	// Apply config default for output-dir when the CLI flag wasn't explicitly set
	if o.flagsChanged != nil && !o.flagsChanged["output-dir"] {
		actionCfg := o.getEffectiveActionConfig(ctx)
		if actionCfg.OutputDir != "" {
			o.OutputDir = actionCfg.OutputDir
		}
	}

	// --force overrides --on-conflict to skip-unchanged
	if o.Force {
		o.OnConflict = "skip-unchanged"
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

	// Skip action files during auto-discovery.
	o.discoveryMode = settings.DiscoveryModeSolution

	// Validate and prepare output directory before execution (fail-fast).
	// In dry-run mode, resolve the path without creating the directory.
	absOutputDir, err := o.resolveOutputDir(ctx, o.DryRun)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}

	// Detect @- / -f - conflict early: stdin can only be consumed once.
	if o.File == "-" && flags.ContainsStdinRef(o.ResolverParams) {
		return o.exitWithCode(ctx,
			fmt.Errorf("cannot use both -f - and @-: stdin can only be read once"),
			exitcode.InvalidInput)
	}

	// Capture the original working directory before prepareSolutionForExecution,
	// which may os.Chdir into a bundle extraction directory. This ensures __cwd
	// in action expressions reflects the user's actual working directory.
	originalCwd, err := provider.GetWorkingDirectory(ctx)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to get working directory: %w", err), exitcode.GeneralError)
	}

	// Prepare solution: load, set up registry, handle bundles
	sol, reg, solutionDir, cleanup, providerCtx, err := o.prepareSolutionForExecution(ctx)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.FileNotFound)
	}
	defer cleanup()
	if providerCtx != nil {
		ctx = providerCtx(ctx)
	}

	// Set the solution directory for resolver-phase path resolution.
	// --base-dir takes precedence; otherwise use the solution file's directory.
	//
	// WorkingDirectory is intentionally NOT set for the resolver-phase context.
	// AbsFromContext checks WorkingDirectory before SolutionDirectory, so setting
	// both causes WorkingDirectory to win and SolutionDirectory to be ignored.
	// The action phase sets WorkingDirectory separately via actionCtx.
	//
	// For bundle runs (solutionDir == bundleDir), only set SolutionDirectory so
	// resolver-phase paths (e.g. reading bundled files) resolve against the bundle
	// extraction directory.
	//
	// For unbundled catalog runs, solutionDir is empty and paths fall back to the
	// process CWD.
	isBundleRun := strings.HasPrefix(sol.GetPath(), "catalog:") && solutionDir != ""
	switch {
	case o.BaseDir != "":
		absBaseDir, baseDirErr := filepath.Abs(o.BaseDir)
		if baseDirErr != nil {
			return o.exitWithCode(ctx, fmt.Errorf("--base-dir: %w", baseDirErr), exitcode.InvalidInput)
		}
		ctx = provider.WithSolutionDirectory(ctx, absBaseDir)
	case isBundleRun:
		ctx = provider.WithSolutionDirectory(ctx, solutionDir)
	case solutionDir != "":
		ctx = provider.WithSolutionDirectory(ctx, solutionDir)
	}

	actionAdapter := &actionRegistryAdapter{registry: reg}

	// Require a workflow — run solution is for executing actions.
	// Use 'scafctl run resolver' for resolver-only solutions.
	if !sol.Spec.HasWorkflow() {
		return o.exitWithCode(ctx,
			fmt.Errorf("solution %q has no workflow defined; use '%s run resolver' to execute resolvers without actions", sol.Metadata.Name, o.BinaryName),
			exitcode.InvalidInput)
	}

	// Validate the workflow
	if err := action.ValidateWorkflow(sol.Spec.Workflow, actionAdapter); err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("workflow validation failed: %w", err), exitcode.ValidationFailed)
	}

	// Apply --action filter: keep only the specified actions and their transitive deps.
	// Finally actions are always preserved. Filtering happens on the original workflow
	// before graph building so that pruned actions never enter the DAG.
	workflow := sol.Spec.Workflow
	{
		filtered, filterErr := action.FilterWorkflowActions(workflow, o.ActionNames)
		if filterErr != nil {
			return o.exitWithCode(ctx, filterErr, exitcode.InvalidInput)
		}
		workflow = filtered
		if len(o.ActionNames) > 0 {
			lgr.V(1).Info("action filter applied", "targets", o.ActionNames, "remaining", len(workflow.Actions))
		}
	}

	// Parse resolver parameters (pass stdin for @- support)
	var stdinReader io.Reader
	if o.IOStreams != nil {
		stdinReader = o.IOStreams.In
	}
	params, err := flags.ParseResolverFlagsWithStdin(o.ResolverParams, stdinReader)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to parse resolver parameters: %w", err), exitcode.ValidationFailed)
	}

	lgr.V(1).Info("parsed parameters", "count", len(params))

	// Validate parameter keys against parameter provider 'key' inputs (early
	// typo detection), honoring the --on-unknown-resolver policy.
	if err := o.validateResolverParams(ctx, sol, params); err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}

	// Resolve the validation-failure policy up front so it governs both the
	// state two-phase pre-load and the main resolver execution below. run
	// solution defaults to "error" (validation failures abort the workflow);
	// "warn" continues with diagnostics, "ignore" skips validation.
	validationPolicy, err := o.resolveValidationPolicy(ctx)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
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

	// Ensure actions resolve output paths against the caller's original working
	// directory rather than the process CWD, which may have been changed to a
	// temporary bundle extraction directory for catalog solutions. This aligns
	// catalog runs with local -f behaviour: files land in the caller's CWD unless
	// --output-dir is explicitly specified (which takes precedence in ResolvePath).
	actionCtx = provider.WithWorkingDirectory(actionCtx, originalCwd)

	// State lifecycle: load persisted state before resolver execution so that
	// the state provider can serve previously saved values. State is loaded
	// even in dry-run mode (resolvers need context) but never saved.
	// params are passed so that state backend inputs can reference CLI
	// parameters via __params in CEL expressions (e.g. __params.appName).
	var stateMgr *state.Manager
	var stateData *state.Data
	var stateSeed map[string]*resolver.ExecutionResult
	if o.NoState {
		warnStateSkipped(ctx, sol)
	} else if sol.State != nil {
		stateMgr = state.NewManager(sol.State, reg, state.RuntimeProvenanceFromContext(ctx))
		cmdInfo := buildCommandInfo("run solution", params)
		loadResult, loadErr := stateMgr.LoadTwoPhase(ctx, params, cmdInfo, o.buildStateTwoPhaseInput(sol, params, reg))
		if loadErr != nil {
			return o.handleStateLoadError(ctx, loadErr)
		}
		stateSeed = loadResult.Seed
		if !loadResult.Skipped {
			ctx = loadResult.Ctx
			actionCtx = state.WithState(actionCtx, loadResult.Data)
			stateData = loadResult.Data
			params = loadResult.MergedParams
		}
	}

	// Dry run — execute resolvers with ctx (CWD by default, or base-dir when
	// --base-dir is explicitly set) so resolver paths resolve accordingly. The
	// action-phase WhatIf report uses actionCtx which has the working-dir
	// override for accurate output-dir resolution.
	if o.DryRun {
		return o.executeDryRun(ctx, actionCtx, sol, reg, params, workflow, stateData, originalCwd, stateSeed)
	}

	// Execute resolvers if present
	resolvers := sol.Spec.ResolversToSlice()

	// Track timing for execution metadata
	start := time.Now()

	resolverData, resolverCtx, err := o.executeResolvers(ctx, sol, resolvers, params, reg, withSeededResults(stateSeed))
	if err != nil {
		validationOnly := execute.IsValidationOnlyFailure(err)
		if validationPolicy == settings.ValidationWarn && validationOnly {
			// warn: surface diagnostics on stderr and continue to the workflow
			// with the partially-resolved values. The failure is embedded in
			// the final success envelope (via o.validationWarn*), not treated
			// as fatal.
			o.renderResolverDiagnostics(ctx, err)
			o.validationWarn = err
			o.validationWarnResolvers = o.buildResolverOutputMap(resolverData, sol)
		} else {
			// Fatal: resolve/transform failures under any policy, and
			// validation failures under the "error" policy. Preserve the
			// resolved values in the failure envelope instead of dropping them.
			// Validation-only failures use the dedicated ValidationFailed code.
			code := exitcode.GeneralError
			if validationOnly {
				code = exitcode.ValidationFailed
			}
			return o.failStructured(ctx, o.buildResolverOutputMap(resolverData, sol), err, code)
		}
	}

	resolverElapsed := time.Since(start)

	// State lifecycle (D1): commit immutable locks now -- after resolvers and
	// deferred validation have succeeded, but before running side-effecting
	// actions. An immutable lock represents a value that is now fixed; it must be
	// persisted independent of whether a downstream action later fails, so the
	// next run does not regenerate a different value. Resolvers whose deferred
	// validation failed are not locked. Merged parameters are saved after actions.
	if stateMgr != nil && stateData != nil {
		solMeta := buildStateSolutionMeta(sol)
		skip := deferredValidationFailures(resolverCtx)
		if saveErr := stateMgr.SaveImmutables(ctx, stateData, resolverCtx, resolvers, params, resolverData, solMeta, skip); saveErr != nil {
			return o.exitWithCode(ctx, fmt.Errorf("state save immutables: %w", saveErr), exitcode.GeneralError)
		}
	}

	// Build action graph
	graph, err := action.BuildGraph(ctx, workflow, resolverData, nil)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to build action graph: %w", err), exitcode.InvalidInput)
	}

	lgr.V(1).Info("action graph built",
		"totalActions", len(graph.Actions),
		"mainPhases", len(graph.ExecutionOrder),
		"finallyPhases", len(graph.FinallyOrder))

	// Action banners are always enabled (default-on like go-task/make); suppressed by --quiet.
	// writer.FromContext returns nil when no writer is in context (e.g. direct Run calls in tests);
	// in that case we skip the callback so the executor's nil-guard suppresses output safely.
	var actionProgressCallback action.ProgressCallback
	if w := writer.FromContext(ctx); w != nil {
		actionProgressCallback = NewActionProgressCallback(w)
	}

	// Get effective action config (CLI flags override app config)
	actionCfg := o.getEffectiveActionConfig(ctx)

	// Build execution metadata before constructing the action executor so it can be
	// injected as __execution into action when conditions and provider inputs,
	// regardless of whether --show-execution is set for resolver output display.
	resolverExecutionData := execute.BuildExecutionData(resolverCtx, resolvers, resolverElapsed)

	funcBinder, funcBinderErr := sol.TemplateFuncBinder()
	if funcBinderErr != nil {
		return fmt.Errorf("compiling spec.functions: %w", funcBinderErr)
	}

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
		action.WithFingerprintChecker(fingerprint.NewChecker(stateData)),
		action.WithNoCache(o.SkipFingerprint),
		action.WithCalls(sol.Spec.Calls),
	)
	if funcBinder != nil {
		action.WithTemplateFuncBinder(funcBinder)(actionExecutor)
	}

	result, err := actionExecutor.Execute(actionCtx, workflow)
	if err != nil && result != nil && result.FinalStatus != action.ExecutionPartialSuccess {
		// An action failed hard. In structured output the full action envelope
		// (status:"failed", per-action error, failedActions) is far more useful
		// than an empty stdout, so emit it before returning the non-zero exit.
		if o.isStructuredOutput() {
			var executionData map[string]any
			if o.ShowExecution {
				executionData = resolverExecutionData
			}
			if writeErr := o.writeActionOutput(ctx, result, executionData); writeErr == nil {
				if w := writer.FromContext(ctx); w != nil {
					w.Errorf("action execution failed: %v", err)
				}
				return exitcode.WithCode(fmt.Errorf("action execution failed: %w", err), exitcode.ActionFailed)
			}
		}
		return o.exitWithCode(ctx, fmt.Errorf("action execution failed: %w", err), exitcode.ActionFailed)
	}

	// State lifecycle: save merged parameters after successful action execution.
	// Immutable locks were already committed before actions (D1).
	if stateMgr != nil && stateData != nil {
		solMeta := buildStateSolutionMeta(sol)
		if saveErr := stateMgr.SaveParams(ctx, stateData, params, resolverData, solMeta); saveErr != nil {
			return o.exitWithCode(ctx, fmt.Errorf("state save: %w", saveErr), exitcode.GeneralError)
		}
	}

	// Build and write output — only include __execution in output when --show-execution is set
	var executionData map[string]any
	if o.ShowExecution {
		executionData = resolverExecutionData
	}

	// Partial success: some continueOnError actions failed but the run did not
	// fail hard. When --detailed-exit-code is set, emit the full output envelope
	// (so json/yaml/table still show per-action detail) and then return the
	// distinct PartialSuccess code. When the flag is off, fall through to the
	// normal exit-0 success path (non-breaking default).
	if result != nil && result.FinalStatus == action.ExecutionPartialSuccess && o.DetailedExitCode {
		if writeErr := o.writeActionOutput(ctx, result, executionData); writeErr != nil {
			return writeErr
		}
		return exitcode.WithCode(
			fmt.Errorf("run completed with partial success: %d action(s) failed with continueOnError", len(result.FailedActions)),
			exitcode.PartialSuccess,
		)
	}

	return o.writeActionOutput(ctx, result, executionData)
}

// executeDryRun executes resolvers normally (they are side-effect-free) and
// produces a structured WhatIf report showing what actions would do.
//
// resolverCtx is used for resolver execution (solution-dir aware, no working-dir
// override) so resolver paths resolve relative to the solution file.
// actionCtx carries CLI overrides (output-dir, on-conflict, backup, working-dir)
// so that dryrun.Generate / WhatIf sees the same context as real execution.
func (o *SolutionOptions) executeDryRun(resolverCtx, actionCtx context.Context, sol *solution.Solution, reg *provider.Registry, params map[string]any, workflow *action.Workflow, stateData *state.Data, cwd string, stateSeed map[string]*resolver.ExecutionResult) error {
	// Execute resolvers via the shared method so that IOStreams, progress
	// callbacks, and CLI-flag-driven config are wired identically to the
	// live execution path. Resolver providers are side-effect-free, so we
	// get real data for WhatIf message generation safely.
	resolvers := sol.Spec.ResolversToSlice()
	resolverData, _, err := o.executeResolvers(resolverCtx, sol, resolvers, params, reg, withSeededResults(stateSeed))
	if err != nil {
		// Non-fatal — report will include warnings about missing resolver data.
		resolverData = make(map[string]any)
	}

	report, err := dryrun.Generate(actionCtx, sol, dryrun.Options{
		Registry:     reg,
		ResolverData: resolverData,
		Verbose:      o.Verbose,
		Workflow:     workflow,
		StateData:    stateData,
		Cwd:          cwd,
	})
	if err != nil {
		return o.exitWithCode(resolverCtx, fmt.Errorf("dry-run failed: %w", err), exitcode.GeneralError)
	}

	return o.writeDryRunOutput(resolverCtx, report)
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
			if act.FingerprintStatus != "" {
				w.Plainlnf("    (fingerprint: %s \u2014 %s)", act.FingerprintStatus, act.FingerprintReason)
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
	if o.IOStreams == nil {
		return nil
	}
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
// When verbose is enabled, a status line for every action is written to stderr.
func (o *SolutionOptions) writeActionOutputDefault(ctx context.Context, result *action.ExecutionResult) error {
	w := writer.FromContext(ctx)
	if w == nil {
		return nil
	}
	for name, ar := range result.Actions {
		// Verbose: show a status line for every action on stderr
		if o.Verbose {
			o.writeVerboseActionStatus(w, name, ar)
		}

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
			// Verbose mode already shows skip status via writeVerboseActionStatus.
			// Only show the non-verbose skip line when verbose is off and it's not a condition skip.
			if !o.Verbose && ar.SkipReason != action.SkipReasonCondition {
				w.WarnStderrf("Skipped [%s]: %s", name, ar.SkipReason)
			}
		case action.StatusPending, action.StatusRunning, action.StatusCancelled:
			// These statuses should not appear in final results; ignore.
		}
	}

	return nil
}

// writeVerboseActionStatus writes a single action status line to stderr.
func (o *SolutionOptions) writeVerboseActionStatus(w *writer.Writer, name string, ar *action.ActionResult) {
	dur := ar.Duration()
	switch ar.Status {
	case action.StatusSucceeded:
		w.PlainStderrf("  ✓ %s (%s)", name, dur)
	case action.StatusSkipped:
		w.PlainStderrf("  ○ %s (skipped: %s)", name, ar.SkipReason)
	case action.StatusFailed:
		w.PlainStderrf("  ✗ %s (failed: %s)", name, ar.Error)
	case action.StatusTimeout:
		w.PlainStderrf("  ✗ %s (timeout)", name)
	case action.StatusCancelled:
		w.PlainStderrf("  ○ %s (cancelled)", name)
	case action.StatusPending, action.StatusRunning:
		// Should not appear in final results; ignore.
	}
}

// writeActionOutputStructured writes action results as JSON or YAML (the full execution envelope).
func (o *SolutionOptions) writeActionOutputStructured(ctx context.Context, result *action.ExecutionResult, executionData map[string]any, format string) error {
	outputData := action.BuildOutputData(result, executionData)
	// Under the "warn" validation policy a validation-only failure did not abort
	// the run: embed its diagnostics and the resolved values so the warning is
	// visible in structured output alongside the successful action results.
	if o.validationWarn != nil {
		outputData = execute.InjectSolutionDiagnostics(outputData, o.validationWarn, o.validationWarnResolvers)
	}
	return o.writeStructuredData(ctx, outputData, format)
}

// writeStructuredData marshals a structured map to JSON or YAML and writes it to
// stdout. It is the shared serialization path for both the success envelope and
// the failure envelope, so both honour the same format handling. Only json and
// yaml are supported (matching writeActionOutput's dispatch); any other format
// returns an error so callers can fall back rather than emit an empty document.
func (o *SolutionOptions) writeStructuredData(ctx context.Context, outputData map[string]any, format string) error {
	var data []byte
	var marshalErr error

	switch format {
	case "yaml":
		data, marshalErr = yaml.Marshal(outputData)
	case "json":
		data, marshalErr = json.MarshalIndent(outputData, "", "  ")
	default:
		return fmt.Errorf("unsupported output format: %s", format)
	}

	if marshalErr != nil {
		return fmt.Errorf("failed to marshal output: %w", marshalErr)
	}

	if w := writer.FromContext(ctx); w != nil {
		w.Plainln(string(data))
	}
	return nil
}

// failStructured renders a solution/action failure so that machine-readable
// output formats (json/yaml) still emit a parseable {status, diagnostics,
// resolvers} document on stdout instead of an empty stdout with a stderr-only
// error. It is used for setup and resolver-phase failures that occur before an
// action ExecutionResult exists; action-execution failures instead emit the
// full action envelope via writeActionOutput. When resolverOut (an
// already-redacted resolver output map) is non-empty its values are embedded
// under "resolvers" so successfully-resolved values are never dropped. For human
// formats (table/quiet) it falls back to the stderr-only exitWithCode behavior.
func (o *SolutionOptions) failStructured(ctx context.Context, resolverOut map[string]any, err error, code int) error {
	if !o.isStructuredOutput() {
		return o.exitWithCode(ctx, err, code)
	}
	envelope := execute.BuildFailureEnvelope(err, resolverOut)
	if writeErr := o.writeStructuredData(ctx, envelope, o.Output); writeErr != nil {
		return o.exitWithCode(ctx, err, code)
	}
	if w := writer.FromContext(ctx); w != nil {
		w.Errorf("%v", err)
	}
	return exitcode.WithCode(err, code)
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

// ActionProgressCallback implements action.ProgressCallback for CLI output.
// Action banners are written to stderr by default (like go-task/make) and are
// suppressed only by --quiet. Use --verbose for additional debug detail.
type ActionProgressCallback struct {
	w      *writer.Writer
	starts sync.Map // actionName -> time.Time
}

// NewActionProgressCallback creates a new action progress callback.
func NewActionProgressCallback(w *writer.Writer) *ActionProgressCallback {
	return &ActionProgressCallback{w: w}
}

func (a *ActionProgressCallback) OnActionStart(actionName, description string) {
	a.starts.Store(actionName, time.Now())
	if description != "" {
		a.w.PlainStderrf("==> %s: %s", actionName, description)
	} else {
		a.w.PlainStderrf("==> %s", actionName)
	}
}

func (a *ActionProgressCallback) OnActionComplete(actionName string, _ any) {
	elapsed := a.elapsed(actionName)
	a.w.PlainStderrf("    done: %s (%s)", actionName, formatElapsed(elapsed))
}

func (a *ActionProgressCallback) OnActionFailed(actionName string, err error) {
	elapsed := a.elapsed(actionName)
	a.w.Errorf("    failed: %s (%s): %v", actionName, formatElapsed(elapsed), err)
}

func (a *ActionProgressCallback) OnActionSkipped(actionName, reason string) {
	a.w.PlainStderrf("    skipped: %s (%s)", actionName, reason)
}

func (a *ActionProgressCallback) OnActionTimeout(actionName string, _ time.Duration) {
	elapsed := a.elapsed(actionName)
	a.w.PlainStderrf("    timeout: %s (after %s)", actionName, formatElapsed(elapsed))
}

func (a *ActionProgressCallback) OnActionCancelled(actionName string) {
	elapsed := a.elapsed(actionName)
	a.w.PlainStderrf("    cancelled: %s (%s)", actionName, formatElapsed(elapsed))
}

func (a *ActionProgressCallback) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	a.w.PlainStderrf("    retry %d/%d: %s: %v", attempt, maxAttempts, actionName, err)
}

func (a *ActionProgressCallback) OnForEachProgress(actionName string, completed, total int) {
	a.w.Verbosef("[ACTION] %s: %d/%d iterations complete", actionName, completed, total)
}

func (a *ActionProgressCallback) OnPhaseStart(_ int, _ []string) {}
func (a *ActionProgressCallback) OnPhaseComplete(_ int)          {}
func (a *ActionProgressCallback) OnFinallyStart()                {}
func (a *ActionProgressCallback) OnFinallyComplete()             {}

// elapsed returns the duration since the action started, or 0 if not found.
// The entry is deleted from starts after reading to avoid stale entries on retries.
func (a *ActionProgressCallback) elapsed(actionName string) time.Duration {
	if v, ok := a.starts.LoadAndDelete(actionName); ok {
		if t, ok := v.(time.Time); ok {
			return time.Since(t)
		}
	}
	return 0
}

// formatElapsed formats a duration as a compact human-readable string.
func formatElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
