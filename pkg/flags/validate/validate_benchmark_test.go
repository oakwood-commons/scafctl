// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"encoding/base64"
	"testing"
)

func BenchmarkValidateValue(b *testing.B) {
	b.Run("plain_value", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = ValidateValue("key", "simple-value")
		}
	})

	b.Run("json_scheme", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = ValidateValue("key", `json://{"name":"test","port":8080}`)
		}
	})

	b.Run("yaml_scheme", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _ = ValidateValue("key", "yaml://name: test\nport: 8080")
		}
	})

	b.Run("base64_scheme", func(b *testing.B) {
		encoded := "base64://" + base64.StdEncoding.EncodeToString([]byte("hello world"))
		b.ReportAllocs()
		for b.Loop() {
			_, _ = ValidateValue("key", encoded)
		}
	})
}

func BenchmarkValidateJSON(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = ValidateJSON(`{"key":"value"}`)
		}
	})

	b.Run("nested", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = ValidateJSON(`{"server":{"host":"localhost","port":8080,"tls":{"enabled":true}}}`)
		}
	})
}

func BenchmarkValidateBase64(b *testing.B) {
	encoded := base64.StdEncoding.EncodeToString([]byte("benchmark test data for validation"))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ValidateBase64(encoded)
	}
}
