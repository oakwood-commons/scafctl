// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/flags/resolve"
)

func BenchmarkResolveValue_JSON(b *testing.B) {
	ctx := context.Background()

	b.Run("simple object", func(b *testing.B) {
		value := `json://{"key":"value"}`
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = resolve.ResolveValue(ctx, "test", value)
		}
	})

	b.Run("complex object", func(b *testing.B) {
		value := `json://{"db":"postgres","port":5432,"config":{"timeout":30,"retries":3}}`
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = resolve.ResolveValue(ctx, "test", value)
		}
	})
}

func BenchmarkResolveValue_YAML(b *testing.B) {
	ctx := context.Background()

	b.Run("simple", func(b *testing.B) {
		value := `yaml://key: value`
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = resolve.ResolveValue(ctx, "test", value)
		}
	})
}

func BenchmarkResolveValue_Base64(b *testing.B) {
	ctx := context.Background()

	b.Run("small", func(b *testing.B) {
		value := `base64://SGVsbG8=`
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = resolve.ResolveValue(ctx, "test", value)
		}
	})
}

func BenchmarkResolveValue_File(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()
	smallFile := filepath.Join(tmpDir, "small.txt")
	_ = os.WriteFile(smallFile, []byte("Hello, World!"), 0o600)

	b.Run("small file", func(b *testing.B) {
		value := "file://" + smallFile
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = resolve.ResolveValue(ctx, "test", value)
		}
	})
}

func BenchmarkResolveValue_Plain(b *testing.B) {
	ctx := context.Background()

	b.Run("simple string", func(b *testing.B) {
		value := "plain value"
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = resolve.ResolveValue(ctx, "test", value)
		}
	})
}

func BenchmarkResolveAll(b *testing.B) {
	ctx := context.Background()

	b.Run("small batch", func(b *testing.B) {
		input := map[string][]string{
			"config": {`json://{"key":"value"}`},
			"env":    {"production"},
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = resolve.ResolveAll(ctx, input)
		}
	})

	b.Run("medium batch", func(b *testing.B) {
		input := map[string][]string{
			"config": {`json://{"db":"postgres","port":5432}`},
			"data":   {`yaml://items: [a, b, c]`},
			"token":  {`base64://SGVsbG8sIFdvcmxkIQ==`},
			"env":    {"production"},
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = resolve.ResolveAll(ctx, input)
		}
	})
}

func BenchmarkSchemeComparison(b *testing.B) {
	ctx := context.Background()
	tmpDir := b.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.txt")
	_ = os.WriteFile(tmpFile, []byte("test content"), 0o600)

	schemes := map[string]string{
		"json":   `json://{"key":"value"}`,
		"yaml":   `yaml://key: value`,
		"base64": `base64://SGVsbG8=`,
		"file":   "file://" + tmpFile,
		"plain":  "plain value",
	}

	for name, value := range schemes {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = resolve.ResolveValue(ctx, "test", value)
			}
		})
	}
}
