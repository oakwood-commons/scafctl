// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWalk_NilInputs(t *testing.T) {
	assert.NoError(t, Walk(nil, &Visitor{}))
	assert.NoError(t, Walk(&solution.Solution{}, nil))
}

func TestWalk_SolutionCallback(t *testing.T) {
	var called bool
	err := Walk(&solution.Solution{}, &Visitor{
		Solution: func(_ *solution.Solution) error { called = true; return nil },
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestWalk_Resolvers(t *testing.T) {
	expr := celexp.Expression("true")
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"alpha": {
					Name: "alpha",
					When: &resolver.Condition{Expr: &expr},
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "parameter",
							Inputs:   map[string]*spec.ValueRef{"value": {Literal: "hello"}},
						}},
					},
				},
				"beta": {
					Name: "beta",
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{{Provider: "cel"}},
					},
				},
				"nil_resolver": nil, // should be skipped
			},
		},
	}

	var names, provPaths, condPaths, vrPaths []string
	err := Walk(sol, &Visitor{
		Resolver: func(_, name string, _ *resolver.Resolver) error {
			names = append(names, name)
			return nil
		},
		ProviderSource: func(p string, _ *resolver.ProviderSource) error {
			provPaths = append(provPaths, p)
			return nil
		},
		ProviderTransform: func(p string, _ *resolver.ProviderTransform) error {
			provPaths = append(provPaths, p)
			return nil
		},
		Condition: func(p, _ string, _ *celexp.Expression) error {
			condPaths = append(condPaths, p)
			return nil
		},
		ValueRef: func(p string, _ *spec.ValueRef) error {
			vrPaths = append(vrPaths, p)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta"}, names, "sorted order")
	assert.Contains(t, provPaths, "spec.resolvers.alpha.resolve.with[0]")
	assert.Contains(t, provPaths, "spec.resolvers.beta.transform.with[0]")
	assert.Contains(t, condPaths, "spec.resolvers.alpha.when")
	assert.Contains(t, vrPaths, "spec.resolvers.alpha.resolve.with[0].inputs.value")
}

func TestWalk_Workflow(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"deploy":  {Provider: "exec"},
					"nil_act": nil, // should be skipped
				},
				Finally: map[string]*action.Action{
					"cleanup": {Provider: "exec"},
				},
			},
		},
	}
	var names, sects []string
	err := Walk(sol, &Visitor{
		Action: func(_, name, section string, _ *action.Action) error {
			names = append(names, name)
			sects = append(sects, section)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Contains(t, names, "deploy")
	assert.Contains(t, names, "cleanup")
}

func TestWalk_Tests(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Testing: &soltesting.TestSuite{
				Cases: map[string]*soltesting.TestCase{
					"b-test": {Name: "b-test"},
					"a-test": {Name: "a-test"},
				},
			},
		},
	}
	var names []string
	err := Walk(sol, &Visitor{
		TestCase: func(_, name string, _ *soltesting.TestCase) error {
			names = append(names, name)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"a-test", "b-test"}, names, "sorted order")
}

func TestWalk_ErrorAborts(t *testing.T) {
	sentinel := errors.New("stop")
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"a": {Name: "a"},
				"b": {Name: "b"},
				"c": {Name: "c"},
			},
		},
	}
	var n int
	err := Walk(sol, &Visitor{
		Resolver: func(_, _ string, _ *resolver.Resolver) error {
			n++
			if n == 2 {
				return sentinel
			}
			return nil
		},
	})
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 2, n)
}

func TestWalk_ValidatePhase(t *testing.T) {
	msg := &spec.ValueRef{Literal: "fail"}
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"v1": {
					Name: "v1",
					Validate: &resolver.ValidatePhase{
						With: []resolver.ProviderValidation{{
							Provider: "validation",
							Message:  msg,
						}},
					},
				},
			},
		},
	}
	var pvP, vrP []string
	err := Walk(sol, &Visitor{
		ProviderValidation: func(p string, _ *resolver.ProviderValidation) error {
			pvP = append(pvP, p)
			return nil
		},
		ValueRef: func(p string, _ *spec.ValueRef) error {
			vrP = append(vrP, p)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"spec.resolvers.v1.validate.with[0]"}, pvP)
	assert.Equal(t, []string{"spec.resolvers.v1.validate.with[0].message"}, vrP)
}
