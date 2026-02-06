package render

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/run"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// graphRenderer defines the interface for types that can render as ASCII, DOT, and Mermaid.
type graphRenderer interface {
	RenderASCII(w io.Writer) error
	RenderDOT(w io.Writer) error
	RenderMermaid(w io.Writer) error
}

// ValidOutputTypes defines the supported output formats
var ValidOutputTypes = []string{"json", "yaml"}

// SolutionOptions holds configuration for the render solution command
type SolutionOptions struct {
	IOStreams       *terminal.IOStreams
	CliParams       *settings.Run
	Output          string
	OutputFile      string
	File            string
	ResolverParams  []string
	ResolverTimeout time.Duration
	PhaseTimeout    time.Duration
	Compact         bool
	NoTimestamp     bool

	// Mode flags (mutually exclusive)
	Graph        bool   // --graph: Show resolver dependency graph
	ActionGraph  bool   // --action-graph: Show action dependency graph
	GraphFormat  string // --graph-format: Graph format (ascii, dot, mermaid, json)
	Snapshot     bool   // --snapshot: Save execution snapshot
	SnapshotFile string // --snapshot-file: Snapshot output file
	Redact       bool   // --redact: Redact sensitive values in snapshot

	// Track which flags were explicitly set by user
	flagsChanged map[string]bool

	// For dependency injection in tests
	getter   get.Interface
	registry *provider.Registry
}

// CommandSolution creates the 'render solution' subcommand
func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &SolutionOptions{}

	cCmd := &cobra.Command{
		Use:     "solution",
		Aliases: []string{"sol", "s", "solutions"},
		Short:   "Render a solution's action graph, dependency graph, or snapshot",
		Long: `Render a solution as an executor-ready action graph, dependency graph, or snapshot.

DEFAULT MODE (action graph):
  Resolves all defined resolvers, then builds the action graph with materialized
  inputs where possible. Expressions referencing __actions are preserved as
  deferred expressions for runtime evaluation.

GRAPH MODE (--graph):
  Visualizes the resolver dependency graph showing execution phases, parallelization
  opportunities, and dependencies. Useful for understanding resolver execution order.
  
  Supported formats:
    ascii   - Human-readable ASCII art (default)
    dot     - Graphviz DOT format (pipe to 'dot' command)
    mermaid - Mermaid diagram syntax
    json    - Machine-readable JSON format

ACTION GRAPH MODE (--action-graph):
  Visualizes the action dependency graph showing execution phases, parallel actions,
  finally blocks, and dependencies. Useful for understanding action execution order.
  
  Supported formats are the same as --graph.

SNAPSHOT MODE (--snapshot):
  Executes resolvers and saves the execution state to a snapshot file for
  debugging, testing, comparison, and audit trails.

OUTPUT FORMATS:
  json    JSON output (default) - uses pretty-print unless --compact is set
  yaml    YAML output - human-readable format

Examples:
  # Render action graph to stdout (JSON)
  scafctl render solution -f ./solution.yaml

  # Render action graph as YAML
  scafctl render solution -f ./solution.yaml -o yaml

  # Show resolver dependency graph (ASCII)
  scafctl render solution -f ./solution.yaml --graph

  # Generate PNG graph using Graphviz
  scafctl render solution -f ./solution.yaml --graph --graph-format=dot | dot -Tpng > graph.png

  # Generate Mermaid diagram
  scafctl render solution -f ./solution.yaml --graph --graph-format=mermaid

  # Show action dependency graph (ASCII)
  scafctl render solution -f ./solution.yaml --action-graph

  # Generate action graph as DOT
  scafctl render solution -f ./solution.yaml --action-graph --graph-format=dot | dot -Tpng > actions.png

  # Save execution snapshot
  scafctl render solution -f ./solution.yaml --snapshot --snapshot-file=snapshot.json

  # Save snapshot with sensitive data redacted
  scafctl render solution -f ./solution.yaml --snapshot --snapshot-file=snapshot.json --redact

  # Render with parameters
  scafctl render solution -f ./solution.yaml -r env=prod`,
		PreRun: func(cCmd *cobra.Command, _ []string) {
			// Track which flags were explicitly set by the user
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
		},
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			lgr := logger.FromContext(cCmd.Context())
			if lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			// Transfer config from parent context
			if appCfg := config.FromContext(cCmd.Context()); appCfg != nil {
				ctx = config.WithConfig(ctx, appCfg)
			}

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			err := output.ValidateCommands(args)
			if err != nil {
				writeSolutionError(options, err.Error())
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Validate mutually exclusive modes
			modeCount := 0
			if options.Graph {
				modeCount++
			}
			if options.ActionGraph {
				modeCount++
			}
			if options.Snapshot {
				modeCount++
			}
			if modeCount > 1 {
				err := fmt.Errorf("--graph, --action-graph, and --snapshot are mutually exclusive")
				writeSolutionError(options, err.Error())
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Validate snapshot file requirement
			if options.Snapshot && options.SnapshotFile == "" {
				err := fmt.Errorf("--snapshot-file is required when using --snapshot")
				writeSolutionError(options, err.Error())
				return exitcode.WithCode(err, exitcode.InvalidInput)
			}

			// Validate output format
			if options.Output != "" && !options.Graph && !options.ActionGraph {
				err = output.ValidateOutputType(options.Output, ValidOutputTypes)
				if err != nil {
					writeSolutionError(options, err.Error())
					return exitcode.WithCode(err, exitcode.InvalidInput)
				}
			}

			return options.Run(ctx)
		},
		SilenceUsage: true,
	}

	// File and output flags
	cCmd.Flags().StringVarP(&options.File, "file", "f", "", "Solution file path (auto-discovered if not provided, use '-' for stdin)")
	cCmd.Flags().StringArrayVarP(&options.ResolverParams, "resolver", "r", nil, "Resolver parameters (key=value or @file.yaml)")
	cCmd.Flags().StringVarP(&options.Output, "output", "o", "json", fmt.Sprintf("Output format: %s", strings.Join(ValidOutputTypes, ", ")))
	cCmd.Flags().StringVar(&options.OutputFile, "output-file", "", "Write output to file instead of stdout")
	cCmd.Flags().BoolVar(&options.Compact, "compact", false, "Compact output (JSON only, no pretty-printing)")
	cCmd.Flags().BoolVar(&options.NoTimestamp, "no-timestamp", false, "Omit generation timestamp from output")
	cCmd.Flags().DurationVar(&options.ResolverTimeout, "resolver-timeout", settings.DefaultResolverTimeout, "Timeout per resolver")
	cCmd.Flags().DurationVar(&options.PhaseTimeout, "phase-timeout", settings.DefaultPhaseTimeout, "Timeout per phase")

	// Graph mode flags
	cCmd.Flags().BoolVar(&options.Graph, "graph", false, "Show resolver dependency graph instead of action graph")
	cCmd.Flags().BoolVar(&options.ActionGraph, "action-graph", false, "Show action dependency graph (ASCII, DOT, Mermaid, JSON)")
	cCmd.Flags().StringVar(&options.GraphFormat, "graph-format", "ascii", "Graph output format: ascii, dot, mermaid, json")

	// Snapshot mode flags
	cCmd.Flags().BoolVar(&options.Snapshot, "snapshot", false, "Save execution snapshot instead of rendering")
	cCmd.Flags().StringVar(&options.SnapshotFile, "snapshot-file", "", "Snapshot output file (required with --snapshot)")
	cCmd.Flags().BoolVar(&options.Redact, "redact", false, "Redact sensitive values in snapshot")

	return cCmd
}

// getEffectiveResolverConfig returns resolver config values, using app config
// as defaults when CLI flags weren't explicitly set.
func (o *SolutionOptions) getEffectiveResolverConfig(ctx context.Context) config.ResolverConfigValues {
	// Start with CLI flag values
	result := config.ResolverConfigValues{
		Timeout:        o.ResolverTimeout,
		PhaseTimeout:   o.PhaseTimeout,
		MaxConcurrency: 0,
		WarnValueSize:  settings.DefaultWarnValueSize,
		MaxValueSize:   settings.DefaultMaxValueSize,
		ValidateAll:    false,
	}

	// If config is available, use its values for non-changed flags
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return result
	}

	// Parse config values
	configValues, err := cfg.Resolver.ToResolverValues()
	if err != nil {
		lgr := logger.FromContext(ctx)
		lgr.V(1).Info("failed to parse resolver config, using CLI defaults", "error", err)
		return result
	}

	// Override with config values for flags that weren't explicitly set.
	// Only apply overrides when flagsChanged is set (i.e., we're in command execution flow).
	// When flagsChanged is nil (e.g., in tests), respect the values set on the options struct.
	if o.flagsChanged != nil {
		if !o.flagsChanged["resolver-timeout"] {
			result.Timeout = configValues.Timeout
		}
		if !o.flagsChanged["phase-timeout"] {
			result.PhaseTimeout = configValues.PhaseTimeout
		}
	}

	// MaxConcurrency always comes from config (no CLI flag)
	result.MaxConcurrency = configValues.MaxConcurrency

	return result
}

// Run executes the render solution command
func (o *SolutionOptions) Run(ctx context.Context) error {
	lgr := logger.FromContext(ctx)

	// Route to appropriate mode
	if o.Graph {
		return o.runGraph(ctx, *lgr)
	}
	if o.ActionGraph {
		return o.runActionGraphVisualization(ctx, *lgr)
	}
	if o.Snapshot {
		return o.runSnapshot(ctx, *lgr)
	}
	return o.runActionGraph(ctx, *lgr)
}

// runActionGraph renders the action graph (default mode)
func (o *SolutionOptions) runActionGraph(ctx context.Context, lgr logr.Logger) error {
	lgr.V(1).Info("rendering action graph",
		"file", o.File,
		"output", o.Output,
		"outputFile", o.OutputFile,
		"compact", o.Compact)

	// Load the solution
	sol, err := o.loadSolution(ctx)
	if err != nil {
		return o.exitWithCode(err, exitcode.FileNotFound)
	}

	lgr.V(1).Info("loaded solution",
		"name", sol.Metadata.Name,
		"version", sol.Metadata.Version,
		"hasWorkflow", sol.Spec.HasWorkflow())

	// Check if there's a workflow
	if !sol.Spec.HasWorkflow() {
		return o.exitWithCode(fmt.Errorf("solution does not define a workflow"), exitcode.InvalidInput)
	}

	// Validate the workflow
	reg := o.getRegistry()
	adapter := &solutionRegistryAdapter{registry: reg}
	if err := action.ValidateWorkflow(sol.Spec.Workflow, adapter); err != nil {
		return o.exitWithCode(fmt.Errorf("workflow validation failed: %w", err), exitcode.ValidationFailed)
	}

	// Resolve resolvers first to get data for action inputs
	resolverData := make(map[string]any)
	if sol.Spec.HasResolvers() {
		lgr.V(1).Info("resolving resolvers for action inputs")

		// Parse resolver parameters
		params, err := run.ParseResolverFlags(o.ResolverParams)
		if err != nil {
			return o.exitWithCode(fmt.Errorf("failed to parse resolver parameters: %w", err), exitcode.ValidationFailed)
		}

		// Create resolver registry adapter
		resolverAdapter := &solutionResolverRegistryAdapter{solutionRegistryAdapter: adapter}

		// Get effective resolver config (CLI flags override app config)
		resolverCfg := o.getEffectiveResolverConfig(ctx)

		// Execute resolvers
		resolvers := sol.Spec.ResolversToSlice()
		executorOpts := []resolver.ExecutorOption{
			resolver.WithDefaultTimeout(resolverCfg.Timeout),
			resolver.WithPhaseTimeout(resolverCfg.PhaseTimeout),
		}
		if resolverCfg.MaxConcurrency > 0 {
			executorOpts = append(executorOpts, resolver.WithMaxConcurrency(resolverCfg.MaxConcurrency))
		}
		executor := resolver.NewExecutor(resolverAdapter, executorOpts...)

		resultCtx, err := executor.Execute(ctx, resolvers, params)
		if err != nil {
			return o.exitWithCode(fmt.Errorf("resolver execution failed: %w", err), exitcode.RenderFailed)
		}

		// Build resolver data map
		resolverCtx, ok := resolver.FromContext(resultCtx)
		if !ok {
			return o.exitWithCode(fmt.Errorf("failed to retrieve resolver results"), exitcode.RenderFailed)
		}

		for name := range sol.Spec.Resolvers {
			result, ok := resolverCtx.GetResult(name)
			if ok && result.Status == resolver.ExecutionStatusSuccess {
				resolverData[name] = result.Value
			}
		}

		lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
	}

	// Build the action graph
	graph, err := action.BuildGraph(ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to build action graph: %w", err), exitcode.RenderFailed)
	}

	// Render the graph
	renderOpts := &action.RenderOptions{
		Format:           o.Output,
		IncludeTimestamp: !o.NoTimestamp,
		PrettyPrint:      !o.Compact,
	}

	rendered, err := action.Render(graph, renderOpts)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to render graph: %w", err), exitcode.RenderFailed)
	}

	// Write output
	return o.writeOutput(rendered)
}

// runGraph renders the resolver dependency graph (--graph mode)
func (o *SolutionOptions) runGraph(ctx context.Context, lgr logr.Logger) error {
	lgr.V(1).Info("rendering dependency graph",
		"file", o.File,
		"format", o.GraphFormat)

	// Load the solution
	sol, err := o.loadSolution(ctx)
	if err != nil {
		return o.exitWithCode(err, exitcode.FileNotFound)
	}

	if !sol.Spec.HasResolvers() {
		return o.exitWithCode(fmt.Errorf("solution does not define any resolvers"), exitcode.ValidationFailed)
	}

	resolvers := sol.Spec.ResolversToSlice()
	lgr.V(1).Info("building dependency graph", "resolvers", len(resolvers))

	// Build dependency graph
	graph, err := resolver.BuildGraph(resolvers, nil)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to build dependency graph: %w", err), exitcode.RenderFailed)
	}

	lgr.V(1).Info("graph built successfully",
		"nodes", len(graph.Nodes),
		"phases", len(graph.Phases),
		"maxParallelism", graph.Stats.MaxParallelism)

	// Render the graph
	return o.renderGraph(graph, graph)
}

// runActionGraphVisualization renders the action graph visualization (--action-graph mode)
func (o *SolutionOptions) runActionGraphVisualization(ctx context.Context, lgr logr.Logger) error {
	lgr.V(1).Info("rendering action dependency graph",
		"file", o.File,
		"format", o.GraphFormat)

	// Load the solution
	sol, err := o.loadSolution(ctx)
	if err != nil {
		return o.exitWithCode(err, exitcode.FileNotFound)
	}

	if !sol.Spec.HasWorkflow() {
		return o.exitWithCode(fmt.Errorf("solution does not define a workflow"), exitcode.ValidationFailed)
	}

	// Validate the workflow
	reg := o.getRegistry()
	adapter := &solutionRegistryAdapter{registry: reg}
	if err := action.ValidateWorkflow(sol.Spec.Workflow, adapter); err != nil {
		return o.exitWithCode(fmt.Errorf("workflow validation failed: %w", err), exitcode.ValidationFailed)
	}

	// Resolve resolvers first to get data for action inputs (for full graph)
	resolverData := make(map[string]any)
	if sol.Spec.HasResolvers() {
		lgr.V(1).Info("resolving resolvers for action inputs")

		// Parse resolver parameters
		params, err := run.ParseResolverFlags(o.ResolverParams)
		if err != nil {
			return o.exitWithCode(fmt.Errorf("failed to parse resolver parameters: %w", err), exitcode.ValidationFailed)
		}

		// Create resolver registry adapter
		resolverAdapter := &solutionResolverRegistryAdapter{solutionRegistryAdapter: adapter}

		// Get effective resolver config (CLI flags override app config)
		resolverCfg := o.getEffectiveResolverConfig(ctx)

		// Execute resolvers
		resolvers := sol.Spec.ResolversToSlice()
		executorOpts := []resolver.ExecutorOption{
			resolver.WithDefaultTimeout(resolverCfg.Timeout),
			resolver.WithPhaseTimeout(resolverCfg.PhaseTimeout),
		}
		if resolverCfg.MaxConcurrency > 0 {
			executorOpts = append(executorOpts, resolver.WithMaxConcurrency(resolverCfg.MaxConcurrency))
		}
		executor := resolver.NewExecutor(resolverAdapter, executorOpts...)

		resultCtx, err := executor.Execute(ctx, resolvers, params)
		if err != nil {
			return o.exitWithCode(fmt.Errorf("resolver execution failed: %w", err), exitcode.RenderFailed)
		}

		// Build resolver data map
		resolverCtx, ok := resolver.FromContext(resultCtx)
		if ok {
			for name := range sol.Spec.Resolvers {
				result, ok := resolverCtx.GetResult(name)
				if ok && result.Status == resolver.ExecutionStatusSuccess {
					resolverData[name] = result.Value
				}
			}
		}

		lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
	}

	// Build the action graph
	graph, err := action.BuildGraph(ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to build action graph: %w", err), exitcode.RenderFailed)
	}

	lgr.V(1).Info("action graph built successfully",
		"actions", len(graph.Actions),
		"phases", len(graph.ExecutionOrder),
		"finallyPhases", len(graph.FinallyOrder))

	// Build visualization data and render
	viz := action.BuildVisualization(graph)
	return o.renderGraph(viz, viz)
}

// runSnapshot saves execution snapshot (--snapshot mode)
func (o *SolutionOptions) runSnapshot(ctx context.Context, lgr logr.Logger) error {
	lgr.V(1).Info("saving execution snapshot",
		"file", o.File,
		"snapshotFile", o.SnapshotFile,
		"redact", o.Redact)

	// Load the solution
	sol, err := o.loadSolution(ctx)
	if err != nil {
		return o.exitWithCode(err, exitcode.FileNotFound)
	}

	if !sol.Spec.HasResolvers() {
		return o.exitWithCode(fmt.Errorf("solution does not define any resolvers"), exitcode.ValidationFailed)
	}

	// Parse resolver parameters
	params, err := run.ParseResolverFlags(o.ResolverParams)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to parse resolver parameters: %w", err), exitcode.ValidationFailed)
	}

	resolvers := sol.Spec.ResolversToSlice()
	lgr.V(1).Info("executing resolvers for snapshot",
		"count", len(resolvers),
		"parameters", len(params))

	// Execute resolvers
	reg := o.getRegistry()
	adapter := &solutionRegistryAdapter{registry: reg}
	resolverAdapter := &solutionResolverRegistryAdapter{solutionRegistryAdapter: adapter}

	// Get effective resolver config (CLI flags override app config)
	resolverCfg := o.getEffectiveResolverConfig(ctx)

	executorOpts := []resolver.ExecutorOption{
		resolver.WithDefaultTimeout(resolverCfg.Timeout),
		resolver.WithPhaseTimeout(resolverCfg.PhaseTimeout),
	}
	if resolverCfg.MaxConcurrency > 0 {
		executorOpts = append(executorOpts, resolver.WithMaxConcurrency(resolverCfg.MaxConcurrency))
	}
	executor := resolver.NewExecutor(resolverAdapter, executorOpts...)

	start := time.Now()
	execCtx, err := executor.Execute(ctx, resolvers, params)
	duration := time.Since(start)

	status := resolver.ExecutionStatusSuccess
	if err != nil {
		lgr.V(1).Info("resolver execution completed with errors", "error", err)
		status = resolver.ExecutionStatusFailed
		// Continue to capture snapshot even with errors
	}

	// Capture snapshot
	lgr.V(1).Info("capturing snapshot")
	versionStr := ""
	if sol.Metadata.Version != nil {
		versionStr = sol.Metadata.Version.String()
	}
	snapshot, err := resolver.CaptureSnapshot(
		execCtx,
		sol.Metadata.Name,
		versionStr,
		settings.VersionInformation.BuildVersion,
		params,
		duration,
		status,
	)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to capture snapshot: %w", err), exitcode.RenderFailed)
	}

	// Redact sensitive values if requested
	if o.Redact {
		lgr.V(1).Info("redacting sensitive values")
		sensitiveMap := make(map[string]bool)
		for name, r := range sol.Spec.Resolvers {
			if r.Sensitive {
				sensitiveMap[name] = true
			}
		}
		for name, sr := range snapshot.Resolvers {
			if sensitiveMap[name] {
				sr.Value = "<redacted>"
				sr.Sensitive = true
			}
		}
	}

	// Save snapshot
	lgr.V(1).Info("saving snapshot", "output", o.SnapshotFile)
	if err := resolver.SaveSnapshot(snapshot, o.SnapshotFile); err != nil {
		return o.exitWithCode(fmt.Errorf("failed to save snapshot: %w", err), exitcode.RenderFailed)
	}

	fmt.Fprintf(o.IOStreams.Out, "Snapshot saved to %s\n", o.SnapshotFile)
	fmt.Fprintf(o.IOStreams.Out, "  Solution: %s (v%s)\n", snapshot.Metadata.Solution, snapshot.Metadata.Version)
	fmt.Fprintf(o.IOStreams.Out, "  Resolvers: %d\n", len(snapshot.Resolvers))
	fmt.Fprintf(o.IOStreams.Out, "  Duration: %s\n", snapshot.Metadata.TotalDuration)
	fmt.Fprintf(o.IOStreams.Out, "  Status: %s\n", snapshot.Metadata.Status)

	return nil
}

// loadSolution loads the solution from file, stdin, catalog, or auto-discovery
func (o *SolutionOptions) loadSolution(ctx context.Context) (*solution.Solution, error) {
	getter := o.getter
	if getter == nil {
		lgr := logger.FromContext(ctx)

		// Set up getter options
		getterOpts := []get.Option{
			get.WithLogger(*lgr),
		}

		// Try to set up catalog resolver for bare name resolution
		localCatalog, err := catalog.NewLocalCatalog(*lgr)
		if err == nil {
			resolver := catalog.NewSolutionResolver(localCatalog, *lgr)
			getterOpts = append(getterOpts, get.WithCatalogResolver(resolver))
		} else {
			lgr.V(1).Info("catalog not available for solution resolution", "error", err)
		}

		getter = get.NewGetter(getterOpts...)
	}

	// Handle stdin
	if o.File == "-" {
		data, err := io.ReadAll(o.IOStreams.In)
		if err != nil {
			return nil, fmt.Errorf("failed to read from stdin: %w", err)
		}

		var sol solution.Solution
		if err := sol.LoadFromBytes(data); err != nil {
			return nil, fmt.Errorf("failed to parse solution from stdin: %w", err)
		}
		return &sol, nil
	}

	// Use getter for file, catalog, or auto-discovery
	return getter.Get(ctx, o.File)
}

// getRegistry returns the provider registry
func (o *SolutionOptions) getRegistry() *provider.Registry {
	if o.registry != nil {
		return o.registry
	}

	reg, err := builtin.DefaultRegistry()
	if err != nil {
		lgr := logger.Get(0)
		lgr.V(0).Info("warning: failed to register some providers", "error", err)
		return provider.GetGlobalRegistry()
	}

	return reg
}

// writeOutput writes the rendered output to stdout or file
func (o *SolutionOptions) writeOutput(data []byte) error {
	if o.OutputFile != "" {
		return o.writeToFile(data)
	}

	fmt.Fprintln(o.IOStreams.Out, string(data))
	return nil
}

// writeToFile writes data to the specified file
func (o *SolutionOptions) writeToFile(data []byte) error {
	// Ensure correct extension
	ext := filepath.Ext(o.OutputFile)
	if ext == "" {
		switch o.Output {
		case "yaml":
			o.OutputFile += ".yaml"
		default:
			o.OutputFile += ".json"
		}
	}

	return os.WriteFile(o.OutputFile, data, 0o600)
}

// exitWithCode returns the error with appropriate exit handling
func (o *SolutionOptions) exitWithCode(err error, code int) error {
	writeSolutionError(o, err.Error())
	return exitcode.WithCode(err, code)
}

// writeSolutionError writes an error message
func writeSolutionError(o *SolutionOptions, msg string) {
	output.NewWriteMessageOptions(
		o.IOStreams,
		output.MessageTypeError,
		o.CliParams.NoColor,
		o.CliParams.ExitOnError,
	).WriteMessage(msg)
}

// solutionRegistryAdapter adapts provider.Registry to action.RegistryInterface
type solutionRegistryAdapter struct {
	registry *provider.Registry
}

func (r *solutionRegistryAdapter) Get(name string) (provider.Provider, bool) {
	return r.registry.Get(name)
}

func (r *solutionRegistryAdapter) Has(name string) bool {
	_, ok := r.registry.Get(name)
	return ok
}

// For resolver.RegistryInterface compatibility
func (r *solutionRegistryAdapter) Register(p provider.Provider) error {
	return r.registry.Register(p)
}

func (r *solutionRegistryAdapter) List() []provider.Provider {
	return r.registry.ListProviders()
}

func (r *solutionRegistryAdapter) DescriptorLookup() resolver.DescriptorLookup {
	return r.registry.DescriptorLookup()
}

// solutionResolverRegistryAdapter adapts solutionRegistryAdapter to resolver.RegistryInterface
type solutionResolverRegistryAdapter struct {
	*solutionRegistryAdapter
}

// Get implements resolver.RegistryInterface with error return
func (r *solutionResolverRegistryAdapter) Get(name string) (provider.Provider, error) {
	p, ok := r.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

// renderGraph renders a graph in the specified format using the common interface.
func (o *SolutionOptions) renderGraph(graph graphRenderer, data any) error {
	switch o.GraphFormat {
	case "ascii":
		if err := graph.RenderASCII(o.IOStreams.Out); err != nil {
			return o.exitWithCode(fmt.Errorf("failed to render ASCII graph: %w", err), exitcode.RenderFailed)
		}

	case "dot":
		if err := graph.RenderDOT(o.IOStreams.Out); err != nil {
			return o.exitWithCode(fmt.Errorf("failed to render DOT graph: %w", err), exitcode.RenderFailed)
		}

	case "mermaid":
		if err := graph.RenderMermaid(o.IOStreams.Out); err != nil {
			return o.exitWithCode(fmt.Errorf("failed to render Mermaid graph: %w", err), exitcode.RenderFailed)
		}

	case "json":
		encoder := json.NewEncoder(o.IOStreams.Out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(data); err != nil {
			return o.exitWithCode(fmt.Errorf("failed to encode JSON: %w", err), exitcode.RenderFailed)
		}

	default:
		return o.exitWithCode(fmt.Errorf("unsupported graph format: %s (supported: ascii, dot, mermaid, json)", o.GraphFormat), exitcode.ValidationFailed)
	}
	return nil
}
