// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmpl

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"text/template"
)

// ── Template samples of increasing complexity ───────────────────────────

var benchTemplates = map[string]string{
	"simple": "Hello, {{.Name}}!",

	"medium": `{{- range .Items -}}
- {{.Name}}: {{.Value}}
{{end -}}
Total items: {{len .Items}}`,

	"complex": `{{- $title := .Title -}}
# {{$title}}

{{range $i, $section := .Sections -}}
## {{$section.Heading}}

{{range $section.Paragraphs -}}
{{.}}

{{end -}}
{{if $section.Items -}}
{{range $section.Items -}}
- {{.}}
{{end -}}
{{end -}}
{{end -}}

---
Generated for {{.Author}} ({{len .Sections}} sections)`,

	"nested_logic": `{{- if .Enabled -}}
{{range $k, $v := .Config -}}
{{$k}} = {{if eq (printf "%T" $v) "string"}}"{{$v}}"{{else}}{{$v}}{{end}}
{{end -}}
{{if .Extras -}}
extras:
{{range .Extras -}}
  - {{.}}
{{end -}}
{{end -}}
{{else -}}
# disabled
{{end -}}`,
}

var benchData = map[string]any{
	"simple": map[string]any{"Name": "World"},

	"medium": map[string]any{
		"Items": []map[string]any{
			{"Name": "alpha", "Value": 1},
			{"Name": "beta", "Value": 2},
			{"Name": "gamma", "Value": 3},
			{"Name": "delta", "Value": 4},
			{"Name": "epsilon", "Value": 5},
		},
	},

	"complex": map[string]any{
		"Title":  "Benchmark Report",
		"Author": "scafctl",
		"Sections": []map[string]any{
			{
				"Heading":    "Introduction",
				"Paragraphs": []string{"This is the first paragraph.", "This is the second."},
				"Items":      []string{"item-a", "item-b"},
			},
			{
				"Heading":    "Details",
				"Paragraphs": []string{"Technical details here."},
				"Items":      nil,
			},
			{
				"Heading":    "Conclusion",
				"Paragraphs": []string{"Summary of findings.", "Final notes."},
				"Items":      []string{"action-1", "action-2", "action-3"},
			},
		},
	},

	"nested_logic": map[string]any{
		"Enabled": true,
		"Config": map[string]any{
			"host":    "localhost",
			"port":    8080,
			"debug":   true,
			"timeout": "30s",
		},
		"Extras": []string{"plugin-a", "plugin-b"},
	},
}

// ── Benchmark: No-cache baseline (parse every time) ────────────────────

func BenchmarkTemplateParseNoCache(b *testing.B) {
	for name, content := range benchTemplates {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				tmpl := template.New("bench")
				_, err := tmpl.Parse(content)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// ── Benchmark: Cached path (parse once, clone on hit) ──────────────────

func BenchmarkTemplateCacheHit(b *testing.B) {
	for name, content := range benchTemplates {
		b.Run(name, func(b *testing.B) {
			cache := NewTemplateCache(100)

			// Pre-populate the cache
			tmpl, err := template.New("bench").Parse(content)
			if err != nil {
				b.Fatal(err)
			}
			key := generateTemplateCacheKey(content, "{{", "}}", MissingKeyDefault, nil)
			cache.Put(key, tmpl, name)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				got, ok := cache.Get(key)
				if !ok {
					b.Fatal("expected cache hit")
				}
				_ = got
			}
		})
	}
}

// ── Benchmark: Full Execute path (cached vs uncached) ──────────────────

func BenchmarkServiceExecuteNoCache(b *testing.B) {
	for name, content := range benchTemplates {
		b.Run(name, func(b *testing.B) {
			// Use a nil-cache service that forces parse every time
			svc := &noCacheService{svc: NewServiceRaw(nil)}
			ctx := context.Background()
			data := benchData[name]

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				err := svc.execute(ctx, content, name, data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkServiceExecuteCached(b *testing.B) {
	for name, content := range benchTemplates {
		b.Run(name, func(b *testing.B) {
			cache := NewTemplateCache(100)
			svc := NewServiceWithCache(nil, cache)
			ctx := context.Background()
			data := benchData[name]

			// Warm the cache with one call
			_, err := svc.Execute(ctx, TemplateOptions{
				Content: content,
				Name:    name,
				Data:    data,
			})
			if err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := svc.Execute(ctx, TemplateOptions{
					Content: content,
					Name:    name,
					Data:    data,
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// ── Benchmark: Cache key generation overhead ───────────────────────────

func BenchmarkCacheKeyGeneration(b *testing.B) {
	funcKeys := []string{"upper", "lower", "title", "replace", "trim", "contains"}

	b.Run("no_funcs", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = generateTemplateCacheKey("Hello {{.Name}}", "{{", "}}", MissingKeyDefault, nil)
		}
	})

	b.Run("with_funcs", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = generateTemplateCacheKey("Hello {{.Name}}", "{{", "}}", MissingKeyDefault, funcKeys)
		}
	})

	b.Run("complex_template", func(b *testing.B) {
		b.ReportAllocs()
		content := benchTemplates["complex"]
		for i := 0; i < b.N; i++ {
			_ = generateTemplateCacheKey(content, "{{", "}}", MissingKeyDefault, funcKeys)
		}
	})
}

// ── Benchmark: Repeated same-template execution (realistic workload) ───

func BenchmarkRepeatedExecutionWorkload(b *testing.B) {
	// Simulates a resolver/scaffold run: the same template rendered N times
	// with different data (e.g., different parameters per environment).
	content := benchTemplates["complex"]
	dataSlice := make([]any, 20)
	for i := range dataSlice {
		dataSlice[i] = map[string]any{
			"Title":  fmt.Sprintf("Report %d", i),
			"Author": fmt.Sprintf("user-%d", i),
			"Sections": []map[string]any{
				{
					"Heading":    fmt.Sprintf("Section %d", i),
					"Paragraphs": []string{fmt.Sprintf("Content for %d.", i)},
					"Items":      []string{fmt.Sprintf("item-%d-a", i), fmt.Sprintf("item-%d-b", i)},
				},
			},
		}
	}

	b.Run("uncached", func(b *testing.B) {
		svc := &noCacheService{svc: NewServiceRaw(nil)}
		ctx := context.Background()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, d := range dataSlice {
				err := svc.execute(ctx, content, "bench", d)
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})

	b.Run("cached", func(b *testing.B) {
		cache := NewTemplateCache(100)
		svc := NewServiceWithCache(nil, cache)
		ctx := context.Background()

		// Warm
		_, _ = svc.Execute(ctx, TemplateOptions{Content: content, Name: "bench", Data: dataSlice[0]})

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, d := range dataSlice {
				_, err := svc.Execute(ctx, TemplateOptions{
					Content: content,
					Name:    "bench",
					Data:    d,
				})
				if err != nil {
					b.Fatal(err)
				}
			}
		}
	})
}

// ── noCacheService wraps Service but parses every time (pre-cache baseline) ─

type noCacheService struct {
	svc *Service
}

func (n *noCacheService) execute(_ context.Context, content, name string, data any) error {
	// Directly parse + execute without any cache, simulating pre-cache behaviour
	tmpl := template.New(name)
	tmpl = tmpl.Delims(DefaultLeftDelim, DefaultRightDelim)
	tmpl = tmpl.Option("missingkey=default")

	funcMap := make(template.FuncMap)
	for k, v := range n.svc.defaultFuncs {
		funcMap[k] = v
	}
	if len(funcMap) > 0 {
		tmpl = tmpl.Funcs(funcMap)
	}

	parsed, err := tmpl.Parse(content)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := parsed.Execute(&buf, data); err != nil {
		return err
	}
	return nil
}

// ── Benchmark: Parallel cache access ───────────────────────────────────

func BenchmarkTemplateCacheParallelHit(b *testing.B) {
	cache := NewTemplateCache(100)
	content := benchTemplates["complex"]
	tmpl, _ := template.New("bench").Parse(content)
	key := generateTemplateCacheKey(content, "{{", "}}", MissingKeyDefault, nil)
	cache.Put(key, tmpl, "bench")

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			got, ok := cache.Get(key)
			if !ok {
				b.Fatal("expected cache hit")
			}
			_ = got
		}
	})
}

// ── Benchmark: Many unique templates (cache miss path + eviction) ──────

func BenchmarkTemplateCacheMissEviction(b *testing.B) {
	cache := NewTemplateCache(100) // Small cache to trigger evictions

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := fmt.Sprintf("Template variant %d: {{.Name}}", i)
		key := generateTemplateCacheKey(content, "{{", "}}", MissingKeyDefault, nil)
		tmpl, _ := template.New("bench").Parse(content)
		cache.Put(key, tmpl, "bench")
	}
}
