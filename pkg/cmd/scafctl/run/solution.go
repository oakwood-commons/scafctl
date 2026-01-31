package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/cmd/flags"
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
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// ValidOutputTypes defines the supported output formats
var ValidOutputTypes = kvx.BaseOutputFormats()

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

	// Action execution options
	ActionTimeout        time.Duration
	MaxActionConcurrency int
	DryRun               bool
	SkipActions          bool

	// kvx output integration (shared flags)
	flags.KvxOutputFlags

	// Track which flags were explicitly set by user
	flagsChanged map[string]bool

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
		Aliases: []string{"sol", "s", "solutions"},
		Short:   "Run a solution by executing resolvers and actions",
		Long: `Execute a solution file by running resolvers and then actions in dependency order.

The execution proceeds in two phases:
1. RESOLVER PHASE: All resolvers execute in dependency order (concurrent within phases)
2. ACTION PHASE: Actions execute using resolved data (if workflow defined)

RESOLVER PARAMETERS:
  Parameters can be passed using -r/--resolver flag in several formats:
    key=value         Simple key-value pair
    @file.yaml        Load parameters from YAML file  
    @file.json        Load parameters from JSON file
    key=val1,val2     Multiple values become an array

EXECUTION ORDER:
  1. Parse and validate solution
  2. Execute resolvers in dependency phases (concurrent within phases)
  3. If workflow defined: build action graph and execute actions
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
  3  Invalid solution (cycle/parse error)
  4  File not found
  6  Action/workflow execution failed

Examples:
  # Run solution (resolvers + actions)
  scafctl run solution -f ./my-solution.yaml

  # Run with parameters
  scafctl run solution -r env=prod -r region=us-east1

  # Dry run (validate and show what would execute)
  scafctl run solution -f ./my-solution.yaml --dry-run

  # Run resolvers only (skip actions)
  scafctl run solution -f ./my-solution.yaml --skip-actions

  # Explore resolver results interactively
  scafctl run solution -f ./my-solution.yaml --skip-actions -i

  # JSON output for piping
  scafctl run solution -f ./my-solution.yaml -o json | jq .

  # Limit concurrent actions
  scafctl run solution --max-action-concurrency=2

  # Show progress during execution
  scafctl run solution --progress`,
		PreRun: func(cCmd *cobra.Command, _ []string) {
			// Track which flags were explicitly set by the user
			options.flagsChanged = make(map[string]bool)
			cCmd.Flags().Visit(func(f *pflag.Flag) {
				options.flagsChanged[f.Name] = true
			})
		},
		RunE:         makeRunEFunc(cfg, "solution"),
		SilenceUsage: true,
	}

	// Flags
	cCmd.Flags().StringVarP(&options.File, "file", "f", "", "Solution file path (auto-discovered if not provided, use '-' for stdin)")
	cCmd.Flags().StringArrayVarP(&options.ResolverParams, "resolver", "r", nil, "Resolver parameters (key=value or @file.yaml)")
	// Add shared kvx output flags (-o, -i, -e)
	flags.AddKvxOutputFlagsToStruct(cCmd, &options.KvxOutputFlags)

	// Command-specific flags
	cCmd.Flags().StringVar(&options.Only, "only", "", "Execute only this resolver and its dependencies")
	cCmd.Flags().BoolVar(&options.ResolveAll, "resolve-all", false, "Execute all resolvers regardless of action requirements")
	cCmd.Flags().BoolVar(&options.Progress, "progress", false, "Show execution progress (output to stderr)")
	cCmd.Flags().BoolVar(&options.ValidateAll, "validate-all", false, "Continue execution and show all validation/resolver errors")
	cCmd.Flags().BoolVar(&options.SkipValidation, "skip-validation", false, "Skip the validation phase of all resolvers")
	cCmd.Flags().BoolVar(&options.ShowMetrics, "show-metrics", false, "Show provider execution metrics after completion (output to stderr)")
	cCmd.Flags().Int64Var(&options.WarnValueSize, "warn-value-size", settings.DefaultWarnValueSize, "Warn when value exceeds this size in bytes (default: 1MB)")
	cCmd.Flags().Int64Var(&options.MaxValueSize, "max-value-size", settings.DefaultMaxValueSize, "Fail when value exceeds this size in bytes (default: 10MB)")
	cCmd.Flags().DurationVar(&options.ResolverTimeout, "resolver-timeout", settings.DefaultResolverTimeout, "Timeout per resolver")
	cCmd.Flags().DurationVar(&options.PhaseTimeout, "phase-timeout", settings.DefaultPhaseTimeout, "Timeout per resolver phase")

	// Action execution flags
	cCmd.Flags().DurationVar(&options.ActionTimeout, "action-timeout", settings.DefaultActionTimeout, "Default timeout per action")
	cCmd.Flags().IntVar(&options.MaxActionConcurrency, "max-action-concurrency", 0, "Maximum concurrent actions (0=unlimited)")
	cCmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Validate and show what would be executed without running")
	cCmd.Flags().BoolVar(&options.SkipActions, "skip-actions", false, "Execute resolvers only, skip actions")

	return cCmd
}

// getEffectiveResolverConfig returns resolver config values, using app config
// as defaults when CLI flags weren't explicitly set.
func (o *SolutionOptions) getEffectiveResolverConfig(ctx context.Context) config.ResolverConfigValues {
	// Start with CLI flag values (which already have settings package defaults)
	result := config.ResolverConfigValues{
		Timeout:        o.ResolverTimeout,
		PhaseTimeout:   o.PhaseTimeout,
		MaxConcurrency: 0, // Not currently a CLI flag, use config if available
		WarnValueSize:  o.WarnValueSize,
		MaxValueSize:   o.MaxValueSize,
		ValidateAll:    o.ValidateAll,
	}

	// If config is available, use its values for non-changed flags
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return result
	}

	// Parse config values
	configValues, err := cfg.Resolver.ToResolverValues()
	if err != nil {
		// If config parsing fails, log and use CLI defaults
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
		if !o.flagsChanged["warn-value-size"] {
			result.WarnValueSize = configValues.WarnValueSize
		}
		if !o.flagsChanged["max-value-size"] {
			result.MaxValueSize = configValues.MaxValueSize
		}
		if !o.flagsChanged["validate-all"] {
			result.ValidateAll = configValues.ValidateAll
		}
	}

	// MaxConcurrency always comes from config (no CLI flag for resolver concurrency)
	result.MaxConcurrency = configValues.MaxConcurrency

	return result
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
		// If config parsing fails, log and use CLI defaults
		lgr := logger.FromContext(ctx)
		lgr.V(1).Info("failed to parse action config, using CLI defaults", "error", err)
		return result
	}

	// Override with config values for flags that weren't explicitly set.
	// Only apply overrides when flagsChanged is set (i.e., we're in command execution flow).
	// When flagsChanged is nil (e.g., in tests), respect the values set on the options struct.
	if o.flagsChanged != nil {
		if !o.flagsChanged["action-timeout"] {
			result.DefaultTimeout = configValues.DefaultTimeout
		}
		if !o.flagsChanged["max-action-concurrency"] {
			result.MaxConcurrency = configValues.MaxConcurrency
		}
	}

	// GracePeriod always comes from config (no CLI flag)
	result.GracePeriod = configValues.GracePeriod

	return result
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
		"dryRun", o.DryRun,
		"skipActions", o.SkipActions,
		"showMetrics", o.ShowMetrics)

	// Enable metrics collection if requested
	if o.ShowMetrics {
		provider.GlobalMetrics.Enable()
		defer o.writeMetricsSolution()
	}

	// Load the solution
	sol, err := o.loadSolution(ctx)
	if err != nil {
		return o.exitWithCode(err, exitcode.FileNotFound)
	}

	lgr.V(1).Info("loaded solution",
		"name", sol.Metadata.Name,
		"version", sol.Metadata.Version,
		"hasResolvers", sol.Spec.HasResolvers(),
		"hasWorkflow", sol.Spec.HasWorkflow())

	// Set up provider registry
	reg := o.getRegistry()
	actionAdapter := &actionRegistryAdapter{registry: reg}

	// Validate the workflow if present and not skipping actions
	if sol.Spec.HasWorkflow() && !o.SkipActions {
		if err := action.ValidateWorkflow(sol.Spec.Workflow, actionAdapter); err != nil {
			return o.exitWithCode(fmt.Errorf("workflow validation failed: %w", err), exitcode.ValidationFailed)
		}
	}

	// Parse resolver parameters
	params, err := ParseResolverFlags(o.ResolverParams)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to parse resolver parameters: %w", err), exitcode.ValidationFailed)
	}

	lgr.V(1).Info("parsed parameters", "count", len(params))

	// Build resolver data map
	resolverData := make(map[string]any)

	// Execute resolvers if present
	if sol.Spec.HasResolvers() {
		// Get resolvers to execute
		resolvers := o.getResolversToExecute(sol)
		if len(resolvers) == 0 {
			lgr.V(0).Info("no resolvers to execute")
		} else {
			resolverAdapter := &registryAdapter{registry: reg}

			// Set up progress reporter if enabled
			var progress *ProgressReporter
			var progressCallback *ProgressCallback
			if o.Progress {
				progress = NewProgressReporter(o.IOStreams.ErrOut, len(resolvers))
				progressCallback = NewProgressCallback(progress)
			}

			// Get effective resolver config (CLI flags override app config)
			resolverCfg := o.getEffectiveResolverConfig(ctx)

			// Create executor with options
			executorOpts := []resolver.ExecutorOption{
				resolver.WithDefaultTimeout(resolverCfg.Timeout),
				resolver.WithPhaseTimeout(resolverCfg.PhaseTimeout),
			}
			if resolverCfg.MaxConcurrency > 0 {
				executorOpts = append(executorOpts, resolver.WithMaxConcurrency(resolverCfg.MaxConcurrency))
			}
			if resolverCfg.WarnValueSize > 0 {
				executorOpts = append(executorOpts, resolver.WithWarnValueSize(resolverCfg.WarnValueSize))
			}
			if resolverCfg.MaxValueSize > 0 {
				executorOpts = append(executorOpts, resolver.WithMaxValueSize(resolverCfg.MaxValueSize))
			}
			if progressCallback != nil {
				executorOpts = append(executorOpts, resolver.WithProgressCallback(progressCallback))
			}
			if resolverCfg.ValidateAll {
				executorOpts = append(executorOpts, resolver.WithValidateAll(true))
			}
			if o.SkipValidation {
				executorOpts = append(executorOpts, resolver.WithSkipValidation(true))
			}
			executor := resolver.NewExecutor(resolverAdapter, executorOpts...)

			// Execute resolvers
			resultCtx, err := executor.Execute(ctx, resolvers, params)
			if err != nil {
				// Wait for progress to complete before returning error
				if progress != nil {
					progress.Wait()
				}
				return o.exitWithCode(fmt.Errorf("resolver execution failed: %w", err), exitcode.GeneralError)
			}

			// Get resolver context with results
			resolverCtx, ok := resolver.FromContext(resultCtx)
			if !ok {
				// Wait for progress to complete before returning error
				if progress != nil {
					progress.Wait()
				}
				return o.exitWithCode(fmt.Errorf("failed to retrieve resolver results"), exitcode.GeneralError)
			}

			// Build resolver data map for actions
			for name := range sol.Spec.Resolvers {
				result, ok := resolverCtx.GetResult(name)
				if ok && result.Status == resolver.ExecutionStatusSuccess {
					resolverData[name] = result.Value
				}
			}

			// Wait for progress bars to complete
			if progress != nil {
				progress.Wait()
			}

			lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
		}
	}

	// If skipping actions or no workflow, output resolver results
	if o.SkipActions || !sol.Spec.HasWorkflow() {
		results := o.buildResolverOutputMap(resolverData, sol)
		if err := o.checkValueSizes(results, *lgr); err != nil {
			return o.exitWithCode(err, exitcode.ValidationFailed)
		}
		return o.writeOutput(ctx, results)
	}

	// Build action graph
	graph, err := action.BuildGraph(ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to build action graph: %w", err), exitcode.InvalidInput)
	}

	lgr.V(1).Info("action graph built",
		"totalActions", len(graph.Actions),
		"mainPhases", len(graph.ExecutionOrder),
		"finallyPhases", len(graph.FinallyOrder))

	// Dry run - just show what would be executed
	if o.DryRun {
		o.showDryRun(graph)
		return nil
	}

	// Set up action progress callback if enabled
	var actionProgressCallback action.ProgressCallback
	if o.Progress {
		actionProgressCallback = NewActionProgressCallback(o.IOStreams.ErrOut)
	}

	// Get effective action config (CLI flags override app config)
	actionCfg := o.getEffectiveActionConfig(ctx)

	// Execute actions
	actionExecutor := action.NewExecutor(
		action.WithRegistry(actionAdapter),
		action.WithResolverData(resolverData),
		action.WithProgressCallback(actionProgressCallback),
		action.WithDefaultTimeout(actionCfg.DefaultTimeout),
		action.WithGracePeriod(actionCfg.GracePeriod),
		action.WithMaxConcurrency(actionCfg.MaxConcurrency),
	)

	result, err := actionExecutor.Execute(ctx, sol.Spec.Workflow)
	if err != nil && result != nil && result.FinalStatus != action.ExecutionPartialSuccess {
		return o.exitWithCode(fmt.Errorf("action execution failed: %w", err), exitcode.ActionFailed)
	}

	// Build and write output
	return o.writeActionOutput(ctx, result)
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

// writeOutput writes the results in the specified format using the shared kvx output handler.
func (o *SolutionOptions) writeOutput(ctx context.Context, results map[string]any) error {
	// Use the shared kvx output infrastructure
	kvxOpts := flags.NewKvxOutputOptionsFromFlags(
		o.Output,
		o.Interactive,
		o.Expression,
		kvx.WithOutputContext(ctx),
		kvx.WithOutputNoColor(o.CliParams.NoColor),
		kvx.WithOutputAppName("scafctl run solution"),
		kvx.WithOutputHelp("scafctl run solution", []string{
			"Resolver Results Viewer",
			"",
			"Navigate: ↑↓ arrows | Back: ← | Enter: →",
			"Search: / or F3 | Expression: F6",
			"Copy path: F5 | Quit: q or F10",
		}),
	)
	kvxOpts.IOStreams = o.IOStreams

	return kvxOpts.Write(results)
}

// exitWithCode returns the error with appropriate exit handling
func (o *SolutionOptions) exitWithCode(err error, _ int) error {
	return err
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

// buildResolverOutputMap builds the output map from resolver data with redaction for sensitive values
func (o *SolutionOptions) buildResolverOutputMap(resolverData map[string]any, sol *solution.Solution) map[string]any {
	results := make(map[string]any)

	for name, value := range resolverData {
		// Check if resolver is marked as sensitive
		if r, ok := sol.Spec.Resolvers[name]; ok && r.Sensitive {
			results[name] = "[REDACTED]"
		} else {
			results[name] = value
		}
	}

	return results
}

// showDryRun displays what would be executed without actually running
func (o *SolutionOptions) showDryRun(graph *action.Graph) {
	fmt.Fprintln(o.IOStreams.Out, "=== DRY RUN ===")
	fmt.Fprintln(o.IOStreams.Out, "")
	fmt.Fprintf(o.IOStreams.Out, "Total actions: %d\n", len(graph.Actions))
	fmt.Fprintf(o.IOStreams.Out, "Main phases: %d\n", len(graph.ExecutionOrder))
	fmt.Fprintf(o.IOStreams.Out, "Finally phases: %d\n", len(graph.FinallyOrder))
	fmt.Fprintln(o.IOStreams.Out, "")

	fmt.Fprintln(o.IOStreams.Out, "EXECUTION ORDER:")
	for i, phase := range graph.ExecutionOrder {
		fmt.Fprintf(o.IOStreams.Out, "  Phase %d: %s\n", i, strings.Join(phase, ", "))
	}

	if len(graph.FinallyOrder) > 0 {
		fmt.Fprintln(o.IOStreams.Out, "")
		fmt.Fprintln(o.IOStreams.Out, "FINALLY ORDER:")
		for i, phase := range graph.FinallyOrder {
			fmt.Fprintf(o.IOStreams.Out, "  Phase %d: %s\n", i, strings.Join(phase, ", "))
		}
	}

	fmt.Fprintln(o.IOStreams.Out, "")
	fmt.Fprintln(o.IOStreams.Out, "ACTIONS:")
	for name, act := range graph.Actions {
		fmt.Fprintf(o.IOStreams.Out, "  %s:\n", name)
		fmt.Fprintf(o.IOStreams.Out, "    provider: %s\n", act.Provider)
		if len(act.Dependencies) > 0 {
			fmt.Fprintf(o.IOStreams.Out, "    dependencies: %s\n", strings.Join(act.Dependencies, ", "))
		}
		if act.ForEachMetadata != nil {
			fmt.Fprintf(o.IOStreams.Out, "    forEach: expanded from %s[%d]\n", act.ForEachMetadata.ExpandedFrom, act.ForEachMetadata.Index)
		}
	}
}

// writeActionOutput writes the action execution results
func (o *SolutionOptions) writeActionOutput(_ context.Context, result *action.ExecutionResult) error {
	if o.Output == "quiet" {
		return nil
	}

	// Build output structure
	output := map[string]any{
		"status":    string(result.FinalStatus),
		"startTime": result.StartTime.Format(time.RFC3339),
		"endTime":   result.EndTime.Format(time.RFC3339),
		"duration":  result.Duration().String(),
	}

	// Add action results
	actions := make(map[string]any)
	for name, ar := range result.Actions {
		actionOutput := map[string]any{
			"status": string(ar.Status),
		}
		if ar.Results != nil {
			actionOutput["results"] = ar.Results
		}
		if ar.Error != "" {
			actionOutput["error"] = ar.Error
		}
		if ar.SkipReason != "" {
			actionOutput["skipReason"] = string(ar.SkipReason)
		}
		actions[name] = actionOutput
	}
	output["actions"] = actions

	if len(result.FailedActions) > 0 {
		output["failedActions"] = result.FailedActions
	}
	if len(result.SkippedActions) > 0 {
		output["skippedActions"] = result.SkippedActions
	}

	var data []byte
	var err error

	switch o.Output {
	case "yaml":
		data, err = yaml.Marshal(output)
	case "json", "":
		data, err = json.MarshalIndent(output, "", "  ")
	default:
		return fmt.Errorf("unsupported output format: %s", o.Output)
	}

	if err != nil {
		return fmt.Errorf("failed to marshal output: %w", err)
	}

	fmt.Fprintln(o.IOStreams.Out, string(data))
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
	out io.Writer
}

// NewActionProgressCallback creates a new action progress callback
func NewActionProgressCallback(out io.Writer) *ActionProgressCallback {
	return &ActionProgressCallback{out: out}
}

func (a *ActionProgressCallback) OnActionStart(actionName string) {
	fmt.Fprintf(a.out, "[ACTION] Starting: %s\n", actionName)
}

func (a *ActionProgressCallback) OnActionComplete(actionName string, _ any) {
	fmt.Fprintf(a.out, "[ACTION] Completed: %s ✓\n", actionName)
}

func (a *ActionProgressCallback) OnActionFailed(actionName string, err error) {
	fmt.Fprintf(a.out, "[ACTION] Failed: %s ✗ (%v)\n", actionName, err)
}

func (a *ActionProgressCallback) OnActionSkipped(actionName, reason string) {
	fmt.Fprintf(a.out, "[ACTION] Skipped: %s (%s)\n", actionName, reason)
}

func (a *ActionProgressCallback) OnActionTimeout(actionName string, timeout time.Duration) {
	fmt.Fprintf(a.out, "[ACTION] Timeout: %s (after %v)\n", actionName, timeout)
}

func (a *ActionProgressCallback) OnActionCancelled(actionName string) {
	fmt.Fprintf(a.out, "[ACTION] Cancelled: %s\n", actionName)
}

func (a *ActionProgressCallback) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	fmt.Fprintf(a.out, "[ACTION] Retry %d/%d for %s: %v\n", attempt, maxAttempts, actionName, err)
}

func (a *ActionProgressCallback) OnForEachProgress(actionName string, completed, total int) {
	fmt.Fprintf(a.out, "[ACTION] %s: %d/%d iterations complete\n", actionName, completed, total)
}

func (a *ActionProgressCallback) OnPhaseStart(phase int, actionNames []string) {
	fmt.Fprintf(a.out, "[PHASE] Starting phase %d: %s\n", phase, strings.Join(actionNames, ", "))
}

func (a *ActionProgressCallback) OnPhaseComplete(phase int) {
	fmt.Fprintf(a.out, "[PHASE] Completed phase %d\n", phase)
}

func (a *ActionProgressCallback) OnFinallyStart() {
	fmt.Fprintf(a.out, "[FINALLY] Starting finally section\n")
}

func (a *ActionProgressCallback) OnFinallyComplete() {
	fmt.Fprintf(a.out, "[FINALLY] Completed finally section\n")
}
