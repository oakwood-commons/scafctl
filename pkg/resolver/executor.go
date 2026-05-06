// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// RegistryInterface defines the interface for provider registries

// Log key constants for structured logging.
const (
	logKeyProvider = "provider"
	logKeyStep     = "step"
)

type RegistryInterface interface {
	Register(p provider.Provider) error
	Get(name string) (provider.Provider, error)
	List() []provider.Provider
	// DescriptorLookup returns a function that looks up provider descriptors by name.
	// Returns nil if the registry does not support descriptor lookup.
	DescriptorLookup() DescriptorLookup
}

// ProgressCallback is an interface for receiving execution progress events.
// Implementations can use this to display progress bars, log events, etc.
type ProgressCallback interface {
	// OnPhaseStart is called when a new execution phase begins
	OnPhaseStart(phaseNum int, resolverNames []string)
	// OnResolverComplete is called when a resolver completes successfully.
	// elapsed is the pure execution time of the resolver.
	OnResolverComplete(resolverName string, elapsed time.Duration)
	// OnResolverFailed is called when a resolver fails
	OnResolverFailed(resolverName string, err error)
	// OnResolverSkipped is called when a resolver is skipped due to when condition
	OnResolverSkipped(resolverName string)
}

// Executor executes resolvers in phases with concurrency control
type Executor struct {
	registry         RegistryInterface
	timeout          time.Duration
	maxConcurrency   int              // Max concurrent resolvers per phase (0 = unlimited)
	phaseTimeout     time.Duration    // Max time per phase
	warnValueSize    int64            // Warn when value exceeds this size in bytes (0 = disabled)
	maxValueSize     int64            // Fail when value exceeds this size in bytes (0 = disabled)
	progressCallback ProgressCallback // Optional callback for progress events
	validateAll      bool             // Continue execution and collect all errors instead of stopping at first
	skipValidation   bool             // Skip the validation phase of all resolvers
	skipTransform    bool             // Skip the transform and validation phases of all resolvers
	mockedResolvers  map[string]any   // Pre-populated resolver values that skip execution
}

// ExecutorOption is a functional option for configuring the Executor
type ExecutorOption func(*Executor)

// WithMaxConcurrency sets the maximum number of resolvers that can execute concurrently per phase
func WithMaxConcurrency(maxConcurrency int) ExecutorOption {
	return func(e *Executor) {
		e.maxConcurrency = maxConcurrency
	}
}

// WithPhaseTimeout sets the maximum time allowed for each phase to complete
func WithPhaseTimeout(timeout time.Duration) ExecutorOption {
	return func(e *Executor) {
		e.phaseTimeout = timeout
	}
}

// WithDefaultTimeout sets the default timeout for individual resolver execution
func WithDefaultTimeout(timeout time.Duration) ExecutorOption {
	return func(e *Executor) {
		e.timeout = timeout
	}
}

// WithWarnValueSize sets the size threshold for warning about large values
// Set to 0 to disable warnings
func WithWarnValueSize(bytes int64) ExecutorOption {
	return func(e *Executor) {
		e.warnValueSize = bytes
	}
}

// WithMaxValueSize sets the maximum allowed value size in bytes
// Values exceeding this limit will cause resolver execution to fail
// Set to 0 to disable the limit
func WithMaxValueSize(bytes int64) ExecutorOption {
	return func(e *Executor) {
		e.maxValueSize = bytes
	}
}

// WithProgressCallback sets a callback for receiving execution progress events.
// This enables real-time progress reporting during resolver execution.
func WithProgressCallback(callback ProgressCallback) ExecutorOption {
	return func(e *Executor) {
		e.progressCallback = callback
	}
}

// WithValidateAll enables validate-all mode where execution continues even when
// resolvers fail, collecting all errors instead of stopping at the first error.
// Resolvers that depend on failed resolvers will be skipped.
func WithValidateAll(enabled bool) ExecutorOption {
	return func(e *Executor) {
		e.validateAll = enabled
	}
}

// WithSkipValidation disables the validation phase for all resolvers.
// When enabled, resolvers will execute their resolve and transform phases
// but skip validation entirely.
func WithSkipValidation(enabled bool) ExecutorOption {
	return func(e *Executor) {
		e.skipValidation = enabled
	}
}

// WithSkipTransform disables the transform and validation phases for all resolvers.
// When enabled, resolvers will execute only the resolve phase, returning the raw
// resolved value without any transformations or validations applied.
func WithSkipTransform(enabled bool) ExecutorOption {
	return func(e *Executor) {
		e.skipTransform = enabled
	}
}

// WithMockedResolvers pre-populates resolver values so that the corresponding
// resolvers skip execution entirely and return the mocked value. This enables
// functional testing of downstream resolvers and CEL expressions without
// hitting external APIs or services.
func WithMockedResolvers(mocks map[string]any) ExecutorOption {
	return func(e *Executor) {
		e.mockedResolvers = mocks
	}
}

// NewExecutor creates a new resolver executor
func NewExecutor(registry RegistryInterface, opts ...ExecutorOption) *Executor {
	executor := &Executor{
		registry:       registry,
		timeout:        settings.DefaultResolverTimeout,
		maxConcurrency: 0, // unlimited by default
		phaseTimeout:   settings.DefaultPhaseTimeout,
	}

	for _, opt := range opts {
		opt(executor)
	}

	return executor
}

// ConfigInput holds the configuration values for resolver executor initialization.
// This mirrors config.ResolverConfig but avoids circular dependencies.
type ConfigInput struct {
	// Timeout is the default timeout per resolver execution
	Timeout time.Duration `json:"timeout" yaml:"timeout" doc:"Default timeout per resolver execution"`
	// PhaseTimeout is the maximum time for each resolution phase
	PhaseTimeout time.Duration `json:"phaseTimeout" yaml:"phaseTimeout" doc:"Maximum time for each resolution phase"`
	// MaxConcurrency is the maximum concurrent resolvers per phase (0 = unlimited)
	MaxConcurrency int `json:"maxConcurrency" yaml:"maxConcurrency" doc:"Maximum concurrent resolvers per phase (0 = unlimited)" maximum:"1000" example:"10"`
	// WarnValueSize is the warn threshold in bytes (0 = disabled)
	WarnValueSize int64 `json:"warnValueSize" yaml:"warnValueSize" doc:"Warn threshold in bytes (0 = disabled)" example:"1048576"`
	// MaxValueSize is the max value size in bytes (0 = disabled)
	MaxValueSize int64 `json:"maxValueSize" yaml:"maxValueSize" doc:"Max value size in bytes (0 = disabled)" example:"10485760"`
	// ValidateAll enables collecting all errors instead of stopping at first
	ValidateAll bool `json:"validateAll" yaml:"validateAll" doc:"Collect all validation errors instead of stopping at first"`
}

// NewExecutorFromAppConfig creates a new resolver executor using app configuration.
// CLI flags can override these defaults using the returned executor options.
//
// Example:
//
//	cfg := resolver.ResolverConfigInput{
//	    Timeout:        30 * time.Second,
//	    PhaseTimeout:   5 * time.Minute,
//	    MaxConcurrency: 0,
//	}
//	opts := resolver.OptionsFromAppConfig(cfg)
//	executor := resolver.NewExecutor(registry, opts...)
func OptionsFromAppConfig(cfg ConfigInput) []ExecutorOption {
	var opts []ExecutorOption

	if cfg.Timeout > 0 {
		opts = append(opts, WithDefaultTimeout(cfg.Timeout))
	}
	if cfg.PhaseTimeout > 0 {
		opts = append(opts, WithPhaseTimeout(cfg.PhaseTimeout))
	}
	if cfg.MaxConcurrency > 0 {
		opts = append(opts, WithMaxConcurrency(cfg.MaxConcurrency))
	}
	if cfg.WarnValueSize > 0 {
		opts = append(opts, WithWarnValueSize(cfg.WarnValueSize))
	}
	if cfg.MaxValueSize > 0 {
		opts = append(opts, WithMaxValueSize(cfg.MaxValueSize))
	}
	if cfg.ValidateAll {
		opts = append(opts, WithValidateAll(true))
	}

	return opts
}

// Execute runs all resolvers in phases and returns the enriched context.
// Use FromContext(ctx) to retrieve the resolver results.
func (e *Executor) Execute(ctx context.Context, resolvers []*Resolver, params map[string]any) (context.Context, error) {
	lgr := logger.FromContext(ctx)

	// Span for the full resolver pass so per-resolver child spans nest under it.
	ctx, span := telemetry.Tracer(telemetry.TracerResolver).Start(ctx, "resolver.Execute",
		trace.WithAttributes(attribute.Int("resolver.count", len(resolvers))),
	)
	defer span.End()

	// Create descriptor lookup function from registry
	lookup := e.registry.DescriptorLookup()

	// Build execution phases
	buildResult, err := BuildPhases(resolvers, lookup)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return ctx, fmt.Errorf("failed to build execution phases: %w", err)
	}

	lgr.V(1).Info("resolver execution plan", "phases", len(buildResult.Phases))

	// Create resolver context
	resolverCtx := NewContext()
	ctx = WithContext(ctx, resolverCtx)

	// Inject pre-execution plan topology as __plan so resolvers can reference
	// phase number, dependency list, and dependency count in when conditions
	// and provider inputs before any resolver executes.
	resolverCtx.Set(celexp.VarPlan, buildResult.Plan.ToMap())

	// Add parameters to context for parameter provider
	ctx = provider.WithParameters(ctx, params)

	// Also add parameters directly to resolver context for CEL expressions
	for key, value := range params {
		resolverCtx.Set(key, value)
	}

	// Inject mocked resolver values. These resolvers will be skipped during
	// execution (see executeResolver) and downstream resolvers can reference
	// them normally via CEL expressions.
	for name, value := range e.mockedResolvers {
		resolverCtx.SetResult(name, &ExecutionResult{
			Value:  value,
			Status: ExecutionStatusSuccess,
		})
		lgr.V(1).Info("injected mocked resolver value", "resolver", name)
	}

	// Track failed resolvers for validate-all mode
	var failedResolvers sync.Map // key: resolver name, value: true
	var aggregatedError *AggregatedExecutionError
	if e.validateAll {
		aggregatedError = &AggregatedExecutionError{}
	}

	// Build dependency map for all resolvers (used in validate-all mode).
	// Re-use the deps map already computed by BuildPhases.
	depsMap := buildResult.Deps

	// Execute phases sequentially
	for _, phase := range buildResult.Phases {
		lgr.V(1).Info("executing resolver phase",
			"phase", phase.Phase,
			"resolvers", len(phase.Resolvers))

		// Notify callback of phase start
		if e.progressCallback != nil {
			resolverNames := make([]string, len(phase.Resolvers))
			for i, r := range phase.Resolvers {
				resolverNames[i] = r.Name
			}
			e.progressCallback.OnPhaseStart(phase.Phase, resolverNames)
		}

		phaseErr := e.executePhase(ctx, phase, &failedResolvers, depsMap, aggregatedError)
		if phaseErr != nil {
			if !e.validateAll {
				phaseWrapErr := fmt.Errorf("phase %d failed: %w", phase.Phase, phaseErr)
				span.RecordError(phaseWrapErr)
				span.SetStatus(codes.Error, phaseWrapErr.Error())
				return ctx, phaseWrapErr
			}
			// In validate-all mode, continue to next phase
			lgr.V(1).Info("phase had errors but continuing in validate-all mode",
				"phase", phase.Phase)
		}
	}

	lgr.V(1).Info("resolver execution complete", "total_phases", len(buildResult.Phases))

	// In validate-all mode, return aggregated error if there were any failures
	if e.validateAll && aggregatedError != nil && aggregatedError.HasErrors() {
		span.RecordError(aggregatedError)
		span.SetStatus(codes.Error, aggregatedError.Error())
		return ctx, aggregatedError
	}

	return ctx, nil
}

// executePhase executes all resolvers in a phase concurrently with optional concurrency limit.
// In validate-all mode, it tracks failed resolvers and skips resolvers whose dependencies failed.
func (e *Executor) executePhase(ctx context.Context, phase *PhaseGroup, failedResolvers *sync.Map, depsMap map[string][]string, aggregatedError *AggregatedExecutionError) error {
	lgr := logger.FromContext(ctx)

	// Apply phase timeout
	phaseCtx, cancel := context.WithTimeout(ctx, e.phaseTimeout)
	defer cancel()

	var wg sync.WaitGroup

	// Use struct to track both error and resolver name for aggregation
	type resolverResult struct {
		resolverName string
		err          error
		skipped      bool // skipped due to when condition
		depSkipped   bool // skipped due to failed dependency
	}
	resultChan := make(chan resolverResult, len(phase.Resolvers))

	// Create semaphore for concurrency control (if limit is set)
	var sem chan struct{}
	if e.maxConcurrency > 0 {
		sem = make(chan struct{}, e.maxConcurrency)
		lgr.V(1).Info("phase concurrency limit enabled",
			"phase", phase.Phase,
			"maxConcurrency", e.maxConcurrency)
	}

	for _, resolver := range phase.Resolvers {
		wg.Add(1)

		go func(r *Resolver) {
			defer wg.Done()

			// In validate-all mode, check if any dependencies have failed
			if e.validateAll && failedResolvers != nil {
				deps := depsMap[r.Name]
				for _, depName := range deps {
					if _, failed := failedResolvers.Load(depName); failed {
						lgr.V(1).Info("skipping resolver due to failed dependency",
							"resolver", r.Name,
							"failedDependency", depName)
						if e.progressCallback != nil {
							e.progressCallback.OnResolverSkipped(r.Name)
						}
						// Mark this resolver as failed too (since it couldn't run)
						failedResolvers.Store(r.Name, true)
						resultChan <- resolverResult{resolverName: r.Name, depSkipped: true}
						return
					}
				}
			}

			// Acquire semaphore slot (blocks if at limit)
			if sem != nil {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }() // Release on completion
				case <-phaseCtx.Done():
					if e.progressCallback != nil {
						e.progressCallback.OnResolverFailed(r.Name, fmt.Errorf("phase timeout before execution"))
					}
					err := fmt.Errorf("resolver %q: phase timeout before execution", r.Name)
					if failedResolvers != nil {
						failedResolvers.Store(r.Name, true)
					}
					resultChan <- resolverResult{resolverName: r.Name, err: err}
					return
				}
			}

			// Check for phase timeout before executing
			if phaseCtx.Err() != nil {
				if e.progressCallback != nil {
					e.progressCallback.OnResolverFailed(r.Name, fmt.Errorf("phase timeout"))
				}
				err := fmt.Errorf("resolver %q: phase timeout", r.Name)
				if failedResolvers != nil {
					failedResolvers.Store(r.Name, true)
				}
				resultChan <- resolverResult{resolverName: r.Name, err: err}
				return
			}

			resolverStart := time.Now()
			skipped, err := e.executeResolver(phaseCtx, r, phase.Phase)
			resolverElapsed := time.Since(resolverStart)
			switch {
			case err != nil:
				if e.progressCallback != nil {
					e.progressCallback.OnResolverFailed(r.Name, err)
				}
				if failedResolvers != nil {
					failedResolvers.Store(r.Name, true)
				}
				resultChan <- resolverResult{resolverName: r.Name, err: fmt.Errorf("resolver %q failed: %w", r.Name, err)}
			case skipped:
				resultChan <- resolverResult{resolverName: r.Name, skipped: true}
			default:
				// Resolver completed successfully
				if e.progressCallback != nil {
					e.progressCallback.OnResolverComplete(r.Name, resolverElapsed)
				}
				resultChan <- resolverResult{resolverName: r.Name}
			}
		}(resolver)
	}

	// Wait for all resolvers in phase to complete
	wg.Wait()
	close(resultChan)

	// Collect results
	var errs []error
	for result := range resultChan {
		switch {
		case result.err != nil:
			errs = append(errs, result.err)
			if aggregatedError != nil {
				aggregatedError.Add(result.resolverName, phase.Phase, result.err)
			}
		case result.depSkipped:
			if aggregatedError != nil {
				aggregatedError.AddSkipped(result.resolverName)
			}
		case !result.skipped:
			// Only count as success if not skipped for any reason
			if aggregatedError != nil {
				aggregatedError.IncrementSucceeded()
			}
		}
	}

	if len(errs) > 0 {
		if e.validateAll {
			// In validate-all mode, we've already recorded errors, just return non-nil to signal there were errors
			return errs[0]
		}
		// In normal mode, return first error
		return errs[0]
	}

	return nil
}

// executeResolver executes a single resolver through all phases and tracks execution metadata.
// Returns (skipped, error) where skipped is true if the resolver was skipped due to when condition.
func (e *Executor) executeResolver(ctx context.Context, r *Resolver, phaseNum int) (bool, error) {
	lgr := logger.FromContext(ctx)

	// Add resolver-specific context fields for all logging
	resolverLgr := lgr.WithValues("resolver", r.Name, "phase", phaseNum)
	if r.Sensitive {
		resolverLgr = resolverLgr.WithValues("sensitive", true)
	}
	ctx = logger.WithLogger(ctx, &resolverLgr)

	// Create a child span for this individual resolver execution.
	ctx, span := telemetry.Tracer(telemetry.TracerResolver).Start(ctx, "resolver.executeResolver",
		trace.WithAttributes(
			attribute.String("resolver.name", r.Name),
			attribute.Int("resolver.phase", phaseNum),
			attribute.Bool("resolver.sensitive", r.Sensitive),
		),
	)
	defer span.End()

	resolverLgr.V(1).Info("executing resolver")

	// Get resolver context from context
	resolverCtx, _ := FromContext(ctx)

	// Initialize execution result
	result := &ExecutionResult{
		Phase:          phaseNum,
		StartTime:      time.Now(),
		Status:         ExecutionStatusSuccess,
		PhaseMetrics:   []PhaseMetrics{},
		FailedAttempts: []ProviderAttempt{},
	}
	providerCallCount := 0

	// Ensure result is stored even on early return
	defer func() {
		result.EndTime = time.Now()
		result.TotalDuration = result.EndTime.Sub(result.StartTime)
		result.ProviderCallCount = providerCallCount

		// Calculate value size
		if result.Value != nil {
			result.ValueSizeBytes = calculateValueSize(result.Value)
		}

		// Count dependencies (nil lookup is acceptable for counting since provider-specific
		// extraction would add more deps, not fewer - this is a conservative estimate)
		result.DependencyCount = len(extractDependencies(r, nil))

		// Collect failed attempts from context
		if existing, ok := resolverCtx.data.Load("__failed_attempts"); ok {
			if attempts, ok := existing.([]ProviderAttempt); ok {
				result.FailedAttempts = attempts
			}
			// Clean up temporary storage
			resolverCtx.data.Delete("__failed_attempts")
		}

		// Only store result in context if resolver was NOT skipped
		// Skipped resolvers must be truly absent from _ (the resolver context map)
		if result.Status != ExecutionStatusSkipped {
			resolverCtx.SetResult(r.Name, result)
		}

		// Record metrics (always, including skipped)
		RecordResolverExecution(r.Name, result)

		// Log completion with status
		switch result.Status {
		case ExecutionStatusSuccess:
			resolverLgr.V(1).Info("resolver completed successfully",
				"duration", result.TotalDuration,
				"providerCalls", result.ProviderCallCount,
				"valueSizeBytes", result.ValueSizeBytes)
		case ExecutionStatusSkipped:
			resolverLgr.V(1).Info("resolver skipped")
		case ExecutionStatusFailed:
			// Redact error message if sensitive
			errorMsg := "error message redacted"
			if !r.Sensitive && result.Error != nil {
				errorMsg = result.Error.Error()
			}
			resolverLgr.V(1).Info("resolver failed",
				"duration", result.TotalDuration,
				"providerCalls", result.ProviderCallCount,
				"failedAttempts", len(result.FailedAttempts),
				"error", errorMsg)
			// Record failure on the span (never expose redacted messages to telemetry).
			if result.Error != nil {
				span.RecordError(result.Error)
				span.SetStatus(codes.Error, "resolver failed")
			}
		}
	}()

	// Apply resolver timeout
	timeout := e.timeout
	if r.Timeout != nil {
		timeout = *r.Timeout
	}

	resolverContext, cancelResolver := context.WithTimeout(ctx, timeout)
	defer cancelResolver()

	// Check if this resolver has a mocked value (injected via WithMockedResolvers).
	// If so, the value is already in the resolver context; skip execution entirely.
	// Note: we do NOT call OnResolverComplete here because the phase runner
	// (executePhase) already emits the completion callback after executeResolver returns.
	if resolverCtx.Has(r.Name) && e.isMocked(r.Name) {
		mockedValue, _ := resolverCtx.Get(r.Name)
		result.Value = mockedValue
		result.Status = ExecutionStatusSuccess
		resolverLgr.V(1).Info("resolver mocked — skipping execution")
		return false, nil
	}

	// Check when condition
	if r.When != nil {
		shouldExecute, err := e.evaluateCondition(resolverContext, r.When)
		if err != nil {
			result.Status = ExecutionStatusFailed
			result.Error = fmt.Errorf("failed to evaluate when condition: %w", err)
			return false, result.Error
		}
		if !shouldExecute {
			lgr.V(1).Info("skipping resolver due to when condition", "name", r.Name)
			result.Status = ExecutionStatusSkipped
			// Notify callback that resolver was skipped
			if e.progressCallback != nil {
				e.progressCallback.OnResolverSkipped(r.Name)
			}
			return true, nil
		}
	}

	// Execute resolve phase
	phaseStart := time.Now()
	value, providerCalls, err := e.executeResolvePhase(resolverContext, r.Resolve)
	providerCallCount += providerCalls
	result.PhaseMetrics = append(result.PhaseMetrics, PhaseMetrics{
		Phase:    "resolve",
		Duration: time.Since(phaseStart),
		Started:  phaseStart,
		Ended:    time.Now(),
	})

	if err != nil {
		// Emit nil on failure (partial emission)
		result.Value = nil
		result.Status = ExecutionStatusFailed
		result.Error = &ExecutionError{
			ResolverName:  r.Name,
			Phase:         "resolve",
			Step:          0,
			Cause:         err,
			CustomMessage: resolveCustomErrorMessage(resolverContext, r, err),
		}
		return false, result.Error
	}

	// Execute transform phase
	// Skip if transform is disabled via executor option (also skips validate)
	coercionPhase := "resolve"
	if r.Transform != nil && !e.skipTransform {
		coercionPhase = "transform"
		phaseStart := time.Now()
		transformed, providerCalls, err := e.executeTransformPhase(resolverContext, r.Transform, value)
		providerCallCount += providerCalls
		result.PhaseMetrics = append(result.PhaseMetrics, PhaseMetrics{
			Phase:    "transform",
			Duration: time.Since(phaseStart),
			Started:  phaseStart,
			Ended:    time.Now(),
		})

		if err != nil {
			// Emit pre-transform value (partial emission)
			result.Value = value
			result.Status = ExecutionStatusFailed
			result.Error = &ExecutionError{
				ResolverName:  r.Name,
				Phase:         "transform",
				Step:          0,
				Cause:         err,
				CustomMessage: resolveCustomErrorMessage(resolverContext, r, err),
			}
			return false, result.Error
		}

		value = transformed
	}

	// Type coercion on the final value (after resolve + optional transform)
	// The type field describes the resolver's output contract, so we enforce it
	// exactly once on whatever the final value is.
	if r.Type != "" && r.Type != TypeAny {
		coerced, err := CoerceType(value, r.Type)
		if err != nil {
			result.Value = value
			result.Status = ExecutionStatusFailed
			result.Error = &TypeCoercionError{
				ResolverName:  r.Name,
				Phase:         coercionPhase,
				SourceType:    fmt.Sprintf("%T", value),
				TargetType:    r.Type,
				Cause:         err,
				CustomMessage: resolveCustomErrorMessage(resolverContext, r, err),
			}
			return false, result.Error
		}
		value = coerced
	}

	// Execute validate phase (runs all validations and aggregates failures)
	// Skip if validation is disabled via executor option, or if transform is skipped (implies skip validation)
	if r.Validate != nil && !e.skipValidation && !e.skipTransform {
		phaseStart := time.Now()
		providerCalls, validationErr := e.executeValidatePhase(resolverContext, r.Name, r.Sensitive, r.Validate, value)
		providerCallCount += providerCalls
		result.PhaseMetrics = append(result.PhaseMetrics, PhaseMetrics{
			Phase:    "validate",
			Duration: time.Since(phaseStart),
			Started:  phaseStart,
			Ended:    time.Now(),
		})

		if validationErr != nil {
			// Emit transformed value even on validation failure (partial emission)
			result.Value = value
			result.Status = ExecutionStatusFailed
			// Inject custom message from messages.error if configured
			var aggErr *AggregatedValidationError
			if errors.As(validationErr, &aggErr) {
				aggErr.CustomMessage = resolveCustomErrorMessage(resolverContext, r, validationErr)
			}
			result.Error = validationErr
			return false, result.Error
		}
	}

	// Check value size limits before storing final value
	if valueSize := calculateValueSize(value); valueSize > 0 {
		if e.maxValueSize > 0 && valueSize > e.maxValueSize {
			result.Value = value
			result.Status = ExecutionStatusFailed
			result.Error = &ValueSizeError{
				ResolverName: r.Name,
				ActualSize:   valueSize,
				MaxSize:      e.maxValueSize,
			}
			return false, result.Error
		}
		if e.warnValueSize > 0 && valueSize > e.warnValueSize {
			lgr.V(0).Info("resolver value exceeds recommended size",
				"resolver", r.Name,
				"size", valueSize,
				"limit", e.warnValueSize)
		}
	}

	// Emit final value
	result.Value = value
	lgr.V(1).Info("resolver completed",
		"name", r.Name,
		"duration", result.TotalDuration,
		"providerCalls", result.ProviderCallCount)

	return false, nil
}

// isMocked returns true if the resolver name is in the mocked resolvers set.
func (e *Executor) isMocked(name string) bool {
	if e.mockedResolvers == nil {
		return false
	}
	_, ok := e.mockedResolvers[name]
	return ok
}

func (e *Executor) evaluateCondition(ctx context.Context, cond *Condition) (bool, error) {
	if cond == nil || cond.Expr == nil {
		return true, nil
	}

	// Get resolver context from context
	resolverCtx, _ := FromContext(ctx)

	// Get resolver data for CEL evaluation
	data := resolverCtx.ToMap()

	// Promote __plan to a top-level CEL variable so when conditions can use
	// __plan["resolverName"].phase rather than _["__plan"]["resolverName"]["phase"].
	additionalVars := map[string]any{}
	if plan, ok := data[celexp.VarPlan]; ok {
		additionalVars[celexp.VarPlan] = plan
		delete(data, celexp.VarPlan)
	}

	// Evaluate the CEL expression
	result, err := celexp.EvaluateExpression(ctx, string(*cond.Expr), data, additionalVars)
	if err != nil {
		return false, fmt.Errorf("condition evaluation failed: %w", err)
	}

	// Check if result is boolean
	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("condition must evaluate to boolean, got %T", result)
	}

	return boolResult, nil
}

// evaluateConditionWithSelf evaluates a condition expression with __self set to the provided value
// This is used for until: conditions where __self should be the current resolved value
func (e *Executor) evaluateConditionWithSelf(ctx context.Context, cond *Condition, self any) (bool, error) {
	if cond == nil || cond.Expr == nil {
		return true, nil
	}

	// Get resolver context from context
	resolverCtx, _ := FromContext(ctx)

	// Get resolver data for CEL evaluation
	data := resolverCtx.ToMap()

	// Pass __self as an additional variable so it's available as a top-level CEL variable.
	// Also promote __plan so until: conditions can reference topology data.
	additionalVars := map[string]any{
		celexp.VarSelf: self,
	}
	if plan, ok := data[celexp.VarPlan]; ok {
		additionalVars[celexp.VarPlan] = plan
		delete(data, celexp.VarPlan)
	}

	// Evaluate the CEL expression
	result, err := celexp.EvaluateExpression(ctx, string(*cond.Expr), data, additionalVars)
	if err != nil {
		return false, fmt.Errorf("condition evaluation failed: %w", err)
	}

	// Check if result is boolean
	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("condition must evaluate to boolean, got %T", result)
	}

	return boolResult, nil
}

// executeResolvePhase executes the resolve phase with provider fallback chain
func (e *Executor) executeResolvePhase(ctx context.Context, phase *ResolvePhase) (any, int, error) {
	if phase == nil {
		return nil, 0, fmt.Errorf("resolve phase is required")
	}

	lgr := logger.FromContext(ctx)
	providerCallCount := 0

	// Get resolver context to track failed attempts
	resolverCtx, _ := FromContext(ctx)

	// Check phase-level when condition
	if phase.When != nil {
		shouldExecute, err := e.evaluateCondition(ctx, phase.When)
		if err != nil {
			return nil, providerCallCount, fmt.Errorf("failed to evaluate resolve phase when condition: %w", err)
		}
		if !shouldExecute {
			lgr.V(1).Info("skipping resolve phase due to when condition")
			return nil, providerCallCount, fmt.Errorf("resolve phase skipped by when condition")
		}
	}

	// Try sources in order until one succeeds or we reach the end
	var lastErr error
	for i, source := range phase.With {
		attemptStart := time.Now()

		// Check source-level when condition
		if source.When != nil {
			shouldExecute, err := e.evaluateCondition(ctx, source.When)
			if err != nil {
				lgr.V(1).Info("failed to evaluate source when condition",
					"source", i+1,
					logKeyProvider, source.Provider,
					"error", err)
				if source.OnError == ErrorBehaviorFail {
					return nil, providerCallCount, fmt.Errorf("source %d: when condition evaluation failed: %w", i+1, err)
				}
				// Default: continue to next source (resolve phase is a fallback chain)
				lastErr = err
				continue
			}
			if !shouldExecute {
				lgr.V(1).Info("skipping source due to when condition",
					"source", i+1,
					logKeyProvider, source.Provider)
				continue
			}
		}

		// Handle forEach iteration on resolve sources
		if source.ForEach != nil {
			result, calls, err := e.executeForEachSource(ctx, &source, i)
			providerCallCount += calls
			if err != nil {
				lgr.V(1).Info("forEach source failed",
					"source", i+1,
					logKeyProvider, source.Provider,
					"error", err)
				if source.OnError == ErrorBehaviorFail {
					return nil, providerCallCount, fmt.Errorf("source %d (%s) forEach failed: %w", i+1, source.Provider, err)
				}
				lastErr = err
				continue
			}
			return result, providerCallCount, nil
		}

		// Execute provider in resolve (from) mode
		value, err := e.executeProvider(provider.WithExecutionMode(ctx, provider.CapabilityFrom), source.Provider, source.Inputs)
		providerCallCount++
		attemptDuration := time.Since(attemptStart)

		if err != nil {
			lgr.V(1).Info("provider failed",
				"source", i+1,
				logKeyProvider, source.Provider,
				"error", err,
				"onError", source.OnError,
				"duration", attemptDuration)

			// Track failed attempt
			e.trackFailedAttempt(resolverCtx, source.Provider, "resolve", err, attemptDuration, string(source.OnError), i)

			lastErr = err

			// Handle error behavior: resolve phase defaults to continue (fallback chain)
			if source.OnError == ErrorBehaviorFail {
				return nil, providerCallCount, fmt.Errorf("source %d (%s) failed: %w", i+1, source.Provider, err)
			}

			continue // Default: try next source
		}

		// Success - check until condition with __self set to current value
		if phase.Until != nil {
			stop, err := e.evaluateConditionWithSelf(ctx, phase.Until, value)
			if err != nil {
				return nil, providerCallCount, fmt.Errorf("until condition evaluation failed: %w", err)
			}
			if stop {
				lgr.V(1).Info("stopping resolve chain due to until condition", "source", i+1)
				return value, providerCallCount, nil
			}
		}

		// Default: return first successful value
		return value, providerCallCount, nil
	}

	// All sources failed
	if lastErr != nil {
		return nil, providerCallCount, fmt.Errorf("all sources failed, last error: %w", lastErr)
	}

	return nil, providerCallCount, fmt.Errorf("no sources produced a value")
}

// executeTransformPhase executes the transform phase as a chain of transformations
func (e *Executor) executeTransformPhase(ctx context.Context, phase *TransformPhase, value any) (any, int, error) {
	if phase == nil {
		return value, 0, nil
	}

	lgr := logger.FromContext(ctx)
	providerCallCount := 0

	// Get resolver context from context
	resolverCtx, _ := FromContext(ctx)

	// Check phase-level when condition
	if phase.When != nil {
		shouldExecute, err := e.evaluateCondition(ctx, phase.When)
		if err != nil {
			return value, providerCallCount, fmt.Errorf("failed to evaluate transform phase when condition: %w", err)
		}
		if !shouldExecute {
			lgr.V(1).Info("skipping transform phase due to when condition")
			return value, providerCallCount, nil
		}
	}

	// Apply transformations in sequence
	currentValue := value
	for i, transform := range phase.With {
		attemptStart := time.Now()

		// Handle forEach iteration
		if transform.ForEach != nil {
			transformed, calls, err := e.executeForEachTransform(ctx, &transform, currentValue, i)
			providerCallCount += calls
			if err != nil {
				if transform.OnError == ErrorBehaviorContinue {
					lgr.V(1).Info("forEach transform failed, continuing",
						logKeyStep, i+1,
						logKeyProvider, transform.Provider,
						"error", err)
					continue
				}
				return currentValue, providerCallCount, err
			}
			currentValue = transformed
			continue
		}

		// Check transform-level when condition (with __self set to current value)
		if transform.When != nil {
			shouldExecute, err := e.evaluateConditionWithSelf(ctx, transform.When, currentValue)
			if err != nil {
				lgr.V(1).Info("failed to evaluate transform when condition",
					logKeyStep, i+1,
					logKeyProvider, transform.Provider,
					"error", err)
				if transform.OnError == ErrorBehaviorContinue {
					continue
				}
				return currentValue, providerCallCount, fmt.Errorf("step %d: when condition evaluation failed: %w", i+1, err)
			}
			if !shouldExecute {
				lgr.V(1).Info("skipping transform step due to when condition",
					logKeyStep, i+1,
					logKeyProvider, transform.Provider)
				continue
			}
		}

		// Execute provider with __self set to current value (transform mode)
		transformed, err := e.executeProviderWithSelf(provider.WithExecutionMode(ctx, provider.CapabilityTransform), transform.Provider, transform.Inputs, currentValue)
		providerCallCount++
		attemptDuration := time.Since(attemptStart)

		if err != nil {
			lgr.V(1).Info("transform provider failed",
				logKeyStep, i+1,
				logKeyProvider, transform.Provider,
				"error", err,
				"onError", transform.OnError,
				"duration", attemptDuration)

			// Track failed attempt
			e.trackFailedAttempt(resolverCtx, transform.Provider, "transform", err, attemptDuration, string(transform.OnError), i)

			// Handle error behavior
			if transform.OnError == ErrorBehaviorContinue {
				continue // Skip this transform, keep current value
			}

			return currentValue, providerCallCount, fmt.Errorf("step %d (%s) failed: %w", i+1, transform.Provider, err)
		}

		// Update current value for next transform
		currentValue = transformed
	}

	return currentValue, providerCallCount, nil
}

// executeForEachTransform executes a transform step with forEach iteration.
// It iterates over the input array, executing the provider for each element in parallel,
// and collects results preserving order.
func (e *Executor) executeForEachTransform(ctx context.Context, transform *ProviderTransform, currentValue any, stepIndex int) (any, int, error) {
	lgr := logger.FromContext(ctx)
	resolverCtx, _ := FromContext(ctx)
	resolverData := resolverCtx.ToMap()

	// Determine the array to iterate over
	var inputArray []any
	if transform.ForEach.In != nil {
		// Use explicit 'in' source
		resolved, err := transform.ForEach.In.Resolve(ctx, resolverData, currentValue)
		if err != nil {
			return currentValue, 0, fmt.Errorf("step %d: failed to resolve forEach.in: %w", stepIndex+1, err)
		}
		arr, ok := toSlice(resolved)
		if !ok {
			return currentValue, 0, &ForEachTypeError{
				Step:       stepIndex,
				ActualType: fmt.Sprintf("%T", resolved),
			}
		}
		inputArray = arr
	} else {
		// Default to __self (currentValue)
		arr, ok := toSlice(currentValue)
		if !ok {
			return currentValue, 0, &ForEachTypeError{
				Step:       stepIndex,
				ActualType: fmt.Sprintf("%T", currentValue),
			}
		}
		inputArray = arr
	}

	// Handle empty array - return empty array
	if len(inputArray) == 0 {
		lgr.V(1).Info("forEach: empty input array, returning []",
			logKeyStep, stepIndex+1,
			logKeyProvider, transform.Provider)
		return []any{}, 0, nil
	}

	lgr.V(1).Info("executing forEach transform",
		logKeyStep, stepIndex+1,
		logKeyProvider, transform.Provider,
		"itemCount", len(inputArray),
		"concurrency", transform.ForEach.Concurrency)

	// Create result slice and error tracking
	results := make([]any, len(inputArray))
	errors := make([]error, len(inputArray))
	providerCallCount := 0
	var providerCallCountMu sync.Mutex

	// Create semaphore for concurrency control
	var sem chan struct{}
	if transform.ForEach.Concurrency > 0 {
		sem = make(chan struct{}, transform.ForEach.Concurrency)
	}

	var wg sync.WaitGroup
	for idx, item := range inputArray {
		wg.Add(1)
		go func(i int, itm any) {
			defer wg.Done()

			// Acquire semaphore if concurrency limited
			if sem != nil {
				sem <- struct{}{}
				defer func() { <-sem }()
			}

			// Build iteration context
			iterCtx := &IterationContext{
				Item:       itm,
				Index:      i,
				ItemAlias:  transform.ForEach.Item,
				IndexAlias: transform.ForEach.Index,
			}

			// Check when condition with iteration variables
			if transform.When != nil {
				shouldExecute, err := e.evaluateConditionWithIterationContext(ctx, transform.When, currentValue, iterCtx)
				if err != nil {
					errors[i] = fmt.Errorf("when condition evaluation failed: %w", err)
					return
				}
				if !shouldExecute {
					lgr.V(2).Info("skipping forEach iteration due to when condition",
						logKeyStep, stepIndex+1,
						"index", i)
					// Mark as skipped with nil result
					results[i] = nil
					return
				}
			}

			// Execute provider with iteration context (transform mode)
			result, err := e.executeProviderWithIterationContext(provider.WithExecutionMode(ctx, provider.CapabilityTransform), transform.Provider, transform.Inputs, currentValue, iterCtx)
			providerCallCountMu.Lock()
			providerCallCount++
			providerCallCountMu.Unlock()

			if err != nil {
				errors[i] = err
				lgr.V(1).Info("forEach iteration failed",
					logKeyStep, stepIndex+1,
					"index", i,
					"error", err)
			} else {
				results[i] = result
			}
		}(idx, item)
	}

	wg.Wait()

	// Check for errors and build output
	var hasErrors bool
	for _, err := range errors {
		if err != nil {
			hasErrors = true
			break
		}
	}

	if hasErrors {
		if transform.OnError == ErrorBehaviorContinue {
			// Build result array with error metadata for failed items
			outputResults := make([]any, len(inputArray))
			for i := range inputArray {
				if errors[i] != nil {
					outputResults[i] = ForEachIterationResult{
						Index: i,
						Error: errors[i].Error(),
						Item:  inputArray[i],
					}
				} else {
					outputResults[i] = ForEachIterationResult{
						Index: i,
						Data:  results[i],
						Item:  inputArray[i],
					}
				}
			}
			return outputResults, providerCallCount, nil
		}
		// Return first error
		for i, err := range errors {
			if err != nil {
				return currentValue, providerCallCount, fmt.Errorf("step %d forEach iteration %d failed: %w", stepIndex+1, i, err)
			}
		}
	}

	// All succeeded - filter out nil entries for skipped items unless keepSkipped is set
	if transform.When != nil && !transform.ForEach.KeepSkipped {
		filtered := results[:0]
		for _, r := range results {
			if r != nil {
				filtered = append(filtered, r)
			}
		}
		return filtered, providerCallCount, nil
	}
	return results, providerCallCount, nil
}

// executeForEachSource executes a resolve source with forEach iteration.
// Unlike transform forEach, resolve forEach has no __self and requires forEach.in.
func (e *Executor) executeForEachSource(ctx context.Context, source *ProviderSource, sourceIndex int) (any, int, error) {
	lgr := logger.FromContext(ctx)
	resolverCtx, _ := FromContext(ctx)
	resolverData := resolverCtx.ToMap()

	// forEach.in is required for resolve sources (no __self to default to)
	if source.ForEach.In == nil {
		return nil, 0, fmt.Errorf("source %d: forEach.in is required on resolve steps (no __self available)", sourceIndex+1)
	}

	// Resolve the input array
	resolved, err := source.ForEach.In.Resolve(ctx, resolverData, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("source %d: failed to resolve forEach.in: %w", sourceIndex+1, err)
	}
	inputArray, ok := toSlice(resolved)
	if !ok {
		return nil, 0, &ForEachTypeError{
			Step:       sourceIndex,
			ActualType: fmt.Sprintf("%T", resolved),
		}
	}

	// Handle empty array
	if len(inputArray) == 0 {
		lgr.V(1).Info("forEach: empty input array, returning []",
			"source", sourceIndex+1,
			logKeyProvider, source.Provider)
		return []any{}, 0, nil
	}

	lgr.V(1).Info("executing forEach resolve source",
		"source", sourceIndex+1,
		logKeyProvider, source.Provider,
		"itemCount", len(inputArray),
		"concurrency", source.ForEach.Concurrency)

	// Create result slice and error tracking
	results := make([]any, len(inputArray))
	errors := make([]error, len(inputArray))
	providerCallCount := 0
	var providerCallCountMu sync.Mutex

	// Create semaphore for concurrency control
	var sem chan struct{}
	if source.ForEach.Concurrency > 0 {
		sem = make(chan struct{}, source.ForEach.Concurrency)
	}

	var wg sync.WaitGroup
	for idx, item := range inputArray {
		wg.Add(1)
		go func(i int, itm any) {
			defer wg.Done()

			// Acquire semaphore if concurrency limited
			if sem != nil {
				sem <- struct{}{}
				defer func() { <-sem }()
			}

			// Build iteration context
			iterCtx := &IterationContext{
				Item:       itm,
				Index:      i,
				ItemAlias:  source.ForEach.Item,
				IndexAlias: source.ForEach.Index,
			}

			// Check when condition with iteration variables (no __self in resolve phase)
			if source.When != nil {
				shouldExecute, err := e.evaluateConditionWithIterationContext(ctx, source.When, nil, iterCtx)
				if err != nil {
					errors[i] = fmt.Errorf("when condition evaluation failed: %w", err)
					return
				}
				if !shouldExecute {
					lgr.V(2).Info("skipping forEach iteration due to when condition",
						"source", sourceIndex+1,
						"index", i)
					results[i] = nil
					return
				}
			}

			// Execute provider with iteration context (resolve/from mode)
			result, err := e.executeProviderWithIterationContext(provider.WithExecutionMode(ctx, provider.CapabilityFrom), source.Provider, source.Inputs, nil, iterCtx)
			providerCallCountMu.Lock()
			providerCallCount++
			providerCallCountMu.Unlock()

			if err != nil {
				errors[i] = err
				lgr.V(1).Info("forEach iteration failed",
					"source", sourceIndex+1,
					"index", i,
					"error", err)
			} else {
				results[i] = result
			}
		}(idx, item)
	}

	wg.Wait()

	// Check for errors
	var hasErrors bool
	for _, err := range errors {
		if err != nil {
			hasErrors = true
			break
		}
	}

	if hasErrors {
		if source.OnError == ErrorBehaviorContinue {
			outputResults := make([]any, len(inputArray))
			for i := range inputArray {
				if errors[i] != nil {
					outputResults[i] = ForEachIterationResult{
						Index: i,
						Error: errors[i].Error(),
						Item:  inputArray[i],
					}
				} else {
					outputResults[i] = ForEachIterationResult{
						Index: i,
						Data:  results[i],
						Item:  inputArray[i],
					}
				}
			}
			return outputResults, providerCallCount, nil
		}
		// Return first error
		for i, err := range errors {
			if err != nil {
				return nil, providerCallCount, fmt.Errorf("source %d forEach iteration %d failed: %w", sourceIndex+1, i, err)
			}
		}
	}

	// All succeeded - filter out nil entries for skipped items unless keepSkipped is set
	if source.When != nil && !source.ForEach.KeepSkipped {
		filtered := results[:0]
		for _, r := range results {
			if r != nil {
				filtered = append(filtered, r)
			}
		}
		return filtered, providerCallCount, nil
	}
	return results, providerCallCount, nil
}

// evaluateConditionWithIterationContext evaluates a condition with forEach iteration variables
func (e *Executor) evaluateConditionWithIterationContext(ctx context.Context, cond *Condition, self any, iterCtx *IterationContext) (bool, error) {
	if cond == nil || cond.Expr == nil {
		return true, nil
	}

	resolverCtx, _ := FromContext(ctx)
	data := resolverCtx.ToMap()

	// Build additional variables for iteration context
	// All iteration variables go in additionalVars
	additionalVars := make(map[string]any, 5)
	additionalVars[celexp.VarSelf] = self
	additionalVars[celexp.VarItem] = iterCtx.Item
	additionalVars[celexp.VarIndex] = iterCtx.Index
	if iterCtx.ItemAlias != "" {
		additionalVars[iterCtx.ItemAlias] = iterCtx.Item
	}
	if iterCtx.IndexAlias != "" {
		additionalVars[iterCtx.IndexAlias] = iterCtx.Index
	}

	result, err := celexp.EvaluateExpression(ctx, string(*cond.Expr), data, additionalVars)
	if err != nil {
		return false, fmt.Errorf("condition evaluation failed: %w", err)
	}

	boolResult, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("condition must evaluate to boolean, got %T", result)
	}

	return boolResult, nil
}

// executeProviderWithIterationContext executes a provider with forEach iteration context
func (e *Executor) executeProviderWithIterationContext(ctx context.Context, providerName string, inputRefs map[string]*ValueRef, self any, iterCtx *IterationContext) (any, error) {
	resolverCtx, _ := FromContext(ctx)

	prov, err := e.registry.Get(providerName)
	if err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", providerName, err)
	}

	// Resolve all inputs with iteration context
	inputs := make(map[string]any)
	resolverData := resolverCtx.ToMap()

	for key, valueRef := range inputRefs {
		resolved, err := valueRef.ResolveWithIterationContext(ctx, resolverData, self, iterCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve input %q: %w", key, err)
		}
		inputs[key] = resolved
	}

	// Add context variables for provider access
	resolverData["__self"] = self
	resolverData["__item"] = iterCtx.Item
	resolverData["__index"] = iterCtx.Index
	if iterCtx.ItemAlias != "" {
		resolverData[iterCtx.ItemAlias] = iterCtx.Item
	}
	if iterCtx.IndexAlias != "" {
		resolverData[iterCtx.IndexAlias] = iterCtx.Index
	}

	ctxWithResolvers := provider.WithResolverContext(ctx, resolverData)

	// Check for write operations in resolver context
	if err := provider.ValidateWriteOperation(ctxWithResolvers, prov, inputs); err != nil {
		return nil, err
	}

	// Also pass the iteration context separately so providers can access aliases
	provIterCtx := &provider.IterationContext{
		Item:       iterCtx.Item,
		Index:      iterCtx.Index,
		ItemAlias:  iterCtx.ItemAlias,
		IndexAlias: iterCtx.IndexAlias,
	}
	ctxWithResolvers = provider.WithIterationContext(ctxWithResolvers, provIterCtx)

	output, err := prov.Execute(ctxWithResolvers, inputs)
	if err != nil {
		return nil, err
	}

	if output == nil {
		return nil, fmt.Errorf("provider returned nil output")
	}

	return output.Data, nil
}

// toSlice converts a value to []any if it's a slice or array type
func toSlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}

	// Handle []any directly
	if arr, ok := v.([]any); ok {
		return arr, true
	}

	// Handle []interface{} (sometimes returned by JSON unmarshal)
	if arr, ok := v.([]interface{}); ok {
		result := make([]any, len(arr))
		copy(result, arr)
		return result, true
	}

	// Use reflection for other slice types
	val := reflect.ValueOf(v)
	if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
		return nil, false
	}

	result := make([]any, val.Len())
	for i := 0; i < val.Len(); i++ {
		result[i] = val.Index(i).Interface()
	}
	return result, true
}

// executeValidatePhase executes all validation rules and aggregates failures.
// All validations run regardless of failures to provide comprehensive error reporting.
func (e *Executor) executeValidatePhase(ctx context.Context, resolverName string, sensitive bool, phase *ValidatePhase, value any) (int, error) {
	if phase == nil {
		return 0, nil
	}

	lgr := logger.FromContext(ctx)
	providerCallCount := 0

	// Get resolver context from context
	resolverCtx, _ := FromContext(ctx)

	// Check phase-level when condition (with __self so conditions can reference the resolved value)
	if phase.When != nil {
		shouldExecute, err := e.evaluateConditionWithSelf(ctx, phase.When, value)
		if err != nil {
			return providerCallCount, fmt.Errorf("failed to evaluate validate phase when condition: %w", err)
		}
		if !shouldExecute {
			lgr.V(1).Info("skipping validate phase due to when condition")
			return providerCallCount, nil
		}
	}

	// Create validation error to collect failures
	validationErr := &AggregatedValidationError{
		ResolverName: resolverName,
		Value:        value,
		Sensitive:    sensitive,
		Failures:     make([]ValidationFailure, 0),
	}

	// Run ALL validation rules and collect failures
	for i, validation := range phase.With {
		// Execute validation provider with __self set to value being validated
		_, err := e.executeProviderWithSelf(ctx, validation.Provider, validation.Inputs, value)
		providerCallCount++

		if err != nil {
			// Build failure message
			message := err.Error()

			// Use custom message if provided
			if validation.Message != nil {
				customMsg, msgErr := validation.Message.Resolve(ctx, resolverCtx.ToMap(), &value)
				if msgErr == nil {
					if msgStr, ok := customMsg.(string); ok {
						message = msgStr
					}
				}
			}

			// Redact message if sensitive
			if sensitive {
				message = "[REDACTED]"
			}

			failure := ValidationFailure{
				Rule:      i,
				Provider:  validation.Provider,
				Message:   message,
				Cause:     err,
				Sensitive: sensitive,
			}

			validationErr.AddFailure(failure)

			lgr.V(1).Info("validation rule failed",
				"rule", i+1,
				logKeyProvider, validation.Provider,
				"message", redactForLog(message, sensitive))
		}
	}

	// Return aggregated error if any failures occurred
	if validationErr.HasFailures() {
		return providerCallCount, validationErr
	}

	return providerCallCount, nil
}

// executeProvider executes a provider with resolved inputs
func (e *Executor) executeProvider(ctx context.Context, providerName string, inputRefs map[string]*ValueRef) (any, error) {
	return e.executeProviderWithSelf(ctx, providerName, inputRefs, nil)
}

// executeProviderWithSelf executes a provider with resolved inputs and __self set to the provided value
// If self is nil, __self will not be available during input resolution
func (e *Executor) executeProviderWithSelf(ctx context.Context, providerName string, inputRefs map[string]*ValueRef, self any) (any, error) {
	// Get resolver context from context
	resolverCtx, _ := FromContext(ctx)

	// Get provider from registry
	prov, err := e.registry.Get(providerName)
	if err != nil {
		return nil, fmt.Errorf("provider %q not found: %w", providerName, err)
	}

	// Resolve all inputs with __self if provided
	inputs := make(map[string]any)
	resolverData := resolverCtx.ToMap()

	for key, valueRef := range inputRefs {
		if valueRef == nil {
			return nil, fmt.Errorf("input %q has no value (nil); check for dangling YAML keys with no value", key)
		}
		resolved, err := valueRef.Resolve(ctx, resolverData, self)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve input %q: %w", key, err)
		}
		inputs[key] = resolved
	}

	// Add resolver data to context for provider access (including __self if present)
	if self != nil {
		resolverData["__self"] = self
	}
	ctxWithResolvers := provider.WithResolverContext(ctx, resolverData)

	// Check for write operations in resolver context
	if err := provider.ValidateWriteOperation(ctxWithResolvers, prov, inputs); err != nil {
		return nil, err
	}

	// Execute provider
	output, err := prov.Execute(ctxWithResolvers, inputs)
	if err != nil {
		return nil, err
	}

	if output == nil {
		return nil, fmt.Errorf("provider returned nil output")
	}

	return output.Data, nil
}

// calculateValueSize estimates the size of a value in bytes using JSON serialization
func calculateValueSize(value any) int64 {
	if value == nil {
		return 0
	}

	// Use json.Marshal as a rough estimate of size
	data, err := json.Marshal(value)
	if err != nil {
		return 0
	}
	return int64(len(data))
}

// trackFailedAttempt records a failed provider attempt in the current resolver's execution result
func (e *Executor) trackFailedAttempt(resolverCtx *Context, providerName, phase string, err error, duration time.Duration, onError string, sourceStep int) {
	// This will be called from within executeResolver's defer, so we need to get the current result
	// We'll add the attempt to a temporary storage that the defer can pick up
	// For now, we'll use a simple approach: store in context with a special key

	attempt := ProviderAttempt{
		Provider:   providerName,
		Phase:      phase,
		Error:      err.Error(),
		Duration:   duration,
		OnError:    onError,
		Timestamp:  time.Now(),
		SourceStep: sourceStep,
	}

	// Get the current resolver name from the logger context
	// Since we're in the execution flow, we can't easily access the resolver name here
	// Instead, we'll need to pass it down or use a context value
	// For now, we'll store it in a slice in the context that the defer will pick up

	// Try to get existing attempts
	var attempts []ProviderAttempt
	if existing, ok := resolverCtx.data.Load("__failed_attempts"); ok {
		if existingAttempts, ok := existing.([]ProviderAttempt); ok {
			attempts = existingAttempts
		}
	}

	// Append new attempt
	attempts = append(attempts, attempt)

	// Store back
	resolverCtx.data.Store("__failed_attempts", attempts)
}

// redactForLog returns [REDACTED] if sensitive is true, otherwise returns the original value
func redactForLog(value string, sensitive bool) string {
	if sensitive {
		return "[REDACTED]"
	}
	return value
}

// RedactValue returns [REDACTED] for any value if sensitive is true, otherwise returns the value unchanged.
// This is safe for use in logs, error messages, and JSON output.
func RedactValue(value any, sensitive bool) any {
	if !sensitive {
		return value
	}
	return "[REDACTED]"
}

// RedactError wraps an error with redaction if sensitive is true.
// The original error is preserved and can be accessed via errors.Unwrap.
func RedactError(err error, sensitive bool) error {
	if err == nil || !sensitive {
		return err
	}
	return NewRedactedError(err)
}

// RedactMapValues redacts all values in a map if sensitive is true.
// Keys are preserved, only values are replaced with [REDACTED].
func RedactMapValues(m map[string]any, sensitive bool) map[string]any {
	if !sensitive {
		return m
	}
	result := make(map[string]any, len(m))
	for k := range m {
		result[k] = "[REDACTED]"
	}
	return result
}

// resolveCustomErrorMessage evaluates a resolver's messages.error field and returns the
// resolved string, or "" if no custom message is configured or evaluation fails.
func resolveCustomErrorMessage(ctx context.Context, r *Resolver, originalErr error) string {
	if r.Messages == nil || r.Messages.Error == nil {
		return ""
	}

	resolverCtx, ok := FromContext(ctx)
	if !ok {
		return ""
	}

	resolverData := resolverCtx.ToMap()

	// Pass __error as the self parameter so it's available as __self in CEL/templates,
	// and also add it to resolverData so it's available as _.__error or _["__error"].
	errorMsg := originalErr.Error()
	resolverData["__error"] = errorMsg

	resolved, err := r.Messages.Error.Resolve(ctx, resolverData, errorMsg)
	if err != nil {
		return ""
	}

	if msg, ok := resolved.(string); ok {
		return msg
	}
	return fmt.Sprintf("%v", resolved)
}
