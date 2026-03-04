// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// ResolverOptions holds configuration for the run resolver command
type ResolverOptions struct {
	sharedResolverOptions

	// Names is the list of resolver names to execute (positional args).
	// If empty, all resolvers are executed.
	Names []string

	// SkipTransform skips the transform and validation phases,
	// returning raw resolved values.
	SkipTransform bool

	// DryRun shows the execution plan without running providers.
	DryRun bool

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

	// HideExecution suppresses the __execution metadata from output.
	HideExecution bool
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
			options.IOStreams = ios
			options.CliParams = cli
		},
	}

	cCmd := &cobra.Command{
		Use:     "resolver [name...]",
		Aliases: []string{"res", "resolvers"},
		Short:   "Execute resolvers for debugging and inspection",
		Long: `Execute resolvers from a solution without running actions.

This command is designed for debugging and inspecting resolver execution.
It loads a solution file and executes only the resolvers, skipping the
action/workflow phase entirely.

By default, the output includes a __execution key containing per-resolver
execution metadata: phase numbers, timing, provider info, dependencies, the
resolver dependency graph, provider usage summary, and an aggregate summary.
Use --hide-execution to suppress this metadata for cleaner output.

RESOLVER SELECTION:
  Pass resolver names as positional arguments to execute only specific
  resolvers and their transitive dependencies. When no names are provided,
  all resolvers in the solution are executed.

  Examples:
    scafctl run resolver                    Execute all resolvers
    scafctl run resolver db config          Execute 'db', 'config', and their deps
    scafctl run resolver auth -f sol.yaml   Execute 'auth' and its deps

SKIPPING PHASES:
  Use --skip-validation to skip the validation phase of all resolvers.
  Use --skip-transform to skip both the transform and validation phases,
  returning only the raw resolved values. This is useful for inspecting
  what providers return before any transformation.

DRY RUN:
  Use --dry-run to show the execution plan without running any providers.
  This displays the DAG-based execution phases, resolver dependencies,
  provider types, and configured phases for each resolver.

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

RESOLVER PARAMETERS:
  Parameters can be passed using -r/--resolver flag in several formats:
    key=value         Simple key-value pair
    @file.yaml        Load parameters from YAML file
    @file.json        Load parameters from JSON file
    key=val1,val2     Multiple values become an array

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
  # Run all resolvers from a solution file
  scafctl run resolver -f ./my-solution.yaml

  # Run specific resolvers (with their dependencies)
  scafctl run resolver db config -f ./my-solution.yaml

  # Run with parameters
  scafctl run resolver -r env=prod -r region=us-east1

  # JSON output for scripting
  scafctl run resolver -f ./my-solution.yaml -o json | jq .

  # Skip transform and validation phases (raw resolved values)
  scafctl run resolver --skip-transform -f ./my-solution.yaml

  # Show execution plan without running providers
  scafctl run resolver --dry-run -f ./my-solution.yaml

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
  scafctl run resolver --show-metrics -f ./my-solution.yaml`,
		Args: cobra.ArbitraryArgs,
		PreRun: func(cCmd *cobra.Command, args []string) {
			// Track which flags were explicitly set by the user
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
			// Store positional args as resolver names
			options.Names = args
		},
		RunE:         makeRunEFunc(cfg, "resolver"),
		SilenceUsage: true,
	}

	// Shared resolver flags
	addSharedResolverFlags(cCmd, &options.sharedResolverOptions)

	// Resolver-specific flags
	cCmd.Flags().BoolVar(&options.SkipTransform, "skip-transform", false, "Skip transform and validation phases, returning raw resolved values")
	cCmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Show execution plan without running providers")
	cCmd.Flags().BoolVar(&options.Graph, "graph", false, "Show resolver dependency graph instead of executing")
	cCmd.Flags().StringVar(&options.GraphFormat, "graph-format", "ascii", "Graph output format: ascii, dot, mermaid, json")
	cCmd.Flags().BoolVar(&options.Snapshot, "snapshot", false, "Save execution snapshot instead of normal output")
	cCmd.Flags().StringVar(&options.SnapshotFile, "snapshot-file", "", "Snapshot output file (required with --snapshot)")
	cCmd.Flags().BoolVar(&options.Redact, "redact", false, "Redact sensitive values in snapshot")
	cCmd.Flags().BoolVar(&options.HideExecution, "hide-execution", false, "Suppress __execution metadata from output")

	return cCmd
}

// Run executes the resolver-only flow
func (o *ResolverOptions) Run(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("running resolver",
		"file", o.File,
		"output", o.Output,
		"names", o.Names,
		"skipTransform", o.SkipTransform,
		"dryRun", o.DryRun,
		"graph", o.Graph,
		"snapshot", o.Snapshot,
		"resolveAll", o.ResolveAll,
		"progress", o.Progress,
		"showMetrics", o.ShowMetrics)

	// Validate mutually exclusive modes
	modeCount := 0
	if o.DryRun {
		modeCount++
	}
	if o.Graph {
		modeCount++
	}
	if o.Snapshot {
		modeCount++
	}
	if modeCount > 1 {
		return o.exitWithCode(ctx,
			fmt.Errorf("--dry-run, --graph, and --snapshot are mutually exclusive"),
			exitcode.InvalidInput)
	}

	// Validate snapshot requirements
	if o.Snapshot && o.SnapshotFile == "" {
		return o.exitWithCode(ctx,
			fmt.Errorf("--snapshot-file is required when using --snapshot"),
			exitcode.InvalidInput)
	}

	// Prepare solution: load, set up registry, handle bundles
	sol, reg, cleanup, err := o.prepareSolutionForExecution(ctx)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.FileNotFound)
	}
	defer cleanup()

	// Parse resolver parameters
	params, err := ParseResolverFlags(o.ResolverParams)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to parse resolver parameters: %w", err), exitcode.ValidationFailed)
	}

	lgr.V(1).Info("parsed parameters", "count", len(params))

	// Get all resolvers, then filter by names if specified
	allResolvers := sol.Spec.ResolversToSlice()
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

	// Dry run: show execution plan without running providers
	if o.DryRun {
		return o.showResolverDryRun(ctx, resolvers, reg)
	}

	// Snapshot mode: execute resolvers and save snapshot
	if o.Snapshot {
		return o.showResolverSnapshot(ctx, sol, resolvers, params, reg)
	}

	// Wire skip-transform flag into shared options for executeResolvers
	if o.SkipTransform {
		o.sharedResolverOptions.SkipTransform = true
	}

	// Track timing
	start := time.Now()

	// Execute resolvers
	resolverData, resolverCtx, err := o.executeResolvers(ctx, sol, resolvers, params, reg)
	if err != nil {
		return o.exitWithCode(ctx, err, exitcode.GeneralError)
	}

	elapsed := time.Since(start)

	// Build output and write
	results := o.buildResolverOutputMap(resolverData, sol)
	if err := o.checkValueSizes(results, *lgr); err != nil {
		return o.exitWithCode(ctx, err, exitcode.ValidationFailed)
	}

	// Include __execution metadata unless --hide-execution is set
	if !o.HideExecution {
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

	// When -o test: generate a functional test definition instead of normal output.
	if o.Output == "test" {
		return o.generateTestOutput(ctx, []string{"run", "resolver"}, o.Names, results)
	}

	return o.writeResolverOutput(ctx, results, "scafctl run resolver")
}

// showResolverDryRun displays the execution plan without running providers
func (o *ResolverOptions) showResolverDryRun(ctx context.Context, resolvers []*resolver.Resolver, reg *provider.Registry) error {
	// Build execution phases from the resolver DAG
	var lookup resolver.DescriptorLookup
	if reg != nil {
		lookup = reg.DescriptorLookup()
	}
	phases, err := resolver.BuildPhases(resolvers, lookup)
	if err != nil {
		return o.exitWithCode(ctx, fmt.Errorf("failed to build execution plan: %w", err), exitcode.InvalidInput)
	}

	// Build structured dry-run output
	plan := buildDryRunPlan(phases, resolvers, o.SkipTransform, o.SkipValidation)

	return o.writeResolverOutput(ctx, plan, "scafctl run resolver --dry-run")
}

// buildDryRunPlan constructs the structured execution plan for dry-run output
func buildDryRunPlan(phases []*resolver.PhaseGroup, resolvers []*resolver.Resolver, skipTransform, skipValidation bool) map[string]any {
	// Build phase list
	phaseList := make([]map[string]any, 0, len(phases))
	for _, pg := range phases {
		resolverNames := make([]string, len(pg.Resolvers))
		for i, r := range pg.Resolvers {
			resolverNames[i] = r.Name
		}
		phaseList = append(phaseList, map[string]any{
			"phase":     pg.Phase,
			"resolvers": resolverNames,
		})
	}

	// Determine active phases
	activePhases := []string{"resolve"}
	skippedPhases := []string{}
	if !skipTransform {
		activePhases = append(activePhases, "transform")
	} else {
		skippedPhases = append(skippedPhases, "transform", "validate")
	}
	if !skipValidation && !skipTransform {
		activePhases = append(activePhases, "validate")
	} else if skipValidation && !skipTransform {
		skippedPhases = append(skippedPhases, "validate")
	}

	// Build per-resolver info
	resolverInfo := make(map[string]any, len(resolvers))
	for _, r := range resolvers {
		deps := resolver.ExtractDependencies(r, nil)
		configuredPhases := []string{"resolve"}
		if r.Transform != nil {
			configuredPhases = append(configuredPhases, "transform")
		}
		if r.Validate != nil {
			configuredPhases = append(configuredPhases, "validate")
		}

		resolverInfo[r.Name] = map[string]any{
			"provider":         execute.ResolverProviderName(r),
			"dependencies":     deps,
			"configuredPhases": configuredPhases,
		}
	}

	return map[string]any{
		"dryRun": true,
		"executionPlan": map[string]any{
			"totalResolvers": len(resolvers),
			"totalPhases":    len(phases),
			"activePhases":   activePhases,
			"skippedPhases":  skippedPhases,
			"phases":         phaseList,
		},
		"resolvers": resolverInfo,
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

	fmt.Fprintf(o.IOStreams.Out, "Snapshot saved to %s\n", o.SnapshotFile)
	fmt.Fprintf(o.IOStreams.Out, "  Solution: %s", snapshot.Metadata.Solution)
	if snapshot.Metadata.Version != "" {
		fmt.Fprintf(o.IOStreams.Out, " (v%s)", snapshot.Metadata.Version)
	}
	fmt.Fprintln(o.IOStreams.Out)
	fmt.Fprintf(o.IOStreams.Out, "  Resolvers: %d\n", len(snapshot.Resolvers))
	fmt.Fprintf(o.IOStreams.Out, "  Duration: %s\n", snapshot.Metadata.TotalDuration)
	fmt.Fprintf(o.IOStreams.Out, "  Status: %s\n", snapshot.Metadata.Status)

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
