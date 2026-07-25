// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingRegistry wraps a static provider whose executions are counted, so
// tests can prove a seeded resolver never invokes its provider.
func newCountingRegistry(counter *int64) *mockRegistry {
	registry := newMockRegistry()
	_ = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			atomic.AddInt64(counter, 1)
			return &provider.Output{Data: inputs["value"]}, nil
		},
	})
	_ = registry.Register(&mockProvider{
		name: "cel",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			// The cel provider echoes its resolved "expr" input, which the
			// executor has already materialized from ValueRefs.
			return &provider.Output{Data: inputs["expr"]}, nil
		},
	})
	return registry
}

func TestExecutor_SeededResult_SkipsExecution(t *testing.T) {
	var calls int64
	registry := newCountingRegistry(&calls)
	executor := NewExecutor(registry, WithSeededResults(map[string]*ExecutionResult{
		"seeded": {Value: "from-preload", Status: ExecutionStatusSuccess},
	}))

	resolvers := []*Resolver{
		{
			Name:    "seeded",
			Type:    TypeString,
			Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "should-not-run"}}}}},
		},
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	result, ok := FromContext(ctx)
	require.True(t, ok)

	value, ok := result.Get("seeded")
	require.True(t, ok)
	assert.Equal(t, "from-preload", value)
	assert.Equal(t, int64(0), atomic.LoadInt64(&calls), "seeded resolver must not invoke its provider")
}

func TestExecutor_SeededResult_VisibleToDependents(t *testing.T) {
	var calls int64
	registry := newCountingRegistry(&calls)
	executor := NewExecutor(registry, WithSeededResults(map[string]*ExecutionResult{
		"base": {Value: "seed-value", Status: ExecutionStatusSuccess},
	}))

	resolvers := []*Resolver{
		{
			Name:    "base",
			Type:    TypeString,
			Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "unused"}}}}},
		},
		{
			Name:    "dependent",
			Type:    TypeString,
			Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr("_.base")}}}}},
		},
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	result, ok := FromContext(ctx)
	require.True(t, ok)

	// dependent reads the seeded value.
	value, ok := result.Get("dependent")
	require.True(t, ok)
	assert.Equal(t, "seed-value", value)

	// base never invoked the static provider; only dependent ran (cel).
	assert.Equal(t, int64(0), atomic.LoadInt64(&calls), "seeded base resolver must not invoke static provider")
}

func TestExecutor_SeededResult_NilEntryRunsNormally(t *testing.T) {
	var calls int64
	registry := newCountingRegistry(&calls)
	executor := NewExecutor(registry, WithSeededResults(map[string]*ExecutionResult{
		"normal": nil, // nil seed entry is treated as absent
	}))

	resolvers := []*Resolver{
		{
			Name:    "normal",
			Type:    TypeString,
			Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "ran"}}}}},
		},
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	result, ok := FromContext(ctx)
	require.True(t, ok)

	value, ok := result.Get("normal")
	require.True(t, ok)
	assert.Equal(t, "ran", value)
	assert.Equal(t, int64(1), atomic.LoadInt64(&calls), "resolver with nil seed entry must execute normally")
}

func TestExecutor_NoSeed_ExecutesNormally(t *testing.T) {
	var calls int64
	registry := newCountingRegistry(&calls)
	executor := NewExecutor(registry)

	resolvers := []*Resolver{
		{
			Name:    "plain",
			Type:    TypeString,
			Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "v"}}}}},
		},
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	result, ok := FromContext(ctx)
	require.True(t, ok)
	value, ok := result.Get("plain")
	require.True(t, ok)
	assert.Equal(t, "v", value)
	assert.Equal(t, int64(1), atomic.LoadInt64(&calls))
}
