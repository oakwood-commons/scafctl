// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package run

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
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

	// Verbose enables additional output: execution phases, timing,
	// dependency edges, and provider information.
	Verbose bool
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

RESOLVER SELECTION:
  Pass resolver names as positional arguments to execute only specific
  resolvers and their transitive dependencies. When no names are provided,
  all resolvers in the solution are executed.

  Examples:
    scafctl run resolver                    Execute all resolvers
    scafctl run resolver db config          Execute 'db', 'config', and their deps
    scafctl run resolver auth -f sol.yaml   Execute 'auth' and its deps

VERBOSE MODE:
  Use --verbose to include a __execution key in the output containing
  per-resolver execution metadata:
  - Execution phase number and status
  - Per-resolver duration and phase metrics breakdown
  - Provider type and dependency information
  - Aggregate summary (total duration, resolver count, phase count)
  The __execution key is extensible for future additions (e.g. diagrams).

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

  # Run with verbose output for debugging
  scafctl run resolver --verbose -f ./my-solution.yaml

  # JSON output for scripting
  scafctl run resolver -f ./my-solution.yaml -o json | jq .

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
	cCmd.Flags().BoolVar(&options.Verbose, "verbose", false, "Include __execution metadata in output (phases, timing, dependencies, providers)")

	return cCmd
}

// Run executes the resolver-only flow
func (o *ResolverOptions) Run(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("running resolver",
		"file", o.File,
		"output", o.Output,
		"names", o.Names,
		"verbose", o.Verbose,
		"resolveAll", o.ResolveAll,
		"progress", o.Progress,
		"showMetrics", o.ShowMetrics)

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
	resolvers := filterResolversWithDependencies(allResolvers, o.Names)

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

	// Verbose: include __execution metadata in output
	if o.Verbose {
		results["__execution"] = o.buildExecutionData(resolverCtx, resolvers, elapsed)
	}

	return o.writeResolverOutput(ctx, results, "scafctl run resolver")
}

// buildExecutionData constructs structured execution metadata from resolver results.
// The returned map is extensible — new top-level sections can be added without
// breaking consumers (e.g. "diagrams", "timeline", "warnings").
func (o *ResolverOptions) buildExecutionData(
	resolverCtx *resolver.Context,
	resolvers []*resolver.Resolver,
	totalElapsed time.Duration,
) map[string]any {
	allResults := resolverCtx.GetAllResults()

	// Build per-resolver execution metadata
	resolverMeta := make(map[string]any, len(resolvers))
	for _, r := range resolvers {
		deps := resolver.ExtractDependencies(r, nil)
		entry := map[string]any{
			"provider":     resolverProviderName(r),
			"dependencies": deps,
		}

		if result, ok := allResults[r.Name]; ok {
			entry["phase"] = result.Phase
			entry["duration"] = result.TotalDuration.Round(time.Millisecond).String()
			entry["status"] = string(result.Status)
			entry["providerCallCount"] = result.ProviderCallCount
			entry["valueSizeBytes"] = result.ValueSizeBytes
			entry["dependencyCount"] = result.DependencyCount

			// Per-phase breakdown (resolve, transform, validate)
			if len(result.PhaseMetrics) > 0 {
				metrics := make([]map[string]any, 0, len(result.PhaseMetrics))
				for _, pm := range result.PhaseMetrics {
					metrics = append(metrics, map[string]any{
						"phase":    pm.Phase,
						"duration": pm.Duration.Round(time.Millisecond).String(),
					})
				}
				entry["phaseMetrics"] = metrics
			}

			// Include failed attempts for debugging
			if len(result.FailedAttempts) > 0 {
				attempts := make([]map[string]any, 0, len(result.FailedAttempts))
				for _, fa := range result.FailedAttempts {
					attempt := map[string]any{
						"provider":   fa.Provider,
						"phase":      fa.Phase,
						"duration":   fa.Duration.Round(time.Millisecond).String(),
						"sourceStep": fa.SourceStep,
					}
					if fa.Error != "" {
						attempt["error"] = fa.Error
					}
					if fa.OnError != "" {
						attempt["onError"] = fa.OnError
					}
					attempts = append(attempts, attempt)
				}
				entry["failedAttempts"] = attempts
			}
		} else {
			entry["phase"] = 0
			entry["duration"] = "0s"
			entry["status"] = "unknown"
		}

		resolverMeta[r.Name] = entry
	}

	// Build summary
	phaseCount := 0
	for _, result := range allResults {
		if result.Phase > phaseCount {
			phaseCount = result.Phase
		}
	}

	summary := map[string]any{
		"totalDuration": totalElapsed.Round(time.Millisecond).String(),
		"resolverCount": len(resolvers),
		"phaseCount":    phaseCount,
	}

	return map[string]any{
		"resolvers": resolverMeta,
		"summary":   summary,
	}
}

// resolverProviderName extracts the primary provider name from a resolver
func resolverProviderName(r *resolver.Resolver) string {
	if r.Resolve != nil && len(r.Resolve.With) > 0 {
		return r.Resolve.With[0].Provider
	}
	return "unknown"
}

// resolverNamesString returns a comma-separated string of resolver names
func resolverNamesString(resolvers []*resolver.Resolver) string {
	names := make([]string, len(resolvers))
	for i, r := range resolvers {
		names[i] = r.Name
	}
	return strings.Join(names, ", ")
}
