// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// completeAt opens content and returns the completion labels at the position of
// token (offset by within) on the first line containing lineHint.
func completeAt(t *testing.T, content, lineHint, token string, within int) []string {
	t.Helper()
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///completion.yaml")
	s.setDoc(uri, 1, content)
	pos := posAt(t, content, lineHint, token, within)
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
	require.True(t, ok, "completion returns []CompletionItem")
	labels := make([]string, 0, len(items))
	for _, it := range items {
		labels = append(labels, it.Label)
	}
	return labels
}

func contains(labels []string, want string) bool {
	for _, l := range labels {
		if l == want {
			return true
		}
	}
	return false
}

// completionFixture has partial keys (no colon) and an enum value being typed.
const completionFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  resolvers:
    appName:
      desc
      resolve:
        with:
          - provider: parameter
            onErr
            inputs:
              value: dev
    envName:
      resolve:
        with:
          - provider: parameter
            onError: fa
            inputs:
              value: dev
`

func TestCompletion_KeyUnderResolver(t *testing.T) {
	labels := completeAt(t, completionFixture, "      desc", "desc", 4)
	assert.Equal(t, []string{"description"}, labels, "partial 'desc' offers the description field")
}

func TestCompletion_KeyUnderProviderSource(t *testing.T) {
	// "onErr" under a with[] element completes to provider-source fields.
	labels := completeAt(t, completionFixture, "            onErr", "onErr", 5)
	assert.Contains(t, labels, "onError", "partial 'onErr' offers onError")
}

func TestCompletion_BlockKey(t *testing.T) {
	// Cursor on the existing "with" block key offers the resolve-phase children.
	labels := completeAt(t, completionFixture, "        with:", "with", 2)
	assert.True(t, contains(labels, "with"), "labels %v should include with", labels)
}

func TestCompletion_TopLevelKey(t *testing.T) {
	content := "apiVersion: scafctl.io/v1\nkind: Solution\nmeta\n"
	labels := completeAt(t, content, "meta", "meta", 4)
	assert.Contains(t, labels, "metadata", "top-level partial 'meta' offers metadata")
}

func TestCompletion_EnumValue(t *testing.T) {
	labels := completeAt(t, completionFixture, "            onError: fa", "fa", 2)
	assert.Equal(t, []string{"fail"}, labels, "enum partial 'fa' offers fail")
}

func TestCompletion_EnumValueNoPartialOffersAll(t *testing.T) {
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: parameter
            onError: 
            inputs:
              value: dev
`
	// Cursor right after "onError: " (empty value) offers every allowed value.
	labels := completeAt(t, content, "            onError: ", "onError: ", len("onError: "))
	assert.ElementsMatch(t, []string{"fail", "continue"}, labels)
}

func TestCompletion_MapKeyPositionOffersNothing(t *testing.T) {
	// Typing a new resolver NAME (a dynamic map key) is not a schema field
	// position, so there is nothing structural to suggest.
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  resolvers:
    myNewResolver
`
	labels := completeAt(t, content, "    myNewResolver", "myNewResolver", 3)
	assert.Empty(t, labels)
}

func TestCompletion_UnknownPathOffersNothing(t *testing.T) {
	// A key under a mistyped/unknown section resolves to no schema type, so no
	// child fields are offered (distinct from the map-key short-circuit).
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  notARealSection:
    child
`
	labels := completeAt(t, content, "    child", "child", 3)
	assert.Empty(t, labels)
}

func TestCompletion_UnknownDocument(t *testing.T) {
	s := newTestServer(t)
	res, err := s.completion(nil, &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: "file:///missing.yaml"},
			Position:     protocol.Position{},
		},
	})
	require.NoError(t, err)
	assert.Nil(t, res)
}

func TestCompletion_ParseErrorNoPanic(t *testing.T) {
	s := newTestServer(t)
	uri := protocol.DocumentUri("file:///bad.yaml")
	s.setDoc(uri, 1, "spec: {resolvers:\n  provider")
	require.NotPanics(t, func() {
		res, err := s.completion(nil, &protocol.CompletionParams{
			TextDocumentPositionParams: protocol.TextDocumentPositionParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
				Position:     protocol.Position{Line: 1, Character: 4},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, res, "a parse-error document yields no completions")
	})
}

func TestCompletion_DispatchSkeletonReturnsNilForUnhandledClasses(t *testing.T) {
	// The branches #774 will fill (SymbolRef) plus classes with no source remain
	// empty. CEL/Template are now handled by funcCompletions (see completion_funcs_test.go).
	for _, cc := range []CursorContext{
		{Kind: CursorSymbolRef},
		{Kind: CursorProviderName},
		{Kind: CursorNone},
	} {
		assert.Nil(t, completionItems(nil, cc), "kind %s has no source", cc.Kind)
	}
}

func TestCompletion_FeatureRegisteredAndAdvertised(t *testing.T) {
	s := newTestServer(t)
	h := s.Handler()
	assert.NotNil(t, h.TextDocumentCompletion, "completion handler wired")

	res, err := h.Initialize(nil, &protocol.InitializeParams{})
	require.NoError(t, err)
	init := res.(protocol.InitializeResult)
	require.NotNil(t, init.Capabilities.CompletionProvider)
	assert.Equal(t, completionTriggerCharacters, init.Capabilities.CompletionProvider.TriggerCharacters)
}

func TestParentPath(t *testing.T) {
	assert.Equal(t, "spec.resolvers", parentPath("spec.resolvers.appName"))
	assert.Equal(t, "", parentPath("spec"))
	assert.Equal(t, "", parentPath(""))
}

func TestContainerPathByIndent(t *testing.T) {
	raw := []byte(completionFixture)
	// "onErr" is on the line after "- provider: parameter"; its container walks
	// up to spec.resolvers.appName.resolve.with (the list key).
	line := lineIndexOf(t, completionFixture, "            onErr")
	got := containerPathByIndent(raw, line, 12)
	assert.Equal(t, "spec.resolvers.appName.resolve.with", got)

	// "desc" under appName.
	line = lineIndexOf(t, completionFixture, "      desc")
	got = containerPathByIndent(raw, line, 6)
	assert.Equal(t, "spec.resolvers.appName", got)
}

func TestBarePartialKey(t *testing.T) {
	// "  desc" with cursor at end -> identifier "desc" starting at rune 2.
	start, ok := barePartialKey([]rune("  desc"), 6)
	require.True(t, ok)
	assert.Equal(t, 2, start)

	// A "key: value" line is NOT a bare key (handled by parseKeyLine).
	_, ok = barePartialKey([]rune("  key: value"), 4)
	assert.False(t, ok)

	// A dash-led first field.
	start, ok = barePartialKey([]rune("  - prov"), 8)
	require.True(t, ok)
	assert.Equal(t, 4, start)

	// Blank line -> no bare key.
	_, ok = barePartialKey([]rune("    "), 2)
	assert.False(t, ok)
}

// lineIndexOf returns the 0-based index of the first line containing sub.
func lineIndexOf(t *testing.T, content, sub string) int {
	t.Helper()
	line := 0
	cur := ""
	for _, c := range content {
		if c == '\n' {
			if containsSub(cur, sub) {
				return line
			}
			line++
			cur = ""
			continue
		}
		cur += string(c)
	}
	if containsSub(cur, sub) {
		return line
	}
	t.Fatalf("no line contains %q", sub)
	return -1
}

func containsSub(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
