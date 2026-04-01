// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package yaml

import (
	"fmt"
	"strings"
	"testing"
)

func BenchmarkToYaml(b *testing.B) {
	b.Run("simple_map", func(b *testing.B) {
		input := map[string]any{"name": "myapp", "port": 8080}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ToYaml(input)
		}
	})

	b.Run("nested_map", func(b *testing.B) {
		input := map[string]any{
			"server": map[string]any{
				"host": "localhost",
				"port": 443,
				"tls": map[string]any{
					"enabled": true,
					"cert":    "/etc/ssl/cert.pem",
				},
			},
			"database": map[string]any{
				"host": "db.example.com",
				"port": 5432,
				"name": "mydb",
			},
		}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ToYaml(input)
		}
	})

	b.Run("list_of_maps", func(b *testing.B) {
		items := make([]map[string]any, 20)
		for i := range items {
			items[i] = map[string]any{
				"name":  fmt.Sprintf("item-%d", i),
				"value": i,
			}
		}
		input := map[string]any{"items": items}
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = ToYaml(input)
		}
	})
}

func BenchmarkFromYaml(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		input := "name: myapp\nport: 8080\n"
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = FromYaml(input)
		}
	})

	b.Run("nested", func(b *testing.B) {
		input := "server:\n  host: localhost\n  port: 443\n  tls:\n    enabled: true\n    cert: /etc/ssl/cert.pem\ndatabase:\n  host: db.example.com\n  port: 5432\n"
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = FromYaml(input)
		}
	})

	b.Run("large", func(b *testing.B) {
		var sb strings.Builder
		sb.WriteString("items:\n")
		for i := 0; i < 50; i++ {
			fmt.Fprintf(&sb, "  - name: item-%d\n    value: %d\n", i, i)
		}
		input := sb.String()
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			_, _ = FromYaml(input)
		}
	})
}

func BenchmarkToYamlFromYaml_RoundTrip(b *testing.B) {
	input := map[string]any{
		"name": "myapp",
		"config": map[string]any{
			"env":      "production",
			"replicas": 3,
		},
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		yamlStr, _ := ToYaml(input)
		_, _ = FromYaml(yamlStr)
	}
}
