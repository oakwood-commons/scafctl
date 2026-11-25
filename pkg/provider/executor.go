// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/oakwood-commons/scafctl/pkg/redact"
	"github.com/oakwood-commons/scafctl/pkg/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
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
	for _, cp := range desc.Capabilities {
		if cp == execMode {
			return nil
		}
	}

	return fmt.Errorf("provider %q does not support capability %q; supported: %v", desc.Name, execMode, desc.Capabilities)
}

// isReadOnlyResolverMode reports whether the execution mode is a read-only
// resolver context in which write operations must be blocked. All other modes
// (action, state, kubeconfig, authentication) legitimately perform writes and
// are exempt from the write-operation guard.
func isReadOnlyResolverMode(mode Capability) bool {
	switch mode {
	case CapabilityFrom, CapabilityTransform, CapabilityValidation:
		return true
	case CapabilityAuthentication, CapabilityAction, CapabilityState, CapabilityKubeconfig:
		return false
	default:
		return false
	}
}

// ValidateWriteOperation checks whether a write operation is being used in a
// read-only resolver context. Providers classify mutating operations via
// Descriptor.Operations (IsWrite) or Descriptor.WriteOperations, resolved
// through Descriptor.EffectiveWriteOperations. Write operations are rejected
// only in read-only resolver modes (CapabilityFrom, CapabilityTransform,
// CapabilityValidation) and permitted in every other mode (CapabilityAction,
// CapabilityState, CapabilityKubeconfig, CapabilityAuthentication).
//
// Semantics follow the SDK convention:
//   - nil EffectiveWriteOperations: provider cannot classify (no enforcement)
//   - empty []string{}: all operations are reads (everything allowed)
//   - populated: listed operations are blocked in read-only resolver modes,
//     including the operation's description in the error when available
func ValidateWriteOperation(ctx context.Context, p Provider, resolvedInputs map[string]any) error {
	if p == nil {
		return nil
	}
	desc := p.Descriptor()
	if desc == nil {
		return nil
	}

	execMode, ok := ExecutionModeFromContext(ctx)
	if !ok || !isReadOnlyResolverMode(execMode) {
		return nil // writes are only blocked in read-only resolver modes
	}

	operation, _ := resolvedInputs["operation"].(string)
	if operation == "" {
		return nil
	}

	if desc.EffectiveWriteOperations() == nil {
		return nil // provider cannot classify operations -- no enforcement
	}

	if desc.IsWriteOperation(operation) {
		tracker := redact.FromContext(ctx)
		safeOp := tracker.RedactString(operation)
		var err error
		if op := desc.GetOperation(operation); op != nil {
			rawDesc := tracker.RedactString(op.Description)
			if opDesc := sanitizeInlineDescription(rawDesc); opDesc != "" {
				err = fmt.Errorf("operation %q (%s) is a write operation and cannot be used in read-only execution mode %q; use a workflow action instead", safeOp, opDesc, execMode)
			}
		}
		if err == nil {
			err = fmt.Errorf("operation %q is a write operation and cannot be used in read-only execution mode %q; use a workflow action instead", safeOp, execMode)
		}
		return tracker.RedactError(err)
	}

	return nil
}

// sanitizeInlineDescription strips ANSI CSI/OSC escape sequences and C0/C1
// control characters from an operation description and collapses whitespace
// runs into single spaces so single-line error messages cannot inject terminal
// escapes or newline-delimited CI workflow commands.
func sanitizeInlineDescription(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i++
			if i < len(s) {
				switch s[i] {
				case '[':
					i++
					for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3f {
						i++
					}
					if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7e {
						i++
					}
				case ']':
					i++
					for i < len(s) {
						if s[i] == 0x07 {
							i++
							break
						}
						if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
							i += 2
							break
						}
						i++
					}
				default:
					if s[i] >= 0x20 && s[i] <= 0x7e {
						i++
					}
				}
			}
			continue
		}
		r, width := utf8.DecodeRuneInString(s[i:])
		i += width
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// Drop C0/C1 control characters.
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
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

	// Start a span for this provider execution.
	ctx, span := telemetry.Tracer(telemetry.TracerProvider).Start(ctx, "provider.Execute",
		trace.WithAttributes(attribute.String("provider.name", desc.Name)),
	)
	defer span.End()

	// Validate execution mode
	if err := validateExecutionMode(ctx, desc); err != nil {
		return nil, err
	}

	// Check if this is a dry-run
	dryRun := DryRunFromContext(ctx)

	// Ensure a redaction tracker is active before resolving inputs so any scrt:/tkn:
	// credentials resolved by InputResolver or upstream callers are scrubbed.
	tracker := redact.FromContext(ctx)
	if tracker == nil {
		tracker = redact.NewTracker()
		ctx = redact.WithTracker(ctx, tracker)
	}

	// Start timing
	startTime := time.Now()

	// Create input resolver with schema
	inputResolver := NewInputResolver(ctx, desc.Schema, desc.SensitiveFields)

	// Resolve inputs
	resolvedInputs, err := inputResolver.ResolveInputs(inputs)
	if err != nil {
		return nil, tracker.RedactError(fmt.Errorf("failed to resolve inputs: %w", err))
	}

	// Check for write operations in resolver context
	if err := ValidateWriteOperation(ctx, provider, resolvedInputs); err != nil {
		return nil, tracker.RedactError(err)
	}

	// Validate resolved inputs
	if err := e.schemaValidator.ValidateInputs(resolvedInputs, desc.Schema); err != nil {
		return nil, tracker.RedactError(fmt.Errorf("input validation failed: %w", err))
	}

	// Determine what to pass to Execute:
	// - If Decode is defined: call it and pass the decoded (typed) value
	// - If Decode is nil: pass the raw map[string]any
	var executionInput any = resolvedInputs
	if desc.Decode != nil {
		decoded, err := desc.Decode(resolvedInputs)
		if err != nil {
			return nil, tracker.RedactError(fmt.Errorf("failed to decode inputs: %w", err))
		}
		executionInput = decoded
	}

	// Wrap IOStreams (or install redacting fallback streams for nil destinations)
	// and lazily emit GitHub Actions workflow mask commands when streaming starts.
	execCtx, flushStreams := WrapRedactingIOStreams(ctx, tracker, provider)

	// Execute the provider with either typed input or map
	outputPtr, err := provider.Execute(execCtx, executionInput)
	flushStreams()

	// Calculate execution duration
	executionDuration := time.Since(startTime)

	// Record metrics (no-op if metrics collection is disabled)
	GlobalMetrics.Record(ctx, desc.Name, executionDuration, err == nil)

	if err != nil {
		redactedErr := tracker.RedactError(fmt.Errorf("provider execution failed: %w", err))
		span.RecordError(redactedErr)
		span.SetStatus(codes.Error, redactedErr.Error())
		return nil, redactedErr
	}
	if outputPtr == nil {
		nilErr := fmt.Errorf("provider returned nil output")
		span.RecordError(nilErr)
		span.SetStatus(codes.Error, nilErr.Error())
		return nil, nilErr
	}
	output := *outputPtr
	output.Warnings = tracker.RedactSlice(output.Warnings)
	output.Metadata = tracker.RedactMap(output.Metadata)

	// Output validation is intentionally not performed per-capability here.
	// The executor operates at the provider level and does not know which
	// capability (resolve, action, etc.) initiated the call. Per-capability
	// output schema validation is the responsibility of the higher-level
	// caller (e.g., resolver engine or action runner) which has the
	// capability context. Pushing capability awareness into the executor
	// would couple it to the solution execution model, violating the
	// single-responsibility boundary between provider execution and
	// solution orchestration.

	// Build result (scrubbing ResolvedInputs so debugging fields never retain raw credentials)
	result := &ExecutionResult{
		Provider:          provider,
		Output:            output,
		DryRun:            dryRun,
		ExecutionDuration: executionDuration,
		ResolvedInputs:    tracker.RedactMap(resolvedInputs),
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

// GetGlobalExecutor returns the global executor instance.
func GetGlobalExecutor() *Executor {
	return globalExecutor
}

// ResetGlobalExecutor resets the global executor to a new instance.
// This is primarily for testing purposes.
func ResetGlobalExecutor() {
	globalExecutor = NewExecutor()
}
