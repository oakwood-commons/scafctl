// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// RetryExecutor wraps action execution with retry logic and backoff strategies.
// It handles transient failures by retrying the action up to maxAttempts times
// with configurable delays between attempts.
type RetryExecutor struct {
	// config is the retry configuration
	config *RetryConfig

	// jitterFn optionally adds jitter to delays (for testing)
	jitterFn func(time.Duration) time.Duration
}

// NewRetryExecutor creates a new retry executor with the given configuration.
// If config is nil, returns an executor that performs no retries (single attempt).
func NewRetryExecutor(config *RetryConfig) *RetryExecutor {
	return &RetryExecutor{
		config: config,
	}
}

// WithJitter sets a custom jitter function for testing.
// The jitter function receives the base delay and returns a modified delay.
func (r *RetryExecutor) WithJitter(fn func(time.Duration) time.Duration) *RetryExecutor {
	r.jitterFn = fn
	return r
}

// CalculateDelay computes the delay before a retry attempt based on the backoff strategy.
// attempt is 1-indexed (first retry is attempt 2, since attempt 1 is the initial execution).
// Returns 0 for the first attempt or if no retry config is set.
func (r *RetryExecutor) CalculateDelay(attempt int) time.Duration {
	if r.config == nil || attempt <= 1 {
		return 0
	}

	// Get initial delay (default to 1s if not set)
	initialDelay := time.Second
	if r.config.InitialDelay != nil {
		initialDelay = r.config.InitialDelay.Duration
	}

	// Get max delay (default to 5 minutes if not set)
	maxDelay := 5 * time.Minute
	if r.config.MaxDelay != nil {
		maxDelay = r.config.MaxDelay.Duration
	}

	backoff := r.config.Backoff.OrDefault()
	retryNumber := attempt - 1 // Convert to 0-indexed for calculation

	var delay time.Duration

	switch backoff {
	case BackoffFixed:
		// Fixed: always use initialDelay
		delay = initialDelay

	case BackoffLinear:
		// Linear: initialDelay * retryNumber
		delay = initialDelay * time.Duration(retryNumber)

	case BackoffExponential:
		// Exponential: initialDelay * 2^(retryNumber-1)
		// For first retry (retryNumber=1): initialDelay * 2^0 = initialDelay
		// For second retry (retryNumber=2): initialDelay * 2^1 = 2 * initialDelay
		if retryNumber <= 0 {
			delay = initialDelay
		} else {
			multiplier := math.Pow(2, float64(retryNumber-1))
			delay = time.Duration(float64(initialDelay) * multiplier)
		}

	default:
		// Default to fixed
		delay = initialDelay
	}

	// Apply max delay cap
	if delay > maxDelay {
		delay = maxDelay
	}

	// Apply jitter if configured
	if r.jitterFn != nil {
		delay = r.jitterFn(delay)
	}

	return delay
}

// MaxAttempts returns the maximum number of execution attempts.
// Returns 1 if no config is set (no retries).
func (r *RetryExecutor) MaxAttempts() int {
	if r.config == nil || r.config.MaxAttempts < 1 {
		return 1
	}
	return r.config.MaxAttempts
}

// ShouldRetry determines if an execution should be retried based on the error and attempt number.
// Returns (shouldRetry, error). The error is non-nil if retryIf expression evaluation fails.
// Returns false if:
// - No retry config is set
// - Max attempts reached
// - Context was cancelled
// - The error is nil (success)
// - retryIf expression evaluates to false
func (r *RetryExecutor) ShouldRetry(ctx context.Context, err error, attempt int) (bool, error) {
	if err == nil {
		return false, nil
	}
	if r.config == nil {
		return false, nil
	}
	if attempt >= r.MaxAttempts() {
		return false, nil
	}
	if ctx.Err() != nil {
		// Context cancelled - don't retry but this is not an error in the retry logic itself
		return false, nil //nolint:nilerr // Intentionally returning nil error - ctx cancellation is handled by caller
	}

	// If retryIf is configured, evaluate it
	if r.config.RetryIf != nil {
		return r.evaluateRetryCondition(ctx, err, attempt)
	}

	// Default: retry all errors
	return true, nil
}

// evaluateRetryCondition evaluates the retryIf CEL expression.
// Returns (shouldRetry, error). The error is non-nil if expression evaluation fails.
func (r *RetryExecutor) evaluateRetryCondition(ctx context.Context, err error, attempt int) (bool, error) {
	errCtx := NewErrorContext(err, attempt, r.MaxAttempts())

	// Build additional variables with __error context
	additionalVars := map[string]any{
		"__error": errCtx.ToMap(),
	}

	// Evaluate the expression
	result, evalErr := celexp.EvaluateExpression(ctx, string(*r.config.RetryIf), nil, additionalVars)
	if evalErr != nil {
		return false, fmt.Errorf("failed to evaluate retryIf expression: %w", evalErr)
	}

	// Ensure result is boolean
	shouldRetry, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("retryIf expression must return boolean, got %T", result)
	}

	return shouldRetry, nil
}

// ExecuteFunc is the function signature for action execution.
// It takes a context and returns the provider output and any error.
type ExecuteFunc func(ctx context.Context) (*provider.Output, error)

// RetryCallback receives retry events for progress reporting.
type RetryCallback interface {
	// OnRetryAttempt is called before each retry attempt (not for the initial attempt).
	// attempt is 1-indexed (first retry is attempt 2).
	// err is the error from the previous attempt.
	OnRetryAttempt(actionName string, attempt, maxAttempts int, err error)
}

// ExecuteWithRetry runs an action with retry support.
// It executes the action up to maxAttempts times, with delays between retries
// based on the configured backoff strategy.
//
// Parameters:
// - ctx: Context for cancellation and timeout
// - actionName: Name of the action (for callbacks)
// - execute: Function that performs the actual action execution
// - callback: Optional callback for retry events (can be nil)
//
// Returns the output from a successful execution or the last error if all attempts fail.
func (r *RetryExecutor) ExecuteWithRetry(
	ctx context.Context,
	actionName string,
	execute ExecuteFunc,
	callback RetryCallback,
) (*provider.Output, error) {
	maxAttempts := r.MaxAttempts()
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Check for cancellation before each attempt
		if ctx.Err() != nil {
			return nil, fmt.Errorf("execution cancelled: %w", ctx.Err())
		}

		// If this is a retry (not the first attempt), wait and notify callback
		if attempt > 1 {
			delay := r.CalculateDelay(attempt)

			// Notify callback about retry
			if callback != nil {
				callback.OnRetryAttempt(actionName, attempt, maxAttempts, lastErr)
			}

			// Wait for the delay or context cancellation
			if delay > 0 {
				select {
				case <-ctx.Done():
					return nil, fmt.Errorf("execution cancelled during retry delay: %w", ctx.Err())
				case <-time.After(delay):
					// Continue with retry
				}
			}
		}

		// Execute the action
		output, err := execute(ctx)
		if err == nil {
			return output, nil
		}

		lastErr = err

		// Check if we should retry
		shouldRetry, retryErr := r.ShouldRetry(ctx, err, attempt)
		if retryErr != nil {
			// Expression evaluation failed - fail the action with both errors
			return nil, fmt.Errorf("action failed and retryIf evaluation failed: %w (original error: %w)", retryErr, err)
		}
		if !shouldRetry {
			// Return actual attempt count, not maxAttempts
			return nil, fmt.Errorf("action failed after %d attempt(s): %w", attempt, lastErr)
		}
	}

	return nil, fmt.Errorf("action failed after %d attempt(s): %w", maxAttempts, lastErr)
}

// RetryResult contains information about a retry execution.
type RetryResult struct {
	// Output is the successful output (nil if all attempts failed)
	Output *provider.Output `json:"output,omitempty" yaml:"output,omitempty" doc:"Successful output (nil if all attempts failed)"`

	// Attempts is the total number of attempts made
	Attempts int `json:"attempts" yaml:"attempts" doc:"Total number of attempts made" maximum:"100" example:"3"`

	// TotalDuration is the total time spent including delays
	TotalDuration time.Duration `json:"totalDuration" yaml:"totalDuration" doc:"Total time spent including delays"`

	// FinalError is the error from the last attempt (nil if succeeded)
	FinalError error `json:"finalError,omitempty" yaml:"finalError,omitempty" doc:"Error from the last attempt (nil if succeeded)"`

	// AttemptErrors contains errors from each attempt
	AttemptErrors []error `json:"attemptErrors,omitempty" yaml:"attemptErrors,omitempty" doc:"Errors from each attempt" maxItems:"100"`
}

// ExecuteWithRetryDetailed runs an action with retry support and returns detailed results.
// This is useful for debugging and detailed progress reporting.
func (r *RetryExecutor) ExecuteWithRetryDetailed(
	ctx context.Context,
	actionName string,
	execute ExecuteFunc,
	callback RetryCallback,
) *RetryResult {
	maxAttempts := r.MaxAttempts()
	result := &RetryResult{
		AttemptErrors: make([]error, 0, maxAttempts),
	}

	startTime := time.Now()
	defer func() {
		result.TotalDuration = time.Since(startTime)
	}()

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Check for cancellation before each attempt
		if ctx.Err() != nil {
			result.FinalError = fmt.Errorf("execution cancelled: %w", ctx.Err())
			result.Attempts = attempt
			return result
		}

		// If this is a retry (not the first attempt), wait and notify callback
		if attempt > 1 {
			delay := r.CalculateDelay(attempt)

			// Notify callback about retry
			if callback != nil && result.FinalError != nil {
				callback.OnRetryAttempt(actionName, attempt, maxAttempts, result.FinalError)
			}

			// Wait for the delay or context cancellation
			if delay > 0 {
				select {
				case <-ctx.Done():
					result.FinalError = fmt.Errorf("execution cancelled during retry delay: %w", ctx.Err())
					result.Attempts = attempt
					return result
				case <-time.After(delay):
					// Continue with retry
				}
			}
		}

		// Execute the action
		output, err := execute(ctx)
		result.Attempts = attempt

		if err == nil {
			result.Output = output
			result.FinalError = nil
			return result
		}

		result.AttemptErrors = append(result.AttemptErrors, err)
		result.FinalError = err

		// Check if we should retry
		shouldRetry, retryErr := r.ShouldRetry(ctx, err, attempt)
		if retryErr != nil {
			// Expression evaluation failed - fail with both errors
			result.FinalError = fmt.Errorf("action failed and retryIf evaluation failed: %w (original error: %w)", retryErr, err)
			return result
		}
		if !shouldRetry {
			break
		}
	}

	return result
}
