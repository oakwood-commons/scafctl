// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package dns

import (
	"strings"
	"testing"
)

func BenchmarkSlugify(b *testing.B) {
	b.Run("simple", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			Slugify("hello-world")
		}
	})

	b.Run("mixed_case_special_chars", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			Slugify("My Application Name @2024!")
		}
	})

	b.Run("long_string", func(b *testing.B) {
		input := strings.Repeat("Hello World Test! ", 10)
		b.ReportAllocs()
		for b.Loop() {
			Slugify(input)
		}
	})

	b.Run("already_valid", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			Slugify("already-valid-dns-label")
		}
	})

	b.Run("unicode", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			Slugify("héllo wörld über")
		}
	})
}
