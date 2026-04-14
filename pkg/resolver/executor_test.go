// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ErrorAs is a helper to check if an error is of a specific type
func ErrorAs(err error, target any) bool {
	return errors.As(err, target)
}

// mockProvider is a simple mock provider for testing
type mockProvider struct {
	name        string
	executeFunc func(ctx context.Context, inputs map[string]any) (*provider.Output, error)
}

func (m *mockProvider) Descriptor() *provider.Descriptor {
	return &provider.Descriptor{
		Name:        m.name,
		APIVersion:  "v1",
		Description: "Mock provider for testing",
	}
}

func (m *mockProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	inputs, _ := input.(map[string]any)
	if m.executeFunc != nil {
		return m.executeFunc(ctx, inputs)
	}
	return &provider.Output{Data: "mock-value"}, nil
}

// mockRegistry is a mock provider registry for testing
type mockRegistry struct {
	providers map[string]provider.Provider
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		providers: make(map[string]provider.Provider),
	}
}

func (r *mockRegistry) Register(p provider.Provider) error {
	r.providers[p.Descriptor().Name] = p
	return nil
}

func (r *mockRegistry) Get(name string) (provider.Provider, error) {
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found", name)
	}
	return p, nil
}

func (r *mockRegistry) List() []provider.Provider {
	providers := make([]provider.Provider, 0, len(r.providers))
	for _, p := range r.providers {
		providers = append(providers, p)
	}
	return providers
}

func (r *mockRegistry) DescriptorLookup() DescriptorLookup {
	return func(providerName string) *provider.Descriptor {
		p, ok := r.providers[providerName]
		if !ok {
			return nil
		}
		desc := p.Descriptor()
		return desc
	}
}

func TestNewExecutor(t *testing.T) {
	registry := newMockRegistry()

	tests := []struct {
		name     string
		opts     []ExecutorOption
		validate func(t *testing.T, e *Executor)
	}{
		{
			name: "default options",
			opts: nil,
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, 30*time.Second, e.timeout)
				assert.Equal(t, 0, e.maxConcurrency)
				assert.Equal(t, 5*time.Minute, e.phaseTimeout)
			},
		},
		{
			name: "with max concurrency",
			opts: []ExecutorOption{WithMaxConcurrency(5)},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, 5, e.maxConcurrency)
			},
		},
		{
			name: "with phase timeout",
			opts: []ExecutorOption{WithPhaseTimeout(1 * time.Minute)},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, 1*time.Minute, e.phaseTimeout)
			},
		},
		{
			name: "with default timeout",
			opts: []ExecutorOption{WithDefaultTimeout(10 * time.Second)},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, 10*time.Second, e.timeout)
			},
		},
		{
			name: "with multiple options",
			opts: []ExecutorOption{
				WithMaxConcurrency(10),
				WithPhaseTimeout(2 * time.Minute),
				WithDefaultTimeout(5 * time.Second),
			},
			validate: func(t *testing.T, e *Executor) {
				assert.Equal(t, 10, e.maxConcurrency)
				assert.Equal(t, 2*time.Minute, e.phaseTimeout)
				assert.Equal(t, 5*time.Second, e.timeout)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := NewExecutor(registry, tt.opts...)
			require.NotNil(t, executor)
			assert.Equal(t, registry, executor.registry)
			if tt.validate != nil {
				tt.validate(t, executor)
			}
		})
	}
}

func TestExecutor_Execute_Simple(t *testing.T) {
	registry := newMockRegistry()

	// Register a simple static provider
	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			value := inputs["value"]
			return &provider.Output{Data: value}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "simple",
			Type: TypeString,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: "hello"},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)

	require.NoError(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	value, ok := result.Get("simple")
	require.True(t, ok)
	assert.Equal(t, "hello", value)

	// Check execution result
	execResult, ok := result.GetResult("simple")
	require.True(t, ok)
	assert.Equal(t, ExecutionStatusSuccess, execResult.Status)
	assert.Equal(t, 1, execResult.Phase)
	assert.Equal(t, 1, execResult.ProviderCallCount)
	assert.Nil(t, execResult.Error)
}

func TestExecutor_Execute_WithDependencies(t *testing.T) {
	registry := newMockRegistry()

	// Register a static provider
	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	// Register a concat provider
	err = registry.Register(&mockProvider{
		name: "concat",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			a := fmt.Sprintf("%v", inputs["a"])
			b := fmt.Sprintf("%v", inputs["b"])
			return &provider.Output{Data: a + b}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "base",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: "hello"},
						},
					},
				},
			},
		},
		{
			Name: "dependent",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "concat",
						Inputs: map[string]*ValueRef{
							"a": {Resolver: stringPtr("base")},
							"b": {Literal: " world"},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)

	require.NoError(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	// Check base resolver
	baseValue, ok := result.Get("base")
	require.True(t, ok)
	assert.Equal(t, "hello", baseValue)

	// Check dependent resolver
	depValue, ok := result.Get("dependent")
	require.True(t, ok)
	assert.Equal(t, "hello world", depValue)

	// Check execution results
	baseResult, ok := result.GetResult("base")
	require.True(t, ok)
	assert.Equal(t, 1, baseResult.Phase)

	depResult, ok := result.GetResult("dependent")
	require.True(t, ok)
	assert.Equal(t, 2, depResult.Phase)
}

func TestExecutor_Execute_WithConditional(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "enabled",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: true},
						},
					},
				},
			},
		},
		{
			Name: "conditional",
			When: &Condition{
				Expr: celExpPtr("_.enabled == true"),
			},
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: "executed"},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)

	require.NoError(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	// Conditional resolver should execute
	condValue, ok := result.Get("conditional")
	require.True(t, ok)
	assert.Equal(t, "executed", condValue)

	condResult, ok := result.GetResult("conditional")
	require.True(t, ok)
	assert.Equal(t, ExecutionStatusSuccess, condResult.Status)
}

func TestExecutor_Execute_ConditionalSkipped(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "enabled",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: false},
						},
					},
				},
			},
		},
		{
			Name: "conditional",
			When: &Condition{
				Expr: celExpPtr("_.enabled == true"),
			},
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: "should not execute"},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)

	require.NoError(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	// Conditional resolver should be skipped - absent from context (not in _)
	_, ok := result.Get("conditional")
	assert.False(t, ok, "skipped resolver should be absent from resolver context")

	// Enabled resolver should have its value
	enabledVal, ok := result.Get("enabled")
	assert.True(t, ok, "enabled resolver should be present")
	assert.Equal(t, false, enabledVal)
}

func TestExecutor_Execute_WithTransform(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	err = registry.Register(&mockProvider{
		name: "uppercase",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			value := fmt.Sprintf("%v", inputs["value"])
			return &provider.Output{Data: fmt.Sprintf("%s-TRANSFORMED", value)}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "transformed",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: "original"},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "uppercase",
						Inputs: map[string]*ValueRef{
							"value": {Literal: "original"}, // Use literal instead of self-reference
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)

	require.NoError(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	value, ok := result.Get("transformed")
	require.True(t, ok)
	assert.Equal(t, "original-TRANSFORMED", value)

	execResult, ok := result.GetResult("transformed")
	require.True(t, ok)
	assert.Equal(t, 2, execResult.ProviderCallCount) // resolve + transform
	assert.Len(t, execResult.PhaseMetrics, 2)        // resolve and transform phases
}

func TestExecutor_Execute_WithValidation(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	err = registry.Register(&mockProvider{
		name: "validate",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			value := inputs["value"]
			if value == "invalid" {
				return nil, fmt.Errorf("validation failed")
			}
			return &provider.Output{Data: true}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	t.Run("validation success", func(t *testing.T) {
		resolvers := []*Resolver{
			{
				Name: "validated",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "valid"},
							},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validate",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "valid"},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		require.NoError(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		value, ok := result.Get("validated")
		require.True(t, ok)
		assert.Equal(t, "valid", value)
	})

	t.Run("validation failure", func(t *testing.T) {
		resolvers := []*Resolver{
			{
				Name: "validated",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Literal: "invalid"},
							},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validate",
							Inputs: map[string]*ValueRef{
								"value": {Expr: celExpPtr("__self")},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err = executor.Execute(ctx, resolvers, nil)

		require.Error(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		// Value should still be emitted (partial emission)
		execResult, ok := result.GetResult("validated")
		require.True(t, ok)
		assert.Equal(t, ExecutionStatusFailed, execResult.Status)
		assert.Equal(t, "invalid", execResult.Value)
		assert.NotNil(t, execResult.Error)
	})
}

func TestExecutor_Execute_OnErrorContinue(t *testing.T) {
	registry := newMockRegistry()

	callCount := 0
	err := registry.Register(&mockProvider{
		name: "failing",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("first provider failed")
			}
			return &provider.Output{Data: "fallback-value"}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "fallback",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "failing",
						Inputs:   map[string]*ValueRef{},
						OnError:  ErrorBehaviorContinue,
					},
					{
						Provider: "failing",
						Inputs:   map[string]*ValueRef{},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)

	require.NoError(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	value, ok := result.Get("fallback")
	require.True(t, ok)
	assert.Equal(t, "fallback-value", value)
	assert.Equal(t, 2, callCount) // Both providers should be called
}

func TestExecutor_Execute_WithValidateAll(t *testing.T) {
	registry := newMockRegistry()

	// Register providers
	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	err = registry.Register(&mockProvider{
		name: "validation",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			// Simulate validation failure
			return nil, fmt.Errorf("validation failed: %v", inputs["message"])
		},
	})
	require.NoError(t, err)

	t.Run("collects all errors", func(t *testing.T) {
		executor := NewExecutor(registry, WithValidateAll(true))

		// Create resolvers where multiple will fail validation
		resolvers := []*Resolver{
			{
				Name: "fail1",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs:   map[string]*ValueRef{"value": {Literal: "value1"}},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs:   map[string]*ValueRef{"message": {Literal: "error1"}},
						},
					},
				},
			},
			{
				Name: "fail2",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs:   map[string]*ValueRef{"value": {Literal: "value2"}},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs:   map[string]*ValueRef{"message": {Literal: "error2"}},
						},
					},
				},
			},
			{
				Name: "success",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs:   map[string]*ValueRef{"value": {Literal: "success-value"}},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err := executor.Execute(ctx, resolvers, nil)

		require.Error(t, err)
		var aggErr *AggregatedExecutionError
		require.ErrorAs(t, err, &aggErr)
		assert.Equal(t, 2, len(aggErr.Errors), "should collect both validation errors")
		assert.Equal(t, 1, aggErr.SucceededCount, "should count successful resolver")

		// Successful resolver should still have its value
		result, _ := FromContext(ctx)
		require.NotNil(t, result)
		value, ok := result.Get("success")
		require.True(t, ok)
		assert.Equal(t, "success-value", value)
	})

	t.Run("skips dependents of failed resolvers", func(t *testing.T) {
		executor := NewExecutor(registry, WithValidateAll(true))

		// Create resolvers with dependencies where parent fails
		resolvers := []*Resolver{
			{
				Name: "parent",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs:   map[string]*ValueRef{"value": {Literal: "parent-value"}},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs:   map[string]*ValueRef{"message": {Literal: "parent validation failed"}},
						},
					},
				},
			},
			{
				Name: "child",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs: map[string]*ValueRef{
								"value": {Resolver: stringPtr("parent")},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		_, err := executor.Execute(ctx, resolvers, nil)

		require.Error(t, err)
		var aggErr *AggregatedExecutionError
		require.ErrorAs(t, err, &aggErr)
		assert.Equal(t, 1, len(aggErr.Errors), "should have one error (parent)")
		assert.Equal(t, 1, aggErr.SkippedCount, "should skip child resolver")
		assert.Contains(t, aggErr.SkippedNames, "child")
	})

	t.Run("without validate-all stops on first error", func(t *testing.T) {
		executor := NewExecutor(registry) // Default: validate-all is false

		resolvers := []*Resolver{
			{
				Name: "fail1",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs:   map[string]*ValueRef{"value": {Literal: "value1"}},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs:   map[string]*ValueRef{"message": {Literal: "error1"}},
						},
					},
				},
			},
			{
				Name: "fail2",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs:   map[string]*ValueRef{"value": {Literal: "value2"}},
						},
					},
				},
				Validate: &ValidatePhase{
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs:   map[string]*ValueRef{"message": {Literal: "error2"}},
						},
					},
				},
			},
		}

		ctx := context.Background()
		_, err := executor.Execute(ctx, resolvers, nil)

		require.Error(t, err)
		// Should NOT be AggregatedExecutionError in normal mode
		var aggErr *AggregatedExecutionError
		assert.False(t, ErrorAs(err, &aggErr), "should not return AggregatedExecutionError without validate-all")
	})
}

func TestExecutor_Execute_WithConcurrencyLimit(t *testing.T) {
	registry := newMockRegistry()

	var mu sync.Mutex
	concurrentCount := 0
	maxConcurrent := 0

	err := registry.Register(&mockProvider{
		name: "slow",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			mu.Lock()
			concurrentCount++
			if concurrentCount > maxConcurrent {
				maxConcurrent = concurrentCount
			}
			mu.Unlock()

			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			concurrentCount--
			mu.Unlock()

			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry, WithMaxConcurrency(2))

	// Create 5 resolvers that run in parallel (same phase)
	resolvers := make([]*Resolver, 5)
	for i := 0; i < 5; i++ {
		resolvers[i] = &Resolver{
			Name: fmt.Sprintf("resolver%d", i),
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "slow",
						Inputs: map[string]*ValueRef{
							"value": {Literal: i},
						},
					},
				},
			},
		}
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)

	require.NoError(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	// Check that concurrency was limited to 2
	assert.LessOrEqual(t, maxConcurrent, 2)
	assert.Greater(t, maxConcurrent, 0)
}

func TestExecutor_Execute_TypeCoercion(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	tests := []struct {
		name       string
		inputValue any
		targetType Type
		expected   any
		wantErr    bool
	}{
		{
			name:       "int to string",
			inputValue: 42,
			targetType: TypeString,
			expected:   "42",
		},
		{
			name:       "float to int",
			inputValue: 42.0, // Changed from 42.7 - whole number floats can coerce
			targetType: TypeInt,
			expected:   42,
		},
		{
			name:       "string to int",
			inputValue: "123",
			targetType: TypeInt,
			expected:   123,
		},
		{
			name:       "string to bool",
			inputValue: "true",
			targetType: TypeBool,
			expected:   true,
		},
		{
			name:       "any type no coercion",
			inputValue: map[string]any{"key": "value"},
			targetType: TypeAny,
			expected:   map[string]any{"key": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolvers := []*Resolver{
				{
					Name: "typed",
					Type: tt.targetType,
					Resolve: &ResolvePhase{
						With: []ProviderSource{
							{
								Provider: "static",
								Inputs: map[string]*ValueRef{
									"value": {Literal: tt.inputValue},
								},
							},
						},
					},
				},
			}

			ctx := context.Background()
			ctx, err = executor.Execute(ctx, resolvers, nil)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			result, _ := FromContext(ctx)
			require.NotNil(t, result)

			value, ok := result.Get("typed")
			require.True(t, ok)
			assert.Equal(t, tt.expected, value)
		})
	}
}

func TestExecutor_Execute_ProviderNotFound(t *testing.T) {
	registry := newMockRegistry()
	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "broken",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "nonexistent",
						Inputs:   map[string]*ValueRef{},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err := executor.Execute(ctx, resolvers, nil)

	require.Error(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	execResult, ok := result.GetResult("broken")
	require.True(t, ok)
	assert.Equal(t, ExecutionStatusFailed, execResult.Status)
}

func TestCalculateValueSize(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected int64
	}{
		{
			name:     "nil value",
			value:    nil,
			expected: 0,
		},
		{
			name:     "string value",
			value:    "hello",
			expected: 7, // "hello" in JSON
		},
		{
			name:     "int value",
			value:    42,
			expected: 2, // 42 in JSON
		},
		{
			name:     "map value",
			value:    map[string]any{"key": "value"},
			expected: 15, // {"key":"value"} in JSON
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			size := calculateValueSize(tt.value)
			assert.Equal(t, tt.expected, size)
		})
	}
}

func TestCoerceType(t *testing.T) {
	tests := []struct {
		name       string
		value      any
		targetType Type
		expected   any
		wantErr    bool
	}{
		{
			name:       "string to string",
			value:      "hello",
			targetType: TypeString,
			expected:   "hello",
		},
		{
			name:       "int to string",
			value:      42,
			targetType: TypeString,
			expected:   "42",
		},
		{
			name:       "int to int",
			value:      42,
			targetType: TypeInt,
			expected:   42,
		},
		{
			name:       "float to int with decimal",
			value:      42.9,
			targetType: TypeInt,
			wantErr:    true, // Decimal part not allowed
		},
		{
			name:       "string to int valid",
			value:      "123",
			targetType: TypeInt,
			expected:   123,
		},
		{
			name:       "string to int invalid",
			value:      "abc",
			targetType: TypeInt,
			wantErr:    true,
		},
		{
			name:       "bool true",
			value:      "true",
			targetType: TypeBool,
			expected:   true,
		},
		{
			name:       "bool false",
			value:      "false",
			targetType: TypeBool,
			expected:   false,
		},
		{
			name:       "bool invalid",
			value:      "maybe",
			targetType: TypeBool,
			wantErr:    true,
		},
		{
			name:       "any type",
			value:      map[string]any{"key": "value"},
			targetType: TypeAny,
			expected:   map[string]any{"key": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CoerceType(tt.value, tt.targetType)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExecutor_Execute_Timeout tests resolver timeout handling
func TestExecutor_Execute_Timeout(t *testing.T) {
	registry := newMockRegistry()

	// Register a slow provider that takes longer than timeout
	err := registry.Register(&mockProvider{
		name: "slow",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			delay := inputs["delay"].(time.Duration)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				return &provider.Output{Data: "completed"}, nil
			}
		},
	})
	require.NoError(t, err)

	t.Run("resolver timeout exceeded", func(t *testing.T) {
		executor := NewExecutor(registry, WithDefaultTimeout(50*time.Millisecond))

		resolvers := []*Resolver{
			{
				Name: "slow_resolver",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "slow",
							Inputs: map[string]*ValueRef{
								"delay": {Literal: 200 * time.Millisecond},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		start := time.Now()
		ctx, err := executor.Execute(ctx, resolvers, nil)
		elapsed := time.Since(start)

		// Should error due to timeout
		assert.Error(t, err)
		// Should complete quickly due to timeout, not wait full delay
		assert.Less(t, elapsed, 150*time.Millisecond, "should timeout before full delay")
		// Context should still be returned
		assert.NotNil(t, ctx)
	})

	t.Run("resolver completes within timeout", func(t *testing.T) {
		executor := NewExecutor(registry, WithDefaultTimeout(500*time.Millisecond))

		resolvers := []*Resolver{
			{
				Name: "fast_resolver",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "slow",
							Inputs: map[string]*ValueRef{
								"delay": {Literal: 10 * time.Millisecond},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err := executor.Execute(ctx, resolvers, nil)

		require.NoError(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		value, ok := result.Get("fast_resolver")
		assert.True(t, ok)
		assert.Equal(t, "completed", value)
	})
}

// TestExecutor_Execute_PhaseTimeout tests phase-level timeout
func TestExecutor_Execute_PhaseTimeout(t *testing.T) {
	registry := newMockRegistry()

	// Register providers for slow execution
	err := registry.Register(&mockProvider{
		name: "slow",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			delay := inputs["delay"].(time.Duration)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
				return &provider.Output{Data: "completed"}, nil
			}
		},
	})
	require.NoError(t, err)

	t.Run("phase timeout with multiple concurrent resolvers", func(t *testing.T) {
		// Phase timeout shorter than individual resolver completion time
		executor := NewExecutor(registry,
			WithDefaultTimeout(5*time.Second), // Individual resolver timeout is high
			WithPhaseTimeout(100*time.Millisecond),
		)

		// Multiple resolvers in same phase (no dependencies)
		resolvers := []*Resolver{
			{
				Name: "slow1",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "slow",
							Inputs: map[string]*ValueRef{
								"delay": {Literal: 300 * time.Millisecond},
							},
						},
					},
				},
			},
			{
				Name: "slow2",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "slow",
							Inputs: map[string]*ValueRef{
								"delay": {Literal: 300 * time.Millisecond},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		start := time.Now()
		ctx, err := executor.Execute(ctx, resolvers, nil)
		elapsed := time.Since(start)

		// Should error due to phase timeout
		assert.Error(t, err)
		// Phase should timeout before resolvers complete
		assert.Less(t, elapsed, 250*time.Millisecond, "phase should timeout before resolvers complete")
		assert.NotNil(t, ctx)
	})
}

// TestExecutor_Execute_ContextCancellation tests context cancellation handling
func TestExecutor_Execute_ContextCancellation(t *testing.T) {
	registry := newMockRegistry()

	err := registry.Register(&mockProvider{
		name: "slow",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Second):
				return &provider.Output{Data: "completed"}, nil
			}
		},
	})
	require.NoError(t, err)

	t.Run("context cancelled during execution", func(t *testing.T) {
		executor := NewExecutor(registry)

		resolvers := []*Resolver{
			{
				Name: "will_be_cancelled",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "slow",
							Inputs:   map[string]*ValueRef{},
						},
					},
				},
			},
		}

		ctx, cancel := context.WithCancel(context.Background())

		// Cancel after a short delay
		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		ctx, err := executor.Execute(ctx, resolvers, nil)
		elapsed := time.Since(start)

		// Should error due to cancellation
		assert.Error(t, err)
		// Should return quickly after cancellation
		assert.Less(t, elapsed, 200*time.Millisecond, "should return quickly after cancellation")
		assert.NotNil(t, ctx)
	})
}

// TestExecutor_Execute_ConcurrentStress runs stress tests for concurrent execution
func TestExecutor_Execute_ConcurrentStress(t *testing.T) {
	registry := newMockRegistry()

	var (
		mu            sync.Mutex
		executionLogs []string
	)

	// Provider that logs execution order
	err := registry.Register(&mockProvider{
		name: "counter",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			name := inputs["name"].(string)
			delay := inputs["delay"].(time.Duration)

			mu.Lock()
			executionLogs = append(executionLogs, fmt.Sprintf("start:%s", name))
			mu.Unlock()

			time.Sleep(delay)

			mu.Lock()
			executionLogs = append(executionLogs, fmt.Sprintf("end:%s", name))
			mu.Unlock()

			return &provider.Output{Data: name}, nil
		},
	})
	require.NoError(t, err)

	t.Run("many resolvers in same phase execute concurrently", func(t *testing.T) {
		mu.Lock()
		executionLogs = nil
		mu.Unlock()

		executor := NewExecutor(registry)

		// Create 20 resolvers with no dependencies (all same phase)
		count := 20
		resolvers := make([]*Resolver, count)
		for i := 0; i < count; i++ {
			resolvers[i] = &Resolver{
				Name: fmt.Sprintf("r%d", i),
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "counter",
							Inputs: map[string]*ValueRef{
								"name":  {Literal: fmt.Sprintf("r%d", i)},
								"delay": {Literal: 10 * time.Millisecond},
							},
						},
					},
				},
			}
		}

		ctx := context.Background()
		start := time.Now()
		ctx, err := executor.Execute(ctx, resolvers, nil)
		elapsed := time.Since(start)

		require.NoError(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		// All resolvers should have values
		for i := 0; i < count; i++ {
			value, ok := result.Get(fmt.Sprintf("r%d", i))
			assert.True(t, ok, "resolver r%d should have value", i)
			assert.NotNil(t, value)
		}

		// If truly concurrent, should take ~10ms (not 20*10ms = 200ms)
		// Allow some overhead but should be much faster than sequential
		assert.Less(t, elapsed, 100*time.Millisecond, "concurrent execution should be fast")
	})

	t.Run("dependent resolvers execute in correct order", func(t *testing.T) {
		mu.Lock()
		executionLogs = nil
		mu.Unlock()

		executor := NewExecutor(registry)

		// Chain: a -> b -> c (each depends on previous)
		resolvers := []*Resolver{
			{
				Name: "a",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "counter",
							Inputs: map[string]*ValueRef{
								"name":  {Literal: "a"},
								"delay": {Literal: 5 * time.Millisecond},
							},
						},
					},
				},
			},
			{
				Name: "b",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "counter",
							Inputs: map[string]*ValueRef{
								"name":  {Literal: "b"},
								"delay": {Literal: 5 * time.Millisecond},
								"dep":   {Resolver: stringPtr("a")},
							},
						},
					},
				},
			},
			{
				Name: "c",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "counter",
							Inputs: map[string]*ValueRef{
								"name":  {Literal: "c"},
								"delay": {Literal: 5 * time.Millisecond},
								"dep":   {Resolver: stringPtr("b")},
							},
						},
					},
				},
			},
		}

		ctx := context.Background()
		ctx, err := executor.Execute(ctx, resolvers, nil)

		require.NoError(t, err)
		result, _ := FromContext(ctx)
		require.NotNil(t, result)

		// Verify order: a must complete before b starts, b must complete before c starts
		mu.Lock()
		logs := make([]string, len(executionLogs))
		copy(logs, executionLogs)
		mu.Unlock()

		// Find positions
		var endA, startB, endB, startC int
		for i, log := range logs {
			switch log {
			case "end:a":
				endA = i
			case "start:b":
				startB = i
			case "end:b":
				endB = i
			case "start:c":
				startC = i
			}
		}

		assert.Less(t, endA, startB, "a should complete before b starts")
		assert.Less(t, endB, startC, "b should complete before c starts")
	})

	t.Run("race condition detection under load", func(t *testing.T) {
		executor := NewExecutor(registry)

		// Create a diamond dependency pattern:
		//     root
		//    /    \
		//  left   right
		//    \    /
		//    bottom
		resolvers := []*Resolver{
			{
				Name: "root",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "counter",
							Inputs: map[string]*ValueRef{
								"name":  {Literal: "root"},
								"delay": {Literal: time.Millisecond},
							},
						},
					},
				},
			},
			{
				Name: "left",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "counter",
							Inputs: map[string]*ValueRef{
								"name":  {Literal: "left"},
								"delay": {Literal: time.Millisecond},
								"dep":   {Resolver: stringPtr("root")},
							},
						},
					},
				},
			},
			{
				Name: "right",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "counter",
							Inputs: map[string]*ValueRef{
								"name":  {Literal: "right"},
								"delay": {Literal: time.Millisecond},
								"dep":   {Resolver: stringPtr("root")},
							},
						},
					},
				},
			},
			{
				Name: "bottom",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "counter",
							Inputs: map[string]*ValueRef{
								"name":  {Literal: "bottom"},
								"delay": {Literal: time.Millisecond},
								"dep1":  {Resolver: stringPtr("left")},
								"dep2":  {Resolver: stringPtr("right")},
							},
						},
					},
				},
			},
		}

		// Run multiple times to detect race conditions
		for i := 0; i < 10; i++ {
			ctx := context.Background()
			ctx, err := executor.Execute(ctx, resolvers, nil)

			require.NoError(t, err)
			result, _ := FromContext(ctx)
			require.NotNil(t, result)

			// All should complete
			for _, name := range []string{"root", "left", "right", "bottom"} {
				value, ok := result.Get(name)
				assert.True(t, ok, "resolver %s should have value on iteration %d", name, i)
				assert.Equal(t, name, value)
			}
		}
	})
}

func TestExecutor_Execute_NilValueRefInput(t *testing.T) {
	registry := newMockRegistry()
	registry.Register(&mockProvider{name: "mock"})
	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "dangling-key",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "mock",
						Inputs: map[string]*ValueRef{
							"valid-key": {Literal: "hello"},
							"nil-key":   nil, // dangling YAML key
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err := executor.Execute(ctx, resolvers, nil)

	require.Error(t, err)
	result, _ := FromContext(ctx)
	require.NotNil(t, result)

	execResult, ok := result.GetResult("dangling-key")
	require.True(t, ok)
	assert.Equal(t, ExecutionStatusFailed, execResult.Status)
	require.Error(t, execResult.Error)
	assert.Contains(t, execResult.Error.Error(), "no value (nil)")
}

type noopProgressCallback struct{}

func (n *noopProgressCallback) OnPhaseStart(_ int, _ []string)               {}
func (n *noopProgressCallback) OnResolverComplete(_ string, _ time.Duration) {}
func (n *noopProgressCallback) OnResolverFailed(_ string, _ error)           {}
func (n *noopProgressCallback) OnResolverSkipped(_ string)                   {}

func TestWithProgressCallback(t *testing.T) {
	reg := newMockRegistry()
	cb := &noopProgressCallback{}
	exec := NewExecutor(reg, WithProgressCallback(cb))
	assert.NotNil(t, exec.progressCallback)
}

func TestWithSkipValidation(t *testing.T) {
	reg := newMockRegistry()
	exec := NewExecutor(reg, WithSkipValidation(true))
	assert.True(t, exec.skipValidation)
}

func TestWithSkipTransform(t *testing.T) {
	reg := newMockRegistry()
	exec := NewExecutor(reg, WithSkipTransform(true))
	assert.True(t, exec.skipTransform)
}

func TestResolverOptionsFromAppConfig(t *testing.T) {
	cfg := ConfigInput{
		Timeout:        30 * time.Second,
		PhaseTimeout:   5 * time.Minute,
		MaxConcurrency: 5,
		WarnValueSize:  1024,
		MaxValueSize:   10240,
		ValidateAll:    true,
	}
	opts := OptionsFromAppConfig(cfg)
	assert.Len(t, opts, 6)

	reg := newMockRegistry()
	exec := NewExecutor(reg, opts...)
	assert.Equal(t, 30*time.Second, exec.timeout)
	assert.Equal(t, 5*time.Minute, exec.phaseTimeout)
	assert.Equal(t, 5, exec.maxConcurrency)
	assert.Equal(t, int64(1024), exec.warnValueSize)
	assert.Equal(t, int64(10240), exec.maxValueSize)
	assert.True(t, exec.validateAll)
}

func TestResolverOptionsFromAppConfig_ZeroValues(t *testing.T) {
	cfg := ConfigInput{}
	opts := OptionsFromAppConfig(cfg)
	assert.Empty(t, opts)
}

func TestExecutor_ValidatePhase_WhenWithSelf(t *testing.T) {
	registry := newMockRegistry()

	// Register providers
	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	err = registry.Register(&mockProvider{
		name: "validation",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return nil, fmt.Errorf("validation failed: %v", inputs["message"])
		},
	})
	require.NoError(t, err)

	t.Run("__self available in validate when condition", func(t *testing.T) {
		executor := NewExecutor(registry)

		// Resolver with non-empty value and when condition checking __self
		selfExpr := celexp.Expression(`__self != ""`)
		resolvers := []*Resolver{
			{
				Name: "myParam",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs:   map[string]*ValueRef{"value": {Literal: "hello"}},
						},
					},
				},
				Validate: &ValidatePhase{
					When: &Condition{Expr: &selfExpr},
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs:   map[string]*ValueRef{"message": {Literal: "must match pattern"}},
						},
					},
				},
			},
		}

		ctx := context.Background()
		_, err := executor.Execute(ctx, resolvers, nil)
		// Validation should run (when condition is true because __self is "hello")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must match pattern")
	})

	t.Run("__self skips validation when empty", func(t *testing.T) {
		executor := NewExecutor(registry)

		// Resolver with empty value and when condition checking __self
		selfExpr := celexp.Expression(`__self != ""`)
		resolvers := []*Resolver{
			{
				Name: "myParam",
				Resolve: &ResolvePhase{
					With: []ProviderSource{
						{
							Provider: "static",
							Inputs:   map[string]*ValueRef{"value": {Literal: ""}},
						},
					},
				},
				Validate: &ValidatePhase{
					When: &Condition{Expr: &selfExpr},
					With: []ProviderValidation{
						{
							Provider: "validation",
							Inputs:   map[string]*ValueRef{"message": {Literal: "should not fire"}},
						},
					},
				},
			},
		}

		ctx := context.Background()
		_, err := executor.Execute(ctx, resolvers, nil)
		// Validation should be SKIPPED (when condition is false because __self is "")
		require.NoError(t, err)
	})
}
