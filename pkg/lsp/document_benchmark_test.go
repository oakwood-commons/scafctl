// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lsp

import "testing"

// BenchmarkDocumentCache_Set measures the one-time parse+index cost paid once
// per document version.
func BenchmarkDocumentCache_Set(b *testing.B) {
	c := NewDocumentCache()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		//nolint:gosec // benchmark counter is small and non-negative
		c.Set(cacheURI, int32(i), validCacheSolution)
	}
}

// BenchmarkDocumentCache_Get proves that reading a cached document at an
// unchanged version reuses the parse: it does no UnmarshalFromBytes / refindex
// Build work, so it is dramatically cheaper than Set. This is the win that lets
// several features fire on one keystroke and share a single parse.
func BenchmarkDocumentCache_Get(b *testing.B) {
	c := NewDocumentCache()
	c.Set(cacheURI, 1, validCacheSolution)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		entry, ok := c.Get(cacheURI)
		if !ok || entry.Index == nil {
			b.Fatal("expected a cached, parsed entry")
		}
	}
}
