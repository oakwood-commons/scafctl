// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	protocol "github.com/tliron/glsp/protocol_3_16"
)

// cursorFixture exercises every cursor class. Markers in comments name the lines
// the tests target.
const cursorFixture = `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: cursor
spec:
  resolvers:
    environment:
      resolve:
        with:
          - provider: parameter
            inputs:
              value: dev
    appName:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: size(_.environment)
        when: _.environment == "dev"
    greeting:
      resolve:
        with:
          - provider: go-template
            inputs:
              value:
                tmpl: "{{ upper .name }}"
            onError: fail
`

// posAt returns the LSP position of the first occurrence of token on the first
// line containing lineHint, offset by within runes into that token.
func posAt(t *testing.T, content, lineHint, token string, within int) protocol.Position {
	t.Helper()
	for i, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, lineHint) {
			continue
		}
		col := strings.Index(line, token)
		require.GreaterOrEqual(t, col, 0, "token %q not on line %q", token, lineHint)
		// Index gives a byte offset; for the ASCII fixture that equals the rune
		// column.
		return protocol.Position{Line: uint32(i), Character: uint32(col + within)}
	}
	t.Fatalf("no line contains %q", lineHint)
	return protocol.Position{}
}

func resolveFixture(t *testing.T, content string, pos protocol.Position) CursorContext {
	t.Helper()
	c := NewDocumentCache()
	e := c.Set("file:///cursor.yaml", 1, content)
	return ResolveCursor(e, pos)
}

func TestResolveCursor_Classes(t *testing.T) {
	tests := []struct {
		name         string
		lineHint     string
		token        string
		within       int
		wantKind     CursorKind
		wantPartial  string
		wantPrefix   string
		wantPathTail string // suffix the resolved Path must end with (when set)
	}{
		{
			name:     "symbol ref on CEL underscore reference",
			lineHint: "expr: size(_.environment)",
			token:    "environment)",
			within:   2,
			wantKind: CursorSymbolRef,
		},
		{
			name:     "symbol ref on condition reference",
			lineHint: `when: _.environment`,
			token:    "environment ==",
			within:   1,
			wantKind: CursorSymbolRef,
		},
		{
			name:         "yaml key on a scalar mapping key",
			lineHint:     "provider: parameter",
			token:        "provider",
			within:       3,
			wantKind:     CursorYAMLKey,
			wantPartial:  "pro",
			wantPathTail: ".provider",
		},
		{
			name:         "yaml key on a block key",
			lineHint:     "      resolve:",
			token:        "resolve",
			within:       2,
			wantKind:     CursorYAMLKey,
			wantPartial:  "re",
			wantPathTail: "resolve",
		},
		{
			name:         "provider name value",
			lineHint:     "provider: parameter",
			token:        "parameter",
			within:       2,
			wantKind:     CursorProviderName,
			wantPartial:  "pa",
			wantPathTail: ".provider",
		},
		{
			name:         "enum value",
			lineHint:     "onError: fail",
			token:        "fail",
			within:       4,
			wantKind:     CursorEnumValue,
			wantPartial:  "fail",
			wantPathTail: ".onError",
		},
		{
			name:         "cel function-name token (not a reference)",
			lineHint:     "expr: size(_.environment)",
			token:        "size",
			within:       3,
			wantKind:     CursorCEL,
			wantPartial:  "siz",
			wantPrefix:   "",
			wantPathTail: ".expr",
		},
		{
			name:         "template function-name token",
			lineHint:     `tmpl: "{{ upper .name }}"`,
			token:        "upper",
			within:       3,
			wantKind:     CursorTemplate,
			wantPartial:  "upp",
			wantPrefix:   "",
			wantPathTail: ".tmpl",
		},
		{
			name:         "template field reference token",
			lineHint:     `tmpl: "{{ upper .name }}"`,
			token:        ".name",
			within:       3,
			wantKind:     CursorTemplate,
			wantPartial:  "na",
			wantPrefix:   ".",
			wantPathTail: ".tmpl",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pos := posAt(t, cursorFixture, tc.lineHint, tc.token, tc.within)
			ctx := resolveFixture(t, cursorFixture, pos)
			assert.Equal(t, tc.wantKind, ctx.Kind, "kind (path=%q partial=%q prefix=%q)", ctx.Path, ctx.PartialToken, ctx.ExprPrefix)
			if tc.wantPartial != "" {
				assert.Equal(t, tc.wantPartial, ctx.PartialToken, "partial token")
			}
			if tc.wantPrefix != "" || tc.wantKind == CursorCEL || tc.wantKind == CursorTemplate {
				assert.Equal(t, tc.wantPrefix, ctx.ExprPrefix, "expr prefix")
			}
			if tc.wantPathTail != "" {
				assert.True(t, strings.HasSuffix(ctx.Path, tc.wantPathTail),
					"path %q should end with %q", ctx.Path, tc.wantPathTail)
			}
			if tc.wantKind == CursorSymbolRef {
				require.NotNil(t, ctx.Ref)
				assert.Equal(t, "environment", ctx.Ref.Symbol.Name)
			}
		})
	}
}

func TestResolveCursor_CELResolverPrefixPartial(t *testing.T) {
	// When the index is unavailable (document did not build), a resolver-prefixed
	// CEL partial still classifies as CEL with the "_." prefix captured for
	// completion -- exercised here by building a DocEntry with a node map but no
	// index. (When the index IS available, a complete "_.name" is a SymbolRef,
	// which is the right context for completing an existing resolver reference.)
	raw := []byte(`apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  resolvers:
    a:
      resolve:
        with:
          - provider: parameter
            inputs:
              value:
                expr: _.envir
`)
	nodes, err := refindex.NodeMap(raw)
	require.NoError(t, err)
	e := &DocEntry{Raw: raw, Nodes: nodes} // Index intentionally nil

	pos := posAt(t, string(raw), "expr: _.envir", "_.envir", len("_.envir"))
	ctx := ResolveCursor(e, pos)
	assert.Equal(t, CursorCEL, ctx.Kind)
	assert.Equal(t, celResolverPrefix, ctx.ExprPrefix)
	assert.Equal(t, "envir", ctx.PartialToken)
}

func TestCelTokenAt(t *testing.T) {
	tests := []struct {
		expr        string
		at          int
		wantPartial string
		wantPrefix  string
	}{
		{"_.env", 5, "env", "_."},
		{"_.", 2, "", "_."},
		{"__actions.foo", 13, "foo", "__actions."},
		{"size(_.x)", 4, "size", ""},
		{"toUpper", 4, "toUp", ""},
	}
	for _, tc := range tests {
		partial, prefix := celTokenAt(tc.expr, tc.at)
		assert.Equal(t, tc.wantPartial, partial, "partial for %q@%d", tc.expr, tc.at)
		assert.Equal(t, tc.wantPrefix, prefix, "prefix for %q@%d", tc.expr, tc.at)
	}
}

func TestTemplateTokenAt(t *testing.T) {
	tests := []struct {
		tmpl        string
		at          int
		wantPartial string
		wantPrefix  string
	}{
		{"{{ ._.app }}", 9, "app", "._."},
		{"{{ .__actions.a }}", 15, "a", ".__actions."},
		{"{{ .name }}", 8, "name", "."},
		{"{{ upper .x }}", 8, "upper", ""},
		{"literal text", 5, "", ""},   // not inside an action
		{"{{ .a }} text", 11, "", ""}, // after a closed action
	}
	for _, tc := range tests {
		partial, prefix := templateTokenAt(tc.tmpl, tc.at)
		assert.Equal(t, tc.wantPartial, partial, "partial for %q@%d", tc.tmpl, tc.at)
		assert.Equal(t, tc.wantPrefix, prefix, "prefix for %q@%d", tc.tmpl, tc.at)
	}
}

func TestResolveCursor_TemplateResolverPrefixPartial(t *testing.T) {
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
                tmpl: "{{ ._.ap"
`
	pos := posAt(t, content, `tmpl: "{{ ._.ap`, "._.ap", len("._.ap"))
	ctx := resolveFixture(t, content, pos)
	assert.Equal(t, CursorTemplate, ctx.Kind)
	assert.Equal(t, tmplResolverPrefix, ctx.ExprPrefix)
	assert.Equal(t, "ap", ctx.PartialToken)
}

func TestResolveCursor_BlockScalarCELBody(t *testing.T) {
	// A cursor inside a multi-line literal (|) CEL body must classify as CEL --
	// yaml positions the value node on the key line, so body lines have no node
	// of their own; the resolver must find the enclosing block scalar.
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
            inputs:
              value:
                expr: |
                  size(_.foo) && bigFunc
`
	pos := posAt(t, content, "size(_.foo) && bigFunc", "bigFunc", 3)
	ctx := resolveFixture(t, content, pos)
	assert.Equal(t, CursorCEL, ctx.Kind, "path=%q partial=%q", ctx.Path, ctx.PartialToken)
	assert.Equal(t, "big", ctx.PartialToken)
	assert.True(t, strings.HasSuffix(ctx.Path, ".expr"))
}

func TestResolveCursor_BlockScalarTemplateBody(t *testing.T) {
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
                tmpl: |
                  Hello {{ upperCase .name }}
`
	pos := posAt(t, content, "Hello {{ upperCase", "upperCase", 5)
	ctx := resolveFixture(t, content, pos)
	assert.Equal(t, CursorTemplate, ctx.Kind, "path=%q partial=%q prefix=%q", ctx.Path, ctx.PartialToken, ctx.ExprPrefix)
	assert.Equal(t, "upper", ctx.PartialToken)
	assert.Equal(t, "", ctx.ExprPrefix)
	assert.True(t, strings.HasSuffix(ctx.Path, ".tmpl"))
}

func TestResolveCursor_FlowMappingSelectsByColumn(t *testing.T) {
	// Two scalars share a line in a flow mapping; the cursor column must select
	// the right one (onError, not provider) despite provider's longer path.
	content := `apiVersion: scafctl.io/v1
kind: Solution
metadata:
  name: c
spec:
  resolvers:
    a:
      resolve:
        with:
          - {provider: parameter, onError: fail}
`
	pos := posAt(t, content, "onError: fail}", "fail", 2)
	ctx := resolveFixture(t, content, pos)
	assert.Equal(t, CursorEnumValue, ctx.Kind, "path=%q", ctx.Path)
	assert.True(t, strings.HasSuffix(ctx.Path, ".onError"), "path %q", ctx.Path)
}

func TestResolveCursor_Whitespace(t *testing.T) {
	// Cursor in leading indentation is unclassifiable.
	pos := posAt(t, cursorFixture, "provider: parameter", "provider", -4)
	ctx := resolveFixture(t, cursorFixture, pos)
	assert.Equal(t, CursorNone, ctx.Kind)
}

func TestResolveCursor_NilDoc(t *testing.T) {
	assert.Equal(t, CursorNone, ResolveCursor(nil, protocol.Position{}).Kind)
}

func TestResolveCursor_OutOfRangePosition(t *testing.T) {
	c := NewDocumentCache()
	e := c.Set("file:///c.yaml", 1, cursorFixture)
	ctx := ResolveCursor(e, protocol.Position{Line: 9999, Character: 0})
	assert.Equal(t, CursorNone, ctx.Kind)
}

func TestResolveCursor_ParseErrorDegradesGracefully(t *testing.T) {
	// Malformed YAML: NodeMap fails, so there is no node map at all. The resolver
	// must still classify from raw text (or return None) without panicking.
	malformed := "spec: {resolvers: \n  provider: parameter"
	c := NewDocumentCache()
	e := c.Set("file:///bad.yaml", 1, malformed)
	require.Error(t, e.ParseErr)

	require.NotPanics(t, func() {
		// On the "provider:" line, text-based classification still works.
		pos := posAt(t, malformed, "provider: parameter", "parameter", 2)
		ctx := ResolveCursor(e, pos)
		assert.Equal(t, CursorProviderName, ctx.Kind)
		assert.Equal(t, "pa", ctx.PartialToken)
	})
}

func TestResolveCursor_PartialSolutionStillClassifies(t *testing.T) {
	// Valid YAML but the solution fails to build (unknown provider reference in
	// CEL). NodeMap succeeds, so classification by leaf key works even though the
	// index is unavailable.
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
            inputs:
              value:
                expr: _.missing.deeply.nested.value.that.is.long
`
	c := NewDocumentCache()
	e := c.Set("file:///c.yaml", 1, content)
	pos := posAt(t, content, "provider: parameter", "parameter", 2)
	ctx := ResolveCursor(e, pos)
	assert.Equal(t, CursorProviderName, ctx.Kind)
}

func TestResolveCursor_SymbolRefWhenIndexUnavailable(t *testing.T) {
	// A guard that classification never dereferences a nil index.
	e := &DocEntry{Raw: []byte("name: value\n")}
	require.NotPanics(t, func() { ResolveCursor(e, protocol.Position{Line: 0, Character: 6}) })
}

// helper visibility check: lastSegment handles indexed and dotted paths.
func TestLastSegment(t *testing.T) {
	assert.Equal(t, "provider", lastSegment("spec.resolvers.a.resolve.with[0].provider"))
	assert.Equal(t, "with", lastSegment("spec.resolvers.a.resolve.with[0]"))
	assert.Equal(t, "spec", lastSegment("spec"))
	assert.Equal(t, "", lastSegment(""))
}

func TestCursorKind_String(t *testing.T) {
	cases := map[CursorKind]string{
		CursorNone:         "none",
		CursorSymbolRef:    "symbolRef",
		CursorYAMLKey:      "yamlKey",
		CursorEnumValue:    "enumValue",
		CursorCEL:          "cel",
		CursorTemplate:     "template",
		CursorProviderName: "providerName",
		CursorKind(99):     "none",
	}
	for k, want := range cases {
		assert.Equal(t, want, k.String())
	}
}

func TestBackwardIdent_Guards(t *testing.T) {
	assert.Equal(t, "", backwardIdent("abc", -1))
	assert.Equal(t, "abc", backwardIdent("abc", 99))
	assert.Equal(t, "", backwardDotIdent("abc", -1))
	assert.Equal(t, "a.b", backwardDotIdent("a.b", 99))
}
