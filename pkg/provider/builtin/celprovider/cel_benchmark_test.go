// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkCelProvider_Execute(b *testing.B) {
	p := NewCelProvider()

	b.Run("simple_expression", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"name": "hello",
		})
		inputs := map[string]any{
			"expression": "_.name",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("string_transform", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"input": "hello world",
		})
		inputs := map[string]any{
			"expression": "_.input.upperAscii()",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("conditional", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"environment": "prod",
		})
		inputs := map[string]any{
			"expression": "_.environment == 'prod' ? 'production' : 'non-production'",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("with_variables", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"name": "world",
		})
		inputs := map[string]any{
			"expression": "prefix + _.name + suffix",
			"variables": map[string]any{
				"prefix": "Hello, ",
				"suffix": "!",
			},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("arithmetic", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"a": 10,
			"b": 20,
		})
		inputs := map[string]any{
			"expression": "string(_.a + _.b * 2)",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}

func BenchmarkCelProvider_Execute_DryRun(b *testing.B) {
	p := NewCelProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithResolverContext(ctx, map[string]any{})

	inputs := map[string]any{
		"expression": "_.name.upperAscii()",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
