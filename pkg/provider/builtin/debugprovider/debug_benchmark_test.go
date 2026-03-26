// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package debugprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkDebugProvider_Execute(b *testing.B) {
	p := NewDebugProvider()

	b.Run("inspect_simple", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"name":  "test",
			"value": 42,
		})
		inputs := map[string]any{
			"operation": "inspect",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("inspect_nested", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"config": map[string]any{
				"server": map[string]any{
					"host": "localhost",
					"port": 8080,
				},
				"database": map[string]any{
					"host": "db.example.com",
					"port": 5432,
				},
			},
		})
		inputs := map[string]any{
			"operation": "inspect",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}
