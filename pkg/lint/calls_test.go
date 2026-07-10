// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
)

func callLintRegistry() *provider.Registry {
	reg := provider.NewRegistry()
	_ = reg.Register(newFakeProvider("http", nil))
	_ = reg.Register(newFakeProvider("shell", nil))
	_ = reg.Register(newFakeProvider("parameter", nil))
	return reg
}

func exprRef(e string) *spec.ValueRef {
	expr := celexp.Expression(e)
	return &spec.ValueRef{Expr: &expr}
}

func TestLintCalls_ProviderNotFound(t *testing.T) {
	sol := &solution.Solution{Spec: solution.Spec{
		Calls: map[string]*spec.Call{
			"get": {Provider: "does-not-exist"},
		},
	}}
	result := &Result{}
	lintCalls(sol, result, callLintRegistry())
	assert.True(t, hasRuleName(result.Findings, "call-provider-not-found"))
}

func TestLintCalls_DefinitionResolverRef(t *testing.T) {
	sol := &solution.Solution{Spec: solution.Spec{
		Calls: map[string]*spec.Call{
			"get": {
				Provider: "http",
				Inputs:   map[string]*spec.ValueRef{"uri": exprRef("_.baseURL + \"/x\"")},
			},
		},
		Resolvers: map[string]*resolver.Resolver{
			"baseURL": {
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}},
			},
		},
	}}
	result := &Result{}
	lintCalls(sol, result, callLintRegistry())
	assert.True(t, hasRuleName(result.Findings, "call-definition-resolver-ref"))
	// Isolation is enforced: the finding must be an error, not an advisory notice.
	var found *Finding
	for _, f := range result.Findings {
		if f.RuleName == "call-definition-resolver-ref" {
			found = f
			break
		}
	}
	if assert.NotNil(t, found) {
		assert.Equal(t, SeverityError, found.Severity)
	}
}

// TestLintCalls_ArgsNamespaceNotFlagged verifies that a definition input using
// the reserved args namespace (_.args.x) is not reported as a resolver
// reference, even when the solution declares a resolver named "args".
func TestLintCalls_ArgsNamespaceNotFlagged(t *testing.T) {
	sol := &solution.Solution{Spec: solution.Spec{
		Calls: map[string]*spec.Call{
			"get": {
				Provider: "http",
				Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
				Inputs:   map[string]*spec.ValueRef{"uri": exprRef("_.args.id")},
			},
		},
		Resolvers: map[string]*resolver.Resolver{
			"args": {
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}},
			},
		},
	}}
	result := &Result{}
	lintCalls(sol, result, callLintRegistry())
	assert.False(t, hasRuleName(result.Findings, "call-definition-resolver-ref"))
}

func TestLintCalls_SiteRules(t *testing.T) {
	def := &spec.Call{
		Provider: "http",
		Args: map[string]*spec.ArgDef{
			"id": {Type: spec.TypeString, Required: true},
		},
	}

	tests := []struct {
		name     string
		source   resolver.ProviderSource
		wantRule string
	}{
		{
			name: "exclusive call and provider",
			source: resolver.ProviderSource{
				Provider: "http",
				CallRef:  spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}},
			},
			wantRule: "call-provider-exclusive",
		},
		{
			name:     "call not found",
			source:   resolver.ProviderSource{CallRef: spec.CallRef{Call: "missing", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}}},
			wantRule: "call-not-found",
		},
		{
			name: "unknown arg",
			source: resolver.ProviderSource{CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{
				"id":  {Literal: "1"},
				"bad": {Literal: "x"},
			}}},
			wantRule: "call-unknown-arg",
		},
		{
			name:     "missing required arg",
			source:   resolver.ProviderSource{CallRef: spec.CallRef{Call: "get"}},
			wantRule: "call-missing-arg",
		},
		{
			name: "args without call",
			source: resolver.ProviderSource{
				Provider: "http",
				CallRef:  spec.CallRef{Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}},
			},
			wantRule: "call-args-without-call",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sol := &solution.Solution{Spec: solution.Spec{
				Calls: map[string]*spec.Call{"get": def},
				Resolvers: map[string]*resolver.Resolver{
					"r": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{tt.source}}},
				},
			}}
			result := &Result{}
			lintCalls(sol, result, callLintRegistry())
			assert.True(t, hasRuleName(result.Findings, tt.wantRule), "expected rule %q", tt.wantRule)
		})
	}
}

func TestLintCalls_ActionSites(t *testing.T) {
	sol := &solution.Solution{Spec: solution.Spec{
		Calls: map[string]*spec.Call{
			"run": {Provider: "shell", Args: map[string]*spec.ArgDef{"cmd": {Required: true}}},
		},
		Workflow: &action.Workflow{
			Actions: map[string]*action.Action{
				"deploy": {
					Name:    "deploy",
					CallRef: spec.CallRef{Call: "run"}, // missing required cmd
				},
			},
		},
	}}
	result := &Result{}
	lintCalls(sol, result, callLintRegistry())
	assert.True(t, hasRuleName(result.Findings, "call-missing-arg"))
}

func TestLintCalls_ValidCallSiteNoFindings(t *testing.T) {
	sol := &solution.Solution{Spec: solution.Spec{
		Calls: map[string]*spec.Call{
			"get": {
				Provider: "http",
				Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString, Required: true}},
			},
		},
		Resolvers: map[string]*resolver.Resolver{
			"user": {
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{
					CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}},
				}}},
			},
		},
	}}
	result := &Result{}
	lintCalls(sol, result, callLintRegistry())

	for _, rule := range []string{
		"call-not-found", "call-provider-exclusive", "call-args-without-call",
		"call-missing-arg", "call-unknown-arg", "call-provider-not-found",
	} {
		assert.False(t, hasRuleName(result.Findings, rule), "unexpected rule %q", rule)
	}
}

func TestLintCalls_NilSolution(t *testing.T) {
	result := &Result{}
	lintCalls(nil, result, callLintRegistry())
	assert.Empty(t, result.Findings)
}
