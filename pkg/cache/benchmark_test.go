// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkClearDirectory(b *testing.B) {
	for b.Loop() {
		b.StopTimer()
		dir := b.TempDir()
		for i := 0; i < 20; i++ {
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("file%d.txt", i)), []byte("benchmark data"), 0o644)
		}
		b.StartTimer()

		_, _, _ = ClearDirectory(dir, "")
	}
}

func BenchmarkClearDirectory_WithPattern(b *testing.B) {
	for b.Loop() {
		b.StopTimer()
		dir := b.TempDir()
		for i := 0; i < 10; i++ {
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("cache%d.json", i)), []byte("json data"), 0o644)
		}
		for i := 0; i < 10; i++ {
			_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("keep%d.txt", i)), []byte("keep data"), 0o644)
		}
		b.StartTimer()

		_, _, _ = ClearDirectory(dir, "*.json")
	}
}

func BenchmarkGetCacheInfo(b *testing.B) {
	dir := b.TempDir()
	for i := 0; i < 50; i++ {
		_ = os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", i)), []byte("data content here"), 0o644)
	}

	b.ResetTimer()
	for b.Loop() {
		_ = GetCacheInfo("Bench", dir, "benchmark cache")
	}
}

func BenchmarkGetCacheInfo_Empty(b *testing.B) {
	dir := b.TempDir()

	b.ResetTimer()
	for b.Loop() {
		_ = GetCacheInfo("Empty", dir, "empty cache")
	}
}
