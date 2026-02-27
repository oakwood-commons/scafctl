// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// ProviderName is the name of this provider.
const ProviderName = "hcl"

// FileReader abstracts filesystem access for testability.
type FileReader interface {
	ReadFile(path string) ([]byte, error)
}

// Option is a functional option for configuring the HCL provider.
type Option func(*HCLProvider)

// WithFileReader sets a custom file reader for testing.
func WithFileReader(r FileReader) Option {
	return func(p *HCLProvider) {
		p.fileReader = r
	}
}

// HCLProvider parses HCL content and extracts structured block information
// (variables, resources, modules, outputs, etc.) from Terraform/OpenTofu
// configuration files.
type HCLProvider struct {
	descriptor *provider.Descriptor
	fileReader FileReader
}

// osFileReader is the default file reader using the OS filesystem.
type osFileReader struct{}

func (r *osFileReader) ReadFile(path string) ([]byte, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("path is a directory, not a file: %s", absPath)
	}
	return os.ReadFile(absPath)
}

// NewHCLProvider creates a new HCL provider instance.
func NewHCLProvider(opts ...Option) *HCLProvider {
	version := semver.MustParse("1.0.0")

	outputSchema := schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
		"variables": schemahelper.ArrayProp("Extracted variable blocks"),
		"resources": schemahelper.ArrayProp("Extracted resource blocks"),
		"data":      schemahelper.ArrayProp("Extracted data source blocks"),
		"modules":   schemahelper.ArrayProp("Extracted module blocks"),
		"outputs":   schemahelper.ArrayProp("Extracted output blocks"),
		"locals":    schemahelper.AnyProp("Extracted locals as key-value pairs"),
		"providers": schemahelper.ArrayProp("Extracted provider configuration blocks"),
		"terraform": schemahelper.AnyProp("Extracted terraform configuration block"),
		"moved":     schemahelper.ArrayProp("Extracted moved blocks"),
		"import":    schemahelper.ArrayProp("Extracted import blocks"),
		"check":     schemahelper.ArrayProp("Extracted check blocks"),
	})

	p := &HCLProvider{
		fileReader: &osFileReader{},
		descriptor: &provider.Descriptor{
			Name:         ProviderName,
			DisplayName:  "HCL Parser",
			Description:  "Parses HCL (HashiCorp Configuration Language) content and extracts structured block information. Supports Terraform and OpenTofu configuration files including variables, resources, modules, outputs, locals, providers, and more.",
			APIVersion:   "v1",
			Version:      version,
			Category:     "data",
			Beta:         true,
			Tags:         []string{"hcl", "terraform", "opentofu", "parse", "config"},
			MockBehavior: "Returns a mock parsed structure with empty block arrays",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityTransform,
			},
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"content": schemahelper.StringProp("Raw HCL content to parse. Provide either 'content' or 'path', not both.",
					schemahelper.WithMaxLength(10485760),
				),
				"path": schemahelper.StringProp("Path to an HCL file to read and parse. Provide either 'content' or 'path', not both.",
					schemahelper.WithMaxLength(4096),
					schemahelper.WithExample("./main.tf"),
				),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom:      outputSchema,
				provider.CapabilityTransform: outputSchema,
			},
			Examples: []provider.Example{
				{
					Name:        "Parse inline HCL",
					Description: "Parse HCL content provided as a string to extract variable definitions",
					YAML: `name: tf-vars
resolve:
  with:
    - provider: hcl
      inputs:
        content: |
          variable "region" {
            type        = string
            default     = "us-east-1"
            description = "AWS region"
          }`,
				},
				{
					Name:        "Parse HCL file",
					Description: "Read and parse a Terraform configuration file",
					YAML: `name: tf-config
resolve:
  with:
    - provider: hcl
      inputs:
        path: ./main.tf`,
				},
				{
					Name:        "Transform HCL from file provider",
					Description: "Chain with the file provider to parse HCL content",
					YAML: `name: tf-data
resolve:
  with:
    - provider: file
      inputs:
        operation: read
        path: ./variables.tf
  transform:
    - provider: hcl
      inputs:
        content: "{{ .resolvers.tf-data.content }}"`,
				},
			},
			Links: []provider.Link{
				{
					Name: "HCL Language",
					URL:  "https://github.com/hashicorp/hcl",
				},
				{
					Name: "OpenTofu",
					URL:  "https://opentofu.org",
				},
			},
		},
	}

	for _, opt := range opts {
		opt(p)
	}

	return p
}

// Descriptor returns the provider's metadata and schema.
func (p *HCLProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute parses HCL content and returns structured block information.
func (p *HCLProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	content, hasContent := inputs["content"].(string)
	path, hasPath := inputs["path"].(string)

	if !hasContent && !hasPath {
		return nil, fmt.Errorf("%s: either 'content' or 'path' must be provided", ProviderName)
	}
	if hasContent && hasPath {
		return nil, fmt.Errorf("%s: provide either 'content' or 'path', not both", ProviderName)
	}

	if provider.DryRunFromContext(ctx) {
		return &provider.Output{
			Data: map[string]any{
				"variables": []any{},
				"resources": []any{},
				"data":      []any{},
				"modules":   []any{},
				"outputs":   []any{},
				"locals":    map[string]any{},
				"providers": []any{},
				"terraform": map[string]any{},
				"moved":     []any{},
				"import":    []any{},
				"check":     []any{},
			},
			Metadata: map[string]any{"mode": "dry-run"},
		}, nil
	}

	var src []byte
	filename := "input.tf"

	if hasPath {
		lgr.V(1).Info("reading HCL file", "path", path)
		data, err := p.fileReader.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: failed to read file %s: %w", ProviderName, path, err)
		}
		src = data
		filename = path
	} else {
		src = []byte(content)
	}

	lgr.V(1).Info("parsing HCL content", "bytes", len(src), "filename", filename)

	result, err := ParseHCL(src, filename)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	varCount, resCount, modCount := countBlocks(result)
	lgr.V(1).Info("provider completed", "provider", ProviderName,
		"variables", varCount,
		"resources", resCount,
		"modules", modCount,
	)

	return &provider.Output{
		Data: result,
		Metadata: map[string]any{
			"filename": filename,
			"bytes":    len(src),
		},
	}, nil
}

// countBlocks safely extracts the lengths of the variables, resources, and modules
// slices from the parsed result map for logging.
func countBlocks(result map[string]any) (variables, resources, modules int) {
	if v, ok := result["variables"].([]any); ok {
		variables = len(v)
	}
	if r, ok := result["resources"].([]any); ok {
		resources = len(r)
	}
	if m, ok := result["modules"].([]any); ok {
		modules = len(m)
	}

	return variables, resources, modules
}
