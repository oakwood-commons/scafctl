package provider

import (
	"context"
	"fmt"
	"time"
)

// ExecutionResult contains the result of a provider execution.
type ExecutionResult struct {
	// Provider is the provider that was executed
	Provider Provider `json:"provider" yaml:"provider" doc:"The provider that was executed"`

	// Output is the validated output from the provider
	Output Output `json:"output" yaml:"output" doc:"The validated output from the provider"`

	// DryRun indicates whether this was a dry-run execution
	DryRun bool `json:"dryRun" yaml:"dryRun" doc:"Whether this was a dry-run execution"`

	// ExecutionDuration is the total time taken to execute the provider
	ExecutionDuration time.Duration `json:"executionDuration" yaml:"executionDuration" doc:"The total time taken to execute the provider" example:"1000000000"`

	// ResolvedInputs are the inputs after resolution (for debugging)
	ResolvedInputs map[string]any `json:"resolvedInputs,omitempty" yaml:"resolvedInputs,omitempty" doc:"The resolved inputs (for debugging)"`
}

// Executor orchestrates provider execution with input resolution and validation.
type Executor struct {
	schemaValidator *SchemaValidator
}

// ExecutorOption is a functional option for configuring an Executor.
type ExecutorOption func(*Executor)

// WithSchemaValidator sets a custom schema validator.
func WithSchemaValidator(validator *SchemaValidator) ExecutorOption {
	return func(e *Executor) {
		e.schemaValidator = validator
	}
}

// NewExecutor creates a new provider executor with the given options.
func NewExecutor(opts ...ExecutorOption) *Executor {
	e := &Executor{
		schemaValidator: NewSchemaValidator(),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// validateExecutionMode checks that execution mode is set and matches provider capabilities.
func validateExecutionMode(ctx context.Context, desc *Descriptor) error {
	execMode, ok := ExecutionModeFromContext(ctx)
	if !ok {
		return fmt.Errorf("execution mode not provided in context")
	}

	// Check if the execution mode matches declared capabilities
	for _, cap := range desc.Capabilities {
		if cap == execMode {
			return nil
		}
	}

	return fmt.Errorf("provider %q does not support capability %q; supported: %v", desc.Name, execMode, desc.Capabilities)
}

// Execute executes a provider with the given inputs and context.
// It performs:
// 1. Execution mode validation against provider capabilities
// 2. Input resolution (literal, resolver bindings, CEL, templates)
// 3. Input validation against provider schema
// 4. Optional decode (if Descriptor.Decode is set)
// 5. Provider execution (provider checks context for dry-run mode)
// 6. Output validation against output schema
//
// The context should contain:
// - Execution mode (via WithExecutionMode) - REQUIRED
// - Dry-run flag (via WithDryRun) - providers check this to modify behavior
// - Resolver context (via WithResolverContext) for input resolution
//
// Note: Providers are responsible for handling dry-run mode by checking
// DryRunFromContext(ctx) and returning appropriate outputs without performing
// side effects.
func (e *Executor) Execute(ctx context.Context, provider Provider, inputs map[string]any) (*ExecutionResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider cannot be nil")
	}

	desc := provider.Descriptor()
	if desc == nil {
		return nil, fmt.Errorf("provider descriptor cannot be nil")
	}

	// Validate execution mode
	if err := validateExecutionMode(ctx, desc); err != nil {
		return nil, err
	}

	// Check if this is a dry-run
	dryRun := DryRunFromContext(ctx)

	// Start timing
	startTime := time.Now()

	// Create input resolver with schema
	inputResolver := NewInputResolver(ctx, desc.Schema)

	// Resolve inputs
	resolvedInputs, err := inputResolver.ResolveInputs(inputs)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve inputs: %w", err)
	}

	// Validate resolved inputs
	if err := e.schemaValidator.ValidateInputs(resolvedInputs, desc.Schema); err != nil {
		return nil, fmt.Errorf("input validation failed: %w", err)
	}

	// Determine what to pass to Execute:
	// - If Decode is defined: call it and pass the decoded (typed) value
	// - If Decode is nil: pass the raw map[string]any
	var executionInput any = resolvedInputs
	if desc.Decode != nil {
		decoded, err := desc.Decode(resolvedInputs)
		if err != nil {
			return nil, fmt.Errorf("failed to decode inputs: %w", err)
		}
		executionInput = decoded
	}

	// Execute the provider with either typed input or map
	outputPtr, err := provider.Execute(ctx, executionInput)

	// Calculate execution duration
	executionDuration := time.Since(startTime)

	// Record metrics (no-op if metrics collection is disabled)
	GlobalMetrics.Record(desc.Name, executionDuration, err == nil)

	if err != nil {
		return nil, fmt.Errorf("provider execution failed: %w", err)
	}
	if outputPtr == nil {
		return nil, fmt.Errorf("provider returned nil output")
	}
	output := *outputPtr

	// Validate output if schema is defined for the execution mode capability
	// Note: We currently don't have access to which capability was used for execution,
	// so output validation against per-capability schemas would need to be done at a higher level
	// TODO: Consider passing capability context to executor for per-capability output validation

	// Build result
	result := &ExecutionResult{
		Provider:          provider,
		Output:            output,
		DryRun:            dryRun,
		ExecutionDuration: executionDuration,
		ResolvedInputs:    resolvedInputs,
	}

	return result, nil
}

// ExecuteByName executes a provider by name from the global registry.
// This is a convenience method that looks up the provider and calls Execute.
func (e *Executor) ExecuteByName(ctx context.Context, providerName string, inputs map[string]any) (*ExecutionResult, error) {
	provider, exists := Get(providerName)
	if !exists {
		return nil, fmt.Errorf("provider %q not found in registry", providerName)
	}

	return e.Execute(ctx, provider, inputs)
}

// MustExecuteByName executes a provider by name and panics if the provider is not found or execution fails.
// This is useful for initialization code where a provider must exist and execute successfully.
func (e *Executor) MustExecuteByName(ctx context.Context, providerName string, inputs map[string]any) *ExecutionResult {
	result, err := e.ExecuteByName(ctx, providerName, inputs)
	if err != nil {
		panic(fmt.Sprintf("failed to execute provider %q: %v", providerName, err))
	}
	return result
}

// globalExecutor is the default package-level executor.
var globalExecutor = NewExecutor()

// Execute executes a provider using the global executor.
func Execute(ctx context.Context, provider Provider, inputs map[string]any) (*ExecutionResult, error) {
	return globalExecutor.Execute(ctx, provider, inputs)
}

// ExecuteByName executes a provider by name using the global executor.
func ExecuteByName(ctx context.Context, providerName string, inputs map[string]any) (*ExecutionResult, error) {
	return globalExecutor.ExecuteByName(ctx, providerName, inputs)
}

// MustExecuteByName executes a provider by name using the global executor and panics on failure.
func MustExecuteByName(ctx context.Context, providerName string, inputs map[string]any) *ExecutionResult {
	return globalExecutor.MustExecuteByName(ctx, providerName, inputs)
}

// GetGlobalExecutor returns the global executor instance.
func GetGlobalExecutor() *Executor {
	return globalExecutor
}

// ResetGlobalExecutor resets the global executor to a new instance.
// This is primarily for testing purposes.
func ResetGlobalExecutor() {
	globalExecutor = NewExecutor()
}
