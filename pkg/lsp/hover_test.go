// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const hoverFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: hover
spec:
  resolvers:
    environment:
      description: The target deployment environment
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    greeting:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ upper .name }}"
        when: _.environment == "dev"
`

// hoverAt opens the fixture and returns the hover markdown at the position of
// token (offset by within) on the first line containing lineHint. Empty string
// means no hover.
func hoverAt(t *testing.T, lineHint, token string, within int) string {
	t.Helper()
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///hover.yaml")
	s.setDoc(uri, 1, hoverFixture)
	pos := posAt(t, hoverFixture, lineHint, token, within)
	h, err := s.hover(nil, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
	})
	require.NoError(t, err)
	if h == nil {
		return ""
	}
	mc, ok := h.Contents.(protocol.MarkupContent)
	require.True(t, ok, "hover contents should be MarkupContent")
	assert.Equal(t, protocol.MarkupKindMarkdown, mc.Kind)
	return mc.Value
}

func TestHover_SymbolRef(t *testing.T) {
	md := hoverAt(t, `when: _.environment`, "environment ==", 1)
	assert.Contains(t, md, "resolver")
	assert.Contains(t, md, "environment")
	assert.Contains(t, md, "The target deployment environment", "shows the symbol description")
}

func TestHover_ProviderName(t *testing.T) {
	md := hoverAt(t, "provider: parameter", "parameter", 2)
	assert.Contains(t, md, "provider")
	assert.Contains(t, strings.ToLower(md), "parameter")
	assert.Contains(t, md, "**Inputs:**", "provider hover lists inputs")
}

func TestHover_TemplateFunction(t *testing.T) {
	md := hoverAt(t, `tmpl: "{{ upper .name }}"`, "upper", 2)
	assert.Contains(t, md, "template function")
	assert.Contains(t, md, "upper")
}

func TestHover_YAMLKey(t *testing.T) {
	md := hoverAt(t, "      description: The target", "description", 2)
	assert.Contains(t, md, "field")
	assert.Contains(t, md, "description")
	assert.Contains(t, md, "Human-readable", "shows the schema field doc")
}

func TestHover_None(t *testing.T) {
	// Whitespace in indentation yields no hover.
	md := hoverAt(t, "provider: parameter", "provider", -4)
	assert.Empty(t, md)
}

func TestHover_UnknownDocument(t *testing.T) {
	s := newTestServer(t)
	h, err := s.hover(nil, &protocol.HoverParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///missing.yaml"},
			Position:     protocol.Position{},
		},
	})
	require.NoError(t, err)
	assert.Nil(t, h)
}

func TestHover_ParseErrorNoPanic(t *testing.T) {
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///bad.yaml")
	s.setDoc(uri, 1, "spec: {resolvers:\n  provider: parameter")
	require.NotPanics(t, func() {
		_, err := s.hover(nil, &protocol.HoverParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
				Position:     protocol.Position{Line: 1, Character: 4},
			},
		})
		require.NoError(t, err)
	})
}

func TestHover_CELFunctionDirect(t *testing.T) {
	// Namespaced CEL function names are captured whole.
	md := funcHover("arrays.groupBy")
	assert.Contains(t, md, "CEL function")
	assert.Contains(t, md, "arrays.groupBy")
	assert.Contains(t, md, "Example")
}

func TestHover_FeatureRegisteredAndAdvertised(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	assert.NotNil(t, h.TextDocumentHover, "hover handler wired")

	res, err := h.Initialize(nil, &protocol.InitializeParams{})
	require.NoError(t, err)
	init := res.(protocol.InitializeResult)
	assert.Equal(t, true, init.Capabilities.HoverProvider, "HoverProvider advertised")
}

func TestSchemaPath(t *testing.T) {
	cases := map[string]string{
		"spec.resolvers.environment.description":             "spec.resolvers.description",
		"spec.resolvers.a.resolve.with[0].provider":          "spec.resolvers.resolve.with.provider",
		"spec.resolvers.a.resolve.with[0].inputs.value.expr": "spec.resolvers.resolve.with.inputs.expr",
		"spec.workflow.actions.deploy.provider":              "spec.workflow.actions.provider",
		"metadata.name":                                      "metadata.name",
	}
	for in, want := range cases {
		assert.Equal(t, want, schemaPath(in), "schemaPath(%q)", in)
	}
}

func TestStripIndices(t *testing.T) {
	assert.Equal(t, "a.b.c", stripIndices("a.b[0].c"))
	assert.Equal(t, "a.b.c", stripIndices("a.b[10].c[2]"))
	assert.Equal(t, "a", stripIndices("a"))
}

func TestFullIdentAt(t *testing.T) {
	raw := []byte("  value: arrays.groupBy(x)\n")
	// cursor inside "groupBy"
	got := fullIdentAt(raw, protocol.Position{Line: 0, Character: 18})
	assert.Equal(t, "arrays.groupBy", got)
}

func TestSymbolDescription_AllKinds(t *testing.T) {
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: t
spec:
  resolvers:
    myResolver:
      description: A resolver desc
      resolve:
        with:
          - provider: parameter
            inputs:
              value: x
  calls:
    myCall:
      description: A reusable call
      provider: parameter
  functions:
    myFunc:
      description: A helper function
      cel: "1 + 1"
  workflow:
    actions:
      myAction:
        description: An action desc
        provider: debug
    finally:
      cleanup:
        description: A finally action
        provider: debug
`
	sol := &solution.Solution{}
	require.NoError(t, sol.UnmarshalFromBytes([]byte(content)))

	assert.Equal(t, "A resolver desc", symbolDescription(sol, refindex.SymbolResolver, "myResolver"))
	assert.Equal(t, "A reusable call", symbolDescription(sol, refindex.SymbolCall, "myCall"))
	assert.Equal(t, "A helper function", symbolDescription(sol, refindex.SymbolFunction, "myFunc"))
	assert.Equal(t, "An action desc", symbolDescription(sol, refindex.SymbolAction, "myAction"))
	assert.Equal(t, "A finally action", symbolDescription(sol, refindex.SymbolAction, "cleanup"))

	// Unknown name and nil solution yield empty.
	assert.Empty(t, symbolDescription(sol, refindex.SymbolResolver, "nope"))
	assert.Empty(t, symbolDescription(nil, refindex.SymbolResolver, "myResolver"))
}

func TestHoverMarkdown_PrefixedCELTokenIsNotAFunction(t *testing.T) {
	// A reference-prefixed CEL/template token (e.g. "_.foo") has no function
	// metadata; hover must return "".
	s := newTestServer(t)
	e := &DocEntry{Raw: []byte("expr: _.foo\n")}
	md := s.hoverMarkdown(e, CursorContext{Kind: CursorCEL, ExprPrefix: "_.", PartialToken: "foo"}, protocol.Position{})
	assert.Empty(t, md)
}

func TestKeyHover_Concept(t *testing.T) {
	// The "workflow" key matches a concept, so its hover includes the concept
	// summary alongside the schema field doc.
	md := keyHover("spec.workflow")
	assert.Contains(t, md, "concept")
	assert.Contains(t, md, "Workflow")
}
