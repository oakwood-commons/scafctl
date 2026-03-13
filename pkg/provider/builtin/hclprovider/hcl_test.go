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

func TestHCLProvider_Descriptor(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	desc := p.Descriptor()

	assert.Equal(t, "hcl", desc.Name)
	assert.Equal(t, "HCL", desc.DisplayName)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.NotNil(t, desc.Version)
	assert.NotEmpty(t, desc.Description)
	assert.Equal(t, "data", desc.Category)
	assert.True(t, desc.Beta)
	assert.Contains(t, desc.Capabilities, provider.CapabilityFrom)
	assert.Contains(t, desc.Capabilities, provider.CapabilityTransform)
	assert.NotEmpty(t, desc.Tags)
	assert.Contains(t, desc.Tags, "hcl")
	assert.Contains(t, desc.Tags, "terraform")
	assert.Contains(t, desc.Tags, "opentofu")
	assert.NotEmpty(t, desc.Schema.Properties)
	assert.NotEmpty(t, desc.Examples)
	assert.NotEmpty(t, desc.Links)
	assert.NotEmpty(t, desc.OutputSchemas)
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityFrom])
	assert.NotNil(t, desc.OutputSchemas[provider.CapabilityTransform])
}

func TestHCLProvider_Execute_WithContent(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"content": `
variable "region" {
  type    = string
  default = "us-east-1"
}

resource "aws_instance" "web" {
  ami           = "ami-12345"
  instance_type = "t3.micro"
}
`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	vars := data["variables"].([]any)
	require.Len(t, vars, 1)
	assert.Equal(t, "region", vars[0].(map[string]any)["name"])

	resources := data["resources"].([]any)
	require.Len(t, resources, 1)
	assert.Equal(t, "aws_instance", resources[0].(map[string]any)["type"])
	assert.Equal(t, "web", resources[0].(map[string]any)["name"])

	// Check metadata
	assert.Equal(t, "input.tf", output.Metadata["filename"])
	assert.NotZero(t, output.Metadata["bytes"])
}

func TestHCLProvider_Execute_WithPath(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		Content: []byte(`
variable "env" {
  type    = string
  default = "dev"
}
`),
	}

	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"path": "/tmp/test.tf",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	vars := data["variables"].([]any)
	require.Len(t, vars, 1)
	assert.Equal(t, "env", vars[0].(map[string]any)["name"])
	assert.Equal(t, "dev", vars[0].(map[string]any)["default"])

	// Check metadata references the file path
	assert.Equal(t, "/tmp/test.tf", output.Metadata["filename"])
}

func TestHCLProvider_Execute_MissingInputs(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{}

	output, err := p.Execute(ctx, inputs)
	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "one of 'content', 'path', 'paths', or 'dir' must be provided")
}

func TestHCLProvider_Execute_BothInputs(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"content": `variable "x" {}`,
		"path":    "/tmp/test.tf",
	}

	output, err := p.Execute(ctx, inputs)
	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestHCLProvider_Execute_InvalidInputType(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	output, err := p.Execute(ctx, "not-a-map")
	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

func TestHCLProvider_Execute_DryRun(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"content": `variable "x" {}`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Empty(t, data["variables"].([]any))
	assert.Empty(t, data["resources"].([]any))
	assert.Empty(t, data["modules"].([]any))
	assert.Equal(t, "dry-run", output.Metadata["mode"])
}

func TestHCLProvider_Execute_InvalidHCL(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"content": `this is { not valid hcl !!!`,
	}

	output, err := p.Execute(ctx, inputs)
	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to parse HCL")
}

func TestHCLProvider_Execute_FileReadError(t *testing.T) {
	t.Parallel()
	mockReader := &MockFileReader{
		ReadFileErr: true,
	}

	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"path": "/nonexistent/file.tf",
	}

	output, err := p.Execute(ctx, inputs)
	assert.Error(t, err)
	assert.Nil(t, output)
	assert.Contains(t, err.Error(), "failed to read file")
}

func TestHCLProvider_Execute_EmptyContent(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"content": "",
	}

	// Empty string is still valid content — it parses as empty HCL
	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Empty(t, data["variables"].([]any))
	assert.Empty(t, data["resources"].([]any))
}

func TestHCLProvider_Execute_WithFileReaderFunc(t *testing.T) {
	t.Parallel()

	calledWithPath := ""
	mockReader := &MockFileReader{
		ReadFileFunc: func(path string) ([]byte, error) {
			calledWithPath = path
			return []byte(`module "vpc" { source = "./modules/vpc" }`), nil
		},
	}

	p := NewHCLProvider(WithFileReader(mockReader))
	ctx := context.Background()

	inputs := map[string]any{
		"path": "/my/terraform/main.tf",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "/my/terraform/main.tf", calledWithPath)

	data := output.Data.(map[string]any)
	modules := data["modules"].([]any)
	require.Len(t, modules, 1)
	assert.Equal(t, "vpc", modules[0].(map[string]any)["name"])
	assert.Equal(t, "./modules/vpc", modules[0].(map[string]any)["source"])
}

func TestHCLProvider_SchemaValidation(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	desc := p.Descriptor()

	validator := provider.NewSchemaValidator()

	// Valid input with content
	err := validator.ValidateInputs(map[string]any{
		"content": `variable "x" {}`,
	}, desc.Schema)
	assert.NoError(t, err)

	// Valid input with path
	err = validator.ValidateInputs(map[string]any{
		"path": "./main.tf",
	}, desc.Schema)
	assert.NoError(t, err)

	// Empty input should be valid at schema level (business logic validates)
	err = validator.ValidateInputs(map[string]any{}, desc.Schema)
	assert.NoError(t, err)
}

func TestHCLProvider_Execute_TransformCapability(t *testing.T) {
	t.Parallel()
	// The provider works the same way for both from and transform capabilities
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"content": `
output "result" {
  value       = "hello"
  description = "A test output"
}
`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	outputs := data["outputs"].([]any)
	require.Len(t, outputs, 1)
	assert.Equal(t, "result", outputs[0].(map[string]any)["name"])
	assert.Equal(t, "hello", outputs[0].(map[string]any)["value"])
	assert.Equal(t, "A test output", outputs[0].(map[string]any)["description"])
}

func TestHCLProvider_Execute_Format_UnformattedContent(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	// Poorly formatted HCL – compact attribute assignments with no spacing
	unformatted := `variable "region" {
type=string
default="us-east-1"
}`

	inputs := map[string]any{
		"operation": "format",
		"content":   unformatted,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.True(t, data["changed"].(bool), "changed should be true for unformatted input")
	formatted := data["formatted"].(string)
	assert.NotEmpty(t, formatted)
	assert.Contains(t, formatted, "type")
	assert.Contains(t, formatted, "region")

	meta := output.Metadata
	assert.Equal(t, "format", meta["operation"])
}

func TestHCLProvider_Execute_Format_AlreadyFormatted(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	// Content already in canonical form – hclwrite.Format should return it unchanged.
	alreadyFormatted := `variable "region" {
  type    = string
  default = "us-east-1"
}
`

	inputs := map[string]any{
		"operation": "format",
		"content":   alreadyFormatted,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.False(t, data["changed"].(bool), "changed should be false when content is already canonical")
	assert.Equal(t, alreadyFormatted, data["formatted"].(string))
}

func TestHCLProvider_Execute_Format_WithFilePath(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider(WithFileReader(&MockFileReader{
		Content: []byte(`resource "aws_s3_bucket" "b" {
bucket="my-bucket"
}`),
	}))
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "format",
		"path":      "./main.tf",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.True(t, data["changed"].(bool))
	assert.Contains(t, data["formatted"].(string), "aws_s3_bucket")

	meta := output.Metadata
	assert.Equal(t, "format", meta["operation"])
	assert.Equal(t, "main.tf", filepath.Base(meta["filename"].(string)))
}

func TestHCLProvider_Execute_Format_DryRun(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := provider.WithDryRun(context.Background(), true)

	inputs := map[string]any{
		"operation": "format",
		"content":   `variable "x" { type=string }`,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	data := output.Data.(map[string]any)
	assert.Equal(t, "", data["formatted"])
	assert.Equal(t, false, data["changed"])
	assert.Equal(t, "dry-run", output.Metadata["mode"])
}

func TestHCLProvider_Execute_Format_InvalidOperation(t *testing.T) {
	t.Parallel()
	p := NewHCLProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "unknown",
		"content":   `variable "x" {}`,
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operation")
}
