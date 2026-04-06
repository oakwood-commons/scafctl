// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExecutor(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		executor := NewExecutor()
		assert.NotNil(t, executor)
		assert.NotNil(t, executor.actionContext)
		assert.Equal(t, 30*time.Second, executor.gracePeriod)
		assert.Equal(t, 5*time.Minute, executor.defaultTimeout)

		cwd, _ := os.Getwd()
		assert.Equal(t, cwd, executor.cwd)
	})

	t.Run("with options", func(t *testing.T) {
		registry := newExecMockRegistry()
		callback := &recordingProgressCallback{}
		resolverData := map[string]any{"key": "value"}

		executor := NewExecutor(
			WithRegistry(registry),
			WithResolverData(resolverData),
			WithProgressCallback(callback),
			WithMaxConcurrency(4),
			WithGracePeriod(10*time.Second),
			WithDefaultTimeout(2*time.Minute),
		)

		assert.Equal(t, registry, executor.registry)
		assert.Equal(t, resolverData, executor.resolverData)
		assert.Equal(t, callback, executor.progressCallback)
		assert.Equal(t, 4, executor.maxConcurrency)
		assert.Equal(t, 10*time.Second, executor.gracePeriod)
		assert.Equal(t, 2*time.Minute, executor.defaultTimeout)
	})

	t.Run("WithCwd overrides default", func(t *testing.T) {
		executor := NewExecutor(WithCwd("/custom/dir"))
		assert.Equal(t, "/custom/dir", executor.cwd)
	})
}

func TestExecutor_BuildAdditionalVars(t *testing.T) {
	t.Run("includes __cwd", func(t *testing.T) {
		executor := NewExecutor(WithCwd("/test/cwd"))
		vars := executor.buildAdditionalVars(nil)

		assert.Equal(t, "/test/cwd", vars["__cwd"])
		assert.Contains(t, vars, "__actions")
	})

	t.Run("includes __cwd alongside aliases", func(t *testing.T) {
		executor := NewExecutor(WithCwd("/project/root"))
		executor.actionContext.MarkSucceeded("step1", map[string]any{"output": "val"})

		aliases := map[string]string{"s1": "step1"}
		vars := executor.buildAdditionalVars(aliases)

		assert.Equal(t, "/project/root", vars["__cwd"])
		assert.Contains(t, vars, "__actions")
		assert.Contains(t, vars, "s1")
	})

	t.Run("__cwd is empty when not set", func(t *testing.T) {
		executor := &Executor{
			actionContext: NewContext(),
		}
		vars := executor.buildAdditionalVars(nil)

		assert.Equal(t, "", vars["__cwd"])
	})
}

func TestExecutor_Execute_NilWorkflow(t *testing.T) {
	executor := NewExecutor()
	result, err := executor.Execute(context.Background(), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow cannot be nil")
	assert.Nil(t, result)
}

func TestExecutor_Execute_EmptyWorkflow(t *testing.T) {
	executor := NewExecutor()
	result, err := executor.Execute(context.Background(), &Workflow{})

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.Empty(t, result.FailedActions)
	assert.Empty(t, result.SkippedActions)
}

func TestExecutor_Execute_SingleAction(t *testing.T) {
	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			return &provider.Output{Data: map[string]any{"result": "success"}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"test-action": {
				Provider: "test-provider",
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.Len(t, result.Actions, 1)
	assert.Contains(t, result.Actions, "test-action")
	assert.Equal(t, StatusSucceeded, result.Actions["test-action"].Status)
}

func TestExecutor_Execute_ActionChain(t *testing.T) {
	executionOrder := make([]string, 0)
	var mu sync.Mutex

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			// Extract action name from inputs for tracking
			inputs, ok := input.(map[string]any)
			if ok {
				if name, exists := inputs["name"]; exists {
					mu.Lock()
					executionOrder = append(executionOrder, name.(string))
					mu.Unlock()
				}
			}
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"action-a": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "action-a"},
				},
			},
			"action-b": {
				Provider:  "test-provider",
				DependsOn: []string{"action-a"},
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "action-b"},
				},
			},
			"action-c": {
				Provider:  "test-provider",
				DependsOn: []string{"action-b"},
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "action-c"},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.Len(t, result.Actions, 3)

	// Verify execution order (a before b before c)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, executionOrder, 3)
	assert.Equal(t, "action-a", executionOrder[0])
	assert.Equal(t, "action-b", executionOrder[1])
	assert.Equal(t, "action-c", executionOrder[2])
}

func TestExecutor_Execute_ParallelActions(t *testing.T) {
	started := make(chan string, 3)
	done := make(chan struct{})

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, ok := input.(map[string]any)
			if ok {
				if name, exists := inputs["name"]; exists {
					started <- name.(string)
				}
			}
			<-done // Wait until test signals completion
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"action-a": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "action-a"},
				},
			},
			"action-b": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "action-b"},
				},
			},
			"action-c": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "action-c"},
				},
			},
		},
	}

	// Start execution in goroutine
	resultChan := make(chan *ExecutionResult)
	errChan := make(chan error)
	go func() {
		result, err := executor.Execute(context.Background(), workflow)
		if err != nil {
			errChan <- err
			return
		}
		resultChan <- result
	}()

	// Wait for all actions to start
	startedActions := make(map[string]bool)
	for i := 0; i < 3; i++ {
		select {
		case name := <-started:
			startedActions[name] = true
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for actions to start")
		}
	}

	// Verify all started concurrently
	assert.True(t, startedActions["action-a"])
	assert.True(t, startedActions["action-b"])
	assert.True(t, startedActions["action-c"])

	// Signal completion
	close(done)

	// Wait for result
	select {
	case result := <-resultChan:
		assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	case err := <-errChan:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for execution to complete")
	}
}

func TestExecutor_Execute_ActionFailure(t *testing.T) {
	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			if name, ok := inputs["name"]; ok && name == "fail-action" {
				return nil, errors.New("intentional failure")
			}
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fail-action": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "fail-action"},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.Error(t, err)
	assert.Equal(t, ExecutionFailed, result.FinalStatus)
	assert.Contains(t, result.FailedActions, "fail-action")
}

func TestExecutor_Execute_OnErrorContinue(t *testing.T) {
	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			if name, ok := inputs["name"]; ok && name == "fail-action" {
				return nil, errors.New("intentional failure")
			}
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fail-action": {
				Provider: "test-provider",
				OnError:  spec.OnErrorContinue,
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "fail-action"},
				},
			},
			"success-action": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "success-action"},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, ExecutionPartialSuccess, result.FinalStatus)
	assert.Contains(t, result.FailedActions, "fail-action")
	assert.Equal(t, StatusSucceeded, result.Actions["success-action"].Status)
}

func TestExecutor_Execute_DependencyFailure(t *testing.T) {
	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			if name, ok := inputs["name"]; ok && name == "fail-action" {
				return nil, errors.New("intentional failure")
			}
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fail-action": {
				Provider: "test-provider",
				OnError:  spec.OnErrorContinue, // Allow execution to continue
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "fail-action"},
				},
			},
			"dependent-action": {
				Provider:  "test-provider",
				DependsOn: []string{"fail-action"},
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "dependent-action"},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Contains(t, result.FailedActions, "fail-action")
	assert.Contains(t, result.SkippedActions, "dependent-action")
	assert.Equal(t, StatusSkipped, result.Actions["dependent-action"].Status)
	assert.Equal(t, SkipReasonDependencyFailed, result.Actions["dependent-action"].SkipReason)
}

func TestExecutor_Execute_Timeout(t *testing.T) {
	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(ctx context.Context, _ any) (*provider.Output, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return &provider.Output{Data: map[string]any{"done": true}}, nil
			}
		},
	})

	timeout := duration.New(100 * time.Millisecond)
	executor := NewExecutor(
		WithRegistry(registry),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"timeout-action": {
				Provider: "test-provider",
				Timeout:  &timeout,
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.Error(t, err)
	assert.Equal(t, ExecutionFailed, result.FinalStatus)
	assert.Equal(t, StatusTimeout, result.Actions["timeout-action"].Status)
}

func TestExecutor_Execute_Cancellation(t *testing.T) {
	started := make(chan struct{})

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(ctx context.Context, _ any) (*provider.Output, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(30*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"long-action": {
				Provider: "test-provider",
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start execution
	resultChan := make(chan *ExecutionResult)
	errChan := make(chan error)
	go func() {
		result, err := executor.Execute(ctx, workflow)
		if err != nil {
			errChan <- err
		}
		resultChan <- result
	}()

	// Wait for action to start, then cancel
	<-started
	cancel()

	// Wait for result
	select {
	case result := <-resultChan:
		assert.Equal(t, ExecutionCancelled, result.FinalStatus)
	case <-errChan:
		// Error is expected
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for cancellation")
	}
}

func TestExecutor_Execute_Finally(t *testing.T) {
	executionOrder := make([]string, 0)
	var mu sync.Mutex

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, ok := input.(map[string]any)
			if ok {
				if name, exists := inputs["name"]; exists {
					mu.Lock()
					executionOrder = append(executionOrder, name.(string))
					mu.Unlock()
				}
			}
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"main-action": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "main-action"},
				},
			},
		},
		Finally: map[string]*Action{
			"cleanup-action": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "cleanup-action"},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, executionOrder, 2)
	assert.Equal(t, "main-action", executionOrder[0])
	assert.Equal(t, "cleanup-action", executionOrder[1])
}

func TestExecutor_Execute_FinallyRunsAfterFailure(t *testing.T) {
	finallyExecuted := false

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, input any) (*provider.Output, error) {
			inputs, _ := input.(map[string]any)
			if name, ok := inputs["name"]; ok {
				if name == "cleanup" {
					finallyExecuted = true
				}
				if name == "fail-action" {
					return nil, errors.New("intentional failure")
				}
			}
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"fail-action": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "fail-action"},
				},
			},
		},
		Finally: map[string]*Action{
			"cleanup": {
				Provider: "test-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "cleanup"},
				},
			},
		},
	}

	result, _ := executor.Execute(context.Background(), workflow)

	assert.True(t, finallyExecuted, "finally section should execute after failure")
	assert.Equal(t, ExecutionFailed, result.FinalStatus)
}

func TestExecutor_Execute_ProgressCallback(t *testing.T) {
	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	callback := &recordingProgressCallback{}

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
		WithProgressCallback(callback),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"test-action": {
				Provider: "test-provider",
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)

	// Verify callbacks were called
	assert.Contains(t, callback.events, "phase_start:0")
	assert.Contains(t, callback.events, "action_start:test-action")
	assert.Contains(t, callback.events, "action_complete:test-action")
	assert.Contains(t, callback.events, "phase_complete:0")
}

func TestExecutor_Execute_Retry(t *testing.T) {
	attempts := 0
	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			attempts++
			if attempts < 3 {
				return nil, errors.New("transient error")
			}
			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	tenMillis := duration.New(10 * time.Millisecond)
	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"retry-action": {
				Provider: "test-provider",
				Retry: &RetryConfig{
					MaxAttempts:  5,
					Backoff:      BackoffFixed,
					InitialDelay: &tenMillis,
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.Equal(t, 3, attempts)
}

func TestExecutor_Execute_MaxConcurrency(t *testing.T) {
	maxConcurrent := 0
	currentConcurrent := 0
	var mu sync.Mutex

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "test-provider",
		execute: func(_ context.Context, _ any) (*provider.Output, error) {
			mu.Lock()
			currentConcurrent++
			if currentConcurrent > maxConcurrent {
				maxConcurrent = currentConcurrent
			}
			mu.Unlock()

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			currentConcurrent--
			mu.Unlock()

			return &provider.Output{Data: map[string]any{"done": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
		WithMaxConcurrency(2),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"action-a": {Provider: "test-provider"},
			"action-b": {Provider: "test-provider"},
			"action-c": {Provider: "test-provider"},
			"action-d": {Provider: "test-provider"},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)

	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.LessOrEqual(t, maxConcurrent, 2, "should not exceed max concurrency")
}

func TestExecutor_Reset(t *testing.T) {
	executor := NewExecutor()

	// Add some state
	executor.actionContext.MarkSucceeded("test", map[string]any{"done": true})
	assert.Equal(t, 1, executor.actionContext.ActionCount())

	// Reset
	executor.Reset()
	assert.Equal(t, 0, executor.actionContext.ActionCount())
}

func TestExecutionResult_Duration(t *testing.T) {
	start := time.Now()
	end := start.Add(5 * time.Second)

	result := &ExecutionResult{
		StartTime: start,
		EndTime:   end,
	}

	assert.Equal(t, 5*time.Second, result.Duration())
}

func TestNoOpProgressCallback(t *testing.T) {
	callback := NoOpProgressCallback{}

	// All methods should be callable without panic
	callback.OnActionStart("test")
	callback.OnActionComplete("test", nil)
	callback.OnActionFailed("test", nil)
	callback.OnActionSkipped("test", "reason")
	callback.OnActionTimeout("test", time.Second)
	callback.OnActionCancelled("test")
	callback.OnRetryAttempt("test", 1, 3, nil)
	callback.OnForEachProgress("test", 1, 10)
	callback.OnPhaseStart(0, nil)
	callback.OnPhaseComplete(0)
	callback.OnFinallyStart()
	callback.OnFinallyComplete()
}

// Mock types for testing

type execMockRegistry struct {
	mu        sync.RWMutex
	providers map[string]provider.Provider
}

func newExecMockRegistry() *execMockRegistry {
	return &execMockRegistry{
		providers: make(map[string]provider.Provider),
	}
}

func (r *execMockRegistry) register(p provider.Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Descriptor().Name] = p
}

func (r *execMockRegistry) Get(name string) (provider.Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

func (r *execMockRegistry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[name]
	return ok
}

type execMockProvider struct {
	name    string
	execute func(ctx context.Context, input any) (*provider.Output, error)
}

func (p *execMockProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        p.name,
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Description: "Mock provider for testing",
		Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
			"name": schemahelper.StringProp("Test input"),
		}),
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityAction: schemahelper.ObjectSchema([]string{"success"}, map[string]*jsonschema.Schema{
				"success": schemahelper.BoolProp("Whether action succeeded"),
			}),
		},
		Capabilities: []provider.Capability{provider.CapabilityAction},
	}
}

func (p *execMockProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	if p.execute != nil {
		return p.execute(ctx, input)
	}
	return &provider.Output{Data: map[string]any{"success": true}}, nil
}

type recordingProgressCallback struct {
	mu     sync.Mutex
	events []string
}

func (c *recordingProgressCallback) OnActionStart(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "action_start:"+name)
}

func (c *recordingProgressCallback) OnActionComplete(name string, _ any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "action_complete:"+name)
}

func (c *recordingProgressCallback) OnActionFailed(name string, _ error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "action_failed:"+name)
}

func (c *recordingProgressCallback) OnActionSkipped(name, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "action_skipped:"+name+":"+reason)
}

func (c *recordingProgressCallback) OnActionTimeout(name string, _ time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "action_timeout:"+name)
}

func (c *recordingProgressCallback) OnActionCancelled(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "action_cancelled:"+name)
}

func (c *recordingProgressCallback) OnRetryAttempt(name string, attempt, maxAttempts int, _ error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "retry:"+name)
	_ = attempt
	_ = maxAttempts
}

func (c *recordingProgressCallback) OnForEachProgress(name string, completed, total int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "foreach_progress:"+name)
	_ = completed
	_ = total
}

func (c *recordingProgressCallback) OnPhaseStart(phase int, _ []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "phase_start:"+string(rune('0'+phase)))
}

func (c *recordingProgressCallback) OnPhaseComplete(phase int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "phase_complete:"+string(rune('0'+phase)))
}

func (c *recordingProgressCallback) OnFinallyStart() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "finally_start")
}

func (c *recordingProgressCallback) OnFinallyComplete() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, "finally_complete")
}

func TestWithIOStreams_ActionExecutor(t *testing.T) {
	streams := &provider.IOStreams{}
	e := NewExecutor(WithIOStreams(streams))
	assert.Equal(t, streams, e.ioStreams)
}

func TestExecutor_IOStreamsInjectedIntoActionContext(t *testing.T) {
	// Verify that IOStreams set on the executor are propagated to actions
	// via the provider context, so streaming providers can write output.
	var receivedStreams *provider.IOStreams

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "streaming-provider",
		execute: func(ctx context.Context, _ any) (*provider.Output, error) {
			if streams, ok := provider.IOStreamsFromContext(ctx); ok {
				receivedStreams = streams
			}
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	streams := &provider.IOStreams{
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}
	executor := NewExecutor(
		WithRegistry(registry),
		WithIOStreams(streams),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"stream-action": {
				Provider: "streaming-provider",
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.NotNil(t, receivedStreams, "IOStreams should be injected into action context")
	assert.Equal(t, streams, receivedStreams, "Action should receive the same IOStreams as the executor")
}

func TestExecutor_NoIOStreams_NilContext(t *testing.T) {
	// Verify that when no IOStreams are set on the executor,
	// actions do not receive IOStreams in their context.
	var hasStreams bool

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "check-provider",
		execute: func(ctx context.Context, _ any) (*provider.Output, error) {
			_, hasStreams = provider.IOStreamsFromContext(ctx)
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	executor := NewExecutor(
		WithRegistry(registry),
		WithDefaultTimeout(5*time.Second),
	)

	workflow := &Workflow{
		Actions: map[string]*Action{
			"check-action": {
				Provider: "check-provider",
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)
	assert.False(t, hasStreams, "IOStreams should not be in context when not set on executor")
}

func TestExecutor_IOStreamsSharedAcrossParallelActions(t *testing.T) {
	// Verify that all parallel actions in a phase share the same IOStreams
	// when set on the executor (no per-action PrefixedWriter).
	var collectedStreams sync.Map

	registry := newExecMockRegistry()
	registry.register(&execMockProvider{
		name: "parallel-provider",
		execute: func(ctx context.Context, input any) (*provider.Output, error) {
			if streams, ok := provider.IOStreamsFromContext(ctx); ok {
				inputs := input.(map[string]any)
				name := inputs["name"].(string)
				collectedStreams.Store(name, streams)
			}
			return &provider.Output{Data: map[string]any{"success": true}}, nil
		},
	})

	streams := &provider.IOStreams{
		Out:    os.Stdout,
		ErrOut: os.Stderr,
	}
	executor := NewExecutor(
		WithRegistry(registry),
		WithIOStreams(streams),
		WithDefaultTimeout(5*time.Second),
	)

	// Two independent actions → both run in parallel in the same phase
	workflow := &Workflow{
		Actions: map[string]*Action{
			"action-1": {
				Provider: "parallel-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "action-1"},
				},
			},
			"action-2": {
				Provider: "parallel-provider",
				Inputs: map[string]*spec.ValueRef{
					"name": {Literal: "action-2"},
				},
			},
		},
	}

	result, err := executor.Execute(context.Background(), workflow)
	require.NoError(t, err)
	assert.Equal(t, ExecutionSucceeded, result.FinalStatus)

	// Both actions should have received the same IOStreams pointer
	s1, ok1 := collectedStreams.Load("action-1")
	s2, ok2 := collectedStreams.Load("action-2")
	assert.True(t, ok1, "action-1 should have received IOStreams")
	assert.True(t, ok2, "action-2 should have received IOStreams")
	if ok1 && ok2 {
		assert.Same(t, s1.(*provider.IOStreams), s2.(*provider.IOStreams),
			"Parallel actions should share the same IOStreams (no PrefixedWriter)")
	}
}

func TestOptionsFromAppConfig(t *testing.T) {
	cfg := ConfigInput{
		DefaultTimeout: 5 * time.Minute,
		GracePeriod:    30 * time.Second,
		MaxConcurrency: 3,
	}
	opts := OptionsFromAppConfig(cfg)
	assert.Len(t, opts, 3)

	// Apply to executor and check
	e := NewExecutor(opts...)
	assert.Equal(t, 5*time.Minute, e.defaultTimeout)
	assert.Equal(t, 30*time.Second, e.gracePeriod)
	assert.Equal(t, 3, e.maxConcurrency)
}

func TestOptionsFromAppConfig_ZeroValues(t *testing.T) {
	cfg := ConfigInput{}
	opts := OptionsFromAppConfig(cfg)
	assert.Empty(t, opts)
}

func TestExecutor_GetContext(t *testing.T) {
	e := NewExecutor()
	ctx := e.GetContext()
	assert.NotNil(t, ctx)
	assert.Equal(t, e.actionContext, ctx)
}

func TestWithExecutionData(t *testing.T) {
	t.Parallel()

	execData := map[string]any{
		"resolvers": map[string]any{
			"myResolver": map[string]any{
				"status":   "success",
				"duration": "1.2s",
				"phase":    float64(1),
			},
		},
	}

	e := NewExecutor(WithExecutionData(execData))
	assert.Equal(t, execData, e.executionData)
}

func TestWithExecutionData_NilSafe(t *testing.T) {
	t.Parallel()

	e := NewExecutor()
	assert.Nil(t, e.executionData)

	// buildAdditionalVars should not include __execution when nil
	vars := e.buildAdditionalVars(nil)
	_, hasExecution := vars["__execution"]
	assert.False(t, hasExecution, "__execution should not be injected when executionData is nil")
}

func TestBuildAdditionalVars_InjectsExecution(t *testing.T) {
	t.Parallel()

	execData := map[string]any{
		"resolvers": map[string]any{
			"build": map[string]any{"status": "success"},
		},
	}

	e := NewExecutor(WithExecutionData(execData))
	vars := e.buildAdditionalVars(nil)

	execution, ok := vars["__execution"]
	assert.True(t, ok, "__execution should be present when executionData is set")
	assert.Equal(t, execData, execution)
}

func TestBuildAdditionalVars_ExecutionDoesNotOverrideActions(t *testing.T) {
	t.Parallel()

	execData := map[string]any{"resolvers": map[string]any{}}

	e := NewExecutor(WithExecutionData(execData))
	vars := e.buildAdditionalVars(nil)

	// both __actions and __execution must coexist
	_, hasActions := vars["__actions"]
	_, hasExecution := vars["__execution"]
	assert.True(t, hasActions)
	assert.True(t, hasExecution)
}
