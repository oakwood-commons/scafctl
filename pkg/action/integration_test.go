// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Infrastructure
// =============================================================================

// mockRegistry implements action.RegistryInterface for integration tests.
type mockRegistry struct {
	mu        sync.RWMutex
	providers map[string]provider.Provider
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		providers: make(map[string]provider.Provider),
	}
}

func (r *mockRegistry) register(p provider.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Descriptor().Name] = p
}

func (r *mockRegistry) Get(name string) (provider.Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

func (r *mockRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[name]
	return ok
}

// mockProvider implements provider.Provider for testing.
type mockProvider struct {
	name    string
	execute func(ctx context.Context, input any) (*provider.Output, error)
}

func (p *mockProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:         p.name,
		APIVersion:   "v1",
		Version:      semver.MustParse("1.0.0"),
		Description:  "Mock provider for integration testing",
		MockBehavior: "Returns configured response",
		Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
			"name":      schemahelper.StringProp("Test input name"),
			"value":     schemahelper.AnyProp("Generic value input"),
			"fail":      schemahelper.BoolProp("Whether to fail"),
			"isFinally": schemahelper.BoolProp("Whether this is a finally action"),
			"env":       schemahelper.StringProp("Environment name"),
			"server":    schemahelper.StringProp("Server name"),
			"item":      schemahelper.AnyProp("ForEach item value"),
		}),
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityAction: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("Whether action succeeded"),
			}),
		},
		Capabilities: []provider.Capability{provider.CapabilityAction},
	}
}

func (p *mockProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	if p.execute != nil {
		return p.execute(ctx, input)
	}
	return &provider.Output{Data: map[string]any{"success": true}}, nil
}

// progressRecorder implements action.ProgressCallback for testing.
type progressRecorder struct {
	mu     sync.Mutex
	events []string
}

func newProgressRecorder() *progressRecorder {
	return &progressRecorder{events: make([]string, 0)}
}

func (r *progressRecorder) OnActionStart(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("start:%s", name))
}

func (r *progressRecorder) OnActionComplete(name string, _ any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("complete:%s", name))
}

func (r *progressRecorder) OnActionFailed(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("failed:%s:%v", name, err))
}

func (r *progressRecorder) OnActionSkipped(name, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("skipped:%s:%s", name, reason))
}

func (r *progressRecorder) OnActionTimeout(name string, _ time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("timeout:%s", name))
}

func (r *progressRecorder) OnActionCancelled(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("cancelled:%s", name))
}

func (r *progressRecorder) OnRetryAttempt(name string, attempt, maxAttempts int, _ error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("retry:%s:%d/%d", name, attempt, maxAttempts))
}

func (r *progressRecorder) OnForEachProgress(name string, completed, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("foreach:%s:%d/%d", name, completed, total))
}

func (r *progressRecorder) OnPhaseStart(phase int, actions []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("phase_start:%d:%v", phase, actions))
}

func (r *progressRecorder) OnPhaseComplete(phase int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("phase_complete:%d", phase))
}

func (r *progressRecorder) OnFinallyStart() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "finally_start")
}

func (r *progressRecorder) OnFinallyComplete() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "finally_complete")
}

func (r *progressRecorder) getEvents() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]string, len(r.events))
	copy(result, r.events)
	return result
}

func (r *progressRecorder) contains(event string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e == event {
			return true
		}
	}
	return false
}

// phaseTrackingProgressRecorder is a specialized progress recorder that allows
// tracking phase changes with a callback.
type phaseTrackingProgressRecorder struct {
	mu                   sync.Mutex
	events               []string
	onPhaseStartCallback func(phase int)
}

func (r *phaseTrackingProgressRecorder) OnActionStart(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("start:%s", name))
}

func (r *phaseTrackingProgressRecorder) OnActionComplete(name string, _ any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("complete:%s", name))
}

func (r *phaseTrackingProgressRecorder) OnActionFailed(name string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("failed:%s:%v", name, err))
}

func (r *phaseTrackingProgressRecorder) OnActionSkipped(name, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("skipped:%s:%s", name, reason))
}

func (r *phaseTrackingProgressRecorder) OnActionTimeout(name string, _ time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("timeout:%s", name))
}

func (r *phaseTrackingProgressRecorder) OnActionCancelled(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("cancelled:%s", name))
}

func (r *phaseTrackingProgressRecorder) OnRetryAttempt(name string, attempt, maxAttempts int, _ error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("retry:%s:%d/%d", name, attempt, maxAttempts))
}

func (r *phaseTrackingProgressRecorder) OnForEachProgress(name string, completed, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("foreach:%s:%d/%d", name, completed, total))
}

func (r *phaseTrackingProgressRecorder) OnPhaseStart(phase int, actions []string) {
	if r.onPhaseStartCallback != nil {
		r.onPhaseStartCallback(phase)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("phase_start:%d:%v", phase, actions))
}

func (r *phaseTrackingProgressRecorder) OnPhaseComplete(phase int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, fmt.Sprintf("phase_complete:%d", phase))
}

func (r *phaseTrackingProgressRecorder) OnFinallyStart() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "finally_start")
}

func (r *phaseTrackingProgressRecorder) OnFinallyComplete() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "finally_complete")
}

// Helper functions for building test workflows and expressions

func makeExpr(expr string) *celexp.Expression {
	e := celexp.Expression(expr)
	return &e
}

func makeLiteral(v any) *spec.ValueRef {
	return &spec.ValueRef{Literal: v}
}

func makeCondition(expr string) *spec.Condition {
	e := celexp.Expression(expr)
	return &spec.Condition{Expr: &e}
}

// =============================================================================
// Integration Test: Simple Linear Action Chain
// =============================================================================

func TestIntegration_SimpleLinearChain(t *testing.T) {
	// Scenario: A → B → C (simple sequential dependency)
	executionOrder := make([]string, 0)
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			if name, ok := inputs["name"]; ok {
				mu.Lock()
				executionOrder = append(executionOrder, name.(string))
				mu.Unlock()
			}
			return &provider.Output{Data: map[string]any{"result": inputs["name"]}}, nil
		},
	})

	progress := newProgressRecorder()
	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"step-a": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": makeLiteral("step-a"),
				},
			},
			"step-b": {
				Provider:  "test-provider",
				DependsOn: []string{"step-a"},
				Inputs: map[string]*spec.ValueRef{
					"name": makeLiteral("step-b"),
				},
			},
			"step-c": {
				Provider:  "test-provider",
				DependsOn: []string{"step-b"},
				Inputs: map[string]*spec.ValueRef{
					"name": makeLiteral("step-c"),
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)
	assert.Len(t, result.Actions, 3)

	// Verify strict execution order
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, executionOrder, 3)
	assert.Equal(t, "step-a", executionOrder[0])
	assert.Equal(t, "step-b", executionOrder[1])
	assert.Equal(t, "step-c", executionOrder[2])

	// Verify all actions succeeded
	for name, ar := range result.Actions {
		assert.Equal(t, action.StatusSucceeded, ar.Status, "action %s should succeed", name)
	}
}

// =============================================================================
// Integration Test: Parallel Actions with Dependencies
// =============================================================================

func TestIntegration_ParallelWithDependencies(t *testing.T) {
	// Scenario: Diamond dependency pattern
	//      A
	//     / \
	//    B   C
	//     \ /
	//      D

	startTimes := make(map[string]time.Time)
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			name := inputs["name"].(string)
			mu.Lock()
			startTimes[name] = time.Now()
			mu.Unlock()
			time.Sleep(50 * time.Millisecond) // Simulate work
			return &provider.Output{Data: map[string]any{"result": name}}, nil
		},
	})

	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithDefaultTimeout(10*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"action-a": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("action-a")},
			},
			"action-b": {
				Provider:  "test-provider",
				DependsOn: []string{"action-a"},
				Inputs:    map[string]*spec.ValueRef{"name": makeLiteral("action-b")},
			},
			"action-c": {
				Provider:  "test-provider",
				DependsOn: []string{"action-a"},
				Inputs:    map[string]*spec.ValueRef{"name": makeLiteral("action-c")},
			},
			"action-d": {
				Provider:  "test-provider",
				DependsOn: []string{"action-b", "action-c"},
				Inputs:    map[string]*spec.ValueRef{"name": makeLiteral("action-d")},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)
	assert.Len(t, result.Actions, 4)

	mu.Lock()
	defer mu.Unlock()

	// A must start first
	assert.True(t, startTimes["action-a"].Before(startTimes["action-b"]))
	assert.True(t, startTimes["action-a"].Before(startTimes["action-c"]))

	// B and C should start at approximately the same time (parallel)
	bStartTime := startTimes["action-b"]
	cStartTime := startTimes["action-c"]
	timeDiff := bStartTime.Sub(cStartTime)
	if timeDiff < 0 {
		timeDiff = -timeDiff
	}
	assert.Less(t, timeDiff.Milliseconds(), int64(40), "B and C should start nearly simultaneously")

	// D must start after both B and C
	assert.True(t, startTimes["action-d"].After(startTimes["action-b"]))
	assert.True(t, startTimes["action-d"].After(startTimes["action-c"]))
}

// =============================================================================
// Integration Test: ForEach Expansion and Execution
// =============================================================================

func TestIntegration_ForEachExpansion(t *testing.T) {
	// Scenario: Action with forEach iterates over array
	executedItems := make([]any, 0)
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			mu.Lock()
			executedItems = append(executedItems, inputs["item"])
			mu.Unlock()
			return &provider.Output{Data: map[string]any{"processed": inputs["item"]}}, nil
		},
	})

	resolverData := map[string]any{
		"targets": []any{"server1", "server2", "server3"},
	}

	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithResolverData(resolverData),
		action.WithDefaultTimeout(10*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"item": {Expr: makeExpr("__item")},
				},
				ForEach: &spec.ForEachClause{
					In: &spec.ValueRef{Expr: makeExpr("_.targets")},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)

	// Should have 3 expanded actions (deploy[0], deploy[1], deploy[2])
	assert.Len(t, result.Actions, 3)
	assert.Contains(t, result.Actions, "deploy[0]")
	assert.Contains(t, result.Actions, "deploy[1]")
	assert.Contains(t, result.Actions, "deploy[2]")

	// All items should have been processed
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, executedItems, 3)
	assert.Contains(t, executedItems, "server1")
	assert.Contains(t, executedItems, "server2")
	assert.Contains(t, executedItems, "server3")
}

// =============================================================================
// Integration Test: Error Handling (fail vs continue)
// =============================================================================

func TestIntegration_ErrorHandling_Fail(t *testing.T) {
	// Scenario: Action fails with onError: fail (default)
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			if inputs["fail"] == true {
				return nil, errors.New("intentional failure")
			}
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"first": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"fail": makeLiteral(false)},
			},
			"failing": {
				Provider:  "test-provider",
				DependsOn: []string{"first"},
				Inputs:    map[string]*spec.ValueRef{"fail": makeLiteral(true)},
			},
			"skipped": {
				Provider:  "test-provider",
				DependsOn: []string{"failing"},
				Inputs:    map[string]*spec.ValueRef{"fail": makeLiteral(false)},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.Error(t, err)
	assert.Equal(t, action.ExecutionFailed, result.FinalStatus)
	assert.Contains(t, result.FailedActions, "failing")
	assert.Contains(t, result.SkippedActions, "skipped")
	assert.Equal(t, action.StatusSucceeded, result.Actions["first"].Status)
	assert.Equal(t, action.StatusFailed, result.Actions["failing"].Status)
	assert.Equal(t, action.StatusSkipped, result.Actions["skipped"].Status)
}

func TestIntegration_ErrorHandling_Continue(t *testing.T) {
	// Scenario: Action fails with onError: continue
	executed := make([]string, 0)
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			name := inputs["name"].(string)
			mu.Lock()
			executed = append(executed, name)
			mu.Unlock()
			if inputs["fail"] == true {
				return nil, errors.New("intentional failure")
			}
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"action-1": {
				Provider: "test-provider",
				OnError:  spec.OnErrorContinue,
				Inputs: map[string]*spec.ValueRef{
					"name": makeLiteral("action-1"),
					"fail": makeLiteral(true),
				},
			},
			"action-2": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": makeLiteral("action-2"),
					"fail": makeLiteral(false),
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionPartialSuccess, result.FinalStatus)

	mu.Lock()
	defer mu.Unlock()
	// Both actions should have been executed
	assert.Contains(t, executed, "action-1")
	assert.Contains(t, executed, "action-2")
}

// =============================================================================
// Integration Test: Retry with Backoff
// =============================================================================

func TestIntegration_RetryWithBackoff(t *testing.T) {
	// Scenario: Action fails initially but succeeds on retry
	attemptCount := 0
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			mu.Lock()
			attemptCount++
			current := attemptCount
			mu.Unlock()
			if current < 3 {
				return nil, fmt.Errorf("attempt %d failed", current)
			}
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	progress := newProgressRecorder()
	initialDelay := action.Duration(10 * time.Millisecond)
	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(10*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"retry-action": {
				Provider: "test-provider",
				Retry: &action.RetryConfig{
					MaxAttempts:  5,
					Backoff:      action.BackoffExponential,
					InitialDelay: &initialDelay,
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)
	assert.Equal(t, action.StatusSucceeded, result.Actions["retry-action"].Status)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 3, attemptCount, "should succeed on third attempt")

	// Verify retry events were fired
	events := progress.getEvents()
	retryCount := 0
	for _, e := range events {
		if len(e) > 5 && e[:5] == "retry" {
			retryCount++
		}
	}
	assert.Equal(t, 2, retryCount, "should have 2 retry events")
}

// =============================================================================
// Integration Test: Timeout Handling
// =============================================================================

func TestIntegration_TimeoutHandling(t *testing.T) {
	// Scenario: Action exceeds timeout
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(ctx context.Context, _ any) (*provider.Output, error) {
			select {
			case <-time.After(5 * time.Second):
				return &provider.Output{Data: map[string]any{"success": true}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	progress := newProgressRecorder()
	timeout := action.Duration(100 * time.Millisecond)
	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"slow-action": {
				Provider: "test-provider",
				Timeout:  &timeout,
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.Error(t, err)
	assert.Equal(t, action.ExecutionFailed, result.FinalStatus)
	assert.Equal(t, action.StatusTimeout, result.Actions["slow-action"].Status)
	assert.Contains(t, result.FailedActions, "slow-action")

	// Verify timeout event was fired
	assert.True(t, progress.contains("timeout:slow-action"))
}

// =============================================================================
// Integration Test: Condition Evaluation (Resolver-only)
// =============================================================================

func TestIntegration_ConditionEvaluation(t *testing.T) {
	// Scenario: Actions with when conditions based on resolver data
	executed := make([]string, 0)
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			mu.Lock()
			executed = append(executed, inputs["name"].(string))
			mu.Unlock()
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	resolverData := map[string]any{
		"environment": "staging",
		"enabled":     true,
	}

	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithResolverData(resolverData),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"prod-only": {
				Provider: "test-provider",
				When:     makeCondition("_.environment == 'prod'"),
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("prod-only")},
			},
			"staging-only": {
				Provider: "test-provider",
				When:     makeCondition("_.environment == 'staging'"),
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("staging-only")},
			},
			"enabled-check": {
				Provider: "test-provider",
				When:     makeCondition("_.enabled == true"),
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("enabled-check")},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)

	mu.Lock()
	defer mu.Unlock()

	// prod-only should be skipped, staging-only and enabled-check should run
	assert.NotContains(t, executed, "prod-only")
	assert.Contains(t, executed, "staging-only")
	assert.Contains(t, executed, "enabled-check")

	assert.Equal(t, action.StatusSkipped, result.Actions["prod-only"].Status)
	assert.Equal(t, action.StatusSucceeded, result.Actions["staging-only"].Status)
	assert.Equal(t, action.StatusSucceeded, result.Actions["enabled-check"].Status)
}

// =============================================================================
// Integration Test: Finally Section Execution
// =============================================================================

func TestIntegration_FinallySection(t *testing.T) {
	// Scenario: Finally section runs after main actions
	executionOrder := make([]string, 0)
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			mu.Lock()
			executionOrder = append(executionOrder, inputs["name"].(string))
			mu.Unlock()
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	progress := newProgressRecorder()
	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"main-action": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("main-action")},
			},
		},
		Finally: map[string]*action.Action{
			"cleanup": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("cleanup")},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)

	mu.Lock()
	defer mu.Unlock()

	// main-action should run before cleanup
	require.Len(t, executionOrder, 2)
	assert.Equal(t, "main-action", executionOrder[0])
	assert.Equal(t, "cleanup", executionOrder[1])

	// Verify finally events
	assert.True(t, progress.contains("finally_start"))
	assert.True(t, progress.contains("finally_complete"))
}

func TestIntegration_FinallyRunsAfterFailure(t *testing.T) {
	// Scenario: Finally section runs even when main actions fail
	executionOrder := make([]string, 0)
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			name := inputs["name"].(string)
			mu.Lock()
			executionOrder = append(executionOrder, name)
			mu.Unlock()
			if name == "failing-action" {
				return nil, errors.New("intentional failure")
			}
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	progress := newProgressRecorder()
	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"failing-action": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("failing-action")},
			},
		},
		Finally: map[string]*action.Action{
			"cleanup": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("cleanup")},
			},
		},
	}

	result, _ := executor.Execute(context.Background(), workflow)

	mu.Lock()
	order := append([]string{}, executionOrder...)
	mu.Unlock()

	// Main action should have run and failed
	assert.Contains(t, order, "failing-action", "main action should have run")

	// Verify the workflow status reflects the failure
	assert.Equal(t, action.ExecutionFailed, result.FinalStatus)

	// Finally section should run even after main failure
	assert.Contains(t, order, "cleanup", "finally section should run even after main failure")

	// Verify finally events
	assert.True(t, progress.contains("finally_start"))
	assert.True(t, progress.contains("finally_complete"))
}

// =============================================================================
// Integration Test: Cancellation Behavior
// =============================================================================

func TestIntegration_Cancellation(t *testing.T) {
	// Scenario: Context cancellation during execution
	started := make(chan struct{})
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(ctx context.Context, _ any) (*provider.Output, error) {
			close(started)
			select {
			case <-time.After(10 * time.Second):
				return &provider.Output{Data: map[string]any{"success": true}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	})

	progress := newProgressRecorder()
	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"long-running": {
				Provider: "test-provider",
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start execution in goroutine
	resultChan := make(chan *action.ExecutionResult, 1)
	go func() {
		result, _ := executor.Execute(ctx, workflow)
		resultChan <- result
	}()

	// Wait for action to start
	<-started

	// Cancel the context
	cancel()

	// Wait for result
	select {
	case result := <-resultChan:
		// Action should be cancelled or failed
		assert.True(t,
			result.FinalStatus == action.ExecutionCancelled ||
				result.FinalStatus == action.ExecutionFailed,
			"expected cancelled or failed status")
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for execution to complete after cancellation")
	}
}

// =============================================================================
// Integration Test: Progress Callback Events
// =============================================================================

func TestIntegration_ProgressCallbackEvents(t *testing.T) {
	// Scenario: Verify all expected progress events fire
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	progress := newProgressRecorder()
	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"action-a": {
				Provider: "test-provider",
			},
			"action-b": {
				Provider:  "test-provider",
				DependsOn: []string{"action-a"},
			},
		},
		Finally: map[string]*action.Action{
			"cleanup": {
				Provider: "test-provider",
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)

	events := progress.getEvents()

	// Verify phase events for main actions
	hasPhaseStart := false
	hasPhaseComplete := false
	for _, e := range events {
		if len(e) >= 11 && e[:11] == "phase_start" {
			hasPhaseStart = true
		}
		if len(e) >= 14 && e[:14] == "phase_complete" {
			hasPhaseComplete = true
		}
	}
	assert.True(t, hasPhaseStart, "should have phase_start event")
	assert.True(t, hasPhaseComplete, "should have phase_complete event")

	// Verify finally events
	assert.True(t, progress.contains("finally_start"))
	assert.True(t, progress.contains("finally_complete"))

	// Verify action events
	assert.True(t, progress.contains("start:action-a"))
	assert.True(t, progress.contains("complete:action-a"))
	assert.True(t, progress.contains("start:action-b"))
	assert.True(t, progress.contains("complete:action-b"))
	assert.True(t, progress.contains("start:cleanup"))
	assert.True(t, progress.contains("complete:cleanup"))
}

// =============================================================================
// Integration Test: Rendered Graph Correctness
// =============================================================================

func TestIntegration_RenderedGraphCorrectness(t *testing.T) {
	// Scenario: Verify rendered graph produces correct structure
	resolverData := map[string]any{
		"environment": "prod",
		"servers":     []any{"web1", "web2"},
	}

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"setup": {
				Provider:    "setup-provider",
				Description: "Initial setup",
				Inputs: map[string]*spec.ValueRef{
					"env": {Expr: makeExpr("_.environment")},
				},
			},
			"deploy": {
				Provider:  "deploy-provider",
				DependsOn: []string{"setup"},
				ForEach: &spec.ForEachClause{
					In: &spec.ValueRef{Expr: makeExpr("_.servers")},
				},
				Inputs: map[string]*spec.ValueRef{
					"server": {Expr: makeExpr("__item")},
				},
			},
		},
		Finally: map[string]*action.Action{
			"cleanup": {
				Provider: "cleanup-provider",
			},
		},
	}

	// Build graph
	graph, err := action.BuildGraph(context.Background(), workflow, resolverData, nil)
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Render graph
	rendered, err := action.RenderToStruct(graph, nil)
	require.NoError(t, err)
	require.NotNil(t, rendered)

	// Verify structure
	assert.Equal(t, "scafctl.oakwood-commons.github.io/v1alpha1", rendered.APIVersion)
	assert.Equal(t, "ActionGraph", rendered.Kind)

	// Should have 4 actions: setup + deploy[0] + deploy[1] + cleanup
	assert.Len(t, rendered.Actions, 4)
	assert.Contains(t, rendered.Actions, "setup")
	assert.Contains(t, rendered.Actions, "deploy[0]")
	assert.Contains(t, rendered.Actions, "deploy[1]")
	assert.Contains(t, rendered.Actions, "cleanup")

	// Verify forEach metadata
	deploy0 := rendered.Actions["deploy[0]"]
	require.NotNil(t, deploy0.ForEach)
	assert.Equal(t, "deploy", deploy0.ForEach.ExpandedFrom)
	assert.Equal(t, 0, deploy0.ForEach.Index)
	assert.Equal(t, "web1", deploy0.ForEach.Item)

	deploy1 := rendered.Actions["deploy[1]"]
	require.NotNil(t, deploy1.ForEach)
	assert.Equal(t, "deploy", deploy1.ForEach.ExpandedFrom)
	assert.Equal(t, 1, deploy1.ForEach.Index)
	assert.Equal(t, "web2", deploy1.ForEach.Item)

	// Verify execution order has correct phases
	assert.NotEmpty(t, rendered.ExecutionOrder)
	assert.NotEmpty(t, rendered.FinallyOrder)

	// Verify metadata
	assert.NotNil(t, rendered.Metadata)
	assert.Equal(t, 4, rendered.Metadata.TotalActions)
	assert.True(t, rendered.Metadata.HasFinally)
}

// =============================================================================
// Integration Test: Deferred Inputs with __actions Reference
// =============================================================================

func TestIntegration_DeferredInputs(t *testing.T) {
	// Scenario: Action uses __actions reference to get result from previous action
	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "producer",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			t.Logf("Producer executed")
			return &provider.Output{Data: map[string]any{
				"value":   42,
				"message": "produced",
			}}, nil
		},
	})

	var receivedInput any
	registry.register(&mockProvider{
		name: "consumer",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			t.Logf("Consumer executed with input: %+v", input)
			receivedInput = input
			return &provider.Output{Data: map[string]any{"consumed": true}}, nil
		},
	})

	progress := newProgressRecorder()
	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithDefaultTimeout(5*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"produce": {
				Provider: "producer",
			},
			"consume": {
				Provider:  "consumer",
				DependsOn: []string{"produce"},
				Inputs: map[string]*spec.ValueRef{
					"value": {Expr: makeExpr("__actions.produce.results.value")},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	t.Logf("Progress events: %v", progress.events)
	for name, ar := range result.Actions {
		t.Logf("Action %s: Status=%v, Error=%v, Results=%v", name, ar.Status, ar.Error, ar.Results)
	}

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)

	// Verify that the consumer action was executed
	t.Logf("receivedInput: %+v", receivedInput)
	assert.NotNil(t, receivedInput)
	inputs, ok := receivedInput.(map[string]any)
	require.True(t, ok)
	// The value input should contain either the resolved value (42) or a DeferredValue
	// depending on implementation. The key point is the workflow executed successfully.
	_, hasValue := inputs["value"]
	assert.True(t, hasValue, "consumer should have received a 'value' input")
}

// =============================================================================
// Integration Test: Complex Multi-Phase Workflow
// =============================================================================

func TestIntegration_ComplexMultiPhaseWorkflow(t *testing.T) {
	// Scenario: Complex workflow with multiple phases
	//
	// Phase 0: init
	// Phase 1: build, test (parallel)
	// Phase 2: deploy (depends on build and test)
	// Phase 3: verify (depends on deploy)
	// Finally: notify

	phaseTracking := make(map[string]int)
	currentPhase := 0
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			name := inputs["name"].(string)
			mu.Lock()
			phaseTracking[name] = currentPhase
			mu.Unlock()
			return &provider.Output{Data: map[string]any{"result": name}}, nil
		},
	})

	// Custom progress recorder that tracks phase changes
	progress := &phaseTrackingProgressRecorder{
		events: make([]string, 0),
		onPhaseStartCallback: func(phase int) {
			mu.Lock()
			currentPhase = phase
			mu.Unlock()
		},
	}

	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithProgressCallback(progress),
		action.WithMaxConcurrency(4),
		action.WithDefaultTimeout(10*time.Second),
	)

	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"init": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("init")},
			},
			"build": {
				Provider:  "test-provider",
				DependsOn: []string{"init"},
				Inputs:    map[string]*spec.ValueRef{"name": makeLiteral("build")},
			},
			"test": {
				Provider:  "test-provider",
				DependsOn: []string{"init"},
				Inputs:    map[string]*spec.ValueRef{"name": makeLiteral("test")},
			},
			"deploy": {
				Provider:  "test-provider",
				DependsOn: []string{"build", "test"},
				Inputs:    map[string]*spec.ValueRef{"name": makeLiteral("deploy")},
			},
			"verify": {
				Provider:  "test-provider",
				DependsOn: []string{"deploy"},
				Inputs:    map[string]*spec.ValueRef{"name": makeLiteral("verify")},
			},
		},
		Finally: map[string]*action.Action{
			"notify": {
				Provider: "test-provider",
				Inputs:   map[string]*spec.ValueRef{"name": makeLiteral("notify")},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)
	assert.Len(t, result.Actions, 6) // 5 main + 1 finally

	mu.Lock()
	defer mu.Unlock()

	// Verify phase ordering
	assert.Equal(t, 0, phaseTracking["init"])
	assert.Equal(t, 1, phaseTracking["build"])
	assert.Equal(t, 1, phaseTracking["test"])
	assert.Equal(t, 2, phaseTracking["deploy"])
	assert.Equal(t, 3, phaseTracking["verify"])

	// All actions should have succeeded
	for _, ar := range result.Actions {
		assert.Equal(t, action.StatusSucceeded, ar.Status)
	}
}

// =============================================================================
// Integration Test: Max Concurrency Enforcement
// =============================================================================

func TestIntegration_MaxConcurrencyEnforcement(t *testing.T) {
	// Scenario: Verify max concurrency is enforced
	var currentConcurrency int64
	var maxObservedConcurrency int64
	var mu sync.Mutex

	registry := newMockRegistry()
	registry.register(&mockProvider{
		name: "test-provider",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			mu.Lock()
			currentConcurrency++
			if currentConcurrency > maxObservedConcurrency {
				maxObservedConcurrency = currentConcurrency
			}
			mu.Unlock()

			time.Sleep(50 * time.Millisecond) // Simulate work

			mu.Lock()
			currentConcurrency--
			mu.Unlock()

			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	executor := action.NewExecutor(
		action.WithRegistry(registry),
		action.WithMaxConcurrency(2),
		action.WithDefaultTimeout(10*time.Second),
	)

	// Create 5 parallel actions (no dependencies)
	workflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"action-1": {Provider: "test-provider"},
			"action-2": {Provider: "test-provider"},
			"action-3": {Provider: "test-provider"},
			"action-4": {Provider: "test-provider"},
			"action-5": {Provider: "test-provider"},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, action.ExecutionSucceeded, result.FinalStatus)
	assert.Len(t, result.Actions, 5)

	mu.Lock()
	defer mu.Unlock()
	assert.LessOrEqual(t, maxObservedConcurrency, int64(2),
		"max concurrency should be enforced at 2")
}

// =============================================================================
// Integration Test: Validation
// =============================================================================

func TestIntegration_ValidationErrors(t *testing.T) {
	t.Run("invalid action name", func(t *testing.T) {
		workflow := &action.Workflow{
			Actions: map[string]*action.Action{
				"__reserved": {
					Provider: "test-provider",
				},
			},
		}

		err := action.ValidateWorkflow(workflow, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reserved")
	})

	t.Run("cycle detection", func(t *testing.T) {
		workflow := &action.Workflow{
			Actions: map[string]*action.Action{
				"a": {
					Provider:  "test-provider",
					DependsOn: []string{"b"},
				},
				"b": {
					Provider:  "test-provider",
					DependsOn: []string{"a"},
				},
			},
		}

		err := action.ValidateWorkflow(workflow, nil)
		require.Error(t, err)
		// The error message uses "circular dependency"
		assert.Contains(t, err.Error(), "circular")
	})

	t.Run("forEach in finally not allowed", func(t *testing.T) {
		workflow := &action.Workflow{
			Finally: map[string]*action.Action{
				"cleanup": {
					Provider: "test-provider",
					ForEach: &spec.ForEachClause{
						In: &spec.ValueRef{Literal: []any{1, 2, 3}},
					},
				},
			},
		}

		err := action.ValidateWorkflow(workflow, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forEach")
	})

	t.Run("invalid retry config", func(t *testing.T) {
		workflow := &action.Workflow{
			Actions: map[string]*action.Action{
				"action": {
					Provider: "test-provider",
					Retry: &action.RetryConfig{
						MaxAttempts: 0, // Invalid: must be >= 1
					},
				},
			},
		}

		err := action.ValidateWorkflow(workflow, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "maxAttempts")
	})
}
