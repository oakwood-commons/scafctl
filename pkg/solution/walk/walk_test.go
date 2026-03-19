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

func TestWalk_ForEach_OnAction(t *testing.T) {
	itemRef := &spec.ValueRef{Literal: []string{"a", "b"}}
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"deploy": {
						Name:     "deploy",
						Provider: "shell",
						ForEach: &spec.ForEachClause{
							In: itemRef,
						},
					},
				},
			},
		},
	}

	var fePaths []string
	var vrPaths []string
	err := Walk(sol, &Visitor{
		ForEach: func(p string, _ *spec.ForEachClause) error {
			fePaths = append(fePaths, p)
			return nil
		},
		ValueRef: func(p string, _ *spec.ValueRef) error {
			vrPaths = append(vrPaths, p)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Contains(t, fePaths, "spec.workflow.actions.deploy.forEach")
	assert.Contains(t, vrPaths, "spec.workflow.actions.deploy.forEach.in")
}

func TestWalk_MetadataCatalogSpec(t *testing.T) {
	sol := &solution.Solution{}
	var metaCalled, catalogCalled, specCalled bool
	err := Walk(sol, &Visitor{
		Metadata: func(_ string, _ *solution.Metadata) error {
			metaCalled = true
			return nil
		},
		Catalog: func(_ string, _ *solution.Catalog) error {
			catalogCalled = true
			return nil
		},
		Spec: func(_ string, _ *solution.Spec) error {
			specCalled = true
			return nil
		},
	})
	require.NoError(t, err)
	assert.True(t, metaCalled)
	assert.True(t, catalogCalled)
	assert.True(t, specCalled)
}

func TestWalk_WorkflowCallback(t *testing.T) {
	wf := &action.Workflow{}
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: wf,
		},
	}
	var called bool
	err := Walk(sol, &Visitor{
		Workflow: func(_ string, w *action.Workflow) error {
			called = true
			assert.Equal(t, wf, w)
			return nil
		},
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestWalk_ResolverWithConditions(t *testing.T) {
	expr := celexp.Expression("true")
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Name: "r1",
					Resolve: &resolver.ResolvePhase{
						When:  &resolver.Condition{Expr: &expr},
						Until: &resolver.Condition{Expr: &expr},
					},
					Transform: &resolver.TransformPhase{
						When: &resolver.Condition{Expr: &expr},
					},
				},
			},
		},
	}
	var conditions []string
	err := Walk(sol, &Visitor{
		Condition: func(path, _ string, _ *celexp.Expression) error {
			conditions = append(conditions, path)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Contains(t, conditions, "spec.resolvers.r1.resolve.when")
	assert.Contains(t, conditions, "spec.resolvers.r1.resolve.until")
	assert.Contains(t, conditions, "spec.resolvers.r1.transform.when")
}

func TestWalk_ResolverPhaseCallbacks(t *testing.T) {
	expr := celexp.Expression("true")
	itemRef := &spec.ValueRef{Literal: "item"}
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Name: "r1",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "parameter",
								When:     &resolver.Condition{Expr: &expr},
								Inputs:   map[string]*spec.ValueRef{"key": itemRef},
							},
						},
					},
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{
							{
								Provider: "cel",
								When:     &resolver.Condition{Expr: &expr},
								ForEach: &resolver.ForEachClause{
									In: &spec.ValueRef{Literal: []string{"a", "b"}},
								},
								Inputs: map[string]*spec.ValueRef{"val": itemRef},
							},
						},
					},
				},
			},
		},
	}

	var rPhasePaths, tPhasePaths, srcPaths, transformPaths, vrPaths, conditionPaths []string
	err := Walk(sol, &Visitor{
		ResolvePhase: func(path, _ string, _ *resolver.ResolvePhase) error {
			rPhasePaths = append(rPhasePaths, path)
			return nil
		},
		TransformPhase: func(path, _ string, _ *resolver.TransformPhase) error {
			tPhasePaths = append(tPhasePaths, path)
			return nil
		},
		ProviderSource: func(path string, _ *resolver.ProviderSource) error {
			srcPaths = append(srcPaths, path)
			return nil
		},
		ProviderTransform: func(path string, _ *resolver.ProviderTransform) error {
			transformPaths = append(transformPaths, path)
			return nil
		},
		ValueRef: func(path string, _ *spec.ValueRef) error {
			vrPaths = append(vrPaths, path)
			return nil
		},
		Condition: func(path, _ string, _ *celexp.Expression) error {
			conditionPaths = append(conditionPaths, path)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Contains(t, rPhasePaths, "spec.resolvers.r1.resolve")
	assert.Contains(t, tPhasePaths, "spec.resolvers.r1.transform")
	assert.Contains(t, srcPaths, "spec.resolvers.r1.resolve.with[0]")
	assert.Contains(t, transformPaths, "spec.resolvers.r1.transform.with[0]")
	assert.Contains(t, vrPaths, "spec.resolvers.r1.resolve.with[0].inputs.key")
	assert.Contains(t, conditionPaths, "spec.resolvers.r1.resolve.with[0].when")
	assert.Contains(t, conditionPaths, "spec.resolvers.r1.transform.with[0].when")
}

func TestWalk_WorkflowWithFinally(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"a1": {Name: "a1", Provider: "shell"},
				},
				Finally: map[string]*action.Action{
					"cleanup": {Name: "cleanup", Provider: "shell"},
				},
			},
		},
	}

	var actionNames []string
	err := Walk(sol, &Visitor{
		Action: func(_, name, _ string, _ *action.Action) error {
			actionNames = append(actionNames, name)
			return nil
		},
	})
	require.NoError(t, err)
	assert.Contains(t, actionNames, "a1")
	assert.Contains(t, actionNames, "cleanup")
}
