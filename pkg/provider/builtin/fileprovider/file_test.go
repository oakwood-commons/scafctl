// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fileprovider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	gotmplext "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/settings"
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
	assert.Contains(t, data["_message"], "Would create")
	assert.Equal(t, "created", data["_plannedStatus"])
	assert.Equal(t, "error", data["_strategy"])
}

func TestFileProvider_DryRun_Write_PlannedStatus(t *testing.T) {
	t.Parallel()

	t.Run("new file all strategies produce created", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "new.txt")

		for _, strategy := range []string{"error", "overwrite", "skip", "skip-unchanged", "append"} {
			ctx := provider.WithDryRun(context.Background(), true)
			inputs := map[string]any{
				"operation":  "write",
				"path":       target,
				"content":    "hello",
				"onConflict": strategy,
			}
			result, err := p.Execute(ctx, inputs)
			require.NoError(t, err, "strategy: %s", strategy)
			data := result.Data.(map[string]any)
			assert.Equal(t, "created", data["_plannedStatus"], "strategy: %s", strategy)
		}
	})

	t.Run("existing file error strategy produces error", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "new content",
			"onConflict": "error",
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "error", data["_plannedStatus"])
	})

	t.Run("existing file skip strategy produces skipped", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "new content",
			"onConflict": "skip",
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "skipped", data["_plannedStatus"])
	})

	t.Run("existing file skip-unchanged same content produces unchanged", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("same content"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "same content",
			"onConflict": "skip-unchanged",
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "unchanged", data["_plannedStatus"])
	})

	t.Run("existing file skip-unchanged different content produces overwritten", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("old content"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "new content",
			"onConflict": "skip-unchanged",
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "overwritten", data["_plannedStatus"])
	})

	t.Run("existing file overwrite strategy produces overwritten", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "new content",
			"onConflict": "overwrite",
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "overwritten", data["_plannedStatus"])
	})

	t.Run("existing file append produces appended", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("line1\n"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "line2\n",
			"onConflict": "append",
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "appended", data["_plannedStatus"])
	})

	t.Run("append dedupe all duplicates produces unchanged", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("line1\nline2\n"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "line1\nline2\n",
			"onConflict": "append",
			"dedupe":     true,
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "unchanged", data["_plannedStatus"])
	})

	t.Run("append dedupe with new lines produces appended", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("line1\n"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "line1\nline2\n",
			"onConflict": "append",
			"dedupe":     true,
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "appended", data["_plannedStatus"])
	})

	t.Run("append empty content produces unchanged", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "",
			"onConflict": "append",
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "unchanged", data["_plannedStatus"])
	})

	t.Run("backup flag reported in dry-run", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "new",
			"onConflict": "overwrite",
			"backup":     true,
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.True(t, data["_backup"].(bool))
	})

	t.Run("context strategy used when no explicit input", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		ctx = provider.WithConflictStrategy(ctx, "skip")
		inputs := map[string]any{
			"operation": "write",
			"path":      target,
			"content":   "new",
		}
		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "skip", data["_strategy"])
		assert.Equal(t, "skipped", data["_plannedStatus"])
	})

	t.Run("no files written in dry-run", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "newfile.txt")

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "hello world",
			"onConflict": "overwrite",
		}
		_, err := p.Execute(ctx, inputs)
		require.NoError(t, err)

		_, statErr := os.Stat(target)
		assert.True(t, os.IsNotExist(statErr), "dry-run should not create files")
	})
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

func TestFileProvider_WriteTree_DryRun_PlannedStatuses(t *testing.T) {
	t.Parallel()

	t.Run("mixed existing and new files", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()

		// Pre-create one file with same content (unchanged) and one with different (overwritten)
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "same.txt"), []byte("same"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "different.txt"), []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write-tree",
			"basePath":   tmpDir,
			"onConflict": "skip-unchanged",
			"entries": []any{
				map[string]any{"path": "same.txt", "content": "same"},
				map[string]any{"path": "different.txt", "content": "new"},
				map[string]any{"path": "new.txt", "content": "hello"},
			},
		}

		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)

		filesStatus := data["filesStatus"].([]map[string]any)
		require.Len(t, filesStatus, 3)
		assert.Equal(t, "unchanged", filesStatus[0]["_plannedStatus"])
		assert.Equal(t, "overwritten", filesStatus[1]["_plannedStatus"])
		assert.Equal(t, "created", filesStatus[2]["_plannedStatus"])

		assert.Equal(t, 1, data["created"])
		assert.Equal(t, 1, data["overwritten"])
		assert.Equal(t, 1, data["unchanged"])
		assert.Equal(t, 0, data["skipped"])
		assert.Equal(t, 0, data["appended"])
		assert.Equal(t, 2, data["filesWritten"]) // created + overwritten

		// Ensure no files were mutated
		content, _ := os.ReadFile(filepath.Join(tmpDir, "different.txt"))
		assert.Equal(t, "old", string(content))
		_, statErr := os.Stat(filepath.Join(tmpDir, "new.txt"))
		assert.True(t, os.IsNotExist(statErr))
	})

	t.Run("per-entry strategy overrides", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "keep.txt"), []byte("keep"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "replace.txt"), []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write-tree",
			"basePath":   tmpDir,
			"onConflict": "skip-unchanged",
			"entries": []any{
				map[string]any{"path": "keep.txt", "content": "new", "onConflict": "skip"},
				map[string]any{"path": "replace.txt", "content": "new", "onConflict": "overwrite"},
			},
		}

		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)

		filesStatus := data["filesStatus"].([]map[string]any)
		require.Len(t, filesStatus, 2)
		assert.Equal(t, "skipped", filesStatus[0]["_plannedStatus"])
		assert.Equal(t, "skip", filesStatus[0]["_strategy"])
		assert.Equal(t, "overwritten", filesStatus[1]["_plannedStatus"])
		assert.Equal(t, "overwrite", filesStatus[1]["_strategy"])
	})

	t.Run("append dedupe in write-tree dry-run", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "lines.txt"), []byte("line1\nline2\n"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write-tree",
			"basePath":   tmpDir,
			"onConflict": "append",
			"dedupe":     true,
			"entries": []any{
				map[string]any{"path": "lines.txt", "content": "line1\nline3\n"},
			},
		}

		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)

		filesStatus := data["filesStatus"].([]map[string]any)
		require.Len(t, filesStatus, 1)
		assert.Equal(t, "appended", filesStatus[0]["_plannedStatus"])
	})

	t.Run("context strategy used in write-tree dry-run", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "exists.txt"), []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		ctx = provider.WithConflictStrategy(ctx, "skip")
		inputs := map[string]any{
			"operation": "write-tree",
			"basePath":  tmpDir,
			"entries": []any{
				map[string]any{"path": "exists.txt", "content": "new"},
			},
		}

		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)

		filesStatus := data["filesStatus"].([]map[string]any)
		require.Len(t, filesStatus, 1)
		assert.Equal(t, "skipped", filesStatus[0]["_plannedStatus"])
		assert.Equal(t, "skip", filesStatus[0]["_strategy"])
		assert.Equal(t, 0, data["filesWritten"])
	})

	t.Run("backup flag reported per entry", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()

		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("old"), 0o600))

		ctx := provider.WithDryRun(context.Background(), true)
		inputs := map[string]any{
			"operation":  "write-tree",
			"basePath":   tmpDir,
			"onConflict": "overwrite",
			"entries": []any{
				map[string]any{"path": "a.txt", "content": "new", "backup": true},
				map[string]any{"path": "b.txt", "content": "hello"},
			},
		}

		result, err := p.Execute(ctx, inputs)
		require.NoError(t, err)
		data := result.Data.(map[string]any)

		filesStatus := data["filesStatus"].([]map[string]any)
		require.Len(t, filesStatus, 2)
		assert.True(t, filesStatus[0]["_backup"].(bool))
		_, hasBackup := filesStatus[1]["_backup"]
		assert.False(t, hasBackup)
	})
}

func TestFileProvider_WriteTree_Overwrite(t *testing.T) {
	p := NewFileProvider()
	tmpDir := t.TempDir()

	// Pre-create a file
	err := os.WriteFile(filepath.Join(tmpDir, "existing.txt"), []byte("old"), 0o644)
	require.NoError(t, err)

	ctx := context.Background()
	inputs := map[string]any{
		"operation":  "write-tree",
		"basePath":   tmpDir,
		"onConflict": "overwrite",
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

// =============================================================================
// Phase 6: Conflict Strategy Write Tests (real file I/O)
// =============================================================================

func TestWrite_NewFile_AllStrategies(t *testing.T) {
	t.Parallel()

	strategies := []string{"error", "overwrite", "skip", "skip-unchanged", "append"}
	for _, strategy := range strategies {
		t.Run(strategy, func(t *testing.T) {
			t.Parallel()
			p := NewFileProvider()
			tmpDir := t.TempDir()
			target := filepath.Join(tmpDir, "new.txt")

			result, err := p.Execute(context.Background(), map[string]any{
				"operation":  "write",
				"path":       target,
				"content":    "hello",
				"onConflict": strategy,
			})

			require.NoError(t, err)
			data := result.Data.(map[string]any)
			assert.Equal(t, "created", data["status"])

			content, err := os.ReadFile(target)
			require.NoError(t, err)
			assert.Equal(t, "hello", string(content))
		})
	}
}

func TestWrite_ExistingFile_Error(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

	_, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "new",
		"onConflict": "error",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "file already exists")

	// Original file unchanged
	content, _ := os.ReadFile(target)
	assert.Equal(t, "old", string(content))
}

func TestWrite_ExistingFile_Error_IdenticalContent_ReturnsUnchanged(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("same content"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "same content",
		"onConflict": "error",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "unchanged", data["status"])
}

func TestWrite_ExistingFile_Error_ReturnsFileConflictError(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("old content"), 0o600))

	_, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "different content",
		"onConflict": "error",
	})

	require.Error(t, err)
	var conflictErr *FileConflictError
	require.ErrorAs(t, err, &conflictErr)
	assert.Len(t, conflictErr.Changed, 1)
	// Should contain the path provided in inputs
	assert.Equal(t, target, conflictErr.Changed[0])
}

func TestWrite_ExistingFile_Skip(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "new",
		"onConflict": "skip",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "skipped", data["status"])

	content, _ := os.ReadFile(target)
	assert.Equal(t, "old", string(content))
}

func TestWrite_ExistingFile_SkipUnchanged_Same(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("same content"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "same content",
		"onConflict": "skip-unchanged",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "unchanged", data["status"])
}

func TestWrite_ExistingFile_SkipUnchanged_Different(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("old content"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "new content",
		"onConflict": "skip-unchanged",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "overwritten", data["status"])

	content, _ := os.ReadFile(target)
	assert.Equal(t, "new content", string(content))
}

func TestWrite_ExistingFile_Overwrite(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "new",
		"onConflict": "overwrite",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "overwritten", data["status"])

	content, _ := os.ReadFile(target)
	assert.Equal(t, "new", string(content))
}

func TestWrite_ExistingFile_Append_Raw(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("line1\n"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "line2\n",
		"onConflict": "append",
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "appended", data["status"])

	content, _ := os.ReadFile(target)
	assert.Equal(t, "line1\nline2\n", string(content))
}

func TestWrite_ExistingFile_Append_Dedupe(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("line1\nline2\n"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "line2\nline3\n",
		"onConflict": "append",
		"dedupe":     true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "appended", data["status"])

	content, _ := os.ReadFile(target)
	assert.Contains(t, string(content), "line3")
	// line2 should not be duplicated
	assert.Equal(t, 1, strings.Count(string(content), "line2"))
}

func TestWrite_ExistingFile_Append_Dedupe_AllDuplicates(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("line1\nline2\n"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "line1\nline2\n",
		"onConflict": "append",
		"dedupe":     true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "unchanged", data["status"])

	// File should not have been modified
	content, _ := os.ReadFile(target)
	assert.Equal(t, "line1\nline2\n", string(content))
}

func TestWrite_Backup_OnOverwrite(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("original"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "replacement",
		"onConflict": "overwrite",
		"backup":     true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "overwritten", data["status"])

	backupPath, ok := data["backupPath"].(string)
	require.True(t, ok, "backupPath should be present")
	assert.FileExists(t, backupPath)

	// Backup has old content
	bakContent, _ := os.ReadFile(backupPath)
	assert.Equal(t, "original", string(bakContent))

	// Target has new content
	content, _ := os.ReadFile(target)
	assert.Equal(t, "replacement", string(content))
}

func TestWrite_Backup_MultipleBaks(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("v1"), 0o600))

	// Create .bak manually to force numbered backup
	require.NoError(t, os.WriteFile(target+".bak", []byte("v0"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "v2",
		"onConflict": "overwrite",
		"backup":     true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	backupPath := data["backupPath"].(string)
	assert.Equal(t, target+".bak.1", backupPath)

	bakContent, _ := os.ReadFile(backupPath)
	assert.Equal(t, "v1", string(bakContent))
}

func TestWrite_Backup_OnAppend(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("line1\n"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "line2\n",
		"onConflict": "append",
		"backup":     true,
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "appended", data["status"])

	backupPath, ok := data["backupPath"].(string)
	require.True(t, ok, "backupPath should be present when appending")
	bakContent, _ := os.ReadFile(backupPath)
	assert.Equal(t, "line1\n", string(bakContent))
}

func TestWrite_Backup_PreservesPermissions(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o644))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "new",
		"onConflict": "overwrite",
		"backup":     true,
	})

	require.NoError(t, err)
	backupPath := result.Data.(map[string]any)["backupPath"].(string)

	info, err := os.Stat(backupPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func TestWrite_Backup_CapExceeded_Error(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("current"), 0o600))

	// Fill up all backup slots: .bak plus .bak.1 through .bak.(DefaultMaxBackups-1)
	// matches backupFile's loop bound: for i := 1; i < DefaultMaxBackups
	require.NoError(t, os.WriteFile(target+".bak", []byte("bak"), 0o600))
	for i := 1; i < settings.DefaultMaxBackups; i++ {
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "exists.txt.bak."+itoa(i)), []byte("bak"), 0o600))
	}

	_, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "new",
		"onConflict": "overwrite",
		"backup":     true,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup limit reached")

	// Original file unchanged
	content, _ := os.ReadFile(target)
	assert.Equal(t, "current", string(content))
}

func TestWrite_Dedupe_NonAppend_Error(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "test.txt")

	for _, strategy := range []string{"error", "overwrite", "skip", "skip-unchanged"} {
		t.Run(strategy, func(t *testing.T) {
			t.Parallel()
			_, err := p.Execute(context.Background(), map[string]any{
				"operation":  "write",
				"path":       target,
				"content":    "hello",
				"onConflict": strategy,
				"dedupe":     true,
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), "dedupe can only be used with append")
		})
	}
}

func TestWrite_Append_EmptyContent(t *testing.T) {
	t.Parallel()

	t.Run("existing file stays unchanged", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "exists.txt")
		require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

		result, err := p.Execute(context.Background(), map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "",
			"onConflict": "append",
		})

		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "unchanged", data["status"])
	})

	t.Run("missing file not created", func(t *testing.T) {
		t.Parallel()
		p := NewFileProvider()
		tmpDir := t.TempDir()
		target := filepath.Join(tmpDir, "missing.txt")

		result, err := p.Execute(context.Background(), map[string]any{
			"operation":  "write",
			"path":       target,
			"content":    "",
			"onConflict": "append",
		})

		require.NoError(t, err)
		data := result.Data.(map[string]any)
		assert.Equal(t, "unchanged", data["status"])

		_, statErr := os.Stat(target)
		assert.True(t, os.IsNotExist(statErr))
	})
}

func TestWrite_Error_ReturnsGoError(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "new",
		"onConflict": "error",
	})

	// Should return a Go error, NOT a structured output with success: false
	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "file already exists")
}

// =============================================================================
// Phase 6: Write-Tree Conflict Strategy Tests (real file I/O)
// =============================================================================

func TestWriteTree_MixedStrategies(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()

	// Pre-create files
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "skip-me.txt"), []byte("old"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "replace-me.txt"), []byte("old"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write-tree",
		"basePath":   tmpDir,
		"onConflict": "skip-unchanged",
		"entries": []any{
			map[string]any{"path": "skip-me.txt", "content": "new", "onConflict": "skip"},
			map[string]any{"path": "replace-me.txt", "content": "new", "onConflict": "overwrite"},
			map[string]any{"path": "create-me.txt", "content": "brand new"},
		},
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	filesStatus := data["filesStatus"].([]map[string]any)
	require.Len(t, filesStatus, 3)
	assert.Equal(t, "skipped", filesStatus[0]["status"])
	assert.Equal(t, "overwritten", filesStatus[1]["status"])
	assert.Equal(t, "created", filesStatus[2]["status"])

	// Skip leaves old content
	content, _ := os.ReadFile(filepath.Join(tmpDir, "skip-me.txt"))
	assert.Equal(t, "old", string(content))

	// Overwrite replaces
	content, _ = os.ReadFile(filepath.Join(tmpDir, "replace-me.txt"))
	assert.Equal(t, "new", string(content))

	// New file created
	content, _ = os.ReadFile(filepath.Join(tmpDir, "create-me.txt"))
	assert.Equal(t, "brand new", string(content))
}

func TestWriteTree_SummaryCounts(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()

	// Set up: one existing with same content, one with different, one new
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "same.txt"), []byte("same"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "different.txt"), []byte("old"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write-tree",
		"basePath":   tmpDir,
		"onConflict": "skip-unchanged",
		"entries": []any{
			map[string]any{"path": "same.txt", "content": "same"},
			map[string]any{"path": "different.txt", "content": "new"},
			map[string]any{"path": "new.txt", "content": "hello"},
		},
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	assert.Equal(t, 1, data["unchanged"])
	assert.Equal(t, 1, data["overwritten"])
	assert.Equal(t, 1, data["created"])
	assert.Equal(t, 0, data["skipped"])
	assert.Equal(t, 0, data["appended"])
	assert.Equal(t, 2, data["filesWritten"]) // created + overwritten
}

func TestWriteTree_ContextDefault(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "exists.txt"), []byte("old"), 0o600))

	ctx := provider.WithConflictStrategy(context.Background(), "skip")
	result, err := p.Execute(ctx, map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "exists.txt", "content": "new"},
		},
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	filesStatus := data["filesStatus"].([]map[string]any)
	assert.Equal(t, "skipped", filesStatus[0]["status"])

	// File unchanged
	content, _ := os.ReadFile(filepath.Join(tmpDir, "exists.txt"))
	assert.Equal(t, "old", string(content))
}

func TestWriteTree_AppendDedupe_PerEntry(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "gitignore"), []byte("node_modules\n.env\n"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write-tree",
		"basePath":   tmpDir,
		"onConflict": "append",
		"entries": []any{
			map[string]any{
				"path":    "gitignore",
				"content": "node_modules\ndist\n",
				"dedupe":  true,
			},
		},
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	filesStatus := data["filesStatus"].([]map[string]any)
	assert.Equal(t, "appended", filesStatus[0]["status"])

	content, _ := os.ReadFile(filepath.Join(tmpDir, "gitignore"))
	assert.Contains(t, string(content), "dist")
	assert.Equal(t, 1, strings.Count(string(content), "node_modules"))
}

func TestWriteTree_BackupPerEntry(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("old-a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("old-b"), 0o600))

	result, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write-tree",
		"basePath":   tmpDir,
		"onConflict": "overwrite",
		"entries": []any{
			map[string]any{"path": "a.txt", "content": "new-a", "backup": true},
			map[string]any{"path": "b.txt", "content": "new-b"},
		},
	})

	require.NoError(t, err)
	data := result.Data.(map[string]any)

	filesStatus := data["filesStatus"].([]map[string]any)
	require.Len(t, filesStatus, 2)

	// a.txt has backup
	assert.NotEmpty(t, filesStatus[0]["backupPath"])
	bakContent, _ := os.ReadFile(filesStatus[0]["backupPath"].(string))
	assert.Equal(t, "old-a", string(bakContent))

	// b.txt has no backup
	_, hasBackup := filesStatus[1]["backupPath"]
	assert.False(t, hasBackup)
}

func TestWriteTree_Error_CheckAll(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0o600))

	_, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write-tree",
		"basePath":   tmpDir,
		"onConflict": "error",
		"entries": []any{
			map[string]any{"path": "a.txt", "content": "new"},
			map[string]any{"path": "new.txt", "content": "new"},
			map[string]any{"path": "b.txt", "content": "new"},
		},
	})

	require.Error(t, err)
	// Default check-all mode: error lists BOTH conflicting files
	assert.Contains(t, err.Error(), "a.txt")
	assert.Contains(t, err.Error(), "b.txt")

	// No files should have been modified (pre-scan fails before writes)
	_, statErr := os.Stat(filepath.Join(tmpDir, "new.txt"))
	assert.True(t, os.IsNotExist(statErr))
}

func TestWriteTree_Error_FailFast(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0o600))

	_, err := p.Execute(context.Background(), map[string]any{
		"operation":  "write-tree",
		"basePath":   tmpDir,
		"onConflict": "error",
		"failFast":   true,
		"entries": []any{
			map[string]any{"path": "new.txt", "content": "new"},
			map[string]any{"path": "a.txt", "content": "new"},
			map[string]any{"path": "b.txt", "content": "new"},
		},
	})

	require.Error(t, err)
	// FailFast mode: the first existing file causes the error
	assert.Contains(t, err.Error(), "file already exists")
	assert.Contains(t, err.Error(), "a.txt")
}

func TestWriteTree_InvalidPerEntryOnConflict(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()

	_, err := p.Execute(context.Background(), map[string]any{
		"operation": "write-tree",
		"basePath":  tmpDir,
		"entries": []any{
			map[string]any{"path": "a.txt", "content": "hello", "onConflict": "invalid-strategy"},
		},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "entries[0].onConflict")
	assert.Contains(t, err.Error(), "invalid strategy")
}

func TestDryRunWriteMessage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status   FileWriteStatus
		contains string
	}{
		{StatusCreated, "Would create"},
		{StatusOverwritten, "Would overwrite"},
		{StatusSkipped, "Would skip"},
		{StatusUnchanged, "content unchanged"},
		{StatusAppended, "Would append"},
		{StatusError, "Would error"},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			t.Parallel()
			msg := dryRunWriteMessage(tt.status, 100, "/tmp/test.txt")
			assert.Contains(t, msg, tt.contains)
		})
	}
}

func TestDryRun_Write_ErrorStrategy_Message(t *testing.T) {
	t.Parallel()
	p := NewFileProvider()
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "exists.txt")
	require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

	ctx := provider.WithDryRun(context.Background(), true)
	inputs := map[string]any{
		"operation":  "write",
		"path":       target,
		"content":    "new content",
		"onConflict": "error",
	}
	result, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	data := result.Data.(map[string]any)
	assert.Equal(t, "error", data["_plannedStatus"])
	assert.Contains(t, data["_message"].(string), "Would error")
}

func TestFileProvider_WhatIf_Operations(t *testing.T) {
	p := NewFileProvider()
	ctx := context.Background()
	desc := p.Descriptor()
	require.NotNil(t, desc.WhatIf)

	tests := []struct {
		name     string
		input    any
		contains string
	}{
		{
			name:     "write",
			input:    map[string]any{"operation": "write", "path": "/tmp/file.txt"},
			contains: "/tmp/file.txt",
		},
		{
			name:     "delete",
			input:    map[string]any{"operation": "delete", "path": "/tmp/file.txt"},
			contains: "/tmp/file.txt",
		},
		{
			name:     "read",
			input:    map[string]any{"operation": "read", "path": "/tmp/file.txt"},
			contains: "/tmp/file.txt",
		},
		{
			name:     "exists",
			input:    map[string]any{"operation": "exists", "path": "/tmp/file.txt"},
			contains: "/tmp/file.txt",
		},
		{
			name:     "write-tree",
			input:    map[string]any{"operation": "write-tree", "path": "/tmp", "basePath": "/tmp/output"},
			contains: "/tmp/output",
		},
		{
			name:     "default operation",
			input:    map[string]any{"operation": "rename", "path": "/tmp/file.txt"},
			contains: "rename",
		},
		{
			name:     "non-map input",
			input:    "not-a-map",
			contains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := desc.WhatIf(ctx, tt.input)
			require.NoError(t, err)
			if tt.contains != "" {
				assert.Contains(t, msg, tt.contains)
			}
		})
	}
}
