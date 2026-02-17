// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"sync"
	"time"
)

// Context manages the __actions namespace during workflow execution.
// It provides thread-safe storage for action results and supports forEach
// iteration result aggregation.
type Context struct {
	mu sync.RWMutex

	// actions stores results keyed by action name
	actions map[string]*ActionResult

	// iterations stores forEach iteration results keyed by base action name
	iterations map[string][]*ForEachIterationResult
}

// NewContext creates a new action context for workflow execution.
func NewContext() *Context {
	return &Context{
		actions:    make(map[string]*ActionResult),
		iterations: make(map[string][]*ForEachIterationResult),
	}
}

// SetResult stores an action's result in the context.
// This is called after an action completes (success, failure, or skip).
func (c *Context) SetResult(name string, result *ActionResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.actions[name] = result
}

// GetResult retrieves an action's result by name.
// Returns the result and true if found, nil and false otherwise.
func (c *Context) GetResult(name string) (*ActionResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.actions[name]
	return result, ok
}

// HasResult checks if a result exists for the given action name.
func (c *Context) HasResult(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.actions[name]
	return ok
}

// GetNamespace returns the __actions map for CEL/template evaluation.
// The returned map contains action results in a format suitable for
// expression evaluation (e.g., __actions.build.results.exitCode).
func (c *Context) GetNamespace() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	namespace := make(map[string]any, len(c.actions))
	for name, result := range c.actions {
		namespace[name] = actionResultToMap(result)
	}
	return namespace
}

// actionResultToMap converts an ActionResult to a map for CEL/template access.
func actionResultToMap(r *ActionResult) map[string]any {
	if r == nil {
		return nil
	}
	m := map[string]any{
		"inputs":  r.Inputs,
		"results": r.Results,
		"status":  string(r.Status),
	}
	if r.SkipReason != "" {
		m["skipReason"] = string(r.SkipReason)
	}
	if r.StartTime != nil {
		m["startTime"] = r.StartTime.Format(time.RFC3339)
	}
	if r.EndTime != nil {
		m["endTime"] = r.EndTime.Format(time.RFC3339)
	}
	if r.Error != "" {
		m["error"] = r.Error
	}
	return m
}

// AddIteration records a forEach iteration result.
// Results are stored in order by index.
func (c *Context) AddIteration(actionName string, result *ForEachIterationResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.iterations[actionName] = append(c.iterations[actionName], result)
}

// GetIterations retrieves all iteration results for a forEach action.
func (c *Context) GetIterations(actionName string) []*ForEachIterationResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	results := c.iterations[actionName]
	if results == nil {
		return nil
	}

	// Return a copy to avoid concurrent modification
	copied := make([]*ForEachIterationResult, len(results))
	copy(copied, results)
	return copied
}

// FinalizeForEach aggregates forEach iteration results into a single ActionResult.
// This should be called after all iterations complete to create the combined result.
func (c *Context) FinalizeForEach(actionName string, inputs map[string]any) *ActionResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	iterations := c.iterations[actionName]
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

	result := &ActionResult{
		Inputs:    inputs,
		Results:   results,
		Status:    status,
		StartTime: startTime,
		EndTime:   endTime,
		Error:     firstError,
	}

	// Store the aggregated result
	c.actions[actionName] = result

	return result
}

// MarkRunning marks an action as running with the current time.
func (c *Context) MarkRunning(name string, inputs map[string]any) {
	now := time.Now()
	c.SetResult(name, &ActionResult{
		Inputs:    inputs,
		Status:    StatusRunning,
		StartTime: &now,
	})
}

// MarkSucceeded marks an action as successfully completed.
func (c *Context) MarkSucceeded(name string, results any) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if existing, ok := c.actions[name]; ok {
		existing.Results = results
		existing.Status = StatusSucceeded
		existing.EndTime = &now
	} else {
		c.actions[name] = &ActionResult{
			Results:   results,
			Status:    StatusSucceeded,
			StartTime: &now,
			EndTime:   &now,
		}
	}
}

// MarkStreamed marks an action as having streamed its output to the terminal.
func (c *Context) MarkStreamed(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if existing, ok := c.actions[name]; ok {
		existing.Streamed = true
	}
}

// MarkFailed marks an action as failed with an error message.
func (c *Context) MarkFailed(name, errMsg string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if existing, ok := c.actions[name]; ok {
		existing.Status = StatusFailed
		existing.EndTime = &now
		existing.Error = errMsg
	} else {
		c.actions[name] = &ActionResult{
			Status:    StatusFailed,
			StartTime: &now,
			EndTime:   &now,
			Error:     errMsg,
		}
	}
}

// MarkSkipped marks an action as skipped with a reason.
func (c *Context) MarkSkipped(name string, reason SkipReason) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.actions[name] = &ActionResult{
		Status:     StatusSkipped,
		SkipReason: reason,
		StartTime:  &now,
		EndTime:    &now,
	}
}

// MarkTimeout marks an action as timed out.
func (c *Context) MarkTimeout(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if existing, ok := c.actions[name]; ok {
		existing.Status = StatusTimeout
		existing.EndTime = &now
		existing.Error = "action timed out"
	} else {
		c.actions[name] = &ActionResult{
			Status:    StatusTimeout,
			StartTime: &now,
			EndTime:   &now,
			Error:     "action timed out",
		}
	}
}

// MarkCancelled marks an action as cancelled.
func (c *Context) MarkCancelled(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	if existing, ok := c.actions[name]; ok {
		existing.Status = StatusCancelled
		existing.EndTime = &now
	} else {
		c.actions[name] = &ActionResult{
			Status:    StatusCancelled,
			StartTime: &now,
			EndTime:   &now,
		}
	}
}

// AllActionNames returns all action names that have results.
func (c *Context) AllActionNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	names := make([]string, 0, len(c.actions))
	for name := range c.actions {
		names = append(names, name)
	}
	return names
}

// ActionCount returns the number of actions with results.
func (c *Context) ActionCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.actions)
}

// Reset clears all stored results and iterations.
func (c *Context) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.actions = make(map[string]*ActionResult)
	c.iterations = make(map[string][]*ForEachIterationResult)
}

// Clone creates a deep copy of the action context.
// Useful for testing or creating snapshots.
func (c *Context) Clone() *Context {
	c.mu.RLock()
	defer c.mu.RUnlock()

	clone := NewContext()

	for name, result := range c.actions {
		// Create a copy of the result
		clonedResult := &ActionResult{
			Inputs:     copyMap(result.Inputs),
			Results:    result.Results, // Shallow copy for results
			Status:     result.Status,
			SkipReason: result.SkipReason,
			Error:      result.Error,
		}
		if result.StartTime != nil {
			t := *result.StartTime
			clonedResult.StartTime = &t
		}
		if result.EndTime != nil {
			t := *result.EndTime
			clonedResult.EndTime = &t
		}
		clone.actions[name] = clonedResult
	}

	for name, iterations := range c.iterations {
		clonedIterations := make([]*ForEachIterationResult, len(iterations))
		for i, iter := range iterations {
			clonedIter := &ForEachIterationResult{
				Index:   iter.Index,
				Name:    iter.Name,
				Results: iter.Results,
				Status:  iter.Status,
				Error:   iter.Error,
			}
			if iter.StartTime != nil {
				t := *iter.StartTime
				clonedIter.StartTime = &t
			}
			if iter.EndTime != nil {
				t := *iter.EndTime
				clonedIter.EndTime = &t
			}
			clonedIterations[i] = clonedIter
		}
		clone.iterations[name] = clonedIterations
	}

	return clone
}

// copyMap creates a shallow copy of a map.
func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	copied := make(map[string]any, len(m))
	for k, v := range m {
		copied[k] = v
	}
	return copied
}
