package render

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/cmd/scafctl/run"
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
)

// Exit codes for the render command
const (
	ExitSuccess          = 0 // Successful rendering
	ExitRenderFailed     = 1 // Rendering failed
	ExitValidationFailed = 2 // Validation failed
	ExitInvalidSolution  = 3 // Circular dependency / invalid solution
	ExitFileNotFound     = 4 // File not found / parse error
	ExitNoWorkflow       = 5 // No workflow defined in solution
)

// ValidOutputTypes defines the supported output formats
var ValidOutputTypes = []string{"json", "yaml"}

// WorkflowOptions holds configuration for the render workflow command
type WorkflowOptions struct {
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

	// For dependency injection in tests
	getter   get.Interface
	registry *provider.Registry
}

// CommandWorkflow creates the 'render workflow' subcommand
func CommandWorkflow(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &WorkflowOptions{}

	cCmd := &cobra.Command{
		Use:     "workflow",
		Aliases: []string{"wf", "w"},
		Short:   "Render a workflow's action graph for external execution",
		Long: `Render a solution's workflow as an executor-ready action graph.

The command first resolves all defined resolvers, then builds the action graph
with materialized inputs where possible. Expressions that reference __actions
(action results) are preserved as deferred expressions for runtime evaluation.

The output includes:
  - API version and kind for schema identification
  - Metadata with generation timestamp and statistics
  - Execution order phases for parallel execution
  - Finally order phases (always executed after main actions)
  - Action definitions with:
    - Materialized inputs (resolved from resolvers)
    - Deferred inputs (expressions referencing __actions)
    - Dependencies and conditions
    - Retry and timeout configuration
    - ForEach expansion metadata

OUTPUT FORMATS:
  json    JSON output (default) - uses pretty-print unless --compact is set
  yaml    YAML output - human-readable format

Examples:
  # Render workflow to stdout (JSON)
  scafctl render workflow -f ./solution.yaml

  # Render workflow as YAML
  scafctl render workflow -f ./solution.yaml -o yaml

  # Render workflow to file
  scafctl render workflow -f ./solution.yaml --output-file graph.json

  # Render with parameters
  scafctl render workflow -f ./solution.yaml -r env=prod

  # Render with compact JSON output
  scafctl render workflow -f ./solution.yaml --compact`,
		RunE: func(cCmd *cobra.Command, args []string) error {
			cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
			ctx := settings.IntoContext(context.Background(), cliParams)

			lgr := logger.FromContext(cCmd.Context())
			if lgr != nil {
				ctx = logger.WithLogger(ctx, lgr)
			}

			options.IOStreams = ioStreams
			options.CliParams = cliParams

			err := output.ValidateCommands(args)
			if err != nil {
				writeError(options, err.Error())
				return err
			}

			if options.Output != "" {
				err = output.ValidateOutputType(options.Output, ValidOutputTypes)
				if err != nil {
					writeError(options, err.Error())
					return err
				}
			}

			return options.Run(ctx)
		},
		SilenceUsage: true,
	}

	// Flags
	cCmd.Flags().StringVarP(&options.File, "file", "f", "", "Solution file path (auto-discovered if not provided, use '-' for stdin)")
	cCmd.Flags().StringArrayVarP(&options.ResolverParams, "resolver", "r", nil, "Resolver parameters (key=value or @file.yaml)")
	cCmd.Flags().StringVarP(&options.Output, "output", "o", "json", fmt.Sprintf("Output format: %s", strings.Join(ValidOutputTypes, ", ")))
	cCmd.Flags().StringVar(&options.OutputFile, "output-file", "", "Write output to file instead of stdout")
	cCmd.Flags().BoolVar(&options.Compact, "compact", false, "Compact output (JSON only, no pretty-printing)")
	cCmd.Flags().BoolVar(&options.NoTimestamp, "no-timestamp", false, "Omit generation timestamp from output")
	cCmd.Flags().DurationVar(&options.ResolverTimeout, "resolver-timeout", 30*time.Second, "Timeout per resolver")
	cCmd.Flags().DurationVar(&options.PhaseTimeout, "phase-timeout", 5*time.Minute, "Timeout per phase")

	return cCmd
}

// Run executes the render workflow command
func (o *WorkflowOptions) Run(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("rendering workflow",
		"file", o.File,
		"output", o.Output,
		"outputFile", o.OutputFile,
		"compact", o.Compact)

	// Load the solution
	sol, err := o.loadSolution(ctx)
	if err != nil {
		return o.exitWithCode(err, ExitFileNotFound)
	}

	lgr.V(1).Info("loaded solution",
		"name", sol.Metadata.Name,
		"version", sol.Metadata.Version,
		"hasWorkflow", sol.Spec.HasWorkflow())

	// Check if there's a workflow
	if !sol.Spec.HasWorkflow() {
		return o.exitWithCode(fmt.Errorf("solution does not define a workflow"), ExitNoWorkflow)
	}

	// Validate the workflow
	reg := o.getRegistry()
	adapter := &registryAdapter{registry: reg}
	if err := action.ValidateWorkflow(sol.Spec.Workflow, adapter); err != nil {
		return o.exitWithCode(fmt.Errorf("workflow validation failed: %w", err), ExitValidationFailed)
	}

	// Resolve resolvers first to get data for action inputs
	resolverData := make(map[string]any)
	if sol.Spec.HasResolvers() {
		lgr.V(1).Info("resolving resolvers for workflow inputs")

		// Parse resolver parameters (reuse from run package)
		params, err := run.ParseResolverFlags(o.ResolverParams)
		if err != nil {
			return o.exitWithCode(fmt.Errorf("failed to parse resolver parameters: %w", err), ExitValidationFailed)
		}

		// Create resolver registry adapter (resolver.RegistryInterface requires Get returning error)
		resolverAdapter := &resolverRegistryAdapter{registryAdapter: adapter}

		// Execute resolvers
		resolvers := sol.Spec.ResolversToSlice()
		executorOpts := []resolver.ExecutorOption{
			resolver.WithDefaultTimeout(o.ResolverTimeout),
			resolver.WithPhaseTimeout(o.PhaseTimeout),
		}
		executor := resolver.NewExecutor(resolverAdapter, executorOpts...)

		resultCtx, err := executor.Execute(ctx, resolvers, params)
		if err != nil {
			return o.exitWithCode(fmt.Errorf("resolver execution failed: %w", err), ExitRenderFailed)
		}

		// Build resolver data map
		resolverCtx, ok := resolver.FromContext(resultCtx)
		if !ok {
			return o.exitWithCode(fmt.Errorf("failed to retrieve resolver results"), ExitRenderFailed)
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
		return o.exitWithCode(fmt.Errorf("failed to build action graph: %w", err), ExitRenderFailed)
	}

	// Render the graph
	renderOpts := &action.RenderOptions{
		Format:           o.Output,
		IncludeTimestamp: !o.NoTimestamp,
		PrettyPrint:      !o.Compact,
	}

	rendered, err := action.Render(graph, renderOpts)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to render graph: %w", err), ExitRenderFailed)
	}

	// Write output
	return o.writeOutput(rendered)
}

// loadSolution loads the solution from file, stdin, or auto-discovery
func (o *WorkflowOptions) loadSolution(ctx context.Context) (*solution.Solution, error) {
	getter := o.getter
	if getter == nil {
		getter = get.NewGetter()
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

	// Use getter for file or auto-discovery
	return getter.Get(ctx, o.File)
}

// getRegistry returns the provider registry
func (o *WorkflowOptions) getRegistry() *provider.Registry {
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
func (o *WorkflowOptions) writeOutput(data []byte) error {
	if o.OutputFile != "" {
		return o.writeToFile(data)
	}

	fmt.Fprintln(o.IOStreams.Out, string(data))
	return nil
}

// writeToFile writes data to the specified file
func (o *WorkflowOptions) writeToFile(data []byte) error {
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
func (o *WorkflowOptions) exitWithCode(err error, _ int) error {
	writeError(o, err.Error())
	return err
}

// writeError writes an error message
func writeError(o *WorkflowOptions, msg string) {
	output.NewWriteMessageOptions(
		o.IOStreams,
		output.MessageTypeError,
		o.CliParams.NoColor,
		o.CliParams.ExitOnError,
	).WriteMessage(msg)
}

// registryAdapter adapts provider.Registry to action.RegistryInterface
type registryAdapter struct {
	registry *provider.Registry
}

func (r *registryAdapter) Get(name string) (provider.Provider, bool) {
	return r.registry.Get(name)
}

func (r *registryAdapter) Has(name string) bool {
	_, ok := r.registry.Get(name)
	return ok
}

// For resolver.RegistryInterface compatibility
func (r *registryAdapter) Register(p provider.Provider) error {
	return r.registry.Register(p)
}

func (r *registryAdapter) List() []provider.Provider {
	return r.registry.ListProviders()
}

func (r *registryAdapter) DescriptorLookup() resolver.DescriptorLookup {
	return r.registry.DescriptorLookup()
}

// resolverRegistryAdapter adapts registryAdapter to resolver.RegistryInterface
type resolverRegistryAdapter struct {
	*registryAdapter
}

// Get implements resolver.RegistryInterface with error return
func (r *resolverRegistryAdapter) Get(name string) (provider.Provider, error) {
	p, ok := r.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}
