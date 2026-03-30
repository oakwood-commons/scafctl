// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkHCLProvider_Execute_DryRun(b *testing.B) {
	p := NewHCLProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithResolverContext(ctx, map[string]any{})
	inputs := map[string]any{
		"template": `variable "name" { default = "{{.name}}" }`,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
