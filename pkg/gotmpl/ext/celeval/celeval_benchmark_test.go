// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celeval

import (
	"testing"
)

func BenchmarkCel(b *testing.B) {
	b.Run("simple_string", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = Cel("'hello world'", nil)
		}
	})

	b.Run("arithmetic", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = Cel("2 + 3 * 4", nil)
		}
	})

	b.Run("data_access", func(b *testing.B) {
		data := map[string]any{
			"name":  "test",
			"count": 42,
			"items": []any{"a", "b", "c"},
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = Cel("_.name + ' has ' + string(_.count) + ' items'", data)
		}
	})

	b.Run("conditional", func(b *testing.B) {
		data := map[string]any{"count": 15}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = Cel("_.count > 10 ? 'many' : 'few'", data)
		}
	})

	b.Run("list_filter", func(b *testing.B) {
		data := map[string]any{
			"items": []any{
				map[string]any{"name": "a", "active": true},
				map[string]any{"name": "b", "active": false},
				map[string]any{"name": "c", "active": true},
			},
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = Cel("_.items.filter(x, x.active)", data)
		}
	})
}
