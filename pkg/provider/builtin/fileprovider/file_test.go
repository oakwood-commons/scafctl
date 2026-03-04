// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewFileProvider(t *testing.T) {
	p := NewFileProvider()

	assert.NotNil(t, p)
	assert.NotNil(t, p.descriptor)
	assert.Equal(t, "file", p.descriptor.Name)
	assert.Equal(t, "File Provider", p.descriptor.DisplayName)
	assert.Equal(t, "v1", p.descriptor.APIVersion)
	assert.Equal(t, "filesystem", p.descriptor.Category)
	assert.Contains(t, p.descriptor.Capabilities, provider.CapabilityFrom)
	assert.Contains(t, p.descriptor.Capabilities, provider.CapabilityAction)
	assert.Contains(t, p.descriptor.Capabilities, provider.CapabilityTransform)
}

func TestFileProvider_Descriptor(t *testing.T) {
	p := NewFileProvider()
	desc := p.Descriptor()

	assert.NotNil(t, desc)
	assert.Equal(t, "file", desc.Name)
	assert.NotNil(t, desc.Schema.Properties)
	assert.Contains(t, desc.Schema.Properties, "operation")
	assert.Contains(t, desc.Schema.Properties, "path")
	assert.Contains(t, desc.Schema.Properties, "content")
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityFrom].Properties)
}

func TestFileProvider_Execute_Read_Success(t *testing.T) {
	p := NewFileProvider()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	content := "Hello, World!"
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	tmpFile.Close()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "read",
		"path":      tmpFile.Name(),
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.Equal(t, content, data["content"])
	assert.NotEmpty(t, data["path"])
	assert.Greater(t, data["size"], int64(0))
}

func TestFileProvider_Execute_Read_FileNotExists(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "read",
		"path":      "/nonexistent/file.txt",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestFileProvider_Execute_Read_Directory(t *testing.T) {
	p := NewFileProvider()

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "test-dir-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "read",
		"path":      tmpDir,
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "is a directory")
}

func TestFileProvider_Execute_Write_Success(t *testing.T) {
	p := NewFileProvider()

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "test-dir-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "test.txt")
	content := "Test content"

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write",
		"path":      tmpFile,
		"content":   content,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.NotEmpty(t, data["path"])

	// Verify file was written
	readContent, err := os.ReadFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, content, string(readContent))
}

func TestFileProvider_Execute_Write_WithCreateDirs(t *testing.T) {
	p := NewFileProvider()

	// Create a temporary directory
	tmpDir, err := os.MkdirTemp("", "test-dir-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "nested", "dir", "test.txt")
	content := "Test content"

	ctx := context.Background()
	inputs := map[string]any{
		"operation":  "write",
		"path":       tmpFile,
		"content":    content,
		"createDirs": true,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))

	// Verify file was written
	readContent, err := os.ReadFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, content, string(readContent))
}

func TestFileProvider_Execute_Write_MissingContent(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write",
		"path":      "/tmp/test.txt",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "content is required")
}

func TestFileProvider_Execute_Write_InvalidPath(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write",
		"path":      "/nonexistent/deeply/nested/path/file.txt",
		"content":   "test",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestFileProvider_Execute_Exists_True(t *testing.T) {
	p := NewFileProvider()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "exists",
		"path":      tmpFile.Name(),
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["exists"].(bool))
	assert.NotEmpty(t, data["path"])
}

func TestFileProvider_Execute_Exists_False(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "exists",
		"path":      "/nonexistent/file.txt",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.False(t, data["exists"].(bool))
}

func TestFileProvider_Execute_Delete_Success(t *testing.T) {
	p := NewFileProvider()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	require.NoError(t, err)
	tmpFileName := tmpFile.Name()
	tmpFile.Close()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "delete",
		"path":      tmpFileName,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))

	// Verify file was deleted
	_, err = os.Stat(tmpFileName)
	assert.True(t, os.IsNotExist(err))
}

func TestFileProvider_Execute_Delete_FileNotExists(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "delete",
		"path":      "/nonexistent/file.txt",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestFileProvider_Execute_DryRun_Read(t *testing.T) {
	p := NewFileProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation": "read",
		"path":      "/some/path.txt",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.Equal(t, "[DRY RUN] Would read file content", data["content"])
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"], "Would read file")
}

func TestFileProvider_Execute_DryRun_Write(t *testing.T) {
	p := NewFileProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation": "write",
		"path":      "/some/path.txt",
		"content":   "test content",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"], "Would write")
}

func TestFileProvider_Execute_DryRun_Exists(t *testing.T) {
	p := NewFileProvider()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation": "exists",
		"path":      tmpFile.Name(),
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	// Exists operation should actually check even in dry-run
	assert.True(t, data["exists"].(bool))
}

func TestFileProvider_Execute_DryRun_Delete(t *testing.T) {
	p := NewFileProvider()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation": "delete",
		"path":      "/some/path.txt",
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.True(t, data["_dryRun"].(bool))
	assert.Contains(t, data["_message"], "Would delete")
}

func TestFileProvider_Execute_MissingOperation(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"path": "/some/path.txt",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "operation is required")
}

func TestFileProvider_Execute_MissingPath(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "read",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "path is required")
}

func TestFileProvider_Execute_UnsupportedOperation(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "unknown",
		"path":      "/some/path.txt",
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unsupported operation")
}

func TestFileProvider_Execute_RelativePath(t *testing.T) {
	p := NewFileProvider()

	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Get relative path
	cwd, err := os.Getwd()
	require.NoError(t, err)
	relPath, err := filepath.Rel(cwd, tmpFile.Name())
	require.NoError(t, err)

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "exists",
		"path":      relPath,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	// Should convert to absolute path
	assert.True(t, filepath.IsAbs(data["path"].(string)))
}

// --- write-tree tests ---

func TestFileProvider_WriteTree_BasicNestedDirs(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "file1.txt", "content": "hello"},
			map[string]any{"path": "sub/file2.txt", "content": "world"},
			map[string]any{"path": "sub/deep/file3.txt", "content": "nested"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.Equal(t, "write-tree", data["operation"])
	assert.Equal(t, 3, data["filesWritten"])
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"file1.txt", "sub/file2.txt", "sub/deep/file3.txt"}, paths)

	// Verify files actually exist
	b1, err := os.ReadFile(filepath.Join(tmpDir, "file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b1))

	b2, err := os.ReadFile(filepath.Join(tmpDir, "sub", "file2.txt"))
	require.NoError(t, err)
	assert.Equal(t, "world", string(b2))

	b3, err := os.ReadFile(filepath.Join(tmpDir, "sub", "deep", "file3.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested", string(b3))
}

func TestFileProvider_WriteTree_OutputPathStripExtension(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "deployment.yaml.tpl", "content": "apiVersion: apps/v1"},
			map[string]any{"path": "configs/app.conf.tpl", "content": "key=value"},
		},
		"outputPath": `{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}`,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, 2, data["filesWritten"])
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"deployment.yaml", "configs/app.conf"}, paths)

	// Verify content
	b, err := os.ReadFile(filepath.Join(tmpDir, "deployment.yaml"))
	require.NoError(t, err)
	assert.Equal(t, "apiVersion: apps/v1", string(b))
}

func TestFileProvider_WriteTree_OutputPathReplaceExtension(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "app/main.go.tpl", "content": "package main"},
		},
		"outputPath": `{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}`,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"app/main.go"}, paths)
}

func TestFileProvider_WriteTree_OutputPathFlatten(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "a/b/file.txt", "content": "flat"},
		},
		// Flatten: ignore directory, just use the filename
		"outputPath": `{{ .__fileName }}`,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"file.txt"}, paths)

	b, err := os.ReadFile(filepath.Join(tmpDir, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "flat", string(b))
}

func TestFileProvider_WriteTree_NoOutputPath(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "keep/original/path.txt", "content": "preserved"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"keep/original/path.txt"}, paths)

	b, err := os.ReadFile(filepath.Join(tmpDir, "keep", "original", "path.txt"))
	require.NoError(t, err)
	assert.Equal(t, "preserved", string(b))
}

func TestFileProvider_WriteTree_PathTraversalBlocked(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "../../etc/passwd", "content": "evil"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestFileProvider_WriteTree_OutputPathTraversalBlocked(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "safe.txt", "content": "evil"},
		},
		"outputPath": `../../escaped`,
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestFileProvider_WriteTree_EmptyEntries(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries":   []any{},
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.Equal(t, 0, data["filesWritten"])
}

func TestFileProvider_WriteTree_MissingBasePath(t *testing.T) {
	p := NewFileProvider()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"entries": []any{
			map[string]any{"path": "x.txt", "content": "y"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "basePath is required")
}

func TestFileProvider_WriteTree_MissingEntries(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "entries is required")
}

func TestFileProvider_WriteTree_InvalidEntryType(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries":   []any{"not a map"},
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "entries[0] must be a map")
}

func TestFileProvider_WriteTree_MissingEntryPath(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"content": "no path here"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "entries[0].path is required")
}

func TestFileProvider_WriteTree_MissingEntryContent(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "file.txt"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "entries[0].content is required")
}

func TestFileProvider_WriteTree_DryRun(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "a.txt", "content": "alpha"},
			map[string]any{"path": "sub/b.txt", "content": "beta"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))
	assert.True(t, data["_dryRun"].(bool))
	assert.Equal(t, 2, data["filesWritten"])
	assert.Contains(t, data["_message"], "Would write 2 files")
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"a.txt", "sub/b.txt"}, paths)

	// Ensure no files were actually written
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestFileProvider_WriteTree_DryRunWithOutputPath(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation":  "write-tree",
		"basePath":   tmpDir,
		"outputPath": `{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}`,
		"entries": []any{
			map[string]any{"path": "app/main.go.tpl", "content": "package main"},
			map[string]any{"path": "deploy.yaml.tpl", "content": "apiVersion: v1"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"app/main.go", "deploy.yaml"}, paths)
}

func TestFileProvider_WriteTree_Overwrite(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	// Pre-create a file
	err := os.WriteFile(filepath.Join(tmpDir, "existing.txt"), []byte("old"), 0o644)
	require.NoError(t, err)

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "existing.txt", "content": "new content"},
		},
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.True(t, data["success"].(bool))

	b, err := os.ReadFile(filepath.Join(tmpDir, "existing.txt"))
	require.NoError(t, err)
	assert.Equal(t, "new content", string(b))
}

func TestFileProvider_WriteTree_OutputPathWithSprigFunctions(t *testing.T) {
	// Register extension functions (Sprig etc.) for this test
	gotmpl.SetExtensionFuncMapFactory(gotmplext.AllFuncMap)

	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "SRC/MyFile.txt", "content": "lowered"},
		},
		"outputPath": `{{ lower .__filePath }}`,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"src/myfile.txt"}, paths)

	b, err := os.ReadFile(filepath.Join(tmpDir, "src", "myfile.txt"))
	require.NoError(t, err)
	assert.Equal(t, "lowered", string(b))
}

func TestFileProvider_WriteTree_FileVarsComputed(t *testing.T) {
	// Validate that all __file* variables are correctly populated
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{
				"path":    "charts/templates/deployment.yaml.tpl",
				"content": "test",
			},
		},
		// Template that uses all __file* vars to build the output path
		"outputPath": `{{ .__fileDir }}/{{ .__fileStem }}`,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	paths := data["paths"].([]string)
	// __fileDir = "charts/templates", __fileStem = "deployment.yaml" (stem of "deployment.yaml.tpl" is "deployment.yaml")
	assert.Equal(t, []string{"charts/templates/deployment.yaml"}, paths)
}

func TestFileProvider_WriteTree_RootLevelFileDir(t *testing.T) {
	// When a file is at the root level, __fileDir should be empty
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "readme.md.tpl", "content": "# Hello"},
		},
		// Should produce just the stem since __fileDir is empty
		"outputPath": `{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}`,
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	paths := data["paths"].([]string)
	assert.Equal(t, []string{"readme.md"}, paths)
}

func TestFileProvider_WriteTree_DoesNotRequirePath(t *testing.T) {
	// write-tree should NOT require the "path" field (it uses basePath instead)
	p := NewFileProvider()
	tmpDir := t.TempDir()

	ctx := context.Background()
	inputs := map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries":   []any{},
	}

	result, err := p.Execute(ctx, inputs)

	require.NoError(t, err)
	require.NotNil(t, result)
}
