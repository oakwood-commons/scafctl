// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
)

// newCall builds a minimal call definition for tests.
func newCall(provider string, args map[string]*spec.ArgDef) *spec.Call {
	return &spec.Call{Provider: provider, Args: args}
}

// exprValueRef builds a ValueRef carrying a CEL expression for tests.
func exprValueRef(s string) *spec.ValueRef {
	e := celexp.Expression(s)
	return &spec.ValueRef{Expr: &e}
}

func TestValidateCalls_Definitions(t *testing.T) {
	tests := []struct {
		name    string
		calls   map[string]*spec.Call
		wantSub string // substring expected in a problem, or "" for no problems
	}{
		{
			name:    "valid definition",
			calls:   map[string]*spec.Call{"get": newCall("http", nil)},
			wantSub: "",
		},
		{
			name:    "nil definition",
			calls:   map[string]*spec.Call{"get": nil},
			wantSub: "null value",
		},
		{
			name:    "missing provider",
			calls:   map[string]*spec.Call{"get": newCall("", nil)},
			wantSub: "must declare a provider",
		},
		{
			name: "required with default",
			calls: map[string]*spec.Call{
				"get": newCall("http", map[string]*spec.ArgDef{
					"id": {Required: true, Default: "x"},
				}),
			},
			wantSub: "cannot be required and also declare a default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Solution{Spec: Spec{Calls: tt.calls}}
			problems := s.validateCalls()
			if tt.wantSub == "" {
				assert.Empty(t, problems)
				return
			}
			assert.Contains(t, joinProblems(problems), tt.wantSub)
		})
	}
}

// TestValidateCalls_DefinitionIsolation verifies that a call definition whose
// input references a solution resolver by name is rejected, while a definition
// that references only its args passes.
func TestValidateCalls_DefinitionIsolation(t *testing.T) {
	resolvers := map[string]*resolver.Resolver{
		"baseURL": {
			Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}},
		},
	}

	t.Run("resolver reference is an error", func(t *testing.T) {
		s := &Solution{Spec: Spec{
			Resolvers: resolvers,
			Calls: map[string]*spec.Call{
				"get": {
					Provider: "http",
					Inputs:   map[string]*spec.ValueRef{"uri": exprValueRef("_.baseURL + \"/x\"")},
				},
			},
		}}
		problems := s.validateCalls()
		assert.Contains(t, joinProblems(problems), "references solution resolver \"baseURL\"")
	})

	t.Run("args-only reference is allowed", func(t *testing.T) {
		s := &Solution{Spec: Spec{
			Resolvers: resolvers,
			Calls: map[string]*spec.Call{
				"get": {
					Provider: "http",
					Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
					Inputs:   map[string]*spec.ValueRef{"uri": exprValueRef("_.args.id")},
				},
			},
		}}
		problems := s.validateCalls()
		assert.Empty(t, problems)
	})

	// A solution may legitimately declare a resolver named "args"; the reserved
	// args namespace extracted from _.args.x must not be misread as a reference
	// to that resolver.
	t.Run("resolver named args does not flag the args namespace", func(t *testing.T) {
		s := &Solution{Spec: Spec{
			Resolvers: map[string]*resolver.Resolver{
				"args": {
					Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}},
				},
			},
			Calls: map[string]*spec.Call{
				"get": {
					Provider: "http",
					Args:     map[string]*spec.ArgDef{"id": {Type: spec.TypeString}},
					Inputs:   map[string]*spec.ValueRef{"uri": exprValueRef("_.args.id")},
				},
			},
		}}
		problems := s.validateCalls()
		assert.Empty(t, problems)
	})
}

func TestValidateCalls_ResolverCallSites(t *testing.T) {
	callDef := newCall("http", map[string]*spec.ArgDef{
		"id":   {Type: spec.TypeString, Required: true},
		"page": {Type: spec.TypeInt},
	})

	tests := []struct {
		name    string
		source  resolver.ProviderSource
		wantSub string
	}{
		{
			name: "valid call site",
			source: resolver.ProviderSource{
				CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}},
			},
			wantSub: "",
		},
		{
			name: "exclusivity call and provider",
			source: resolver.ProviderSource{
				Provider: "http",
				CallRef:  spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}},
			},
			wantSub: "mutually exclusive",
		},
		{
			name: "undefined call",
			source: resolver.ProviderSource{
				CallRef: spec.CallRef{Call: "missing", Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}},
			},
			wantSub: "undefined call",
		},
		{
			name: "unknown argument",
			source: resolver.ProviderSource{
				CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{
					"id":  {Literal: "1"},
					"bad": {Literal: "x"},
				}},
			},
			wantSub: "unknown argument",
		},
		{
			name: "missing required argument",
			source: resolver.ProviderSource{
				CallRef: spec.CallRef{Call: "get", Args: map[string]*spec.ValueRef{"page": {Literal: "1"}}},
			},
			wantSub: "missing required argument",
		},
		{
			name: "args without call",
			source: resolver.ProviderSource{
				Provider: "http",
				CallRef:  spec.CallRef{Args: map[string]*spec.ValueRef{"id": {Literal: "1"}}},
			},
			wantSub: "args are only valid alongside call",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Solution{Spec: Spec{
				Calls: map[string]*spec.Call{"get": callDef},
				Resolvers: map[string]*resolver.Resolver{
					"r": {
						Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{tt.source}},
					},
				},
			}}
			problems := s.validateCalls()
			if tt.wantSub == "" {
				assert.Empty(t, problems)
				return
			}
			assert.Contains(t, joinProblems(problems), tt.wantSub)
		})
	}
}

func TestValidateCalls_TransformAndValidateSites(t *testing.T) {
	callDef := newCall("http", map[string]*spec.ArgDef{"id": {Required: true}})

	s := &Solution{Spec: Spec{
		Calls: map[string]*spec.Call{"get": callDef},
		Resolvers: map[string]*resolver.Resolver{
			"r": {
				Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}},
				Transform: &resolver.TransformPhase{With: []resolver.ProviderTransform{{
					CallRef: spec.CallRef{Call: "missing"},
				}}},
				Validate: &resolver.ValidatePhase{With: []resolver.ProviderValidation{{
					CallRef: spec.CallRef{Call: "get"}, // missing required id
				}}},
			},
		},
	}}

	problems := s.validateCalls()
	joined := joinProblems(problems)
	assert.Contains(t, joined, "transform step 0 references undefined call")
	assert.Contains(t, joined, "validate step 0 is missing required argument")
}

func TestValidateCalls_ActionSites(t *testing.T) {
	callDef := newCall("shell", map[string]*spec.ArgDef{"cmd": {Required: true}})

	s := &Solution{Spec: Spec{
		Calls: map[string]*spec.Call{"run": callDef},
		Workflow: &action.Workflow{
			Actions: map[string]*action.Action{
				"deploy": {
					Name:     "deploy",
					Provider: "shell",
					CallRef:  spec.CallRef{Call: "run", Args: map[string]*spec.ValueRef{"cmd": {Literal: "echo"}}},
				},
			},
			Finally: map[string]*action.Action{
				"cleanup": {
					Name:    "cleanup",
					CallRef: spec.CallRef{Call: "run"}, // missing required cmd
				},
			},
		},
	}}

	problems := s.validateCalls()
	joined := joinProblems(problems)
	assert.Contains(t, joined, `action "deploy" sets both call "run" and provider "shell"`)
	assert.Contains(t, joined, `finally action "cleanup" is missing required argument "cmd"`)
}

func TestValidateCalls_NilSolutionAndNoCalls(t *testing.T) {
	var s *Solution
	assert.Nil(t, s.validateCalls())

	empty := &Solution{Spec: Spec{}}
	assert.Empty(t, empty.validateCalls())
}

func joinProblems(problems []string) string {
	out := ""
	for _, p := range problems {
		out += p + "\n"
	}
	return out
}
