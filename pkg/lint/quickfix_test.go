// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadQuickFixSolution parses YAML into a Solution (retaining raw content and
// source map, which QuickFixFor relies on).
func loadQuickFixSolution(t *testing.T, y string) *solution.Solution {
	t.Helper()
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(y)))
	return sol
}

// findingByRule runs lint and returns the first finding for the given rule.
func findingByRule(t *testing.T, sol *solution.Solution, rule string) *Finding {
	t.Helper()
	result := Solution(sol, "solution.yaml", nil)
	for _, f := range result.Findings {
		if f.RuleName == rule {
			return f
		}
	}
	t.Fatalf("no finding with rule %q found (findings: %v)", rule, ruleNames(result.Findings))
	return nil
}

func ruleNames(fs []*Finding) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.RuleName)
	}
	return out
}

// applyAndValidate applies edits to the solution's raw content and asserts the
// result still parses and passes spec validation -- the core safety guarantee of
// a quick fix (it must never produce a broken solution).
func applyAndValidate(t *testing.T, sol *solution.Solution, edits []refactor.TextEdit) string {
	t.Helper()
	out, err := refactor.Apply(sol.RawContent(), edits)
	require.NoError(t, err, "edits must apply")
	fixed := &solution.Solution{}
	require.NoError(t, fixed.UnmarshalFromBytes(out), "fixed output must re-parse")
	require.NoError(t, fixed.ValidateSpec(), "fixed output must pass spec validation")
	return string(out)
}

func TestQuickFixFor_UnsupportedRule(t *testing.T) {
	sol := loadQuickFixSolution(t, unusedResolverFixture)
	edits, ok := QuickFixFor(sol, &Finding{RuleName: "some-other-rule", Location: "resolvers.x"})
	assert.False(t, ok)
	assert.Nil(t, edits)
}

func TestQuickFixFor_NilInputs(t *testing.T) {
	edits, ok := QuickFixFor(nil, &Finding{RuleName: "unused-resolver"})
	assert.False(t, ok)
	assert.Nil(t, edits)

	sol := loadQuickFixSolution(t, unusedResolverFixture)
	edits, ok = QuickFixFor(sol, nil)
	assert.False(t, ok)
	assert.Nil(t, edits)
}

// ── deprecated-field ─────────────────────────────────────────────────────────

const deprecatedResolveFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dep
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: parameter
            onError: continue
            inputs:
              value: dev
    b:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.a
`

func TestQuickFixFor_DeprecatedField_ResolveContinue(t *testing.T) {
	sol := loadQuickFixSolution(t, deprecatedResolveFixture)
	f := findingByRule(t, sol, "deprecated-field")
	require.Equal(t, "resolvers.a.resolve.with[0].onError", f.Location)

	edits, ok := QuickFixFor(sol, f)
	require.True(t, ok)
	require.Len(t, edits, 1)

	out := applyAndValidate(t, sol, edits)
	assert.Contains(t, out, "continueOnError: true")
	assert.NotContains(t, out, "onError: continue")
}

const deprecatedActionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dep-action
spec:
  workflow:
    actions:
      deploy:
        provider: message
        onError: fail
        inputs:
          message: go
`

func TestQuickFixFor_DeprecatedField_ActionFail(t *testing.T) {
	sol := loadQuickFixSolution(t, deprecatedActionFixture)
	f := findingByRule(t, sol, "deprecated-field")
	require.Equal(t, "workflow.actions.deploy.onError", f.Location)

	edits, ok := QuickFixFor(sol, f)
	require.True(t, ok)
	out := applyAndValidate(t, sol, edits)
	assert.Contains(t, out, "continueOnError: false")
	assert.NotContains(t, out, "onError: fail")
}

const deprecatedForEachFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: dep-foreach
spec:
  workflow:
    actions:
      deploy:
        provider: message
        forEach:
          in:
            expr: "['a','b']"
          onError: continue
        inputs:
          message: "{{ .__item }}"
`

func TestQuickFixFor_DeprecatedField_ForEachContinue(t *testing.T) {
	sol := loadQuickFixSolution(t, deprecatedForEachFixture)
	f := findingByRule(t, sol, "deprecated-field")
	require.Equal(t, "workflow.actions.deploy.forEach.onError", f.Location)

	edits, ok := QuickFixFor(sol, f)
	require.True(t, ok)
	out := applyAndValidate(t, sol, edits)
	assert.Contains(t, out, "continueOnError: true")
}

// ── redundant-depends-on ─────────────────────────────────────────────────────

const redundantAllFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: redundant-all
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    app:
      dependsOn:
        - env
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.env
`

func TestQuickFixFor_RedundantDependsOn_All(t *testing.T) {
	sol := loadQuickFixSolution(t, redundantAllFixture)
	f := findingByRule(t, sol, "redundant-depends-on")
	require.Equal(t, "resolvers.app.dependsOn", f.Location)

	edits, ok := QuickFixFor(sol, f)
	require.True(t, ok)
	out := applyAndValidate(t, sol, edits)
	assert.NotContains(t, out, "dependsOn:", "the whole dependsOn entry is removed")
	assert.Contains(t, out, "expr: _.env")
}

const redundantPartialFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: redundant-partial
spec:
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    other:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: x
    app:
      dependsOn:
        - env
        - other
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.env
`

func TestQuickFixFor_RedundantDependsOn_Partial(t *testing.T) {
	sol := loadQuickFixSolution(t, redundantPartialFixture)
	f := findingByRule(t, sol, "redundant-depends-on")

	edits, ok := QuickFixFor(sol, f)
	require.True(t, ok)
	// Only 'env' is inferred (via expr:), 'other' is a genuine ordering dep.
	require.Len(t, edits, 1)
	out := applyAndValidate(t, sol, edits)
	assert.Contains(t, out, "dependsOn:", "the list survives")
	assert.NotContains(t, out, "- env")
	assert.Contains(t, out, "- other", "the non-redundant entry survives")
}

// ── unused-resolver ──────────────────────────────────────────────────────────

const unusedResolverFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: unused
spec:
  resolvers:
    used:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    orphan:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: x
  workflow:
    actions:
      show:
        provider: message
        inputs:
          message:
            expr: _.used
`

func TestQuickFixFor_UnusedResolver(t *testing.T) {
	sol := loadQuickFixSolution(t, unusedResolverFixture)
	f := findingByRule(t, sol, "unused-resolver")
	require.Equal(t, "resolvers.orphan", f.Location)

	edits, ok := QuickFixFor(sol, f)
	require.True(t, ok)
	out := applyAndValidate(t, sol, edits)
	assert.NotContains(t, out, "orphan:", "the orphan resolver is removed")
	assert.Contains(t, out, "used:", "the used resolver survives")
}

// soleUnusedResolverFixture has exactly one resolver, and it is unused: removing
// only its entry would leave `resolvers:` with a null value.
const soleUnusedResolverFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: sole
spec:
  resolvers:
    orphan:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: x
  workflow:
    actions:
      show:
        provider: message
        inputs:
          message: hi
`

func TestQuickFixFor_UnusedResolver_SoleResolverRemovesParent(t *testing.T) {
	sol := loadQuickFixSolution(t, soleUnusedResolverFixture)
	f := findingByRule(t, sol, "unused-resolver")
	require.Equal(t, "resolvers.orphan", f.Location)

	edits, ok := QuickFixFor(sol, f)
	require.True(t, ok)
	out := applyAndValidate(t, sol, edits)

	// The whole resolvers: block is removed, not left as a null-valued key.
	assert.NotContains(t, out, "resolvers:", "empty resolvers: key must not remain")
	assert.NotContains(t, out, "orphan:", "the orphan resolver is removed")
	assert.Contains(t, out, "workflow:", "the rest of the spec survives")

	// Re-linting the fixed document must NOT introduce a new schema error from a
	// null resolvers: value (the failure mode this fix guards against).
	fixed := &solution.Solution{}
	require.NoError(t, fixed.UnmarshalFromBytes([]byte(out)))
	for _, nf := range Solution(fixed, "solution.yaml", nil).Findings {
		assert.NotContains(t, nf.Message, "null",
			"fix must not introduce a null-value finding (rule %q)", nf.RuleName)
	}
}
