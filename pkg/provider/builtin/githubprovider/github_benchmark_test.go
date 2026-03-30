// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package githubprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkGitHubProvider_Execute_DryRun(b *testing.B) {
	p := NewGitHubProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation": "get_repo",
		"owner":     "example-org",
		"repo":      "example-repo",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
