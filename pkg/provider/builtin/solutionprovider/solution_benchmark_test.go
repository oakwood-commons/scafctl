// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solutionprovider

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

func BenchmarkSolutionProvider_From(b *testing.B) {
	reg := provider.NewRegistry()
	_ = reg.Register(&mockProvider{
		name:   "static",
		output: &provider.Output{Data: "bench-val"},
	})

	child := &solution.Solution{
		Metadata: solution.Metadata{Name: "bench-child"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Name: "r1",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "static"}},
					},
				},
				"r2": {
					Name:      "r2",
					DependsOn: []string{"r1"},
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "static"}},
					},
				},
			},
		},
	}

	p := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"bench@1.0.0": child}}),
		WithRegistry(reg),
	)
	ctx := provider.WithExecutionMode(testContext(), provider.CapabilityFrom)
	input := &Input{Source: "bench@1.0.0"}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := p.Execute(ctx, input)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSolutionProvider_Action(b *testing.B) {
	reg := provider.NewRegistry()
	_ = reg.Register(&mockProvider{
		name:   "static",
		output: &provider.Output{Data: map[string]any{"success": true}},
	})

	child := &solution.Solution{
		Metadata: solution.Metadata{Name: "bench-action-child"},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"cfg": {
					Name: "cfg",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "static"}},
					},
				},
			},
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"step1": {Name: "step1", Provider: "static"},
					"step2": {Name: "step2", Provider: "static", DependsOn: []string{"step1"}},
					"opt":   {Name: "opt", Provider: "static", Explicit: true},
				},
			},
		},
	}

	solProv := New(
		WithLoader(&mockLoader{solutions: map[string]*solution.Solution{"bench-wf@1.0.0": child}}),
		WithRegistry(reg),
	)
	_ = reg.Register(solProv)

	parentWorkflow := &action.Workflow{
		Actions: map[string]*action.Action{
			"run-child": {
				Name:     "run-child",
				Provider: ProviderName,
				Inputs: map[string]*spec.ValueRef{
					"source": {Literal: "bench-wf@1.0.0"},
				},
			},
		},
	}

	ctx := testContext()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		exec := action.NewExecutor(
			action.WithRegistry(reg),
			action.WithProgressCallback(action.NoOpProgressCallback{}),
		)
		if _, err := exec.Execute(ctx, parentWorkflow); err != nil {
			b.Fatal(err)
		}
	}
}
