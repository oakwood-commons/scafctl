// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	glsp "github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const deprecatedDoc = `apiVersion: scafctl.io/v1
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

const cleanDoc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: clean
spec:
  resolvers:
    a:
      description: an input
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
  workflow:
    actions:
      show:
        description: prints
        provider: message
        inputs:
          message:
            expr: _.a
`

// openDoc seeds the server's document cache with content at uri.
func openDoc(t *testing.T, s *Server, uri protocol.DocumentUri, text string) {
	t.Helper()
	s.docs.Set(uri, 1, text)
	entry, ok := s.getDoc(uri)
	require.True(t, ok)
	require.NotNil(t, entry.Sol, "test document must parse")
}

// quickFixParams builds a code-action request over the given line range with a
// single incoming deprecated-field diagnostic (the rule used by these tests).
func quickFixParams(uri protocol.DocumentUri, line uint32, only []protocol.CodeActionKind) *protocol.CodeActionParams {
	rng := protocol.Range{
		Start: protocol.Position{Line: line, Character: 0},
		End:   protocol.Position{Line: line, Character: 20},
	}
	code := protocol.IntegerOrString{Value: "deprecated-field"}
	return &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        rng,
		Context: protocol.CodeActionContext{
			Diagnostics: []protocol.Diagnostic{{Range: rng, Code: &code}},
			Only:        only,
		},
	}
}

func TestCodeAction_DeprecatedField_ReturnsQuickFix(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///dep.yaml"
	openDoc(t, s, uri, deprecatedDoc)

	// The onError line is line 10 (0-based) in deprecatedDoc.
	params := quickFixParams(uri, 10, nil)
	res, err := s.codeAction(&glsp.Context{}, params)
	require.NoError(t, err)
	require.NotNil(t, res)

	actions, ok := res.([]protocol.CodeAction)
	require.True(t, ok)
	require.Len(t, actions, 1)

	a := actions[0]
	assert.Equal(t, "Replace deprecated 'onError' with 'continueOnError'", a.Title)
	require.NotNil(t, a.Kind)
	assert.Equal(t, protocol.CodeActionKindQuickFix, *a.Kind)
	require.NotNil(t, a.IsPreferred)
	assert.True(t, *a.IsPreferred)
	require.NotNil(t, a.Edit)
	edits := a.Edit.Changes[uri]
	require.NotEmpty(t, edits)
	assert.Equal(t, "continueOnError: true", edits[0].NewText)
	// The resolved diagnostic is attached.
	require.Len(t, a.Diagnostics, 1)
}

// twoDeprecatedDoc has two deprecated onError fields on different lines (line 10
// in resolver a, line 17 in resolver b), producing two deprecated-field findings.
// A workflow references both resolvers so neither is flagged unused.
const twoDeprecatedDoc = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: twodep
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
            onError: fail
            inputs:
              value:
                expr: _.a
  workflow:
    actions:
      show:
        provider: message
        inputs:
          message:
            expr: _.a + _.b
`

func TestCodeAction_AttachesOnlyItsOwnDiagnostic(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///twodep.yaml"
	openDoc(t, s, uri, twoDeprecatedDoc)

	code := protocol.IntegerOrString{Value: "deprecated-field"}
	diagA := protocol.Diagnostic{
		Range: protocol.Range{Start: protocol.Position{Line: 10}, End: protocol.Position{Line: 10, Character: 30}},
		Code:  &code,
	}
	diagB := protocol.Diagnostic{
		Range: protocol.Range{Start: protocol.Position{Line: 17}, End: protocol.Position{Line: 17, Character: 30}},
		Code:  &code,
	}
	// A request spanning both findings, carrying both same-rule diagnostics.
	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        protocol.Range{Start: protocol.Position{Line: 0}, End: protocol.Position{Line: 30}},
		Context:      protocol.CodeActionContext{Diagnostics: []protocol.Diagnostic{diagA, diagB}},
	}

	res, err := s.codeAction(&glsp.Context{}, params)
	require.NoError(t, err)
	actions, ok := res.([]protocol.CodeAction)
	require.True(t, ok)

	// Both deprecated-field quick fixes are returned, and each attaches exactly
	// ONE diagnostic -- the one on its own line -- not every same-rule diagnostic
	// in the document.
	deprecatedActions := 0
	for _, a := range actions {
		if a.Title != "Replace deprecated 'onError' with 'continueOnError'" {
			continue
		}
		deprecatedActions++
		require.Len(t, a.Diagnostics, 1,
			"action must resolve only its own diagnostic")
		attachedLine := a.Diagnostics[0].Range.Start.Line
		editLine := a.Edit.Changes[uri][0].Range.Start.Line
		assert.Equal(t, editLine, attachedLine,
			"attached diagnostic line must match the action's edit line")
	}
	assert.Equal(t, 2, deprecatedActions, "one quick fix per deprecated-field finding")
}

func TestCodeAction_CleanDoc_ReturnsNil(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///clean.yaml"
	openDoc(t, s, uri, cleanDoc)

	params := quickFixParams(uri, 0, nil)
	res, err := s.codeAction(&glsp.Context{}, params)
	require.NoError(t, err)
	assert.Nil(t, res, "a clean document offers no quick fixes")
}

func TestCodeAction_UnknownDocument_ReturnsNil(t *testing.T) {
	s := newTestServer(t)
	res, err := s.codeAction(&glsp.Context{}, quickFixParams("file:///missing.yaml", 0, nil))
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCodeAction_RespectsOnlyFilter(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///dep.yaml"
	openDoc(t, s, uri, deprecatedDoc)

	// Only refactor kinds requested -> the server contributes nothing.
	params := quickFixParams(uri, 10, []protocol.CodeActionKind{protocol.CodeActionKindRefactor})
	res, err := s.codeAction(&glsp.Context{}, params)
	require.NoError(t, err)
	assert.Nil(t, res)

	// Explicitly requesting quickfix -> the action is returned.
	params = quickFixParams(uri, 10, []protocol.CodeActionKind{protocol.CodeActionKindQuickFix})
	res, err = s.codeAction(&glsp.Context{}, params)
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestCodeAction_MatchesByDiagnosticCodeOutsideRange(t *testing.T) {
	s := newTestServer(t)
	const uri = "file:///dep.yaml"
	openDoc(t, s, uri, deprecatedDoc)

	// Request range is far from the finding line, but an incoming diagnostic
	// carries the matching rule Code at the finding's line -> still matched.
	rng := protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: 0, Character: 1},
	}
	code := protocol.IntegerOrString{Value: "deprecated-field"}
	diagRange := protocol.Range{
		Start: protocol.Position{Line: 10, Character: 0},
		End:   protocol.Position{Line: 10, Character: 20},
	}
	params := &protocol.CodeActionParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: uri},
		Range:        rng,
		Context: protocol.CodeActionContext{
			Diagnostics: []protocol.Diagnostic{{Range: diagRange, Code: &code}},
		},
	}
	res, err := s.codeAction(&glsp.Context{}, params)
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestCodeActionFeature_AdvertisesQuickFixKind(t *testing.T) {
	f := codeActionFeature()
	require.NotNil(t, f.advertise)

	var caps protocol.ServerCapabilities
	f.advertise(&caps)

	opts, ok := caps.CodeActionProvider.(*protocol.CodeActionOptions)
	require.True(t, ok, "CodeActionProvider must be CodeActionOptions")
	require.Len(t, opts.CodeActionKinds, 1)
	assert.Equal(t, protocol.CodeActionKindQuickFix, opts.CodeActionKinds[0])
}

func TestCodeActionFeature_Registered(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	assert.NotNil(t, h.TextDocumentCodeAction, "codeAction handler must be wired")
}
