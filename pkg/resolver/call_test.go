// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// echoProvider returns the "value" input verbatim, tracking invocation count.
func echoProvider(counter *atomic.Int32) *mockProvider {
	return &mockProvider{
		name: "http",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			if counter != nil {
				counter.Add(1)
			}
			return &provider.Output{Data: inputs["value"]}, nil
		},
	}
}

func TestExecutor_Call_Basic(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(echoProvider(nil)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString, Required: true}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`_.args.id`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "user",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": {Literal: "abc"}},
				},
			}}},
		},
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	result, _ := FromContext(ctx)
	value, ok := result.Get("user")
	require.True(t, ok)
	assert.Equal(t, "abc", value)
}

func TestExecutor_Call_Dedup(t *testing.T) {
	registry := newMockRegistry()
	var count atomic.Int32
	require.NoError(t, registry.Register(echoProvider(&count)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Dedup:    true,
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`_.args.id`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	// Two independent resolvers invoking the same call with identical args.
	mkResolver := func(name string) *Resolver {
		return &Resolver{
			Name: name,
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": {Literal: "same"}},
				},
			}}},
		}
	}
	resolvers := []*Resolver{mkResolver("a"), mkResolver("b")}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	result, _ := FromContext(ctx)
	a, _ := result.Get("a")
	b, _ := result.Get("b")
	assert.Equal(t, "same", a)
	assert.Equal(t, "same", b)
	assert.Equal(t, int32(1), count.Load(), "dedup should collapse identical invocations to a single provider call")
}

func TestExecutor_Call_NoDedupDistinctArgs(t *testing.T) {
	registry := newMockRegistry()
	var count atomic.Int32
	require.NoError(t, registry.Register(echoProvider(&count)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Dedup:    true,
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`_.args.id`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "a",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "x"}}},
			}}},
		},
		{
			Name: "b",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "y"}}},
			}}},
		},
	}

	_, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), count.Load(), "distinct args must not be de-duplicated")
}

func TestExecutor_Call_ForEach(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(echoProvider(nil)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"v": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`_.args.v`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "items",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"v": expr(`__item`)},
				},
				ForEach: &ForEachClause{In: &spec.ValueRef{Literal: []any{"a", "b", "c"}}},
			}}},
		},
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	result, _ := FromContext(ctx)
	value, ok := result.Get("items")
	require.True(t, ok)
	assert.Equal(t, []any{"a", "b", "c"}, value)
}

func TestExecutor_Call_NotFound(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(echoProvider(nil)))

	executor := NewExecutor(registry, WithCalls(map[string]*spec.Call{}))
	resolvers := []*Resolver{
		{
			Name: "user",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{Call: "missing"},
				OnError: ErrorBehaviorFail,
			}}},
		},
	}

	_, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `call "missing" not found`)
}

func TestExecutor_Call_BindArgsError_MissingRequired(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(echoProvider(nil)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString, Required: true}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`_.args.id`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "user",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{Call: "get"}, // required "id" omitted
				OnError: ErrorBehaviorFail,
			}}},
		},
	}

	_, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}

func TestExecutor_Call_UnknownArg(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(echoProvider(nil)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`_.args.id`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "user",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"bogus": {Literal: "1"}},
				},
				OnError: ErrorBehaviorFail,
			}}},
		},
	}

	_, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bogus")
}

func TestExecutor_Call_NilArg(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(echoProvider(nil)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`_.args.id`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "user",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": nil},
				},
				OnError: ErrorBehaviorFail,
			}}},
		},
	}

	_, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no value (nil)")
}

func TestExecutor_Call_ProviderNotFound(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(echoProvider(nil)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "missing-provider",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`_.args.id`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "user",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": {Literal: "1"}},
				},
				OnError: ErrorBehaviorFail,
			}}},
		},
	}

	_, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing-provider")
}

func TestExecutor_Call_DefinitionInputNil(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(echoProvider(nil)))

	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": nil},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "user",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				CallRef: spec.CallRef{
					Call: "get",
					Args: map[string]*spec.ValueRef{"id": {Literal: "1"}},
				},
				OnError: ErrorBehaviorFail,
			}}},
		},
	}

	_, err := executor.Execute(context.Background(), resolvers, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), fmt.Sprintf("%q", "value"))
}

func TestExecutor_Call_Transform(t *testing.T) {
	registry := newMockRegistry()
	require.NoError(t, registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, inputs map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: inputs["value"]}, nil
		},
	}))
	require.NoError(t, registry.Register(echoProvider(nil)))

	calls := map[string]*spec.Call{
		"wrap": {
			Provider: "http",
			Args:     map[string]*spec.ArgDef{"src": {Type: spec.TypeString}},
			Inputs:   map[string]*spec.ValueRef{"value": expr(`"[" + _.args.src + "]"`)},
		},
	}

	executor := NewExecutor(registry, WithCalls(calls))
	resolvers := []*Resolver{
		{
			Name: "wrapped",
			Resolve: &ResolvePhase{With: []ProviderSource{{
				Provider: "static",
				Inputs:   map[string]*ValueRef{"value": {Literal: "hi"}},
			}}},
			Transform: &TransformPhase{With: []ProviderTransform{{
				CallRef: spec.CallRef{
					Call: "wrap",
					Args: map[string]*spec.ValueRef{"src": expr(`string(__self)`)},
				},
			}}},
		},
	}

	ctx, err := executor.Execute(context.Background(), resolvers, nil)
	require.NoError(t, err)

	result, _ := FromContext(ctx)
	value, ok := result.Get("wrapped")
	require.True(t, ok)
	assert.Equal(t, "[hi]", value)
}
