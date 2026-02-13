// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package action provides types and execution logic for the Actions system.
// Actions consume resolved data from resolvers and perform side-effect operations.
package action

import (
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// Workflow contains the action execution specification.
// It defines two sections: regular actions that execute based on dependencies,
// and finally actions that execute after all regular actions complete.
type Workflow struct {
	// Actions is a map of action definitions that execute based on their dependencies.
	// Actions can depend on other actions and access their results via __actions.<name>.results.
	Actions map[string]*Action `json:"actions,omitempty" yaml:"actions,omitempty" doc:"Action definitions keyed by name"`

	// Finally is a map of actions that execute after all regular actions complete.
	// Finally actions cannot use dependsOn to reference regular actions, but can
	// access all regular action results. They have an implicit dependency on all regular actions.
	Finally map[string]*Action `json:"finally,omitempty" yaml:"finally,omitempty" doc:"Cleanup/finalization actions that run after all regular actions"`

	// ResultSchemaMode sets the default validation behavior for resultSchema across all actions.
	// Individual actions can override this setting. Default is "error".
	ResultSchemaMode ResultSchemaMode `json:"resultSchemaMode,omitempty" yaml:"resultSchemaMode,omitempty" doc:"Default result schema validation mode" example:"error" default:"error"`
}

// Action represents a single action definition.
// Actions perform side-effect operations using providers and can depend on
// other actions for sequencing and data flow.
type Action struct {
	// Name is the action identifier, set from the map key.
	// Cannot start with "__" (reserved) or contain "[" or "]".
	Name string `json:"name" yaml:"name" doc:"Action identifier (set from map key)" maxLength:"100" pattern:"^[a-zA-Z_][a-zA-Z0-9_-]*$" patternDescription:"Must start with letter/underscore, followed by alphanumerics, underscores, or hyphens"`

	// Description provides documentation for the action.
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable description of what the action does" maxLength:"500"`

	// DisplayName is a human-friendly name for UI display.
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-friendly display name" maxLength:"100"`

	// Sensitive indicates the action handles sensitive data (masks in logs).
	Sensitive bool `json:"sensitive,omitempty" yaml:"sensitive,omitempty" doc:"If true, inputs and outputs are masked in logs"`

	// Provider specifies which action provider to use for execution.
	// The provider must have CapabilityAction.
	Provider string `json:"provider" yaml:"provider" doc:"Action provider name" maxLength:"100" example:"shell"`

	// Inputs is a map of input values passed to the provider.
	// Values can be literals, resolver references, expressions, or templates.
	// Expressions referencing __actions are deferred until runtime.
	Inputs map[string]*spec.ValueRef `json:"inputs,omitempty" yaml:"inputs,omitempty" doc:"Input values for the provider"`

	// DependsOn lists action names that must complete before this action runs.
	// For regular actions, only other regular actions can be referenced.
	// For finally actions, only other finally actions can be referenced.
	DependsOn []string `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty" doc:"Actions that must complete before this action runs" maxItems:"100"`

	// When is a condition that must evaluate to true for the action to execute.
	// If false, the action is skipped with SkipReasonCondition.
	When *spec.Condition `json:"when,omitempty" yaml:"when,omitempty" doc:"Condition for execution (skipped if false)"`

	// OnError defines behavior when this action fails.
	// Default is "fail" which stops workflow execution.
	OnError spec.OnErrorBehavior `json:"onError,omitempty" yaml:"onError,omitempty" doc:"Error handling behavior" example:"fail" default:"fail"`

	// Timeout limits how long the action can run.
	// If exceeded, the action fails with StatusTimeout.
	Timeout *duration.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty" doc:"Maximum execution duration" example:"30s"`

	// Retry configures automatic retry on failure.
	Retry *RetryConfig `json:"retry,omitempty" yaml:"retry,omitempty" doc:"Retry configuration for transient failures"`

	// ForEach enables iteration, executing the action once per array element.
	// Only allowed in workflow.actions, not workflow.finally.
	ForEach *spec.ForEachClause `json:"forEach,omitempty" yaml:"forEach,omitempty" doc:"Iteration configuration (not allowed in finally)"`

	// ResultSchema defines the expected structure of the action's output using JSON Schema.
	// If provided, the provider's output is validated against this schema at runtime.
	// Supports full JSON Schema 2020-12 specification including $ref, allOf, anyOf, oneOf, etc.
	// This enables self-documenting actions and catches mismatches early.
	// Use ResultSchemaMode to control validation behavior (error, warn, ignore).
	ResultSchema *jsonschema.Schema `json:"resultSchema,omitempty" yaml:"resultSchema,omitempty" doc:"JSON Schema for result validation"`

	// ResultSchemaMode controls behavior when result schema validation fails.
	// Overrides the workflow-level default. Options: "error" (fail action), "warn" (log and continue), "ignore" (skip validation).
	ResultSchemaMode ResultSchemaMode `json:"resultSchemaMode,omitempty" yaml:"resultSchemaMode,omitempty" doc:"Result schema validation mode" example:"error"`
}

// ResultSchemaMode defines the validation behavior when result schema validation fails.
type ResultSchemaMode string

const (
	// ResultSchemaModeError fails the action when schema validation fails (default).
	ResultSchemaModeError ResultSchemaMode = "error"

	// ResultSchemaModeWarn logs a warning and continues execution.
	ResultSchemaModeWarn ResultSchemaMode = "warn"

	// ResultSchemaModeIgnore skips schema validation entirely.
	ResultSchemaModeIgnore ResultSchemaMode = "ignore"
)

// IsValid returns true if the result schema mode is valid.
func (m ResultSchemaMode) IsValid() bool {
	switch m {
	case ResultSchemaModeError, ResultSchemaModeWarn, ResultSchemaModeIgnore, "":
		return true
	default:
		return false
	}
}

// OrDefault returns the mode or the default (ResultSchemaModeError) if empty.
func (m ResultSchemaMode) OrDefault() ResultSchemaMode {
	if m == "" {
		return ResultSchemaModeError
	}
	return m
}

// RetryConfig defines automatic retry behavior for failed actions.
type RetryConfig struct {
	// MaxAttempts is the total number of execution attempts (including initial).
	// Must be >= 1.
	MaxAttempts int `json:"maxAttempts" yaml:"maxAttempts" doc:"Total execution attempts (min: 1)" minimum:"1" example:"3"`

	// Backoff defines the delay strategy between retries.
	// Default is "fixed".
	Backoff BackoffType `json:"backoff,omitempty" yaml:"backoff,omitempty" doc:"Backoff strategy" example:"exponential" default:"fixed"`

	// InitialDelay is the delay before the first retry.
	// For exponential backoff, subsequent delays are multiplied by 2.
	InitialDelay *duration.Duration `json:"initialDelay,omitempty" yaml:"initialDelay,omitempty" doc:"Delay before first retry" example:"1s"`

	// MaxDelay caps the maximum delay between retries.
	// Only meaningful for linear and exponential backoff.
	MaxDelay *duration.Duration `json:"maxDelay,omitempty" yaml:"maxDelay,omitempty" doc:"Maximum delay between retries" example:"30s"`

	// RetryIf is a CEL expression that determines whether a retry should occur.
	// The expression has access to __error context with error details:
	//   - __error.message: Error message string
	//   - __error.type: Error type ("http", "exec", "timeout", "validation", "unknown")
	//   - __error.statusCode: HTTP status code (0 if not HTTP)
	//   - __error.exitCode: Process exit code (0 if not exec)
	//   - __error.attempt: Current attempt number (1-based)
	//   - __error.maxAttempts: Maximum configured attempts
	// If not specified, all errors are retried (default behavior).
	// If specified and evaluates to false, no retry occurs.
	// Example: "${ __error.statusCode == 429 || __error.statusCode >= 500 }"
	RetryIf *celexp.Expression `json:"retryIf,omitempty" yaml:"retryIf,omitempty" doc:"CEL expression to determine if retry should occur"`
}

// BackoffType defines the backoff strategy for retries.
type BackoffType string

const (
	// BackoffFixed uses a constant delay between retries.
	BackoffFixed BackoffType = "fixed"

	// BackoffLinear increases delay linearly (initialDelay * attempt).
	BackoffLinear BackoffType = "linear"

	// BackoffExponential doubles the delay with each retry.
	BackoffExponential BackoffType = "exponential"
)

// IsValid returns true if the backoff type is valid.
func (b BackoffType) IsValid() bool {
	switch b {
	case BackoffFixed, BackoffLinear, BackoffExponential, "":
		return true
	default:
		return false
	}
}

// OrDefault returns the backoff type or the default (BackoffFixed) if empty.
func (b BackoffType) OrDefault() BackoffType {
	if b == "" {
		return BackoffFixed
	}
	return b
}

// ActionStatus represents the execution status of an action.
//
//nolint:revive // ActionStatus is intentionally named for clarity in domain context
type ActionStatus string

const (
	// StatusPending indicates the action has not started.
	StatusPending ActionStatus = "pending"

	// StatusRunning indicates the action is currently executing.
	StatusRunning ActionStatus = "running"

	// StatusSucceeded indicates the action completed successfully.
	StatusSucceeded ActionStatus = "succeeded"

	// StatusFailed indicates the action failed.
	StatusFailed ActionStatus = "failed"

	// StatusSkipped indicates the action was not executed.
	StatusSkipped ActionStatus = "skipped"

	// StatusTimeout indicates the action exceeded its timeout.
	StatusTimeout ActionStatus = "timeout"

	// StatusCancelled indicates the action was cancelled.
	StatusCancelled ActionStatus = "cancelled"
)

// IsTerminal returns true if the status is a terminal state.
func (s ActionStatus) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusSkipped, StatusTimeout, StatusCancelled:
		return true
	case StatusPending, StatusRunning:
		return false
	default:
		return false
	}
}

// IsSuccess returns true if the status indicates successful completion.
func (s ActionStatus) IsSuccess() bool {
	return s == StatusSucceeded
}

// SkipReason indicates why an action was skipped.
type SkipReason string

const (
	// SkipReasonCondition indicates the when condition evaluated to false.
	SkipReasonCondition SkipReason = "condition"

	// SkipReasonDependencyFailed indicates a required dependency failed.
	SkipReasonDependencyFailed SkipReason = "dependency-failed"
)

// ActionResult represents the result of an action execution.
// It is stored in the __actions namespace and accessible to dependent actions.
//
//nolint:revive // ActionResult is intentionally named for clarity in domain context
type ActionResult struct {
	// Inputs contains the resolved input values that were passed to the provider.
	Inputs map[string]any `json:"inputs" yaml:"inputs" doc:"Resolved inputs passed to provider"`

	// Results contains the output data from the provider.
	// This is accessible as __actions.<name>.results in CEL/templates.
	Results any `json:"results,omitempty" yaml:"results,omitempty" doc:"Provider output data"`

	// Status is the final execution status.
	Status ActionStatus `json:"status" yaml:"status" doc:"Execution status"`

	// SkipReason explains why the action was skipped (if Status is StatusSkipped).
	SkipReason SkipReason `json:"skipReason,omitempty" yaml:"skipReason,omitempty" doc:"Reason for skipping (if skipped)"`

	// StartTime is when execution began.
	StartTime *time.Time `json:"startTime,omitempty" yaml:"startTime,omitempty" doc:"Execution start time"`

	// EndTime is when execution completed.
	EndTime *time.Time `json:"endTime,omitempty" yaml:"endTime,omitempty" doc:"Execution end time"`

	// Error contains the error message if Status is StatusFailed or StatusTimeout.
	Error string `json:"error,omitempty" yaml:"error,omitempty" doc:"Error message (if failed)"`
}

// Duration returns the execution duration, or zero if not available.
func (r *ActionResult) Duration() time.Duration {
	if r.StartTime == nil || r.EndTime == nil {
		return 0
	}
	return r.EndTime.Sub(*r.StartTime)
}

// ForEachIterationResult represents the result of a single forEach iteration.
// When an action uses forEach, results are collected into an array of these.
type ForEachIterationResult struct {
	// Index is the 0-based iteration index.
	Index int `json:"index" yaml:"index" doc:"Iteration index (0-based)"`

	// Name is the expanded action name (e.g., "deploy[0]", "deploy[1]").
	Name string `json:"name" yaml:"name" doc:"Expanded action name" maxLength:"150"`

	// Results contains the output data from this iteration.
	Results any `json:"results,omitempty" yaml:"results,omitempty" doc:"Iteration output data"`

	// Status is the execution status of this iteration.
	Status ActionStatus `json:"status" yaml:"status" doc:"Iteration execution status"`

	// StartTime is when this iteration began.
	StartTime *time.Time `json:"startTime,omitempty" yaml:"startTime,omitempty" doc:"Iteration start time"`

	// EndTime is when this iteration completed.
	EndTime *time.Time `json:"endTime,omitempty" yaml:"endTime,omitempty" doc:"Iteration end time"`

	// Error contains the error message if this iteration failed.
	Error string `json:"error,omitempty" yaml:"error,omitempty" doc:"Error message (if failed)"`
}

// Duration returns the iteration execution duration, or zero if not available.
func (r *ForEachIterationResult) Duration() time.Duration {
	if r.StartTime == nil || r.EndTime == nil {
		return 0
	}
	return r.EndTime.Sub(*r.StartTime)
}
