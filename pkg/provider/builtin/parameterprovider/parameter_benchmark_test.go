// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package parameterprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkParameterProvider_Execute(b *testing.B) {
	p := NewParameterProvider()

	b.Run("simple_get", func(b *testing.B) {
		ctx := provider.WithParameters(context.Background(), map[string]any{
			"env":    "production",
			"region": "us-east-1",
		})
		inputs := map[string]any{
			"name": "env",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("with_default", func(b *testing.B) {
		ctx := provider.WithParameters(context.Background(), map[string]any{})
		inputs := map[string]any{
			"name":    "missing",
			"default": "fallback-value",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}
