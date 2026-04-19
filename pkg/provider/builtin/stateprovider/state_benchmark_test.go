// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package stateprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/state"
)

func BenchmarkExecute_Read(b *testing.B) {
	stateData := state.NewMockData("bench", "1.0.0", map[string]*state.Entry{
		"key1": {Value: "val1", Type: "string"},
	})
	ctx := state.WithState(context.Background(), stateData)
	p := New()
	input := map[string]any{
		"key": "key1",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, input)
	}
}

func BenchmarkExecute_Read_Miss(b *testing.B) {
	stateData := state.NewMockData("bench", "1.0.0", nil)
	ctx := state.WithState(context.Background(), stateData)
	p := New()
	input := map[string]any{
		"key":      "missing",
		"fallback": "default",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, input)
	}
}

func BenchmarkExecute_Write(b *testing.B) {
	stateData := state.NewMockData("bench", "1.0.0", nil)
	ctx := state.WithState(context.Background(), stateData)
	p := New()
	input := map[string]any{
		"key":   "key1",
		"value": "val1",
		"type":  "string",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, input)
	}
}
