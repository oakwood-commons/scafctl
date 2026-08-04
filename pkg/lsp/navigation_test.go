// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// mkRange builds a 1-based source range from line/column pairs.
func mkRange(startLine, startCol, endLine, endCol int) sourcepos.Range {
	return sourcepos.Range{
		Start: sourcepos.Position{Line: startLine, Column: startCol},
		End:   sourcepos.Position{Line: endLine, Column: endCol},
	}
}

const navFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nav
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
`

const navURI = protocol.DocumentUri("file:///nav.yaml")

// celRefPosition returns an LSP position at the start of the CEL reference to
// name, using the index as the source of truth.
func celRefPosition(t *testing.T, content string) protocol.Position {
	t.Helper()
	_, idx, err := loadIndex([]byte(content))
	require.NoError(t, err)
	for _, r := range idx.References("environment") {
		if r.Origin == refindex.OriginCEL {
			return toLSPPosition(r.Range.Start)
		}
	}
	t.Fatalf("no CEL reference to environment found")
	return protocol.Position{}
}

func TestDefinition_JumpsToResolverDefinition(t *testing.T) {
	pos := celRefPosition(t, navFixture)
	loc := Definition([]byte(navFixture), navURI, pos)
	require.NotNil(t, loc)
	assert.Equal(t, navURI, loc.URI)
	// The definition is the `environment:` key on line 7 (0-based line 6).
	assert.Equal(t, uint32(6), loc.Range.Start.Line)
}

func TestDefinition_NoSymbolReturnsNil(t *testing.T) {
	// Line 0 (apiVersion) has no resolver reference.
	loc := Definition([]byte(navFixture), navURI, protocol.Position{Line: 0, Character: 0})
	assert.Nil(t, loc)
}

func TestReferences_IncludeAndExcludeDeclaration(t *testing.T) {
	pos := celRefPosition(t, navFixture)

	withDecl := References([]byte(navFixture), navURI, pos, true)
	withoutDecl := References([]byte(navFixture), navURI, pos, false)

	// environment: definition + dependsOn + CEL = 3 occurrences; uses = 2.
	assert.Len(t, withDecl, 3)
	assert.Len(t, withoutDecl, 2)
	for _, l := range withDecl {
		assert.Equal(t, navURI, l.URI)
	}
}

func TestPrepareRename(t *testing.T) {
	pos := celRefPosition(t, navFixture)
	rng := PrepareRename([]byte(navFixture), pos)
	require.NotNil(t, rng)
	// The range should cover the "environment" identifier on the same line.
	assert.Equal(t, pos.Line, rng.Start.Line)
	assert.Equal(t, uint32(len("environment")), rng.End.Character-rng.Start.Character)

	// Not on a symbol -> nil (client blocks the rename).
	assert.Nil(t, PrepareRename([]byte(navFixture), protocol.Position{Line: 0, Character: 0}))
}

func TestRename_ProducesWorkspaceEdit(t *testing.T) {
	pos := celRefPosition(t, navFixture)
	edit, err := Rename([]byte(navFixture), navURI, pos, "env")
	require.NoError(t, err)
	require.NotNil(t, edit)

	edits, ok := edit.Changes[navURI]
	require.True(t, ok)
	// definition + dependsOn + CEL = 3 edits, all to "env".
	assert.Len(t, edits, 3)
	for _, e := range edits {
		assert.Equal(t, "env", e.NewText)
	}
}

func TestRename_InvalidNameErrors(t *testing.T) {
	pos := celRefPosition(t, navFixture)
	_, err := Rename([]byte(navFixture), navURI, pos, "1bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid resolver name")
}

func TestRename_NoSymbolErrors(t *testing.T) {
	_, err := Rename([]byte(navFixture), navURI, protocol.Position{Line: 0, Character: 0}, "env")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no renameable symbol")
}

func TestRename_RefusesWhenUnresolved(t *testing.T) {
	// A $-rooted template reference to environment is unpositionable, so the
	// rename must refuse -- surfaced to the client as an error.
	fixture := `apiVersion: scafctl.io/v1
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
                expr: _.environment
    greet:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                tmpl: "{{ $.environment }}"
`
	pos := celRefPosition(t, fixture)
	_, err := Rename([]byte(fixture), navURI, pos, "env")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not be located")
}

func TestRangeContains(t *testing.T) {
	// Range covering columns 5-11 (1-based) on line 2 (1-based).
	r := mkRange(2, 5, 2, 12)
	assert.True(t, rangeContains(r, protocol.Position{Line: 1, Character: 4}))  // at start
	assert.True(t, rangeContains(r, protocol.Position{Line: 1, Character: 11})) // at end (inclusive)
	assert.False(t, rangeContains(r, protocol.Position{Line: 1, Character: 12}))
	assert.False(t, rangeContains(r, protocol.Position{Line: 0, Character: 5})) // wrong line
}
