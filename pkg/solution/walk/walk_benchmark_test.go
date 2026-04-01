// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

func buildTestSolution(resolverCount, actionCount int) *solution.Solution {
	resolvers := make(map[string]*resolver.Resolver, resolverCount)
	for i := 0; i < resolverCount; i++ {
		name := "resolver-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		resolvers[name] = &resolver.Resolver{
			Name: name,
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "static",
						Inputs:   map[string]*spec.ValueRef{"value": {Literal: "test"}},
					},
				},
			},
		}
	}

	trueExpr := celexp.Expression("true")

	actions := make(map[string]*action.Action, actionCount)
	for i := 0; i < actionCount; i++ {
		name := "action-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		actions[name] = &action.Action{
			Name:     name,
			Provider: "static",
			Inputs:   map[string]*spec.ValueRef{"value": {Literal: "test"}},
			When: &spec.Condition{
				Expr: &trueExpr,
			},
		}
	}

	return &solution.Solution{
		Metadata: solution.Metadata{Name: "bench-solution", Version: semver.MustParse("1.0.0")},
		Spec: solution.Spec{
			Resolvers: resolvers,
			Workflow: &action.Workflow{
				Actions: actions,
			},
		},
	}
}

func BenchmarkWalk(b *testing.B) {
	b.Run("small_5resolvers_5actions", func(b *testing.B) {
		sol := buildTestSolution(5, 5)
		visitor := &Visitor{
			Resolver: func(_, _ string, _ *resolver.Resolver) error { return nil },
			Action:   func(_, _, _ string, _ *action.Action) error { return nil },
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = Walk(sol, visitor)
		}
	})

	b.Run("medium_20resolvers_20actions", func(b *testing.B) {
		sol := buildTestSolution(20, 20)
		visitor := &Visitor{
			Resolver:       func(_, _ string, _ *resolver.Resolver) error { return nil },
			Action:         func(_, _, _ string, _ *action.Action) error { return nil },
			ProviderSource: func(_ string, _ *resolver.ProviderSource) error { return nil },
			ValueRef:       func(_ string, _ *spec.ValueRef) error { return nil },
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = Walk(sol, visitor)
		}
	})

	b.Run("large_50resolvers_50actions", func(b *testing.B) {
		sol := buildTestSolution(50, 50)
		visitor := &Visitor{
			Resolver:       func(_, _ string, _ *resolver.Resolver) error { return nil },
			Action:         func(_, _, _ string, _ *action.Action) error { return nil },
			ProviderSource: func(_ string, _ *resolver.ProviderSource) error { return nil },
			ValueRef:       func(_ string, _ *spec.ValueRef) error { return nil },
			Condition:      func(_, _ string, _ *celexp.Expression) error { return nil },
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_ = Walk(sol, visitor)
		}
	})
}
