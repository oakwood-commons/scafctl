package run

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
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

// Exit codes for the workflow command (in addition to standard solution exit codes)
const (
	ExitWorkflowFailed = 6 // Workflow execution failed
	ExitNoWorkflow     = 7 // No workflow defined in solution
)

// WorkflowOptions holds configuration for the run workflow command
type WorkflowOptions struct {
	IOStreams            *terminal.IOStreams
	CliParams            *settings.Run
	Output               string
	File                 string
	ResolverParams       []string
	Progress             bool
	ShowMetrics          bool
	ResolverTimeout      time.Duration
	PhaseTimeout         time.Duration
	ActionTimeout        time.Duration
	MaxActionConcurrency int
	DryRun               bool

	// For dependency injection in tests
	getter   get.Interface
	registry *provider.Registry
}

// CommandWorkflow creates the 'run workflow' subcommand
func CommandWorkflow(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
	options := &WorkflowOptions{}

	cfg := runCommandConfig{
		cliParams: cliParams,
		ioStreams: ioStreams,
		path:      path,
		runner:    options,
		writeErrorFn: func(msg string) {
			writeWorkflowError(options, msg)
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
		Use:     "workflow",
		Aliases: []string{"wf", "w"},
		Short:   "Run a solution's workflow (resolvers then actions)",
		Long: `Execute a solution's complete workflow including resolvers and actions.

The workflow execution proceeds in two phases:
1. RESOLVER PHASE: All resolvers are executed in dependency order
2. ACTION PHASE: Actions are executed using resolved data

Actions are executed in phases based on their dependencies. Actions within
the same phase run concurrently (up to --max-action-concurrency limit).
Finally section actions always execute, even if main actions fail.

EXECUTION ORDER:
  1. Parse and validate solution
  2. Execute resolvers in dependency order
  3. Build action graph with materialized inputs
  4. Execute main actions in dependency phases
  5. Execute finally actions (always runs)

OUTPUT FORMATS:
  quiet   Suppress structured output (default) - just show progress
  json    JSON output - action results for scripting/pipelines
  yaml    YAML output - action results

EXIT CODES:
  0  Success
  1  Resolver execution failed
  2  Validation failed
  3  Invalid solution (cycle/parse error)
  4  File not found
  6  Workflow/action execution failed
  7  No workflow defined in solution

Examples:
  # Run workflow with auto-discovery
  scafctl run workflow

  # Run workflow from specific file
  scafctl run workflow -f ./solution.yaml

  # Run with parameters
  scafctl run workflow -r env=prod -r region=us-east1

  # Run without progress output (for scripts)
  scafctl run workflow --no-progress -o json

  # Dry run (validate and show what would execute)
  scafctl run workflow --dry-run

  # Limit concurrent actions
  scafctl run workflow --max-action-concurrency=2`,
		RunE:         makeRunEFunc(cfg, "workflow"),
		SilenceUsage: true,
	}

	// Flags
	cCmd.Flags().StringVarP(&options.File, "file", "f", "", "Solution file path (auto-discovered if not provided, use '-' for stdin)")
	cCmd.Flags().StringArrayVarP(&options.ResolverParams, "resolver", "r", nil, "Resolver parameters (key=value or @file.yaml)")
	cCmd.Flags().StringVarP(&options.Output, "output", "o", "quiet", fmt.Sprintf("Output format: %s", strings.Join(ValidOutputTypes, ", ")))
	cCmd.Flags().BoolVar(&options.Progress, "no-progress", false, "Disable execution progress output")
	cCmd.Flags().BoolVar(&options.ShowMetrics, "show-metrics", false, "Show provider execution metrics after completion")
	cCmd.Flags().DurationVar(&options.ResolverTimeout, "resolver-timeout", 30*time.Second, "Timeout per resolver")
	cCmd.Flags().DurationVar(&options.PhaseTimeout, "phase-timeout", 5*time.Minute, "Timeout per resolver phase")
	cCmd.Flags().DurationVar(&options.ActionTimeout, "action-timeout", 5*time.Minute, "Default timeout per action")
	cCmd.Flags().IntVar(&options.MaxActionConcurrency, "max-action-concurrency", 0, "Maximum concurrent actions (0=unlimited)")
	cCmd.Flags().BoolVar(&options.DryRun, "dry-run", false, "Validate and show what would be executed without running")

	return cCmd
}

// Run executes the workflow
func (o *WorkflowOptions) Run(ctx context.Context) error {
	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("running workflow",
		"file", o.File,
		"output", o.Output,
		"noProgress", o.Progress,
		"dryRun", o.DryRun,
		"maxActionConcurrency", o.MaxActionConcurrency)

	// Enable metrics collection if requested
	if o.ShowMetrics {
		provider.GlobalMetrics.Enable()
		defer o.writeMetricsWorkflow()
	}

	// Load the solution
	sol, err := o.loadSolution(ctx)
	if err != nil {
		return o.exitWithCode(err, ExitFileNotFound)
	}

	lgr.V(1).Info("loaded solution",
		"name", sol.Metadata.Name,
		"version", sol.Metadata.Version,
		"hasResolvers", sol.Spec.HasResolvers(),
		"hasWorkflow", sol.Spec.HasWorkflow())

	// Check if there's a workflow
	if !sol.Spec.HasWorkflow() {
		return o.exitWithCode(fmt.Errorf("solution does not define a workflow"), ExitNoWorkflow)
	}

	// Set up provider registry
	reg := o.getRegistry()
	adapter := &workflowRegistryAdapter{registry: reg}

	// Validate the workflow
	if err := action.ValidateWorkflow(sol.Spec.Workflow, adapter); err != nil {
		return o.exitWithCode(fmt.Errorf("workflow validation failed: %w", err), ExitValidationFailed)
	}

	// Parse resolver parameters
	params, err := ParseResolverFlags(o.ResolverParams)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to parse resolver parameters: %w", err), ExitValidationFailed)
	}

	lgr.V(1).Info("parsed parameters", "count", len(params))

	// Set up progress reporter if enabled (default is on, disabled with --no-progress)
	var progressCallback action.ProgressCallback
	if !o.Progress {
		progressCallback = NewWorkflowProgressCallback(o.IOStreams.ErrOut)
	}

	// Execute resolvers first
	resolverData := make(map[string]any)
	if sol.Spec.HasResolvers() {
		lgr.V(1).Info("executing resolvers")

		// Set up resolver progress reporter if enabled (default is on, disabled with --no-progress)
		var resolverProgressCallback *ProgressCallback
		var resolverProgress *ProgressReporter
		if !o.Progress {
			resolvers := sol.Spec.ResolversToSlice()
			resolverProgress = NewProgressReporter(o.IOStreams.ErrOut, len(resolvers))
			resolverProgressCallback = NewProgressCallback(resolverProgress)
		}

		// Create resolver registry adapter (resolver.RegistryInterface requires Get returning error)
		resolverAdapter := &resolverRegistryAdapter{workflowRegistryAdapter: adapter}

		// Create resolver executor
		executorOpts := []resolver.ExecutorOption{
			resolver.WithDefaultTimeout(o.ResolverTimeout),
			resolver.WithPhaseTimeout(o.PhaseTimeout),
		}
		if resolverProgressCallback != nil {
			executorOpts = append(executorOpts, resolver.WithProgressCallback(resolverProgressCallback))
		}
		executor := resolver.NewExecutor(resolverAdapter, executorOpts...)

		// Execute resolvers
		resolvers := sol.Spec.ResolversToSlice()
		resultCtx, err := executor.Execute(ctx, resolvers, params)

		// Wait for resolver progress output to complete before continuing
		if resolverProgress != nil {
			resolverProgress.Wait()
		}

		if err != nil {
			return o.exitWithCode(fmt.Errorf("resolver execution failed: %w", err), ExitResolverFailed)
		}

		// Build resolver data map
		resolverCtx, ok := resolver.FromContext(resultCtx)
		if !ok {
			return o.exitWithCode(fmt.Errorf("failed to retrieve resolver results"), ExitResolverFailed)
		}

		for name := range sol.Spec.Resolvers {
			result, ok := resolverCtx.GetResult(name)
			if ok && result.Status == resolver.ExecutionStatusSuccess {
				resolverData[name] = result.Value
			}
		}

		lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
	}

	// Build action graph
	graph, err := action.BuildGraph(ctx, sol.Spec.Workflow, resolverData, nil)
	if err != nil {
		return o.exitWithCode(fmt.Errorf("failed to build action graph: %w", err), ExitInvalidSolution)
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

	// Execute actions
	actionExecutor := action.NewExecutor(
		action.WithRegistry(adapter),
		action.WithResolverData(resolverData),
		action.WithProgressCallback(progressCallback),
		action.WithDefaultTimeout(o.ActionTimeout),
		action.WithMaxConcurrency(o.MaxActionConcurrency),
	)

	result, err := actionExecutor.Execute(ctx, sol.Spec.Workflow)
	if err != nil && result != nil && result.FinalStatus != action.ExecutionPartialSuccess {
		return o.exitWithCode(fmt.Errorf("workflow execution failed: %w", err), ExitWorkflowFailed)
	}

	// Build output
	return o.writeWorkflowOutput(ctx, result)
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

// showDryRun displays what would be executed without actually running
func (o *WorkflowOptions) showDryRun(graph *action.Graph) {
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

// writeWorkflowOutput writes the workflow execution results
func (o *WorkflowOptions) writeWorkflowOutput(_ context.Context, result *action.ExecutionResult) error {
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

// writeMetricsWorkflow outputs provider execution metrics
func (o *WorkflowOptions) writeMetricsWorkflow() {
	writeMetrics(o.IOStreams.ErrOut)
}

// exitWithCode returns the error with appropriate exit handling
func (o *WorkflowOptions) exitWithCode(err error, _ int) error {
	writeWorkflowError(o, err.Error())
	return err
}

// writeWorkflowError writes an error message
func writeWorkflowError(o *WorkflowOptions, msg string) {
	output.NewWriteMessageOptions(
		o.IOStreams,
		output.MessageTypeError,
		o.CliParams.NoColor,
		o.CliParams.ExitOnError,
	).WriteMessage(msg)
}

// workflowRegistryAdapter adapts provider.Registry to both action and resolver interfaces
type workflowRegistryAdapter struct {
	registry *provider.Registry
}

// Get returns a provider by name (for action.RegistryInterface - returns bool)
func (r *workflowRegistryAdapter) Get(name string) (provider.Provider, bool) {
	return r.registry.Get(name)
}

// Has checks if a provider exists (for action.RegistryInterface)
func (r *workflowRegistryAdapter) Has(name string) bool {
	_, ok := r.registry.Get(name)
	return ok
}

// For resolver.RegistryInterface compatibility
func (r *workflowRegistryAdapter) Register(p provider.Provider) error {
	return r.registry.Register(p)
}

func (r *workflowRegistryAdapter) List() []provider.Provider {
	return r.registry.ListProviders()
}

func (r *workflowRegistryAdapter) DescriptorLookup() resolver.DescriptorLookup {
	return r.registry.DescriptorLookup()
}

// resolverRegistryAdapter adapts workflowRegistryAdapter to resolver.RegistryInterface
type resolverRegistryAdapter struct {
	*workflowRegistryAdapter
}

// Get implements resolver.RegistryInterface with error return
func (r *resolverRegistryAdapter) Get(name string) (provider.Provider, error) {
	p, ok := r.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

// WorkflowProgressCallback implements action.ProgressCallback for CLI output
type WorkflowProgressCallback struct {
	out io.Writer
}

// NewWorkflowProgressCallback creates a new workflow progress callback
func NewWorkflowProgressCallback(out io.Writer) *WorkflowProgressCallback {
	return &WorkflowProgressCallback{out: out}
}

func (w *WorkflowProgressCallback) OnActionStart(actionName string) {
	fmt.Fprintf(w.out, "[ACTION] Starting: %s\n", actionName)
}

func (w *WorkflowProgressCallback) OnActionComplete(actionName string, _ any) {
	fmt.Fprintf(w.out, "[ACTION] Completed: %s ✓\n", actionName)
}

func (w *WorkflowProgressCallback) OnActionFailed(actionName string, err error) {
	fmt.Fprintf(w.out, "[ACTION] Failed: %s ✗ (%v)\n", actionName, err)
}

func (w *WorkflowProgressCallback) OnActionSkipped(actionName, reason string) {
	fmt.Fprintf(w.out, "[ACTION] Skipped: %s (%s)\n", actionName, reason)
}

func (w *WorkflowProgressCallback) OnActionTimeout(actionName string, timeout time.Duration) {
	fmt.Fprintf(w.out, "[ACTION] Timeout: %s (after %v)\n", actionName, timeout)
}

func (w *WorkflowProgressCallback) OnActionCancelled(actionName string) {
	fmt.Fprintf(w.out, "[ACTION] Cancelled: %s\n", actionName)
}

func (w *WorkflowProgressCallback) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	fmt.Fprintf(w.out, "[ACTION] Retry %d/%d for %s: %v\n", attempt, maxAttempts, actionName, err)
}

func (w *WorkflowProgressCallback) OnForEachProgress(actionName string, completed, total int) {
	fmt.Fprintf(w.out, "[ACTION] %s: %d/%d iterations complete\n", actionName, completed, total)
}

func (w *WorkflowProgressCallback) OnPhaseStart(phase int, actionNames []string) {
	fmt.Fprintf(w.out, "[PHASE] Starting phase %d: %s\n", phase, strings.Join(actionNames, ", "))
}

func (w *WorkflowProgressCallback) OnPhaseComplete(phase int) {
	fmt.Fprintf(w.out, "[PHASE] Completed phase %d\n", phase)
}

func (w *WorkflowProgressCallback) OnFinallyStart() {
	fmt.Fprintf(w.out, "[FINALLY] Starting finally section\n")
}

func (w *WorkflowProgressCallback) OnFinallyComplete() {
	fmt.Fprintf(w.out, "[FINALLY] Completed finally section\n")
}
