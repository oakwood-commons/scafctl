// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Executor runs actions in dependency order with support for parallel execution,
// retry, timeout, and error handling.
type Executor struct {
	// registry provides access to action providers
	registry RegistryInterface

	// resolverData contains resolved data from the resolver phase
	resolverData map[string]any

	// actionContext manages the __actions namespace
	actionContext *Context

	// progressCallback receives execution events
	progressCallback ProgressCallback

	// maxConcurrency limits parallel action execution (0 = unlimited)
	maxConcurrency int

	// gracePeriod is how long to wait for running actions during cancellation
	gracePeriod time.Duration

	// defaultTimeout is the default timeout for actions without a specific timeout
	defaultTimeout time.Duration

	// workflowResultSchemaMode is the workflow-level default for result schema validation
	workflowResultSchemaMode ResultSchemaMode

	// ioStreams provides terminal IO for providers that support streaming output.
	// When set, providers can write output directly to the terminal in real-time.
	ioStreams *provider.IOStreams
}

// ExecutorOption configures the executor.
type ExecutorOption func(*Executor)

// WithRegistry sets the provider registry for the executor.
func WithRegistry(registry RegistryInterface) ExecutorOption {
	return func(e *Executor) {
		e.registry = registry
	}
}

// WithResolverData sets the resolver data for input resolution.
func WithResolverData(data map[string]any) ExecutorOption {
	return func(e *Executor) {
		e.resolverData = data
	}
}

// WithProgressCallback sets the progress callback for execution events.
func WithProgressCallback(callback ProgressCallback) ExecutorOption {
	return func(e *Executor) {
		e.progressCallback = callback
	}
}

// WithMaxConcurrency limits the number of parallel actions.
// Set to 0 for unlimited concurrency.
func WithMaxConcurrency(n int) ExecutorOption {
	return func(e *Executor) {
		e.maxConcurrency = n
	}
}

// WithGracePeriod sets how long to wait for running actions during cancellation.
func WithGracePeriod(d time.Duration) ExecutorOption {
	return func(e *Executor) {
		e.gracePeriod = d
	}
}

// WithDefaultTimeout sets the default timeout for actions.
func WithDefaultTimeout(d time.Duration) ExecutorOption {
	return func(e *Executor) {
		e.defaultTimeout = d
	}
}

// WithIOStreams sets the terminal IO streams for provider output streaming.
// When set, providers that support streaming (e.g., exec) can write output
// directly to the terminal in real-time. For parallel actions, each action
// gets a prefixed writer to attribute output clearly.
func WithIOStreams(streams *provider.IOStreams) ExecutorOption {
	return func(e *Executor) {
		e.ioStreams = streams
	}
}

// NewExecutor creates a new action executor with the given options.
func NewExecutor(opts ...ExecutorOption) *Executor {
	e := &Executor{
		actionContext:  NewContext(),
		gracePeriod:    settings.DefaultGracePeriod,
		defaultTimeout: settings.DefaultActionTimeout,
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// ConfigInput holds the configuration values for action executor initialization.
// This mirrors config.ActionConfig but avoids circular dependencies.
type ConfigInput struct {
	// DefaultTimeout is the default timeout per action execution
	DefaultTimeout time.Duration
	// GracePeriod is the cancellation grace period
	GracePeriod time.Duration
	// MaxConcurrency is the max concurrent actions (0 = unlimited)
	MaxConcurrency int
}

// OptionsFromAppConfig creates executor options from app configuration.
// CLI flags can override these defaults using the returned executor options.
//
// Example:
//
//	cfg := action.ActionConfigInput{
//	    DefaultTimeout: 5 * time.Minute,
//	    GracePeriod:    30 * time.Second,
//	    MaxConcurrency: 0,
//	}
//	opts := action.OptionsFromAppConfig(cfg)
//	executor := action.NewExecutor(opts...)
func OptionsFromAppConfig(cfg ConfigInput) []ExecutorOption {
	var opts []ExecutorOption

	if cfg.DefaultTimeout > 0 {
		opts = append(opts, WithDefaultTimeout(cfg.DefaultTimeout))
	}
	if cfg.GracePeriod > 0 {
		opts = append(opts, WithGracePeriod(cfg.GracePeriod))
	}
	if cfg.MaxConcurrency > 0 {
		opts = append(opts, WithMaxConcurrency(cfg.MaxConcurrency))
	}

	return opts
}

// ProgressCallback receives execution events for progress reporting.
type ProgressCallback interface {
	// OnActionStart is called when an action begins execution.
	OnActionStart(actionName string)

	// OnActionComplete is called when an action completes successfully.
	OnActionComplete(actionName string, results any)

	// OnActionFailed is called when an action fails.
	OnActionFailed(actionName string, err error)

	// OnActionSkipped is called when an action is skipped.
	OnActionSkipped(actionName, reason string)

	// OnActionTimeout is called when an action times out.
	OnActionTimeout(actionName string, timeout time.Duration)

	// OnActionCancelled is called when an action is cancelled.
	OnActionCancelled(actionName string)

	// OnRetryAttempt is called before each retry attempt.
	OnRetryAttempt(actionName string, attempt, maxAttempts int, err error)

	// OnForEachProgress is called during forEach execution.
	OnForEachProgress(actionName string, completed, total int)

	// OnPhaseStart is called when a new execution phase begins.
	OnPhaseStart(phase int, actionNames []string)

	// OnPhaseComplete is called when an execution phase completes.
	OnPhaseComplete(phase int)

	// OnFinallyStart is called when the finally section begins.
	OnFinallyStart()

	// OnFinallyComplete is called when the finally section completes.
	OnFinallyComplete()
}

// ExecutionResult contains the final execution state.
type ExecutionResult struct {
	// Actions contains results for all executed actions
	Actions map[string]*ActionResult `json:"actions" yaml:"actions" doc:"Results for all actions"`

	// FinalStatus is the overall execution status
	FinalStatus ExecutionStatus `json:"finalStatus" yaml:"finalStatus" doc:"Overall execution status"`

	// StartTime is when execution began
	StartTime time.Time `json:"startTime" yaml:"startTime" doc:"Execution start time"`

	// EndTime is when execution completed
	EndTime time.Time `json:"endTime" yaml:"endTime" doc:"Execution end time"`

	// FailedActions contains names of actions that failed
	FailedActions []string `json:"failedActions,omitempty" yaml:"failedActions,omitempty" doc:"Names of failed actions"`

	// SkippedActions contains names of actions that were skipped
	SkippedActions []string `json:"skippedActions,omitempty" yaml:"skippedActions,omitempty" doc:"Names of skipped actions"`
}

// Duration returns the total execution duration.
func (r *ExecutionResult) Duration() time.Duration {
	return r.EndTime.Sub(r.StartTime)
}

// ExecutionStatus represents the overall execution status.
type ExecutionStatus string

const (
	// ExecutionSucceeded means all actions completed successfully.
	ExecutionSucceeded ExecutionStatus = "succeeded"

	// ExecutionFailed means one or more actions failed.
	ExecutionFailed ExecutionStatus = "failed"

	// ExecutionCancelled means execution was cancelled.
	ExecutionCancelled ExecutionStatus = "cancelled"

	// ExecutionPartialSuccess means some actions succeeded with onError:continue.
	ExecutionPartialSuccess ExecutionStatus = "partial-success"
)

// Execute runs the workflow actions in dependency order.
// It executes main actions first, then always runs finally actions regardless of failures.
func (e *Executor) Execute(ctx context.Context, w *Workflow) (*ExecutionResult, error) {
	if w == nil {
		return nil, fmt.Errorf("workflow cannot be nil")
	}

	// Store workflow-level result schema mode for use during action execution
	e.workflowResultSchemaMode = w.ResultSchemaMode

	// Create a span for the full workflow execution.
	ctx, span := telemetry.Tracer(telemetry.TracerAction).Start(ctx, "action.Execute",
		trace.WithAttributes(attribute.Int("action.count", len(w.Actions))),
	)
	defer span.End()

	result := &ExecutionResult{
		Actions:   make(map[string]*ActionResult),
		StartTime: time.Now(),
	}

	defer func() {
		result.EndTime = time.Now()
	}()

	// Build the action graph
	graph, err := BuildGraph(ctx, w, e.resolverData, nil)
	if err != nil {
		result.FinalStatus = ExecutionFailed
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return result, fmt.Errorf("failed to build action graph: %w", err)
	}

	// Execute main actions
	mainErr := e.executePhases(ctx, graph, graph.ExecutionOrder, false)

	// Always execute finally section, regardless of main errors
	var finallyErr error
	if len(graph.FinallyOrder) > 0 {
		if e.progressCallback != nil {
			e.progressCallback.OnFinallyStart()
		}

		// Create a new context for finally that isn't cancelled
		// (finally should run even if main was cancelled)
		finallyCtx := context.Background()
		if ctx.Err() == nil {
			finallyCtx = ctx
		}

		finallyErr = e.executePhases(finallyCtx, graph, graph.FinallyOrder, true)

		if e.progressCallback != nil {
			e.progressCallback.OnFinallyComplete()
		}
	}

	// Collect results from context
	for name := range graph.Actions {
		if ar, ok := e.actionContext.GetResult(name); ok {
			result.Actions[name] = ar
			switch ar.Status {
			case StatusFailed, StatusTimeout:
				result.FailedActions = append(result.FailedActions, name)
			case StatusSkipped:
				result.SkippedActions = append(result.SkippedActions, name)
			case StatusPending, StatusRunning, StatusSucceeded, StatusCancelled:
				// No action needed for these statuses
			}
		}
	}

	// Determine final status
	result.FinalStatus = e.determineFinalStatus(ctx, mainErr, finallyErr, result)

	if mainErr != nil && result.FinalStatus == ExecutionFailed {
		return result, mainErr
	}

	return result, nil
}

// executePhases executes actions phase by phase with parallel execution within each phase.
func (e *Executor) executePhases(ctx context.Context, graph *Graph, phases [][]string, isFinally bool) error {
	for phaseNum, phase := range phases {
		if ctx.Err() != nil {
			// Mark remaining actions as cancelled
			for _, name := range phase {
				e.actionContext.MarkCancelled(name)
				if e.progressCallback != nil {
					e.progressCallback.OnActionCancelled(name)
				}
			}
			return ctx.Err()
		}

		if e.progressCallback != nil && !isFinally {
			e.progressCallback.OnPhaseStart(phaseNum, phase)
		}

		err := e.executePhase(ctx, graph, phase)

		if e.progressCallback != nil && !isFinally {
			e.progressCallback.OnPhaseComplete(phaseNum)
		}

		if err != nil {
			// Check if we should continue despite errors
			allContinue := true
			for _, name := range phase {
				action := graph.Actions[name]
				if action != nil && action.OnError.OrDefault() != spec.OnErrorContinue {
					allContinue = false
					break
				}
			}

			if !allContinue {
				// Mark all actions in subsequent phases as skipped due to dependency failure
				for _, remainingPhase := range phases[phaseNum+1:] {
					for _, name := range remainingPhase {
						e.actionContext.MarkSkipped(name, SkipReasonDependencyFailed)
						if e.progressCallback != nil {
							e.progressCallback.OnActionSkipped(name, string(SkipReasonDependencyFailed))
						}
					}
				}
				return err
			}
		}
	}

	return nil
}

// executePhase executes all actions in a phase with concurrency control.
func (e *Executor) executePhase(ctx context.Context, graph *Graph, actionNames []string) error {
	if len(actionNames) == 0 {
		return nil
	}

	// Filter out actions that should be skipped due to dependency failures
	actionsToRun := make([]string, 0, len(actionNames))
	for _, name := range actionNames {
		action := graph.Actions[name]
		if action == nil {
			continue
		}

		// Check if any dependencies failed
		shouldSkip := false
		for _, dep := range action.Dependencies {
			if ar, ok := e.actionContext.GetResult(dep); ok {
				if ar.Status == StatusFailed || ar.Status == StatusTimeout {
					shouldSkip = true
					break
				}
			}
		}

		if shouldSkip {
			e.actionContext.MarkSkipped(name, SkipReasonDependencyFailed)
			if e.progressCallback != nil {
				e.progressCallback.OnActionSkipped(name, string(SkipReasonDependencyFailed))
			}
			continue
		}

		actionsToRun = append(actionsToRun, name)
	}

	if len(actionsToRun) == 0 {
		return nil
	}

	// Determine if this is a parallel phase (multiple actions)
	isParallel := len(actionsToRun) > 1

	// Build per-action IOStreams for parallel phases using PrefixedWriter
	actionIOStreams := e.buildActionIOStreams(actionsToRun, isParallel)

	// Determine concurrency limit
	concurrency := len(actionsToRun)
	if e.maxConcurrency > 0 && concurrency > e.maxConcurrency {
		concurrency = e.maxConcurrency
	}

	// Create semaphore for concurrency control
	sem := make(chan struct{}, concurrency)

	// Channel for collecting errors
	errChan := make(chan error, len(actionsToRun))

	var wg sync.WaitGroup

	for _, name := range actionsToRun {
		wg.Add(1)
		go func(actionName string) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				e.actionContext.MarkCancelled(actionName)
				if e.progressCallback != nil {
					e.progressCallback.OnActionCancelled(actionName)
				}
				return
			}

			// Inject per-action IOStreams into context
			actionCtx := ctx
			if streams, ok := actionIOStreams[actionName]; ok {
				actionCtx = provider.WithIOStreams(ctx, streams)
			}

			// Execute the action
			err := e.executeAction(actionCtx, graph, actionName)
			if err != nil {
				errChan <- fmt.Errorf("action %q: %w", actionName, err)
			}
		}(name)
	}

	// Wait for all actions to complete
	wg.Wait()
	close(errChan)

	// Collect errors
	var errs []error
	for err := range errChan {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("phase execution failed: %w", errs[0])
	}

	return nil
}

// buildActionIOStreams creates per-action IO streams. For parallel phases (multiple actions),
// each action gets a PrefixedWriter so output is clearly attributed. For single-action
// phases, the raw IOStreams are used directly for clean, unprefixed output.
func (e *Executor) buildActionIOStreams(actionNames []string, isParallel bool) map[string]*provider.IOStreams {
	streams := make(map[string]*provider.IOStreams, len(actionNames))

	if e.ioStreams == nil {
		return streams
	}

	for _, name := range actionNames {
		if isParallel {
			streams[name] = &provider.IOStreams{
				Out:    terminal.NewPrefixedWriter(e.ioStreams.Out, name),
				ErrOut: terminal.NewPrefixedWriter(e.ioStreams.ErrOut, name),
			}
		} else {
			streams[name] = e.ioStreams
		}
	}

	return streams
}

// executeAction executes a single action with retry, timeout, and error handling.
func (e *Executor) executeAction(ctx context.Context, graph *Graph, actionName string) error {
	action := graph.Actions[actionName]
	if action == nil {
		return fmt.Errorf("action not found in graph")
	}

	// Create a child span for this individual action execution.
	ctx, span := telemetry.Tracer(telemetry.TracerAction).Start(ctx, "action.executeAction",
		trace.WithAttributes(attribute.String("action.name", actionName)),
	)
	defer span.End()

	// Evaluate condition if present
	if action.When != nil {
		additionalVars := e.buildAdditionalVars(graph.AliasMap)
		shouldRun, err := action.When.EvaluateWithAdditionalVars(ctx, e.resolverData, additionalVars)
		if err != nil {
			e.actionContext.MarkFailed(actionName, fmt.Sprintf("condition evaluation failed: %v", err))
			if e.progressCallback != nil {
				e.progressCallback.OnActionFailed(actionName, err)
			}
			return err
		}

		if !shouldRun {
			e.actionContext.MarkSkipped(actionName, SkipReasonCondition)
			if e.progressCallback != nil {
				e.progressCallback.OnActionSkipped(actionName, string(SkipReasonCondition))
			}
			return nil
		}
	}

	// Resolve inputs (including deferred values)
	resolvedInputs, err := e.resolveInputs(ctx, action, graph.AliasMap)
	if err != nil {
		e.actionContext.MarkFailed(actionName, fmt.Sprintf("input resolution failed: %v", err))
		if e.progressCallback != nil {
			e.progressCallback.OnActionFailed(actionName, err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Mark as running
	e.actionContext.MarkRunning(actionName, resolvedInputs)
	if e.progressCallback != nil {
		e.progressCallback.OnActionStart(actionName)
	}

	// Set up timeout
	timeout := e.defaultTimeout
	if action.Timeout != nil {
		timeout = action.Timeout.Duration
	}

	actionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create retry executor
	retryExecutor := NewRetryExecutor(action.Retry)

	// Create the execution function
	execFunc := func(execCtx context.Context) (*provider.Output, error) {
		return e.callProvider(execCtx, action, resolvedInputs)
	}

	// Execute with retry
	var retryCallback RetryCallback
	if e.progressCallback != nil {
		retryCallback = &progressRetryAdapter{callback: e.progressCallback, actionName: actionName}
	}

	output, err := retryExecutor.ExecuteWithRetry(actionCtx, actionName, execFunc, retryCallback)

	// Handle results
	if actionCtx.Err() == context.DeadlineExceeded {
		e.actionContext.MarkTimeout(actionName)
		if e.progressCallback != nil {
			e.progressCallback.OnActionTimeout(actionName, timeout)
		}
		timeoutErr := fmt.Errorf("action timed out after %v", timeout)
		span.RecordError(timeoutErr)
		span.SetStatus(codes.Error, timeoutErr.Error())
		return timeoutErr
	}

	if err != nil {
		e.actionContext.MarkFailed(actionName, err.Error())
		if e.progressCallback != nil {
			e.progressCallback.OnActionFailed(actionName, err)
		}

		// Check if we should continue on error
		if action.OnError.OrDefault() == spec.OnErrorContinue {
			return nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Success
	var results any
	if output != nil {
		results = output.Data
	}

	// Validate result against schema if defined
	if action.ResultSchema != nil {
		// Determine effective mode: action-level overrides workflow-level
		mode := action.ResultSchemaMode
		if mode == "" {
			mode = e.workflowResultSchemaMode
		}
		mode = mode.OrDefault()

		if mode != ResultSchemaModeIgnore {
			if validationErr := ValidateResult(results, action.ResultSchema); validationErr != nil {
				switch mode {
				case ResultSchemaModeError:
					e.actionContext.MarkFailed(actionName, validationErr.Error())
					if e.progressCallback != nil {
						e.progressCallback.OnActionFailed(actionName, validationErr)
					}
					if action.OnError.OrDefault() == spec.OnErrorContinue {
						return nil
					}
					return fmt.Errorf("result schema validation failed: %w", validationErr)
				case ResultSchemaModeWarn:
					logger.FromContext(ctx).V(0).Info("result schema validation warning",
						"action", actionName,
						"error", validationErr.Error())
					// Continue execution - don't fail
				case ResultSchemaModeIgnore:
					// Already handled above, but included for exhaustive switch
				}
			}
		}
	}

	e.actionContext.MarkSucceeded(actionName, results)

	// Track whether the provider streamed output to the terminal
	if output != nil && output.Streamed {
		e.actionContext.MarkStreamed(actionName)
	}

	if e.progressCallback != nil {
		e.progressCallback.OnActionComplete(actionName, results)
	}

	return nil
}

// resolveInputs resolves all inputs including deferred values.
func (e *Executor) resolveInputs(ctx context.Context, action *ExpandedAction, aliasMap map[string]string) (map[string]any, error) {
	// Start with materialized inputs
	inputs := make(map[string]any)
	for k, v := range action.MaterializedInputs {
		inputs[k] = v
	}

	// Resolve deferred inputs using current action results
	if len(action.DeferredInputs) > 0 {
		additionalVars := e.buildAdditionalVars(aliasMap)

		for name, deferredVal := range action.DeferredInputs {
			if deferredVal == nil || !deferredVal.IsDeferred() {
				continue
			}

			resolved, err := deferredVal.Evaluate(ctx, e.resolverData, additionalVars)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve deferred input %q: %w", name, err)
			}
			inputs[name] = resolved
		}
	}

	return inputs, nil
}

// buildAdditionalVars creates the additional variables map for CEL evaluation.
// It includes the __actions namespace and any alias top-level variables.
// Each alias points to the same data as __actions.<actionName> for the aliased action.
func (e *Executor) buildAdditionalVars(aliasMap map[string]string) map[string]any {
	namespace := e.actionContext.GetNamespace()

	additionalVars := map[string]any{
		"__actions": namespace,
	}

	// Add aliases as top-level variables
	for alias, actionName := range aliasMap {
		if actionData, ok := namespace[actionName]; ok {
			additionalVars[alias] = actionData
		}
	}

	return additionalVars
}

// callProvider executes the provider for an action.
func (e *Executor) callProvider(ctx context.Context, action *ExpandedAction, inputs map[string]any) (*provider.Output, error) {
	if e.registry == nil {
		return nil, fmt.Errorf("no provider registry configured")
	}

	prov, ok := e.registry.Get(action.Provider)
	if !ok {
		return nil, fmt.Errorf("provider %q not found", action.Provider)
	}

	// Check for CapabilityAction
	desc := prov.Descriptor()
	hasActionCap := false
	for _, cap := range desc.Capabilities {
		if cap == provider.CapabilityAction {
			hasActionCap = true
			break
		}
	}
	if !hasActionCap {
		return nil, fmt.Errorf("provider %q does not support action capability", action.Provider)
	}

	// Set up execution context with action mode
	execCtx := provider.WithExecutionMode(ctx, provider.CapabilityAction)

	// Pass through IOStreams from context if available (set by executePhase)
	if streams, ok := provider.IOStreamsFromContext(ctx); ok {
		execCtx = provider.WithIOStreams(execCtx, streams)
	}

	// Create executor and run
	providerExecutor := provider.NewExecutor()
	result, err := providerExecutor.Execute(execCtx, prov, inputs)
	if err != nil {
		return nil, err
	}

	return &result.Output, nil
}

// determineFinalStatus determines the overall execution status.
func (e *Executor) determineFinalStatus(ctx context.Context, mainErr, finallyErr error, result *ExecutionResult) ExecutionStatus {
	if ctx.Err() != nil {
		return ExecutionCancelled
	}

	if len(result.FailedActions) == 0 && mainErr == nil && finallyErr == nil {
		return ExecutionSucceeded
	}

	// Check if all failures were actions with onError:continue
	if mainErr == nil && len(result.FailedActions) > 0 {
		return ExecutionPartialSuccess
	}

	return ExecutionFailed
}

// GetContext returns the action context for inspection.
func (e *Executor) GetContext() *Context {
	return e.actionContext
}

// Reset clears the executor state for reuse.
func (e *Executor) Reset() {
	e.actionContext.Reset()
}

// progressRetryAdapter adapts ProgressCallback to RetryCallback.
type progressRetryAdapter struct {
	callback   ProgressCallback
	actionName string
}

func (a *progressRetryAdapter) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	if a.callback != nil {
		a.callback.OnRetryAttempt(actionName, attempt, maxAttempts, err)
	}
}

// NoOpProgressCallback is a progress callback that does nothing.
// Useful for testing or when progress tracking is not needed.
type NoOpProgressCallback struct{}

func (NoOpProgressCallback) OnActionStart(_ string)                     {}
func (NoOpProgressCallback) OnActionComplete(_ string, _ any)           {}
func (NoOpProgressCallback) OnActionFailed(_ string, _ error)           {}
func (NoOpProgressCallback) OnActionSkipped(_, _ string)                {}
func (NoOpProgressCallback) OnActionTimeout(_ string, _ time.Duration)  {}
func (NoOpProgressCallback) OnActionCancelled(_ string)                 {}
func (NoOpProgressCallback) OnRetryAttempt(_ string, _, _ int, _ error) {}
func (NoOpProgressCallback) OnForEachProgress(_ string, _, _ int)       {}
func (NoOpProgressCallback) OnPhaseStart(_ int, _ []string)             {}
func (NoOpProgressCallback) OnPhaseComplete(_ int)                      {}
func (NoOpProgressCallback) OnFinallyStart()                            {}
func (NoOpProgressCallback) OnFinallyComplete()                         {}
