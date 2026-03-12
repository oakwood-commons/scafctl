// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package execute provides business logic for validating and executing solutions.
// This package is the shared domain layer used by CLI, MCP, and future API consumers.
package execute

import (
	"context"
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
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
func ValidateSolution(_ context.Context, sol *solution.Solution, reg *provider.Registry) *SolutionValidationResult {
	result := &SolutionValidationResult{
		Valid:        true,
		HasResolvers: sol.Spec.HasResolvers(),
		HasWorkflow:  sol.Spec.HasWorkflow(),
	}

	// Validate workflow if present
	if sol.Spec.HasWorkflow() {
		adapter := &actionRegistryAdapter{registry: reg}
		if err := action.ValidateWorkflow(sol.Spec.Workflow, adapter); err != nil {
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

	// DryRun enables dry-run mode: providers return mock/no-op outputs
	// instead of performing real side effects.
	DryRun bool `json:"dryRun,omitempty" yaml:"dryRun,omitempty" doc:"Enable dry-run mode (providers return mock outputs)"`
}

// ResolverExecutionResult holds the structured output of resolver execution.
type ResolverExecutionResult struct {
	// Data contains the resolved values keyed by resolver name.
	Data map[string]any `json:"data" yaml:"data" doc:"Resolved values"`

	// Context is the resolver execution context with full metadata.
	// Only available when execution succeeds.
	Context *resolver.Context `json:"-" yaml:"-"`
}

// Resolvers runs the resolver execution pipeline on the given solution.
// This standalone function decouples resolver execution from CLI-specific types
// (IOStreams, progress bars, output formatting). The MCP server uses this to
// execute resolvers and return structured results.
func Resolvers(
	ctx context.Context,
	sol *solution.Solution,
	params map[string]any,
	reg *provider.Registry,
	cfg ResolverExecutionConfig,
) (*ResolverExecutionResult, error) {
	lgr := logger.FromContext(ctx)

	// Enable dry-run mode on the context when requested.
	if cfg.DryRun {
		ctx = provider.WithDryRun(ctx, true)
	}

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

	adapter := NewResolverRegistryAdapter(reg)

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
	if cfg.SkipValidation {
		executorOpts = append(executorOpts, resolver.WithSkipValidation(true))
	}
	if cfg.SkipTransform {
		executorOpts = append(executorOpts, resolver.WithSkipTransform(true))
	}
	executor := resolver.NewExecutor(adapter, executorOpts...)

	// Execute resolvers
	resultCtx, err := executor.Execute(ctx, resolvers, params)
	if err != nil {
		return nil, fmt.Errorf("resolver execution failed: %w", err)
	}

	// Get resolver context with results
	resolverCtx, ok := resolver.FromContext(resultCtx)
	if !ok {
		return nil, fmt.Errorf("failed to retrieve resolver results")
	}

	// Build resolver data map
	for name := range sol.Spec.Resolvers {
		result, ok := resolverCtx.GetResult(name)
		if ok && result.Status == resolver.ExecutionStatusSuccess {
			resolverData[name] = result.Value
		}
	}

	if lgr != nil {
		lgr.V(1).Info("resolver execution complete", "resolvedCount", len(resolverData))
	}

	return &ResolverExecutionResult{
		Data:    resolverData,
		Context: resolverCtx,
	}, nil
}

// ResolversForPreview is a convenience wrapper over Resolvers that accepts
// a provider.Registry directly and returns only the resolved data map.
// It initialises a default registry when reg is nil and reads the execution
// config from context. This is the shared entry point for preview/render
// operations in both the MCP server and the CLI.
func ResolversForPreview(
	ctx context.Context,
	sol *solution.Solution,
	params map[string]any,
	reg *provider.Registry,
) (map[string]any, error) {
	if !sol.Spec.HasResolvers() {
		return make(map[string]any), nil
	}

	if reg == nil {
		var err error
		reg, err = builtin.DefaultRegistry(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to create provider registry: %w", err)
		}
	}

	cfg := ResolverExecutionConfigFromContext(ctx)
	result, err := Resolvers(ctx, sol, params, reg, cfg)
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
// Adapter: action.RegistryInterface
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Adapter: resolver.RegistryInterface
// ---------------------------------------------------------------------------

// ResolverRegistryAdapter adapts provider.Registry to resolver.RegistryInterface.
type ResolverRegistryAdapter struct {
	registry *provider.Registry
}

// NewResolverRegistryAdapter creates a new ResolverRegistryAdapter wrapping
// the given provider.Registry.
func NewResolverRegistryAdapter(registry *provider.Registry) *ResolverRegistryAdapter {
	return &ResolverRegistryAdapter{registry: registry}
}

func (r *ResolverRegistryAdapter) Register(p provider.Provider) error {
	return r.registry.Register(p)
}

func (r *ResolverRegistryAdapter) Get(name string) (provider.Provider, error) {
	p, ok := r.registry.Get(name)
	if !ok {
		return nil, fmt.Errorf("provider %s not found", name)
	}
	return p, nil
}

func (r *ResolverRegistryAdapter) List() []provider.Provider {
	return r.registry.ListProviders()
}

func (r *ResolverRegistryAdapter) DescriptorLookup() resolver.DescriptorLookup {
	return r.registry.DescriptorLookup()
}

// toSolutionMeta converts a solution's metadata into the provider-package SolutionMeta type.
func toSolutionMeta(sol *solution.Solution) *provider.SolutionMeta {
	meta := &provider.SolutionMeta{
		Name:        sol.Metadata.Name,
		DisplayName: sol.Metadata.DisplayName,
		Description: sol.Metadata.Description,
		Category:    sol.Metadata.Category,
		Tags:        sol.Metadata.Tags,
	}
	if sol.Metadata.Version != nil {
		meta.Version = sol.Metadata.Version.String()
	}
	return meta
}
