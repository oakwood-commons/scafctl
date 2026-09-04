// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

func resolverBoolCondition(v string) *resolver.Condition {
	expr := celexp.Expression(v)
	return &resolver.Condition{Expr: &expr}
}

func TestLintDeprecatedFields_ResolverSourceOnError(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider: "http",
							OnError:  resolver.ErrorBehaviorContinue,
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", provider.NewRegistry())

	findings := filterFindingsByRule(result, "deprecated-field")
	require.Len(t, findings, 1)
	assert.Equal(t, SeverityWarning, findings[0].Severity)
	assert.Contains(t, findings[0].Message, "onError")
	assert.Contains(t, findings[0].Message, "continueOnError")
	assert.Equal(t, "resolvers.r.resolve.with[0].onError", findings[0].Location)
}

func TestLintDeprecatedFields_ResolverTransformOnError(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r": {
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{{
							Provider: "cel",
							OnError:  resolver.ErrorBehaviorFail,
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", provider.NewRegistry())

	findings := filterFindingsByRule(result, "deprecated-field")
	require.Len(t, findings, 1)
	assert.Equal(t, "resolvers.r.transform.with[0].onError", findings[0].Location)
}

func TestLintDeprecatedFields_Conflict(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider:        "http",
							OnError:         resolver.ErrorBehaviorContinue,
							ContinueOnError: resolverBoolCondition("true"),
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", provider.NewRegistry())

	conflicts := filterFindingsByRule(result, "deprecated-field-conflict")
	require.Len(t, conflicts, 1)
	assert.Equal(t, SeverityError, conflicts[0].Severity)
	assert.Contains(t, conflicts[0].Message, "takes precedence")

	// When the replacement is also set, only the conflict error is emitted,
	// not the plain deprecation warning.
	assert.Empty(t, filterFindingsByRule(result, "deprecated-field"))
}

func TestLintDeprecatedFields_ActionAndForEach(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"deploy": {
						Provider: "exec",
						OnError:  spec.OnErrorContinue,
						ForEach: &spec.ForEachClause{
							OnError: spec.OnErrorFail, //nolint:staticcheck // intentionally exercises the deprecated-field lint rule
						},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", provider.NewRegistry())

	findings := filterFindingsByRule(result, "deprecated-field")
	require.Len(t, findings, 2)

	locations := map[string]bool{}
	for _, f := range findings {
		locations[f.Location] = true
	}
	assert.True(t, locations["workflow.actions.deploy.onError"])
	assert.True(t, locations["workflow.actions.deploy.forEach.onError"])
}

func TestLintDeprecatedFields_NoDeprecatedUsage(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{
							Provider:        "http",
							ContinueOnError: resolverBoolCondition("true"),
						}},
					},
				},
			},
		},
	}

	result := Solution(sol, "test.yaml", provider.NewRegistry())

	assert.Empty(t, filterFindingsByRule(result, "deprecated-field"))
	assert.Empty(t, filterFindingsByRule(result, "deprecated-field-conflict"))
}
