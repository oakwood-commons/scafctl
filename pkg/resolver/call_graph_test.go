// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolver

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
)

func expr(s string) *spec.ValueRef {
	e := celexp.Expression(s)
	return &spec.ValueRef{Expr: &e}
}

func TestExtractDependenciesWithCalls_IsolatedDefinition(t *testing.T) {
	// Call definitions are strictly isolated: a definition input that references
	// a resolver ("baseURL"/"credentials") is a validation error, not an
	// auto-wired dependency. The graph must therefore NOT pull those names into
	// the invoking resolver's dependencies.
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Inputs: map[string]*spec.ValueRef{
				"uri":   expr("_.baseURL + \"/\" + _.args.id"),
				"token": expr("_.credentials.token"),
			},
		},
	}
	r := &Resolver{
		Name: "user",
		Resolve: &ResolvePhase{With: []ProviderSource{{
			CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}},
		}}},
	}

	deps := extractDependenciesWithCalls(r, nil, calls)
	assert.NotContains(t, deps, "baseURL")
	assert.NotContains(t, deps, "credentials")
	assert.NotContains(t, deps, "args")
	assert.NotContains(t, deps, "user")
}

func TestExtractDependenciesWithCalls_NoCalls(t *testing.T) {
	r := &Resolver{
		Name: "x",
		Resolve: &ResolvePhase{With: []ProviderSource{{
			Provider: "cel",
			Inputs:   map[string]*spec.ValueRef{"expression": expr("_.y")},
		}}},
	}
	deps := extractDependenciesWithCalls(r, nil, nil)
	assert.Contains(t, deps, "y")
}

func TestExtractStrictDependenciesWithCalls_IsolatedDefinition(t *testing.T) {
	calls := map[string]*spec.Call{
		"get": {
			Provider: "http",
			Inputs:   map[string]*spec.ValueRef{"uri": expr("_.baseURL")},
		},
	}
	r := &Resolver{
		Name: "user",
		Resolve: &ResolvePhase{With: []ProviderSource{{
			CallRef: spec.CallRef{Call: "get"},
		}}},
	}
	deps := extractStrictDependenciesWithCalls(r, calls)
	assert.False(t, deps["baseURL"])
	assert.False(t, deps["args"])
	assert.False(t, deps["user"])
}
