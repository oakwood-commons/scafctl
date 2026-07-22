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
