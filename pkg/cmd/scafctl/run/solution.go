package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-logr/logr"
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
	"gopkg.in/yaml.v3"
)

// Exit codes for the run command
const (
	ExitSuccess          = 0 // Successful execution
	ExitResolverFailed   = 1 // Resolver execution failed
	ExitValidationFailed = 2 // Validation failed
	ExitInvalidSolution  = 3 // Circular dependency / invalid solution
	ExitFileNotFound     = 4 // File not found / parse error
)

// ValidOutputTypes defines the supported output formats
var ValidOutputTypes = []string{"json", "yaml", "quiet"}

// SolutionOptions holds configuration for the run solution command
type SolutionOptions struct {
	IOStreams       *terminal.IOStreams
	CliParams       *settings.Run
	Output          string
	File            string
	ResolverParams  []string
	Only            string
	ResolveAll      bool
	Progress        bool
	ValidateAll     bool
	SkipValidation  bool
	ShowMetrics     bool
	WarnValueSize   int64
	MaxValueSize    int64
	ResolverTimeout time.Duration
	PhaseTimeout    time.Duration

	// For dependency injection in tests
	getter   get.Interface
	registry *provider.Registry
}

// CommandSolution creates the 'run solution' subcommand
func CommandSolution(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &SolutionOptions{}

	cfg := runCommandConfig{
		cliParams: cliParams,
		ioStreams: ioStreams,
		path:      path,
		runner:    options,
		writeErrorFn: func(msg string) {
			writeError(options, msg)
		},
		getOutputFn: func() string {
			return options.Output
		},
		setIOStreamFn: func(ios *terminal.IOStreams, cli *settings.Run) {
			options.IOStreams = ios
			options.CliParams = cli
		},
	}

	cCmd := &cobra.Command{
		Use:     "solution",
		Aliases: []string{"sol", "s"},
		Short:   "Run a solution by executing all resolvers",
		Long: `Execute a solution file by running all defined resolvers in dependency order.

Resolvers are organized into phases based on their dependencies. Resolvers within 
the same phase execute concurrently. The command outputs resolved values in the 
specified format (JSON by default).

RESOLVER PARAMETERS:
  Parameters can be passed using -r/--resolver flag in several formats:
    key=value         Simple key-value pair
    @file.yaml        Load parameters from YAML file  
    @file.json        Load parameters from JSON file
    key=val1,val2     Multiple values become an array

EXECUTION ORDER:
  1. Dependencies are analyzed and resolvers grouped into phases
  2. Resolvers in each phase execute concurrently
  3. Each resolver runs: resolve → transform → validate

OUTPUT FORMATS:
  json    JSON output (default)
  yaml    YAML output
  quiet   Suppress output (exit code only)

EXIT CODES:
  0  Success
  1  Resolver execution failed
  2  Validation failed
  3  Invalid solution (cycle/parse error)
  4  File not found

Examples:
  # Run solution with auto-discovery
  scafctl run solution

  # Run solution from specific file
  scafctl run solution -f ./my-solution.yaml

  # Read solution from stdin
  cat solution.yaml | scafctl run solution -f -

  # Run with inline parameters
  scafctl run solution -r env=prod -r region=us-east1

  # Run with parameters from file
  scafctl run solution -r @params.yaml

  # Run only a specific resolver and its dependencies
  scafctl run solution --only database-config

  # Show progress during execution
  scafctl run solution --progress

  # Output as YAML
  scafctl run solution -o yaml`,
		RunE:         makeRunEFunc(cfg, "solution"),
		SilenceUsage: true,
	}

	// Flags
	cCmd.Flags().StringVarP(&options.File, "file", "f", "", "Solution file path (auto-discovered if not provided, use '-' for stdin)")
	cCmd.Flags().StringArrayVarP(&options.ResolverParams, "resolver", "r", nil, "Resolver parameters (key=value or @file.yaml)")
	cCmd.Flags().StringVarP(&options.Output, "output", "o", "json", fmt.Sprintf("Output format: %s", strings.Join(ValidOutputTypes, ", ")))
	cCmd.Flags().StringVar(&options.Only, "only", "", "Execute only this resolver and its dependencies")
	cCmd.Flags().BoolVar(&options.ResolveAll, "resolve-all", false, "Execute all resolvers regardless of action requirements")
	cCmd.Flags().BoolVar(&options.Progress, "progress", false, "Show execution progress (output to stderr)")
	cCmd.Flags().BoolVar(&options.ValidateAll, "validate-all", false, "Continue execution and show all validation/resolver errors")
	cCmd.Flags().BoolVar(&options.SkipValidation, "skip-validation", false, "Skip the validation phase of all resolvers")
	cCmd.Flags().BoolVar(&options.ShowMetrics, "show-metrics", false, "Show provider execution metrics after completion (output to stderr)")
	cCmd.Flags().Int64Var(&options.WarnValueSize, "warn-value-size", 1024*1024, "Warn when value exceeds this size in bytes (default: 1MB)")
	cCmd.Flags().Int64Var(&options.MaxValueSize, "max-value-size", 10*1024*1024, "Fail when value exceeds this size in bytes (default: 10MB)")
	cCmd.Flags().DurationVar(&options.ResolverTimeout, "resolver-timeout", 30*time.Second, "Timeout per resolver")
	cCmd.Flags().DurationVar(&options.PhaseTimeout, "phase-timeout", 5*time.Minute, "Timeout per phase")

	return cCmd
}

// Run executes the solution
func (o *SolutionOptions) Run(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("running solution",
		"file", o.File,
		"output", o.Output,
		"only", o.Only,
		"resolveAll", o.ResolveAll,
		"progress", o.Progress,
		"showMetrics", o.ShowMetrics)

	// Enable metrics collection if requested
	if o.ShowMetrics {
		provider.GlobalMetrics.Enable()
		defer o.writeMetricsSolution()
	}

	// Load the solution
	sol, err := o.loadSolution(ctx)
	if err != nil {
		return o.exitWithCode(err, ExitFileNotFound)
	}

	lgr.V(1).Info("loaded solution",
		"name", sol.Metadata.Name,
		"version", sol.Metadata.Version,
		"resolvers", len(sol.Spec.Resolvers))

	// Check if there are resolvers to execute
	if !sol.Spec.HasResolvers() {
		lgr.V(0).Info("no resolvers defined in solution")
		return o.writeOutput(ctx, map[string]any{})
	}

	// Parse resolver parameters
	params, err := ParseResolverFlags(o.ResolverParams)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to parse resolver parameters: %w", err), ExitValidationFailed)
	}

	lgr.V(1).Info("parsed parameters", "count", len(params))

	// Get resolvers to execute
	resolvers := o.getResolversToExecute(sol)
	if len(resolvers) == 0 {
		lgr.V(0).Info("no resolvers to execute")
		return o.writeOutput(ctx, map[string]any{})
	}

	// Set up provider registry
	reg := o.getRegistry()
	adapter := &registryAdapter{registry: reg}

	// Set up progress reporter if enabled
	var progress *ProgressReporter
	var progressCallback *ProgressCallback
	if o.Progress {
		progress = NewProgressReporter(o.IOStreams.ErrOut, len(resolvers))
		progressCallback = NewProgressCallback(progress)
		defer progress.Wait()
	}

	// Create executor with options
	executorOpts := []resolver.ExecutorOption{
		resolver.WithDefaultTimeout(o.ResolverTimeout),
		resolver.WithPhaseTimeout(o.PhaseTimeout),
	}
	if progressCallback != nil {
		executorOpts = append(executorOpts, resolver.WithProgressCallback(progressCallback))
	}
	if o.ValidateAll {
		executorOpts = append(executorOpts, resolver.WithValidateAll(true))
	}
	if o.SkipValidation {
		executorOpts = append(executorOpts, resolver.WithSkipValidation(true))
	}
	executor := resolver.NewExecutor(adapter, executorOpts...)

	// Execute resolvers
	resultCtx, err := executor.Execute(ctx, resolvers, params)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("resolver execution failed: %w", err), ExitResolverFailed)
	}

	// Get resolver context with results
	resolverCtx, ok := resolver.FromContext(resultCtx)
	if !ok {
		return o.exitWithCode(fmt.Errorf("failed to retrieve resolver results"), ExitResolverFailed)
	}

	// Build output map
	results := o.buildOutputMap(resolverCtx, sol)

	// Check value sizes
	if err := o.checkValueSizes(results, *lgr); err != nil {
		return o.exitWithCode(err, ExitValidationFailed)
	}

	return o.writeOutput(ctx, results)
}

// loadSolution loads the solution from file, stdin, or auto-discovery
func (o *SolutionOptions) loadSolution(ctx context.Context) (*solution.Solution, error) {
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

// getResolversToExecute returns the resolvers to execute based on options
func (o *SolutionOptions) getResolversToExecute(sol *solution.Solution) []*resolver.Resolver {
	resolvers := sol.Spec.ResolversToSlice()

	// If --only is specified, filter to just that resolver and its dependencies
	if o.Only != "" {
		resolvers = o.filterResolverWithDependencies(resolvers, o.Only)
	}

	return resolvers
}

// filterResolverWithDependencies returns the specified resolver and all its dependencies
func (o *SolutionOptions) filterResolverWithDependencies(resolvers []*resolver.Resolver, targetName string) []*resolver.Resolver {
	// Build a map of resolvers by name
	resolverMap := make(map[string]*resolver.Resolver)
	for _, r := range resolvers {
		resolverMap[r.Name] = r
	}

	// Check if target exists
	if _, exists := resolverMap[targetName]; !exists {
		return nil
	}

	// Collect all dependencies recursively
	needed := make(map[string]bool)
	var collectDeps func(name string)
	collectDeps = func(name string) {
		if needed[name] {
			return
		}
		needed[name] = true

		r, exists := resolverMap[name]
		if !exists {
			return
		}

		// Extract dependencies from resolver
		deps := extractResolverDependencies(r)
		for _, dep := range deps {
			collectDeps(dep)
		}
	}

	collectDeps(targetName)

	// Filter resolvers to only those needed
	var result []*resolver.Resolver
	for _, r := range resolvers {
		if needed[r.Name] {
			result = append(result, r)
		}
	}

	return result
}

// extractResolverDependencies extracts dependency names from a resolver
func extractResolverDependencies(r *resolver.Resolver) []string {
	var deps []string
	seen := make(map[string]bool)

	addDep := func(name string) {
		if !seen[name] {
			seen[name] = true
			deps = append(deps, name)
		}
	}

	// Check resolve phase
	if r.Resolve != nil {
		for _, src := range r.Resolve.With {
			for _, vr := range src.Inputs {
				if vr != nil && vr.Resolver != nil {
					addDep(*vr.Resolver)
				}
			}
		}
	}

	// Check transform phase
	if r.Transform != nil {
		for _, tr := range r.Transform.With {
			for _, vr := range tr.Inputs {
				if vr != nil && vr.Resolver != nil {
					addDep(*vr.Resolver)
				}
			}
		}
	}

	// Check validate phase
	if r.Validate != nil {
		for _, vl := range r.Validate.With {
			for _, vr := range vl.Inputs {
				if vr != nil && vr.Resolver != nil {
					addDep(*vr.Resolver)
				}
			}
		}
	}

	return deps
}

// getRegistry returns the provider registry (creates default if not injected)
func (o *SolutionOptions) getRegistry() *provider.Registry {
	if o.registry != nil {
		return o.registry
	}

	// Use DefaultRegistry which handles lazy initialization with sync.Once
	reg, err := builtin.DefaultRegistry()
	if err != nil {
		// Log warning but continue - some providers may already be registered
		lgr := logger.Get(0)
		lgr.V(0).Info("warning: failed to register some providers", "error", err)
		// Fall back to global registry
		return provider.GetGlobalRegistry()
	}

	return reg
}

// buildOutputMap builds the output map from resolver results
func (o *SolutionOptions) buildOutputMap(resolverCtx *resolver.Context, sol *solution.Solution) map[string]any {
	results := make(map[string]any)

	for name, r := range sol.Spec.Resolvers {
		result, ok := resolverCtx.GetResult(name)
		if !ok {
			continue
		}

		// Skip if resolver was skipped and we're not in resolve-all mode
		if result.Status == resolver.ExecutionStatusSkipped && !o.ResolveAll {
			continue
		}

		// Redact sensitive values
		if r.Sensitive {
			results[name] = "[REDACTED]"
		} else {
			results[name] = result.Value
		}
	}

	return results
}

// checkValueSizes checks if any values exceed size limits
func (o *SolutionOptions) checkValueSizes(results map[string]any, lgr logr.Logger) error {
	for name, value := range results {
		size := calculateValueSize(value)

		if o.MaxValueSize > 0 && size > o.MaxValueSize {
			return fmt.Errorf("resolver %q value exceeds maximum size: %d > %d bytes", name, size, o.MaxValueSize)
		}

		if o.WarnValueSize > 0 && size > o.WarnValueSize {
			lgr.V(0).Info("resolver value exceeds recommended size",
				"resolver", name,
				"size", size,
				"limit", o.WarnValueSize)
		}
	}

	return nil
}

// calculateValueSize estimates the size of a value in bytes
func calculateValueSize(value any) int64 {
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return int64(len(data))
}

// writeOutput writes the results in the specified format
func (o *SolutionOptions) writeOutput(_ context.Context, results map[string]any) error {
	if o.Output == "quiet" {
		return nil
	}

	var data []byte
	var err error

	switch o.Output {
	case "yaml":
		data, err = yaml.Marshal(results)
	case "json", "":
		data, err = json.MarshalIndent(results, "", "  ")
	default:
		return fmt.Errorf("unsupported output format: %s", o.Output)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	fmt.Fprintln(o.IOStreams.Out, string(data))
	return nil
}

// exitWithCode returns the error with appropriate exit handling
func (o *SolutionOptions) exitWithCode(err error, _ int) error {
	writeError(o, err.Error())
	// The exit code is handled by the caller based on the error type
	// For now, we just return the error
	return err
}

// writeError writes an error message using the output package
func writeError(o *SolutionOptions, msg string) {
	output.NewWriteMessageOptions(
		o.IOStreams,
		output.MessageTypeError,
		o.CliParams.NoColor,
		o.CliParams.ExitOnError,
	).WriteMessage(msg)
}

// registryAdapter adapts provider.Registry to resolver.RegistryInterface
type registryAdapter struct {
	registry *provider.Registry
}

func (r *registryAdapter) Register(p provider.Provider) error {
	return r.registry.Register(p)
}

func (r *registryAdapter) Get(name string) (provider.Provider, error) {
	p, ok := r.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

func (r *registryAdapter) List() []provider.Provider {
	return r.registry.ListProviders()
}

func (r *registryAdapter) DescriptorLookup() resolver.DescriptorLookup {
	return r.registry.DescriptorLookup()
}

// writeMetricsSolution outputs provider execution metrics to stderr
func (o *SolutionOptions) writeMetricsSolution() {
	writeMetrics(o.IOStreams.ErrOut)
}
