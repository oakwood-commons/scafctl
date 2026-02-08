// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForEach_BasicIteration(t *testing.T) {
	registry := newMockRegistry()

	// Register a provider that doubles numbers
	err := registry.Register(&mockProvider{
		name: "double",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			val, ok := inputs["value"]
			if !ok {
				return nil, fmt.Errorf("missing value input")
			}
			switch v := val.(type) {
			case int:
				return &provider.Output{Data: v * 2}, nil
			case int64:
				return &provider.Output{Data: v * 2}, nil
			case float64:
				return &provider.Output{Data: int64(v) * 2}, nil
			default:
				return nil, fmt.Errorf("expected int, got %T", val)
			}
		},
	})
	require.NoError(t, err)

	// Register static provider
	err = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "doubled",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{1, 2, 3, 4, 5}},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "double",
						ForEach: &ForEachClause{
							Item: "num",
						},
						Inputs: map[string]*ValueRef{
							"value": {Expr: exprPtr("num")},
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

	value, ok := result.Get("doubled")
	require.True(t, ok)

	arr, ok := value.([]any)
	require.True(t, ok, "expected []any, got %T", value)
	assert.Equal(t, []any{int64(2), int64(4), int64(6), int64(8), int64(10)}, arr)
}

func TestForEach_WithIndex(t *testing.T) {
	registry := newMockRegistry()

	// Register a CEL provider that creates indexed objects
	err := registry.Register(&mockProvider{
		name: "cel",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			// Get iteration variables from context
			resolverData, _ := provider.ResolverContextFromContext(ctx)

			item := resolverData["__item"]
			index := resolverData["__index"]

			return &provider.Output{Data: map[string]any{
				"value": item,
				"index": index,
			}}, nil
		},
	})
	require.NoError(t, err)

	// Register static provider
	err = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "indexed",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{"a", "b", "c"}},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "cel",
						ForEach: &ForEachClause{
							Item:  "letter",
							Index: "i",
						},
						Inputs: map[string]*ValueRef{},
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

	value, ok := result.Get("indexed")
	require.True(t, ok)

	arr, ok := value.([]any)
	require.True(t, ok)
	require.Len(t, arr, 3)

	// Check each item has correct index
	for i, item := range arr {
		m, ok := item.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, i, m["index"])
	}
}

func TestForEach_EmptyArray(t *testing.T) {
	registry := newMockRegistry()

	// Register static provider
	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	// Provider that should never be called
	var callCount int32
	err = registry.Register(&mockProvider{
		name: "shouldNotBeCalled",
		executeFunc: func(_ context.Context, _ map[string]any) (*provider.Output, error) {
			atomic.AddInt32(&callCount, 1)
			return &provider.Output{Data: "called"}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "empty",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{}},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "shouldNotBeCalled",
						ForEach:  &ForEachClause{},
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

	value, ok := result.Get("empty")
	require.True(t, ok)

	arr, ok := value.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 0)
	assert.Equal(t, int32(0), atomic.LoadInt32(&callCount), "provider should not have been called")
}

func TestForEach_NonArrayError(t *testing.T) {
	registry := newMockRegistry()

	// Register static provider
	err := registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	err = registry.Register(&mockProvider{
		name: "identity",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "notArray",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: "not an array"},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "identity",
						ForEach:  &ForEachClause{},
						Inputs: map[string]*ValueRef{
							"value": {Expr: exprPtr("__item")},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	_, err = executor.Execute(ctx, resolvers, nil)
	require.Error(t, err)

	var typeErr *ForEachTypeError
	assert.True(t, ErrorAs(err, &typeErr), "expected ForEachTypeError, got %T: %v", err, err)
}

func TestForEach_ConcurrencyLimit(t *testing.T) {
	registry := newMockRegistry()

	// Track concurrent executions
	var concurrent int32
	var maxConcurrent int32

	err := registry.Register(&mockProvider{
		name: "tracked",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			current := atomic.AddInt32(&concurrent, 1)
			defer atomic.AddInt32(&concurrent, -1)

			// Track max concurrent
			for {
				old := atomic.LoadInt32(&maxConcurrent)
				if current <= old || atomic.CompareAndSwapInt32(&maxConcurrent, old, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	// Register static provider
	err = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "limited",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{1, 2, 3, 4, 5, 6, 7, 8}},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "tracked",
						ForEach: &ForEachClause{
							Concurrency: 2, // Limit to 2 concurrent
						},
						Inputs: map[string]*ValueRef{
							"value": {Expr: exprPtr("__item")},
						},
					},
				},
			},
		},
	}

	ctx := context.Background()
	_, err = executor.Execute(ctx, resolvers, nil)
	require.NoError(t, err)

	assert.LessOrEqual(t, atomic.LoadInt32(&maxConcurrent), int32(2),
		"max concurrent should not exceed limit of 2")
}

func TestForEach_OrderPreservation(t *testing.T) {
	registry := newMockRegistry()

	// Provider with variable delays to test order preservation
	err := registry.Register(&mockProvider{
		name: "delayedIdentity",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			val := inputs["value"]
			delay := inputs["delay"].(time.Duration)
			time.Sleep(delay)
			return &provider.Output{Data: val}, nil
		},
	})
	require.NoError(t, err)

	// Register static provider
	err = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	// Items with varying delays - later items complete faster
	resolvers := []*Resolver{
		{
			Name: "ordered",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{
								map[string]any{"id": 1, "delay": 100 * time.Millisecond},
								map[string]any{"id": 2, "delay": 50 * time.Millisecond},
								map[string]any{"id": 3, "delay": 10 * time.Millisecond},
							}},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "delayedIdentity",
						ForEach: &ForEachClause{
							Item: "item",
						},
						Inputs: map[string]*ValueRef{
							"value": {Expr: exprPtr("item.id")},
							"delay": {Expr: exprPtr("item.delay")},
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
	value, ok := result.Get("ordered")
	require.True(t, ok)

	arr, ok := value.([]any)
	require.True(t, ok)

	// Should preserve input order despite different completion times
	// CEL converts integers to int64
	expected := []any{int64(1), int64(2), int64(3)}
	assert.Equal(t, expected, arr, "order should be preserved")
}

func TestForEach_WithWhenCondition(t *testing.T) {
	registry := newMockRegistry()

	// Register identity provider
	err := registry.Register(&mockProvider{
		name: "identity",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			resolverData, _ := provider.ResolverContextFromContext(ctx)
			return &provider.Output{Data: resolverData["__item"]}, nil
		},
	})
	require.NoError(t, err)

	// Register static provider
	err = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	// Only process even numbers
	resolvers := []*Resolver{
		{
			Name: "filtered",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{1, 2, 3, 4, 5, 6}},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "identity",
						ForEach: &ForEachClause{
							Item: "num",
						},
						When: &Condition{
							Expr: exprPtr("num % 2 == 0"),
						},
						Inputs: map[string]*ValueRef{},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)
	require.NoError(t, err)

	result, _ := FromContext(ctx)
	value, ok := result.Get("filtered")
	require.True(t, ok)

	arr, ok := value.([]any)
	require.True(t, ok)

	// Should have 6 items: nil for odd, values for even
	require.Len(t, arr, 6)
	assert.Nil(t, arr[0], "odd indices should be nil")
	assert.Equal(t, 2, arr[1])
	assert.Nil(t, arr[2])
	assert.Equal(t, 4, arr[3])
	assert.Nil(t, arr[4])
	assert.Equal(t, 6, arr[5])
}

func TestForEach_OnErrorContinue(t *testing.T) {
	registry := newMockRegistry()

	// Provider that fails on specific items
	err := registry.Register(&mockProvider{
		name: "mayFail",
		executeFunc: func(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
			resolverData, _ := provider.ResolverContextFromContext(ctx)
			item := resolverData["__item"].(int)
			if item%2 == 0 {
				return nil, fmt.Errorf("even numbers fail")
			}
			return &provider.Output{Data: item * 10}, nil
		},
	})
	require.NoError(t, err)

	// Register static provider
	err = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name: "partial",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{1, 2, 3, 4, 5}},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "mayFail",
						ForEach: &ForEachClause{
							Item: "num",
						},
						OnError: ErrorBehaviorContinue,
						Inputs:  map[string]*ValueRef{},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)
	require.NoError(t, err, "should not error with onError: continue")

	result, _ := FromContext(ctx)
	value, ok := result.Get("partial")
	require.True(t, ok)

	arr, ok := value.([]any)
	require.True(t, ok)
	require.Len(t, arr, 5)

	// Check successful items have data, failed items have error
	for i, item := range arr {
		res, ok := item.(ForEachIterationResult)
		require.True(t, ok, "expected ForEachIterationResult at index %d", i)

		if (i+1)%2 == 0 { // Even items (2, 4) - indices 1, 3
			assert.NotEmpty(t, res.Error, "even items should have error")
			assert.Nil(t, res.Data, "failed items should have nil data")
		} else { // Odd items (1, 3, 5) - indices 0, 2, 4
			assert.Empty(t, res.Error, "odd items should succeed")
			assert.Equal(t, (i+1)*10, res.Data, "successful items should have transformed data")
		}
	}
}

func TestForEach_CustomInSource(t *testing.T) {
	registry := newMockRegistry()

	// Register identity provider
	err := registry.Register(&mockProvider{
		name: "identity",
		executeFunc: func(ctx context.Context, _ map[string]any) (*provider.Output, error) {
			resolverData, _ := provider.ResolverContextFromContext(ctx)
			return &provider.Output{Data: resolverData["__item"]}, nil
		},
	})
	require.NoError(t, err)

	// Register static provider
	err = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	// Use a different resolver's value as the iteration source
	resolvers := []*Resolver{
		{
			Name: "source",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{"x", "y", "z"}},
						},
					},
				},
			},
		},
		{
			Name: "fromOther",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: "ignored"}, // This will be overwritten by forEach
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "identity",
						ForEach: &ForEachClause{
							Item: "letter",
							In: &ValueRef{
								Resolver: stringPtr("source"),
							},
						},
						Inputs: map[string]*ValueRef{},
					},
				},
			},
		},
	}

	ctx := context.Background()
	ctx, err = executor.Execute(ctx, resolvers, nil)
	require.NoError(t, err)

	result, _ := FromContext(ctx)
	value, ok := result.Get("fromOther")
	require.True(t, ok)

	arr, ok := value.([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"x", "y", "z"}, arr)
}

func TestForEach_ChainedForEach(t *testing.T) {
	registry := newMockRegistry()

	// Provider that doubles values
	err := registry.Register(&mockProvider{
		name: "double",
		executeFunc: func(ctx context.Context, _ map[string]any) (*provider.Output, error) {
			resolverData, _ := provider.ResolverContextFromContext(ctx)
			item := resolverData["__item"]
			switch v := item.(type) {
			case int:
				return &provider.Output{Data: v * 2}, nil
			case float64:
				return &provider.Output{Data: int(v) * 2}, nil
			}
			return nil, fmt.Errorf("unexpected type: %T", item)
		},
	})
	require.NoError(t, err)

	// Provider that adds 1
	err = registry.Register(&mockProvider{
		name: "addOne",
		executeFunc: func(ctx context.Context, _ map[string]any) (*provider.Output, error) {
			resolverData, _ := provider.ResolverContextFromContext(ctx)
			item := resolverData["__item"]
			switch v := item.(type) {
			case int:
				return &provider.Output{Data: v + 1}, nil
			case float64:
				return &provider.Output{Data: int(v) + 1}, nil
			}
			return nil, fmt.Errorf("unexpected type: %T", item)
		},
	})
	require.NoError(t, err)

	// Register static provider
	err = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	require.NoError(t, err)

	executor := NewExecutor(registry)

	// Chain two forEach transforms: double then add 1
	resolvers := []*Resolver{
		{
			Name: "chained",
			Type: TypeArray,
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs: map[string]*ValueRef{
							"value": {Literal: []any{1, 2, 3}},
						},
					},
				},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{
					{
						Provider: "double",
						ForEach:  &ForEachClause{},
						Inputs:   map[string]*ValueRef{},
					},
					{
						Provider: "addOne",
						ForEach:  &ForEachClause{},
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
	value, ok := result.Get("chained")
	require.True(t, ok)

	arr, ok := value.([]any)
	require.True(t, ok)
	// [1,2,3] -> double -> [2,4,6] -> addOne -> [3,5,7]
	assert.Equal(t, []any{3, 5, 7}, arr)
}

// Helper function to create expression pointers
func exprPtr(s string) *celexp.Expression {
	expr := celexp.Expression(s)
	return &expr
}
