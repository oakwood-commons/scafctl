// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// ForEachExecutor handles forEach iteration execution with concurrency control.
// It manages parallel execution of iterations while respecting concurrency limits
// and error handling policies.
type ForEachExecutor struct {
	// concurrency is the maximum number of parallel iterations (0 = unlimited)
	concurrency int

	// onError defines behavior when an iteration fails
	onError spec.OnErrorBehavior

	// progressCallback receives progress updates during execution
	progressCallback RetryObserver
}

// ForEachExecutorOption configures the ForEachExecutor.
type ForEachExecutorOption func(*ForEachExecutor)

// WithForEachConcurrency sets the concurrency limit for forEach execution.
// Set to 0 for unlimited concurrency.
func WithForEachConcurrency(n int) ForEachExecutorOption {
	return func(e *ForEachExecutor) {
		e.concurrency = n
	}
}

// WithForEachOnError sets the error handling behavior.
func WithForEachOnError(behavior spec.OnErrorBehavior) ForEachExecutorOption {
	return func(e *ForEachExecutor) {
		e.onError = behavior
	}
}

// WithForEachProgressCallback sets the progress callback for forEach execution.
// It accepts a RetryObserver since forEach only reports retry and iteration progress.
func WithForEachProgressCallback(callback RetryObserver) ForEachExecutorOption {
	return func(e *ForEachExecutor) {
		e.progressCallback = callback
	}
}

// NewForEachExecutor creates a new ForEachExecutor with the given options.
func NewForEachExecutor(opts ...ForEachExecutorOption) *ForEachExecutor {
	e := &ForEachExecutor{
		onError: spec.OnErrorFail,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// FromForEachClause creates a ForEachExecutor configured from a ForEachClause.
func FromForEachClause(clause *spec.ForEachClause, progressCallback RetryObserver) *ForEachExecutor {
	if clause == nil {
		return NewForEachExecutor()
	}
	return NewForEachExecutor(
		WithForEachConcurrency(clause.Concurrency),
		WithForEachOnError(clause.OnError),
		WithForEachProgressCallback(progressCallback),
	)
}

// ForEachExecuteFunc is the function signature for executing a single forEach iteration.
// It receives the execution context, the current item, and the iteration index.
// Returns the provider output and any error.
type ForEachExecuteFunc func(ctx context.Context, item any, index int) (*provider.Output, error)

// ExecuteResult contains the results of forEach execution.
type ExecuteResult struct {
	// Iterations contains results for each iteration
	Iterations []*ForEachIterationResult `json:"iterations" yaml:"iterations" doc:"Results for each iteration"`

	// TotalCount is the total number of iterations
	TotalCount int `json:"totalCount" yaml:"totalCount" doc:"Total number of iterations"`

	// SuccessCount is the number of successful iterations
	SuccessCount int `json:"successCount" yaml:"successCount" doc:"Number of successful iterations"`

	// FailureCount is the number of failed iterations
	FailureCount int `json:"failureCount" yaml:"failureCount" doc:"Number of failed iterations"`

	// AllSucceeded indicates if all iterations succeeded
	AllSucceeded bool `json:"allSucceeded" yaml:"allSucceeded" doc:"Whether all iterations succeeded"`

	// FirstError is the first error encountered (if any)
	FirstError error `json:"-" yaml:"-"`
}

// AggregatedResults returns all iteration results as a slice for __actions.name.results access.
func (r *ExecuteResult) AggregatedResults() []any {
	if r == nil || len(r.Iterations) == 0 {
		return []any{}
	}
	results := make([]any, len(r.Iterations))
	for i, iter := range r.Iterations {
		results[i] = iter.Results
	}
	return results
}

// Execute runs forEach iterations with concurrency control and error handling.
// Items is the array to iterate over, actionName is used for naming iterations.
func (e *ForEachExecutor) Execute(
	ctx context.Context,
	actionName string,
	items []any,
	execute ForEachExecuteFunc,
) (*ExecuteResult, error) {
	if len(items) == 0 {
		return &ExecuteResult{
			Iterations:   []*ForEachIterationResult{},
			TotalCount:   0,
			SuccessCount: 0,
			FailureCount: 0,
			AllSucceeded: true,
		}, nil
	}

	result := &ExecuteResult{
		Iterations: make([]*ForEachIterationResult, len(items)),
		TotalCount: len(items),
	}

	// Determine effective concurrency
	concurrency := e.concurrency
	if concurrency <= 0 || concurrency > len(items) {
		concurrency = len(items)
	}

	// Create semaphore for concurrency control
	sem := make(chan struct{}, concurrency)

	// Channel for errors when onError is "fail"
	errChan := make(chan error, 1)

	// Context with cancellation for early termination
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var mu sync.Mutex
	completed := 0

	for i, item := range items {
		// Check if we should abort due to previous error
		select {
		case <-execCtx.Done():
			// Mark remaining iterations as cancelled
			mu.Lock()
			for j := i; j < len(items); j++ {
				now := time.Now()
				result.Iterations[j] = &ForEachIterationResult{
					Index:     j,
					Name:      fmt.Sprintf("%s[%d]", actionName, j),
					Status:    StatusCancelled,
					StartTime: &now,
					EndTime:   &now,
				}
			}
			mu.Unlock()
			break
		default:
		}

		wg.Add(1)
		go func(idx int, itm any) {
			defer wg.Done()

			// Acquire semaphore
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-execCtx.Done():
				now := time.Now()
				mu.Lock()
				result.Iterations[idx] = &ForEachIterationResult{
					Index:     idx,
					Name:      fmt.Sprintf("%s[%d]", actionName, idx),
					Status:    StatusCancelled,
					StartTime: &now,
					EndTime:   &now,
				}
				mu.Unlock()
				return
			}

			// Execute the iteration
			iterResult := e.executeIteration(execCtx, actionName, idx, itm, execute)

			mu.Lock()
			result.Iterations[idx] = iterResult
			completed++

			// Report progress
			if e.progressCallback != nil {
				e.progressCallback.OnForEachProgress(actionName, completed, len(items))
			}
			mu.Unlock()

			// Check for error and abort if needed
			if iterResult.Status == StatusFailed || iterResult.Status == StatusTimeout {
				if e.onError.OrDefault() == spec.OnErrorFail {
					// Signal abort
					select {
					case errChan <- fmt.Errorf("iteration %d failed: %s", idx, iterResult.Error):
						cancel()
					default:
						// Error already sent
					}
				}
			}
		}(i, item)
	}

	// Wait for all iterations
	wg.Wait()
	close(errChan)

	// Calculate final statistics
	for _, iter := range result.Iterations {
		if iter == nil {
			continue
		}
		switch iter.Status {
		case StatusSucceeded:
			result.SuccessCount++
		case StatusFailed, StatusTimeout:
			result.FailureCount++
			if result.FirstError == nil && iter.Error != "" {
				result.FirstError = fmt.Errorf("iteration %d: %s", iter.Index, iter.Error)
			}
		case StatusSkipped:
			// Skipped iterations don't count as success or failure
		case StatusPending, StatusRunning, StatusCancelled:
			// These statuses shouldn't appear in final results, but handle them
		}
	}

	result.AllSucceeded = result.FailureCount == 0

	// Return error only if onError is "fail" and there were failures
	if !result.AllSucceeded && e.onError.OrDefault() == spec.OnErrorFail {
		return result, result.FirstError
	}

	return result, nil
}

// executeIteration runs a single forEach iteration.
func (e *ForEachExecutor) executeIteration(
	ctx context.Context,
	actionName string,
	index int,
	item any,
	execute ForEachExecuteFunc,
) *ForEachIterationResult {
	iterName := fmt.Sprintf("%s[%d]", actionName, index)
	now := time.Now()

	result := &ForEachIterationResult{
		Index:     index,
		Name:      iterName,
		Status:    StatusRunning,
		StartTime: &now,
	}

	// Execute the iteration
	output, err := execute(ctx, item, index)

	endTime := time.Now()
	result.EndTime = &endTime

	if ctx.Err() == context.DeadlineExceeded {
		result.Status = StatusTimeout
		result.Error = "iteration timed out"
		return result
	}

	if ctx.Err() == context.Canceled {
		result.Status = StatusCancelled
		return result
	}

	if err != nil {
		result.Status = StatusFailed
		result.Error = err.Error()
		return result
	}

	// Success
	result.Status = StatusSucceeded
	if output != nil {
		result.Results = output.Data
	}

	return result
}

// ExpandForEachItems evaluates the forEach.In expression and returns the items to iterate over.
// This is a convenience function that wraps evaluateForEachArray for external use.
func ExpandForEachItems(ctx context.Context, forEach *spec.ForEachClause, resolverData map[string]any) ([]any, error) {
	return evaluateForEachArray(ctx, forEach, resolverData)
}

// CreateIterationName creates the expanded name for a forEach iteration.
// Format: "baseName[index]" (e.g., "deploy[0]", "deploy[1]")
func CreateIterationName(baseName string, index int) string {
	return fmt.Sprintf("%s[%d]", baseName, index)
}

// IsForEachIteration checks if an action name represents a forEach iteration.
// Returns true if the name matches the pattern "baseName[index]".
func IsForEachIteration(name string) bool {
	// Check for pattern: ends with [N] where N is a number
	n := len(name)
	if n < 4 { // Minimum valid: "a[0]" = 4 chars
		return false
	}

	// Must end with ]
	if name[n-1] != ']' {
		return false
	}

	// Find the opening bracket and count digits
	bracketIdx := -1
	digitCount := 0
	for i := n - 2; i >= 0; i-- {
		if name[i] == '[' {
			bracketIdx = i
			break
		}
		// Must be a digit between [ and ]
		if name[i] < '0' || name[i] > '9' {
			return false
		}
		digitCount++
	}

	// Must have found an opening bracket with at least one char before it
	// and at least one digit between the brackets
	return bracketIdx > 0 && digitCount > 0
}

// ParseIterationName extracts the base name and index from a forEach iteration name.
// Returns the base name, index, and true if successful.
// Returns empty string, -1, false if not a valid iteration name.
func ParseIterationName(name string) (baseName string, index int, ok bool) {
	if !IsForEachIteration(name) {
		return "", -1, false
	}

	n := len(name)
	bracketIdx := -1
	for i := n - 2; i >= 0; i-- {
		if name[i] == '[' {
			bracketIdx = i
			break
		}
	}

	baseName = name[:bracketIdx]
	indexStr := name[bracketIdx+1 : n-1]

	// Parse index
	index = 0
	for _, c := range indexStr {
		index = index*10 + int(c-'0')
	}

	return baseName, index, true
}

// AggregateForEachResults aggregates forEach iteration results into an ActionResult.
// This creates the combined result that will be stored in __actions namespace.
func AggregateForEachResults(
	_ string, // actionName - reserved for future use
	iterations []*ForEachIterationResult,
	inputs map[string]any,
) *ActionResult {
	if len(iterations) == 0 {
		return &ActionResult{
			Inputs:  inputs,
			Results: []any{},
			Status:  StatusSucceeded,
		}
	}

	// Aggregate results
	results := make([]any, len(iterations))
	var startTime, endTime *time.Time
	var hasError bool
	var firstError string
	allSucceeded := true

	for i, iter := range iterations {
		if iter == nil {
			continue
		}
		results[i] = iter.Results

		// Track earliest start time
		if iter.StartTime != nil {
			if startTime == nil || iter.StartTime.Before(*startTime) {
				startTime = iter.StartTime
			}
		}

		// Track latest end time
		if iter.EndTime != nil {
			if endTime == nil || iter.EndTime.After(*endTime) {
				endTime = iter.EndTime
			}
		}

		// Track errors
		if iter.Status == StatusFailed || iter.Status == StatusTimeout {
			allSucceeded = false
			if !hasError {
				hasError = true
				firstError = iter.Error
			}
		} else if iter.Status != StatusSucceeded {
			allSucceeded = false
		}
	}

	// Determine overall status
	status := StatusSucceeded
	if !allSucceeded {
		status = StatusFailed
	}

	return &ActionResult{
		Inputs:    inputs,
		Results:   results,
		Status:    status,
		StartTime: startTime,
		EndTime:   endTime,
		Error:     firstError,
	}
}
