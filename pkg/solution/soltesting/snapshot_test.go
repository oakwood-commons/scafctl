// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func snapshotIndexOf(s, substr string) int {
	return strings.Index(s, substr)
}

func TestNormalize_TimestampsReplaced(t *testing.T) {
	input := "started at 2024-01-15T10:30:00Z and ended at 2024-06-30T23:59:59+05:00"
	result := soltesting.Normalize(input, "")
	assert.Contains(t, result, soltesting.TimestampPlaceholder)
	assert.NotContains(t, result, "2024-01-15")
	assert.NotContains(t, result, "2024-06-30")
}

func TestNormalize_UUIDsReplaced(t *testing.T) {
	input := "id: a1b2c3d4-e5f6-7890-abcd-ef1234567890 ref: AABBCCDD-1122-3344-5566-778899AABBCC"
	result := soltesting.Normalize(input, "")
	assert.Contains(t, result, soltesting.UUIDPlaceholder)
	assert.NotContains(t, result, "a1b2c3d4-e5f6-7890-abcd-ef1234567890")
	assert.NotContains(t, result, "AABBCCDD-1122-3344-5566-778899AABBCC")
}

func TestNormalize_SandboxPathReplaced(t *testing.T) {
	sandboxPath := "/tmp/scafctl-test-12345"
	input := "file at /tmp/scafctl-test-12345/solution.yaml was processed"
	result := soltesting.Normalize(input, sandboxPath)
	assert.Contains(t, result, soltesting.SandboxPlaceholder)
	assert.NotContains(t, result, sandboxPath)
}

func TestNormalize_JSONKeysSorted(t *testing.T) {
	input := `{"zebra":"last","alpha":"first","middle":"mid"}`
	result := soltesting.Normalize(input, "")
	alphaIdx := snapshotIndexOf(result, `"alpha"`)
	middleIdx := snapshotIndexOf(result, `"middle"`)
	zebraIdx := snapshotIndexOf(result, `"zebra"`)
	assert.Greater(t, middleIdx, alphaIdx, "alpha should come before middle")
	assert.Greater(t, zebraIdx, middleIdx, "middle should come before zebra")
}

func TestNormalize_NonJSONPassthrough(t *testing.T) {
	input := "just plain text with 2024-01-15T10:00:00Z timestamp"
	result := soltesting.Normalize(input, "")
	assert.Contains(t, result, "just plain text")
	assert.Contains(t, result, soltesting.TimestampPlaceholder)
}

func TestNormalize_EmptyInput(t *testing.T) {
	result := soltesting.Normalize("", "")
	assert.Equal(t, "", result)
}

func TestNormalize_AllReplacementsCombined(t *testing.T) {
	sandboxPath := "/tmp/sandbox-abc"
	input := `{"path":"/tmp/sandbox-abc/out.txt","time":"2025-01-01T00:00:00Z","id":"11111111-2222-3333-4444-555555555555"}`
	result := soltesting.Normalize(input, sandboxPath)
	assert.Contains(t, result, soltesting.SandboxPlaceholder)
	assert.Contains(t, result, soltesting.TimestampPlaceholder)
	assert.Contains(t, result, soltesting.UUIDPlaceholder)
	assert.NotContains(t, result, sandboxPath)
	assert.NotContains(t, result, "2025-01-01")
	assert.NotContains(t, result, "11111111-2222")
}

func TestCompareSnapshot_Match(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	content := "hello world\n"
	require.NoError(t, os.WriteFile(snapshotPath, []byte(content), 0o644))

	match, diff, err := soltesting.CompareSnapshot("hello world\n", snapshotPath, "")
	require.NoError(t, err)
	assert.True(t, match)
	assert.Empty(t, diff)
}

func TestCompareSnapshot_Mismatch(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	require.NoError(t, os.WriteFile(snapshotPath, []byte("expected output\n"), 0o644))

	match, diff, err := soltesting.CompareSnapshot("actual output\n", snapshotPath, "")
	require.NoError(t, err)
	assert.False(t, match)
	assert.Contains(t, diff, "--- expected")
	assert.Contains(t, diff, "+++ actual")
	assert.Contains(t, diff, "-expected output")
	assert.Contains(t, diff, "+actual output")
}

func TestCompareSnapshot_FileNotFound(t *testing.T) {
	_, _, err := soltesting.CompareSnapshot("anything", "/nonexistent/golden.txt", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading snapshot file")
}

func TestCompareSnapshot_NormalizesBeforeCompare(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	normalized := "started at " + soltesting.TimestampPlaceholder + "\n"
	require.NoError(t, os.WriteFile(snapshotPath, []byte(normalized), 0o644))

	match, diff, err := soltesting.CompareSnapshot(
		"started at 2025-02-13T12:00:00Z\n", snapshotPath, "")
	require.NoError(t, err)
	assert.True(t, match, "should match after normalization, diff: %s", diff)
}

func TestUpdateSnapshot_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "sub", "golden.txt")

	err := soltesting.UpdateSnapshot("raw content 2025-01-01T00:00:00Z\n", snapshotPath, "")
	require.NoError(t, err)

	data, err := os.ReadFile(snapshotPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), soltesting.TimestampPlaceholder)
	assert.NotContains(t, string(data), "2025-01-01")
}

func TestUpdateSnapshot_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	require.NoError(t, os.WriteFile(snapshotPath, []byte("old content"), 0o644))

	err := soltesting.UpdateSnapshot("new content\n", snapshotPath, "")
	require.NoError(t, err)

	data, err := os.ReadFile(snapshotPath)
	require.NoError(t, err)
	assert.Equal(t, "new content\n", string(data))
}

func TestCompareSnapshot_WithSandboxPath(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	sandboxPath := "/tmp/scafctl-test-xyz"
	normalized := "file at " + soltesting.SandboxPlaceholder + "/out.txt\n"
	require.NoError(t, os.WriteFile(snapshotPath, []byte(normalized), 0o644))

	match, _, err := soltesting.CompareSnapshot(
		"file at /tmp/scafctl-test-xyz/out.txt\n", snapshotPath, sandboxPath)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestNormalize_NestedJSON(t *testing.T) {
	input := `{"b":{"z":1,"a":2},"a":"first"}`
	result := soltesting.Normalize(input, "")
	aIdx := snapshotIndexOf(result, `"a"`)
	bIdx := snapshotIndexOf(result, `"b"`)
	assert.Greater(t, bIdx, aIdx, "a should come before b in sorted output")
}
