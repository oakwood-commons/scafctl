// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package directoryprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkDirectoryProvider_Execute_List(b *testing.B) {
	p := NewDirectoryProvider()

	tmpDir := b.TempDir()
	for i := 0; i < 20; i++ {
		name := filepath.Join(tmpDir, fmt.Sprintf("file-%02d.txt", i))
		if err := os.WriteFile(name, []byte("content"), 0o600); err != nil {
			b.Fatal(err)
		}
	}

	ctx := provider.WithWorkingDirectory(context.Background(), tmpDir)
	inputs := map[string]any{
		"operation": "list",
		"path":      tmpDir,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}

func BenchmarkDirectoryProvider_Execute_DryRun(b *testing.B) {
	p := NewDirectoryProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithWorkingDirectory(ctx, b.TempDir())
	inputs := map[string]any{
		"operation": "create",
		"path":      "new-dir",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
