// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refs

import (
	"context"
	"testing"
)

func BenchmarkExtractFromTemplate(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		content := `Hello {{.appName}}, welcome to {{.environment}}`
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ExtractFromTemplate(content, "{{", "}}")
		}
	})

	b.Run("multiple_refs", func(b *testing.B) {
		content := `{{.config}}/{{.appName}}/{{.environment}}/{{.region}}/{{.cluster}}`
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ExtractFromTemplate(content, "{{", "}}")
		}
	})

	b.Run("complex_template", func(b *testing.B) {
		content := `{{if .enabled}}{{.appName}} runs on {{.host}}:{{.port}}{{else}}disabled{{end}}`
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ExtractFromTemplate(content, "{{", "}}")
		}
	})
}

func BenchmarkExtractFromCEL(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ExtractFromCEL(context.Background(), "_.appName")
		}
	})

	b.Run("complex", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ExtractFromCEL(context.Background(), "_.config.host + ':' + string(_.config.port)")
		}
	})
}

func BenchmarkExtractResolverName(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ExtractResolverName("appName")
		}
	})

	b.Run("dotted_path", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			ExtractResolverName("config.database.host")
		}
	})
}
