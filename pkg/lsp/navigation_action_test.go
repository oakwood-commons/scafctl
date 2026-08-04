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

// navActionFixture references action "build" via dependsOn, CEL, and template.
const navActionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: nav-action
spec:
  resolvers: {}
  workflow:
    actions:
      build:
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

// actionCELRefPosition returns an LSP position at the start of the CEL reference
// to action "build" in navActionFixture.
func actionCELRefPosition(t *testing.T) protocol.Position {
	t.Helper()
	_, idx, err := loadIndex([]byte(navActionFixture))
	require.NoError(t, err)
	for _, r := range idx.References(refindex.SymbolAction, "build") {
		if r.Origin == refindex.OriginCEL {
			return toLSPPosition(r.Range.Start)
		}
	}
	t.Fatalf("no CEL reference to action build found")
	return protocol.Position{}
}

func TestDefinition_JumpsToActionDefinition(t *testing.T) {
	pos := actionCELRefPosition(t)
	loc := Definition([]byte(navActionFixture), navURI, pos)
	require.NotNil(t, loc)
	assert.Equal(t, navURI, loc.URI)
	// The `build:` action key is on line 9 (0-based line 8).
	assert.Equal(t, uint32(8), loc.Range.Start.Line)
}

func TestReferences_ActionIncludeAndExcludeDeclaration(t *testing.T) {
	pos := actionCELRefPosition(t)

	withDecl := References([]byte(navActionFixture), navURI, pos, true)
	withoutDecl := References([]byte(navActionFixture), navURI, pos, false)

	// build: definition + dependsOn + CEL + template = 4 occurrences; uses = 3.
	assert.Len(t, withDecl, 4)
	assert.Len(t, withoutDecl, 3)
}

func TestRename_ActionProducesWorkspaceEdit(t *testing.T) {
	pos := actionCELRefPosition(t)
	edit, err := Rename([]byte(navActionFixture), navURI, pos, "compile")
	require.NoError(t, err)
	require.NotNil(t, edit)

	edits, ok := edit.Changes[navURI]
	require.True(t, ok)
	// definition + dependsOn + CEL + template = 4 edits, all to "compile".
	assert.Len(t, edits, 4)
	for _, e := range edits {
		assert.Equal(t, "compile", e.NewText)
	}
}

func TestRename_ActionInvalidNameErrors(t *testing.T) {
	pos := actionCELRefPosition(t)
	_, err := Rename([]byte(navActionFixture), navURI, pos, "1bad")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a valid action name")
}
