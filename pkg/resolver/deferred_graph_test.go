// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildDeferredReportingPlan(t *testing.T) {
	tests := []struct {
		name       string
		units      []DeferredValidationUnit
		wantOrder  []string
		wantCycles [][]string
	}{
		{
			name:      "empty units",
			units:     nil,
			wantOrder: []string{},
		},
		{
			name: "single unit with no owner deps",
			units: []DeferredValidationUnit{
				{ResolverName: "checker", DependsOn: []string{"env", "region"}},
			},
			// env/region have no deferred units, so they are not reporting nodes.
			wantOrder: []string{"checker"},
		},
		{
			name: "dependency reported before dependent (root-cause-first)",
			units: []DeferredValidationUnit{
				{ResolverName: "checker", DependsOn: []string{"env"}},
				{ResolverName: "env", DependsOn: []string{}},
			},
			wantOrder: []string{"env", "checker"},
		},
		{
			name: "chain a->b->c reports c,b,a",
			units: []DeferredValidationUnit{
				{ResolverName: "a", DependsOn: []string{"b"}},
				{ResolverName: "b", DependsOn: []string{"c"}},
				{ResolverName: "c", DependsOn: []string{}},
			},
			wantOrder: []string{"c", "b", "a"},
		},
		{
			name: "independent units ordered by name",
			units: []DeferredValidationUnit{
				{ResolverName: "zeta"},
				{ResolverName: "alpha"},
				{ResolverName: "mike"},
			},
			wantOrder: []string{"alpha", "mike", "zeta"},
		},
		{
			name: "two-node cycle is tolerated and name-ordered",
			units: []DeferredValidationUnit{
				{ResolverName: "b", DependsOn: []string{"a"}},
				{ResolverName: "a", DependsOn: []string{"b"}},
			},
			wantOrder:  []string{"a", "b"},
			wantCycles: [][]string{{"a", "b"}},
		},
		{
			name: "cycle plus an independent root-cause chain",
			units: []DeferredValidationUnit{
				{ResolverName: "a", DependsOn: []string{"b"}},
				{ResolverName: "b", DependsOn: []string{"a"}},
				{ResolverName: "root", DependsOn: []string{}},
				{ResolverName: "a2", DependsOn: []string{"root"}},
			},
			// Nodes are visited in sorted order (a, a2, b, root). Visiting "a"
			// first emits the {a,b} cycle before the root->a2 chain; within the
			// chain, root (dependency) precedes a2 (dependent).
			wantOrder:  []string{"a", "b", "root", "a2"},
			wantCycles: [][]string{{"a", "b"}},
		},
		{
			name: "self reference is ignored (no self cycle)",
			units: []DeferredValidationUnit{
				{ResolverName: "solo", DependsOn: []string{"solo"}},
			},
			wantOrder: []string{"solo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := buildDeferredReportingPlan(tt.units)
			assert.Equal(t, tt.wantOrder, plan.Order, "reporting order")
			assert.Equal(t, tt.wantCycles, plan.Cycles, "cycles")
		})
	}
}

func TestBuildDeferredReportingPlan_OrderIsDeterministic(t *testing.T) {
	units := []DeferredValidationUnit{
		{ResolverName: "d", DependsOn: []string{"a", "b"}},
		{ResolverName: "c", DependsOn: []string{"a"}},
		{ResolverName: "b", DependsOn: []string{}},
		{ResolverName: "a", DependsOn: []string{}},
	}
	first := buildDeferredReportingPlan(units).Order
	for i := 0; i < 20; i++ {
		got := buildDeferredReportingPlan(units).Order
		assert.Equal(t, first, got, "order must be stable across runs")
	}
	// a and b are roots; c depends on a; d depends on a,b. Roots come first.
	assert.Equal(t, []string{"a", "b", "c", "d"}, first)
}
