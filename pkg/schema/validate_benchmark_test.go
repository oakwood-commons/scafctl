// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import "testing"

func BenchmarkValidateDataAgainstSchema(b *testing.B) {
	jsonSchema := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"count": {"type": "integer", "minimum": 1}
		},
		"required": ["name"],
		"additionalProperties": false
	}`)
	data := map[string]any{"name": "hi", "count": 3}

	b.ReportAllocs()
	for b.Loop() {
		if _, err := ValidateDataAgainstSchema(jsonSchema, data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkValidateComposePartAgainstSchema(b *testing.B) {
	data := map[string]any{
		"spec": map[string]any{
			"testing": map[string]any{
				"cases": map[string]any{
					"smoke": map[string]any{
						"description": "smoke test",
						"command":     []any{"render", "solution"},
						"assertions":  []any{map[string]any{"contains": "ok"}},
					},
				},
			},
		},
	}

	if _, err := GenerateComposePartSchema(); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := ValidateComposePartAgainstSchema(data); err != nil {
			b.Fatal(err)
		}
	}
}
