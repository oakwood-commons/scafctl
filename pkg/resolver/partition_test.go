// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"sort"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
)

// keys returns the sorted keys of a string-set for stable assertions.
func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestResolversUsingProvider(t *testing.T) {
	resolvers := []*Resolver{
		{
			Name: "reads_state",
			Resolve: &ResolvePhase{
				With: []ProviderSource{{Provider: "state"}},
			},
		},
		{
			Name: "reads_state_in_transform",
			Resolve: &ResolvePhase{
				With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "x"}}}},
			},
			Transform: &TransformPhase{
				With: []ProviderTransform{{Provider: "state"}},
			},
		},
		{
			Name: "plain",
			Resolve: &ResolvePhase{
				With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "y"}}}},
			},
		},
	}

	got := ResolversUsingProvider(resolvers, nil, "state")
	assert.Equal(t, []string{"reads_state", "reads_state_in_transform"}, keys(got))
}

func TestResolversUsingProvider_EmptyProviderName(t *testing.T) {
	resolvers := []*Resolver{{Name: "a", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "state"}}}}}
	got := ResolversUsingProvider(resolvers, nil, "")
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestResolversUsingProvider_ViaCall(t *testing.T) {
	resolvers := []*Resolver{
		{
			Name: "via_call",
			Resolve: &ResolvePhase{
				With: []ProviderSource{{CallRef: spec.CallRef{Call: "read_state"}}},
			},
		},
		{
			Name: "via_other_call",
			Resolve: &ResolvePhase{
				With: []ProviderSource{{CallRef: spec.CallRef{Call: "read_http"}}},
			},
		},
	}
	calls := map[string]*spec.Call{
		"read_state": {Provider: "state"},
		"read_http":  {Provider: "http"},
	}

	got := ResolversUsingProvider(resolvers, calls, "state")
	assert.Equal(t, []string{"via_call"}, keys(got))
}

func TestTransitiveDependents(t *testing.T) {
	// c depends on b depends on a. Seed {a} => {a, b, c}.
	resolvers := []*Resolver{
		{Name: "a", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "x"}}}}}},
		{Name: "b", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr("_.a")}}}}}},
		{Name: "c", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr("_.b")}}}}}},
		{Name: "unrelated", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "z"}}}}}},
	}

	got := TransitiveDependents(resolvers, nil, nil, map[string]bool{"a": true})
	assert.Equal(t, []string{"a", "b", "c"}, keys(got))
}

func TestTransitiveDependents_EmptySeed(t *testing.T) {
	resolvers := []*Resolver{{Name: "a", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static"}}}}}
	got := TransitiveDependents(resolvers, nil, nil, nil)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestProviderDependencyClosure(t *testing.T) {
	// b reads state; c depends on b; d depends on c; e is independent.
	resolvers := []*Resolver{
		{Name: "b", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "state"}}}},
		{Name: "c", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr("_.b")}}}}}},
		{Name: "d", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr("_.c")}}}}}},
		{Name: "e", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "x"}}}}}},
	}

	got := ProviderDependencyClosure(resolvers, nil, nil, "state")
	assert.Equal(t, []string{"b", "c", "d"}, keys(got))
}

func TestTransitiveDependencies(t *testing.T) {
	// c depends on b depends on a. Roots {c} => {a, b, c}.
	resolvers := []*Resolver{
		{Name: "a", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static", Inputs: map[string]*ValueRef{"value": {Literal: "x"}}}}}},
		{Name: "b", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr("_.a")}}}}}},
		{Name: "c", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr("_.b")}}}}}},
		{Name: "unrelated", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr("_.a")}}}}}},
	}

	got := TransitiveDependencies(resolvers, nil, nil, map[string]bool{"c": true})
	assert.Equal(t, []string{"a", "b", "c"}, keys(got))
}

func TestTransitiveDependencies_EmptyRoots(t *testing.T) {
	resolvers := []*Resolver{{Name: "a", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "static"}}}}}
	got := TransitiveDependencies(resolvers, nil, nil, nil)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func BenchmarkProviderDependencyClosure(b *testing.B) {
	// Chain of 100 resolvers r0..r99 where r0 reads state and each ri depends on ri-1.
	const n = 100
	resolvers := make([]*Resolver, 0, n)
	resolvers = append(resolvers, &Resolver{Name: "r0", Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "state"}}}})
	for i := 1; i < n; i++ {
		prev := "_.r" + itoa(i-1)
		resolvers = append(resolvers, &Resolver{
			Name:    "r" + itoa(i),
			Resolve: &ResolvePhase{With: []ProviderSource{{Provider: "cel", Inputs: map[string]*ValueRef{"expr": {Expr: celExpPtr(prev)}}}}},
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ProviderDependencyClosure(resolvers, nil, nil, "state")
	}
}

// itoa is a tiny local int-to-string to avoid importing strconv in the benchmark.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [8]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
