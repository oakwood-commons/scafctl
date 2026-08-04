// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const renameFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: rename-test # keep this comment
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      dependsOn:
        - environment
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.environment
    greeting:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                tmpl: "{{ ._.appName }}-{{ ._.environment }}"
`

func loadSolution(t *testing.T, y string) *solution.Solution {
	t.Helper()
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(y)))
	return sol
}

func TestRenameResolver_HappyPath(t *testing.T) {
	sol := loadSolution(t, renameFixture)

	res, err := RenameResolver(sol, "environment", "env")
	require.NoError(t, err)
	require.Equal(t, "environment", res.OldName)
	require.Equal(t, "env", res.NewName)
	// definition + dependsOn + CEL + template = 4 occurrences.
	require.Len(t, res.Edits, 4)

	raw := sol.RawContent()
	out, err := res.Apply(raw)
	require.NoError(t, err)

	// Exactly the four occurrences shrank from "environment" (11) to "env" (3).
	assert.Equal(t, len(raw)-4*(len("environment")-len("env")), len(out))
	assert.NotContains(t, string(out), "environment")

	// Formatting/comments are preserved verbatim.
	assert.Contains(t, string(out), "# keep this comment")
	assert.Contains(t, string(out), "  name: rename-test")

	// The rewritten content re-parses and the rename is complete and consistent.
	newSol := loadSolution(t, string(out))
	idx, err := refindex.Build(newSol)
	require.NoError(t, err)
	assert.Zero(t, idx.Unresolved())
	def, ok := idx.Definition("env")
	require.True(t, ok)
	assert.True(t, def.IsDef)
	assert.Len(t, idx.Occurrences("env"), 4)
	_, stillThere := idx.Definition("environment")
	assert.False(t, stillThere)
}

func TestRenameResolver_Guards(t *testing.T) {
	sol := loadSolution(t, renameFixture)

	tests := []struct {
		name    string
		old     string
		newName string
		wantMsg string
	}{
		{"invalid new name", "environment", "1bad", "not a valid resolver name"},
		{"equal names", "environment", "environment", "equals old name"},
		{"undefined old", "missing", "whatever", "is not defined"},
		{"collision", "environment", "appName", "already exists"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := RenameResolver(sol, tt.old, tt.newName)
			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestRenameResolver_NilSolution(t *testing.T) {
	res, err := RenameResolver(nil, "a", "b")
	require.Error(t, err)
	assert.Nil(t, res)
}

func TestRenameResolver_RefusesWhenUnresolved(t *testing.T) {
	// A bare-field template reference to the SAME name being renamed cannot be
	// positioned (context-dependent), so the rename must refuse rather than risk
	// missing it. (Name-scoped: a bare field of an unrelated name would NOT block.)
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: unresolved
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    user:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                tmpl: "{{ .environment }}"
`
	sol := loadSolution(t, y)
	res, err := RenameResolver(sol, "environment", "env")
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "could not be located")
}

func TestRenameResolver_RefusesOnNestedLiteralRef(t *testing.T) {
	// A nested inline {rslvr: environment} inside a literal is a real reference
	// that is not positioned; the rename must refuse.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nested
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    user:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                settings:
                  env:
                    rslvr: environment
`
	sol := loadSolution(t, y)
	res, err := RenameResolver(sol, "environment", "env")
	require.Error(t, err)
	assert.Nil(t, res)
	assert.Contains(t, err.Error(), "could not be located")
}

func TestRenameResolver_NameScopedFailSafe(t *testing.T) {
	// An unlocatable reference to a DIFFERENT resolver must not block this rename.
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: scoped
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    region:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: us
    user:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                tmpl: "{{ .region }}"
`
	sol := loadSolution(t, y)
	// Renaming "environment" is fine even though "region" has an unpositioned
	// bare-field reference.
	res, err := RenameResolver(sol, "environment", "env")
	require.NoError(t, err)
	require.NotNil(t, res)
	// Renaming "region" (the one with the unlocatable ref) must refuse.
	_, err = RenameResolver(sol, "region", "zone")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be located")
}

func TestApply_EmptyEditsReturnsCopy(t *testing.T) {
	raw := []byte("unchanged content")
	out, err := Apply(raw, nil)
	require.NoError(t, err)
	assert.Equal(t, raw, out)
	// It must be a distinct slice (not aliasing the input).
	if len(out) > 0 {
		out[0] = 'X'
		assert.Equal(t, byte('u'), raw[0])
	}
}

func TestApply_MultipleEditsPreserveSurroundings(t *testing.T) {
	raw := []byte("aaa bbb ccc")
	li := sourcepos.NewLineIndex(raw, "")
	edits := []TextEdit{
		{Range: li.Range(0, 3), NewText: "XX"},   // "aaa" -> "XX"
		{Range: li.Range(8, 11), NewText: "YYY"}, // "ccc" -> "YYY"
	}
	out, err := Apply(raw, edits)
	require.NoError(t, err)
	assert.Equal(t, "XX bbb YYY", string(out))
}

func TestApply_RejectsOverlappingEdits(t *testing.T) {
	raw := []byte("hello world")
	li := sourcepos.NewLineIndex(raw, "")
	edits := []TextEdit{
		{Range: li.Range(0, 5), NewText: "X"},
		{Range: li.Range(3, 8), NewText: "Y"}, // overlaps the first
	}
	_, err := Apply(raw, edits)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "overlapping")
}

func TestApply_RejectsInvertedRange(t *testing.T) {
	raw := []byte("hello world")
	li := sourcepos.NewLineIndex(raw, "")
	// Start resolves after End (byte 8 > byte 2), so start > end triggers the
	// bounds guard.
	edits := []TextEdit{{Range: sourcepos.Range{
		Start: li.Position(8),
		End:   li.Position(2),
	}, NewText: "x"}}
	_, err := Apply(raw, edits)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of bounds")
}

func TestApply_InsertionAtOffset(t *testing.T) {
	raw := []byte("abcdef")
	li := sourcepos.NewLineIndex(raw, "")
	// Zero-width range == insertion.
	edits := []TextEdit{{Range: li.Range(3, 3), NewText: "-"}}
	out, err := Apply(raw, edits)
	require.NoError(t, err)
	assert.Equal(t, "abc-def", string(out))
}

func BenchmarkRenameResolver(b *testing.B) {
	sol := &solution.Solution{}
	if err := sol.UnmarshalFromBytes([]byte(renameFixture)); err != nil {
		b.Fatal(err)
	}
	raw := sol.RawContent()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		res, err := RenameResolver(sol, "environment", "env")
		if err != nil {
			b.Fatal(err)
		}
		_, _ = Apply(raw, res.Edits)
	}
}
