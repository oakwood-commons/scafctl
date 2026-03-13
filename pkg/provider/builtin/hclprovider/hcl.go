// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package hclprovider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

// ProviderName is the name of this provider.
const ProviderName = "hcl"

// FileReader abstracts filesystem access for testability.
type FileReader interface {
	ReadFile(path string) ([]byte, error)
	// ListHCLFiles returns all .tf and .tf.json files in a directory (non-recursive).
	ListHCLFiles(dir string) ([]string, error)
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

func (r *osFileReader) ListHCLFiles(dir string) ([]string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving dir: %w", err)
	}
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, fmt.Errorf("reading directory: %w", err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tf") || strings.HasSuffix(name, ".tf.json") {
			files = append(files, filepath.Join(absDir, name))
		}
	}
	return files, nil
}

// NewHCLProvider creates a new HCL provider instance.
func NewHCLProvider(opts ...Option) *HCLProvider {
	version := semver.MustParse("2.0.0")

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
			DisplayName:  "HCL",
			Description:  "Processes HCL (HashiCorp Configuration Language) content. Supports parsing into structured data, formatting to canonical style, validating syntax, and generating HCL from structured input. Accepts single files, multiple paths, or a directory of .tf files.",
			APIVersion:   "v1",
			Version:      version,
			Category:     "data",
			Beta:         true,
			Tags:         []string{"hcl", "terraform", "opentofu", "parse", "format", "validate", "generate", "config"},
			MockBehavior: "Returns a mock structure appropriate to the requested operation",
			Capabilities: []provider.Capability{
				provider.CapabilityFrom,
				provider.CapabilityTransform,
			},
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform: 'parse' (default) extracts structured blocks; 'format' canonically formats; 'validate' checks syntax; 'generate' produces HCL from structured input.",
					schemahelper.WithEnum("parse", "format", "validate", "generate")),
				"content": schemahelper.StringProp("Raw HCL content to process. Provide 'content', 'path', 'paths', or 'dir' — these are mutually exclusive.",
					schemahelper.WithMaxLength(10485760),
				),
				"path": schemahelper.StringProp("Path to a single HCL file. Mutually exclusive with 'content', 'paths', and 'dir'.",
					schemahelper.WithMaxLength(4096),
					schemahelper.WithExample("./main.tf"),
				),
				"paths": schemahelper.ArrayProp("Array of HCL file paths to process. Results are merged (parse) or returned per file (format/validate). Mutually exclusive with 'content', 'path', and 'dir'.",
					schemahelper.WithMaxItems(1000),
					schemahelper.WithItems(schemahelper.StringProp("Path to an HCL file")),
				),
				"dir": schemahelper.StringProp("Directory path. All .tf and .tf.json files in the directory are processed. Mutually exclusive with 'content', 'path', and 'paths'.",
					schemahelper.WithMaxLength(4096),
					schemahelper.WithExample("./terraform"),
				),
				"blocks":        schemahelper.AnyProp("Structured block data for the 'generate' operation. Uses the same schema as parse output: {variables: [...], resources: [...], ...}."),
				"output_format": schemahelper.StringProp("Output format for the 'generate' operation: 'hcl' (default) produces native HCL syntax (.tf); 'json' produces Terraform JSON syntax (.tf.json).", schemahelper.WithEnum("hcl", "json")),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom:      outputSchema,
				provider.CapabilityTransform: outputSchema,
			},
			// Note: when operation=format the output schema is {formatted: string, changed: bool}.
			// The schemas above describe the parse operation output.
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
					Name:        "Parse a directory of .tf files",
					Description: "Parse all .tf files in a directory and merge the results",
					YAML: `name: tf-full
resolve:
  with:
    - provider: hcl
      inputs:
        dir: ./terraform`,
				},
				{
					Name:        "Parse multiple specific files",
					Description: "Parse a list of specific .tf files and merge the results",
					YAML: `name: tf-multi
resolve:
  with:
    - provider: hcl
      inputs:
        paths:
          - ./main.tf
          - ./variables.tf
          - ./outputs.tf`,
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
				{
					Name:        "Format inline HCL",
					Description: "Canonically format HCL content; output contains 'formatted' (string) and 'changed' (bool)",
					YAML: `name: tf-fmt
resolve:
  with:
    - provider: hcl
      inputs:
        operation: format
        content: |
          variable "region" {
          type=string
          default="us-east-1"
          }`,
				},
				{
					Name:        "Format a directory",
					Description: "Format all .tf files in a directory; returns per-file results",
					YAML: `name: tf-fmt-dir
resolve:
  with:
    - provider: hcl
      inputs:
        operation: format
        dir: ./terraform`,
				},
				{
					Name:        "Validate HCL syntax",
					Description: "Check HCL for syntax errors without parsing blocks",
					YAML: `name: tf-validate
resolve:
  with:
    - provider: hcl
      inputs:
        operation: validate
        path: ./main.tf`,
				},
				{
					Name:        "Validate a directory",
					Description: "Validate all .tf files in a directory",
					YAML: `name: tf-validate-dir
resolve:
  with:
    - provider: hcl
      inputs:
        operation: validate
        dir: ./terraform`,
				},
				{
					Name:        "Generate HCL from structured data",
					Description: "Produce HCL text from a map following the parse output schema",
					YAML: `name: tf-gen
resolve:
  with:
    - provider: hcl
      inputs:
        operation: generate
        blocks:
          variables:
            - name: region
              type: string
              default: us-east-1
              description: "AWS region"
          resources:
            - type: aws_instance
              name: web
              attributes:
                ami: ami-12345
                instance_type: t3.micro`,
				},
				{
					Name:        "Generate Terraform JSON from structured data",
					Description: "Produce .tf.json format instead of native HCL",
					YAML: `name: tf-gen-json
resolve:
  with:
    - provider: hcl
      inputs:
        operation: generate
        output_format: json
        blocks:
          variables:
            - name: region
              type: string
              default: us-east-1`,
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

// Execute processes HCL content according to the requested operation.
func (p *HCLProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	operation := "parse"
	if op, ok := inputs["operation"].(string); ok && op != "" {
		operation = op
	}

	validOps := map[string]bool{"parse": true, "format": true, "validate": true, "generate": true}
	if !validOps[operation] {
		return nil, fmt.Errorf("%s: unsupported operation %q; must be one of: parse, format, validate, generate", ProviderName, operation)
	}

	// Generate uses "blocks" input, not content/path/paths/dir.
	if operation == "generate" {
		return p.executeGenerate(ctx, lgr, inputs)
	}

	// Resolve source(s) for parse/format/validate.
	sources, err := p.resolveSources(ctx, inputs)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	if provider.DryRunFromContext(ctx) {
		return dryRunOutput(operation, sources), nil
	}

	switch operation {
	case "parse":
		return p.executeParse(lgr, sources)
	case "format":
		return p.executeFormat(lgr, sources)
	case "validate":
		return p.executeValidate(lgr, sources)
	default:
		return nil, fmt.Errorf("%s: unhandled operation %q", ProviderName, operation)
	}
}

// hclSource represents a single unit of HCL content to process.
type hclSource struct {
	filename string
	data     []byte
}

// resolveSources resolves the input specification into one or more HCL source units.
// Exactly one of content, path, paths, or dir must be provided.
func (p *HCLProvider) resolveSources(ctx context.Context, inputs map[string]any) ([]hclSource, error) {
	content, hasContent := inputs["content"].(string)
	path, hasPath := inputs["path"].(string)
	rawPaths, hasPaths := inputs["paths"]
	dir, hasDir := inputs["dir"].(string)

	// Validate mutual exclusivity.
	set := 0
	if hasContent {
		set++
	}
	if hasPath {
		set++
	}
	if hasPaths {
		set++
	}
	if hasDir {
		set++
	}
	if set == 0 {
		return nil, fmt.Errorf("one of 'content', 'path', 'paths', or 'dir' must be provided")
	}
	if set > 1 {
		return nil, fmt.Errorf("'content', 'path', 'paths', and 'dir' are mutually exclusive")
	}

	switch {
	case hasContent:
		return []hclSource{{filename: "input.tf", data: []byte(content)}}, nil

	case hasPath:
		absPath, resolveErr := provider.ResolvePath(ctx, path)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolving path: %w", resolveErr)
		}
		data, err := p.fileReader.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", path, err)
		}
		return []hclSource{{filename: absPath, data: data}}, nil

	case hasPaths:
		return p.resolvePathsList(ctx, rawPaths)

	case hasDir:
		return p.resolveDir(ctx, dir)

	default:
		return nil, fmt.Errorf("one of 'content', 'path', 'paths', or 'dir' must be provided")
	}
}

// resolvePathsList reads multiple files from a paths array.
func (p *HCLProvider) resolvePathsList(ctx context.Context, rawPaths any) ([]hclSource, error) {
	pathSlice, ok := rawPaths.([]any)
	if !ok {
		return nil, fmt.Errorf("'paths' must be an array of strings")
	}
	if len(pathSlice) == 0 {
		return nil, fmt.Errorf("'paths' array must not be empty")
	}
	var sources []hclSource
	for _, raw := range pathSlice {
		filePath, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("each item in 'paths' must be a string, got %T", raw)
		}
		absPath, resolveErr := provider.ResolvePath(ctx, filePath)
		if resolveErr != nil {
			return nil, fmt.Errorf("resolving path %s: %w", filePath, resolveErr)
		}
		data, err := p.fileReader.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
		}
		sources = append(sources, hclSource{filename: absPath, data: data})
	}
	return sources, nil
}

// resolveDir lists all .tf files in a directory and reads them.
func (p *HCLProvider) resolveDir(ctx context.Context, dir string) ([]hclSource, error) {
	absDir, resolveErr := provider.ResolvePath(ctx, dir)
	if resolveErr != nil {
		return nil, fmt.Errorf("resolving dir: %w", resolveErr)
	}
	files, err := p.fileReader.ListHCLFiles(absDir)
	if err != nil {
		return nil, fmt.Errorf("listing directory %s: %w", dir, err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no .tf or .tf.json files found in directory: %s", dir)
	}
	var sources []hclSource
	for _, f := range files {
		data, err := p.fileReader.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", f, err)
		}
		sources = append(sources, hclSource{filename: f, data: data})
	}
	return sources, nil
}

// executeParse parses one or more HCL sources. Multiple sources are merged.
func (p *HCLProvider) executeParse(lgr *logr.Logger, sources []hclSource) (*provider.Output, error) {
	merged := emptyParseResult()
	totalBytes := 0
	var filenames []string

	for _, src := range sources {
		lgr.V(1).Info("parsing HCL content", "bytes", len(src.data), "filename", src.filename)
		result, err := ParseHCL(src.data, src.filename)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ProviderName, err)
		}
		mergeParseResults(merged, result)
		totalBytes += len(src.data)
		filenames = append(filenames, src.filename)
	}

	varCount, resCount, modCount := countBlocks(merged)
	lgr.V(1).Info("provider completed", "provider", ProviderName,
		"operation", "parse",
		"files", len(sources),
		"variables", varCount,
		"resources", resCount,
		"modules", modCount,
	)

	meta := map[string]any{
		"operation": "parse",
		"bytes":     totalBytes,
		"files":     len(sources),
	}
	if len(sources) == 1 {
		meta["filename"] = filenames[0]
	} else {
		meta["filenames"] = filenames
	}

	return &provider.Output{Data: merged, Metadata: meta}, nil
}

// executeFormat formats one or more HCL sources.
func (p *HCLProvider) executeFormat(lgr *logr.Logger, sources []hclSource) (*provider.Output, error) {
	if len(sources) == 1 {
		src := sources[0]
		lgr.V(1).Info("formatting HCL content", "bytes", len(src.data), "filename", src.filename)
		formatted := hclwrite.Format(src.data)
		changed := !bytes.Equal(src.data, formatted)
		lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", "format", "changed", changed)
		return &provider.Output{
			Data: map[string]any{
				"formatted": string(formatted),
				"changed":   changed,
			},
			Metadata: map[string]any{
				"filename":  src.filename,
				"bytes":     len(src.data),
				"operation": "format",
			},
		}, nil
	}

	// Multi-file: return per-file results.
	results := make([]any, 0, len(sources))
	anyChanged := false
	for _, src := range sources {
		lgr.V(1).Info("formatting HCL content", "bytes", len(src.data), "filename", src.filename)
		formatted := hclwrite.Format(src.data)
		changed := !bytes.Equal(src.data, formatted)
		if changed {
			anyChanged = true
		}
		results = append(results, map[string]any{
			"filename":  src.filename,
			"formatted": string(formatted),
			"changed":   changed,
		})
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", "format", "files", len(sources), "anyChanged", anyChanged)
	return &provider.Output{
		Data: map[string]any{
			"files":   results,
			"changed": anyChanged,
		},
		Metadata: map[string]any{
			"operation": "format",
			"files":     len(sources),
		},
	}, nil
}

// executeValidate validates one or more HCL sources.
func (p *HCLProvider) executeValidate(lgr *logr.Logger, sources []hclSource) (*provider.Output, error) {
	if len(sources) == 1 {
		src := sources[0]
		lgr.V(1).Info("validating HCL content", "bytes", len(src.data), "filename", src.filename)
		result := ValidateHCL(src.data, src.filename)
		lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", "validate", "valid", result["valid"])
		return &provider.Output{
			Data: result,
			Metadata: map[string]any{
				"filename":  src.filename,
				"bytes":     len(src.data),
				"operation": "validate",
			},
		}, nil
	}

	// Multi-file: return per-file results with aggregate validity.
	results := make([]any, 0, len(sources))
	allValid := true
	totalErrors := 0
	for _, src := range sources {
		lgr.V(1).Info("validating HCL content", "bytes", len(src.data), "filename", src.filename)
		result := ValidateHCL(src.data, src.filename)
		result["filename"] = src.filename
		if valid, ok := result["valid"].(bool); ok && !valid {
			allValid = false
		}
		if ec, ok := result["error_count"].(int); ok {
			totalErrors += ec
		}
		results = append(results, result)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", "validate", "files", len(sources), "allValid", allValid)
	return &provider.Output{
		Data: map[string]any{
			"valid":       allValid,
			"error_count": totalErrors,
			"files":       results,
		},
		Metadata: map[string]any{
			"operation": "validate",
			"files":     len(sources),
		},
	}, nil
}

// executeGenerate generates HCL from structured input.
func (p *HCLProvider) executeGenerate(ctx context.Context, lgr *logr.Logger, inputs map[string]any) (*provider.Output, error) {
	outputFormat := "hcl"
	if f, ok := inputs["output_format"].(string); ok && f != "" {
		outputFormat = f
	}

	if provider.DryRunFromContext(ctx) {
		return &provider.Output{
			Data:     map[string]any{"hcl": ""},
			Metadata: map[string]any{"mode": "dry-run", "operation": "generate", "output_format": outputFormat},
		}, nil
	}

	blocks, ok := inputs["blocks"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: 'blocks' input is required for the generate operation and must be a map", ProviderName)
	}

	lgr.V(1).Info("generating HCL", "provider", ProviderName, "output_format", outputFormat)

	var generated string
	var err error

	switch outputFormat {
	case "json":
		generated, err = GenerateHCLJSON(blocks)
	default:
		generated, err = GenerateHCL(blocks)
	}

	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "operation", "generate", "output_format", outputFormat, "bytes", len(generated))
	return &provider.Output{
		Data: map[string]any{
			"hcl": generated,
		},
		Metadata: map[string]any{
			"operation":     "generate",
			"output_format": outputFormat,
			"bytes":         len(generated),
		},
	}, nil
}

// dryRunOutput returns an appropriate dry-run response for the given operation.
func dryRunOutput(operation string, sources []hclSource) *provider.Output {
	meta := map[string]any{"mode": "dry-run", "operation": operation}

	switch operation {
	case "format":
		if len(sources) == 1 {
			return &provider.Output{
				Data:     map[string]any{"formatted": "", "changed": false},
				Metadata: meta,
			}
		}
		return &provider.Output{
			Data:     map[string]any{"files": []any{}, "changed": false},
			Metadata: meta,
		}
	case "validate":
		if len(sources) == 1 {
			return &provider.Output{
				Data:     map[string]any{"valid": true, "error_count": 0, "diagnostics": []any{}},
				Metadata: meta,
			}
		}
		return &provider.Output{
			Data:     map[string]any{"valid": true, "error_count": 0, "files": []any{}},
			Metadata: meta,
		}
	default: // parse
		return &provider.Output{
			Data:     emptyParseResult(),
			Metadata: meta,
		}
	}
}

// emptyParseResult returns the empty parse output structure.
func emptyParseResult() map[string]any {
	return map[string]any{
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
	}
}

// mergeParseResults merges the source parse result into the target, appending
// arrays and merging maps.
func mergeParseResults(target, source map[string]any) {
	arrayKeys := []string{"variables", "resources", "data", "modules", "outputs", "providers", "moved", "import", "check"}
	for _, key := range arrayKeys {
		if srcArr, ok := source[key].([]any); ok && len(srcArr) > 0 {
			if tgtArr, ok := target[key].([]any); ok {
				target[key] = append(tgtArr, srcArr...)
			} else {
				target[key] = srcArr
			}
		}
	}

	// Merge locals maps.
	if srcLocals, ok := source["locals"].(map[string]any); ok {
		tgtLocals, _ := target["locals"].(map[string]any)
		if tgtLocals == nil {
			tgtLocals = map[string]any{}
		}
		for k, v := range srcLocals {
			tgtLocals[k] = v
		}
		target["locals"] = tgtLocals
	}

	// Merge terraform block (later file wins for conflicting keys).
	if srcTf, ok := source["terraform"].(map[string]any); ok && len(srcTf) > 0 {
		tgtTf, _ := target["terraform"].(map[string]any)
		if tgtTf == nil {
			tgtTf = map[string]any{}
		}
		for k, v := range srcTf {
			tgtTf[k] = v
		}
		target["terraform"] = tgtTf
	}

	// Merge top-level attributes if present.
	if srcAttrs, ok := source["attributes"].(map[string]any); ok {
		tgtAttrs, _ := target["attributes"].(map[string]any)
		if tgtAttrs == nil {
			tgtAttrs = map[string]any{}
		}
		for k, v := range srcAttrs {
			tgtAttrs[k] = v
		}
		target["attributes"] = tgtAttrs
	}
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
