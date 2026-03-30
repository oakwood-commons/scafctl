// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package envprovider

import (
	"context"
	"testing"
)

func BenchmarkEnvProvider_Execute_Get(b *testing.B) {
	p := NewEnvProvider()
	ctx := context.Background()

	b.Setenv("BENCH_TEST_VAR", "benchmark-value")

	inputs := map[string]any{
		"operation": "get",
		"name":      "BENCH_TEST_VAR",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}

func BenchmarkEnvProvider_Execute_List(b *testing.B) {
	p := NewEnvProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "list",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
