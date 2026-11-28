// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/call"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/fingerprint"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
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

	// executionData contains resolver execution metadata (phases, timing, providers).
	// When set, it is injected as __execution into action when conditions and inputs,
	// allowing actions to inspect how resolvers ran (status, duration, phase, etc.).
	executionData map[string]any

	// actionContext manages the __actions namespace
	actionContext *Context

	// progressCallback receives execution events
	progressCallback ProgressCallback

	// activeProgress is the mutex-synchronized wrapper around progressCallback
	// initialized at the start of Execute and shared with nested executors.
	activeProgress ProgressCallback

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

	// activeIOStreams holds the mutex-synchronized wrapper around ioStreams
	// initialized at the start of Execute and cleared on Reset.
	activeIOStreams *provider.IOStreams

	// cwd is the original working directory captured at executor creation time.
	// Exposed as __cwd in action expressions for referencing the original CWD.
	cwd string

	// fingerprintChecker evaluates source/generates file hashes for up-to-date checks.
	// When nil, fingerprint-based skipping is disabled.
	fingerprintChecker *fingerprint.Checker

	// noCache disables fingerprint-based up-to-date checks when true.
	noCache bool

	// calls holds reusable call definitions invoked by actions via call + args.
	calls map[string]*spec.Call

	// dedupMemo is a per-run memo for opt-in call de-duplication.
	dedupMemo *call.Memo

	// templateFuncBinder holds the solution's author-defined template helper
	// functions (spec.functions), injected into provider context so templates
	// rendered by actions can invoke them.
	templateFuncBinder provider.TemplateFuncBinder

	// sensitiveResolvers lists resolver names marked sensitive: true or backed
	// by credentials so ActionResult.Inputs and coercion errors mask them
	// regardless of value length or coerced scalar type.
	sensitiveResolvers map[string]bool
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

// WithExecutionData sets the resolver execution metadata for action contexts.
// When set, the data is injected as __execution into action when conditions and
// provider inputs, allowing actions to read resolver status, duration, and phase.
func WithExecutionData(data map[string]any) ExecutorOption {
	return func(e *Executor) {
		e.executionData = data
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
// When set, providers that support streaming (e.g., exec, message) can write
// output directly to the terminal in real-time. All actions share the same
// raw streams; use the message provider for structured terminal output.
func WithIOStreams(streams *provider.IOStreams) ExecutorOption {
	return func(e *Executor) {
		e.ioStreams = streams
	}
}

// WithCwd sets the original working directory for the executor.
// This is exposed as __cwd in action expressions.
func WithCwd(cwd string) ExecutorOption {
	return func(e *Executor) {
		e.cwd = cwd
	}
}

// WithFingerprintChecker sets the fingerprint checker for up-to-date detection.
// When set, actions with sources/generates fields are checked for staleness.
func WithFingerprintChecker(checker *fingerprint.Checker) ExecutorOption {
	return func(e *Executor) {
		e.fingerprintChecker = checker
	}
}

// WithNoCache disables fingerprint-based up-to-date checks.
// All actions will execute regardless of fingerprint state.
func WithNoCache(noCache bool) ExecutorOption {
	return func(e *Executor) {
		e.noCache = noCache
	}
}

// WithCalls sets the reusable call definitions available to actions that invoke
// providers via call + args.
func WithCalls(calls map[string]*spec.Call) ExecutorOption {
	return func(e *Executor) {
		e.calls = calls
	}
}

// WithTemplateFuncBinder sets the solution's author-defined template helper
// functions (spec.functions) so templates rendered by actions can invoke them
// as {{ name arg... }}. A nil binder is a no-op.
func WithTemplateFuncBinder(binder provider.TemplateFuncBinder) ExecutorOption {
	return func(e *Executor) {
		e.templateFuncBinder = binder
	}
}

// NewExecutor creates a new action executor with the given options.
// It captures the current working directory at creation time for use as __cwd
// in action expressions.
func NewExecutor(opts ...ExecutorOption) *Executor {
	cwd, _ := os.Getwd()

	e := &Executor{
		actionContext:  NewContext(),
		gracePeriod:    settings.DefaultGracePeriod,
		defaultTimeout: settings.DefaultActionTimeout,
		cwd:            cwd,
		dedupMemo:      call.NewMemo(),
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
	DefaultTimeout time.Duration `json:"defaultTimeout" yaml:"defaultTimeout" doc:"Default timeout per action execution"`
	// GracePeriod is the cancellation grace period
	GracePeriod time.Duration `json:"gracePeriod" yaml:"gracePeriod" doc:"Cancellation grace period"`
	// MaxConcurrency is the max concurrent actions (0 = unlimited)
	MaxConcurrency int `json:"maxConcurrency" yaml:"maxConcurrency" doc:"Maximum concurrent actions (0 = unlimited)" maximum:"1000" example:"5"`
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
// It composes ActionObserver, PhaseObserver, and RetryObserver for full
// lifecycle coverage. New code should prefer accepting the narrower
// observer interface(s) it actually needs.
type ProgressCallback interface {
	Observer
	PhaseObserver
	RetryObserver
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

	// Reset the opt-in de-duplication memo at the start of each run.
	e.dedupMemo = call.NewMemo()

	// Wrap progressCallback in a single mutex-serialized adapter so parent and
	// nested sub-solution executors never race on unsynchronized observers.
	e.activeProgress = SynchronizeProgressCallback(e.progressCallback)

	// Wrap ioStreams writers in mutex-synchronized adapters so concurrent
	// actions streaming or flushing buffered output never race.
	e.activeIOStreams = synchronizeIOStreams(e.ioStreams)

	inheritedCfg := InheritedExecutorConfig{
		DefaultTimeout:   e.defaultTimeout,
		GracePeriod:      e.gracePeriod,
		MaxConcurrency:   e.maxConcurrency,
		ProgressCallback: e.activeProgress,
		IOStreams:        e.activeIOStreams,
		Cwd:              e.cwd,
	}
	ctx = WithInheritedExecutorConfig(ctx, inheritedCfg)

	// Ensure a run-scoped secret redaction tracker is present in ctx.
	runTracker := redact.FromContext(ctx)
	if runTracker == nil {
		runTracker = redact.NewTracker()
		ctx = redact.WithTracker(ctx, runTracker)
	}

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
		err = runTracker.RedactError(err)
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
		if cb := e.progress(); cb != nil {
			cb.OnFinallyStart()
		}

		// Create a context for finally that ignores parent cancellation while
		// preserving all runtime values (loader, registry, ancestor stack, redaction tracker).
		finallyCtx := ctx
		if ctx.Err() != nil {
			finallyCtx = context.WithoutCancel(ctx)
		}

		finallyErr = e.executePhases(finallyCtx, graph, graph.FinallyOrder, true)

		if cb := e.progress(); cb != nil {
			cb.OnFinallyComplete()
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

	if result.FinalStatus == ExecutionFailed {
		if mainErr != nil {
			return result, mainErr
		}
		if finallyErr != nil {
			return result, finallyErr
		}
	}

	return result, nil
}

// executePhases executes actions phase by phase with parallel execution within each phase.
func (e *Executor) executePhases(ctx context.Context, graph *Graph, phases [][]string, isFinally bool) error {
	cb := e.progress()
	for phaseNum, phase := range phases {
		if ctx.Err() != nil {
			// Mark remaining actions as cancelled
			for _, name := range phase {
				e.actionContext.MarkCancelled(name)
				if cb != nil {
					cb.OnActionCancelled(name)
				}
			}
			return ctx.Err()
		}

		if cb != nil && !isFinally {
			cb.OnPhaseStart(phaseNum, phase)
		}

		err := e.executePhase(ctx, graph, phase)

		if cb != nil && !isFinally {
			cb.OnPhaseComplete(phaseNum)
		}

		if err != nil {
			// Check if we should continue despite errors
			allContinue := true
			for _, name := range phase {
				action := graph.Actions[name]
				if action == nil {
					continue
				}
				var actionErr error
				attempts := 1
				if ar, ok := e.actionContext.GetResult(name); ok && ar.Error != "" {
					actionErr = errors.New(ar.Error)
					if ar.Attempts > 0 {
						attempts = ar.Attempts
					}
				}
				// Only consult continueOnError for actions that actually failed.
				// A successful action must not gate workflow continuation, and
				// evaluating a continueOnError expression that references __error
				// would fail with an undefined-variable error.
				if actionErr == nil {
					continue
				}
				// Reuse the action's recorded attempt count (and its configured
				// maxAttempts) so this re-check matches the decision executeAction
				// already made, including expressions that read __error.attempt.
				maxAttempts := NewRetryExecutor(action.Retry).MaxAttempts()
				cont, decideErr := e.actionShouldContinue(ctx, action, actionErr, attempts, maxAttempts)
				if decideErr != nil || !cont {
					allContinue = false
					break
				}
			}

			if !allContinue {
				// Mark all actions in subsequent phases as skipped due to dependency failure
				for _, remainingPhase := range phases[phaseNum+1:] {
					for _, name := range remainingPhase {
						e.actionContext.MarkSkipped(name, SkipReasonDependencyFailed)
						if cb != nil {
							cb.OnActionSkipped(name, string(SkipReasonDependencyFailed))
						}
					}
				}
				return err
			}
		}
	}

	return nil
}

// actionShouldContinue reports whether a failed action should allow the workflow
// to continue. The effective continue-on-error policy is resolved by
// effectiveOnErrorPolicy: for forEach iterations the loop-level
// forEach.continueOnError/onError take precedence; otherwise the action-level
// fields apply. When the policy is a CEL expression it is evaluated with the
// structured error bound as __error (carrying the real attempt/maxAttempts) and,
// for forEach iterations, the iteration variables (__item/__index plus any
// configured aliases). A truthy result continues the workflow.
func (e *Executor) actionShouldContinue(ctx context.Context, action *ExpandedAction, actionErr error, attempt, maxAttempts int) (bool, error) {
	cond, fallback := effectiveOnErrorPolicy(action)
	if cond != nil && cond.Expr != nil {
		additionalVars := e.continueOnErrorVars(action, actionErr, attempt, maxAttempts)
		result, evalErr := celexp.EvaluateExpression(ctx, string(*cond.Expr), e.resolverData, additionalVars)
		if evalErr != nil {
			return false, fmt.Errorf("continueOnError condition evaluation failed: %w", evalErr)
		}
		cont, ok := result.(bool)
		if !ok {
			return false, fmt.Errorf("continueOnError condition must evaluate to boolean, got %T", result)
		}
		return cont, nil
	}
	return fallback.OrDefault() == spec.OnErrorContinue, nil
}

// effectiveOnErrorPolicy resolves the continue-on-error condition and deprecated
// onError fallback that govern a failed action. For a forEach iteration the
// loop-level policy wins when the forEach clause sets either continueOnError or
// the deprecated onError, because the user attached the error policy to the
// loop; it falls back to the action-level fields only when the loop specifies
// neither. For a non-forEach action the action-level fields always apply.
func effectiveOnErrorPolicy(action *ExpandedAction) (*spec.Condition, spec.OnErrorBehavior) {
	if action.IsForEachIteration() && action.ForEach != nil {
		fe := action.ForEach
		//nolint:staticcheck // deprecated onError still supported for back-compat
		if fe.ContinueOnError != nil || fe.OnError != "" {
			//nolint:staticcheck // deprecated onError still supported for back-compat
			return fe.ContinueOnError, fe.OnError
		}
	}
	//nolint:staticcheck // deprecated onError still supported for back-compat
	return action.ContinueOnError, action.OnError
}

// continueOnErrorVars builds the additional CEL variables for a continueOnError
// expression: the structured __error context (with the real attempt count) and,
// for forEach iterations, the iteration variables (__item/__index and any
// configured aliases) so the expression can branch on the failing item.
func (e *Executor) continueOnErrorVars(action *ExpandedAction, actionErr error, attempt, maxAttempts int) map[string]any {
	vars := make(map[string]any, 4)
	if actionErr != nil {
		errCtx := NewErrorContext(actionErr, attempt, maxAttempts)
		vars[celexp.VarError] = errCtx.ToMap()
	}
	if action.IsForEachIteration() && action.ForEachMetadata != nil {
		meta := action.ForEachMetadata
		vars[celexp.VarItem] = meta.Item
		vars[celexp.VarIndex] = meta.Index
		if action.ForEach != nil {
			if action.ForEach.Item != "" {
				vars[action.ForEach.Item] = meta.Item
			}
			if action.ForEach.Index != "" {
				vars[action.ForEach.Index] = meta.Index
			}
		}
	}
	return vars
}

// executePhase executes all actions in a phase with concurrency control.
func (e *Executor) executePhase(ctx context.Context, graph *Graph, actionNames []string) error {
	if len(actionNames) == 0 {
		return nil
	}

	cb := e.progress()

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
			if cb != nil {
				cb.OnActionSkipped(name, string(SkipReasonDependencyFailed))
			}
			continue
		}

		actionsToRun = append(actionsToRun, name)
	}

	if len(actionsToRun) == 0 {
		return nil
	}

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
				if cb != nil {
					cb.OnActionCancelled(actionName)
				}
				return
			}

			// Inject IOStreams into context for streaming providers
			actionCtx := ctx
			if streams := e.streams(); streams != nil {
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

// executeAction executes a single action with retry, timeout, and error handling.
func (e *Executor) executeAction(ctx context.Context, graph *Graph, actionName string) error {
	action := graph.Actions[actionName]
	if action == nil {
		return fmt.Errorf("action not found in graph")
	}

	cb := e.progress()

	// Create a child span for this individual action execution.
	ctx, span := telemetry.Tracer(telemetry.TracerAction).Start(ctx, "action.executeAction",
		trace.WithAttributes(attribute.String("action.name", actionName)),
	)
	defer span.End()

	actionTracker := redact.FromContext(ctx)
	if actionTracker == nil {
		actionTracker = redact.NewTracker()
		ctx = redact.WithTracker(ctx, actionTracker)
	}

	// Evaluate condition if present
	if action.WhenSkipped {
		e.actionContext.MarkSkipped(actionName, SkipReasonCondition)
		if cb != nil {
			cb.OnActionSkipped(actionName, string(SkipReasonCondition))
		}
		return nil
	}
	if action.When != nil {
		additionalVars := e.buildAdditionalVars(graph.AliasMap)
		shouldRun, err := action.When.EvaluateWithAdditionalVars(ctx, e.resolverData, additionalVars)
		if err != nil {
			err = actionTracker.RedactError(err)
			e.actionContext.MarkFailed(actionName, fmt.Sprintf("condition evaluation failed: %v", err))
			if cb != nil {
				cb.OnActionFailed(actionName, err)
			}
			return err
		}

		if !shouldRun {
			e.actionContext.MarkSkipped(actionName, SkipReasonCondition)
			if cb != nil {
				cb.OnActionSkipped(actionName, string(SkipReasonCondition))
			}
			return nil
		}
	}

	// Phase 1: File-based fingerprint check (cheap, before input resolution).
	// If files changed, we know we need to re-execute -- skip Phase 2.
	var filesFresh bool
	if e.fingerprintChecker != nil && !e.noCache && len(action.Sources) > 0 {
		fpResult, fpErr := e.fingerprintChecker.CheckFiles(ctx, actionName, action.Sources, action.Generates, e.cwd)
		if fpErr != nil {
			// A malformed, absolute, or path-traversing glob is a user/security
			// error, not a transient miss -- fail the action hard rather than
			// silently executing it (fail-open would defeat the pattern
			// validation and its containment checks).
			if errors.Is(fpErr, fingerprint.ErrPatternInvalid) {
				fpErr = actionTracker.RedactError(fpErr)
				e.actionContext.MarkFailed(actionName, fmt.Sprintf("invalid sources/generates pattern: %v", fpErr))
				if cb != nil {
					cb.OnActionFailed(actionName, fpErr)
				}
				span.RecordError(fpErr)
				span.SetStatus(codes.Error, fpErr.Error())
				return fpErr
			}
			// Other errors are non-fatal: log and continue execution (fail-open).
			logger.FromContext(ctx).V(0).Info("fingerprint file check failed, executing action",
				"action", actionName, "error", actionTracker.RedactError(fpErr).Error())
		} else {
			e.warnEmptySources(ctx, actionName, fpResult.SourcesEmptyPatterns, fpResult.SourcesAllEmpty)
			if !fpResult.Stale {
				filesFresh = true
			}
		}
	}

	// Resolve inputs (including deferred values). Call-based actions expand the
	// referenced definition instead of resolving direct provider inputs.
	var (
		resolvedInputs map[string]any
		providerName   string
		callPlan       *actionCallPlan
		err            error
	)

	// Phase 2: Input-based fingerprint check (only when files were fresh).
	// Compares fingerprint-normalized inputs hash against stored state so rotating
	// credential bytes stay cache-hit stable while scrt:/tkn: reference or option
	// changes invalidate the cache.
	// Skipped when fingerprint.scope is "files" (user opted out of input hashing).
	var fingerprintInputs map[string]any
	needFingerprintInputs := e.fingerprintChecker != nil && len(action.Sources) > 0 && fingerprintScope(action.Action) != FingerprintScopeFiles
	if filesFresh {
		scope := fingerprintScope(action.Action)
		if scope == FingerprintScopeFiles {
			// scope: files — skip input check, files alone determine freshness
			e.actionContext.MarkSkipped(actionName, SkipReasonUpToDate)
			if cb != nil {
				cb.OnActionSkipped(actionName, string(SkipReasonUpToDate))
			}
			return nil
		}

		var fpInputsErr error
		fingerprintInputs, fpInputsErr = e.resolveFingerprintInputs(ctx, action, graph.AliasMap, actionTracker)
		if fpInputsErr == nil {
			fpResult, fpErr := e.fingerprintChecker.CheckInputs(ctx, actionName, fingerprintInputs)
			if fpErr != nil {
				logger.FromContext(ctx).V(0).Info("fingerprint inputs check failed, executing action",
					"action", actionName, "error", fpErr.Error())
			} else if !fpResult.Stale {
				e.actionContext.MarkSkipped(actionName, SkipReasonUpToDate)
				if cb != nil {
					cb.OnActionSkipped(actionName, string(SkipReasonUpToDate))
				}
				return nil
			}
		}
	}

	if action.HasCall() {
		callPlan, err = e.expandActionCall(ctx, action, graph.AliasMap)
		if err == nil {
			resolvedInputs = callPlan.Inputs
			providerName = callPlan.ProviderName
		}
	} else {
		resolvedInputs, err = e.resolveInputs(ctx, action, graph.AliasMap)
		providerName = action.Provider
	}
	if err != nil {
		err = actionTracker.RedactError(err)
		e.actionContext.MarkFailed(actionName, fmt.Sprintf("input resolution failed: %v", err))
		if cb != nil {
			cb.OnActionFailed(actionName, err)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	redactedInputs, secretProps := e.maskResolvedActionInputs(action, resolvedInputs, actionTracker)
	if needFingerprintInputs && fingerprintInputs == nil {
		if fpIn, fpErr := e.resolveFingerprintInputs(ctx, action, graph.AliasMap, actionTracker); fpErr == nil {
			fingerprintInputs = fpIn
		} else {
			fingerprintInputs = redactedInputs
		}
	}

	// Mark as running with redacted inputs so plaintext credentials never leak
	// into __actions.<name>.inputs or API/CLI execution envelopes.
	e.actionContext.MarkRunning(actionName, redactedInputs)
	if cb != nil {
		cb.OnActionStart(actionName, action.Description)
	}

	// Set up timeout
	timeout := e.defaultTimeout
	if action.Timeout != nil {
		timeout = action.Timeout.Duration
	}

	actionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pollingUntil := action.Retry != nil && action.Retry.Until != nil

	// Create the execution function
	callFn := func(execCtx context.Context) (*provider.Output, provider.CapturedStreamLogs, error) {
		// Install the resolver data as the provider resolver context so
		// providers that render templates or expressions internally (e.g. the
		// file provider's write-tree outputPath, or cel) can read resolver
		// values. Call-based actions use the enriched resolver data (carrying
		// the args namespace) so providers can read _.args.* like resolver
		// calls; regular actions use the base resolver data.
		//
		// Clone the map before installing it: actions run concurrently and
		// some providers mutate the resolver context they receive (e.g. the
		// cel provider deletes special keys), so sharing the map by reference
		// would cause data races and cross-action interference.
		switch {
		case callPlan != nil && callPlan.ResolverData != nil:
			execCtx = provider.WithResolverContext(execCtx, maps.Clone(callPlan.ResolverData))
		case e.resolverData != nil:
			execCtx = provider.WithResolverContext(execCtx, maps.Clone(e.resolverData))
		}
		// Expose the author-defined template functions to providers that render
		// Go templates (e.g. go-template, file) during action execution.
		if e.templateFuncBinder != nil {
			execCtx = provider.WithTemplateFuncBinder(execCtx, e.templateFuncBinder)
		}
		if len(secretProps) > 0 {
			execCtx = provider.WithSecretProperties(execCtx, secretProps)
		}
		return e.callProvider(execCtx, providerName, resolvedInputs)
	}

	// Opt-in de-duplication: identical call args within a run run the provider once.
	// Actions with retry.until poll live external state across attempts, so they
	// always execute the provider directly rather than replaying a memoized first poll.
	if callPlan != nil && callPlan.Dedup && e.dedupMemo != nil && !pollingUntil {
		inner := callFn
		callFn = func(execCtx context.Context) (*provider.Output, provider.CapturedStreamLogs, error) {
			val, dedupErr := e.dedupMemo.Do(callPlan.DedupKey, func() (any, error) {
				out, logs, callErr := inner(execCtx)
				return dedupProviderResult{output: out, logs: logs}, callErr
			})
			var res dedupProviderResult
			if val != nil {
				var ok bool
				res, ok = val.(dedupProviderResult)
				if !ok && dedupErr == nil {
					return nil, provider.CapturedStreamLogs{}, fmt.Errorf("action %q: dedup memo returned unexpected type %T (want dedupProviderResult)", actionName, val)
				}
			}
			return res.output, res.logs, dedupErr
		}
	}

	var (
		lastCapturedLogs     provider.CapturedStreamLogs
		currentCandidateLogs provider.CapturedStreamLogs
		lastAttemptCapture   *attemptStreamCapture
		lastOrigStreamed     bool
	)
	execFunc := func(execCtx context.Context) (*provider.Output, error) {
		var capture *attemptStreamCapture
		if pollingUntil {
			capture = &attemptStreamCapture{}
			execCtx = redact.WithSuppressGitHubActionsMasks(execCtx)
			execCtx = provider.WithIOStreams(execCtx, capture.providerStreams())
			inheritedCfg, ok := InheritedExecutorConfigFromContext(execCtx)
			if !ok {
				inheritedCfg = InheritedExecutorConfig{
					DefaultTimeout:   e.defaultTimeout,
					GracePeriod:      e.gracePeriod,
					MaxConcurrency:   e.maxConcurrency,
					ProgressCallback: e.activeProgress,
					Cwd:              e.cwd,
				}
			}
			inheritedCfg.IOStreams = capture.childStreams()
			execCtx = WithInheritedExecutorConfig(execCtx, inheritedCfg)
		}

		out, logs, callErr := callFn(execCtx)
		origStreamed := out != nil && out.Streamed
		if pollingUntil && origStreamed {
			cp := *out
			cp.Streamed = false
			out = &cp
		}
		provLogs := capture.providerLogs(actionTracker)
		candidateLogs := mergeCapturedStreamLogs(logs, provLogs)
		resultLogs := logs
		if shouldMergeAttemptLogsIntoResults(out, e.streams() != nil, providerName) {
			resultLogs = candidateLogs
		}
		if out == nil && (!resultLogs.Empty() || (capture != nil && !capture.empty())) {
			out = &provider.Output{}
		}
		currentCandidateLogs = candidateLogs
		lastAttemptCapture = capture
		lastOrigStreamed = origStreamed
		if callErr == nil || out != nil {
			lastCapturedLogs = resultLogs
		}
		return out, callErr
	}

	// Create retry executor; build action CEL scope only when retryIf or until
	// is configured so ordinary actions avoid unnecessary namespace copies.
	retryExecutor := NewRetryExecutor(action.Retry)
	if action.Retry != nil && (action.Retry.RetryIf != nil || action.Retry.Until != nil) {
		retryExecutor.
			WithRootData(e.resolverData).
			WithAdditionalVars(e.buildActionEvalVars(action, graph.AliasMap))
	}
	if pollingUntil {
		retryExecutor.WithCandidateExtractor(func(out *provider.Output) any {
			var data any
			if out != nil && out.Data != nil {
				data = actionTracker.RedactAny(out.Data)
			}
			return currentCandidateLogs.MergeIntoData(data)
		})
	}

	// Execute with retry
	var retryCallback RetryCallback
	if cb != nil {
		retryCallback = &progressRetryAdapter{callback: cb, actionName: actionName, tracker: actionTracker}
	}

	output, attempts, err := retryExecutor.ExecuteWithRetry(actionCtx, actionName, execFunc, retryCallback)
	err = actionTracker.RedactError(err)
	maxAttempts := retryExecutor.MaxAttempts()
	e.actionContext.RecordAttempts(actionName, attempts)

	// Handle results
	if actionCtx.Err() == context.DeadlineExceeded {
		failedResults, streamed := e.flushFinalActionOutput(ctx, output, lastCapturedLogs, lastAttemptCapture, lastOrigStreamed, actionTracker)
		e.actionContext.RecordOutput(actionName, failedResults, streamed)
		e.actionContext.MarkTimeout(actionName)
		if cb != nil {
			cb.OnActionTimeout(actionName, timeout)
		}
		timeoutErr := fmt.Errorf("action timed out after %v", timeout)
		span.RecordError(timeoutErr)
		span.SetStatus(codes.Error, timeoutErr.Error())
		return timeoutErr
	}

	if err != nil {
		failedResults, streamed := e.flushFinalActionOutput(ctx, output, lastCapturedLogs, lastAttemptCapture, lastOrigStreamed, actionTracker)
		e.actionContext.RecordOutput(actionName, failedResults, streamed)

		// Evaluate continueOnError before recording/reporting the failure so any
		// expression evaluation error (decideErr) is included in ActionResult.Error
		// and observer diagnostics rather than swallowed.
		cont, decideErr := e.actionShouldContinue(ctx, action, err, attempts, maxAttempts)
		if decideErr != nil {
			err = errors.Join(err, actionTracker.RedactError(decideErr))
			cont = false
		}
		e.actionContext.MarkFailed(actionName, err.Error())
		if cb != nil {
			cb.OnActionFailed(actionName, err)
		}
		if cont {
			return nil
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	// Success (read output without mutating it in place, since dedupMemo may
	// share the *provider.Output pointer across concurrent actions in a phase).
	var rawResults any
	if output != nil {
		rawResults = output.Data
	}

	// Validate original provider output against schema before redacting or
	// merging captured stream chunks so credential-bearing fields and non-TTY
	// JSON/YAML log capture never change resultSchema validation outcomes.
	if action.ResultSchema != nil {
		mode := action.ResultSchemaMode
		if mode == "" {
			mode = e.workflowResultSchemaMode
		}
		mode = mode.OrDefault()

		if mode != ResultSchemaModeIgnore {
			if validationErr := ValidateResult(rawResults, action.ResultSchema); validationErr != nil {
				validationErr = actionTracker.RedactError(validationErr)
				switch mode {
				case ResultSchemaModeError:
					failedResults, streamed := e.flushFinalActionOutput(ctx, output, lastCapturedLogs, lastAttemptCapture, lastOrigStreamed, actionTracker)
					e.actionContext.RecordOutput(actionName, failedResults, streamed)
					cont, decideErr := e.actionShouldContinue(ctx, action, validationErr, attempts, maxAttempts)
					reportErr := validationErr
					retErr := fmt.Errorf("result schema validation failed: %w", validationErr)
					if decideErr != nil {
						decideErr = actionTracker.RedactError(decideErr)
						reportErr = errors.Join(validationErr, decideErr)
						retErr = errors.Join(retErr, decideErr)
						cont = false
					}
					e.actionContext.MarkFailed(actionName, reportErr.Error())
					if cb != nil {
						cb.OnActionFailed(actionName, reportErr)
					}
					if cont {
						return nil
					}
					span.RecordError(retErr)
					span.SetStatus(codes.Error, retErr.Error())
					return retErr
				case ResultSchemaModeWarn:
					logger.FromContext(ctx).V(0).Info("result schema validation warning",
						"action", actionName,
						"error", validationErr.Error())
				case ResultSchemaModeIgnore:
				}
			}
		}
	}

	results, streamed := e.flushFinalActionOutput(ctx, output, lastCapturedLogs, lastAttemptCapture, lastOrigStreamed, actionTracker)

	e.actionContext.MarkSucceeded(actionName, results)

	// Record fingerprint after successful execution using symbolic credential
	// identity markers (no live credential bytes) so rotating JWT claims stay
	// cache-hit stable while scrt:/tkn: reference or option edits invalidate.
	if e.fingerprintChecker != nil && len(action.Sources) > 0 {
		scope := fingerprintScope(action.Action)
		var recordInputs map[string]any
		if scope != FingerprintScopeFiles {
			recordInputs = fingerprintInputs
		}
		if recordErr := e.fingerprintChecker.Record(ctx, actionName, action.Sources, action.Generates, e.cwd, recordInputs); recordErr != nil {
			logger.FromContext(ctx).V(0).Info("fingerprint record failed",
				"action", actionName, "error", recordErr.Error())
		}
	}

	// Track whether the provider streamed or flushed output to the terminal
	if streamed {
		e.actionContext.MarkStreamed(actionName)
	}

	if cb != nil {
		cb.OnActionComplete(actionName, results)
	}

	return nil
}

type dedupProviderResult struct {
	output *provider.Output
	logs   provider.CapturedStreamLogs
}

type streamChunkRecord struct {
	stderr bool
	data   string
}

// attemptStreamCapture buffers direct provider IOStreams writes and inherited
// child-executor IOStreams writes for a single retry.until attempt so pending
// polls do not write prematurely to live terminal streams or process stdio.
type attemptStreamCapture struct {
	mu         sync.Mutex
	chunks     []streamChunkRecord
	provStdout strings.Builder
	provStderr strings.Builder
}

type attemptStreamWriter struct {
	capture *attemptStreamCapture
	stderr  bool
	direct  bool
}

func (w *attemptStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s := string(p)
	w.capture.mu.Lock()
	w.capture.chunks = append(w.capture.chunks, streamChunkRecord{stderr: w.stderr, data: s})
	if w.direct {
		if w.stderr {
			w.capture.provStderr.WriteString(s)
		} else {
			w.capture.provStdout.WriteString(s)
		}
	}
	w.capture.mu.Unlock()
	return len(p), nil
}

func (c *attemptStreamCapture) providerStreams() *provider.IOStreams {
	return &provider.IOStreams{
		Out:    redact.NewStreamWriter(&attemptStreamWriter{capture: c, direct: true}, nil),
		ErrOut: redact.NewStreamWriter(&attemptStreamWriter{capture: c, stderr: true, direct: true}, nil),
	}
}

func (c *attemptStreamCapture) childStreams() *provider.IOStreams {
	return &provider.IOStreams{
		Out:    redact.NewStreamWriter(&attemptStreamWriter{capture: c}, nil),
		ErrOut: redact.NewStreamWriter(&attemptStreamWriter{capture: c, stderr: true}, nil),
	}
}

func (c *attemptStreamCapture) empty() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.chunks) == 0
}

func (c *attemptStreamCapture) providerLogs(tracker *redact.Tracker) provider.CapturedStreamLogs {
	if c == nil {
		return provider.CapturedStreamLogs{}
	}
	c.mu.Lock()
	stdout := c.provStdout.String()
	stderr := c.provStderr.String()
	c.mu.Unlock()
	if tracker != nil {
		stdout = tracker.RedactString(stdout)
		stderr = tracker.RedactString(stderr)
	}
	return provider.CapturedStreamLogs{
		Stdout: stdout,
		Stderr: stderr,
	}
}

func (c *attemptStreamCapture) snapshotChunks() []streamChunkRecord {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.chunks) == 0 {
		return nil
	}
	chunks := make([]streamChunkRecord, len(c.chunks))
	copy(chunks, c.chunks)
	return chunks
}

func mergeCapturedStreamLogs(primary, fallback provider.CapturedStreamLogs) provider.CapturedStreamLogs {
	out := primary.Stdout
	if out == "" {
		out = fallback.Stdout
	}
	errOut := primary.Stderr
	if errOut == "" {
		errOut = fallback.Stderr
	}
	return provider.CapturedStreamLogs{
		Stdout: out,
		Stderr: errOut,
	}
}

func shouldMergeAttemptLogsIntoResults(out *provider.Output, hasLiveStreams bool, providerName string) bool {
	if providerName == "message" || providerName == "debug" {
		return false
	}
	if !hasLiveStreams {
		return true
	}
	if out == nil || out.Data == nil {
		return true
	}
	if m, ok := out.Data.(map[string]any); ok {
		_, hasOut := m["stdout"]
		_, hasErr := m["stderr"]
		return hasOut || hasErr
	}
	return false
}

func (e *Executor) flushFinalActionOutput(
	ctx context.Context,
	output *provider.Output,
	capturedLogs provider.CapturedStreamLogs,
	attemptCapture *attemptStreamCapture,
	origStreamed bool,
	actionTracker *redact.Tracker,
) (any, bool) {
	if attemptCapture == nil || attemptCapture.empty() {
		return e.flushBufferedActionOutput(ctx, output, capturedLogs, actionTracker)
	}
	if cliParams, ok := settings.FromContext(ctx); ok && cliParams != nil && cliParams.IsQuiet {
		return e.flushBufferedActionOutput(ctx, output, capturedLogs, actionTracker)
	}

	streams := e.streams()
	if streams == nil {
		return e.flushBufferedActionOutput(ctx, output, capturedLogs, actionTracker)
	}

	chunks := attemptCapture.snapshotChunks()
	if len(chunks) == 0 {
		return e.flushBufferedActionOutput(ctx, output, capturedLogs, actionTracker)
	}

	var explicitStdout, explicitStderr string
	if !origStreamed && output != nil {
		if rawMap, ok := output.Data.(map[string]any); ok {
			explicitStdout, _ = rawMap["stdout"].(string)
			explicitStderr, _ = rawMap["stderr"].(string)
		}
	}

	// Share one boundary-safe redacting writer per channel across captured chunks
	// and trailing buffered stdout/stderr, flushing only after both sources write.
	flushCtx, flushRedacting := provider.WrapDirectiveAlignedRedactingIOStreams(
		provider.WithIOStreams(ctx, streams),
		actionTracker,
		nil,
	)
	wrappedStreams, _ := provider.IOStreamsFromContext(flushCtx)
	if wrappedStreams == nil {
		wrappedStreams = streams
	}

	var flushedOut, flushedErr bool
	for _, ch := range chunks {
		if ch.data == "" {
			continue
		}
		if ch.stderr {
			if streams.ErrOut != nil && wrappedStreams.ErrOut != nil {
				_, _ = io.WriteString(wrappedStreams.ErrOut, ch.data)
				flushedErr = true
			}
			continue
		}
		if streams.Out != nil && wrappedStreams.Out != nil {
			_, _ = io.WriteString(wrappedStreams.Out, ch.data)
			flushedOut = true
		}
	}

	if !origStreamed {
		tailOut := explicitStdout
		if !flushedOut && tailOut == "" {
			tailOut = capturedLogs.Stdout
		}
		if tailOut != "" && streams.Out != nil && wrappedStreams.Out != nil {
			_, _ = io.WriteString(wrappedStreams.Out, tailOut)
			flushedOut = true
		}

		tailErr := explicitStderr
		if !flushedErr && tailErr == "" {
			tailErr = capturedLogs.Stderr
		}
		if tailErr != "" && streams.ErrOut != nil && wrappedStreams.ErrOut != nil {
			_, _ = io.WriteString(wrappedStreams.ErrOut, tailErr)
			flushedErr = true
		}
	}

	flushRedacting()

	if !flushedOut && !flushedErr {
		return e.flushBufferedActionOutput(ctx, output, capturedLogs, actionTracker)
	}

	var marked provider.Output
	if output != nil {
		marked = *output
	}
	marked.Streamed = true
	results, _ := e.flushBufferedActionOutput(ctx, &marked, capturedLogs, actionTracker)
	return results, true
}

// flushBufferedActionOutput scrubs output.Data via actionTracker, merges any
// non-TTY captured stream chunks into the result envelope, and -- when terminal
// IOStreams are attached and the provider did not already stream its
// output -- writes exact buffered stdout/stderr bytes before completion/failure
// callbacks run so output stays inside the action's log group and is never lost
// on failure.
func (e *Executor) flushBufferedActionOutput(
	ctx context.Context,
	output *provider.Output,
	capturedLogs provider.CapturedStreamLogs,
	actionTracker *redact.Tracker,
) (any, bool) {
	if output == nil && capturedLogs.Empty() {
		return nil, false
	}

	var streamed bool
	var results any
	if output != nil {
		streamed = output.Streamed
		if output.Data != nil {
			results = actionTracker.RedactAny(output.Data)
		}
	}
	if !capturedLogs.Empty() {
		redactedLogs := provider.CapturedStreamLogs{
			Stdout: actionTracker.RedactString(capturedLogs.Stdout),
			Stderr: actionTracker.RedactString(capturedLogs.Stderr),
		}
		results = redactedLogs.MergeIntoData(results)
	}

	streams := e.streams()
	if streamed || streams == nil {
		return results, streamed
	}
	if cliParams, ok := settings.FromContext(ctx); ok && cliParams != nil && cliParams.IsQuiet {
		return results, streamed
	}

	resMap, ok := results.(map[string]any)
	if !ok {
		return results, false
	}

	stdout, _ := resMap["stdout"].(string)
	stderr, _ := resMap["stderr"].(string)
	if stdout == "" && stderr == "" {
		return results, false
	}

	if stdout != "" && streams.Out != nil {
		_, _ = io.WriteString(streams.Out, stdout)
	}
	if stderr != "" && streams.ErrOut != nil {
		_, _ = io.WriteString(streams.ErrOut, stderr)
	}

	return results, true
}

// resolveFingerprintInputs evaluates action inputs against FingerprintCredentialResolver
// so scrt: names and full tkn: options are preserved deterministically without
// touching secret stores or minting tokens, then scrubs any resolver-derived
// tracked secrets from the resulting map.
func (e *Executor) resolveFingerprintInputs(
	ctx context.Context,
	action *ExpandedAction,
	aliasMap map[string]string,
	actionTracker *redact.Tracker,
) (map[string]any, error) {
	fpCtx := spec.WithCredentialResolver(
		redact.WithTracker(ctx, redact.NewTracker()),
		spec.FingerprintCredentialResolver(),
	)
	var (
		fpInputs map[string]any
		err      error
	)
	if action.HasCall() {
		plan, callErr := e.expandActionCall(fpCtx, action, aliasMap)
		if callErr != nil {
			return nil, callErr
		}
		fpInputs = plan.Inputs
	} else {
		fpInputs, err = e.resolveInputs(fpCtx, action, aliasMap)
		if err != nil {
			return nil, err
		}
	}
	return actionTracker.RedactMap(fpInputs), nil
}

// warnEmptySources surfaces user-visible warnings when an action's source globs
// match no files. A total no-match (allEmpty) means the action cannot be
// fingerprinted meaningfully, so caching is effectively disabled. A partial
// no-match (some patterns matched) still fingerprints correctly and is a milder
// typo hint. Both are written to stderr so they never corrupt structured
// command output (e.g. -o json). It is a no-op when no writer is attached to the
// context (e.g. embedded/library use) so domain behavior never depends on CLI
// wiring.
func (e *Executor) warnEmptySources(ctx context.Context, actionName string, emptyPatterns []string, allEmpty bool) {
	if len(emptyPatterns) == 0 {
		return
	}
	w := writer.FromContext(ctx)
	if w == nil {
		return
	}
	if allEmpty {
		w.WarnStderrf("%s: sources matched no files (fingerprint disabled, action will always re-run): %v",
			actionName, emptyPatterns)
		return
	}
	w.WarnStderrf("%s: source pattern(s) matched no files (ignored): %v",
		actionName, emptyPatterns)
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
		celexp.VarActions: namespace,
		celexp.VarCwd:     e.cwd,
	}

	// Inject resolver execution metadata when available so action when conditions
	// and provider inputs can read __execution.resolvers.<name>.status, duration, etc.
	if e.executionData != nil {
		additionalVars[celexp.VarExecution] = e.executionData
	}

	// Add aliases as top-level variables
	for alias, actionName := range aliasMap {
		if actionData, ok := namespace[actionName]; ok {
			additionalVars[alias] = actionData
		}
	}

	return additionalVars
}

// buildActionEvalVars returns the CEL variables available to an action's runtime
// retry conditions (retryIf and until), combining buildAdditionalVars with any
// forEach iteration bindings when the action is an expanded forEach iteration.
func (e *Executor) buildActionEvalVars(action *ExpandedAction, aliasMap map[string]string) map[string]any {
	vars := e.buildAdditionalVars(aliasMap)
	if action != nil && action.IsForEachIteration() && action.ForEachMetadata != nil {
		meta := action.ForEachMetadata
		vars[celexp.VarItem] = meta.Item
		vars[celexp.VarIndex] = meta.Index
		if action.ForEach != nil {
			if action.ForEach.Item != "" {
				vars[action.ForEach.Item] = meta.Item
			}
			if action.ForEach.Index != "" {
				vars[action.ForEach.Index] = meta.Index
			}
		}
	}
	return vars
}

// callProvider executes the provider for an action. providerName is the
// effective provider (the action's own provider, or a call definition's provider
// for call-based actions).
func (e *Executor) callProvider(ctx context.Context, providerName string, inputs map[string]any) (*provider.Output, provider.CapturedStreamLogs, error) {
	if e.registry == nil {
		return nil, provider.CapturedStreamLogs{}, fmt.Errorf("no provider registry configured")
	}

	prov, ok := e.registry.Get(providerName)
	if !ok {
		return nil, provider.CapturedStreamLogs{}, fmt.Errorf("provider %q not found", providerName)
	}

	// Check for CapabilityAction
	desc := prov.Descriptor()
	hasActionCap := false
	for _, cp := range desc.Capabilities {
		if cp == provider.CapabilityAction {
			hasActionCap = true
			break
		}
	}
	if !hasActionCap {
		return nil, provider.CapturedStreamLogs{}, fmt.Errorf("provider %q does not support action capability", providerName)
	}

	// Set up execution context with action mode, pre-resolved input marker, and
	// a CapturedStreamSink so non-TTY stream chunks stay separate during
	// resultSchema validation.
	sink := &provider.CapturedStreamSink{}
	execCtx := provider.WithCapturedStreamSink(
		provider.WithPreResolvedInputs(provider.WithExecutionMode(ctx, provider.CapabilityAction)),
		sink,
	)

	// Pass through IOStreams from context if available (set by executePhase)
	if streams, ok := provider.IOStreamsFromContext(ctx); ok && streams != nil {
		execCtx = provider.WithIOStreams(execCtx, streams)
	}

	// Create executor and run
	providerExecutor := provider.NewExecutor()
	result, err := providerExecutor.Execute(execCtx, prov, inputs)
	logs := sink.Logs()
	if err != nil {
		if result != nil {
			return &result.Output, logs, err
		}
		return nil, logs, err
	}

	return &result.Output, logs, nil
}

// syncWriter serializes concurrent writes to an underlying io.Writer while
// keeping ordinary Write calls strictly byte-preserving. Explicit CI directives
// use WriteDirectiveLine to align onto column 0 after unterminated output.
type syncWriter struct {
	mu          sync.Mutex
	w           io.Writer
	atLineStart bool
}

func wrapSyncWriter(w io.Writer) io.Writer {
	if w == nil {
		return nil
	}
	if _, ok := w.(*syncWriter); ok {
		return w
	}
	return &syncWriter{w: w, atLineStart: true}
}

func (s *syncWriter) Write(p []byte) (int, error) {
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
func (s *syncWriter) WriteDirectiveLine(line string) error {
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

func synchronizeIOStreams(streams *provider.IOStreams) *provider.IOStreams {
	if streams == nil {
		return nil
	}
	syncOut := wrapSyncWriter(streams.Out)
	var syncErr io.Writer
	if sameUnderlyingWriter(streams.Out, streams.ErrOut) {
		syncErr = syncOut
	} else {
		syncErr = wrapSyncWriter(streams.ErrOut)
	}
	return &provider.IOStreams{
		Out:    syncOut,
		ErrOut: syncErr,
	}
}

func (e *Executor) streams() *provider.IOStreams {
	if e.activeIOStreams != nil {
		return e.activeIOStreams
	}
	return e.ioStreams
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
	if mainErr == nil && finallyErr == nil && len(result.FailedActions) > 0 {
		return ExecutionPartialSuccess
	}

	return ExecutionFailed
}

// GetContext returns the action context for inspection.
func (e *Executor) GetContext() *Context {
	return e.actionContext
}

// progress returns the synchronized progress callback initialized by Execute,
// falling back to progressCallback when internal helpers are called directly.
func (e *Executor) progress() ProgressCallback {
	if e.activeProgress != nil {
		return e.activeProgress
	}
	return e.progressCallback
}

// Reset clears the executor state for reuse.
func (e *Executor) Reset() {
	e.actionContext.Reset()
	e.activeProgress = nil
	e.activeIOStreams = nil
}

// progressRetryAdapter adapts RetryObserver to RetryCallback.
type progressRetryAdapter struct {
	callback   RetryObserver
	actionName string
	tracker    *redact.Tracker
}

func (a *progressRetryAdapter) OnRetryAttempt(actionName string, attempt, maxAttempts int, err error) {
	if a.callback != nil {
		a.callback.OnRetryAttempt(actionName, attempt, maxAttempts, a.tracker.RedactError(err))
	}
}

// NoOpProgressCallback is a progress callback that does nothing.
// Useful for testing or when progress tracking is not needed.
type NoOpProgressCallback struct{}

func (NoOpProgressCallback) OnActionStart(_, _ string)                  {}
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

// fingerprintScope returns the effective fingerprint scope for an action.
// Returns FingerprintScopeAll when no fingerprint config is set.
func fingerprintScope(act *Action) FingerprintScope {
	if act.Fingerprint == nil {
		return FingerprintScopeAll
	}
	return act.Fingerprint.Scope.OrDefault()
}
