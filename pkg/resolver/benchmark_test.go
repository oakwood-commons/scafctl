// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"context"
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkExecutor_SingleResolver(b *testing.B) {
	registry := newMockRegistry()
	_ = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, _ map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: "bench-value"}, nil
		},
	})

	resolvers := []*Resolver{
		{
			Name: "single",
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs:   map[string]*ValueRef{"value": {Literal: "hello"}},
					},
				},
			},
		},
	}

	executor := NewExecutor(registry)

	b.ResetTimer()
	for b.Loop() {
		ctx := context.Background()
		_, _ = executor.Execute(ctx, resolvers, nil)
	}
}

func BenchmarkExecutor_ParallelResolvers(b *testing.B) {
	registry := newMockRegistry()
	_ = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, _ map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: "bench-value"}, nil
		},
	})

	// 10 independent resolvers that can all run in the same phase
	resolvers := make([]*Resolver, 10)
	for i := 0; i < 10; i++ {
		resolvers[i] = &Resolver{
			Name: fmt.Sprintf("resolver%d", i),
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs:   map[string]*ValueRef{"value": {Literal: fmt.Sprintf("val%d", i)}},
					},
				},
			},
		}
	}

	executor := NewExecutor(registry)

	b.ResetTimer()
	for b.Loop() {
		ctx := context.Background()
		_, _ = executor.Execute(ctx, resolvers, nil)
	}
}

func BenchmarkExecutor_DependencyChain(b *testing.B) {
	registry := newMockRegistry()
	_ = registry.Register(&mockProvider{
		name: "static",
		executeFunc: func(_ context.Context, _ map[string]any) (*provider.Output, error) {
			return &provider.Output{Data: "chain-value"}, nil
		},
	})

	// Chain: r0 -> r1 -> r2 -> r3 -> r4
	resolvers := make([]*Resolver, 5)
	for i := 0; i < 5; i++ {
		r := &Resolver{
			Name: fmt.Sprintf("r%d", i),
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs:   map[string]*ValueRef{"value": {Literal: "data"}},
					},
				},
			},
		}
		if i > 0 {
			r.DependsOn = []string{fmt.Sprintf("r%d", i-1)}
		}
		resolvers[i] = r
	}

	executor := NewExecutor(registry)

	b.ResetTimer()
	for b.Loop() {
		ctx := context.Background()
		_, _ = executor.Execute(ctx, resolvers, nil)
	}
}

func BenchmarkExtractDependencies(b *testing.B) {
	r := &Resolver{
		Name:      "complex",
		DependsOn: []string{"dep1", "dep2"},
		Resolve: &ResolvePhase{
			With: []ProviderSource{
				{
					Provider: "static",
					Inputs: map[string]*ValueRef{
						"val1": {Resolver: stringPtr("ref1")},
						"val2": {Resolver: stringPtr("ref2")},
						"val3": {Literal: "plain"},
					},
				},
			},
		},
	}

	// nil lookup is safe for static inputs without provider-specific deps
	var lookup DescriptorLookup

	b.ResetTimer()
	for b.Loop() {
		_ = extractDependencies(r, lookup)
	}
}

func BenchmarkBuildGraph_Small(b *testing.B) {
	resolvers := make([]*Resolver, 10)
	for i := 0; i < 10; i++ {
		r := &Resolver{
			Name: fmt.Sprintf("r%d", i),
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs:   map[string]*ValueRef{"value": {Literal: "v"}},
					},
				},
			},
		}
		if i > 0 {
			r.DependsOn = []string{fmt.Sprintf("r%d", i-1)}
		}
		resolvers[i] = r
	}

	var lookup DescriptorLookup

	b.ResetTimer()
	for b.Loop() {
		_, _ = BuildGraph(resolvers, lookup)
	}
}

func BenchmarkBuildGraph_Large(b *testing.B) {
	resolvers := make([]*Resolver, 100)
	for i := 0; i < 100; i++ {
		r := &Resolver{
			Name: fmt.Sprintf("r%d", i),
			Resolve: &ResolvePhase{
				With: []ProviderSource{
					{
						Provider: "static",
						Inputs:   map[string]*ValueRef{"value": {Literal: "v"}},
					},
				},
			},
		}
		if i > 0 {
			r.DependsOn = []string{fmt.Sprintf("r%d", i-1)}
		}
		resolvers[i] = r
	}

	var lookup DescriptorLookup

	b.ResetTimer()
	for b.Loop() {
		_, _ = BuildGraph(resolvers, lookup)
	}
}
