// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const sigFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  functions:
    greet:
      description: Greets someone
      params:
        - name: who
          type: string
          required: true
        - name: punct
          type: string
      template: "{{ .args.who }}"
  calls:
    fetchThing:
      description: Fetches a thing
      args:
        url:
          type: string
          required: true
        retries:
          type: int
      provider: http
  resolvers:
    celR:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: arrays.groupBy(_.items, dev)
    tmplR:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ greet .x .y }}"
    callR:
      resolve:
        with:
          - call: fetchThing
            args:
              retries: 3
`

func sigAt(t *testing.T, content, lineHint, token string, within int) *protocol.SignatureHelp {
	t.Helper()
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///sig.yaml")
	s.setDoc(uri, 1, content)
	pos := posAt(t, content, lineHint, token, within)
	sh, err := s.signatureHelp(nil, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
	})
	require.NoError(t, err)
	return sh
}

func firstSig(t *testing.T, sh *protocol.SignatureHelp) protocol.SignatureInformation {
	t.Helper()
	require.NotNil(t, sh)
	require.NotEmpty(t, sh.Signatures)
	return sh.Signatures[0]
}

func paramLabels(si protocol.SignatureInformation) []string {
	out := make([]string, 0, len(si.Parameters))
	for _, p := range si.Parameters {
		out = append(out, fmt.Sprint(p.Label))
	}
	return out
}

func activeParam(si protocol.SignatureInformation) int {
	if si.ActiveParameter == nil {
		return -1
	}
	return int(*si.ActiveParameter)
}

func TestSignatureHelp_CELFunction(t *testing.T) {
	// Cursor on the first argument (_.items).
	si := firstSig(t, sigAt(t, sigFixture, "expr: arrays.groupBy", "_.items", 1))
	assert.Contains(t, si.Label, "arrays.groupBy")
	assert.Equal(t, []string{"list<map<string,dyn>>", "string"}, paramLabels(si),
		"CEL params split on top-level commas (generic commas ignored)")
	assert.Equal(t, 0, activeParam(si), "on the first arg")
}

func TestSignatureHelp_CELActiveParameterTracksCursor(t *testing.T) {
	// Cursor on the second argument (after the comma).
	si := firstSig(t, sigAt(t, sigFixture, "expr: arrays.groupBy", "dev", 1))
	assert.Equal(t, 1, activeParam(si), "on the second arg")
}

func TestSignatureHelp_TemplateAuthorFunction(t *testing.T) {
	// Cursor on the first template argument ".x".
	si := firstSig(t, sigAt(t, sigFixture, `tmpl: "{{ greet`, ".x", 1))
	assert.Contains(t, si.Label, "greet(")
	assert.Equal(t, []string{"who: string!", "punct: string"}, paramLabels(si))
	assert.Equal(t, 0, activeParam(si))
}

func TestSignatureHelp_TemplateActiveParameterTracksCursor(t *testing.T) {
	// Cursor on the second template argument ".y".
	si := firstSig(t, sigAt(t, sigFixture, `tmpl: "{{ greet`, ".y", 1))
	assert.Equal(t, 1, activeParam(si), "on the second template arg")
}

func TestSignatureHelp_TemplateBuiltinFunction(t *testing.T) {
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ upper .x }}"
`
	si := firstSig(t, sigAt(t, content, `tmpl: "{{ upper`, ".x", 1))
	assert.Equal(t, "upper", si.Label, "builtin template function shows its name")
}

func TestSignatureHelp_CallArgs(t *testing.T) {
	// Cursor on the "retries" arg key inside the call's args block.
	si := firstSig(t, sigAt(t, sigFixture, "              retries: 3", "retries", 2))
	assert.Contains(t, si.Label, "fetchThing(")
	// Args are listed sorted; retries before url.
	assert.Equal(t, []string{"retries: int", "url: string!"}, paramLabels(si))
	assert.Equal(t, 0, activeParam(si), "highlights the retries arg on the cursor line")
}

func TestSignatureHelp_CursorOnCallLineDoesNotBorrowSiblingArgs(t *testing.T) {
	// With the cursor on a "- call:" invocation line, we must not surface a
	// sibling call's args (regression for the nearest-key scan).
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  calls:
    first:
      args:
        a:
          type: string
      provider: http
    second:
      provider: http
  resolvers:
    r:
      resolve:
        with:
          - call: first
            args:
              a: x
          - call: second
`
	// Cursor on the "- call: second" line (naming the call, not filling args).
	sh := sigAt(t, content, "          - call: second", "second", 2)
	assert.Nil(t, sh, "no args signature on the call invocation line")
}

func TestSignatureHelp_TemplatePipedCall(t *testing.T) {
	// In "{{ .x | greet a }}" the active call after the pipe is greet.
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  functions:
    greet:
      params:
        - name: who
          type: string
      template: "{{ .args.who }}"
  resolvers:
    a:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ .x | greet a }}"
`
	si := firstSig(t, sigAt(t, content, `tmpl: "{{ .x | greet`, "greet a", len("greet ")))
	assert.Contains(t, si.Label, "greet(")
}

func TestSignatureHelp_NoneWhenNotInCall(t *testing.T) {
	// A plain scalar value is not a call.
	sh := sigAt(t, sigFixture, "  name: c", "c", 0)
	assert.Nil(t, sh)
}

func TestSignatureHelp_UnknownDocument(t *testing.T) {
	s := newTestServer(t)
	sh, err := s.signatureHelp(nil, &protocol.SignatureHelpParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///missing.yaml"},
			Position:     protocol.Position{},
		},
	})
	require.NoError(t, err)
	assert.Nil(t, sh)
}

func TestSignatureHelp_ParseErrorNoPanic(t *testing.T) {
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///bad.yaml")
	s.setDoc(uri, 1, "spec: {calls:\n  expr: arrays.groupBy(x")
	require.NotPanics(t, func() {
		_, err := s.signatureHelp(nil, &protocol.SignatureHelpParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
				Position:     protocol.Position{Line: 1, Character: 22},
			},
		})
		require.NoError(t, err)
	})
}

func TestSignatureHelp_NilEntry(t *testing.T) {
	assert.Nil(t, SignatureHelp(nil, protocol.Position{}))
}

func TestSignatureHelp_FeatureRegisteredAndAdvertised(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	assert.NotNil(t, h.TextDocumentSignatureHelp)

	res, err := h.Initialize(nil, &protocol.InitializeParams{})
	require.NoError(t, err)
	init := res.(protocol.InitializeResult)
	require.NotNil(t, init.Capabilities.SignatureHelpProvider)
	assert.Equal(t, signatureHelpTriggerCharacters, init.Capabilities.SignatureHelpProvider.TriggerCharacters)
}

// --- helper unit tests ---

func TestEnclosingCallParen(t *testing.T) {
	r := []rune(`foo(a, bar(b`)
	open, ok := enclosingCallParen(r, len(r)) // inside bar(
	require.True(t, ok)
	assert.Equal(t, 10, open)

	// A closed call: cursor after the ")" is not inside a call.
	r = []rune(`foo(a)`)
	_, ok = enclosingCallParen(r, len(r))
	assert.False(t, ok)

	// Parens inside a string are ignored.
	r = []rune(`foo("(") `)
	_, ok = enclosingCallParen(r, len(r))
	assert.False(t, ok)
}

func TestIdentifierBefore(t *testing.T) {
	r := []rune("arrays.groupBy(")
	assert.Equal(t, "arrays.groupBy", identifierBefore(r, len(r)-1))
	assert.Equal(t, "", identifierBefore([]rune("  ("), 2))
}

func TestTopLevelCommas(t *testing.T) {
	r := []rune("a, b, c")
	assert.Equal(t, uint32(2), topLevelCommas(r, 0, len(r)))
	// Commas inside generics/brackets are not counted.
	r = []rune("map<string,dyn>, x")
	assert.Equal(t, uint32(1), topLevelCommas(r, 0, len(r)))
	// Commas inside a string literal are not counted.
	r = []rune(`"a,b", c`)
	assert.Equal(t, uint32(1), topLevelCommas(r, 0, len(r)))
}

func TestSignatureParams(t *testing.T) {
	params := signatureParams("arrays.groupBy(list<map<string,dyn>>, string) -> map<string, list>")
	assert.Equal(t, []string{"list<map<string,dyn>>", "string"}, params)

	assert.Nil(t, signatureParams("noParens"))
	assert.Empty(t, signatureParams("fn()"))
}

func TestEnclosingCallName(t *testing.T) {
	raw := []byte("spec:\n  resolvers:\n    a:\n      resolve:\n        with:\n          - call: myCall\n            args:\n              foo: 1\n")
	name, ok := enclosingCallName(raw, 7) // the "foo: 1" line
	require.True(t, ok)
	assert.Equal(t, "myCall", name)

	// Not inside an args block.
	_, ok = enclosingCallName(raw, 5)
	assert.False(t, ok)
}
