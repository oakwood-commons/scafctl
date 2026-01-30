package fileprovider

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
