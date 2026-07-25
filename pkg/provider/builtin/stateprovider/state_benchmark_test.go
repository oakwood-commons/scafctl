// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package stateprovider

import (
	"context"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/state"
)

func BenchmarkStateProvider_Execute(b *testing.B) {
	p := New()
	sd := state.NewData()
	sd.Resolvers["db_password"] = &state.PersistedEntry{Value: "secret", Type: "string", CreatedAt: time.Now().UTC()}
	ctx := state.WithState(context.Background(), sd)

	b.Run("hit", func(b *testing.B) {
		inputs := map[string]any{"operation": OperationGet, "key": "db_password"}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("miss_with_default", func(b *testing.B) {
		inputs := map[string]any{"key": "absent", "default": ""}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("keys_map", func(b *testing.B) {
		inputs := map[string]any{"keys": []any{"db_password", "absent"}}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("all_map", func(b *testing.B) {
		inputs := map[string]any{"all": true}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}
