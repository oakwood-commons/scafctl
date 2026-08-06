// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

const funcCompletionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  functions:
    myHelper:
      description: A custom helper
      template: "{{ . }}"
  resolvers:
    celR:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: arr
    tmplR:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ up }}"
    authorR:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ my }}"
`

func funcCompleteAt(t *testing.T, lineHint, token string, within int) []protocol.CompletionItem {
	t.Helper()
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///fc.yaml")
	s.setDoc(uri, 1, funcCompletionFixture)
	pos := posAt(t, funcCompletionFixture, lineHint, token, within)
	res, err := s.completion(nil, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
	})
	require.NoError(t, err)
	if res == nil {
		return nil
	}
	items, ok := res.([]protocol.CompletionItem)
	require.True(t, ok)
	return items
}

func labelsOf(items []protocol.CompletionItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

func TestFuncCompletion_CELContextListsCELFunctions(t *testing.T) {
	items := funcCompleteAt(t, "                expr: arr", "arr", 3)
	labels := labelsOf(items)
	require.NotEmpty(t, labels)
	assert.Contains(t, labels, "arrays.groupBy")
	// Every suggestion is a CEL function (has a signature detail) with a call snippet.
	for _, it := range items {
		require.NotNil(t, it.Kind)
		assert.Equal(t, protocol.CompletionItemKindFunction, *it.Kind)
		require.NotNil(t, it.InsertText)
		assert.Contains(t, *it.InsertText, "($0)")
		require.NotNil(t, it.InsertTextFormat)
		assert.Equal(t, protocol.InsertTextFormatSnippet, *it.InsertTextFormat)
	}
}

func TestFuncCompletion_CELDoesNotListTemplateFunctions(t *testing.T) {
	// "up" would match template "upper"; in CEL context it must not appear.
	items := funcCompleteAt(t, "                expr: arr", "arr", 3)
	assert.NotContains(t, labelsOf(items), "upper")
}

func TestFuncCompletion_TemplateContextListsTemplateFunctions(t *testing.T) {
	items := funcCompleteAt(t, `                tmpl: "{{ up`, "up", 2)
	assert.Contains(t, labelsOf(items), "upper")
	// Template builtins have no CEL signature, so their detail is the generic
	// "template function" label.
	for _, it := range items {
		if it.Label == "upper" {
			require.NotNil(t, it.Detail)
			assert.Equal(t, "template function", *it.Detail)
		}
	}
}

func TestFuncCompletion_LiteralTemplateTextOffersNothing(t *testing.T) {
	// Outside a {{ }} action (plain literal text in a tmpl value) is not an
	// expression position, so no functions are offered.
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
                tmpl: "hello wo"
`
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///lit.yaml")
	s.setDoc(uri, 1, content)
	pos := posAt(t, content, `tmpl: "hello wo`, "wo", 2)
	res, err := s.completion(nil, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position:     pos,
		},
	})
	require.NoError(t, err)
	assert.Nil(t, res, "literal text in a template offers no completions")
}

func TestFuncCompletion_TemplateContextListsAuthorFunctions(t *testing.T) {
	items := funcCompleteAt(t, `                tmpl: "{{ my`, "my", 2)
	labels := labelsOf(items)
	assert.Contains(t, labels, "myHelper")
	// The author function carries its declared description and an author detail.
	for _, it := range items {
		if it.Label == "myHelper" {
			require.NotNil(t, it.Detail)
			assert.Equal(t, "author function", *it.Detail)
			mc, ok := it.Documentation.(protocol.MarkupContent)
			require.True(t, ok)
			assert.Contains(t, mc.Value, "A custom helper")
		}
	}
}

func TestFuncCompletion_PrefixFilters(t *testing.T) {
	all := funcCompleteAt(t, `                tmpl: "{{ up`, "up", 2)
	for _, l := range labelsOf(all) {
		assert.True(t, len(l) >= 2 && (l[:2] == "up" || l[:2] == "Up"), "unexpected non-matching label %q", l)
	}
}

func TestFuncCompletion_ReferencePrefixIsNotFunction(t *testing.T) {
	// A reference-prefixed token is a data/symbol reference (handled by #774),
	// not a function completion.
	assert.Nil(t, funcCompletions(nil, CursorContext{Kind: CursorCEL, ExprPrefix: "_.", PartialToken: "env"}))
	assert.Nil(t, funcCompletions(nil, CursorContext{Kind: CursorTemplate, ExprPrefix: "._.", PartialToken: "app"}))
}

func TestFuncCompletion_NilEntryTemplateStillListsBuiltins(t *testing.T) {
	// Author functions need the doc, but built-in template functions do not.
	items := funcCompletions(nil, CursorContext{Kind: CursorTemplate, PartialToken: "upper"})
	assert.Contains(t, labelsOf(items), "upper")
}

func TestFuncDocMarkdown(t *testing.T) {
	md := funcDocMarkdown(FuncInfo{Description: "does a thing", Examples: []string{"thing(x)"}})
	assert.Contains(t, md, "does a thing")
	assert.Contains(t, md, "thing(x)")
	assert.Contains(t, md, "_Example:_")

	assert.Empty(t, funcDocMarkdown(FuncInfo{}))
}

func TestMatchesPrefix(t *testing.T) {
	assert.True(t, matchesPrefix("upper", ""))
	assert.True(t, matchesPrefix("Upper", "up"))
	assert.False(t, matchesPrefix("lower", "up"))
}
