// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	cmdflags "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/dryrun"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/fingerprint"
	"github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin/fileprovider"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/load"
	"github.com/oakwood-commons/scafctl/pkg/solution/prepare"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/solution/source"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// ValidOutputTypes defines the supported output formats
var ValidOutputTypes = kvx.BaseOutputFormats()

// solutionOutputFormats lists the output formats accepted by run solution and
// non-list run action invocations for shell completion.
var solutionOutputFormats = []string{
	string(kvx.OutputFormatAuto),
	string(kvx.OutputFormatTable),
	string(kvx.OutputFormatList),
	string(kvx.OutputFormatTree),
	string(kvx.OutputFormatJSON),
	string(kvx.OutputFormatYAML),
	string(kvx.OutputFormatQuiet),
	string(kvx.OutputFormatTest),
}

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

	// CIOutput controls CI workflow annotations and log grouping ("auto", "none", "github-actions").
	CIOutput string

	// Backup enables .bak backup creation before mutating existing files.
	Backup bool

	// ActionNames is the list of action names to execute selectively.
	// When set, only the specified actions, their transitive dependsOn
	// dependencies, and the resolvers required by the filtered workflow are
	// executed (unless --resolve-all is set). Finally actions always run.
	ActionNames []string

	// DynamicArgs are resolver parameters from positional key=value/@file/@-
	// arguments passed after [name[@version]].
	DynamicArgs []string

	// positionalPathErr is set in PreRun when the user passes invalid
	// -f/--file or positional arguments.
	positionalPathErr error

	// validationWarn holds a non-fatal validation-only failure captured under
	// the "warn" policy. When set, its diagnostics and the resolved values
	// (validationWarnResolvers) are injected into the successful action output
	// envelope so the warning is visible in structured output. Reset per run.
	validationWarn          error
	validationWarnResolvers map[string]any

	// progressPrintedFailure records whether ActionProgressCallback already
	// emitted a failure/timeout line during execution so writeActionOutputDefault
	// does not repeat it.
	progressPrintedFailure bool
	hasProgressCallback    bool
	ciDialect              output.CIDialect
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
		Use:     "solution [name[@version]] [key=value...]",
		Aliases: []string{"sol", "s", "solutions"},
		Short:   "Run a solution by executing resolvers and actions",
		Long: strings.ReplaceAll(`Execute a solution by running resolvers and then actions in dependency order.

Solutions can be loaded from:
- Local catalog: Use the solution name as a positional argument (e.g., "my-app" or "my-app@1.2.3")
- Local file: Use -f/--file flag (e.g., -f ./solution.yaml)
- Stdin: Use -f - to read from stdin
- URL: Use an HTTP(S) URL as a positional argument
- Auto-discovery: If no source is specified, searches for solution.yaml in current directory

NOTE: Positional arguments accept at most one catalog name, remote registry ref, or URL,
followed by optional key=value / @file / @- resolver parameters.
Local file paths must use -f/--file.

The solution MUST define a workflow with actions. If no workflow is defined,
the command will error and suggest using 'scafctl run resolver' instead.

The execution proceeds in two phases:
1. RESOLVER PHASE: Resolvers execute in dependency order (concurrent within phases; scoped to the selected actions when --action is set, unless --resolve-all is passed)
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
  tree     ASCII tree structure
  json     JSON output (for piping/scripting)
  yaml     YAML output (for piping/scripting)
  quiet    Suppress output (exit code only)

FAILURE OUTPUT (json/yaml):
  On failure the structured output still emits a parseable document so callers
  piping to jq/yq can detect and inspect the failure programmatically instead of
  receiving empty stdout. Setup and resolver-phase failures emit a
  {status: "failed", diagnostics: [...]} envelope in json/yaml (table/tree/quiet
  keep stderr-only behavior); an action-execution failure emits the full action
  result envelope in json/yaml/tree (status: "failed" with per-action error
  detail), flushes buffered action output in table mode, and suppresses stdout
  under --quiet.

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
  3   Invalid solution or input (no workflow, cycle, parse error, undeclared provider, or bad lock/ref)
  4   File not found
  6   Action/workflow execution failed
  7   Configuration or policy error (unconfigured catalog, allowlist, or signature policy)
  8   Catalog or plugin acquisition failed (offline, auth rejected, or digest/signature failure)
  10  Permission denied
  12  Partial success (only with --detailed-exit-code; some continueOnError actions failed)

Examples:
  # Run solution from catalog by name (latest version)
  scafctl run solution my-app

  # Run specific version from catalog
  scafctl run solution my-app@1.2.3

  # Run solution from file
  scafctl run solution -f ./my-solution.yaml

  # Run only specific actions (and their transitive dependencies)
  scafctl run solution -f ./my-solution.yaml -a lint
  scafctl run solution -f ./my-solution.yaml -a lint -a test

  # Run with parameters (positional key=value or -r)
  scafctl run solution my-app env=prod region=us-east1
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

  # Tree output for inspecting nested action results
  scafctl run solution -f ./my-solution.yaml -o tree

  # Limit concurrent actions
  scafctl run solution --max-action-concurrency=2

  # Show progress during execution
  scafctl run solution --progress

  # Include resolver execution metadata in output
  scafctl run solution --show-execution -f ./my-solution.yaml -o json`, settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.ArbitraryArgs,
		PreRun: func(cCmd *cobra.Command, args []string) {
			options.positionalPathErr = nil
			options.DynamicArgs = nil
			// Track which flags were explicitly set by the user
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
			binName := settings.CliBinaryName
			if cliParams != nil && cliParams.BinaryName != "" {
				binName = cliParams.BinaryName
			}
			cmdUsage := binName + " run solution"
			if _, err := source.ValidateFileFlagPositional(options.File, cmdUsage); err != nil {
				options.positionalPathErr = err
				return
			}
			names, params := splitNamesAndParams(args)
			options.DynamicArgs = params
			if len(names) >= 1 {
				if _, err := source.ValidatePositionalRef(names[0], options.File, cmdUsage); err != nil {
					options.positionalPathErr = err
					return
				}
				if len(names) >= 2 {
					options.positionalPathErr = fmt.Errorf(
						"accepts at most 1 solution reference (got %d: %s); to run specific actions use -a/--action (e.g. %s %s -a %s) or pass parameters as key=value",
						len(names), strings.Join(names, ", "), cmdUsage, names[0], names[1],
					)
					return
				}
				options.File = names[0]
			}
		},
		RunE:         makeRunEFunc(cfg, "solution"),
		SilenceUsage: true,
	}

	// Shared resolver flags
	options.outputCompletionFn = func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return filterOutputFormatCompletions(solutionOutputFormats, toComplete)
	}
	addSharedResolverFlags(cCmd, &options.sharedResolverOptions)

	// Action execution flags
	cCmd.Flags().DurationVar(&options.ActionTimeout, "action-timeout", settings.DefaultActionTimeout, "Default timeout per action")
	cCmd.Flags().IntVar(&options.MaxActionConcurrency, "max-action-concurrency", 0, "Maximum concurrent actions (0=unlimited)")
	cCmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Validate and show what would be executed without running")
	cCmd.Flags().BoolVar(&options.ShowExecution, "show-execution", false, "Include __execution metadata in output (phases, timing, dependencies, providers)")
	cCmd.Flags().StringSliceVarP(&options.ActionNames, "action", "a", nil, "Run only the named action(s), their transitive dependencies, and the resolvers they require (repeatable)")

	// File conflict strategy flags
	cCmd.Flags().StringVar(&options.OnConflict, "on-conflict", "", "Conflict strategy for file writes (error|overwrite|skip|skip-unchanged|append) (default: error)")
	cCmd.Flags().BoolVar(&options.Force, "force", false, "Skip unchanged files and write only new or modified content (shorthand for --on-conflict skip-unchanged)")
	cCmd.Flags().BoolVar(&options.SkipFingerprint, "skip-fingerprint", false, "Disable fingerprint-based up-to-date checks (re-run all actions)")
	cCmd.Flags().BoolVar(&options.Backup, "backup", false, "Create .bak backups before mutating existing files")

	// Detailed exit code: opt-in distinct exit code (12) on partial success.
	// Defaults from cliParams (embedder/config); the flag overrides it.
	cCmd.Flags().BoolVar(&options.DetailedExitCode, "detailed-exit-code", cliParams.DetailedExitCode, "Return a distinct exit code (12) when the run completes with partial success (some continueOnError actions failed); default off (partial success exits 0)")
	cCmd.Flags().StringVar(&options.CIOutput, "ci-output", settings.DefaultActionCIOutput, "CI output format for action banners and annotations (auto|none|github-actions)")

	return cCmd
}

// getEffectiveActionConfig returns action config values, using app config
// as defaults when CLI flags weren't explicitly set.
func (o *SolutionOptions) getEffectiveActionConfig(ctx context.Context) config.ActionConfigValues {
	ciOutput := o.CIOutput
	if ciOutput == "" {
		ciOutput = settings.DefaultActionCIOutput
	}
	// Start with CLI flag values
	result := config.ActionConfigValues{
		DefaultTimeout: o.ActionTimeout,
		GracePeriod:    settings.DefaultGracePeriod, // Not a CLI flag
		MaxConcurrency: o.MaxActionConcurrency,
		CIOutput:       ciOutput,
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
		if !o.flagsChanged["ci-output"] {
			result.CIOutput = configValues.CIOutput
		}
	} else if o.CIOutput == "" {
		result.CIOutput = configValues.CIOutput
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
	o.progressPrintedFailure = false
	o.hasProgressCallback = false
	o.ciDialect = nil

	// Fail early if PreRun detected a local file path as positional arg
	if o.positionalPathErr != nil {
		return o.exitWithCode(ctx, o.positionalPathErr, exitcode.InvalidInput)
	}

	// Reject mutually exclusive state flags before any work begins.
	if err := o.validateStateFlags(); err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}

	// Include pre-release versions in catalog resolution when --pre-release is set.
	if o.PreRelease {
		ctx = catalog.WithIncludePreRelease(ctx)
	}

	// Validate --lock-mode early so an invalid value fails with InvalidInput
	// instead of a misleading FileNotFound from prepareSolutionFromSource.
	if err := o.validateLockMode(); err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}

	actionCfg := o.getEffectiveActionConfig(ctx)
	ciDialect, err := output.ResolveCIDialect(actionCfg.CIOutput, os.Getenv)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.InvalidInput)
	}
	o.ciDialect = ciDialect

	lgr := logger.FromContext(ctx)

	// Apply config default for output-dir when the CLI flag wasn't explicitly set
	if o.flagsChanged != nil && !o.flagsChanged["output-dir"] && actionCfg.OutputDir != "" {
		o.OutputDir = actionCfg.OutputDir
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
	if o.File == "-" && (flags.ContainsStdinRef(o.ResolverParams) || flags.ContainsStdinRef(o.DynamicArgs)) {
		return o.exitWithCode(ctx,
			fmt.Errorf("cannot use both -f - and @-: stdin can only be read once"),
			exitcode.InvalidInput)
	}

	// Capture the original working directory before prepareSolutionFromSource,
	// which may os.Chdir into a bundle extraction directory. This ensures __cwd
	// in action expressions reflects the user's actual working directory.
	originalCwd, err := provider.GetWorkingDirectory(ctx)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to get working directory: %w", err), exitcode.GeneralError)
	}

	// Prepare solution: select the source (-f or the positional ref, else a
	// discovered file), load it, set up the registry, and handle bundles.
	loader := o.newSolutionLoader(ctx)
	sel, err := loader.Select(ctx, o.File, nil, load.ArgsAreNames, o.sourceOptions()...)
	if err != nil {
		return o.exitWithCode(ctx, err, prepare.ErrorExitCode(err))
	}
	sol, reg, solutionDir, cleanup, providerCtx, err := o.prepareSolutionFromSource(ctx, loader.SelectionLoader(sel), sel.Source)
	if err != nil {
		return o.exitWithCode(ctx, err, prepare.ErrorExitCode(err))
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

	// Require a workflow — run solution is for executing actions.
	// Use 'scafctl run resolver' for resolver-only solutions.
	if !sol.Spec.HasWorkflow() {
		return o.exitWithCode(ctx,
			fmt.Errorf("solution %q has no workflow defined; use '%s run resolver' to execute resolvers without actions", sol.Metadata.Name, o.BinaryName),
			exitcode.InvalidInput)
	}

	// Validate the workflow
	if err := action.ValidateWorkflow(sol.Spec.Workflow, reg); err != nil {
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

	// Parse dynamic positional arguments (key=value and @file.yaml from argv)
	extraParsed, err := flags.ParseDynamicInputArgs(o.DynamicArgs)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to parse positional parameters: %w", err), exitcode.ValidationFailed)
	}

	allParams := make([]string, 0, len(o.ResolverParams)+len(extraParsed))
	allParams = append(allParams, o.ResolverParams...)
	allParams = append(allParams, extraParsed...)

	// Parse resolver parameters (pass stdin for @- support)
	var stdinReader io.Reader
	if o.IOStreams != nil {
		stdinReader = o.IOStreams.In
	}
	params, err := flags.ParseResolverFlagsWithStdin(allParams, stdinReader)
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

	if redact.FromContext(ctx) == nil {
		ctx = redact.WithTracker(ctx, redact.NewTracker())
	}
	if o.DryRun {
		ctx = spec.WithCredentialResolver(ctx, spec.PlaceholderCredentialResolver())
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
	// params are passed so that state load inputs can reference CLI
	// parameters via __params in CEL expressions (e.g. __params.appName).
	allResolvers := sol.Spec.ResolversToSlice()
	var stateMgr *state.Manager
	var stateData *state.Data
	var stateSeed map[string]*resolver.ExecutionResult
	var effectiveStateCfg *state.Config
	stateCfg, stateCfgErr := o.resolveStateConfig(ctx, sol)
	if stateCfgErr != nil {
		return o.exitWithCode(ctx, stateCfgErr, exitcode.InvalidInput)
	}
	if o.NoState {
		warnStateSkipped(ctx, sol)
	} else if stateCfg != nil {
		stateMgr = state.NewManager(stateCfg, reg, state.RuntimeProvenanceFromContext(ctx), o.stateManagerOptions(originalCwd)...)
		cmdInfo := buildCommandInfo("run solution", params)
		loadResult, loadErr := stateMgr.LoadTwoPhase(ctx, params, cmdInfo, o.buildStateTwoPhaseInput(sol, params, reg))
		if loadErr != nil {
			return o.handleStateLoadError(ctx, loadErr)
		}
		if err := o.checkMissingLocks(ctx, loadResult.LoadResult, allResolvers, o.DryRun); err != nil {
			return err
		}
		stateSeed = loadResult.Seed
		if !loadResult.Skipped {
			effectiveStateCfg = stateCfg
			ctx = loadResult.Ctx
			actionCtx = state.WithState(actionCtx, loadResult.Data)
			stateData = loadResult.Data
			params = loadResult.MergedParams
			warnSolutionMismatch(ctx, stateData, sol)
		}
		reportStateLoaded(ctx, loadResult.LoadResult)
	}

	// Scope resolver execution to the selected actions when --action is set and
	// --resolve-all is not set.
	resolvers := allResolvers
	if len(o.ActionNames) > 0 && !o.ResolveAll {
		var lookup resolver.DescriptorLookup
		if reg != nil {
			lookup = reg.DescriptorLookup()
		}
		skipValidation := validationPolicy == settings.ValidationIgnore || o.SkipTransform
		resolvers = execute.ResolversForWorkflow(sol, workflow, effectiveStateCfg, lookup, execute.WithSkipValidationDeps(skipValidation))
		lgr.V(1).Info("resolver execution scoped to selected actions",
			"targets", o.ActionNames,
			"selectedResolvers", len(resolvers),
			"totalResolvers", len(allResolvers))
	}

	// Dry run — execute resolvers with ctx (CWD by default, or base-dir when
	// --base-dir is explicitly set) so resolver paths resolve accordingly. The
	// action-phase WhatIf report uses actionCtx which has the working-dir
	// override for accurate output-dir resolution.
	if o.DryRun {
		return o.executeDryRun(ctx, actionCtx, sol, reg, params, workflow, resolvers, stateData, originalCwd, stateSeed)
	}

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
			o.validationWarnResolvers = o.buildRedactedResolverOutputMap(ctx, resolverCtx, resolverData, sol)
		} else {
			// Fatal: resolve/transform failures under any policy, and
			// validation failures under the "error" policy. Preserve the
			// resolved values in the failure envelope instead of dropping them.
			// Validation-only failures use the dedicated ValidationFailed code.
			code := exitcode.GeneralError
			if validationOnly {
				code = exitcode.ValidationFailed
			}
			return o.failStructured(ctx, o.buildRedactedResolverOutputMap(ctx, resolverCtx, resolverData, sol), err, code)
		}
	}

	resolverElapsed := time.Since(start)

	// State lifecycle: verify immutable values against their locks after
	// resolvers and deferred validation, but before any side-effecting action
	// runs. Then write the checkpoint save targets (checkpoint: true), if any, so
	// immutable values locked now survive a later action failure -- without a
	// checkpoint target, state is written only after the workflow succeeds.
	// Resolvers whose deferred validation failed are never locked.
	var stateSkip map[string]bool
	if stateMgr != nil && stateData != nil {
		if err := o.verifyImmutables(ctx, stateMgr, stateData, resolverCtx, resolvers); err != nil {
			return err
		}
		stateSkip = deferredValidationFailures(resolverCtx)
		if cpErr := stateMgr.Checkpoint(ctx, stateData, resolverCtx, resolvers, params, resolverData, buildStateSolutionMeta(sol), stateSkip); cpErr != nil {
			return o.exitWithCode(ctx, fmt.Errorf("state checkpoint: %w", cpErr), exitcode.GeneralError)
		}
	}

	// Build action graph
	graph, err := action.BuildGraph(ctx, workflow, resolverData, nil)
	if err != nil {
		if t := redact.FromContext(ctx); t != nil {
			err = t.RedactError(err)
		}
		return o.exitWithCode(ctx, fmt.Errorf("failed to build action graph: %w", err), exitcode.InvalidInput)
	}

	lgr.V(1).Info("action graph built",
		"totalActions", len(graph.Actions),
		"mainPhases", len(graph.ExecutionOrder),
		"finallyPhases", len(graph.FinallyOrder))

	// Action banners are always enabled (default-on like go-task/make); suppressed by --quiet.
	// writer.FromContext returns nil when no writer is in context (e.g. direct Run calls in tests);
	// in that case we skip the callback so the executor's nil-guard suppresses output safely.
	actionStreams := o.getActionIOStreams()
	var progressCB *ActionProgressCallback
	var actionProgressCallback action.ProgressCallback
	if w := writer.FromContext(ctx); w != nil && o.Output != "quiet" {
		cbWriter, syncedStreams := synchronizeActionStreams(w, actionStreams)
		actionStreams = syncedStreams
		actionCtx = writer.WithWriter(actionCtx, cbWriter)
		progressCB = NewActionProgressCallbackWithDialect(cbWriter, ciDialect)
		actionProgressCallback = progressCB
	}

	// Build execution metadata before constructing the action executor so it can be
	// injected as __execution into action when conditions and provider inputs,
	// regardless of whether --show-execution is set for resolver output display.
	resolverExecutionData := execute.BuildExecutionData(resolverCtx, resolvers, resolverElapsed)

	funcBinder, funcBinderErr := sol.TemplateFuncBinder()
	if funcBinderErr != nil {
		return fmt.Errorf("compiling spec.functions: %w", funcBinderErr)
	}

	actionExecutor := action.NewExecutor(
		action.WithRegistry(reg),
		action.WithResolverData(resolverData),
		action.WithSensitiveResolvers(execute.SensitiveResolverNames(sol, resolverCtx)),
		action.WithExecutionData(resolverExecutionData),
		action.WithProgressCallback(actionProgressCallback),
		action.WithDefaultTimeout(actionCfg.DefaultTimeout),
		action.WithGracePeriod(actionCfg.GracePeriod),
		action.WithMaxConcurrency(actionCfg.MaxConcurrency),
		action.WithIOStreams(actionStreams),
		action.WithCwd(originalCwd),
		action.WithFingerprintChecker(fingerprint.NewChecker(stateData)),
		action.WithNoCache(o.SkipFingerprint),
		action.WithCalls(sol.Spec.Calls),
	)
	if funcBinder != nil {
		action.WithTemplateFuncBinder(funcBinder)(actionExecutor)
	}

	result, err := actionExecutor.Execute(actionCtx, workflow)
	if progressCB != nil {
		o.hasProgressCallback = true
		o.progressPrintedFailure = progressCB.PrintedFailure()
	}
	if err != nil && result != nil && result.FinalStatus != action.ExecutionPartialSuccess {
		var executionData map[string]any
		if o.ShowExecution {
			executionData = resolverExecutionData
		}
		alreadyPrinted := o.progressPrintedFailure && result.FinalStatus != action.ExecutionCancelled
		errMsg := fmt.Sprintf("action execution failed: %v", err)
		if ciDialect != nil {
			errMsg = ciDialect.SanitizeMultiline(errMsg)
		}
		// An action failed hard. In structured output the full action envelope
		// (status:"failed", per-action error, failedActions) is far more useful
		// than an empty stdout, so emit it before returning the non-zero exit.
		if o.isStructuredOutput() {
			if writeErr := o.writeActionOutput(ctx, result, executionData); writeErr == nil {
				if !alreadyPrinted {
					if w := writer.FromContext(ctx); w != nil {
						w.Error(errMsg)
					}
				}
				return exitcode.WithCode(fmt.Errorf("action execution failed: %w", err), exitcode.ActionFailed)
			}
		} else {
			_ = o.writeActionOutput(ctx, result, executionData)
		}
		if alreadyPrinted {
			return exitcode.WithCode(fmt.Errorf("action execution failed: %w", err), exitcode.ActionFailed)
		}
		if w := writer.FromContext(ctx); w != nil {
			w.Error(errMsg)
		}
		return exitcode.WithCode(fmt.Errorf("action execution failed: %w", err), exitcode.ActionFailed)
	}

	// State lifecycle: write every save target after successful action
	// execution -- the merged parameters plus immutable and persisted values.
	if stateMgr != nil && stateData != nil {
		saveResult, saveErr := stateMgr.Save(ctx, stateData, resolverCtx, resolvers, params, resolverData, buildStateSolutionMeta(sol), stateSkip)
		// Report what was written even when a later target failed.
		reportStateSaved(ctx, saveResult)
		if saveErr != nil {
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
	// distinct PartialSuccess code via exitWithCode so the stderr diagnostic is
	// printed (Cobra silences the returned error at the root). When the flag is
	// off, fall through to the normal exit-0 success path (non-breaking default).
	if result != nil && result.FinalStatus == action.ExecutionPartialSuccess && o.DetailedExitCode {
		if writeErr := o.writeActionOutput(ctx, result, executionData); writeErr != nil {
			return writeErr
		}
		return o.exitWithCode(ctx,
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
func (o *SolutionOptions) executeDryRun(resolverCtx, actionCtx context.Context, sol *solution.Solution, reg providerLookup, params map[string]any, workflow *action.Workflow, resolvers []*resolver.Resolver, stateData *state.Data, cwd string, stateSeed map[string]*resolver.ExecutionResult) error {
	// Never touch keyrings or mint live tokens during --dry-run.
	resolverCtx = spec.WithCredentialResolver(resolverCtx, spec.PlaceholderCredentialResolver())
	actionCtx = spec.WithCredentialResolver(actionCtx, spec.PlaceholderCredentialResolver())
	// Execute resolvers via the shared method so that IOStreams, progress
	// callbacks, and CLI-flag-driven config are wired identically to the
	// live execution path. Resolver providers are side-effect-free, so we
	// get real data for WhatIf message generation safely.
	if resolvers == nil {
		resolvers = sol.Spec.ResolversToSlice()
	}
	resolverData, resExecCtx, err := o.executeResolvers(resolverCtx, sol, resolvers, params, reg, withSeededResults(stateSeed))
	if err != nil {
		// Non-fatal — report will include warnings about missing resolver data.
		resolverData = make(map[string]any)
	}

	report, err := dryrun.Generate(actionCtx, sol, dryrun.Options{
		Registry:        reg,
		ResolverData:    resolverData,
		ResolverContext: resExecCtx,
		Verbose:         o.Verbose,
		Workflow:        workflow,
		StateData:       stateData,
		Cwd:             cwd,
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
	case "tree":
		reportMap, err := kvx.StructToMap(report)
		if err != nil {
			return fmt.Errorf("failed to normalize dry-run report: %w", err)
		}
		return o.writeTreeData(ctx, reportMap)
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
// stream output directly to the terminal. For json/yaml/tree/quiet/test, no streams are
// provided so all output is captured and rendered in the output document.
func (o *SolutionOptions) getActionIOStreams() *provider.IOStreams {
	if o.IOStreams == nil {
		return nil
	}
	switch o.Output {
	case "json", "yaml", "tree", "quiet", "test":
		// Structured/formatted envelope modes: don't stream, capture everything for rendering
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
// For json/yaml/tree, the full execution envelope is rendered.
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
	case "tree":
		return o.writeTreeData(ctx, o.buildStructuredActionOutput(result, executionData))
	case "test":
		return o.writeActionTestOutput(ctx, result, executionData)
	default:
		return fmt.Errorf("unsupported output format: %s", o.Output)
	}
}

func (o *SolutionOptions) effectiveCIDialect() output.CIDialect {
	if o.ciDialect != nil {
		return o.ciDialect
	}
	return output.PlainCIDialect()
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
	dialect := o.effectiveCIDialect()
	names := slices.Sorted(maps.Keys(result.Actions))
	for _, name := range names {
		ar := result.Actions[name]
		safeName := dialect.SanitizeInline(name)
		// Verbose: show a status line for every action on stderr
		if o.Verbose {
			o.writeVerboseActionStatus(w, name, ar)
		}

		// Skip actions that already streamed or flushed their output to the terminal
		if ar.Streamed {
			if (ar.Status == action.StatusFailed || ar.Status == action.StatusTimeout) && !o.progressPrintedFailure {
				w.Errorf("Error [%s]: %s", safeName, dialect.SanitizeMultiline(ar.Error))
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
			// Preserve buffered stdout first, then stderr, then error (unless progress callback already reported it)
			if results, ok := ar.Results.(map[string]any); ok {
				if stdout, ok := results["stdout"].(string); ok && stdout != "" {
					w.Plain(stdout)
				}
				if stderr, ok := results["stderr"].(string); ok && stderr != "" {
					w.WarnStderrf("%s", dialect.SanitizeMultiline(stderr))
				}
			}
			if !o.progressPrintedFailure {
				w.Errorf("Error [%s]: %s", safeName, dialect.SanitizeMultiline(ar.Error))
			}
		case action.StatusSkipped:
			// Verbose mode already shows skip status via writeVerboseActionStatus, and
			// ActionProgressCallback.OnActionSkipped already reported skips during live runs.
			if !o.Verbose && !o.hasProgressCallback && ar.SkipReason != action.SkipReasonCondition {
				w.WarnStderrf("Skipped [%s]: %s", safeName, dialect.SanitizeInline(string(ar.SkipReason)))
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
	noColor := w.NoColor()
	dialect := o.effectiveCIDialect()
	safeName := dialect.SanitizeInline(name)
	safeSkip := dialect.SanitizeInline(string(ar.SkipReason))
	safeErr := dialect.SanitizeMultiline(ar.Error)
	switch ar.Status {
	case action.StatusSucceeded:
		if noColor {
			w.PlainStderrf("  [ok] %s (%s)", safeName, dur)
		} else {
			w.PlainStderrf("  ✓ %s (%s)", safeName, dur)
		}
	case action.StatusSkipped:
		if noColor {
			w.PlainStderrf("  [skip] %s (skipped: %s)", safeName, safeSkip)
		} else {
			w.PlainStderrf("  ○ %s (skipped: %s)", safeName, safeSkip)
		}
	case action.StatusFailed:
		if noColor {
			w.PlainStderrf("  [fail] %s (failed: %s)", safeName, safeErr)
		} else {
			w.PlainStderrf("  ✗ %s (failed: %s)", safeName, safeErr)
		}
	case action.StatusTimeout:
		if noColor {
			w.PlainStderrf("  [fail] %s (timeout)", safeName)
		} else {
			w.PlainStderrf("  ✗ %s (timeout)", safeName)
		}
	case action.StatusCancelled:
		if noColor {
			w.PlainStderrf("  [cancel] %s (cancelled)", safeName)
		} else {
			w.PlainStderrf("  ○ %s (cancelled)", safeName)
		}
	case action.StatusPending, action.StatusRunning:
		// Should not appear in final results; ignore.
	}
}

// buildStructuredActionOutput builds the action execution output map, embedding
// non-fatal validation diagnostics and resolved values when present.
func (o *SolutionOptions) buildStructuredActionOutput(result *action.ExecutionResult, executionData map[string]any) map[string]any {
	outputData := action.BuildOutputData(result, executionData)
	// Under the "warn" validation policy a validation-only failure did not abort
	// the run: embed its diagnostics and the resolved values so the warning is
	// visible alongside the successful action results.
	if o.validationWarn != nil {
		outputData = execute.InjectSolutionDiagnostics(outputData, o.validationWarn, o.validationWarnResolvers)
	}
	return outputData
}

// writeActionOutputStructured writes action results as JSON or YAML (the full execution envelope).
func (o *SolutionOptions) writeActionOutputStructured(ctx context.Context, result *action.ExecutionResult, executionData map[string]any, format string) error {
	return o.writeStructuredData(ctx, o.buildStructuredActionOutput(result, executionData), format)
}

// writeTreeData renders structured data as an ASCII tree through kvx, honoring
// --no-color, -e/--expression, -w/--where, and the configured binary name.
func (o *SolutionOptions) writeTreeData(ctx context.Context, data any) error {
	w := writer.FromContext(ctx)
	if w != nil && w.IsQuiet() {
		return nil
	}
	if o.shouldRedactSensitive() {
		data = o.redactTreeDisplayData(ctx, data)
	}
	ioStreams := o.IOStreams
	if ioStreams == nil && w != nil {
		ioStreams = w.IOStreams()
	}
	if ioStreams == nil || ioStreams.Out == nil {
		return nil
	}

	noColor := (o.CliParams != nil && o.CliParams.NoColor) || (w != nil && w.NoColor())
	binName := o.BinaryName
	if binName == "" {
		if o.CliParams != nil && o.CliParams.BinaryName != "" {
			binName = o.CliParams.BinaryName
		} else {
			binName = settings.CliBinaryName
		}
	}
	appName := o.AppName
	if appName == "" {
		appName = binName + " run solution"
	}

	kvxOpts := cmdflags.ToKvxOutputOptions(
		&o.KvxOutputFlags,
		kvx.WithIOStreams(ioStreams),
		kvx.WithOutputContext(ctx),
		kvx.WithOutputFormat(kvx.OutputFormatTree),
		kvx.WithOutputNoColor(noColor),
		kvx.WithOutputAppName(appName),
	)
	return kvxOpts.Write(data)
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
// formats (table/tree/quiet) it falls back to the stderr-only exitWithCode behavior.
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

	normalizedDynamic, err := flags.ParseDynamicInputArgs(o.DynamicArgs)
	if err != nil {
		return fmt.Errorf("failed to parse positional parameters for test generation: %w", err)
	}
	var testArgs []string
	for _, param := range o.ResolverParams {
		testArgs = append(testArgs, "-r", param)
	}
	for _, param := range normalizedDynamic {
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

// sharedSyncWriter serializes concurrent writes from ActionProgressCallback,
// writer.FromContext(actionCtx), and provider IOStreams when targeting the same
// underlying stream while keeping ordinary Write calls strictly byte-preserving.
// Explicit CI directives use WriteDirectiveLine to align onto column 0.
type sharedSyncWriter struct {
	mu          sync.Mutex
	w           io.Writer
	atLineStart bool
}

func newSharedSyncWriter(w io.Writer) io.Writer {
	if w == nil {
		return nil
	}
	if _, ok := w.(*sharedSyncWriter); ok {
		return w
	}
	return &sharedSyncWriter{w: w, atLineStart: true}
}

func (s *sharedSyncWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n, err := s.w.Write(p)
	if n > 0 {
		s.atLineStart = p[n-1] == '\n'
	}
	return n, err
}

// WriteDirectiveLine writes a CI workflow directive on its own line, inserting
// a leading newline first when preceding output did not end with '\n'.
func (s *sharedSyncWriter) WriteDirectiveLine(line string) error {
	trimmed := strings.TrimRight(line, "\r\n")
	if trimmed == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if dlw, ok := s.w.(interface{ WriteDirectiveLine(string) error }); ok {
		if err := dlw.WriteDirectiveLine(trimmed); err != nil {
			return err
		}
		s.atLineStart = true
		return nil
	}
	if !s.atLineStart {
		if _, err := s.w.Write([]byte{'\n'}); err != nil {
			return err
		}
		s.atLineStart = true
	}
	target := trimmed + "\n"
	n, err := io.WriteString(s.w, target)
	if n > 0 {
		s.atLineStart = target[n-1] == '\n'
	}
	return err
}

func sameUnderlyingWriter(a, b io.Writer) bool {
	if a == nil || b == nil {
		return false
	}
	ta := reflect.TypeOf(a)
	tb := reflect.TypeOf(b)
	if ta != tb || !ta.Comparable() {
		return false
	}
	return a == b
}

func synchronizeActionStreams(w *writer.Writer, actionStreams *provider.IOStreams) (*writer.Writer, *provider.IOStreams) {
	if w == nil || w.IOStreams() == nil {
		return w, actionStreams
	}
	base := w.IOStreams()
	syncOut := newSharedSyncWriter(base.Out)
	var syncErr io.Writer
	if sameUnderlyingWriter(base.Out, base.ErrOut) {
		syncErr = syncOut
	} else {
		syncErr = newSharedSyncWriter(base.ErrOut)
	}
	termStreams := &terminal.IOStreams{
		In:           base.In,
		Out:          syncOut,
		ErrOut:       syncErr,
		ColorEnabled: base.ColorEnabled,
	}
	clonedWriter := w.Clone(writer.WithIOStreams(termStreams))
	if actionStreams == nil {
		return clonedWriter, nil
	}

	actOut := syncOut
	if !sameUnderlyingWriter(actionStreams.Out, base.Out) {
		if sameUnderlyingWriter(actionStreams.Out, base.ErrOut) {
			actOut = syncErr
		} else {
			actOut = newSharedSyncWriter(actionStreams.Out)
		}
	}
	actErr := syncErr
	if !sameUnderlyingWriter(actionStreams.ErrOut, base.ErrOut) {
		switch {
		case sameUnderlyingWriter(actionStreams.ErrOut, base.Out):
			actErr = syncOut
		case sameUnderlyingWriter(actionStreams.ErrOut, actionStreams.Out):
			actErr = actOut
		default:
			actErr = newSharedSyncWriter(actionStreams.ErrOut)
		}
	}
	return clonedWriter, &provider.IOStreams{
		Out:    actOut,
		ErrOut: actErr,
	}
}

// ActionProgressCallback implements action.ProgressCallback for CLI output.
// Action banners are written to stderr by default (like go-task/make) and are
// suppressed only by --quiet. When configured with a CI dialect (such as
// GitHub Actions), it wraps at most one active action at a time in ::group:: /
// ::endgroup:: markers and emits ::error annotations on failure or timeout.
type ActionProgressCallback struct {
	w           *writer.Writer
	dialect     output.CIDialect
	mu          sync.Mutex
	activeGroup string
	printedFail bool
	starts      sync.Map // actionName -> time.Time
}

// NewActionProgressCallback creates a new action progress callback with plain formatting.
func NewActionProgressCallback(w *writer.Writer) *ActionProgressCallback {
	return NewActionProgressCallbackWithDialect(w, output.PlainCIDialect())
}

// NewActionProgressCallbackWithDialect creates a new action progress callback configured with a CIDialect.
func NewActionProgressCallbackWithDialect(w *writer.Writer, dialect output.CIDialect) *ActionProgressCallback {
	if dialect == nil {
		dialect = output.PlainCIDialect()
	}
	return &ActionProgressCallback{
		w:       w,
		dialect: dialect,
	}
}

// PrintedFailure reports whether this callback emitted a failure or timeout diagnostic.
func (a *ActionProgressCallback) PrintedFailure() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.printedFail
}

func (a *ActionProgressCallback) closeGroupLocked(actionName string) {
	if a.activeGroup != "" && a.activeGroup == actionName {
		if end := a.dialect.GroupEnd(); end != "" {
			a.w.PlainStderrDirective(end)
		}
		a.activeGroup = ""
	}
}

func (a *ActionProgressCallback) forceCloseGroupLocked() {
	if a.activeGroup != "" {
		if end := a.dialect.GroupEnd(); end != "" {
			a.w.PlainStderrDirective(end)
		}
		a.activeGroup = ""
	}
}

func (a *ActionProgressCallback) OnActionStart(actionName, description string) {
	a.starts.Store(actionName, time.Now())
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.w.IsQuiet() && a.activeGroup == "" {
		if grp := a.dialect.GroupStart(actionName, description); grp != "" {
			a.w.PlainStderrDirective(grp)
			a.activeGroup = actionName
		}
	}
	safeName := a.dialect.SanitizeInline(actionName)
	safeDesc := a.dialect.SanitizeInline(description)
	if safeDesc != "" {
		a.w.PlainStderrf("==> %s: %s", safeName, safeDesc)
	} else {
		a.w.PlainStderrf("==> %s", safeName)
	}
}

func (a *ActionProgressCallback) OnActionComplete(actionName string, _ any) {
	elapsed := a.elapsed(actionName)
	a.mu.Lock()
	defer a.mu.Unlock()

	a.w.PlainStderrf("    done: %s (%s)", a.dialect.SanitizeInline(actionName), formatElapsed(elapsed))
	a.closeGroupLocked(actionName)
}

func (a *ActionProgressCallback) OnActionFailed(actionName string, err error) {
	elapsed := a.elapsed(actionName)
	a.mu.Lock()
	defer a.mu.Unlock()

	a.printedFail = true
	safeName := a.dialect.SanitizeInline(actionName)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	a.w.Errorf("    failed: %s (%s): %s", safeName, formatElapsed(elapsed), a.dialect.SanitizeMultiline(errText))
	a.forceCloseGroupLocked()
	if err != nil {
		if ann := a.dialect.ErrorAnnotation(actionName, errText); ann != "" {
			a.w.StderrDirective(ann)
		}
	}
}

func (a *ActionProgressCallback) OnActionSkipped(actionName, reason string) {
	a.w.PlainStderrf("    skipped: %s (%s)", a.dialect.SanitizeInline(actionName), a.dialect.SanitizeInline(reason))
}

func (a *ActionProgressCallback) OnActionTimeout(actionName string, timeout time.Duration) {
	elapsed := a.elapsed(actionName)
	a.mu.Lock()
	defer a.mu.Unlock()

	a.printedFail = true
	a.w.Errorf("    timeout: %s (after %s)", a.dialect.SanitizeInline(actionName), formatElapsed(elapsed))
	a.forceCloseGroupLocked()
	msg := fmt.Sprintf("action timed out after %s", formatElapsed(elapsed))
	if timeout > 0 {
		msg = fmt.Sprintf("action timed out after %v", timeout)
	}
	if ann := a.dialect.ErrorAnnotation(actionName, msg); ann != "" {
		a.w.StderrDirective(ann)
	}
}

func (a *ActionProgressCallback) OnActionCancelled(actionName string) {
	elapsed := a.elapsed(actionName)
	a.mu.Lock()
	defer a.mu.Unlock()

	a.w.PlainStderrf("    cancelled: %s (%s)", a.dialect.SanitizeInline(actionName), formatElapsed(elapsed))
	a.closeGroupLocked(actionName)
}

func (a *ActionProgressCallback) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	errText := ""
	if err != nil {
		errText = a.dialect.SanitizeMultiline(err.Error())
	}
	a.w.PlainStderrf("    retry %d/%d: %s: %s", attempt, maxAttempts, a.dialect.SanitizeInline(actionName), errText)
}

func (a *ActionProgressCallback) OnForEachProgress(actionName string, completed, total int) {
	a.w.Verbosef("[ACTION] %s: %d/%d iterations complete", a.dialect.SanitizeInline(actionName), completed, total)
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

// redactTreeDisplayData returns a display-only copy of data with tracked
// credentials and sensitive resolver outputs masked by redact.RedactedPlaceholder.
func (o *SolutionOptions) redactTreeDisplayData(ctx context.Context, data any) any {
	var sensitiveTracker *redact.Tracker
	if len(o.sensitiveValues) > 0 {
		sensitiveTracker = redact.NewTracker()
		for _, sv := range o.sensitiveValues {
			sensitiveTracker.TrackValue(sv)
		}
	}
	masked := maskTreeSensitiveValues(data, o.sensitiveValues, 0)
	if ctxTracker := redact.FromContext(ctx); ctxTracker != nil {
		masked = ctxTracker.RedactAny(masked)
	}
	if sensitiveTracker != nil {
		masked = sensitiveTracker.RedactAny(masked)
	}
	return masked
}

func maskTreeSensitiveValues(v any, sensitiveValues []any, depth int) any {
	if v == nil || len(sensitiveValues) == 0 || depth > 32 {
		return v
	}
	if _, isBool := v.(bool); !isBool {
		for _, sv := range sensitiveValues {
			if sv != nil && reflect.DeepEqual(v, sv) {
				return redact.RedactedPlaceholder
			}
		}
	}
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, elem := range typed {
			out[k] = maskTreeSensitiveValues(elem, sensitiveValues, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, elem := range typed {
			out[i] = maskTreeSensitiveValues(elem, sensitiveValues, depth+1)
		}
		return out
	default:
		return v
	}
}
