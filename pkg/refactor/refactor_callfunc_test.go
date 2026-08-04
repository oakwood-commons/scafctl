// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const renameCallFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: rename-call-test # keep this comment
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: fetching
  resolvers:
    r1:
      resolve:
        with:
          - call: fetch
  workflow:
    actions:
      a1:
        call: fetch
`

func TestRenameCall_HappyPath(t *testing.T) {
	sol := loadSolution(t, renameCallFixture)

	res, err := RenameCall(sol, "fetch", "download")
	require.NoError(t, err)
	// definition + resolve.with call + action call = 3 occurrences.
	require.Len(t, res.Edits, 3)

	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "# keep this comment")
	assert.Contains(t, s, "download:")
	assert.Contains(t, s, "- call: download")
	// The old name survives only in unrelated literal text (message: fetching).
	assert.NotContains(t, s, "call: fetch")
	assert.NotContains(t, s, "fetch:")

	newSol := loadSolution(t, s)
	idx, err := refindex.Build(newSol)
	require.NoError(t, err)
	assert.Zero(t, idx.Unresolved())
	assert.Len(t, idx.Occurrences(refindex.SymbolCall, "download"), 3)
	_, stillThere := idx.Definition(refindex.SymbolCall, "fetch")
	assert.False(t, stillThere)
}

func TestRenameCall_Guards(t *testing.T) {
	sol := loadSolution(t, renameCallFixture)
	tests := []struct {
		name, old, newName, wantMsg string
	}{
		{"invalid new name", "fetch", "1bad", "not a valid call name"},
		{"undefined old", "missing", "whatever", "is not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := RenameCall(sol, tt.old, tt.newName)
			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

// TestRenameCall_KindIsolation verifies renaming a call never touches a
// same-named resolver (and the call: ref is a call, not a resolver ref).
func TestRenameCall_KindIsolation(t *testing.T) {
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: call-iso
spec:
  calls:
    fetch:
      provider: message
      inputs:
        message: hi
  resolvers:
    fetch:
      resolve:
        with:
          - call: fetch
`
	sol := loadSolution(t, y)
	res, err := RenameCall(sol, "fetch", "download")
	require.NoError(t, err)
	// call definition + 1 call: ref = 2 edits; the resolver "fetch" is untouched.
	assert.Len(t, res.Edits, 2)

	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	newSol := loadSolution(t, string(out))
	idx, err := refindex.Build(newSol)
	require.NoError(t, err)
	_, resolverThere := idx.Definition(refindex.SymbolResolver, "fetch")
	assert.True(t, resolverThere, "resolver fetch must be untouched")
	_, callRenamed := idx.Definition(refindex.SymbolCall, "download")
	assert.True(t, callRenamed)
}

// TestRenameCall_HyphenatedNameAllowed documents that call names follow the
// resolver/action grammar (hyphens allowed), unlike author functions.
func TestRenameCall_HyphenatedNameAllowed(t *testing.T) {
	sol := loadSolution(t, renameCallFixture)
	res, err := RenameCall(sol, "fetch", "my-download")
	require.NoError(t, err)
	require.Len(t, res.Edits, 3)
}

const renameFunctionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: rename-func-test # keep this comment
spec:
  functions:
    greet:
      params:
        - name: who
      template: "hello {{ .args.who }}"
    loud:
      params:
        - name: msg
      template: "{{ greet .args.msg }}!"
  resolvers:
    env:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    msg:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ greet ._.env }}"
`

func TestRenameFunction_HappyPath(t *testing.T) {
	sol := loadSolution(t, renameFunctionFixture)

	res, err := RenameFunction(sol, "greet", "welcome")
	require.NoError(t, err)
	// definition + invocation in loud's body + invocation in msg template = 3.
	require.Len(t, res.Edits, 3)

	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "# keep this comment")
	assert.Contains(t, s, "welcome:")
	assert.Contains(t, s, "{{ welcome .args.msg }}")
	assert.Contains(t, s, "{{ welcome ._.env }}")
	assert.NotContains(t, s, "greet")

	newSol := loadSolution(t, s)
	idx, err := refindex.Build(newSol)
	require.NoError(t, err)
	assert.Zero(t, idx.Unresolved())
	assert.Len(t, idx.Occurrences(refindex.SymbolFunction, "welcome"), 3)
}

func TestRenameFunction_Guards(t *testing.T) {
	sol := loadSolution(t, renameFunctionFixture)
	tests := []struct {
		name, old, newName, wantMsg string
	}{
		{"invalid new name", "greet", "1bad", "not a valid function name"},
		{"hyphen not allowed in function name", "greet", "greet-user", "not a valid function name"},
		{"reserved double-underscore prefix", "greet", "__greet", "reserved prefix"},
		{"collides with builtin function", "greet", "printf", "built-in template function"},
		{"collision", "greet", "loud", "already exists"},
		{"undefined old", "missing", "whatever", "is not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := RenameFunction(sol, tt.old, tt.newName)
			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}
