// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package collections

import (
	"fmt"
	"testing"
)

func buildTestItems(n int) []any {
	items := make([]any, n)
	for i := 0; i < n; i++ {
		status := "active"
		if i%3 == 0 {
			status = "inactive"
		}
		items[i] = map[string]any{
			"name":   fmt.Sprintf("item-%d", i),
			"status": status,
			"value":  i,
		}
	}
	return items
}

func BenchmarkWhere(b *testing.B) {
	b.Run("10items", func(b *testing.B) {
		items := buildTestItems(10)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = Where("status", "active", items)
		}
	})

	b.Run("100items", func(b *testing.B) {
		items := buildTestItems(100)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = Where("status", "active", items)
		}
	})

	b.Run("1000items", func(b *testing.B) {
		items := buildTestItems(1000)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = Where("status", "active", items)
		}
	})
}

func BenchmarkSelectField(b *testing.B) {
	b.Run("10items", func(b *testing.B) {
		items := buildTestItems(10)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = SelectField("name", items)
		}
	})

	b.Run("100items", func(b *testing.B) {
		items := buildTestItems(100)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = SelectField("name", items)
		}
	})

	b.Run("1000items", func(b *testing.B) {
		items := buildTestItems(1000)
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = SelectField("name", items)
		}
	})
}
