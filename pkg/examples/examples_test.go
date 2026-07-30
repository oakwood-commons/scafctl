// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"errors"
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
			assert.LessOrEqual(t, prev.DisplayName, curr.DisplayName, "items should be sorted by displayName within category")
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
	assert.ErrorIs(t, err, ErrPathTraversal)
}

func TestRead_PathTraversalVariants(t *testing.T) {
	t.Parallel()
	tests := []string{
		"../../secret.yaml",
		"foo/../../bar.yaml",
		"../solution.yaml",
		// Tricky: path.Clean would resolve these ".." components away if the
		// check ran after cleaning; they must still be rejected.
		"foo/../bar.yaml",
		"foo/..",
		"actions/../secret.yaml",
		"/etc/passwd",
		"C:\\Windows\\system32",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			_, err := Read(path)
			require.Error(t, err, "path traversal should be rejected: %s", path)
			assert.True(t, errors.Is(err, ErrPathTraversal), "expected ErrPathTraversal for path %q", path)
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

// ── Metadata-driven Scan tests ────────────────────────────────────────────────

func TestScan_OnlyListsSolutions(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)
	require.NotEmpty(t, items)

	for _, item := range items {
		// Names come from metadata.name, never the filename, so no ".yaml".
		assert.NotContains(t, item.Name, ".yaml", "name must come from metadata, not filename")
		assert.NotContains(t, item.Name, ".yml")
		// Non-solution files and intentional bad/stress demos must be excluded.
		assert.NotContains(t, item.Path, "bad-solution")
		assert.NotContains(t, item.Path, "lint-stress-test")
	}
}

func TestScan_PopulatesMetadata(t *testing.T) {
	t.Parallel()
	items, err := Scan("")
	require.NoError(t, err)

	var hello *Example
	for i := range items {
		if items[i].Path == "actions/hello-world.yaml" {
			hello = &items[i]
			break
		}
	}
	require.NotNil(t, hello, "actions/hello-world.yaml should be listed")
	assert.Equal(t, "hello-world-action", hello.Name)
	assert.Equal(t, "Hello World Action", hello.DisplayName)
	assert.Equal(t, "actions", hello.Category)
	assert.NotEmpty(t, hello.Description)
	assert.Contains(t, hello.Tags, "action")
}

func TestResolveExample_ExactPath(t *testing.T) {
	t.Parallel()
	got, err := ResolveExample("actions/hello-world.yaml")
	require.NoError(t, err)
	assert.Equal(t, "actions/hello-world.yaml", got)
}

func TestResolveExample_ByName(t *testing.T) {
	t.Parallel()
	// cel-basics is a unique metadata.name.
	got, err := ResolveExample("cel-basics")
	require.NoError(t, err)
	assert.Equal(t, "resolvers/cel-basics.yaml", got)
}

func TestResolveExample_ExactPathToNonSolutionFile(t *testing.T) {
	t.Parallel()
	// The listing is solution-only, but exact-path fetch must still resolve any
	// embedded example file -- including non-solution kinds (e.g. kind: Config).
	// Regression guard: catalog/native-auth.yaml is not listed by Scan but must
	// remain fetchable by exact path.
	got, err := ResolveExample("catalog/native-auth.yaml")
	require.NoError(t, err)
	assert.Equal(t, "catalog/native-auth.yaml", got)

	// And it must NOT appear in the (solution-only) listing.
	items, err := Scan("")
	require.NoError(t, err)
	for _, it := range items {
		assert.NotEqual(t, "catalog/native-auth.yaml", it.Path,
			"non-solution files must not be listed")
	}
}

func TestResolveExample_Ambiguous(t *testing.T) {
	t.Parallel()
	// "hello-world" is a basename shared by several examples.
	_, err := ResolveExample("hello-world")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAmbiguousExample)
	assert.Contains(t, err.Error(), "actions/hello-world.yaml")
}

func TestMatchExamples_UniqueName(t *testing.T) {
	t.Parallel()
	got, err := MatchExamples("hello-world-resolver")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "hello-world-resolver", got[0].Name)
	assert.Equal(t, "resolvers/hello-world.yaml", got[0].Path)
}

func TestMatchExamples_MultipleReturnsAll(t *testing.T) {
	t.Parallel()
	// "hello-world" is a basename shared by several examples; MatchExamples
	// returns all of them (the CLI lists them for the user to pick).
	got, err := MatchExamples("hello-world")
	require.NoError(t, err)
	require.Greater(t, len(got), 1)
	paths := make([]string, len(got))
	for i, m := range got {
		paths[i] = m.Path
	}
	assert.Contains(t, paths, "actions/hello-world.yaml")
	assert.Contains(t, paths, "resolvers/hello-world.yaml")
}

func TestMatchExamples_ExactPath(t *testing.T) {
	t.Parallel()
	got, err := MatchExamples("resolvers/cel-basics.yaml")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "resolvers/cel-basics.yaml", got[0].Path)
}

func TestMatchExamples_NotFoundAndTraversal(t *testing.T) {
	t.Parallel()
	_, err := MatchExamples("does-not-exist-xyz")
	assert.ErrorIs(t, err, ErrExampleNotFound)

	_, err = MatchExamples("../../etc/passwd")
	assert.ErrorIs(t, err, ErrPathTraversal)
}

func TestScan_PopulatesContent(t *testing.T) {
	t.Parallel()
	// Content carries the full example file so the interactive (-i) detail view
	// can show the solution without a second command.
	items, err := Scan("")
	require.NoError(t, err)
	require.NotEmpty(t, items)
	for _, it := range items[:min(5, len(items))] {
		assert.Contains(t, it.Content, "apiVersion:", "%s: content should be the full solution", it.Path)
		assert.Contains(t, it.Content, "kind: Solution", it.Path)
	}
}

func TestExampleNamesAreUnique(t *testing.T) {
	t.Parallel()
	// Regression guard: every listed example must have a unique metadata.name so
	// `get examples <name>` is unambiguous by name.
	items, err := Scan("")
	require.NoError(t, err)
	seen := make(map[string]string, len(items))
	for _, it := range items {
		if prev, dup := seen[it.Name]; dup {
			t.Errorf("duplicate example name %q: %s and %s", it.Name, prev, it.Path)
		}
		seen[it.Name] = it.Path
	}
}

func TestScanSkipsEffectiveGoldenArtifacts(t *testing.T) {
	t.Parallel()
	// Rendered `.effective.yaml` golden artifacts are kind: Solution documents
	// but must not be listed as runnable examples (they collide by name with
	// their source solution).
	items, err := Scan("")
	require.NoError(t, err)
	for _, it := range items {
		assert.False(t,
			strings.HasSuffix(it.Path, ".effective.yaml") || strings.HasSuffix(it.Path, ".effective.yml"),
			"effective golden artifact must not be listed as an example: %s", it.Path)
	}
}

func TestResolveExample_NotFound(t *testing.T) {
	t.Parallel()
	_, err := ResolveExample("this-does-not-exist-anywhere")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrExampleNotFound)
}

func TestResolveExample_Empty(t *testing.T) {
	t.Parallel()
	_, err := ResolveExample("   ")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrExampleNotFound)
}

func TestResolveExample_RejectsPathTraversal(t *testing.T) {
	t.Parallel()
	for _, q := range []string{
		"../../etc/passwd",
		"foo/../../bar.yaml",
		"..\\..\\windows\\system32",
		"actions/../../secret",
		// Tricky: a ".." that path.Clean would resolve away if checked after
		// cleaning. These must still be rejected.
		"foo/../bar.yaml",
		"foo/..",
		"actions/../secret.yaml",
		// Absolute / UNC paths never name an embedded example and are rejected.
		"/etc/passwd",
		"\\\\server\\share",
		"C:\\Windows\\system32",
	} {
		_, err := ResolveExample(q)
		require.Error(t, err, "query %q must be rejected", q)
		assert.ErrorIs(t, err, ErrPathTraversal, "query %q must return ErrPathTraversal", q)
	}
}

func TestHasDotDotSegment(t *testing.T) {
	t.Parallel()
	// Only a ".." path *segment* is traversal; ".." inside a filename is safe.
	traversal := []string{"..", "../x", "a/../../b", "foo/.."}
	for _, p := range traversal {
		assert.True(t, hasDotDotSegment(p), "%q should be flagged as traversal", p)
	}
	safe := []string{"foo..bar.yaml", "a/foo..bar/c.yaml", "resolvers/hello-world.yaml", "..bar", "bar..", ""}
	for _, p := range safe {
		assert.False(t, hasDotDotSegment(p), "%q should NOT be flagged as traversal", p)
	}
}

func TestIsUnsafeExamplePath(t *testing.T) {
	t.Parallel()
	unsafe := []string{
		"..", "../x", "a/../../b", "foo/..", // traversal
		"/etc/passwd", "//server/share", "C:/Windows", // absolute / UNC / drive
	}
	for _, p := range unsafe {
		assert.True(t, isUnsafeExamplePath(p), "%q should be unsafe", p)
	}
	safe := []string{"resolvers/hello-world.yaml", "foo..bar.yaml", "a/b/c.yaml", "..bar"}
	for _, p := range safe {
		assert.False(t, isUnsafeExamplePath(p), "%q should be safe", p)
	}
}

func TestResolveExample_DotDotInFilenameIsNotTraversal(t *testing.T) {
	t.Parallel()
	// A query containing ".." in a segment name (not as a component) must not be
	// rejected as traversal -- it should simply be "not found".
	_, err := ResolveExample("foo..bar")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrExampleNotFound)
	assert.NotErrorIs(t, err, ErrPathTraversal)
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

func BenchmarkResolveExample(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = ResolveExample("cel-basics")
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

func BenchmarkCategories(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		Categories()
	}
}
