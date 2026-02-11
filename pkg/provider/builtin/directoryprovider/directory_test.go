// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package directoryprovider

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDirectoryProvider(t *testing.T) {
	p := NewDirectoryProvider()

	assert.NotNil(t, p)
	assert.NotNil(t, p.descriptor)
	assert.Equal(t, "directory", p.descriptor.Name)
	assert.Equal(t, "Directory Provider", p.descriptor.DisplayName)
	assert.Equal(t, "v1", p.descriptor.APIVersion)
	assert.Equal(t, "filesystem", p.descriptor.Category)
	assert.Contains(t, p.descriptor.Capabilities, provider.CapabilityFrom)
	assert.Contains(t, p.descriptor.Capabilities, provider.CapabilityAction)
}

func TestDirectoryProvider_Descriptor(t *testing.T) {
	p := NewDirectoryProvider()
	desc := p.Descriptor()

	assert.NotNil(t, desc)
	assert.Equal(t, "directory", desc.Name)
	assert.NotNil(t, desc.Schema.Properties)
	assert.Contains(t, desc.Schema.Properties, "operation")
	assert.Contains(t, desc.Schema.Properties, "path")
	assert.Contains(t, desc.Schema.Properties, "recursive")
	assert.Contains(t, desc.Schema.Properties, "maxDepth")
	assert.Contains(t, desc.Schema.Properties, "includeContent")
	assert.Contains(t, desc.Schema.Properties, "filterGlob")
	assert.Contains(t, desc.Schema.Properties, "filterRegex")
	assert.Contains(t, desc.Schema.Properties, "excludeHidden")
	assert.Contains(t, desc.Schema.Properties, "checksum")
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityFrom])
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityAction])
	assert.NotEmpty(t, desc.Examples)
	assert.NotEmpty(t, desc.Tags)
}

func TestDirectoryProvider_Execute_InvalidInput(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()

	t.Run("wrong input type", func(t *testing.T) {
		_, err := p.Execute(ctx, "not a map")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected map[string]any")
	})

	t.Run("missing operation", func(t *testing.T) {
		_, err := p.Execute(ctx, map[string]any{"path": "."})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "operation is required")
	})

	t.Run("missing path", func(t *testing.T) {
		_, err := p.Execute(ctx, map[string]any{"operation": "list"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "path is required")
	})

	t.Run("unsupported operation", func(t *testing.T) {
		_, err := p.Execute(ctx, map[string]any{"operation": "invalid", "path": "."})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported operation")
	})
}

// =============================================================================
// List operation tests
// =============================================================================

func TestDirectoryProvider_List_FlatDirectory(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("hello"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "file2.go"), []byte("package main"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o755))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
	})

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)

	assert.Equal(t, 3, data["totalCount"])
	assert.Equal(t, 1, data["dirCount"])
	assert.Equal(t, 2, data["fileCount"])
	assert.Equal(t, dir, data["basePath"])

	entries := data["entries"].([]map[string]any)
	assert.Len(t, entries, 3)

	for _, e := range entries {
		assert.NotEmpty(t, e["path"])
		assert.NotEmpty(t, e["absolutePath"])
		assert.NotEmpty(t, e["name"])
		assert.NotEmpty(t, e["mode"])
		assert.NotEmpty(t, e["modTime"])
		assert.NotEmpty(t, e["type"])
	}
}

func TestDirectoryProvider_List_Recursive(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.txt"), []byte("root"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "mid.txt"), []byte("mid"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "deep.txt"), []byte("deep"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
		"recursive": true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	// Should include: root.txt, a/, a/mid.txt, a/b/, a/b/deep.txt
	assert.Equal(t, 5, data["totalCount"])
	assert.Equal(t, 2, data["dirCount"])
	assert.Equal(t, 3, data["fileCount"])
}

func TestDirectoryProvider_List_MaxDepth(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a", "b", "c"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "b", "c", "deep.txt"), []byte("deep"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "shallow.txt"), []byte("shallow"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
		"recursive": true,
		"maxDepth":  1,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	entries := data["entries"].([]map[string]any)

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e["path"].(string))
	}
	assert.Contains(t, names, "a")
	assert.Contains(t, names, "a/shallow.txt")
	assert.Contains(t, names, "a/b")
	assert.NotContains(t, names, "a/b/c")
	assert.NotContains(t, names, "a/b/c/deep.txt")
}

func TestDirectoryProvider_List_FilterGlob(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Readme"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("key: val"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation":  "list",
		"path":       dir,
		"filterGlob": "*.go",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, 2, data["fileCount"])
	entries := data["entries"].([]map[string]any)
	for _, e := range entries {
		assert.Equal(t, ".go", e["extension"])
	}
}

func TestDirectoryProvider_List_FilterGlob_Recursive(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.go"), []byte("package main"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "root.txt"), []byte("text"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "nested.go"), []byte("package sub"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sub", "nested.txt"), []byte("text"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation":  "list",
		"path":       dir,
		"recursive":  true,
		"filterGlob": "*.go",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, 2, data["fileCount"])
	entries := data["entries"].([]map[string]any)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e["path"].(string))
	}
	assert.Contains(t, names, "root.go")
	assert.Contains(t, names, "sub/nested.go")
}

func TestDirectoryProvider_List_FilterRegex(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test_main.py"), []byte("pass"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test_utils.py"), []byte("pass"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.py"), []byte("pass"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation":   "list",
		"path":        dir,
		"filterRegex": "^test_.*\\.py$",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, 2, data["fileCount"])
}

func TestDirectoryProvider_List_MutuallyExclusiveFilters(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	_, err := p.Execute(ctx, map[string]any{
		"operation":   "list",
		"path":        dir,
		"filterGlob":  "*.go",
		"filterRegex": "test_.*",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestDirectoryProvider_List_InvalidRegex(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	_, err := p.Execute(ctx, map[string]any{
		"operation":   "list",
		"path":        dir,
		"filterRegex": "[invalid",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filterRegex")
}

func TestDirectoryProvider_List_ExcludeHidden(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("public"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, ".hiddendir"), 0o755))

	result, err := p.Execute(ctx, map[string]any{
		"operation":     "list",
		"path":          dir,
		"excludeHidden": true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, 1, data["totalCount"])
	entries := data["entries"].([]map[string]any)
	assert.Equal(t, "visible.txt", entries[0]["name"])
}

func TestDirectoryProvider_List_IncludesHiddenByDefault(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "visible.txt"), []byte("public"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, 2, data["totalCount"])
}

func TestDirectoryProvider_List_IncludeContent(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	content := "Hello, World!"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte(content), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation":      "list",
		"path":           dir,
		"includeContent": true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	entries := data["entries"].([]map[string]any)
	require.Len(t, entries, 1)

	assert.Equal(t, content, entries[0]["content"])
	assert.Equal(t, "text", entries[0]["contentEncoding"])
}

func TestDirectoryProvider_List_IncludeContent_BinaryFile(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	binaryData := []byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x01, 0x02, 0x03}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "image.png"), binaryData, 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation":      "list",
		"path":           dir,
		"includeContent": true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	entries := data["entries"].([]map[string]any)
	require.Len(t, entries, 1)

	assert.Equal(t, "base64", entries[0]["contentEncoding"])
	decoded, err := base64.StdEncoding.DecodeString(entries[0]["content"].(string))
	require.NoError(t, err)
	assert.Equal(t, binaryData, decoded)
}

func TestDirectoryProvider_List_IncludeContent_ExceedsMaxFileSize(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	largeContent := make([]byte, 100)
	for i := range largeContent {
		largeContent[i] = 'A'
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "large.txt"), largeContent, 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation":      "list",
		"path":           dir,
		"includeContent": true,
		"maxFileSize":    50,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	entries := data["entries"].([]map[string]any)
	require.Len(t, entries, 1)

	_, hasContent := entries[0]["content"]
	assert.False(t, hasContent)
	assert.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "exceeds maxFileSize")
}

func TestDirectoryProvider_List_Checksum(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation":      "list",
		"path":           dir,
		"includeContent": true,
		"checksum":       "sha256",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	entries := data["entries"].([]map[string]any)
	require.Len(t, entries, 1)

	assert.NotEmpty(t, entries[0]["checksum"])
	assert.Equal(t, "sha256", entries[0]["checksumAlgorithm"])
	assert.Equal(t, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", entries[0]["checksum"])
}

func TestDirectoryProvider_List_Checksum_MD5(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation":      "list",
		"path":           dir,
		"includeContent": true,
		"checksum":       "md5",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	entries := data["entries"].([]map[string]any)
	require.Len(t, entries, 1)

	assert.NotEmpty(t, entries[0]["checksum"])
	assert.Equal(t, "md5", entries[0]["checksumAlgorithm"])
}

func TestDirectoryProvider_List_InvalidChecksum(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	_, err := p.Execute(ctx, map[string]any{
		"operation":      "list",
		"path":           dir,
		"includeContent": true,
		"checksum":       "invalid",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported checksum algorithm")
}

func TestDirectoryProvider_List_SkipsSymlinks(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	targetFile := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(targetFile, []byte("target"), 0o644))
	require.NoError(t, os.Symlink(targetFile, filepath.Join(dir, "link.txt")))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, 1, data["fileCount"])
	entries := data["entries"].([]map[string]any)
	assert.Equal(t, "target.txt", entries[0]["name"])
}

func TestDirectoryProvider_List_NonexistentDir(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()

	_, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      "/nonexistent/directory",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestDirectoryProvider_List_PathIsFile(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	filePath := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))

	_, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      filePath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestDirectoryProvider_List_EmptyDirectory(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, 0, data["totalCount"])
	assert.Equal(t, 0, data["dirCount"])
	assert.Equal(t, 0, data["fileCount"])
	entries := data["entries"].([]map[string]any)
	assert.Empty(t, entries)
}

func TestDirectoryProvider_List_FileMetadata(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	entries := data["entries"].([]map[string]any)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "test.go", e["name"])
	assert.Equal(t, ".go", e["extension"])
	assert.Equal(t, "file", e["type"])
	assert.False(t, e["isDir"].(bool))
	assert.NotEmpty(t, e["mode"])
	assert.NotEmpty(t, e["modTime"])
	assert.Equal(t, int64(12), e["size"])
}

func TestDirectoryProvider_List_DirMetadata(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.Mkdir(filepath.Join(dir, "subdir"), 0o755))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	entries := data["entries"].([]map[string]any)
	require.Len(t, entries, 1)

	e := entries[0]
	assert.Equal(t, "subdir", e["name"])
	assert.Equal(t, "dir", e["type"])
	assert.True(t, e["isDir"].(bool))
}

func TestDirectoryProvider_List_InvalidMaxDepth(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	t.Run("too low", func(t *testing.T) {
		_, err := p.Execute(ctx, map[string]any{
			"operation": "list",
			"path":      dir,
			"maxDepth":  0,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "maxDepth must be between")
	})

	t.Run("too high", func(t *testing.T) {
		_, err := p.Execute(ctx, map[string]any{
			"operation": "list",
			"path":      dir,
			"maxDepth":  100,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "maxDepth must be between")
	})
}

func TestDirectoryProvider_List_TotalSize(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.txt"), []byte("aaaa"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "b.txt"), []byte("bbbbbb"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, int64(10), data["totalSize"])
}

// =============================================================================
// Mkdir operation tests
// =============================================================================

func TestDirectoryProvider_Mkdir_Simple(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	newDir := filepath.Join(dir, "newdir")

	result, err := p.Execute(ctx, map[string]any{
		"operation": "mkdir",
		"path":      newDir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.Equal(t, "mkdir", data["operation"])

	info, err := os.Stat(newDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestDirectoryProvider_Mkdir_WithCreateDirs(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	newDir := filepath.Join(dir, "a", "b", "c")

	result, err := p.Execute(ctx, map[string]any{
		"operation":  "mkdir",
		"path":       newDir,
		"createDirs": true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))

	info, err := os.Stat(newDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestDirectoryProvider_Mkdir_WithoutCreateDirs_NestedFails(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	newDir := filepath.Join(dir, "a", "b", "c")

	_, err := p.Execute(ctx, map[string]any{
		"operation": "mkdir",
		"path":      newDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create directory")
}

func TestDirectoryProvider_Mkdir_AlreadyExists(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	existingDir := filepath.Join(dir, "existing")
	require.NoError(t, os.Mkdir(existingDir, 0o755))

	_, err := p.Execute(ctx, map[string]any{
		"operation": "mkdir",
		"path":      existingDir,
	})
	require.Error(t, err)

	result, err := p.Execute(ctx, map[string]any{
		"operation":  "mkdir",
		"path":       existingDir,
		"createDirs": true,
	})
	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
}

// =============================================================================
// Rmdir operation tests
// =============================================================================

func TestDirectoryProvider_Rmdir_EmptyDir(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	targetDir := filepath.Join(dir, "removeme")
	require.NoError(t, os.Mkdir(targetDir, 0o755))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "rmdir",
		"path":      targetDir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.Equal(t, "rmdir", data["operation"])

	_, err = os.Stat(targetDir)
	assert.True(t, os.IsNotExist(err))
}

func TestDirectoryProvider_Rmdir_NonEmptyWithoutForce(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	targetDir := filepath.Join(dir, "notempty")
	require.NoError(t, os.Mkdir(targetDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "file.txt"), []byte("content"), 0o644))

	_, err := p.Execute(ctx, map[string]any{
		"operation": "rmdir",
		"path":      targetDir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to remove directory")
}

func TestDirectoryProvider_Rmdir_NonEmptyWithForce(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	targetDir := filepath.Join(dir, "forcedelete")
	require.NoError(t, os.MkdirAll(filepath.Join(targetDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "file.txt"), []byte("content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(targetDir, "sub", "nested.txt"), []byte("nested"), 0o644))

	result, err := p.Execute(ctx, map[string]any{
		"operation": "rmdir",
		"path":      targetDir,
		"force":     true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))

	_, err = os.Stat(targetDir)
	assert.True(t, os.IsNotExist(err))
}

func TestDirectoryProvider_Rmdir_NonexistentDir(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()

	_, err := p.Execute(ctx, map[string]any{
		"operation": "rmdir",
		"path":      "/nonexistent/directory/path",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestDirectoryProvider_Rmdir_PathIsFile(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	filePath := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))

	_, err := p.Execute(ctx, map[string]any{
		"operation": "rmdir",
		"path":      filePath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

// =============================================================================
// Copy operation tests
// =============================================================================

func TestDirectoryProvider_Copy_Success(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	srcDir := filepath.Join(dir, "source")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file.txt"), []byte("root content"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "sub", "nested.txt"), []byte("nested"), 0o644))

	dstDir := filepath.Join(dir, "destination")

	result, err := p.Execute(ctx, map[string]any{
		"operation":   "copy",
		"path":        srcDir,
		"destination": dstDir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.Equal(t, "copy", data["operation"])

	content, err := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "root content", string(content))

	content, err = os.ReadFile(filepath.Join(dstDir, "sub", "nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested", string(content))
}

func TestDirectoryProvider_Copy_MissingDestination(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	_, err := p.Execute(ctx, map[string]any{
		"operation": "copy",
		"path":      dir,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination is required")
}

func TestDirectoryProvider_Copy_SourceNotExists(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	_, err := p.Execute(ctx, map[string]any{
		"operation":   "copy",
		"path":        "/nonexistent/source",
		"destination": filepath.Join(dir, "dest"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestDirectoryProvider_Copy_SourceNotDirectory(t *testing.T) {
	p := NewDirectoryProvider()
	ctx := context.Background()
	dir := t.TempDir()

	filePath := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("content"), 0o644))

	_, err := p.Execute(ctx, map[string]any{
		"operation":   "copy",
		"path":        filePath,
		"destination": filepath.Join(dir, "dest"),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

// =============================================================================
// Dry-run tests
// =============================================================================

func TestDirectoryProvider_DryRun_List(t *testing.T) {
	p := NewDirectoryProvider()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content"), 0o644))

	ctx := provider.WithDryRun(context.Background(), true)
	result, err := p.Execute(ctx, map[string]any{
		"operation": "list",
		"path":      dir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, 1, data["fileCount"])
}

func TestDirectoryProvider_DryRun_Mkdir(t *testing.T) {
	p := NewDirectoryProvider()
	dir := t.TempDir()
	newDir := filepath.Join(dir, "newdir")

	ctx := provider.WithDryRun(context.Background(), true)
	result, err := p.Execute(ctx, map[string]any{
		"operation": "mkdir",
		"path":      newDir,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "Would create directory")

	_, err = os.Stat(newDir)
	assert.True(t, os.IsNotExist(err))
}

func TestDirectoryProvider_DryRun_Rmdir(t *testing.T) {
	p := NewDirectoryProvider()
	dir := t.TempDir()

	targetDir := filepath.Join(dir, "removeme")
	require.NoError(t, os.Mkdir(targetDir, 0o755))

	ctx := provider.WithDryRun(context.Background(), true)
	result, err := p.Execute(ctx, map[string]any{
		"operation": "rmdir",
		"path":      targetDir,
		"force":     true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "force-remove")

	_, err = os.Stat(targetDir)
	assert.NoError(t, err)
}

func TestDirectoryProvider_DryRun_Copy(t *testing.T) {
	p := NewDirectoryProvider()
	dir := t.TempDir()

	ctx := provider.WithDryRun(context.Background(), true)
	result, err := p.Execute(ctx, map[string]any{
		"operation":   "copy",
		"path":        dir,
		"destination": filepath.Join(dir, "dest"),
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"].(string), "Would copy")
}

// =============================================================================
// Helper function tests
// =============================================================================

func TestIsBinary(t *testing.T) {
	t.Run("text content", func(t *testing.T) {
		assert.False(t, isBinary([]byte("Hello, World!\nThis is text.")))
	})

	t.Run("binary content with null bytes", func(t *testing.T) {
		assert.True(t, isBinary([]byte{0x89, 0x50, 0x4E, 0x47, 0x00, 0x01}))
	})

	t.Run("empty content", func(t *testing.T) {
		assert.False(t, isBinary([]byte{}))
	})
}

func TestMatchesFilter(t *testing.T) {
	t.Run("no filter matches all", func(t *testing.T) {
		assert.True(t, matchesFilter("anything.txt", &listOptions{}))
	})

	t.Run("glob matches", func(t *testing.T) {
		opts := &listOptions{filterGlob: "*.go"}
		assert.True(t, matchesFilter("main.go", opts))
		assert.False(t, matchesFilter("readme.md", opts))
	})

	t.Run("regex matches", func(t *testing.T) {
		opts := &listOptions{filterRegex: regexp.MustCompile(`^test_.*\.py$`)}
		assert.True(t, matchesFilter("test_main.py", opts))
		assert.False(t, matchesFilter("main.py", opts))
	})
}

func TestToInt(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		v, err := toInt(42)
		require.NoError(t, err)
		assert.Equal(t, 42, v)
	})

	t.Run("int64", func(t *testing.T) {
		v, err := toInt(int64(42))
		require.NoError(t, err)
		assert.Equal(t, 42, v)
	})

	t.Run("float64", func(t *testing.T) {
		v, err := toInt(42.0)
		require.NoError(t, err)
		assert.Equal(t, 42, v)
	})

	t.Run("string fails", func(t *testing.T) {
		_, err := toInt("42")
		require.Error(t, err)
	})
}
