// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package execprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkExecProvider_Execute_DryRun(b *testing.B) {
	p := NewExecProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"command": "echo hello",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
