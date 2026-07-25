// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"sort"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func strptr(s string) *string { return &s }

func exprPtr(expr string) *celexp.Expression {
	e := celexp.Expression(expr)
	return &e
}

// staticResolver builds a state-independent resolver.
func staticResolver(name string) *resolver.Resolver {
	return &resolver.Resolver{
		Name:    name,
		Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "static", Inputs: map[string]*resolver.ValueRef{"value": {Literal: "x"}}}}},
	}
}

// stateReadingResolver builds a resolver that reads the state snapshot.
func stateReadingResolver(name string) *resolver.Resolver {
	return &resolver.Resolver{
		Name:    name,
		Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: ReadProviderName}}},
	}
}

// rslvrRef builds a ValueRef that references another resolver by name.
func rslvrRef(name string) *spec.ValueRef {
	return &spec.ValueRef{Resolver: strptr(name)}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestBuildPartition(t *testing.T) {
	resolvers := []*resolver.Resolver{
		staticResolver("independent"),
		stateReadingResolver("reads_state"),
	}
	part := BuildPartition(resolvers, nil, nil, ReadProviderName)

	assert.True(t, part.IsKnown("independent"))
	assert.True(t, part.IsKnown("reads_state"))
	assert.False(t, part.IsKnown("missing"))

	assert.False(t, part.IsStateDependent("independent"))
	assert.True(t, part.IsStateDependent("reads_state"))
}

func TestPartition_NilSafe(t *testing.T) {
	var part *Partition
	assert.False(t, part.IsKnown("anything"))
	assert.False(t, part.IsStateDependent("anything"))
}

func TestValidateStateRefs_AllowsStateIndependent(t *testing.T) {
	resolvers := []*resolver.Resolver{staticResolver("app_name")}
	part := BuildPartition(resolvers, nil, nil, ReadProviderName)

	cfg := &Config{
		Enabled: &spec.ValueRef{Literal: true},
		Backend: Backend{
			Provider: "file",
			Inputs:   map[string]*spec.ValueRef{"path": rslvrRef("app_name")},
		},
	}

	assert.NoError(t, ValidateStateRefs(cfg, part))
}

func TestValidateStateRefs_RejectsStateDependent(t *testing.T) {
	resolvers := []*resolver.Resolver{stateReadingResolver("saved")}
	part := BuildPartition(resolvers, nil, nil, ReadProviderName)

	cfg := &Config{
		Enabled: rslvrRef("saved"),
		Backend: Backend{Provider: "file", Inputs: map[string]*spec.ValueRef{"path": {Literal: "s.json"}}},
	}

	err := ValidateStateRefs(cfg, part)
	require.Error(t, err)
	var cycleErr *CycleError
	require.True(t, errors.As(err, &cycleErr))
	assert.Equal(t, "state.enabled", cycleErr.Location)
	assert.Equal(t, []string{"saved"}, cycleErr.Refs)
}

func TestValidateStateRefs_RejectsUnknown(t *testing.T) {
	resolvers := []*resolver.Resolver{staticResolver("known")}
	part := BuildPartition(resolvers, nil, nil, ReadProviderName)

	cfg := &Config{
		Enabled: &spec.ValueRef{Literal: true},
		Backend: Backend{Provider: "file", Inputs: map[string]*spec.ValueRef{"path": rslvrRef("typo")}},
	}

	err := ValidateStateRefs(cfg, part)
	require.Error(t, err)
	var unknownErr *UnknownStateRefError
	require.True(t, errors.As(err, &unknownErr))
	assert.Equal(t, "state.backend.inputs.path", unknownErr.Location)
	assert.Equal(t, []string{"typo"}, unknownErr.Refs)
}

func TestValidateStateRefs_UnknownTakesPriorityOverDependent(t *testing.T) {
	// A single field references both an unknown and a state-dependent resolver;
	// the unknown reference is reported first.
	resolvers := []*resolver.Resolver{stateReadingResolver("saved")}
	part := BuildPartition(resolvers, nil, nil, ReadProviderName)

	cfg := &Config{
		Enabled: &spec.ValueRef{Expr: exprPtr("_.saved + _.ghost")},
		Backend: Backend{Provider: "file", Inputs: map[string]*spec.ValueRef{"path": {Literal: "s.json"}}},
	}

	err := ValidateStateRefs(cfg, part)
	require.Error(t, err)
	var unknownErr *UnknownStateRefError
	require.True(t, errors.As(err, &unknownErr), "unknown ref should take priority: got %T", err)
	assert.Equal(t, []string{"ghost"}, unknownErr.Refs)
}

func TestPhaseARoots(t *testing.T) {
	resolvers := []*resolver.Resolver{
		staticResolver("app_name"),
		staticResolver("region"),
		stateReadingResolver("saved"),
	}
	part := BuildPartition(resolvers, nil, nil, ReadProviderName)

	cfg := &Config{
		Enabled: rslvrRef("region"),
		Backend: Backend{
			Provider: "file",
			Inputs:   map[string]*spec.ValueRef{"path": rslvrRef("app_name")},
		},
	}

	roots := PhaseARoots(cfg, part)
	assert.Equal(t, []string{"app_name", "region"}, sortedKeys(roots))
}

func TestPhaseARoots_ExcludesStateDependentAndUnknown(t *testing.T) {
	resolvers := []*resolver.Resolver{
		staticResolver("ok"),
		stateReadingResolver("saved"),
	}
	part := BuildPartition(resolvers, nil, nil, ReadProviderName)

	cfg := &Config{
		Enabled: &spec.ValueRef{Expr: exprPtr("_.ok")},
		Backend: Backend{
			Provider: "file",
			// "saved" is state-dependent and "ghost" is unknown; neither is a root.
			Inputs: map[string]*spec.ValueRef{"path": {Expr: exprPtr("_.saved + _.ghost")}},
		},
	}

	roots := PhaseARoots(cfg, part)
	assert.Equal(t, []string{"ok"}, sortedKeys(roots))
}
