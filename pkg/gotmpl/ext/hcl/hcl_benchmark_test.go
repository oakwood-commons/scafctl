// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hcl

import (
	"fmt"
	"testing"
)

func BenchmarkToHcl(b *testing.B) {
	b.Run("simple_map", func(b *testing.B) {
		input := map[string]any{"key": "value", "port": 8080}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ToHcl(input)
		}
	})

	b.Run("nested_map", func(b *testing.B) {
		input := map[string]any{
			"server": map[string]any{
				"host": "localhost",
				"port": 443,
				"tls": map[string]any{
					"enabled": true,
					"cert":    "/etc/ssl/cert.pem",
				},
			},
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ToHcl(input)
		}
	})

	b.Run("with_list", func(b *testing.B) {
		input := map[string]any{
			"tags": []any{"web", "production", "v2"},
			"name": "myapp",
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ToHcl(input)
		}
	})

	b.Run("large_map", func(b *testing.B) {
		input := make(map[string]any, 20)
		for i := 0; i < 20; i++ {
			input[fmt.Sprintf("key%d", i)] = fmt.Sprintf("value%d", i)
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ToHcl(input)
		}
	})
}
