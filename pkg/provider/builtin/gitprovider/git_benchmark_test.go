// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkGitProvider_Execute_DryRun(b *testing.B) {
	p := NewGitProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation":  "clone",
		"repository": "https://github.com/example/repo.git",
		"path":       "/tmp/repo",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
