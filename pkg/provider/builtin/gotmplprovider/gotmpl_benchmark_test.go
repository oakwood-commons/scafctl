// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplprovider

import (
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
)

func BenchmarkGoTemplateProvider_Execute(b *testing.B) {
	p := NewGoTemplateProvider()

	b.Run("simple_template", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"name": "world",
		})
		inputs := map[string]any{
			"template": "Hello, {{.name}}!",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("template_with_range", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"items": []any{"alpha", "beta", "gamma"},
		})
		inputs := map[string]any{
			"template": "{{range .items}}{{.}}\n{{end}}",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("template_with_conditionals", func(b *testing.B) {
		ctx := provider.WithResolverContext(context.Background(), map[string]any{
			"enabled": true,
			"name":    "test",
		})
		inputs := map[string]any{
			"template": "{{if .enabled}}Feature {{.name}} is ON{{else}}Feature {{.name}} is OFF{{end}}",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}

func BenchmarkGoTemplateProvider_Execute_DryRun(b *testing.B) {
	p := NewGoTemplateProvider()
	ctx := provider.WithDryRun(context.Background(), true)
	ctx = provider.WithResolverContext(ctx, map[string]any{})

	inputs := map[string]any{
		"template": "Hello, {{.name}}!",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = p.Execute(ctx, inputs)
	}
}

func BenchmarkGoTemplateProvider_RenderTree(b *testing.B) {
	p := NewGoTemplateProvider()
	ctx := provider.WithResolverContext(context.Background(), map[string]any{})

	b.Run("shared_data_only", func(b *testing.B) {
		inputs := map[string]any{
			"operation": "render-tree",
			"name":      "shared-data-tree",
			"entries": []any{
				map[string]any{
					"path":    "envs/dev/backend.tf",
					"content": "app = {{ .platformAppName }}",
				},
				map[string]any{
					"path":    "envs/staging/backend.tf",
					"content": "app = {{ .platformAppName }}",
				},
				map[string]any{
					"path":    "envs/prod/backend.tf",
					"content": "app = {{ .platformAppName }}",
				},
			},
			"data": map[string]any{
				"platformAppName": "my-app",
			},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("per_entry_data_fanout", func(b *testing.B) {
		inputs := map[string]any{
			"operation": "render-tree",
			"name":      "fanout-tree",
			"entries": []any{
				map[string]any{
					"path":    "envs/dev/backend.tf",
					"content": "app = {{ .platformAppName }}\nenv = {{ .environment }}",
					"data":    map[string]any{"environment": "dev"},
				},
				map[string]any{
					"path":    "envs/staging/backend.tf",
					"content": "app = {{ .platformAppName }}\nenv = {{ .environment }}",
					"data":    map[string]any{"environment": "staging"},
				},
				map[string]any{
					"path":    "envs/prod/backend.tf",
					"content": "app = {{ .platformAppName }}\nenv = {{ .environment }}",
					"data":    map[string]any{"platformAppName": "prod-app", "environment": "prod"},
				},
			},
			"data": map[string]any{
				"platformAppName": "my-app",
			},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})

	b.Run("mixed_raw_and_rendered", func(b *testing.B) {
		inputs := map[string]any{
			"operation": "render-tree",
			"name":      "mixed-tree",
			"rawGlobs":  []any{"**/*.raw"},
			"entries": []any{
				map[string]any{
					"path":    "envs/dev/backend.tf",
					"content": "app = {{ .platformAppName }}",
				},
				map[string]any{
					"path":    "fixtures/verbatim.raw",
					"content": "literal {{ not_parsed }} content",
				},
				map[string]any{
					"path":    "envs/prod/backend.tf",
					"content": "app = {{ .platformAppName }}",
				},
			},
			"data": map[string]any{
				"platformAppName": "my-app",
			},
		}

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = p.Execute(ctx, inputs)
		}
	})
}
