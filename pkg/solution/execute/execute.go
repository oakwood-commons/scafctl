// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package execute provides business logic for validating and executing solutions.
// This package is the shared domain layer used by CLI, MCP, and future API consumers.
package execute

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// SolutionValidationResult holds the structured results of validating a solution.
type SolutionValidationResult struct {
	// Valid is true when the solution passes all validation checks.
	Valid bool `json:"valid" yaml:"valid" doc:"Whether the solution is valid"`

	// HasResolvers indicates whether the solution defines resolvers.
	HasResolvers bool `json:"hasResolvers" yaml:"hasResolvers" doc:"Whether the solution has resolvers"`

	// HasWorkflow indicates whether the solution defines an action workflow.
	HasWorkflow bool `json:"hasWorkflow" yaml:"hasWorkflow" doc:"Whether the solution has a workflow"`

	// Errors contains any validation errors found.
	Errors []string `json:"errors,omitempty" yaml:"errors,omitempty" doc:"Validation errors"`
}

// ValidateSolution validates a loaded solution and its workflow against the
// given provider registry. This standalone function can be called from both
// the CLI and the MCP server without requiring CLI-specific types.
func ValidateSolution(_ context.Context, sol *solution.Solution, reg ProviderLookup) *SolutionValidationResult {
	result := &SolutionValidationResult{
		Valid:        true,
		HasResolvers: sol.Spec.HasResolvers(),
		HasWorkflow:  sol.Spec.HasWorkflow(),
	}

	// Validate workflow if present
	if sol.Spec.HasWorkflow() {
		if err := action.ValidateWorkflow(sol.Spec.Workflow, reg); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("workflow validation: %s", err))
		}
	}

	return result
}

// ---------------------------------------------------------------------------
// Resolver Execution
// ---------------------------------------------------------------------------

// ResolverExecutionConfig holds resolver execution parameters decoupled from CLI types.
// This allows the MCP server to configure resolver execution without constructing
// fake CLI scaffolding (IOStreams, flag sets, etc.).
type ResolverExecutionConfig struct {
	// Timeout is the default timeout per resolver.
	Timeout time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Default timeout per resolver"`

	// PhaseTimeout is the timeout for each execution phase.
	PhaseTimeout time.Duration `json:"phaseTimeout,omitempty" yaml:"phaseTimeout,omitempty" doc:"Timeout for each execution phase"`

	// MaxConcurrency limits concurrent resolver execution (0=unlimited).
	MaxConcurrency int `json:"maxConcurrency,omitempty" yaml:"maxConcurrency,omitempty" doc:"Maximum concurrent resolvers"`

	// WarnValueSize triggers a warning when resolver values exceed this size in bytes.
	WarnValueSize int64 `json:"warnValueSize,omitempty" yaml:"warnValueSize,omitempty" doc:"Warn when value exceeds this size"`

	// MaxValueSize rejects resolver values exceeding this size in bytes.
	MaxValueSize int64 `json:"maxValueSize,omitempty" yaml:"maxValueSize,omitempty" doc:"Reject values exceeding this size"`

	// ValidateAll validates all resolvers even if some fail.
	ValidateAll bool `json:"validateAll,omitempty" yaml:"validateAll,omitempty" doc:"Validate all resolvers even on failure"`

	// SkipValidation skips resolver validation.
	SkipValidation bool `json:"skipValidation,omitempty" yaml:"skipValidation,omitempty" doc:"Skip resolver validation"`

	// SkipTransform skips resolver transforms.
	SkipTransform bool `json:"skipTransform,omitempty" yaml:"skipTransform,omitempty" doc:"Skip resolver transforms"`

	// NonFatalValidation, when true, treats resolver validation-only failures
	// as non-fatal: the populated values are returned alongside structured
	// diagnostics (ResolverExecutionResult.Diagnostics) instead of aborting
	// with an error. Resolve- and transform-phase failures (where no value
	// could be produced) remain fatal. This is used by inspection and
	// troubleshooting paths (run resolver, preview_resolvers) so that resolver
	// output remains useful even when validation fails. It implies validate-all
	// semantics so that all reachable resolvers populate their values.
	NonFatalValidation bool `json:"nonFatalValidation,omitempty" yaml:"nonFatalValidation,omitempty" doc:"Treat validation failures as non-fatal and return partial values with diagnostics"`
}

// ResolverExecutionResult holds the structured output of resolver execution.
type ResolverExecutionResult struct {
	// Data contains the resolved values keyed by resolver name. When
	// NonFatalValidation is enabled, this also includes partial values produced
	// by resolvers that failed validation (their value is captured post-transform
	// before the validate phase rejects it).
	Data map[string]any `json:"data" yaml:"data" doc:"Resolved values"`

	// Context is the resolver execution context with full metadata.
	Context *resolver.Context `json:"-" yaml:"-"`

	// Diagnostics holds the resolver validation or execution error collected
	// when NonFatalValidation is enabled. It is nil when execution was clean.
	// Use DiagnosticsFromError to convert it into a structured list.
	Diagnostics error `json:"-" yaml:"-"`
}

// ResolverDiagnostic describes a single resolver validation or execution
// failure surfaced during a non-fatal inspection run.
type ResolverDiagnostic struct {
	// Resolver is the name of the resolver that failed. It may be empty when
	// the failure is not attributable to a specific resolver.
	Resolver string `json:"resolver,omitempty" yaml:"resolver,omitempty" doc:"Name of the resolver that failed"`

	// Phase is the execution phase number where the failure occurred (0 when unknown).
	Phase int `json:"phase,omitempty" yaml:"phase,omitempty" doc:"Phase number where the failure occurred"`

	// Message is the human-readable failure description.
	Message string `json:"message" yaml:"message" doc:"Human-readable failure message"`
}

// DiagnosticsFromError converts a resolver execution error into a structured
// list of per-resolver diagnostics. It understands AggregatedExecutionError
// (validate-all mode) and AggregatedValidationError, and falls back to a single
// generic diagnostic for any other error. Returns nil when err is nil.
func DiagnosticsFromError(err error) []ResolverDiagnostic {
	if err == nil {
		return nil
	}

	var aggExec *resolver.AggregatedExecutionError
	if errors.As(err, &aggExec) {
		diags := make([]ResolverDiagnostic, 0, len(aggExec.Errors))
		for _, fr := range aggExec.Errors {
			if fr == nil {
				continue
			}
			msg := fr.ErrMessage
			if msg == "" && fr.Err != nil {
				msg = fr.Err.Error()
			}
			diags = append(diags, ResolverDiagnostic{
				Resolver: fr.ResolverName,
				Phase:    fr.Phase,
				Message:  msg,
			})
		}
		return diags
	}

	var aggVal *resolver.AggregatedValidationError
	if errors.As(err, &aggVal) {
		return []ResolverDiagnostic{{
			Resolver: aggVal.ResolverName,
			Message:  aggVal.Error(),
		}}
	}

	return []ResolverDiagnostic{{Message: err.Error()}}
}

// Reserved output keys and status values used to surface a structured failure
// envelope in machine-readable output formats (json/yaml). The DiagnosticsKey
// and StatusKey are double-underscore-prefixed so they never collide with a
// user-defined resolver name in the resolver output map, mirroring the existing
// __execution convention.
const (
	// DiagnosticsKey is the reserved key under which a []ResolverDiagnostic list
	// is attached to the resolver output map on failure.
	DiagnosticsKey = "__diagnostics"

	// StatusKey is the reserved key under which the run status ("failed") is
	// attached to the resolver output map on failure.
	StatusKey = "__status"

	// StatusFieldKey is the plain (non-underscore) status key used by the
	// action/solution failure envelope, matching action.BuildOutputData's
	// "status" field.
	StatusFieldKey = "status"

	// DiagnosticsFieldKey is the plain (non-underscore) diagnostics key used by
	// the action/solution failure envelope, matching its sibling keys
	// ("failedActions", "skippedActions").
	DiagnosticsFieldKey = "diagnostics"

	// ResolversFieldKey is the plain (non-underscore) key under which the
	// resolved (and partially-resolved) resolver values are attached to the
	// action/solution failure envelope so successfully-resolved values are
	// never dropped when a run aborts.
	ResolversFieldKey = "resolvers"

	// StatusFailed is the status value written when a run fails.
	StatusFailed = "failed"
)

// InjectResolverFailureEnvelope attaches a structured failure envelope to a
// resolver output map so machine-readable output (json/yaml) remains parseable
// even when the run fails. It sets StatusKey to StatusFailed and DiagnosticsKey
// to the structured diagnostics derived from err. A nil results map is created;
// a nil err is a no-op (the map is returned unchanged). Existing resolved values
// in the map are preserved so partial results stay inspectable.
func InjectResolverFailureEnvelope(results map[string]any, err error) map[string]any {
	if err == nil {
		return results
	}
	if results == nil {
		results = make(map[string]any)
	}
	results[StatusKey] = StatusFailed
	results[DiagnosticsKey] = DiagnosticsFromError(err)
	return results
}

// BuildFailureEnvelope builds a standalone structured failure envelope for
// commands whose output is a fixed-schema map (run solution / run action) rather
// than a bare resolver map. It returns {status: "failed", diagnostics: [...]}
// keyed to match action.BuildOutputData. When resolverData is non-empty, a
// "resolvers" key carrying the resolved (and partially-resolved) values is
// added so successfully-resolved values are never dropped when the run aborts.
// Returns nil when err is nil.
func BuildFailureEnvelope(err error, resolverData map[string]any) map[string]any {
	if err == nil {
		return nil
	}
	envelope := map[string]any{
		StatusFieldKey:      StatusFailed,
		DiagnosticsFieldKey: DiagnosticsFromError(err),
	}
	if len(resolverData) > 0 {
		envelope[ResolversFieldKey] = resolverData
	}
	return envelope
}

// InjectSolutionDiagnostics attaches non-fatal validation diagnostics (and the
// resolved values) to an otherwise-successful action/solution output envelope.
// It is used under the "warn" validation policy, where a validation-only
// failure does not abort the run: the workflow still executes, but the
// diagnostics and resolved values are surfaced in the structured output so the
// warning is visible to machine consumers. A nil err or nil envelope is a
// no-op. Existing keys in the envelope are preserved.
func InjectSolutionDiagnostics(envelope map[string]any, err error, resolverData map[string]any) map[string]any {
	if envelope == nil || err == nil {
		return envelope
	}
	envelope[DiagnosticsFieldKey] = DiagnosticsFromError(err)
	if len(resolverData) > 0 {
		envelope[ResolversFieldKey] = resolverData
	}
	return envelope
}

// isValidationFailure reports whether a single resolver failure originated from
// the validate phase. Resolve- and transform-phase failures (where no value
// could be produced) return false so callers keep treating them as fatal.
func isValidationFailure(err error) bool {
	if err == nil {
		return false
	}
	var aggVal *resolver.AggregatedValidationError
	if errors.As(err, &aggVal) {
		return true
	}
	var execErr *resolver.ExecutionError
	if errors.As(err, &execErr) {
		return execErr.Phase == "validate"
	}
	return false
}

// IsValidationOnlyFailure reports whether every failure contained in err is a
// validation failure. It returns false when err contains any non-validation
// failure (such as a resolve- or transform-phase error) so those remain fatal.
// Non-fatal inspection mode only suppresses pure validation failures.
func IsValidationOnlyFailure(err error) bool {
	if err == nil {
		return false
	}
	var aggExec *resolver.AggregatedExecutionError
	if errors.As(err, &aggExec) {
		if len(aggExec.Errors) == 0 {
			return false
		}
		sawFailure := false
		for _, fr := range aggExec.Errors {
			if fr == nil {
				continue
			}
			sawFailure = true
			if !isValidationFailure(fr.Err) {
				return false
			}
		}
		// If every entry was nil there is no concrete failure to classify, so
		// do not treat the aggregated error as validation-only.
		return sawFailure
	}
	return isValidationFailure(err)
}

// ProviderGetter looks up providers by name.
type ProviderGetter interface {
	// Get returns a provider by name.
	Get(name string) (provider.Provider, bool)
	// Has reports whether a provider with the given name exists.
	Has(name string) bool
}

// DescriptorLookuper exposes provider descriptor lookup.
type DescriptorLookuper interface {
	// DescriptorLookup returns a function that looks up provider descriptors by name.
	DescriptorLookup() provider.DescriptorLookup
}

// ProviderLookup is the minimal provider registry surface required to execute
// resolvers. It matches resolver.RegistryInterface so a registry can be passed
// directly to the resolver executor without wrapping it in an adapter.
type ProviderLookup interface {
	ProviderGetter
	DescriptorLookuper
}

// Resolvers runs the resolver execution pipeline on the given solution.
// This standalone function decouples resolver execution from CLI-specific types
// (IOStreams, progress bars, output formatting). The MCP server uses this to
// execute resolvers and return structured results.
func Resolvers(
	ctx context.Context,
	sol *solution.Solution,
	params map[string]any,
	registry ProviderLookup,
	cfg ResolverExecutionConfig,
) (*ResolverExecutionResult, error) {
	lgr := logger.FromContext(ctx)

	// Attach solution metadata to the context so providers (e.g., metadata) can access it.
	ctx = provider.WithSolutionMetadata(ctx, toSolutionMeta(sol))

	resolvers := sol.Spec.ResolversToSlice()
	resolverData := make(map[string]any)

	if len(resolvers) == 0 {
		if lgr != nil {
			lgr.V(0).Info("no resolvers to execute")
		}
		return &ResolverExecutionResult{
			Data:    resolverData,
			Context: resolver.NewContext(),
		}, nil
	}

	// Build executor options from config
	executorOpts := []resolver.ExecutorOption{
		resolver.WithDefaultTimeout(cfg.Timeout),
		resolver.WithPhaseTimeout(cfg.PhaseTimeout),
	}
	if cfg.MaxConcurrency > 0 {
		executorOpts = append(executorOpts, resolver.WithMaxConcurrency(cfg.MaxConcurrency))
	}
	if cfg.WarnValueSize > 0 {
		executorOpts = append(executorOpts, resolver.WithWarnValueSize(cfg.WarnValueSize))
	}
	if cfg.MaxValueSize > 0 {
		executorOpts = append(executorOpts, resolver.WithMaxValueSize(cfg.MaxValueSize))
	}
	if cfg.ValidateAll {
		executorOpts = append(executorOpts, resolver.WithValidateAll(true))
	}
	// Non-fatal validation continues past failures and collects all errors like
	// validate-all, but a resolver that fails only a validation rule still
	// produces a usable value, so its dependents keep running and can read it.
	if cfg.NonFatalValidation {
		executorOpts = append(executorOpts, resolver.WithNonFatalValidation(true))
	}
	if cfg.SkipValidation {
		executorOpts = append(executorOpts, resolver.WithSkipValidation(true))
	}
	if cfg.SkipTransform {
		executorOpts = append(executorOpts, resolver.WithSkipTransform(true))
	}
	if sol.Spec.HasCalls() {
		executorOpts = append(executorOpts, resolver.WithCalls(sol.Spec.Calls))
	}
	if binder, binderErr := sol.TemplateFuncBinder(); binderErr != nil {
		return nil, fmt.Errorf("compiling spec.functions: %w", binderErr)
	} else if binder != nil {
		executorOpts = append(executorOpts, resolver.WithTemplateFuncBinder(binder))
	}
	executor := resolver.NewExecutor(registry, executorOpts...)

	// Apply solution-level CEL cost limit if configured
	ctx = ApplySolutionCELCostLimit(ctx, sol)

	// Execute resolvers
	resultCtx, execErr := executor.Execute(ctx, resolvers, params)

	// Get resolver context with results. This is populated even on failure, so
	// retrieve it before deciding how to handle execErr.
	resolverCtx, ok := resolver.FromContext(resultCtx)
	if !ok {
		// Execution failed before a resolver context could be established
		// (e.g., phase build failure). Surface the underlying error.
		if execErr != nil {
			return nil, fmt.Errorf("resolver execution failed: %w", execErr)
		}
		return nil, fmt.Errorf("failed to retrieve resolver results")
	}

	// Build resolver data map. In non-fatal mode, also include partial values
	// captured by resolvers that failed validation so callers can inspect them.
	for name := range sol.Spec.Resolvers {
		result, ok := resolverCtx.GetResult(name)
		if !ok {
			continue
		}
		if result.Status == resolver.ExecutionStatusSuccess {
			resolverData[name] = result.Value
		} else if cfg.NonFatalValidation && result.Value != nil {
			resolverData[name] = result.Value
		}
	}

	if execErr != nil {
		if cfg.NonFatalValidation {
			// Non-fatal: return the populated values plus diagnostics instead of
			// aborting, so the inspection path remains useful for ALL failure
			// kinds. Callers classify the error with IsValidationOnlyFailure to
			// decide status/exit behavior: validation-only failures are a soft
			// gate, while resolve/transform failures are a hard failure -- but
			// the partial values stay inspectable either way.
			if lgr != nil {
				lgr.V(1).Info("resolver execution completed with diagnostics",
					"resolvedCount", len(resolverData))
			}
			return &ResolverExecutionResult{
				Data:        resolverData,
				Context:     resolverCtx,
				Diagnostics: execErr,
			}, nil
		}
		return nil, fmt.Errorf("resolver execution failed: %w", execErr)
	}

	if lgr != nil {
		lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
	}

	return &ResolverExecutionResult{
		Data:    resolverData,
		Context: resolverCtx,
	}, nil
}

// ResolversForPreview is a convenience wrapper over Resolvers that returns
// only the resolved data map. It initialises a default builtin registry when
// registry is nil and reads the execution config from context. This is the
// shared entry point for preview/render operations in both the MCP server
// and the CLI.
func ResolversForPreview(
	ctx context.Context,
	sol *solution.Solution,
	params map[string]any,
	registry ProviderLookup,
) (map[string]any, error) {
	if !sol.Spec.HasResolvers() {
		return make(map[string]any), nil
	}

	if registry == nil {
		reg, err := builtin.DefaultRegistry(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider registry: %w", err)
		}
		registry = reg
	}

	cfg := ResolverExecutionConfigFromContext(ctx)
	result, err := Resolvers(ctx, sol, params, registry, cfg)
	if err != nil {
		return nil, err
	}

	return result.Data, nil
}

// ResolverExecutionConfigFromContext creates a ResolverExecutionConfig from the
// application config stored in context, providing sensible defaults.
func ResolverExecutionConfigFromContext(ctx context.Context) ResolverExecutionConfig {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return ResolverExecutionConfig{
			Timeout:      30 * time.Second,
			PhaseTimeout: 5 * time.Minute,
		}
	}

	values, err := cfg.Resolver.ToResolverValues()
	if err != nil {
		return ResolverExecutionConfig{
			Timeout:      30 * time.Second,
			PhaseTimeout: 5 * time.Minute,
		}
	}

	return ResolverExecutionConfig{
		Timeout:        values.Timeout,
		PhaseTimeout:   values.PhaseTimeout,
		MaxConcurrency: values.MaxConcurrency,
		WarnValueSize:  values.WarnValueSize,
		MaxValueSize:   values.MaxValueSize,
		ValidateAll:    values.ValidateAll,
	}
}

// ---------------------------------------------------------------------------
// Action Execution
// ---------------------------------------------------------------------------

// ActionExecutionConfig holds action execution parameters decoupled from CLI types.
// This allows the MCP server and other consumers to configure action execution
// without constructing CLI-specific scaffolding (IOStreams, flag sets, etc.).
type ActionExecutionConfig struct {
	// DefaultTimeout is the default timeout per action execution.
	DefaultTimeout time.Duration `json:"defaultTimeout,omitempty" yaml:"defaultTimeout,omitempty" doc:"Default per-action execution timeout"`

	// GracePeriod is the cancellation grace period.
	GracePeriod time.Duration `json:"gracePeriod,omitempty" yaml:"gracePeriod,omitempty" doc:"Cancellation grace period"`

	// MaxConcurrency limits concurrent action execution (0=unlimited).
	MaxConcurrency int `json:"maxConcurrency,omitempty" yaml:"maxConcurrency,omitempty" doc:"Maximum concurrent actions" maximum:"1000"`

	// OutputDir is the target directory for action output. When set, providers
	// in action mode resolve relative paths against this directory instead of CWD.
	// An empty string means actions use CWD (backward-compatible default).
	OutputDir string `json:"outputDir,omitempty" yaml:"outputDir,omitempty" doc:"Target directory for action output" maxLength:"4096"`

	// Cwd is the original working directory to expose as __cwd in action
	// expressions. When empty, the executor captures os.Getwd() at creation time.
	// Callers that change the working directory before creating the executor
	// (e.g., bundle extraction) should set this explicitly.
	Cwd string `json:"-" yaml:"-"`
}

// ActionExecutionResult wraps the action executor result with additional metadata.
type ActionExecutionResult struct {
	// Result is the underlying action execution result.
	Result *action.ExecutionResult `json:"result" yaml:"result" doc:"Action execution result"`
}

// Actions runs the action execution pipeline on the given solution.
// It accepts pre-resolved data from a prior resolver execution and a provider
// registry. When cfg.OutputDir is set, providers executing in action mode
// resolve relative paths against that directory instead of CWD.
//
// NOTE: This function performs real execution including filesystem changes
// (e.g. creating OutputDir). For dry-run semantics, callers should use
// dryrun.Generate instead — it builds the action graph and generates WhatIf
// descriptions without invoking providers or creating directories.
func Actions(
	ctx context.Context,
	sol *solution.Solution,
	resolverData map[string]any,
	registry ProviderGetter,
	cfg ActionExecutionConfig,
) (*ActionExecutionResult, error) {
	lgr := logger.FromContext(ctx)

	if !sol.Spec.HasWorkflow() {
		return nil, fmt.Errorf("solution %q has no workflow defined", sol.Metadata.Name)
	}

	// Validate the workflow
	if err := action.ValidateWorkflow(sol.Spec.Workflow, registry); err != nil {
		return nil, fmt.Errorf("workflow validation failed: %w", err)
	}

	// Attach solution metadata to the context.
	ctx = provider.WithSolutionMetadata(ctx, toSolutionMeta(sol))

	// Apply solution-level CEL cost limit if configured
	ctx = ApplySolutionCELCostLimit(ctx, sol)

	// When OutputDir is set, resolve to an absolute path and inject it into
	// the context for action-mode providers.
	if cfg.OutputDir != "" {
		absDir, err := provider.AbsFromContext(ctx, cfg.OutputDir)
		if err != nil {
			return nil, fmt.Errorf("resolving output directory: %w", err)
		}
		if err := os.MkdirAll(absDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating output directory: %w", err)
		}
		ctx = provider.WithOutputDirectory(ctx, absDir)
	}

	// Build executor options from config
	executorOpts := []action.ExecutorOption{
		action.WithRegistry(registry),
		action.WithResolverData(resolverData),
		action.WithDefaultTimeout(cfg.DefaultTimeout),
		action.WithGracePeriod(cfg.GracePeriod),
	}
	if cfg.MaxConcurrency > 0 {
		executorOpts = append(executorOpts, action.WithMaxConcurrency(cfg.MaxConcurrency))
	}
	if cfg.Cwd != "" {
		executorOpts = append(executorOpts, action.WithCwd(cfg.Cwd))
	} else if cwd, ok := provider.WorkingDirectoryFromContext(ctx); ok && cwd != "" {
		executorOpts = append(executorOpts, action.WithCwd(cwd))
	}
	if sol.Spec.HasCalls() {
		executorOpts = append(executorOpts, action.WithCalls(sol.Spec.Calls))
	}

	executor := action.NewExecutor(executorOpts...)

	if lgr != nil {
		lgr.V(1).Info("executing actions",
			"actionCount", len(sol.Spec.Workflow.Actions),
			"outputDir", cfg.OutputDir)
	}

	result, err := executor.Execute(ctx, sol.Spec.Workflow)
	if err != nil && result != nil && result.FinalStatus != action.ExecutionPartialSuccess {
		return nil, fmt.Errorf("action execution failed: %w", err)
	}

	return &ActionExecutionResult{
		Result: result,
	}, nil
}

// ActionExecutionConfigFromContext creates an ActionExecutionConfig from the
// application config stored in context, providing sensible defaults.
func ActionExecutionConfigFromContext(ctx context.Context) ActionExecutionConfig {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return ActionExecutionConfig{
			DefaultTimeout: settings.DefaultActionTimeout,
			GracePeriod:    settings.DefaultGracePeriod,
		}
	}

	values, err := cfg.Action.ToActionValues()
	if err != nil {
		return ActionExecutionConfig{
			DefaultTimeout: settings.DefaultActionTimeout,
			GracePeriod:    settings.DefaultGracePeriod,
		}
	}

	return ActionExecutionConfig{
		DefaultTimeout: values.DefaultTimeout,
		GracePeriod:    values.GracePeriod,
		MaxConcurrency: values.MaxConcurrency,
		OutputDir:      values.OutputDir,
	}
}

// ---------------------------------------------------------------------------
// Adapter: action.RegistryInterface
// ---------------------------------------------------------------------------

// toSolutionMeta converts a solution's metadata into the provider-package SolutionMeta type.
func toSolutionMeta(sol *solution.Solution) *provider.SolutionMeta {
	meta := &provider.SolutionMeta{
		Name:        sol.Metadata.Name,
		DisplayName: sol.Metadata.DisplayName,
		Description: sol.Metadata.Description,
		Category:    sol.Metadata.Category,
		Tags:        sol.Metadata.Tags,
		Source:      sol.Provenance(),
	}
	if sol.Metadata.Version != nil {
		meta.Version = sol.Metadata.Version.String()
	}
	return meta
}

// ApplySolutionCELCostLimit injects the solution's CEL cost limit into the context
// if the solution defines one. The effective limit is min(solutionLimit, globalLimit)
// so that solutions cannot raise the limit above the operator-configured global default.
// When global limiting is disabled (globalLimit == 0), solution overrides are ignored.
func ApplySolutionCELCostLimit(ctx context.Context, sol *solution.Solution) context.Context {
	if sol.Spec.Options == nil || sol.Spec.Options.CEL == nil || sol.Spec.Options.CEL.CostLimit == nil {
		return ctx
	}

	solutionLimit := *sol.Spec.Options.CEL.CostLimit
	if solutionLimit == 0 {
		return ctx
	}

	globalLimit := celexp.GetDefaultCostLimit()
	if globalLimit == 0 {
		// Global limiting is disabled; don't let a solution impose one.
		return ctx
	}

	effectiveLimit := solutionLimit
	if solutionLimit > globalLimit {
		effectiveLimit = globalLimit
	}

	lgr := logger.FromContext(ctx)
	lgr.V(1).Info("applying solution-level CEL cost limit",
		"solutionLimit", solutionLimit,
		"globalLimit", globalLimit,
		"effectiveLimit", effectiveLimit,
	)

	return celexp.ContextWithCostLimit(ctx, effectiveLimit)
}
