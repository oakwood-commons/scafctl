// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSandbox_BasicCreationAndCleanup(t *testing.T) {
	dir := setupSandboxDir(t)

	sb, err := soltesting.NewSandbox(filepath.Join(dir, "solution.yaml"), nil, nil)
	require.NoError(t, err)
	defer sb.Cleanup()

	_, err = os.Stat(sb.Path())
	assert.NoError(t, err)

	_, err = os.Stat(sb.SolutionPath())
	assert.NoError(t, err)

	content, err := os.ReadFile(sb.SolutionPath())
	require.NoError(t, err)
	assert.Equal(t, "apiVersion: scafctl.io/v1\n", string(content))

	sb.Cleanup()
	_, err = os.Stat(sb.Path())
	assert.True(t, os.IsNotExist(err))
}

func TestNewSandbox_CopiesBundleAndTestFiles(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "templates/main.tmpl", "template content")
	writeSandboxFile(t, dir, "testdata/input.json", `{"key":"value"}`)

	sb, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		[]string{"templates/main.tmpl"},
		[]string{"testdata/input.json"},
	)
	require.NoError(t, err)
	defer sb.Cleanup()

	content, err := os.ReadFile(filepath.Join(sb.Path(), "templates/main.tmpl"))
	require.NoError(t, err)
	assert.Equal(t, "template content", string(content))

	content, err = os.ReadFile(filepath.Join(sb.Path(), "testdata/input.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"key":"value"}`, string(content))
}

func TestSandbox_PrePostSnapshot_DetectsNewAndModifiedFiles(t *testing.T) {
	dir := setupSandboxDir(t)

	sb, err := soltesting.NewSandbox(filepath.Join(dir, "solution.yaml"), nil, nil)
	require.NoError(t, err)
	defer sb.Cleanup()

	require.NoError(t, sb.PreSnapshot())

	time.Sleep(50 * time.Millisecond)

	writeSandboxFile(t, sb.Path(), "output/result.txt", "new content")
	require.NoError(t, os.WriteFile(sb.SolutionPath(), []byte("modified"), 0o644))

	files, err := sb.PostSnapshot()
	require.NoError(t, err)

	fi, ok := files["output/result.txt"]
	assert.True(t, ok, "new file should be detected")
	assert.True(t, fi.Exists)
	assert.Equal(t, "new content", fi.Content)

	fi, ok = files["solution.yaml"]
	assert.True(t, ok, "modified file should be detected")
	assert.Equal(t, "modified", fi.Content)
}

func TestSandbox_PrePostSnapshot_IgnoresUnchangedFiles(t *testing.T) {
	dir := setupSandboxDir(t)

	sb, err := soltesting.NewSandbox(filepath.Join(dir, "solution.yaml"), nil, nil)
	require.NoError(t, err)
	defer sb.Cleanup()

	require.NoError(t, sb.PreSnapshot())

	files, err := sb.PostSnapshot()
	require.NoError(t, err)
	assert.Empty(t, files, "no files should be detected when nothing changed")
}

func TestSandbox_PostSnapshot_SizeGuard(t *testing.T) {
	dir := setupSandboxDir(t)

	sb, err := soltesting.NewSandbox(filepath.Join(dir, "solution.yaml"), nil, nil)
	require.NoError(t, err)
	defer sb.Cleanup()

	require.NoError(t, sb.PreSnapshot())

	largeFile := filepath.Join(sb.Path(), "large.bin")
	f, err := os.Create(largeFile)
	require.NoError(t, err)
	data := make([]byte, soltesting.MaxFileSize+1)
	for i := range data {
		data[i] = 'x'
	}
	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	files, err := sb.PostSnapshot()
	require.NoError(t, err)

	fi, ok := files["large.bin"]
	assert.True(t, ok)
	assert.Equal(t, soltesting.FileTooLargePlaceholder, fi.Content)
}

func TestSandbox_PostSnapshot_BinaryGuard(t *testing.T) {
	dir := setupSandboxDir(t)

	sb, err := soltesting.NewSandbox(filepath.Join(dir, "solution.yaml"), nil, nil)
	require.NoError(t, err)
	defer sb.Cleanup()

	require.NoError(t, sb.PreSnapshot())

	binaryFile := filepath.Join(sb.Path(), "image.png")
	require.NoError(t, os.WriteFile(binaryFile, []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}, 0o644))

	files, err := sb.PostSnapshot()
	require.NoError(t, err)

	fi, ok := files["image.png"]
	assert.True(t, ok)
	assert.Equal(t, soltesting.BinaryFilePlaceholder, fi.Content)
}

func TestNewSandbox_RejectsSymlinks(t *testing.T) {
	dir := setupSandboxDir(t)

	writeSandboxFile(t, dir, "real.txt", "real content")
	require.NoError(t, os.Symlink(filepath.Join(dir, "real.txt"), filepath.Join(dir, "link.txt")))

	_, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		[]string{"link.txt"},
		nil,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "symlink")
}

func TestNewSandbox_RejectsPathTraversal(t *testing.T) {
	dir := setupSandboxDir(t)

	_, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		[]string{"../../../etc/passwd"},
		nil,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestNewSandbox_RejectsAbsolutePath(t *testing.T) {
	dir := setupSandboxDir(t)

	_, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		[]string{"/etc/passwd"},
		nil,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestSandbox_PostSnapshot_RequiresPreSnapshot(t *testing.T) {
	dir := setupSandboxDir(t)

	sb, err := soltesting.NewSandbox(filepath.Join(dir, "solution.yaml"), nil, nil)
	require.NoError(t, err)
	defer sb.Cleanup()

	_, err = sb.PostSnapshot()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "PreSnapshot")
}

func TestNewBaseSandbox_And_CopyForTest(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "shared/config.yaml", "shared config")
	writeSandboxFile(t, dir, "testdata/test1.json", "test1 data")

	base, err := soltesting.NewBaseSandbox(
		filepath.Join(dir, "solution.yaml"),
		[]string{"shared/config.yaml"},
	)
	require.NoError(t, err)
	defer base.Cleanup()

	content, err := os.ReadFile(filepath.Join(base.Path(), "shared/config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "shared config", string(content))

	child, err := base.CopyForTest(dir, []string{"testdata/test1.json"})
	require.NoError(t, err)
	defer child.Cleanup()

	content, err = os.ReadFile(filepath.Join(child.Path(), "shared/config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "shared config", string(content))

	content, err = os.ReadFile(filepath.Join(child.Path(), "testdata/test1.json"))
	require.NoError(t, err)
	assert.Equal(t, "test1 data", string(content))

	_, err = os.Stat(child.SolutionPath())
	assert.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(child.Path(), "shared/config.yaml"), []byte("modified"), 0o644))
	content, err = os.ReadFile(filepath.Join(base.Path(), "shared/config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "shared config", string(content))
}

func TestNewSandbox_GlobExpansion(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "templates/main.yaml", "main")
	writeSandboxFile(t, dir, "templates/sub/nested.yaml", "nested")
	writeSandboxFile(t, dir, "templates/other.txt", "other")

	sb, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		nil,
		[]string{"templates/**/*.yaml"},
	)
	require.NoError(t, err)
	defer sb.Cleanup()

	// Both .yaml files should be copied
	content, err := os.ReadFile(filepath.Join(sb.Path(), "templates/main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "main", string(content))

	content, err = os.ReadFile(filepath.Join(sb.Path(), "templates/sub/nested.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "nested", string(content))

	// .txt file should NOT be copied (doesn't match *.yaml glob)
	_, err = os.Stat(filepath.Join(sb.Path(), "templates/other.txt"))
	assert.True(t, os.IsNotExist(err), "non-matching file should not be copied")
}

func TestNewSandbox_DirectoryExpansion(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "testdata/a.json", "a")
	writeSandboxFile(t, dir, "testdata/sub/b.yaml", "b")

	sb, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		nil,
		[]string{"testdata"},
	)
	require.NoError(t, err)
	defer sb.Cleanup()

	content, err := os.ReadFile(filepath.Join(sb.Path(), "testdata/a.json"))
	require.NoError(t, err)
	assert.Equal(t, "a", string(content))

	content, err = os.ReadFile(filepath.Join(sb.Path(), "testdata/sub/b.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "b", string(content))
}

func TestNewSandbox_DirectoryWithTrailingSlash(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "data/file1.txt", "f1")
	writeSandboxFile(t, dir, "data/file2.txt", "f2")

	sb, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		nil,
		[]string{"data/"},
	)
	require.NoError(t, err)
	defer sb.Cleanup()

	content, err := os.ReadFile(filepath.Join(sb.Path(), "data/file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, "f1", string(content))

	content, err = os.ReadFile(filepath.Join(sb.Path(), "data/file2.txt"))
	require.NoError(t, err)
	assert.Equal(t, "f2", string(content))
}

func TestNewSandbox_MixedFilesGlobsAndDirectories(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "single.txt", "single")
	writeSandboxFile(t, dir, "templates/main.yaml", "main-tmpl")
	writeSandboxFile(t, dir, "templates/other.yaml", "other-tmpl")
	writeSandboxFile(t, dir, "data/input.json", "input")

	sb, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		nil,
		[]string{"single.txt", "templates/*.yaml", "data"},
	)
	require.NoError(t, err)
	defer sb.Cleanup()

	content, err := os.ReadFile(filepath.Join(sb.Path(), "single.txt"))
	require.NoError(t, err)
	assert.Equal(t, "single", string(content))

	content, err = os.ReadFile(filepath.Join(sb.Path(), "templates/main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "main-tmpl", string(content))

	content, err = os.ReadFile(filepath.Join(sb.Path(), "templates/other.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "other-tmpl", string(content))

	content, err = os.ReadFile(filepath.Join(sb.Path(), "data/input.json"))
	require.NoError(t, err)
	assert.Equal(t, "input", string(content))
}

func TestNewSandbox_DeduplicatesGlobResults(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "templates/main.yaml", "content")

	// Both entries resolve to the same file — should not error
	sb, err := soltesting.NewSandbox(
		filepath.Join(dir, "solution.yaml"),
		nil,
		[]string{"templates/main.yaml", "templates/*.yaml"},
	)
	require.NoError(t, err)
	defer sb.Cleanup()

	content, err := os.ReadFile(filepath.Join(sb.Path(), "templates/main.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "content", string(content))
}

func TestCopyForTest_GlobAndDirectoryExpansion(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "shared/config.yaml", "shared")
	writeSandboxFile(t, dir, "testdata/a.json", "a")
	writeSandboxFile(t, dir, "testdata/b.json", "b")
	writeSandboxFile(t, dir, "extra/deep/file.txt", "deep")

	base, err := soltesting.NewBaseSandbox(
		filepath.Join(dir, "solution.yaml"),
		[]string{"shared/config.yaml"},
	)
	require.NoError(t, err)
	defer base.Cleanup()

	child, err := base.CopyForTest(dir, []string{"testdata/*.json", "extra"})
	require.NoError(t, err)
	defer child.Cleanup()

	content, err := os.ReadFile(filepath.Join(child.Path(), "testdata/a.json"))
	require.NoError(t, err)
	assert.Equal(t, "a", string(content))

	content, err = os.ReadFile(filepath.Join(child.Path(), "testdata/b.json"))
	require.NoError(t, err)
	assert.Equal(t, "b", string(content))

	content, err = os.ReadFile(filepath.Join(child.Path(), "extra/deep/file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "deep", string(content))
}

func setupSandboxDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeSandboxFile(t, dir, "solution.yaml", "apiVersion: scafctl.io/v1\n")
	return dir
}

func writeSandboxFile(t *testing.T, baseDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), 0o755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0o644))
}

func TestNewSandboxWithBaseDir_NestsUnderSubdir(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "output/data.json", `{"result": true}`)

	sb, err := soltesting.NewSandboxWithBaseDir(
		filepath.Join(dir, "solution.yaml"),
		"myapp",
		nil,
		[]string{"output/data.json"},
	)
	require.NoError(t, err)
	defer sb.Cleanup()

	// Solution should be nested under myapp/
	assert.Contains(t, sb.SolutionPath(), filepath.Join("myapp", "solution.yaml"))

	// Solution file should exist at nested path
	_, err = os.Stat(sb.SolutionPath())
	assert.NoError(t, err)

	// Data file should also be nested
	nestedData := filepath.Join(sb.Path(), "myapp", "output", "data.json")
	content, err := os.ReadFile(nestedData)
	require.NoError(t, err)
	assert.Equal(t, `{"result": true}`, string(content))

	// File should NOT exist at sandbox root level
	_, err = os.Stat(filepath.Join(sb.Path(), "output", "data.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestNewSandboxWithBaseDir_EmptyBaseDirIsSameAsNewSandbox(t *testing.T) {
	dir := setupSandboxDir(t)

	sb, err := soltesting.NewSandboxWithBaseDir(
		filepath.Join(dir, "solution.yaml"),
		"",
		nil,
		nil,
	)
	require.NoError(t, err)
	defer sb.Cleanup()

	// Solution should be at root level (no nesting)
	assert.Equal(t, filepath.Join(sb.Path(), "solution.yaml"), sb.SolutionPath())
}

func TestNewSandboxWithBaseDir_CopyForTestPreservesBaseDir(t *testing.T) {
	dir := setupSandboxDir(t)
	writeSandboxFile(t, dir, "base.txt", "base content")
	writeSandboxFile(t, dir, "extra.txt", "extra content")

	// Create a base sandbox with baseDir
	base, err := soltesting.NewSandboxWithBaseDir(
		filepath.Join(dir, "solution.yaml"),
		"sub",
		nil,
		[]string{"base.txt"},
	)
	require.NoError(t, err)
	defer base.Cleanup()

	// CopyForTest should preserve the baseDir nesting
	child, err := base.CopyForTest(dir, []string{"extra.txt"})
	require.NoError(t, err)
	defer child.Cleanup()

	// Solution should be nested in both
	assert.Contains(t, child.SolutionPath(), filepath.Join("sub", "solution.yaml"))

	// Both files should be nested under sub/
	_, err = os.Stat(filepath.Join(child.Path(), "sub", "base.txt"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(child.Path(), "sub", "extra.txt"))
	assert.NoError(t, err)
}

func TestNewSandboxWithBaseDir_RejectsAbsolutePath(t *testing.T) {
	dir := setupSandboxDir(t)
	_, err := soltesting.NewSandboxWithBaseDir(
		filepath.Join(dir, "solution.yaml"),
		"/etc/evil",
		nil,
		nil,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must be a relative path")
}

func TestNewSandboxWithBaseDir_RejectsTraversal(t *testing.T) {
	tests := []struct {
		name    string
		baseDir string
	}{
		{"bare dotdot", ".."},
		{"dotdot prefix", "../escape"},
		{"nested dotdot", "foo/../../escape"},
	}
	dir := setupSandboxDir(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := soltesting.NewSandboxWithBaseDir(
				filepath.Join(dir, "solution.yaml"),
				tt.baseDir,
				nil,
				nil,
			)
			assert.Error(t, err, "baseDir %q should be rejected", tt.baseDir)
			assert.Contains(t, err.Error(), "must not traverse")
		})
	}
}
