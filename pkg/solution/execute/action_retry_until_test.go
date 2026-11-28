// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execute_test

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/execute"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLintAndWorkflowRefs_ActionRetryUntil(t *testing.T) {
	t.Parallel()

	reg, err := builtin.DefaultRegistry(t.Context())
	require.NoError(t, err)

	t.Run("valid retry.until marks referenced resolver as used and passes lint", func(t *testing.T) {
		t.Parallel()
		untilExpr := celexp.Expression(`__result.statusCode == 200 && __self.body.status == _.expectedStatus`)
		sol := &solution.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solution.Metadata{
				Name:        "poll-demo",
				Version:     semver.MustParse("1.0.0"),
				Description: "Solution exercising action retry.until lint and dependency extraction",
			},
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"expectedStatus": {
						Name:        "expectedStatus",
						Description: "Target status value",
						Resolve: &resolver.ResolvePhase{
							With: []resolver.ProviderSource{{
								Provider: "static",
								Inputs:   map[string]*spec.ValueRef{"value": {Literal: "completed"}},
							}},
						},
					},
				},
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"pollChecks": {
							Name:        "pollChecks",
							Description: "Poll endpoint until status matches",
							Provider:    "http",
							Inputs: map[string]*spec.ValueRef{
								"url": {Literal: "https://api.example.com/checks"},
							},
							Retry: &action.RetryConfig{
								MaxAttempts: 5,
								Until:       &untilExpr,
							},
						},
					},
				},
			},
		}

		refs := execute.WorkflowResolverRefs(sol, sol.Spec.Workflow, nil, reg.DescriptorLookup())
		assert.Equal(t, []string{"expectedStatus"}, refs)

		lintRes := lint.Solution(sol, "", reg)
		assert.Equal(t, 0, lintRes.ErrorCount, "unexpected lint errors: %+v", lintRes.Findings)
		for _, f := range lintRes.Findings {
			assert.NotEqual(t, "unused-resolver", f.RuleName, "resolver referenced in retry.until must not be flagged unused")
		}
	})

	t.Run("invalid retry.until CEL syntax and undefined resolver ref are flagged by lint", func(t *testing.T) {
		t.Parallel()
		badSyntax := celexp.Expression(`__result.status == &&&`)
		badSol := &solution.Solution{
			APIVersion: "scafctl.io/v1",
			Kind:       "Solution",
			Metadata: solution.Metadata{
				Name:        "bad-until",
				Version:     semver.MustParse("1.0.0"),
				Description: "Invalid until expression",
			},
			Spec: solution.Spec{
				Workflow: &action.Workflow{
					Actions: map[string]*action.Action{
						"pollChecks": {
							Name:        "pollChecks",
							Description: "Poll endpoint",
							Provider:    "http",
							Inputs: map[string]*spec.ValueRef{
								"url": {Literal: "https://api.example.com/checks"},
							},
							Retry: &action.RetryConfig{
								MaxAttempts: 3,
								Until:       &badSyntax,
							},
						},
					},
				},
			},
		}

		syntaxRes := lint.Solution(badSol, "", reg)
		assert.Greater(t, syntaxRes.ErrorCount, 0, "expected lint error for invalid retry.until CEL syntax")

		unknownRef := celexp.Expression(`__result.status == _.typoResolver`)
		badSol.Spec.Workflow.Actions["pollChecks"].Retry.Until = &unknownRef
		refRes := lint.Solution(badSol, "", reg)
		rules := make([]string, 0, len(refRes.Findings))
		for _, f := range refRes.Findings {
			rules = append(rules, f.RuleName)
		}
		assert.Contains(t, rules, "unknown-resolver-reference")
	})
}
