// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import (
	"fmt"
	"strings"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"
)

// BenchmarkResolveCursor measures the hot path: ResolveCursor runs per keystroke
// via hover/completion/signature-help, so it must stay cheap.
func BenchmarkResolveCursor(b *testing.B) {
	c := NewDocumentCache()
	e := c.Set("file:///bench.yaml", 1, cursorFixture)
	// A CEL value position (representative: node lookup + token scan).
	pos := protocol.Position{Line: 18, Character: 30}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ResolveCursor(e, pos)
	}
}

// BenchmarkResolveCursor_SymbolRef measures the located-reference fast path.
func BenchmarkResolveCursor_SymbolRef(b *testing.B) {
	c := NewDocumentCache()
	e := c.Set("file:///bench.yaml", 1, cursorFixture)
	pos := protocol.Position{Line: 19, Character: 16} // on _.environment in when:

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ResolveCursor(e, pos)
	}
}

// BenchmarkResolveCursor_LargeDoc measures the hot path on a larger document so
// a regression in the per-keystroke line lookup or node-map scan is visible
// (the small fixtures hide O(document-size) costs).
func BenchmarkResolveCursor_LargeDoc(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("apiVersion: scafctl.io/v1\nkind: Solution\nmetadata:\n  name: big\nspec:\n  resolvers:\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "    r%d:\n      resolve:\n        with:\n          - provider: parameter\n            inputs:\n              value:\n                expr: _.environment\n", i)
	}
	content := sb.String()
	c := NewDocumentCache()
	e := c.Set("file:///big.yaml", 1, content)
	// A CEL position deep in the document.
	line := uint32(strings.Count(content, "\n") - 1)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ResolveCursor(e, protocol.Position{Line: line, Character: 24})
	}
}
