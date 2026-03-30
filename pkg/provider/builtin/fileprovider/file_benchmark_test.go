// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkFileProvider_Execute_Read(b *testing.B) {
	p := NewFileProvider()

	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "bench-test.txt")
	if err := os.WriteFile(testFile, []byte("benchmark content"), 0o600); err != nil {
		b.Fatal(err)
	}

	ctx := provider.WithWorkingDirectory(context.Background(), tmpDir)
	inputs := map[string]any{
		"operation": "read",
		"path":      testFile,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}

func BenchmarkFileProvider_Execute_Exists(b *testing.B) {
	p := NewFileProvider()

	tmpDir := b.TempDir()
	testFile := filepath.Join(tmpDir, "bench-exists.txt")
	if err := os.WriteFile(testFile, []byte("exists"), 0o600); err != nil {
		b.Fatal(err)
	}

	ctx := provider.WithWorkingDirectory(context.Background(), tmpDir)
	inputs := map[string]any{
		"operation": "exists",
		"path":      testFile,
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}

func BenchmarkFileProvider_Execute_DryRun(b *testing.B) {
	p := NewFileProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithWorkingDirectory(ctx, b.TempDir())
	inputs := map[string]any{
		"operation": "write",
		"path":      "output.txt",
		"content":   "hello world",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}
