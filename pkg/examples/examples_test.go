// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Scan tests ────────────────────────────────────────────────────────────────

func TestScan_AllCategories(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)
	assert.NotEmpty(t, items, "should find at least one example")

	for _, item := range items {
		assert.NotEmpty(t, item.Path, "example path should not be empty")
	}
}

func TestScan_SpecificCategory(t *testing.T) {
	t.Parallel()
	items, err := Scan("providers")
	require.NoError(t, err)

	for _, item := range items {
		assert.Equal(t, "providers", item.Category, "all items should be in 'providers' category")
	}
}

func TestScan_NonexistentCategory(t *testing.T) {
	t.Parallel()
	items, err := Scan("this-category-does-not-exist")
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestScan_ResultsAreSorted(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)
	require.True(t, len(items) > 1, "need multiple items to test sorting")

	for i := 1; i < len(items); i++ {
		prev := items[i-1]
		curr := items[i]
		if prev.Category == curr.Category {
			assert.LessOrEqual(t, prev.Name, curr.Name, "items should be sorted by name within category")
		} else {
			assert.Less(t, prev.Category, curr.Category, "items should be sorted by category")
		}
	}
}

func TestScan_ExcludesBadSolution(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)

	for _, item := range items {
		assert.NotContains(t, item.Path, "bad-solution", "bad-solution examples should be excluded")
	}
}

func TestScan_OnlyYAMLFiles(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)

	for _, item := range items {
		isYAML := strings.HasSuffix(item.Path, ".yaml") || strings.HasSuffix(item.Path, ".yml")
		assert.True(t, isYAML, "should only include YAML files, got %q", item.Path)
	}
}

// ── Read tests ────────────────────────────────────────────────────────────────

func TestRead_ValidExample(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)
	require.NotEmpty(t, items)

	content, err := Read(items[0].Path)
	require.NoError(t, err)
	assert.NotEmpty(t, content, "example content should not be empty")
}

func TestRead_BackslashPathNormalized(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)
	require.NotEmpty(t, items)

	// Simulate a Windows-style backslash path
	backslashPath := strings.ReplaceAll(items[0].Path, "/", "\\")
	content, err := Read(backslashPath)
	require.NoError(t, err)
	assert.NotEmpty(t, content, "should read example with backslash path")
}

func TestScan_PathsUseForwardSlashes(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)
	require.NotEmpty(t, items)

	for _, item := range items {
		assert.NotContains(t, item.Path, "\\", "example paths should use forward slashes, got %q", item.Path)
	}
}

func TestRead_NonexistentFile(t *testing.T) {
	t.Parallel()
	_, err := Read("nonexistent/file.yaml")
	require.Error(t, err)
}

func TestRead_PathTraversalBlocked(t *testing.T) {
	t.Parallel()
	_, err := Read("../../../etc/passwd")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "..")
}

func TestRead_PathTraversalVariants(t *testing.T) {
	t.Parallel()
	tests := []string{
		"../../secret.yaml",
		"foo/../../bar.yaml",
		"../solution.yaml",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			_, err := Read(path)
			require.Error(t, err, "path traversal should be rejected: %s", path)
		})
	}
}

// ── Categories tests ──────────────────────────────────────────────────────────

func TestCategories(t *testing.T) {
	t.Parallel()
	cats := Categories()
	assert.NotEmpty(t, cats, "should return at least one category")

	for i := 1; i < len(cats); i++ {
		assert.Less(t, cats[i-1], cats[i], "categories should be sorted")
	}
}

func TestCategories_ContainsExpectedCategories(t *testing.T) {
	t.Parallel()
	cats := Categories()
	expected := []string{"providers", "resolvers", "solutions"}

	for _, exp := range expected {
		assert.Contains(t, cats, exp, "should contain %q category", exp)
	}
}

// ── DescriptionFromPath tests ─────────────────────────────────────────────────

func TestDescriptionFromPath_KnownPath(t *testing.T) {
	t.Parallel()
	desc := DescriptionFromPath("solutions/comprehensive/solution.yaml")
	assert.NotEmpty(t, desc)
	assert.Contains(t, desc, "Comprehensive")
}

func TestDescriptionFromPath_UnknownPath(t *testing.T) {
	t.Parallel()
	desc := DescriptionFromPath("unknown/my-custom-example.yaml")
	assert.Contains(t, desc, "example")
	assert.Contains(t, desc, "My")
}

func TestDescriptionFromPath_CleansFallbackName(t *testing.T) {
	t.Parallel()
	desc := DescriptionFromPath("category/some-complex_file-name.yaml")
	assert.NotContains(t, desc, "-")
	assert.NotContains(t, desc, "_")
}

// ── Example struct tests ──────────────────────────────────────────────────────

func TestExample_Fields(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)
	require.NotEmpty(t, items)

	for _, item := range items[:min(5, len(items))] {
		assert.NotEmpty(t, item.Path, "Path should be set")
		assert.NotEmpty(t, item.Name, "Name should be set")
		assert.NotEmpty(t, item.Description, "Description should be set")
	}
}

// ── Benchmark tests ───────────────────────────────────────────────────────────

func BenchmarkScan_All(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = Scan("")
	}
}

func BenchmarkScan_Category(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = Scan("providers")
	}
}

func BenchmarkRead(b *testing.B) {
	items, err := Scan("")
	if err != nil || len(items) == 0 {
		b.Skip("no examples available")
	}
	path := items[0].Path
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = Read(path)
	}
}

func BenchmarkDescriptionFromPath(b *testing.B) {
	paths := []string{
		"solutions/comprehensive/solution.yaml",
		"providers/static-hello.yaml",
		"unknown/something.yaml",
	}
	b.ReportAllocs()
	b.ResetTimer()
	idx := 0
	for b.Loop() {
		DescriptionFromPath(paths[idx%len(paths)])
		idx++
	}
}

func BenchmarkCategories(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Categories()
	}
}
