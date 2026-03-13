// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateHCL_ValidContent(t *testing.T) {
	t.Parallel()
	src := []byte(`variable "region" {
  type    = string
  default = "us-east-1"
}`)
	result := ValidateHCL(src, "main.tf")

	assert.True(t, result["valid"].(bool))
	assert.Equal(t, 0, result["error_count"])
	diags := result["diagnostics"].([]any)
	assert.Empty(t, diags)
}

func TestValidateHCL_InvalidContent(t *testing.T) {
	t.Parallel()
	src := []byte(`this is { not valid hcl !!!`)
	result := ValidateHCL(src, "bad.tf")

	assert.False(t, result["valid"].(bool))
	assert.Greater(t, result["error_count"].(int), 0)

	diags := result["diagnostics"].([]any)
	require.NotEmpty(t, diags)

	first := diags[0].(map[string]any)
	assert.Equal(t, "error", first["severity"])
	assert.NotEmpty(t, first["summary"])
	assert.Contains(t, first, "range")

	rng := first["range"].(map[string]any)
	assert.Equal(t, "bad.tf", rng["filename"])
	assert.Contains(t, rng, "start")
	assert.Contains(t, rng, "end")
}

func TestValidateHCL_EmptyContent(t *testing.T) {
	t.Parallel()
	result := ValidateHCL([]byte(""), "empty.tf")

	assert.True(t, result["valid"].(bool))
	assert.Equal(t, 0, result["error_count"])
}

func TestValidateHCL_DefaultFilename(t *testing.T) {
	t.Parallel()
	src := []byte(`resource "a" "b" {}`)
	result := ValidateHCL(src, "")

	assert.True(t, result["valid"].(bool))
}

func TestHCLProvider_Execute_Validate_ValidContent(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "validate",
		"content": `variable "env" {
  type    = string
  default = "prod"
}`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
	assert.Equal(t, 0, data["error_count"])
	assert.Empty(t, data["diagnostics"].([]any))
	assert.Equal(t, "validate", output.Metadata["operation"])
}

func TestHCLProvider_Execute_Validate_InvalidContent(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "validate",
		"content":   `resource "aws" "x" { bad syntax !!!`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err) // validate returns output, not an error
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.False(t, data["valid"].(bool))
	assert.Greater(t, data["error_count"].(int), 0)
	assert.NotEmpty(t, data["diagnostics"].([]any))
}

func TestHCLProvider_Execute_Validate_WithPath(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		Content: []byte(`output "result" { value = "hello" }`),
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "validate",
		"path":      "./outputs.tf",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
	assert.Equal(t, "outputs.tf", filepath.Base(output.Metadata["filename"].(string)))
}

func TestHCLProvider_Execute_Validate_DryRun(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation": "validate",
		"content":   `variable "x" {}`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
	assert.Equal(t, 0, data["error_count"])
	assert.Equal(t, "dry-run", output.Metadata["mode"])
}

func TestHCLProvider_Execute_Validate_MultiFile(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		ReadFileFunc: func(path string) ([]byte, error) {
			files := map[string]string{
				"valid.tf":   `variable "x" { type = string }`,
				"invalid.tf": `this is not valid {{{`,
			}
			return []byte(files[filepath.Base(path)]), nil
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "validate",
		"paths":     []any{"./valid.tf", "./invalid.tf"},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.False(t, data["valid"].(bool))
	assert.Greater(t, data["error_count"].(int), 0)

	files := data["files"].([]any)
	require.Len(t, files, 2)

	// First file is valid
	f0 := files[0].(map[string]any)
	assert.True(t, f0["valid"].(bool))
	assert.Equal(t, "valid.tf", filepath.Base(f0["filename"].(string)))

	// Second file is invalid
	f1 := files[1].(map[string]any)
	assert.False(t, f1["valid"].(bool))
	assert.Equal(t, "invalid.tf", filepath.Base(f1["filename"].(string)))
}

func TestHCLProvider_Execute_Validate_MultiFileDryRun(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		ReadFileFunc: func(path string) ([]byte, error) {
			return []byte(`variable "x" {}`), nil
		},
	}
	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation": "validate",
		"paths":     []any{"./a.tf", "./b.tf"},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	data := output.Data.(map[string]any)
	assert.True(t, data["valid"].(bool))
	assert.Equal(t, 0, data["error_count"])
	assert.Empty(t, data["files"].([]any))
	assert.Equal(t, "dry-run", output.Metadata["mode"])
}
