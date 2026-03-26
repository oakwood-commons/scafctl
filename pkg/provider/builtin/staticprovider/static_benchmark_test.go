// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package staticprovider

import (
	"context"
	"testing"
)

func BenchmarkStaticProvider_Execute(b *testing.B) {
	p := New()

	b.Run("string_value", func(b *testing.B) {
		ctx := context.Background()
		inputs := map[string]any{
			"value": "hello-world",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("map_value", func(b *testing.B) {
		ctx := context.Background()
		inputs := map[string]any{
			"value": map[string]any{
				"host": "localhost",
				"port": 8080,
				"ssl":  true,
			},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("list_value", func(b *testing.B) {
		ctx := context.Background()
		inputs := map[string]any{
			"value": []any{"dev", "staging", "production"},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}
