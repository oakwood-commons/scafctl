// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// extractFixture has two resolvers whose resolve steps are structurally
// identical, a transform step, and an inline comment on the first block. It
// exercises extraction, identical-occurrence replacement, and comment
// preservation.
const extractFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: extract-test # keep this comment
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter # inline comment on the block
            inputs:
              value: dev
      transform:
        with:
          - provider: cel
            inputs:
              expression: __self.lowerAscii()
    region:
      resolve:
        with:
          - provider: parameter # inline comment on the block
            inputs:
              value: dev
`

// reparseAndValidate asserts the rewritten bytes parse as a solution and pass
// structural spec validation (which includes call definition/site validation).
func reparseAndValidate(t *testing.T, out []byte) *solution.Solution {
	t.Helper()
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes(out), "rewritten output must re-parse")
	require.NoError(t, sol.ValidateSpec(), "rewritten output must pass spec validation")
	return sol
}

func TestExtractCall_HappyPath(t *testing.T) {
	sol := loadSolution(t, extractFixture)

	res, err := ExtractCall(sol, "spec.resolvers.environment.resolve.with[0]", "getEnv")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, "spec.resolvers.environment.resolve.with[0]", res.OldName)
	assert.Equal(t, "getEnv", res.NewName)
	// One step rewrite + one calls insertion.
	require.Len(t, res.Edits, 2)

	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)

	// The selected step became a call reference.
	assert.Contains(t, s, "          - call: getEnv")
	// A calls block was created with the extracted provider+inputs.
	assert.Contains(t, s, "  calls:\n    getEnv:\n      provider: parameter")
	assert.Contains(t, s, "      inputs:\n        value: dev")
	// The OTHER identical step is left untouched by the base function.
	assert.Contains(t, s, "    region:\n      resolve:\n        with:\n          - provider: parameter")

	// Structure re-parses, validates, and the new call is wired up.
	newSol := reparseAndValidate(t, out)
	require.True(t, newSol.Spec.HasCalls())
	def := newSol.Spec.Calls["getEnv"]
	require.NotNil(t, def)
	assert.Equal(t, "parameter", def.Provider)
	assert.Empty(t, def.Args, "v1 extraction infers no args")

	idx, err := refindex.Build(newSol)
	require.NoError(t, err)
	assert.Zero(t, idx.Unresolved())
	_, ok := idx.Definition(refindex.SymbolCall, "getEnv")
	assert.True(t, ok)
}

func TestExtractCall_PreservesComments(t *testing.T) {
	sol := loadSolution(t, extractFixture)

	res, err := ExtractCall(sol, "spec.resolvers.environment.resolve.with[0]", "getEnv")
	require.NoError(t, err)
	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)

	// The inline comment on the extracted block moves into the call body.
	assert.Contains(t, s, "      provider: parameter # inline comment on the block")
	// Unrelated comments elsewhere survive verbatim.
	assert.Contains(t, s, "  name: extract-test # keep this comment")
	reparseAndValidate(t, out)
}

func TestExtractCall_TransformStep(t *testing.T) {
	sol := loadSolution(t, extractFixture)

	res, err := ExtractCall(sol, "spec.resolvers.environment.transform.with[0]", "toLower")
	require.NoError(t, err)
	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "          - call: toLower")
	assert.Contains(t, s, "    toLower:\n      provider: cel")
	assert.Contains(t, s, "      inputs:\n        expression: __self.lowerAscii()")

	newSol := reparseAndValidate(t, out)
	require.NotNil(t, newSol.Spec.Calls["toLower"])
}

func TestExtractCall_AppendsToExistingCalls(t *testing.T) {
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: existing-calls
spec:
  calls:
    existing:
      provider: static
      inputs:
        value: hi
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
`
	sol := loadSolution(t, y)
	res, err := ExtractCall(sol, "spec.resolvers.environment.resolve.with[0]", "getEnv")
	require.NoError(t, err)
	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)

	// The existing call is untouched; the new one is appended as a sibling.
	assert.Contains(t, s, "    existing:\n      provider: static")
	assert.Contains(t, s, "    getEnv:\n      provider: parameter")
	// There must be exactly one calls: key (no duplicate block created).
	assert.Equal(t, 1, strings.Count(s, "  calls:\n"))

	newSol := reparseAndValidate(t, out)
	require.NotNil(t, newSol.Spec.Calls["existing"])
	require.NotNil(t, newSol.Spec.Calls["getEnv"])
}

func TestExtractCall_MiddleStepInList(t *testing.T) {
	// The extracted step is not the last in its with: list; the following
	// sibling step must survive intact.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: middle
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
          - provider: static
            inputs:
              value: fallback
`
	sol := loadSolution(t, y)
	res, err := ExtractCall(sol, "spec.resolvers.environment.resolve.with[0]", "getEnv")
	require.NoError(t, err)
	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)

	assert.Contains(t, s, "          - call: getEnv\n          - provider: static")
	assert.Contains(t, s, "              value: fallback")
	reparseAndValidate(t, out)
}

func TestExtractCallReplacingIdentical_RewritesDuplicates(t *testing.T) {
	sol := loadSolution(t, extractFixture)

	res, err := ExtractCallReplacingIdentical(sol, "spec.resolvers.environment.resolve.with[0]", "getEnv")
	require.NoError(t, err)
	// Two identical resolve steps rewritten + one calls insertion.
	require.Len(t, res.Edits, 3)

	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)

	// Both structurally-identical steps became call references.
	assert.Equal(t, 2, strings.Count(s, "- call: getEnv"))
	// The differing transform step is NOT rewritten.
	assert.Contains(t, s, "          - provider: cel")

	newSol := reparseAndValidate(t, out)
	idx, err := refindex.Build(newSol)
	require.NoError(t, err)
	assert.Zero(t, idx.Unresolved())
	// Definition + two call references = three occurrences.
	assert.Len(t, idx.Occurrences(refindex.SymbolCall, "getEnv"), 3)
}

func TestExtractCallReplacingIdentical_NoNearMatch(t *testing.T) {
	// A near-match (same provider, different input value) must NOT be rewritten.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: near
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    b:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: prod
`
	sol := loadSolution(t, y)
	res, err := ExtractCallReplacingIdentical(sol, "spec.resolvers.a.resolve.with[0]", "getEnv")
	require.NoError(t, err)
	// Only the selected step + calls insertion (the prod step differs).
	require.Len(t, res.Edits, 2)

	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)
	assert.Equal(t, 1, strings.Count(s, "- call: getEnv"))
	assert.Contains(t, s, "              value: prod")
	reparseAndValidate(t, out)
}

func TestExtractCall_Errors(t *testing.T) {
	sol := loadSolution(t, extractFixtureWithCallStep())

	tests := []struct {
		name      string
		blockPath string
		callName  string
		wantMsg   string
	}{
		{"invalid call name", "spec.resolvers.environment.resolve.with[0]", "1bad", "not a valid call name"},
		{"unknown block path", "spec.resolvers.nope.resolve.with[0]", "getEnv", "does not resolve to a node"},
		{"not a step path", "spec.resolvers.environment", "getEnv", "is not a resolve/transform/validate step path"},
		{"already a call step", "spec.resolvers.viacall.resolve.with[0]", "getEnv", "already uses a call reference"},
		{"name collision", "spec.resolvers.environment.resolve.with[0]", "existing", "already exists"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := ExtractCall(sol, tt.blockPath, tt.callName)
			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestExtractCall_NilSolution(t *testing.T) {
	res, err := ExtractCall(nil, "spec.resolvers.a.resolve.with[0]", "getEnv")
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "nil solution")
}

// TestExtractCall_RejectsEmptyCallsBlock guards the corruption case: a spec.calls
// key that is present in source but decodes to no entries (inline "calls: {}" or
// a bare/comment-only "calls:") must be rejected, not silently rewritten into a
// second, duplicate "calls:" key (which would produce invalid YAML).
func TestExtractCall_RejectsEmptyCallsBlock(t *testing.T) {
	cases := map[string]string{
		"inline empty map": `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty-inline-calls
spec:
  calls: {}
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
`,
		"bare comment-only key": `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: empty-bare-calls
spec:
  calls:
    # no entries yet
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
`,
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			sol := loadSolution(t, y)
			res, err := ExtractCall(sol, "spec.resolvers.environment.resolve.with[0]", "getEnv")
			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), "spec.calls is present but has no entries")
		})
	}
}

// TestExtractCall_RejectsUnsupportedStepFields guards the silent-behavior-loss
// case: a step that carries a step-level field a call definition cannot model
// (when, continueOnError, onError, forEach, or a validation message) must be
// rejected, not hoisted verbatim -- otherwise the field is silently spliced into
// the call body (where spec.Call ignores it) AND stripped from the call site,
// changing conditional/iteration/error-handling behavior with no error.
func TestExtractCall_RejectsUnsupportedStepFields(t *testing.T) {
	cases := map[string]string{
		"when": `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: field-when
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
            when: "1 == 1"
`,
		"continueOnError": `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: field-coe
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
            continueOnError: true
`,
		"forEach": `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: field-foreach
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
            forEach:
              in: "[1, 2, 3]"
`,
		"validate message": `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: field-message
spec:
  resolvers:
    environment:
      validate:
        with:
          - provider: validation
            inputs:
              rule: __self != ""
            message: must not be empty
`,
	}
	for name, y := range cases {
		t.Run(name, func(t *testing.T) {
			sol := loadSolution(t, y)
			phase := "resolve"
			if name == "validate message" {
				phase = "validate"
			}
			res, err := ExtractCall(sol, "spec.resolvers.environment."+phase+".with[0]", "getEnv")
			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), "unsupported field")
		})
	}
}

// extractFixtureWithCallStep returns a solution containing an existing call, a
// provider step, and a step that already uses a call: reference (non-extractable).
func extractFixtureWithCallStep() string {
	return `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: errors
spec:
  calls:
    existing:
      provider: static
      inputs:
        value: hi
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    viacall:
      resolve:
        with:
          - call: existing
`
}
