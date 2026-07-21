// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	stdfilepath "path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/flags"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	resolverdetail "github.com/oakwood-commons/scafctl/pkg/resolver/detail"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/state"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ResolverOptions holds configuration for the run resolver command
type ResolverOptions struct {
	BinaryName string
	sharedResolverOptions

	// Names is the list of resolver names to execute (positional args).
	// If empty, all resolvers are executed.
	Names []string

	// Actions scopes resolver output to one or more actions by name.
	// When set, resolver names are extracted from each action's inputs
	// and the resolver execution is filtered to only those resolvers
	// (plus their transitive dependencies). Multiple values are unioned.
	Actions []string

	// SkipTransform skips the transform and validation phases,
	// returning raw resolved values.
	SkipTransform bool

	// Graph renders the resolver dependency graph instead of executing.
	Graph bool

	// GraphFormat controls the graph rendering format (ascii, dot, mermaid, json).
	GraphFormat string

	// Snapshot saves an execution snapshot to a file instead of normal output.
	Snapshot bool

	// SnapshotFile is the path to write the snapshot file.
	SnapshotFile string

	// Redact redacts sensitive values in the snapshot.
	Redact bool

	// ShowExecution includes the __execution metadata in output.
	ShowExecution bool

	// FailOnValidation makes the command exit with a non-zero status when any
	// resolver fails validation. By default, validation failures are reported as
	// non-fatal diagnostics (values are still shown) and the command exits 0.
	FailOnValidation bool

	// DynamicArgs are resolver parameters from positional key=value syntax
	// (e.g. env=prod region=us-east-1, captured from positional args containing '=').
	DynamicArgs []string
}

// CommandResolver creates the 'run resolver' subcommand
func CommandResolver(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &ResolverOptions{}

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
		},
	}

	cCmd := &cobra.Command{
		Use:     "resolver [resolver-name...] [key=value...]",
		Aliases: []string{"res", "resolvers"},
		Short:   "Execute resolvers for debugging and inspection",
		Long: strings.ReplaceAll(`Execute resolvers from a solution without running actions.

This command is designed for debugging and inspecting resolver execution.
It loads a solution file and executes only the resolvers, skipping the
action/workflow phase entirely.

By default, output contains only the resolved values. Pass --show-execution
to include a __execution key with per-resolver metadata: phase numbers,
timing, provider info, dependencies, the resolver dependency graph, provider
usage summary, and an aggregate summary.

SOLUTION SOURCE:
  Solutions can be loaded from:
  - Local file or catalog: Use -f flag (e.g., -f ./solution.yaml or -f my-app@1.2.3)
  - URL: Provide an HTTP(S) URL via -f/--file (also detected as a positional arg)
  - Auto-discovery: If no source is specified, searches for solution.yaml in current directory

RESOLVER SELECTION:
  Pass resolver names as positional arguments to execute only specific
  resolvers and their transitive dependencies. When no names are provided,
  all resolvers in the solution are executed.

  Examples:
    scafctl run resolver                           Execute all resolvers (auto-discovery)
    scafctl run resolver db config                 Execute 'db', 'config', and their deps
    scafctl run resolver db config -f my-app       Execute from catalog, filter to db + config

SKIPPING PHASES:
  Use --skip-validation to skip the validation phase of all resolvers.
  Use --skip-transform to skip both the transform and validation phases,
  returning only the raw resolved values. This is useful for inspecting
  what providers return before any transformation.

GRAPH MODE:
  Use --graph to visualize the resolver dependency graph without executing
  any providers. Shows execution phases, parallelization opportunities,
  dependencies, and the critical path.

  Supported formats (--graph-format):
    ascii   - Human-readable ASCII art (default)
    dot     - Graphviz DOT format (pipe to 'dot' command for PNG/SVG)
    mermaid - Mermaid diagram syntax
    json    - Machine-readable JSON format

SNAPSHOT MODE:
  Use --snapshot to save a full execution snapshot to a file. Snapshots
  capture resolver values, timing, phases, parameters, and metadata for
  debugging, testing, comparison, and audit trails.

`+ResolverParametersHelp+`

OUTPUT FORMATS:
  table    Bordered table view (default when terminal)
  json     JSON output (for piping/scripting)
  yaml     YAML output (for piping/scripting)
  quiet    Suppress output (exit code only)

EXIT CODES:
  0  Success
  1  Resolver execution failed
  2  Validation failed
  3  Invalid solution (cycle/parse error)
  4  File not found

Examples:
  # Run all resolvers (auto-discovery)
  scafctl run resolver

  # Run specific resolvers (with their dependencies)
  scafctl run resolver db config

  # Run all resolvers from a solution file
  scafctl run resolver -f ./my-solution.yaml

  # Run specific resolvers from catalog
  scafctl run resolver db config -f my-app

  # Run all resolvers from specific catalog version
  scafctl run resolver -f my-app@1.2.3

  # Run with parameters (positional key=value — recommended)
  scafctl run resolver -f ./my-solution.yaml env=prod region=us-east1

  # Run with parameters (explicit flag)
  scafctl run resolver -r env=prod -r region=us-east1

  # Mix positional and flag parameters
  scafctl run resolver -f ./my-solution.yaml -r env=prod region=us-east1

  # Load parameters from a file (positional)
  scafctl run resolver -f ./my-solution.yaml @params.yaml

  # Load parameters from stdin (pipe YAML or JSON)
  echo '{"env": "prod"}' | scafctl run resolver -f ./my-solution.yaml @-

  # Pipe parameters from another command
  cat params.yaml | scafctl run resolver -f ./my-solution.yaml -r @-

  # Pipe raw stdin into a single parameter
  echo hello | scafctl run resolver -f ./my-solution.yaml message=@-

  # Read a file's raw content into a parameter
  scafctl run resolver -f ./my-solution.yaml body=@content.txt

  # JSON output for scripting
  scafctl run resolver -f ./my-solution.yaml -o json | jq .

  # Skip transform and validation phases (raw resolved values)
  scafctl run resolver --skip-transform -f ./my-solution.yaml

  # Show resolver dependency graph (ASCII)
  scafctl run resolver --graph -f ./my-solution.yaml

  # Generate PNG graph using Graphviz
  scafctl run resolver --graph --graph-format=dot -f ./my-solution.yaml | dot -Tpng > graph.png

  # Generate Mermaid diagram
  scafctl run resolver --graph --graph-format=mermaid -f ./my-solution.yaml

  # Save execution snapshot
  scafctl run resolver --snapshot --snapshot-file=snapshot.json -f ./my-solution.yaml

  # Save snapshot with sensitive data redacted
  scafctl run resolver --snapshot --snapshot-file=snapshot.json --redact -f ./my-solution.yaml

  # Explore results interactively
  scafctl run resolver -f ./my-solution.yaml -i

  # Show execution progress
  scafctl run resolver --progress -f ./my-solution.yaml

  # Show provider metrics
  scafctl run resolver --show-metrics -f ./my-solution.yaml

  # Show only the resolvers consumed by a specific action
  scafctl run resolver --action tag -f ./my-solution.yaml

  # Union resolvers from multiple actions
  scafctl run resolver --action tag --action release -f ./my-solution.yaml

  # Run from catalog without -f (auto-fallback)
  scafctl run resolver ford-proxy`, settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.ArbitraryArgs,
		PreRun: func(cCmd *cobra.Command, args []string) {
			// Track which flags were explicitly set by the user
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
			fileExplicit := options.flagsChanged["file"]
			parseResolverArgs(args, options, fileExplicit)
		},
		RunE:         makeRunEFunc(cfg, "resolver"),
		SilenceUsage: true,
	}

	// Shared resolver flags
	addSharedResolverFlags(cCmd, &options.sharedResolverOptions)

	// Resolver-specific flags
	cCmd.Flags().BoolVar(&options.SkipTransform, "skip-transform", false, "Skip transform and validation phases, returning raw resolved values")
	cCmd.Flags().BoolVar(&options.Graph, "graph", false, "Show resolver dependency graph instead of executing")
	cCmd.Flags().StringVar(&options.GraphFormat, "graph-format", "ascii", "Graph output format: ascii, dot, mermaid, json")
	cCmd.Flags().BoolVar(&options.Snapshot, "snapshot", false, "Save execution snapshot instead of normal output")
	cCmd.Flags().StringVar(&options.SnapshotFile, "snapshot-file", "", "Snapshot output file (required with --snapshot)")
	cCmd.Flags().BoolVar(&options.Redact, "redact", false, "Redact sensitive values in snapshot")
	cCmd.Flags().BoolVar(&options.ShowExecution, "show-execution", false, "Include __execution metadata (phases, timing, dependencies, providers) in output")
	cCmd.Flags().BoolVar(&options.FailOnValidation, "fail-on-validation", false, "Exit non-zero when any resolver fails validation (by default validation failures are non-fatal diagnostics)")
	cCmd.Flags().StringArrayVar(&options.Actions, "action", nil, "Scope resolver output to the resolvers consumed by this action (repeatable)")

	setResolverHelpFunc(cCmd)

	return cCmd
}

// CommandValidateResolver creates the 'validate resolver' subcommand. It reuses
// the resolver execution machinery but always treats validation failures as
// fatal (exit code 2), making it a validation gate suitable for CI. Unlike
// 'run resolver', it does not expose graph/snapshot modes — its sole purpose is
// to validate resolver outputs and report failures.
func CommandValidateResolver(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &ResolverOptions{FailOnValidation: true}

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
		},
	}

	cCmd := &cobra.Command{
		Use:     "resolver [resolver-name...] [key=value...]",
		Aliases: []string{"res", "resolvers"},
		Short:   "Validate resolvers and fail when validation does not pass",
		Long: strings.ReplaceAll(`Validate a solution's resolvers and exit non-zero on validation failure.

This command executes the resolvers (resolve, transform, and validate phases)
and reports any validation failures as errors. Unlike 'run resolver', which
treats validation failures as non-fatal diagnostics and exits 0, this command
exits with code 2 when any resolver fails validation. Use it as a validation
gate in CI pipelines or pre-commit checks.

Resolved values are still printed so failures can be inspected. Input
parameters and solution sources work exactly as in 'run resolver'.

`+ResolverParametersHelp+`

EXIT CODES:
  0  All resolvers validated successfully
  1  Resolver execution failed
  2  Validation failed
  3  Invalid solution (cycle/parse error)
  4  File not found

Examples:
  # Validate all resolvers (auto-discovery)
  scafctl validate resolver

  # Validate resolvers from a solution file
  scafctl validate resolver -f ./my-solution.yaml

  # Validate with parameters
  scafctl validate resolver -f ./my-solution.yaml env=prod region=us-east1

  # Validate specific resolvers (with their dependencies)
  scafctl validate resolver db config -f ./my-solution.yaml`, settings.CliBinaryName, cliParams.BinaryName),
		Args: cobra.ArbitraryArgs,
		PreRun: func(cCmd *cobra.Command, args []string) {
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
			fileExplicit := options.flagsChanged["file"]
			parseResolverArgs(args, options, fileExplicit)
		},
		RunE:         makeRunEFunc(cfg, "resolver"),
		SilenceUsage: true,
	}

	addSharedResolverFlags(cCmd, &options.sharedResolverOptions)
	cCmd.Flags().BoolVar(&options.ShowExecution, "show-execution", false, "Include __execution metadata (phases, timing, dependencies, providers) in output")
	cCmd.Flags().StringArrayVar(&options.Actions, "action", nil, "Scope validation to the resolvers consumed by this action (repeatable)")

	setResolverHelpFunc(cCmd)

	return cCmd
}

// parseResolverArgs splits positional args into resolver names and dynamic parameters.
// Bare words are resolver names, args containing '=' or starting with '@' are parameters.
// URLs (http(s)://, oci://) and unambiguous catalog references (versioned refs,
// registry refs) are auto-detected as solution refs when no -f flag is set.
// Bare names like "my-resolver" are always treated as resolver names.
func parseResolverArgs(args []string, options *ResolverOptions, fileExplicit bool) {
	for _, arg := range args {
		switch {
		case !fileExplicit && options.File == "" && (filepath.IsURL(arg) || get.IsUnambiguousCatalogReference(arg)):
			options.File = arg
			fileExplicit = true
		case strings.Contains(arg, "=") || strings.HasPrefix(arg, "@"):
			options.DynamicArgs = append(options.DynamicArgs, arg)
		default:
			options.Names = append(options.Names, arg)
		}
	}
}

// Run executes the resolver-only flow
func (o *ResolverOptions) Run(ctx context.Context) error {
	if o.BinaryName == "" {
		o.BinaryName = settings.CliBinaryName
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

	// Global --verbose implies --show-execution for resolvers
	if o.CliParams != nil && o.CliParams.Verbose {
		o.ShowExecution = true
	}

	// Prefer solution files during auto-discovery; actions.yaml is irrelevant
	// for resolver execution and should not trigger ambiguity warnings.
	o.discoveryMode = settings.DiscoveryModeSolution

	lgr.V(1).Info("running resolver",
		"file", o.File,
		"output", o.Output,
		"names", o.Names,
		"skipTransform", o.SkipTransform,
		"graph", o.Graph,
		"snapshot", o.Snapshot,
		"resolveAll", o.ResolveAll,
		"progress", o.Progress,
		"showMetrics", o.ShowMetrics,
		"showExecution", o.ShowExecution)

	// Validate mutually exclusive modes
	if o.Graph && o.Snapshot {
		return o.exitWithCode(ctx,
			fmt.Errorf("--graph and --snapshot are mutually exclusive"),
			exitcode.InvalidInput)
	}

	// Validate snapshot requirements
	if o.Snapshot && o.SnapshotFile == "" {
		return o.exitWithCode(ctx,
			fmt.Errorf("--snapshot-file is required when using --snapshot"),
			exitcode.InvalidInput)
	}

	// Detect @- / -f - conflict early: stdin can only be consumed once.
	// Check both -r flags and positional dynamic args before anything reads stdin.
	if o.File == "-" && (flags.ContainsStdinRef(o.ResolverParams) || flags.ContainsStdinRef(o.DynamicArgs)) {
		return o.exitWithCode(ctx,
			fmt.Errorf("cannot use both -f - and @-: stdin can only be read once"),
			exitcode.InvalidInput)
	}

	// Track whether the file was explicitly provided (via -f flag or as a
	// positional unambiguous catalog reference). Auto-discovered files set
	// o.File inside prepareSolutionForExecution, so we capture the state here
	// to enable catalog fallback when auto-discovery finds a non-solution file.
	fileWasExplicit := o.File != ""

	// Prepare solution: load, set up registry, handle bundles
	sol, reg, solutionDir, cleanup, providerCtx, err := o.prepareSolutionForExecution(ctx)
	if err != nil {
		// When no explicit file was provided and auto-discovery either found
		// nothing (ErrNoSolutionFound) or picked a known non-solution file
		// (taskfile.yaml/yml), check if the first positional arg looks like a
		// catalog reference and retry using it as the solution source.
		// We intentionally do NOT retry when a real solution file was discovered
		// but failed to parse -- that would mask useful validation errors.
		canRetry := errors.Is(err, get.ErrNoSolutionFound) || isNonSolutionDiscoveryFile(o.File)
		if !fileWasExplicit && canRetry && len(o.Names) > 0 && get.IsCatalogReference(o.Names[0]) {
			o.File = o.Names[0]
			o.Names = o.Names[1:]
			lgr.V(1).Info("retrying with first positional arg as catalog reference", "file", o.File)
			sol, reg, solutionDir, cleanup, providerCtx, err = o.prepareSolutionForExecution(ctx)
		}
		if err != nil {
			return o.exitWithCode(ctx, err, exitcode.FileNotFound)
		}
	}

	// Validate mutually exclusive --action and positional resolver names.
	// This check is placed after catalog fallback so that bare catalog names
	// (parsed into o.Names) can be consumed by fallback first.
	if len(o.Actions) > 0 && len(o.Names) > 0 {
		return o.exitWithCode(ctx,
			fmt.Errorf("--action and positional resolver names are mutually exclusive"),
			exitcode.InvalidInput)
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
	//
	// For bundle runs (catalog: prefix with non-empty solutionDir), only set
	// SolutionDirectory so resolver paths resolve against the bundle extraction
	// directory. For unbundled catalog runs, solutionDir is empty and paths
	// fall back to the process CWD.
	isBundleRun := strings.HasPrefix(sol.GetPath(), "catalog:") && solutionDir != ""
	switch {
	case o.BaseDir != "":
		absBaseDir, baseDirErr := stdfilepath.Abs(o.BaseDir)
		if baseDirErr != nil {
			return o.exitWithCode(ctx, fmt.Errorf("--base-dir: %w", baseDirErr), exitcode.InvalidInput)
		}
		ctx = provider.WithSolutionDirectory(ctx, absBaseDir)
	case isBundleRun:
		ctx = provider.WithSolutionDirectory(ctx, solutionDir)
	case solutionDir != "":
		ctx = provider.WithSolutionDirectory(ctx, solutionDir)
	}

	// Parse dynamic positional arguments (key=value and @file.yaml from argv)
	extraParsed, err := flags.ParseDynamicInputArgs(o.DynamicArgs)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to parse positional parameters: %w", err), exitcode.ValidationFailed)
	}

	// Merge: -r flag values first, then positional args (last-wins on conflict)
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

	// Get all resolvers, then filter by names if specified
	allResolvers := sol.Spec.ResolversToSlice()

	// --action: extract resolver names consumed by the specified action(s)
	if len(o.Actions) > 0 {
		actionNames, actionErr := o.resolveActionResolverNames(sol)
		if actionErr != nil {
			return o.exitWithCode(ctx, actionErr, exitcode.InvalidInput)
		}
		o.Names = actionNames
		lgr.V(1).Info("--action resolved to resolver names", "actions", o.Actions, "resolvers", actionNames)
	}

	// Validate parameter keys against parameter provider 'key' inputs (early typo detection)
	if len(params) > 0 {
		paramKeys := extractParameterKeys(allResolvers)
		if len(paramKeys) > 0 {
			if err := flags.ValidateInputKeys(params, paramKeys, "solution"); err != nil {
				return o.exitWithCode(ctx, err, exitcode.InvalidInput)
			}
		}
	}

	var lookup resolver.DescriptorLookup
	if reg != nil {
		lookup = reg.DescriptorLookup()
	}
	resolvers := execute.FilterResolversWithDependencies(allResolvers, o.Names, lookup)

	// Validate requested names exist
	if len(o.Names) > 0 {
		resolverMap := make(map[string]bool)
		for _, r := range allResolvers {
			resolverMap[r.Name] = true
		}
		var unknown []string
		for _, name := range o.Names {
			if !resolverMap[name] {
				unknown = append(unknown, name)
			}
		}
		if len(unknown) > 0 {
			return o.exitWithCode(ctx,
				fmt.Errorf("unknown resolver(s): %s (available: %s)",
					strings.Join(unknown, ", "),
					resolverNamesString(allResolvers)),
				exitcode.InvalidInput)
		}
	}

	// Graph mode: show dependency graph without executing providers
	if o.Graph {
		return o.showResolverGraph(ctx, resolvers, reg)
	}

	// Snapshot mode: execute resolvers and save snapshot
	if o.Snapshot {
		return o.showResolverSnapshot(ctx, sol, resolvers, params, reg)
	}

	// State lifecycle: load persisted state before resolver execution so that
	// the state provider can serve previously saved values.
	var stateMgr *state.Manager
	var stateData *state.Data
	if o.NoState {
		warnStateSkipped(ctx, sol)
	} else if sol.State != nil {
		stateMgr = state.NewManager(sol.State, reg, state.RuntimeProvenanceFromContext(ctx))
		cmdInfo := buildCommandInfo("run resolver", params)
		loadResult, loadErr := stateMgr.Load(ctx, params, cmdInfo)
		if loadErr != nil {
			return o.handleStateLoadError(ctx, loadErr)
		}
		if !loadResult.Skipped {
			ctx = loadResult.Ctx
			stateData = loadResult.Data
			params = loadResult.MergedParams
		}
	}

	// Wire skip-transform flag into shared options for executeResolvers
	if o.SkipTransform {
		o.sharedResolverOptions.SkipTransform = true
	}

	// run resolver is an inspection command: validation failures must never
	// withhold the produced values. Enable non-fatal mode so executeResolvers
	// returns partial values plus diagnostics instead of aborting.
	o.nonFatalValidation = true

	// Track timing
	start := time.Now()

	// Execute resolvers
	resolverData, resolverCtx, execErr := o.executeResolvers(ctx, sol, resolvers, params, reg)
	if execErr != nil && resolverCtx == nil {
		// A hard failure occurred before any values could be produced
		// (e.g., phase build error). Abort with the error.
		return o.exitWithCode(ctx, execErr, exitcode.GeneralError)
	}

	elapsed := time.Since(start)

	// State lifecycle: save merged parameters and check immutable values
	// after successful resolver execution. Skip the save when validation failed
	// so invalid values are never persisted.
	if stateMgr != nil && stateData != nil && execErr == nil {
		solMeta := buildStateSolutionMeta(sol)
		if saveErr := stateMgr.Save(ctx, stateData, resolverCtx, resolvers, params, resolverData, solMeta); saveErr != nil {
			return o.exitWithCode(ctx, fmt.Errorf("state save: %w", saveErr), exitcode.GeneralError)
		}
	}

	// Build output and write
	results := o.buildResolverOutputMap(resolverData, sol)
	if err := o.checkValueSizes(results, *lgr); err != nil {
		return o.exitWithCode(ctx, err, exitcode.ValidationFailed)
	}

	// Include __execution metadata only when --show-execution is set
	if o.ShowExecution {
		executionData := execute.BuildExecutionData(resolverCtx, resolvers, elapsed)

		// Build and embed the resolver dependency graph
		graph, graphErr := resolver.BuildGraph(resolvers, lookup)
		if graphErr == nil {
			if err := graph.RenderDiagrams(); err != nil {
				lgr.V(1).Info("failed to render dependency graph diagrams", "error", err)
			}
			// Convert to map[string]any so CEL expressions can traverse the graph
			graphJSON, err := json.Marshal(graph)
			if err == nil {
				var graphMap map[string]any
				if err := json.Unmarshal(graphJSON, &graphMap); err == nil {
					executionData["dependencyGraph"] = graphMap
				} else {
					lgr.V(1).Info("failed to unmarshal dependency graph", "error", err)
				}
			} else {
				lgr.V(1).Info("failed to marshal dependency graph", "error", err)
			}
		} else {
			lgr.V(1).Info("failed to build dependency graph for __execution", "error", graphErr)
		}

		// Embed provider usage summary
		executionData["providerSummary"] = execute.BuildProviderSummary(resolverCtx, resolvers)

		results["__execution"] = executionData
	}

	// Surface non-fatal validation diagnostics on stderr so the resolved values
	// (on stdout) remain useful for troubleshooting and machine consumption.
	if execErr != nil {
		o.renderValidationDiagnostics(ctx, execErr)
	}

	// When -o test: generate a functional test definition instead of normal output.
	if o.Output == "test" {
		return o.generateTestOutput(ctx, []string{"run", "resolver"}, o.Names, results)
	}

	if err := o.writeResolverOutput(ctx, results, o.BinaryName+" run resolver"); err != nil {
		return err
	}

	// In non-fatal mode the values are always shown. When --fail-on-validation
	// is set, exit non-zero so CI/gating callers detect the failure.
	if execErr != nil && o.FailOnValidation {
		return exitcode.WithCode(execErr, exitcode.ValidationFailed)
	}

	return nil
}

// renderValidationDiagnostics writes a human-readable summary of resolver
// validation-only failures to stderr. It is used by the non-fatal inspection
// path so the resolved values on stdout are not polluted. Resolve- and
// transform-phase failures remain fatal and are reported earlier.
func (o *ResolverOptions) renderValidationDiagnostics(ctx context.Context, execErr error) {
	w := writer.FromContext(ctx)
	if w == nil {
		// Fall back to a writer built from the command's IO streams so
		// diagnostics are still emitted when the caller did not seed a writer
		// into the context (e.g. embedders or direct Run invocations).
		if o.IOStreams != nil {
			cliParams := o.CliParams
			if cliParams == nil {
				// Writer methods dereference cliParams; default it so direct
				// ResolverOptions.Run() calls outside the cobra wiring do not panic.
				cliParams = &settings.Run{}
			}
			w = writer.New(o.IOStreams, cliParams)
		} else {
			return
		}
	}

	diags := execute.DiagnosticsFromError(execErr)
	if len(diags) == 0 {
		w.WarnStderrf("resolver validation failed: %v", execErr)
		return
	}

	w.WarnStderrf("%d resolver(s) failed validation:", len(diags))
	for _, d := range diags {
		if d.Resolver != "" {
			w.PlainStderrf("  - %s: %s", d.Resolver, d.Message)
		} else {
			w.PlainStderrf("  - %s", d.Message)
		}
	}
	if !o.FailOnValidation {
		w.PlainStderrf("(values shown above; pass --fail-on-validation to exit non-zero)")
	}
}

// showResolverGraph renders the resolver dependency graph without executing providers
func (o *ResolverOptions) showResolverGraph(ctx context.Context, resolvers []*resolver.Resolver, reg *provider.Registry) error {
	var lookup resolver.DescriptorLookup
	if reg != nil {
		lookup = reg.DescriptorLookup()
	}

	graph, err := resolver.BuildGraph(resolvers, lookup)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to build dependency graph: %w", err), exitcode.InvalidInput)
	}

	if err := execute.RenderGraph(o.IOStreams.Out, graph, graph, o.GraphFormat); err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to render graph: %w", err), exitcode.GeneralError)
	}

	return nil
}

// showResolverSnapshot executes resolvers and saves the execution state as a snapshot file
func (o *ResolverOptions) showResolverSnapshot(
	ctx context.Context,
	sol *solution.Solution,
	resolvers []*resolver.Resolver,
	params map[string]any,
	reg *provider.Registry,
) error {
	lgr := logger.FromContext(ctx)

	// Wire skip-transform flag into shared options for executeResolvers
	if o.SkipTransform {
		o.sharedResolverOptions.SkipTransform = true
	}

	// Capture the snapshot even when validation fails: non-fatal mode keeps the
	// populated resolver context so the snapshot reflects partial values.
	o.nonFatalValidation = true

	start := time.Now()

	// Execute resolvers
	_, resolverCtx, err := o.executeResolvers(ctx, sol, resolvers, params, reg)
	elapsed := time.Since(start)

	status := resolver.ExecutionStatusSuccess
	if err != nil {
		lgr.V(1).Info("resolver execution completed with errors", "error", err)
		status = resolver.ExecutionStatusFailed
		// Continue to capture snapshot even with errors
	}
	if resolverCtx == nil {
		resolverCtx = resolver.NewContext()
	}

	// Re-inject resolver context into context.Context for CaptureSnapshot
	snapshotCtx := resolver.WithContext(ctx, resolverCtx)

	versionStr := ""
	if sol.Metadata.Version != nil {
		versionStr = sol.Metadata.Version.String()
	}

	snapshot, err := resolver.CaptureSnapshot(
		snapshotCtx,
		sol.Metadata.Name,
		versionStr,
		settings.VersionInformation.BuildVersion,
		params,
		elapsed,
		status,
	)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to capture snapshot: %w", err), exitcode.GeneralError)
	}

	// Redact sensitive values if requested
	if o.Redact {
		lgr.V(1).Info("redacting sensitive values")
		resolverLikes := make([]resolver.ResolverLike, 0, len(resolvers))
		for _, r := range resolvers {
			resolverLikes = append(resolverLikes, &resolverAdapter{name: r.Name, sensitive: r.Sensitive})
		}
		resolver.RedactSensitiveValues(snapshot, resolverLikes)
	}

	// Save snapshot
	lgr.V(1).Info("saving snapshot", "output", o.SnapshotFile)
	if err := resolver.SaveSnapshot(snapshot, o.SnapshotFile); err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to save snapshot: %w", err), exitcode.GeneralError)
	}

	if w := writer.FromContext(ctx); w != nil {
		w.Successf("Snapshot saved to %s", o.SnapshotFile)
		solutionLine := fmt.Sprintf("  Solution: %s", snapshot.Metadata.Solution)
		if snapshot.Metadata.Version != "" {
			solutionLine += fmt.Sprintf(" (v%s)", snapshot.Metadata.Version)
		}
		w.Plainln(solutionLine)
		w.Plainlnf("  Resolvers: %d", len(snapshot.Resolvers))
		w.Plainlnf("  Duration: %s", snapshot.Metadata.TotalDuration)
		w.Plainlnf("  Status: %s", snapshot.Metadata.Status)
	}

	return nil
}

// resolverNamesString returns a comma-separated string of resolver names
func resolverNamesString(resolvers []*resolver.Resolver) string {
	names := make([]string, len(resolvers))
	for i, r := range resolvers {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}

// resolverAdapter adapts a Resolver's fields to the ResolverLike interface
type resolverAdapter struct {
	name      string
	sensitive bool
}

func (a *resolverAdapter) GetName() string    { return a.name }
func (a *resolverAdapter) GetSensitive() bool { return a.sensitive }

// setResolverHelpFunc installs a custom help function that appends dynamic
// resolver input documentation when a solution file is available.
// For example, `scafctl run resolver -f solution.yaml --help` will show the
// standard command help plus the solution's resolver parameter table.
func setResolverHelpFunc(cmd *cobra.Command) {
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(c *cobra.Command, args []string) {
		// Render the default help first
		defaultHelp(c, args)

		// Try to determine the solution file path from the -f flag or auto-discovery
		solutionPath := extractSolutionPath(c)
		if solutionPath == "" {
			return
		}

		// Load the solution (best effort — don't fail help on errors)
		sol, err := inspect.LoadSolution(c.Context(), solutionPath)
		if err != nil {
			return
		}

		helpText := resolverdetail.FormatResolverInputHelp(sol)
		if helpText != "" {
			fmt.Fprintln(c.OutOrStdout())
			fmt.Fprint(c.OutOrStdout(), helpText)
		}
	})
}

// resolveActionResolverNames looks up the specified action(s) in the solution's
// workflow and extracts the resolver names referenced by their inputs, forEach.in,
// and when conditions. Multiple actions are unioned. Returns an error if the
// solution has no workflow or an action does not exist.
func (o *ResolverOptions) resolveActionResolverNames(sol *solution.Solution) ([]string, error) {
	if !sol.Spec.HasWorkflow() {
		return nil, fmt.Errorf("--action requires a solution with a workflow section, but %q has none", sol.Metadata.Name)
	}

	workflow := sol.Spec.Workflow
	unioned := make(map[string]bool)

	for _, actionName := range o.Actions {
		act, found := workflow.Actions[actionName]
		if !found {
			// Also check finally actions
			act, found = workflow.Finally[actionName]
		}
		if !found {
			// Build available action names for the error message
			available := make([]string, 0, len(workflow.Actions))
			for name := range workflow.Actions {
				available = append(available, name)
			}
			for name := range workflow.Finally {
				available = append(available, name)
			}
			sort.Strings(available)
			return nil, fmt.Errorf("action %q not found (available: %s)", actionName, strings.Join(available, ", "))
		}

		// Build a set of forEach alias names to exclude from extracted refs.
		// These are iteration variables, not resolver names.
		forEachAliases := make(map[string]bool)
		if act.ForEach != nil {
			if act.ForEach.Item != "" {
				forEachAliases[act.ForEach.Item] = true
			}
			if act.ForEach.Index != "" {
				forEachAliases[act.ForEach.Index] = true
			}
		}

		// Extract resolver names from the action's inputs
		for _, name := range resolver.ExtractRefsFromValueRefs(act.Inputs) {
			if !forEachAliases[name] {
				unioned[name] = true
			}
		}

		// Extract from forEach.in if present
		if act.ForEach != nil && act.ForEach.In != nil {
			for _, name := range resolver.ExtractRefsFromValueRefs(map[string]*resolver.ValueRef{"in": act.ForEach.In}) {
				unioned[name] = true
			}
		}

		// Extract from the when condition if present
		if act.When != nil && act.When.Expr != nil {
			celExpr := celexp.Expression(*act.When.Expr)
			vars, err := celExpr.GetUnderscoreVariables(context.TODO())
			if err != nil {
				return nil, fmt.Errorf("failed to parse when condition for action %q: %w", actionName, err)
			}
			for _, v := range vars {
				if !forEachAliases[v] {
					unioned[v] = true
				}
			}
		}
	}

	if len(unioned) == 0 {
		if len(o.Actions) == 1 {
			return nil, fmt.Errorf("action %q does not reference any resolvers in its inputs or conditions", o.Actions[0])
		}
		return nil, fmt.Errorf("actions %v do not reference any resolvers in their inputs or conditions", o.Actions)
	}

	names := make([]string, 0, len(unioned))
	for name := range unioned {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// isNonSolutionDiscoveryFile returns true if the given path refers to a file
// that auto-discovery may resolve but is not a valid solution file. When such
// a file is found and loading fails, the catalog fallback is safe to try.
func isNonSolutionDiscoveryFile(path string) bool {
	base := stdfilepath.Base(path)
	return base == "taskfile.yaml" || base == "taskfile.yml"
}

// extractSolutionPath determines the solution file path from:
// 1. The -f/--file flag value (most reliable)
// 2. os.Args as a fallback (scanning for -f flag)
// 3. Auto-discovery in the current directory
func extractSolutionPath(c *cobra.Command) string {
	// Try the parsed flag value
	if f := c.Flags().Lookup("file"); f != nil && f.Value.String() != "" {
		return f.Value.String()
	}

	// Fallback: scan os.Args for -f or --file
	osArgs := os.Args
	for i, arg := range osArgs {
		if (arg == "-f" || arg == "--file") && i+1 < len(osArgs) {
			return osArgs[i+1]
		}
		if strings.HasPrefix(arg, "-f=") {
			return strings.TrimPrefix(arg, "-f=")
		}
		if strings.HasPrefix(arg, "--file=") {
			return strings.TrimPrefix(arg, "--file=")
		}
	}

	// Final fallback: auto-discover solution file in the current directory
	return get.NewGetterFromContext(c.Context()).FindSolution()
}
