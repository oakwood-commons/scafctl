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

	match, diff, _, err := soltesting.CompareSnapshot("hello world\n", snapshotPath, "", nil)
	require.NoError(t, err)
	assert.True(t, match)
	assert.Empty(t, diff)
}

func TestCompareSnapshot_Mismatch(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	require.NoError(t, os.WriteFile(snapshotPath, []byte("expected output\n"), 0o644))

	match, diff, _, err := soltesting.CompareSnapshot("actual output\n", snapshotPath, "", nil)
	require.NoError(t, err)
	assert.False(t, match)
	assert.Contains(t, diff, "--- expected")
	assert.Contains(t, diff, "+++ actual")
	assert.Contains(t, diff, "-expected output")
	assert.Contains(t, diff, "+actual output")
}

func TestCompareSnapshot_FileNotFound(t *testing.T) {
	ok, _, _, err := soltesting.CompareSnapshot("anything", "/nonexistent/golden.txt", "", nil)
	assert.False(t, ok)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading snapshot file")
}

func TestCompareSnapshot_NormalizesBeforeCompare(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	normalized := "started at " + soltesting.TimestampPlaceholder + "\n"
	require.NoError(t, os.WriteFile(snapshotPath, []byte(normalized), 0o644))

	match, diff, _, err := soltesting.CompareSnapshot(
		"started at 2025-02-13T12:00:00Z\n", snapshotPath, "", nil)
	require.NoError(t, err)
	assert.True(t, match, "should match after normalization, diff: %s", diff)
}

func TestUpdateSnapshot_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "sub", "golden.txt")

	_, err := soltesting.UpdateSnapshot("raw content 2025-01-01T00:00:00Z\n", snapshotPath, "", nil)
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

	_, err := soltesting.UpdateSnapshot("new content\n", snapshotPath, "", nil)
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

	match, _, _, err := soltesting.CompareSnapshot(
		"file at /tmp/scafctl-test-xyz/out.txt\n", snapshotPath, sandboxPath, nil)
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

func TestCompareSnapshot_CustomMaskMatchesAndCounts(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	golden := "admins = <ADMINS>\nobservers = <ADMINS>\n"
	require.NoError(t, os.WriteFile(snapshotPath, []byte(golden), 0o644))

	masks := []soltesting.Mask{
		{Name: "group-list", Pattern: `\[[^\]]*\]`, Placeholder: "<ADMINS>"},
	}
	actual := "admins = [alice, bob]\nobservers = [carol]\n"

	match, diff, counts, err := soltesting.CompareSnapshot(actual, snapshotPath, "", masks)
	require.NoError(t, err)
	assert.True(t, match, "diff: %s", diff)
	assert.Equal(t, 2, counts["group-list"])
}

func TestCompareSnapshot_CustomMaskKeyDefaultsToPattern(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	require.NoError(t, os.WriteFile(snapshotPath, []byte("id=<X>\n"), 0o644))

	masks := []soltesting.Mask{{Pattern: `prj-[a-z0-9]+`, Placeholder: "<X>"}}
	_, _, counts, err := soltesting.CompareSnapshot("id=prj-abc123\n", snapshotPath, "", masks)
	require.NoError(t, err)
	assert.Equal(t, 1, counts[`prj-[a-z0-9]+`])
}

func TestCompareSnapshot_CatalogPresetEmail(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	require.NoError(t, os.WriteFile(snapshotPath, []byte("owner: <EMAIL>\n"), 0o644))

	masks := []soltesting.Mask{{Use: "email"}}
	match, diff, counts, err := soltesting.CompareSnapshot("owner: alice@example.com\n", snapshotPath, "", masks)
	require.NoError(t, err)
	assert.True(t, match, "diff: %s", diff)
	assert.Equal(t, 1, counts["email"])
}

func TestCompareSnapshot_DisableBuiltinPreset(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	// Golden keeps the literal UUID because the uuid preset is disabled.
	golden := "id: a1b2c3d4-e5f6-7890-abcd-ef1234567890\n"
	require.NoError(t, os.WriteFile(snapshotPath, []byte(golden), 0o644))

	masks := []soltesting.Mask{{Use: "uuid", Disabled: true}}
	match, diff, _, err := soltesting.CompareSnapshot(golden, snapshotPath, "", masks)
	require.NoError(t, err)
	assert.True(t, match, "diff: %s", diff)
}

func TestBuildFileManifest_DeterministicAndPathScoped(t *testing.T) {
	files := map[string]soltesting.FileInfo{
		"envs/prod/prod.auto.tfvars": {Exists: true, Content: "admins = [alice, bob]\n"},
		"envs/prod/backend.tf":       {Exists: true, Content: "bucket = \"tf-state\"\n"},
	}
	masks := []soltesting.Mask{
		{Name: "group", Pattern: `\[[^\]]*\]`, Placeholder: "<GROUP>", Path: "envs/**/*.auto.tfvars"},
	}

	manifest, counts := soltesting.BuildFileManifest(files, "", masks)

	// Sorted headers: backend.tf before prod.auto.tfvars.
	backendIdx := strings.Index(manifest, "=== envs/prod/backend.tf ===")
	tfvarsIdx := strings.Index(manifest, "=== envs/prod/prod.auto.tfvars ===")
	assert.Greater(t, tfvarsIdx, backendIdx)

	// Path-scoped mask applied to tfvars only.
	assert.Contains(t, manifest, "admins = <GROUP>")
	assert.Contains(t, manifest, "bucket = \"tf-state\"")
	assert.Equal(t, 1, counts["group"])

	// Manifest is stable across calls.
	manifest2, _ := soltesting.BuildFileManifest(files, "", masks)
	assert.Equal(t, manifest, manifest2)
}

func TestCompareFileSnapshot_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	snapshotPath := filepath.Join(dir, "golden.txt")
	files := map[string]soltesting.FileInfo{
		"a.txt": {Exists: true, Content: "value = [x, y]\n"},
	}
	masks := []soltesting.Mask{
		{Name: "list", Pattern: `\[[^\]]*\]`, Placeholder: "<LIST>", Path: "*.txt"},
	}

	// Update writes the masked manifest.
	counts, err := soltesting.UpdateFileSnapshot(files, snapshotPath, "", masks)
	require.NoError(t, err)
	assert.Equal(t, 1, counts["list"])

	// A different volatile value still matches because the region is masked.
	files["a.txt"] = soltesting.FileInfo{Exists: true, Content: "value = [a, b, c]\n"}
	match, diff, _, err := soltesting.CompareFileSnapshot(files, snapshotPath, "", masks)
	require.NoError(t, err)
	assert.True(t, match, "diff: %s", diff)

	// A change to stable content fails.
	files["a.txt"] = soltesting.FileInfo{Exists: true, Content: "changed = [a, b, c]\n"}
	match, _, _, err = soltesting.CompareFileSnapshot(files, snapshotPath, "", masks)
	require.NoError(t, err)
	assert.False(t, match)
}

func TestIsKnownPreset(t *testing.T) {
	for _, name := range soltesting.PresetNames() {
		assert.True(t, soltesting.IsKnownPreset(name), "preset %q should be known", name)
	}
	assert.False(t, soltesting.IsKnownPreset("does-not-exist"))
	assert.Contains(t, soltesting.PresetNames(), "timestamp")
	assert.Contains(t, soltesting.PresetNames(), "email")
}

func TestMask_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mask    soltesting.Mask
		wantErr string
	}{
		{name: "valid custom", mask: soltesting.Mask{Pattern: `\d+`, Placeholder: "<N>"}},
		{name: "valid preset", mask: soltesting.Mask{Use: "uuid"}},
		{name: "valid preset disabled", mask: soltesting.Mask{Use: "uuid", Disabled: true}},
		{name: "use with pattern", mask: soltesting.Mask{Use: "uuid", Pattern: `\d+`}, wantErr: "must not set"},
		{name: "use with path", mask: soltesting.Mask{Use: "uuid", Path: "**/*.yaml"}, wantErr: "must not set 'path'"},
		{name: "unknown preset", mask: soltesting.Mask{Use: "nope"}, wantErr: "unknown preset"},
		{name: "disabled without use", mask: soltesting.Mask{Pattern: `\d+`, Placeholder: "<N>", Disabled: true}, wantErr: "only valid together with 'use'"},
		{name: "missing placeholder", mask: soltesting.Mask{Pattern: `\d+`}, wantErr: "requires both"},
		{name: "invalid regex", mask: soltesting.Mask{Pattern: `(`, Placeholder: "<N>"}, wantErr: "invalid mask pattern"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.mask
			err := m.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
