// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashFiles(t *testing.T) {
	t.Parallel()

	t.Run("empty patterns returns empty string", func(t *testing.T) {
		t.Parallel()
		hash, err := HashFiles(t.TempDir(), nil)
		require.NoError(t, err)
		assert.Empty(t, hash)
	})

	t.Run("deterministic hash for same content", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "a.go", "package main")
		testWriteFile(t, dir, "b.go", "package b")

		hash1, err := HashFiles(dir, []string{"*.go"})
		require.NoError(t, err)

		hash2, err := HashFiles(dir, []string{"*.go"})
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
		assert.Len(t, hash1, 64) // SHA-256 hex is 64 chars
	})

	t.Run("different content produces different hash", func(t *testing.T) {
		t.Parallel()
		dir1 := t.TempDir()
		testWriteFile(t, dir1, "a.go", "package main")

		dir2 := t.TempDir()
		testWriteFile(t, dir2, "a.go", "package other")

		hash1, err := HashFiles(dir1, []string{"*.go"})
		require.NoError(t, err)

		hash2, err := HashFiles(dir2, []string{"*.go"})
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("file rename produces different hash", func(t *testing.T) {
		t.Parallel()
		dir1 := t.TempDir()
		testWriteFile(t, dir1, "a.go", "package main")

		dir2 := t.TempDir()
		testWriteFile(t, dir2, "b.go", "package main")

		hash1, err := HashFiles(dir1, []string{"*.go"})
		require.NoError(t, err)

		hash2, err := HashFiles(dir2, []string{"*.go"})
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("additional file produces different hash", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		testWriteFile(t, dir, "a.go", "package main")

		hash1, err := HashFiles(dir, []string{"*.go"})
		require.NoError(t, err)

		testWriteFile(t, dir, "b.go", "package b")

		hash2, err := HashFiles(dir, []string{"*.go"})
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})
}

func BenchmarkHashFiles(b *testing.B) {
	dir := b.TempDir()
	// Create 100 files
	for i := range 100 {
		name := filepath.Join("pkg", "file"+string(rune('a'+i%26))+".go")
		testWriteFile(b, dir, name, "package pkg\n// content "+string(rune(i)))
	}

	b.ResetTimer()
	for b.Loop() {
		_, _ = HashFiles(dir, []string{"**/*.go"})
	}
}

func TestHashInputs(t *testing.T) {
	t.Parallel()

	t.Run("nil inputs returns empty string", func(t *testing.T) {
		t.Parallel()
		hash, err := HashInputs(nil)
		require.NoError(t, err)
		assert.Empty(t, hash)
	})

	t.Run("empty inputs returns empty string", func(t *testing.T) {
		t.Parallel()
		hash, err := HashInputs(map[string]any{})
		require.NoError(t, err)
		assert.Empty(t, hash)
	})

	t.Run("deterministic hash for same inputs", func(t *testing.T) {
		t.Parallel()
		inputs := map[string]any{"env": "staging", "port": 8080}

		hash1, err := HashInputs(inputs)
		require.NoError(t, err)

		hash2, err := HashInputs(inputs)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
		assert.Len(t, hash1, 64) // SHA-256 hex is 64 chars
	})

	t.Run("key ordering does not affect hash", func(t *testing.T) {
		t.Parallel()
		// Go maps are unordered; json.Marshal sorts keys.
		// Create two maps with different insertion order.
		m1 := map[string]any{"a": 1, "b": 2, "c": 3}
		m2 := map[string]any{"c": 3, "a": 1, "b": 2}

		hash1, err := HashInputs(m1)
		require.NoError(t, err)

		hash2, err := HashInputs(m2)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("different values produce different hash", func(t *testing.T) {
		t.Parallel()
		hash1, err := HashInputs(map[string]any{"env": "staging"})
		require.NoError(t, err)

		hash2, err := HashInputs(map[string]any{"env": "production"})
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("type sensitivity", func(t *testing.T) {
		t.Parallel()
		hash1, err := HashInputs(map[string]any{"port": 8080})
		require.NoError(t, err)

		hash2, err := HashInputs(map[string]any{"port": "8080"})
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("nested structures hash correctly", func(t *testing.T) {
		t.Parallel()
		inputs := map[string]any{
			"config": map[string]any{
				"nested": "value",
				"list":   []any{1, 2, 3},
			},
		}

		hash1, err := HashInputs(inputs)
		require.NoError(t, err)

		hash2, err := HashInputs(inputs)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
		assert.Len(t, hash1, 64)
	})
}

func BenchmarkHashInputs(b *testing.B) {
	inputs := map[string]any{
		"env":     "production",
		"port":    8080,
		"verbose": true,
		"config": map[string]any{
			"database": "postgres://localhost/db",
			"cache":    "redis://localhost:6379",
		},
		"tags": []any{"web", "api", "v2"},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = HashInputs(inputs)
	}
}
