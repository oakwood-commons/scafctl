package provider

import (
	"context"
	"fmt"
)

// ExecutionResult contains the result of a provider execution.
type ExecutionResult struct {
	// Provider is the provider that was executed
	Provider Provider `json:"provider" yaml:"provider" doc:"The provider that was executed"`

	// Output is the validated output from the provider
	Output Output `json:"output" yaml:"output" doc:"The validated output from the provider"`

	// DryRun indicates whether this was a dry-run execution
	DryRun bool `json:"dryRun" yaml:"dryRun" doc:"Whether this was a dry-run execution"`

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

// Execute executes a provider with the given inputs and context.
// It performs:
// 1. Input resolution (literal, resolver bindings, CEL, templates)
// 2. Input validation against provider schema
// 3. Provider execution (provider checks context for dry-run mode)
// 4. Output validation against output schema
//
// The context should contain:
// - Execution mode (via WithExecutionMode)
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

	// Check if this is a dry-run
	dryRun := DryRunFromContext(ctx)

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

	// Execute the provider (it will handle dry-run mode via context)
	outputPtr, err := provider.Execute(ctx, resolvedInputs)
	if err != nil {
		return nil, fmt.Errorf("provider execution failed: %w", err)
	}
	if outputPtr == nil {
		return nil, fmt.Errorf("provider returned nil output")
	}
	output := *outputPtr

	// Validate output if schema is defined
	if desc.OutputSchema.Properties != nil {
		if err := e.schemaValidator.ValidateOutput(output.Data, desc.OutputSchema); err != nil {
			return nil, fmt.Errorf("output validation failed: %w", err)
		}
	}

	// Build result
	result := &ExecutionResult{
		Provider:       provider,
		Output:         output,
		DryRun:         dryRun,
		ResolvedInputs: resolvedInputs,
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
