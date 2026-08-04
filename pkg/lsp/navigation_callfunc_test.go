// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const navCallFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nav-call
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

const navFunctionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nav-func
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

// firstRefPos returns an LSP position at the first non-definition reference of
// the (kind, name) symbol in content.
func firstRefPos(t *testing.T, content string, kind refindex.SymbolKind, name string) protocol.Position {
	t.Helper()
	_, idx, err := loadIndex([]byte(content))
	require.NoError(t, err)
	refs := idx.References(kind, name)
	require.NotEmpty(t, refs, "expected a reference to %s %q", kind, name)
	return toLSPPosition(refs[0].Range.Start)
}

func TestDefinition_JumpsToCallDefinition(t *testing.T) {
	pos := firstRefPos(t, navCallFixture, refindex.SymbolCall, "fetch")
	loc := Definition([]byte(navCallFixture), navURI, pos)
	require.NotNil(t, loc)
	// The `fetch:` call key is on line 7 (0-based line 6).
	assert.Equal(t, uint32(6), loc.Range.Start.Line)
}

func TestRename_CallProducesWorkspaceEdit(t *testing.T) {
	pos := firstRefPos(t, navCallFixture, refindex.SymbolCall, "fetch")
	edit, err := Rename([]byte(navCallFixture), navURI, pos, "download")
	require.NoError(t, err)
	require.NotNil(t, edit)
	edits := edit.Changes[navURI]
	// definition + resolve.with call + action call = 3.
	assert.Len(t, edits, 3)
	for _, e := range edits {
		assert.Equal(t, "download", e.NewText)
	}
}

func TestReferences_FunctionAcrossTemplatesAndBodies(t *testing.T) {
	pos := firstRefPos(t, navFunctionFixture, refindex.SymbolFunction, "greet")
	withDecl := References([]byte(navFunctionFixture), navURI, pos, true)
	withoutDecl := References([]byte(navFunctionFixture), navURI, pos, false)
	// greet: definition + loud body + msg template = 3; uses = 2.
	assert.Len(t, withDecl, 3)
	assert.Len(t, withoutDecl, 2)
}

func TestRename_FunctionProducesWorkspaceEdit(t *testing.T) {
	pos := firstRefPos(t, navFunctionFixture, refindex.SymbolFunction, "greet")
	edit, err := Rename([]byte(navFunctionFixture), navURI, pos, "welcome")
	require.NoError(t, err)
	require.NotNil(t, edit)
	edits := edit.Changes[navURI]
	assert.Len(t, edits, 3)
	for _, e := range edits {
		assert.Equal(t, "welcome", e.NewText)
	}
}

func TestRename_FunctionInvalidNameErrors(t *testing.T) {
	pos := firstRefPos(t, navFunctionFixture, refindex.SymbolFunction, "greet")
	_, err := Rename([]byte(navFunctionFixture), navURI, pos, "1bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid function name")
}
