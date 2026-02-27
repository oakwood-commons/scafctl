// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"context"
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------- paths (multi-file) ----------

func TestHCLProvider_Execute_Parse_Paths(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		ReadFileFunc: func(path string) ([]byte, error) {
			files := map[string]string{
				"./variables.tf": `variable "region" { type = string }`,
				"./resources.tf": `resource "aws_instance" "web" { ami = "ami-123" }`,
			}
			if c, ok := files[path]; ok {
				return []byte(c), nil
			}
			return nil, fmt.Errorf("file not found: %s", path)
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"paths": []any{"./variables.tf", "./resources.tf"},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)

	// Variables from variables.tf
	vars := data["variables"].([]any)
	require.Len(t, vars, 1)
	assert.Equal(t, "region", vars[0].(map[string]any)["name"])

	// Resources from resources.tf
	resources := data["resources"].([]any)
	require.Len(t, resources, 1)
	assert.Equal(t, "aws_instance", resources[0].(map[string]any)["type"])

	// Metadata should list both filenames and file count
	assert.Equal(t, 2, output.Metadata["files"])
	filenames := output.Metadata["filenames"].([]string)
	assert.Len(t, filenames, 2)
}

func TestHCLProvider_Execute_Parse_Paths_Empty(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"paths": []any{},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must not be empty")
}

func TestHCLProvider_Execute_Parse_Paths_InvalidType(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"paths": "not-an-array",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be an array")
}

func TestHCLProvider_Execute_Parse_Paths_NonStringItem(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"paths": []any{123},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a string")
}

// ---------- dir ----------

func TestHCLProvider_Execute_Parse_Dir(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		DirFiles: []string{"./terraform/main.tf", "./terraform/vars.tf"},
		ReadFileFunc: func(path string) ([]byte, error) {
			files := map[string]string{
				"./terraform/main.tf": `resource "aws_s3_bucket" "b" { bucket = "my-bucket" }`,
				"./terraform/vars.tf": `variable "name" { type = string }`,
			}
			if c, ok := files[path]; ok {
				return []byte(c), nil
			}
			return nil, fmt.Errorf("file not found: %s", path)
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"dir": "./terraform",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	vars := data["variables"].([]any)
	require.Len(t, vars, 1)
	assert.Equal(t, "name", vars[0].(map[string]any)["name"])

	resources := data["resources"].([]any)
	require.Len(t, resources, 1)

	assert.Equal(t, 2, output.Metadata["files"])
}

func TestHCLProvider_Execute_Parse_Dir_Empty(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		DirFiles: []string{}, // No HCL files
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"dir": "./empty-dir",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no .tf or .tf.json files found")
}

func TestHCLProvider_Execute_Parse_Dir_ListError(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		ListHCLFilesFunc: func(dir string) ([]string, error) {
			return nil, fmt.Errorf("permission denied")
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"dir": "./restricted",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied")
}

// ---------- format multi-file ----------

func TestHCLProvider_Execute_Format_Paths(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		ReadFileFunc: func(path string) ([]byte, error) {
			files := map[string]string{
				"./a.tf": `variable "x" {
type=string
}`,
				"./b.tf": `variable "y" {
  type = string
}
`,
			}
			if c, ok := files[path]; ok {
				return []byte(c), nil
			}
			return nil, fmt.Errorf("file not found: %s", path)
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "format",
		"paths":     []any{"./a.tf", "./b.tf"},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.True(t, data["changed"].(bool), "at least one file should have changed")

	files := data["files"].([]any)
	require.Len(t, files, 2)

	// a.tf should have changed
	f0 := files[0].(map[string]any)
	assert.True(t, f0["changed"].(bool))
	assert.Equal(t, "./a.tf", f0["filename"])
	assert.NotEmpty(t, f0["formatted"])

	// b.tf should not have changed (already formatted)
	f1 := files[1].(map[string]any)
	assert.False(t, f1["changed"].(bool))
}

func TestHCLProvider_Execute_Format_Dir(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		DirFiles: []string{"./tf/main.tf", "./tf/vars.tf"},
		ReadFileFunc: func(path string) ([]byte, error) {
			return []byte(`resource "a" "b" {
ami="x"
}`), nil
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "format",
		"dir":       "./tf",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	files := data["files"].([]any)
	require.Len(t, files, 2)
	assert.True(t, files[0].(map[string]any)["changed"].(bool))
	assert.True(t, files[1].(map[string]any)["changed"].(bool))
}

func TestHCLProvider_Execute_Format_MultiDryRun(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		ReadFileFunc: func(path string) ([]byte, error) {
			return []byte(`variable "x" {}`), nil
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation": "format",
		"paths":     []any{"./a.tf", "./b.tf"},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.False(t, data["changed"].(bool))
	assert.Empty(t, data["files"].([]any))
	assert.Equal(t, "dry-run", output.Metadata["mode"])
}

// ---------- validate multi-file with dir ----------

func TestHCLProvider_Execute_Validate_Dir(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		DirFiles: []string{"./tf/ok.tf", "./tf/bad.tf"},
		ReadFileFunc: func(path string) ([]byte, error) {
			files := map[string]string{
				"./tf/ok.tf":  `variable "x" { type = string }`,
				"./tf/bad.tf": `invalid {{{ syntax`,
			}
			if c, ok := files[path]; ok {
				return []byte(c), nil
			}
			return nil, fmt.Errorf("not found: %s", path)
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "validate",
		"dir":       "./tf",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.False(t, data["valid"].(bool))
	assert.Greater(t, data["error_count"].(int), 0)

	files := data["files"].([]any)
	require.Len(t, files, 2)
}

// ---------- mutual exclusivity ----------

func TestHCLProvider_Execute_MutualExclusive_ContentAndPaths(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"content": `variable "x" {}`,
		"paths":   []any{"./a.tf"},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestHCLProvider_Execute_MutualExclusive_PathAndDir(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"path": "./main.tf",
		"dir":  "./terraform",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// ---------- mergeParseResults ----------

func TestMergeParseResults(t *testing.T) {
	t.Parallel()
	target := emptyParseResult()
	source := map[string]any{
		"variables": []any{map[string]any{"name": "a"}},
		"resources": []any{map[string]any{"type": "aws_instance", "name": "b"}},
		"locals":    map[string]any{"key": "val"},
		"terraform": map[string]any{"required_version": ">= 1.0"},
	}

	mergeParseResults(target, source)

	assert.Len(t, target["variables"].([]any), 1)
	assert.Len(t, target["resources"].([]any), 1)
	assert.Equal(t, "val", target["locals"].(map[string]any)["key"])
	assert.Equal(t, ">= 1.0", target["terraform"].(map[string]any)["required_version"])

	// Merge again — should append
	source2 := map[string]any{
		"variables": []any{map[string]any{"name": "c"}},
		"locals":    map[string]any{"other": "thing"},
	}
	mergeParseResults(target, source2)
	assert.Len(t, target["variables"].([]any), 2)
	assert.Equal(t, "thing", target["locals"].(map[string]any)["other"])
	assert.Equal(t, "val", target["locals"].(map[string]any)["key"])
}

// ---------- parse single file metadata ----------

func TestHCLProvider_Execute_Parse_SingleFile_Metadata(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"content": `variable "x" { type = string }`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	// Single file should have "filename" not "filenames"
	assert.Equal(t, "input.tf", output.Metadata["filename"])
	assert.Nil(t, output.Metadata["filenames"])
	assert.Equal(t, 1, output.Metadata["files"])
}

// ---------- osFileReader.ListHCLFiles (unit) ----------

func TestOsFileReader_ListHCLFiles_NonExistentDir(t *testing.T) {
	t.Parallel()
	r := &osFileReader{}
	_, err := r.ListHCLFiles("/nonexistent_dir_12345")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading directory")
}
