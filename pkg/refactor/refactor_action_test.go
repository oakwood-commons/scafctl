// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refactor

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// renameActionFixture references action "build" via dependsOn, CEL, and
// template. "make build" is unrelated literal text. "build" also carries an
// alias "b" that must be left untouched by the rename.
const renameActionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: rename-action-test # keep this comment
spec:
  resolvers: {}
  workflow:
    actions:
      build:
        alias: b
        provider: shell
        inputs:
          command: make build
      deploy:
        dependsOn:
          - build
        provider: shell
        when:
          expr: __actions.build.results.exitCode == 0
        inputs:
          command:
            tmpl: 'deploy {{ .__actions.build.results.stdout }}'
`

func TestRenameAction_HappyPath(t *testing.T) {
	sol := loadSolution(t, renameActionFixture)

	res, err := RenameAction(sol, "build", "compile")
	require.NoError(t, err)
	require.Equal(t, "build", res.OldName)
	require.Equal(t, "compile", res.NewName)
	// definition + dependsOn + CEL + template = 4 occurrences (alias excluded).
	require.Len(t, res.Edits, 4)

	raw := sol.RawContent()
	out, err := res.Apply(raw)
	require.NoError(t, err)
	s := string(out)

	// Comments and unrelated literal text are preserved.
	assert.Contains(t, s, "# keep this comment")
	assert.Contains(t, s, "command: make build")
	// The alias is independent and must not be rewritten.
	assert.Contains(t, s, "alias: b")

	// The rewritten content re-parses and the rename is complete and consistent.
	newSol := loadSolution(t, s)
	idx, err := refindex.Build(newSol)
	require.NoError(t, err)
	assert.Zero(t, idx.Unresolved())
	def, ok := idx.Definition(refindex.SymbolAction, "compile")
	require.True(t, ok)
	assert.True(t, def.IsDef)
	assert.Len(t, idx.Occurrences(refindex.SymbolAction, "compile"), 4)
	_, stillThere := idx.Definition(refindex.SymbolAction, "build")
	assert.False(t, stillThere)
}

func TestRenameAction_Guards(t *testing.T) {
	sol := loadSolution(t, renameActionFixture)

	tests := []struct {
		name    string
		old     string
		newName string
		wantMsg string
	}{
		{"invalid new name", "build", "1bad", "not a valid action name"},
		{"equal names", "build", "build", "equals old name"},
		{"undefined old", "missing", "whatever", "is not defined"},
		{"collision", "build", "deploy", "already exists"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := RenameAction(sol, tt.old, tt.newName)
			require.Error(t, err)
			assert.Nil(t, res)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

func TestRenameAction_NilSolution(t *testing.T) {
	res, err := RenameAction(nil, "a", "b")
	require.Error(t, err)
	assert.Nil(t, res)
}

// TestRenameAction_DoesNotTouchResolvers verifies kind isolation: renaming an
// action never rewrites a resolver of the same name, and vice versa.
func TestRenameAction_DoesNotTouchResolvers(t *testing.T) {
	y := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: mixed
spec:
  resolvers:
    build:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: from-resolver
  workflow:
    actions:
      build:
        provider: shell
        inputs:
          command: make
      deploy:
        dependsOn:
          - build
        provider: shell
        inputs:
          value:
            expr: _.build
`
	sol := loadSolution(t, y)

	// Renaming the action "build" must rewrite only the action definition and
	// its dependsOn use (2 edits) -- never the resolver "build" or the CEL
	// resolver ref _.build.
	res, err := RenameAction(sol, "build", "compile")
	require.NoError(t, err)
	assert.Len(t, res.Edits, 2)

	out, err := res.Apply(sol.RawContent())
	require.NoError(t, err)

	newSol := loadSolution(t, string(out))
	idx, err := refindex.Build(newSol)
	require.NoError(t, err)
	// Resolver "build" is untouched; action is now "compile".
	_, resolverStillThere := idx.Definition(refindex.SymbolResolver, "build")
	assert.True(t, resolverStillThere, "resolver build must be untouched")
	_, actionRenamed := idx.Definition(refindex.SymbolAction, "compile")
	assert.True(t, actionRenamed, "action build must be renamed to compile")
}
