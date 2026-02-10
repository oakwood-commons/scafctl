// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"errors"
	"fmt"
	"strings"
)

// ExecutionError represents an error during resolver execution with context about
// which resolver, phase, step, and provider were involved.
type ExecutionError struct {
	ResolverName string `json:"resolverName" yaml:"resolverName" doc:"Name of the resolver that failed" example:"my-resolver"`
	Phase        string `json:"phase" yaml:"phase" doc:"Execution phase where failure occurred" enum:"resolve,transform,validate" example:"resolve"`
	Step         int    `json:"step" yaml:"step" doc:"Step number within the phase (0-indexed)" minimum:"0" example:"0"`
	Provider     string `json:"provider" yaml:"provider" doc:"Provider name that failed" example:"http"`
	Cause        error  `json:"-" yaml:"-" doc:"Underlying error that caused the failure"`
}

// Error implements the error interface.
func (e *ExecutionError) Error() string {
	if e.Provider != "" {
		return fmt.Sprintf("resolver %q failed in %s phase (step %d, provider %s): %v",
			e.ResolverName, e.Phase, e.Step, e.Provider, e.Cause)
	}
	return fmt.Sprintf("resolver %q failed in %s phase (step %d): %v",
		e.ResolverName, e.Phase, e.Step, e.Cause)
}

// Unwrap returns the underlying cause for use with errors.Is and errors.As.
func (e *ExecutionError) Unwrap() error {
	return e.Cause
}

// NewExecutionError creates a new ExecutionError with the given parameters.
func NewExecutionError(resolverName, phase, provider string, step int, cause error) *ExecutionError {
	return &ExecutionError{
		ResolverName: resolverName,
		Phase:        phase,
		Step:         step,
		Provider:     provider,
		Cause:        cause,
	}
}

// ValidationFailure represents a single validation rule failure.
type ValidationFailure struct {
	Rule      int    `json:"rule" yaml:"rule" doc:"Rule number (0-indexed) that failed" minimum:"0" example:"0"`
	Provider  string `json:"provider" yaml:"provider" doc:"Validation provider name" example:"validation"`
	Message   string `json:"message" yaml:"message" doc:"Human-readable failure message" maxLength:"1000" example:"value must be a valid email address"`
	Cause     error  `json:"-" yaml:"-" doc:"Underlying error from the validation provider"`
	Sensitive bool   `json:"sensitive,omitempty" yaml:"sensitive,omitempty" doc:"Whether this failure contains sensitive information"`
}

// Error implements the error interface for a single failure.
func (f *ValidationFailure) Error() string {
	if f.Message != "" {
		return f.Message
	}
	if f.Cause != nil {
		return f.Cause.Error()
	}
	return fmt.Sprintf("rule %d failed", f.Rule)
}

// AggregatedValidationError represents validation failure with multiple messages.
// This collects all validation failures from a resolver's validate phase.
type AggregatedValidationError struct {
	ResolverName string              `json:"resolverName" yaml:"resolverName" doc:"Name of the resolver that failed validation" example:"user-email"`
	Value        any                 `json:"-" yaml:"-" doc:"The value that failed validation"`
	Failures     []ValidationFailure `json:"failures" yaml:"failures" doc:"List of validation failures" minItems:"1"`
	Sensitive    bool                `json:"sensitive,omitempty" yaml:"sensitive,omitempty" doc:"Whether the resolver value is sensitive"`
}

// Error implements the error interface.
func (e *AggregatedValidationError) Error() string {
	if len(e.Failures) == 0 {
		return fmt.Sprintf("resolver %q validation failed (no details)", e.ResolverName)
	}

	if len(e.Failures) == 1 {
		return fmt.Sprintf("resolver %q validation failed: %s", e.ResolverName, e.Failures[0].Error())
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("resolver %q validation failed with %d errors:\n", e.ResolverName, len(e.Failures)))
	for i, f := range e.Failures {
		sb.WriteString(fmt.Sprintf("  - [rule %d] %s\n", i+1, f.Error()))
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// Unwrap returns nil since this error aggregates multiple failures.
// Use errors.As with *ValidationFailure to check individual failures.
func (e *AggregatedValidationError) Unwrap() error {
	return nil
}

// HasFailures returns true if there are any validation failures.
func (e *AggregatedValidationError) HasFailures() bool {
	return len(e.Failures) > 0
}

// AddFailure adds a validation failure to the error.
func (e *AggregatedValidationError) AddFailure(failure ValidationFailure) {
	e.Failures = append(e.Failures, failure)
}

// CircularDependencyError represents a cycle detected in resolver dependencies.
type CircularDependencyError struct {
	Cycle []string `json:"cycle" yaml:"cycle" doc:"List of resolver names forming the cycle" minItems:"2" example:"[\"resolver-a\", \"resolver-b\", \"resolver-a\"]"`
}

// Error implements the error interface.
func (e *CircularDependencyError) Error() string {
	if len(e.Cycle) == 0 {
		return "circular dependency detected"
	}
	return fmt.Sprintf("circular dependency detected: %s", strings.Join(e.Cycle, " → "))
}

// NewCircularDependencyError creates a new CircularDependencyError from a cycle path.
func NewCircularDependencyError(cycle []string) *CircularDependencyError {
	return &CircularDependencyError{Cycle: cycle}
}

// PhaseTimeoutError represents a timeout during phase execution.
type PhaseTimeoutError struct {
	Phase            int      `json:"phase" yaml:"phase" doc:"Phase number that timed out" minimum:"1" example:"1"`
	ResolversWaiting []string `json:"resolversWaiting" yaml:"resolversWaiting" doc:"Resolvers that were still waiting when timeout occurred"`
}

// Error implements the error interface.
func (e *PhaseTimeoutError) Error() string {
	if len(e.ResolversWaiting) > 0 {
		return fmt.Sprintf("phase %d timed out with %d resolvers still waiting: %s",
			e.Phase, len(e.ResolversWaiting), strings.Join(e.ResolversWaiting, ", "))
	}
	return fmt.Sprintf("phase %d timed out", e.Phase)
}

// ValueSizeError represents a value that exceeds the maximum allowed size.
type ValueSizeError struct {
	ResolverName string `json:"resolverName" yaml:"resolverName" doc:"Name of the resolver with oversized value" example:"large-data"`
	ActualSize   int64  `json:"actualSize" yaml:"actualSize" doc:"Actual size of the value in bytes" minimum:"0" example:"10485760"`
	MaxSize      int64  `json:"maxSize" yaml:"maxSize" doc:"Maximum allowed size in bytes" minimum:"1" example:"1048576"`
}

// Error implements the error interface.
func (e *ValueSizeError) Error() string {
	return fmt.Sprintf("resolver %q value size %d bytes exceeds maximum %d bytes",
		e.ResolverName, e.ActualSize, e.MaxSize)
}

// TypeCoercionError represents a failure to coerce a value to the expected type.
type TypeCoercionError struct {
	ResolverName string `json:"resolverName" yaml:"resolverName" doc:"Name of the resolver" example:"age-value"`
	Phase        string `json:"phase" yaml:"phase" doc:"Phase where coercion was attempted" enum:"resolve,transform" example:"resolve"`
	SourceType   string `json:"sourceType" yaml:"sourceType" doc:"Original type of the value" example:"string"`
	TargetType   Type   `json:"targetType" yaml:"targetType" doc:"Target type for coercion" example:"int"`
	Cause        error  `json:"-" yaml:"-" doc:"Underlying coercion error"`
}

// Error implements the error interface.
func (e *TypeCoercionError) Error() string {
	return fmt.Sprintf("resolver %q: type coercion from %s to %s failed after %s phase: %v",
		e.ResolverName, e.SourceType, e.TargetType, e.Phase, e.Cause)
}

// Unwrap returns the underlying cause.
func (e *TypeCoercionError) Unwrap() error {
	return e.Cause
}

// IsExecutionError checks if an error is an ExecutionError.
func IsExecutionError(err error) bool {
	var execErr *ExecutionError
	return errors.As(err, &execErr)
}

// IsValidationError checks if an error is an AggregatedValidationError.
func IsValidationError(err error) bool {
	var validationErr *AggregatedValidationError
	return errors.As(err, &validationErr)
}

// IsCircularDependencyError checks if an error is a CircularDependencyError.
func IsCircularDependencyError(err error) bool {
	var circularErr *CircularDependencyError
	return errors.As(err, &circularErr)
}

// IsValueSizeError checks if an error is a ValueSizeError.
func IsValueSizeError(err error) bool {
	var sizeErr *ValueSizeError
	return errors.As(err, &sizeErr)
}

// IsTypeCoercionError checks if an error is a TypeCoercionError.
func IsTypeCoercionError(err error) bool {
	var coercionErr *TypeCoercionError
	return errors.As(err, &coercionErr)
}

// ForEachTypeError represents an error when forEach input is not an array.
type ForEachTypeError struct {
	ResolverName string `json:"resolverName" yaml:"resolverName" doc:"Name of the resolver" example:"regionConfigs"`
	Step         int    `json:"step" yaml:"step" doc:"Transform step number (0-indexed)" minimum:"0" example:"0"`
	ActualType   string `json:"actualType" yaml:"actualType" doc:"Actual type received" example:"string"`
}

// Error implements the error interface.
func (e *ForEachTypeError) Error() string {
	return fmt.Sprintf("resolver %q transform step %d: forEach requires array input, got %s",
		e.ResolverName, e.Step, e.ActualType)
}

// IsForEachTypeError checks if an error is a ForEachTypeError.
func IsForEachTypeError(err error) bool {
	var forEachErr *ForEachTypeError
	return errors.As(err, &forEachErr)
}

// ForEachIterationResult represents the result of a single forEach iteration.
// Used when onError: continue allows partial results with error metadata.
type ForEachIterationResult struct {
	Index int    `json:"index" yaml:"index" doc:"Index of the iteration" minimum:"0" example:"1"`
	Data  any    `json:"data,omitempty" yaml:"data,omitempty" doc:"Result data if successful"`
	Error string `json:"error,omitempty" yaml:"error,omitempty" doc:"Error message if failed" maxLength:"1000"`
	Item  any    `json:"item,omitempty" yaml:"item,omitempty" doc:"The item that was processed"`
}

// RedactedError wraps an error to provide a redacted version for sensitive contexts.
type RedactedError struct {
	Original error  `json:"-" yaml:"-" doc:"Original unredacted error"`
	Redacted string `json:"error" yaml:"error" doc:"Redacted error message safe for display" maxLength:"100" example:"[REDACTED]"`
}

// Error returns the redacted error message.
func (e *RedactedError) Error() string {
	return e.Redacted
}

// Unwrap returns the original error for privileged access.
func (e *RedactedError) Unwrap() error {
	return e.Original
}

// NewRedactedError creates a new RedactedError.
func NewRedactedError(original error) *RedactedError {
	return &RedactedError{
		Original: original,
		Redacted: "[REDACTED]",
	}
}

// AggregatedExecutionError represents multiple resolver errors collected during validate-all mode.
// This is used when the executor is configured to continue execution after failures.
type AggregatedExecutionError struct {
	Errors         []*FailedResolver `json:"errors" yaml:"errors" doc:"All errors encountered during execution" minItems:"1"`
	SkippedCount   int               `json:"skippedCount" yaml:"skippedCount" doc:"Number of resolvers skipped due to failed dependencies" minimum:"0" example:"3"`
	SkippedNames   []string          `json:"skippedNames" yaml:"skippedNames" doc:"Names of skipped resolvers"`
	SucceededCount int               `json:"succeededCount" yaml:"succeededCount" doc:"Number of resolvers that succeeded" minimum:"0" example:"5"`
}

// FailedResolver represents an error from a specific resolver with context.
// Used within AggregatedExecutionError for validate-all mode.
type FailedResolver struct {
	ResolverName string `json:"resolverName" yaml:"resolverName" doc:"Name of the resolver that failed" example:"my-resolver"`
	Phase        int    `json:"phase" yaml:"phase" doc:"Phase number where the error occurred" minimum:"1" example:"1"`
	Err          error  `json:"-" yaml:"-" doc:"The underlying error"`
	ErrMessage   string `json:"error" yaml:"error" doc:"Error message" maxLength:"2000" example:"validation failed: value must be positive"`
}

// Error implements the error interface.
func (e *FailedResolver) Error() string {
	return fmt.Sprintf("resolver %q (phase %d): %v", e.ResolverName, e.Phase, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *FailedResolver) Unwrap() error {
	return e.Err
}

// Error implements the error interface.
func (e *AggregatedExecutionError) Error() string {
	if len(e.Errors) == 0 {
		return "no errors"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%d resolver(s) failed", len(e.Errors)))
	if e.SkippedCount > 0 {
		sb.WriteString(fmt.Sprintf(", %d skipped due to failed dependencies", e.SkippedCount))
	}
	if e.SucceededCount > 0 {
		sb.WriteString(fmt.Sprintf(", %d succeeded", e.SucceededCount))
	}
	sb.WriteString(":\n")

	for i, err := range e.Errors {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, err.Error()))
	}

	return strings.TrimSuffix(sb.String(), "\n")
}

// HasErrors returns true if there are any errors.
func (e *AggregatedExecutionError) HasErrors() bool {
	return len(e.Errors) > 0
}

// Add adds a resolver error to the aggregated error.
func (e *AggregatedExecutionError) Add(resolverName string, phase int, err error) {
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	e.Errors = append(e.Errors, &FailedResolver{
		ResolverName: resolverName,
		Phase:        phase,
		Err:          err,
		ErrMessage:   errMsg,
	})
}

// AddSkipped records that a resolver was skipped due to failed dependencies.
func (e *AggregatedExecutionError) AddSkipped(resolverName string) {
	e.SkippedCount++
	e.SkippedNames = append(e.SkippedNames, resolverName)
}

// IncrementSucceeded increments the count of succeeded resolvers.
func (e *AggregatedExecutionError) IncrementSucceeded() {
	e.SucceededCount++
}

// IsAggregatedExecutionError checks if an error is an AggregatedExecutionError.
func IsAggregatedExecutionError(err error) bool {
	var aggErr *AggregatedExecutionError
	return errors.As(err, &aggErr)
}
