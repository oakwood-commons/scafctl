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
			"key": "env",
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
			"key":     "missing",
			"default": "fallback-value",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("keys_map", func(b *testing.B) {
		ctx := provider.WithParameters(context.Background(), map[string]any{
			"env":    "production",
			"region": "us-east-1",
			"tier":   "gold",
		})
		inputs := map[string]any{
			"keys": []any{"env", "region", "missing"},
			"as":   "map",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("all_map", func(b *testing.B) {
		ctx := provider.WithParameters(context.Background(), map[string]any{
			"env":    "production",
			"region": "us-east-1",
			"tier":   "gold",
		})
		inputs := map[string]any{
			"all": true,
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}

func BenchmarkParameterProvider_Execute_TypedCoercion(b *testing.B) {
	p := NewParameterProvider()

	cases := []struct {
		name   string
		value  any
		typeIn string
	}{
		{"auto_int", "8080", TypeAuto},
		{"auto_url_literal", "https://example.com/config", TypeAuto},
		{"int", "8080", TypeInt},
		{"float", "3.14", TypeFloat},
		{"bool", "true", TypeBool},
		{"json", `{"a":1,"b":[2,3]}`, TypeJSON},
		{"csv", "us-east-1,us-west-2,eu-west-1", TypeCSV},
		{"string", "00042", TypeString},
		{"raw", "00042", TypeRaw},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			ctx := provider.WithParameters(context.Background(), map[string]any{
				"val": tc.value,
			})
			inputs := map[string]any{
				"key":  "val",
				"type": tc.typeIn,
			}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _ = p.Execute(ctx, inputs)
			}
		})
	}
}

// BenchmarkParameterProvider_Execute_DeclaredType measures the auto path when
// the enclosing resolver declares a scalar output type, which triggers the
// declared-type context lookup and re-dispatch in resolveValue (and the
// whole-number-float fallback in coerceInt for the int case).
func BenchmarkParameterProvider_Execute_DeclaredType(b *testing.B) {
	p := NewParameterProvider()

	cases := []struct {
		name     string
		value    any
		declared string
	}{
		{"declared_string", "2.0", "string"},
		{"declared_int_whole_float", "2.0", "int"},
		{"declared_int_fast", "8080", "int"},
		{"declared_float", "3.14", "float"},
		{"declared_none_auto", "8080", ""},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			ctx := provider.WithParameters(context.Background(), map[string]any{
				"val": tc.value,
			})
			ctx = provider.WithDeclaredScalarType(ctx, tc.declared)
			inputs := map[string]any{"key": "val"}

			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_, _ = p.Execute(ctx, inputs)
			}
		})
	}
}
