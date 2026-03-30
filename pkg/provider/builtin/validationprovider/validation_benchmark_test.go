// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validationprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkValidationProvider_Execute(b *testing.B) {
	p := NewValidationProvider()

	b.Run("match_pattern", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{})
		inputs := map[string]any{
			"value": "hello-world-123",
			"match": `^[a-z0-9-]+$`,
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("notMatch_pattern", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{})
		inputs := map[string]any{
			"value":    "hello-world",
			"notMatch": `[A-Z]`,
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("expression", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{})
		inputs := map[string]any{
			"value":      42,
			"expression": "value > 0 && value < 100",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}
