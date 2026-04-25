// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplprovider

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/oakwood-commons/scafctl/pkg/ptrs"
)

const (
	// ProviderName is the name of the go-template provider
	ProviderName = "go-template"
	// Version is the version of the go-template provider
	Version = "2.0.0"

	// OperationRender is the default single-template render operation.
	OperationRender = "render"
	// OperationRenderTree is the batch render operation for directory trees.
	OperationRenderTree = "render-tree"
)

// GoTemplateProvider provides data transformation using Go templates
type GoTemplateProvider struct {
	descriptor *provider.Descriptor
	service    *gotmpl.Service
}

// NewGoTemplateProvider creates a new Go template provider
func NewGoTemplateProvider() *GoTemplateProvider {
	version, _ := semver.NewVersion(Version)

	return &GoTemplateProvider{
		service: gotmpl.NewService(nil),
		descriptor: &provider.Descriptor{
			Name:        ProviderName,
			DisplayName: "Go Template Provider",
			APIVersion:  "v1",
			Description: "Transform and render data using Go text/template syntax with resolver data from context. Supports single template rendering (operation: render) and batch directory tree rendering (operation: render-tree).",
			Version:     version,
			Category:    "data",
			WhatIf: func(_ context.Context, input any) (string, error) {
				inputs, ok := input.(map[string]any)
				if !ok {
					return "", nil
				}
				operation, _ := inputs["operation"].(string)
				if operation == "" {
					operation = OperationRender
				}
				name, _ := inputs["name"].(string)
				switch operation {
				case OperationRender:
					if name != "" {
						return fmt.Sprintf("Would render template %q", name), nil
					}
					return "Would render Go template", nil
				case OperationRenderTree:
					if name != "" {
						return fmt.Sprintf("Would render template tree %q", name), nil
					}
					return "Would render Go template tree", nil
				default:
					return fmt.Sprintf("Would perform Go template %s", operation), nil
				}
			},
			Capabilities: []provider.Capability{
				provider.CapabilityTransform,
				provider.CapabilityAction,
			},
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"operation": schemahelper.StringProp("Operation to perform. 'render' (default) renders a single template. 'render-tree' renders an array of file entries (e.g. from the directory provider).",
					schemahelper.WithDefault(OperationRender),
					schemahelper.WithEnum(OperationRender, OperationRenderTree)),
				"template": schemahelper.StringProp("Go template content to render (required for 'render' operation). Resolver data is available as the root context (e.g., .name, .config.host). Use {{.fieldName}} to access values.",
					schemahelper.WithExample("Hello, {{.name}}!"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(65536))),
				"name": schemahelper.StringProp("Name for the template, used in error messages and logging. Required for 'render', optional for 'render-tree' (defaults to 'render-tree').",
					schemahelper.WithExample("greeting-template"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
				"missingKey": schemahelper.StringProp("Behavior when a map key is missing: 'default' (prints <no value>), 'zero' (returns zero value), 'error' (stops with error)",
					schemahelper.WithDefault("default"),
					schemahelper.WithExample("error"),
					schemahelper.WithEnum("default", "zero", "error")),
				"leftDelim": schemahelper.StringProp("Left action delimiter (default: '{{'). Change this if your template content contains literal {{",
					schemahelper.WithDefault("{{"),
					schemahelper.WithExample("<%"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10))),
				"rightDelim": schemahelper.StringProp("Right action delimiter (default: '}}'). Change this if your template content contains literal }}",
					schemahelper.WithDefault("}}"),
					schemahelper.WithExample("%>"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(10))),
				"data": schemahelper.AnyProp("Additional data to merge with resolver context. These values are accessible alongside resolver data in the template."),
				"ignoredBlocks": schemahelper.ArrayProp(
					"List of literal blocks to preserve without template parsing. Each entry uses EITHER start/end markers (for multi-line blocks) OR a line marker (for single-line matches). Content is passed through unchanged. Useful for templates containing syntax like Terraform for_each or GitHub Actions expressions that conflict with Go template delimiters.",
					schemahelper.WithItems(schemahelper.ObjectProp(
						"A block to exclude from template parsing. Use 'start'+'end' for multi-line ranges, or 'line' for single-line matches. These modes are mutually exclusive.",
						nil,
						map[string]*jsonschema.Schema{
							"start": schemahelper.StringProp("Start marker for a multi-line ignored block (e.g., '/*scafctl:ignore:start*/'). Must be paired with 'end'. Mutually exclusive with 'line'.",
								schemahelper.WithExample("/*scafctl:ignore:start*/"),
								schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
							"end": schemahelper.StringProp("End marker for a multi-line ignored block (e.g., '/*scafctl:ignore:end*/'). Must be paired with 'start'. Mutually exclusive with 'line'.",
								schemahelper.WithExample("/*scafctl:ignore:end*/"),
								schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
							"line": schemahelper.StringProp("Marker that identifies lines to ignore individually. Every line containing this substring is preserved literally. Mutually exclusive with 'start'/'end'.",
								schemahelper.WithExample("# scafctl:ignore"),
								schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
						},
					)),
					schemahelper.WithMaxItems(20),
				),
				"entries": schemahelper.ArrayProp("Array of file entry objects to render (required for 'render-tree' operation). Each entry must have 'path' (string) and 'content' (string) fields. Typically produced by the directory provider with includeContent: true.",
					schemahelper.WithItems(schemahelper.ObjectProp(
						"A file entry with path and content to render as a Go template",
						[]string{"path", "content"},
						map[string]*jsonschema.Schema{
							"path":    schemahelper.StringProp("Relative file path (preserved in output for downstream use)"),
							"content": schemahelper.StringProp("File content to render as a Go template"),
						},
					))),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp("The rendered template output (render operation)", schemahelper.WithExample("Hello, World!")),
					"entries": schemahelper.ArrayProp("Array of rendered file entries (render-tree operation)",
						schemahelper.WithItems(schemahelper.ObjectProp(
							"A rendered file entry",
							nil,
							map[string]*jsonschema.Schema{
								"path":    schemahelper.StringProp("Relative file path from the source directory"),
								"content": schemahelper.StringProp("Rendered template content"),
							},
						))),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp("Whether the template rendered successfully"),
					"result":  schemahelper.StringProp("The rendered template output (render operation)", schemahelper.WithExample("Hello, World!")),
					"entries": schemahelper.ArrayProp("Array of rendered file entries (render-tree operation)",
						schemahelper.WithItems(schemahelper.ObjectProp(
							"A rendered file entry",
							nil,
							map[string]*jsonschema.Schema{
								"path":    schemahelper.StringProp("Relative file path from the source directory"),
								"content": schemahelper.StringProp("Rendered template content"),
							},
						))),
				}),
			},
			Tags: []string{"template", "go-template", "text/template", "transform", "render"},
			// ExtractDependencies extracts resolver references from the template input,
			// respecting custom delimiters if specified
			ExtractDependencies: extractDependencies,
			Examples: []provider.Example{
				{
					Name:        "Simple variable substitution",
					Description: "Render a template with values from resolver context",
					YAML: `name: greeting
provider: go-template
inputs:
  name: greeting-template
  template: "Hello, {{.name}}!"`,
				},
				{
					Name:        "Conditional rendering",
					Description: "Use Go template conditionals with resolver data",
					YAML: `name: environment-message
provider: go-template
inputs:
  name: env-conditional
  template: |
    {{if eq .environment "production"}}
    WARNING: You are in production!
    {{else}}
    Environment: {{.environment}}
    {{end}}`,
				},
				{
					Name:        "Loop over items",
					Description: "Iterate over arrays from resolver context",
					YAML: `name: server-list
provider: go-template
inputs:
  name: server-list-template
  template: |
    Servers:
    {{range .servers}}
    - {{.name}}: {{.host}}:{{.port}}
    {{end}}`,
				},
				{
					Name:        "Custom delimiters",
					Description: "Use custom delimiters when template content contains {{",
					YAML: `name: json-template
provider: go-template
inputs:
  name: json-output
  template: '{"name": "<%.name%>", "value": "<%.value%>"}'
  leftDelim: "<%"
  rightDelim: "%>"`,
				},
				{
					Name:        "Strict missing key handling",
					Description: "Fail if a referenced key is missing",
					YAML: `name: strict-template
provider: go-template
inputs:
  name: strict-user-template
  template: "User: {{.user.name}}"
  missingKey: error`,
				},
				{
					Name:        "With additional data",
					Description: "Merge additional data with resolver context",
					YAML: `name: formatted-output
provider: go-template
inputs:
  name: formatted-name
  template: "{{.prefix}}{{.name}}{{.suffix}}"
  data:
    prefix: "*** "
    suffix: " ***"`,
				},
				{
					Name:        "Ignored blocks for literal pass-through",
					Description: "Preserve Terraform for_each expressions that conflict with Go template syntax",
					YAML: `name: terraform-template
provider: go-template
inputs:
  name: terraform-config
  template: |
    resource "azurerm_resource_group" "rg" {
      name     = "{{.resourceGroupName}}"
      location = "{{.location}}"
      /*scafctl:ignore:start*/
      for_each = { for k, v in var.items : k => v }
      /*scafctl:ignore:end*/
    }
  ignoredBlocks:
    - start: "/*scafctl:ignore:start*/"
      end: "/*scafctl:ignore:end*/"`,
				},
				{
					Name:        "Line-based ignore for single-line pass-through",
					Description: "Preserve individual lines containing a marker without needing start/end wrappers",
					YAML: `name: github-actions
provider: go-template
inputs:
  name: workflow-config
  template: |
    name: Deploy {{.appName}}
    on: [push]
    jobs:
      deploy:
        runs-on: ubuntu-latest
        steps:
          - run: echo ${{ secrets.TOKEN }}  # scafctl:ignore
  ignoredBlocks:
    - line: "# scafctl:ignore"`,
				},
				{
					Name:        "Render directory tree of templates",
					Description: "Batch-render an array of file entries from the directory provider. Combine with the file provider's write-tree operation to write rendered files preserving directory structure.",
					YAML: `name: rendered-templates
provider: go-template
inputs:
  operation: render-tree
  name: project-templates
  entries:
    expr: '__self.entries.filter(e, e.type == "file")'
  data:
    appName: my-app
    namespace: production
    replicas: 3`,
				},
			},
		},
	}
}

// Descriptor returns the provider's descriptor
func (p *GoTemplateProvider) Descriptor() *provider.Descriptor {
	return p.descriptor
}

// Execute performs the Go template rendering
func (p *GoTemplateProvider) Execute(ctx context.Context, input any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	inputs, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected map[string]any, got %T", ProviderName, input)
	}

	// Determine operation (default: render for backward compatibility)
	operation := OperationRender
	if op, ok := inputs["operation"].(string); ok && op != "" {
		operation = op
	}

	lgr.V(1).Info("executing provider", "provider", ProviderName, "operation", operation)

	switch operation {
	case OperationRender:
		return p.executeRender(ctx, inputs)
	case OperationRenderTree:
		return p.executeRenderTree(ctx, inputs)
	default:
		return nil, fmt.Errorf("%s: unsupported operation: %s (supported: render, render-tree)", ProviderName, operation)
	}
}

// executeRender performs single-template rendering (the original behavior).
func (p *GoTemplateProvider) executeRender(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	// Check for dry-run mode
	if provider.DryRunFromContext(ctx) {
		return p.executeDryRun(inputs)
	}

	// Extract template (required for render)
	templateStr, ok := inputs["template"].(string)
	if !ok || templateStr == "" {
		return nil, fmt.Errorf("%s: template is required and must be a string", ProviderName)
	}

	// Extract name (required)
	templateName, ok := inputs["name"].(string)
	if !ok || templateName == "" {
		return nil, fmt.Errorf("%s: name is required and must be a string", ProviderName)
	}

	// Parse shared rendering options
	missingKey, leftDelim, rightDelim, err := p.parseRenderingOptions(inputs)
	if err != nil {
		return nil, err
	}

	// Build template data from resolver context and additional data
	templateData := p.buildTemplateData(ctx, inputs)

	lgr.V(2).Info("executing template",
		"name", templateName,
		"templateLength", len(templateStr),
		"dataKeys", len(templateData),
		"missingKey", missingKey,
		"leftDelim", leftDelim,
		"rightDelim", rightDelim)

	// Extract ignored blocks for literal pass-through
	ignoredBlocksCfg := p.parseIgnoredBlocksConfig(inputs)

	// Validate mutual exclusion for ignored blocks
	if blocks, ok := inputs["ignoredBlocks"].([]any); ok {
		for i, block := range blocks {
			blockMap, ok := block.(map[string]any)
			if !ok {
				continue
			}
			start, _ := blockMap["start"].(string)
			end, _ := blockMap["end"].(string)
			line, _ := blockMap["line"].(string)
			hasStartEnd := start != "" || end != ""
			hasLine := line != ""
			if hasLine && hasStartEnd {
				return nil, fmt.Errorf("%s: ignoredBlocks[%d]: 'line' and 'start'/'end' are mutually exclusive — use one mode per entry", ProviderName, i)
			}
		}
	}

	replacements := p.buildIgnoredBlockReplacements(templateStr, ignoredBlocksCfg)

	// Execute the template
	result, err := p.service.Execute(ctx, gotmpl.TemplateOptions{
		Content:      templateStr,
		Name:         templateName,
		Data:         templateData,
		MissingKey:   missingKey,
		LeftDelim:    leftDelim,
		RightDelim:   rightDelim,
		Replacements: replacements,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", ProviderName, err)
	}

	lgr.V(1).Info("provider completed", "provider", ProviderName, "outputLength", len(result.Output))

	// Return result directly - the resolver executor expects output.Data to be the actual value
	return &provider.Output{
		Data: result.Output,
		Metadata: map[string]any{
			"templateName": result.TemplateName,
		},
	}, nil
}

func (p *GoTemplateProvider) executeDryRun(inputs map[string]any) (*provider.Output, error) {
	// Check if this is a render-tree dry-run
	operation := OperationRender
	if op, ok := inputs["operation"].(string); ok && op != "" {
		operation = op
	}

	if operation == OperationRenderTree {
		return p.executeDryRunRenderTree(inputs)
	}

	templateStr, _ := inputs["template"].(string)
	templateName, _ := inputs["name"].(string)

	// Truncate template for display if too long
	displayTemplate := templateStr
	if len(displayTemplate) > 100 {
		displayTemplate = displayTemplate[:100] + "..."
	}

	// Return a placeholder - the resolver executor expects output.Data to be the actual value
	return &provider.Output{
		Data: fmt.Sprintf("[DRY-RUN] Template not rendered: %s", displayTemplate),
		Metadata: map[string]any{
			"dryRun":       true,
			"templateName": templateName,
		},
	}, nil
}

// executeRenderTree batch-renders an array of file entries as Go templates.
// Each entry must have "path" and "content" fields. The output is an array of
// {path, content} objects with rendered content, suitable for the file provider's write-tree operation.
func (p *GoTemplateProvider) executeRenderTree(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	// Check for dry-run mode
	if provider.DryRunFromContext(ctx) {
		return p.executeDryRunRenderTree(inputs)
	}

	// Extract name (optional for render-tree, defaults to "render-tree")
	templateName, _ := inputs["name"].(string)
	if templateName == "" {
		templateName = "render-tree"
	}

	// Extract entries (required for render-tree)
	entriesRaw, ok := inputs["entries"]
	if !ok || entriesRaw == nil {
		return nil, fmt.Errorf("%s: entries is required for render-tree operation", ProviderName)
	}

	entries, ok := entriesRaw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: entries must be an array, got %T", ProviderName, entriesRaw)
	}

	// Parse shared rendering options
	missingKey, leftDelim, rightDelim, err := p.parseRenderingOptions(inputs)
	if err != nil {
		return nil, err
	}

	// Parse ignored blocks configuration (shared with render)
	ignoredBlocksCfg := p.parseIgnoredBlocksConfig(inputs)

	// Build base template data from resolver context + additional data
	baseData := p.buildTemplateData(ctx, inputs)

	lgr.V(1).Info("executing render-tree",
		"name", templateName,
		"entryCount", len(entries),
		"dataKeys", len(baseData),
	)

	// Handle empty entries
	if len(entries) == 0 {
		return &provider.Output{
			Data: []map[string]any{},
			Metadata: map[string]any{
				"templateName": templateName,
				"entryCount":   0,
			},
		}, nil
	}

	// Render each entry
	var warnings []string
	results := make([]map[string]any, 0, len(entries))

	for i, entryRaw := range entries {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: entries[%d] must be a map, got %T", ProviderName, i, entryRaw)
		}

		entryPath, ok := entry["path"].(string)
		if !ok || entryPath == "" {
			return nil, fmt.Errorf("%s: entries[%d].path is required and must be a string", ProviderName, i)
		}

		entryContent, ok := entry["content"].(string)
		if !ok {
			// Skip entries without content (e.g., binary files, directories)
			warnings = append(warnings, fmt.Sprintf("skipped %s: no string content", entryPath))
			continue
		}

		// Build per-entry template data: base data + entry content is the template
		templateData := make(map[string]any, len(baseData))
		maps.Copy(templateData, baseData)

		// Build ignored block replacements for this entry's content
		replacements := p.buildIgnoredBlockReplacements(entryContent, ignoredBlocksCfg)

		// Render the entry content as a Go template
		entryTemplateName := fmt.Sprintf("%s/%s", templateName, entryPath)
		result, renderErr := p.service.Execute(ctx, gotmpl.TemplateOptions{
			Content:      entryContent,
			Name:         entryTemplateName,
			Data:         templateData,
			MissingKey:   missingKey,
			LeftDelim:    leftDelim,
			RightDelim:   rightDelim,
			Replacements: replacements,
		})
		if renderErr != nil {
			return nil, fmt.Errorf("%s: failed to render %s: %w", ProviderName, entryPath, renderErr)
		}

		results = append(results, map[string]any{
			"path":    entryPath,
			"content": result.Output,
		})
	}

	lgr.V(1).Info("render-tree completed",
		"name", templateName,
		"renderedCount", len(results),
		"warningCount", len(warnings),
	)

	output := &provider.Output{
		Data: results,
		Metadata: map[string]any{
			"templateName": templateName,
			"entryCount":   len(results),
		},
	}

	if len(warnings) > 0 {
		output.Warnings = warnings
	}

	return output, nil
}

// executeDryRunRenderTree returns a dry-run placeholder for render-tree.
func (p *GoTemplateProvider) executeDryRunRenderTree(inputs map[string]any) (*provider.Output, error) {
	templateName, _ := inputs["name"].(string)

	entries, _ := inputs["entries"].([]any)
	results := make([]map[string]any, 0, len(entries))

	for _, entryRaw := range entries {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			continue
		}
		entryPath, _ := entry["path"].(string)
		if entryPath == "" {
			continue
		}
		results = append(results, map[string]any{
			"path":    entryPath,
			"content": "[dry-run rendered]",
		})
	}

	return &provider.Output{
		Data: results,
		Metadata: map[string]any{
			"dryRun":       true,
			"templateName": templateName,
			"entryCount":   len(results),
		},
	}, nil
}

// parseRenderingOptions extracts missingKey, leftDelim, and rightDelim from inputs.
func (p *GoTemplateProvider) parseRenderingOptions(inputs map[string]any) (gotmpl.MissingKeyOption, string, string, error) {
	missingKey := gotmpl.MissingKeyDefault
	if mk, ok := inputs["missingKey"].(string); ok && mk != "" {
		switch mk {
		case "default":
			missingKey = gotmpl.MissingKeyDefault
		case "zero":
			missingKey = gotmpl.MissingKeyZero
		case "error":
			missingKey = gotmpl.MissingKeyError
		default:
			return "", "", "", fmt.Errorf("%s: invalid missingKey value %q, must be 'default', 'zero', or 'error'", ProviderName, mk)
		}
	}

	leftDelim := gotmpl.DefaultLeftDelim
	if ld, ok := inputs["leftDelim"].(string); ok && ld != "" {
		leftDelim = ld
	}
	rightDelim := gotmpl.DefaultRightDelim
	if rd, ok := inputs["rightDelim"].(string); ok && rd != "" {
		rightDelim = rd
	}

	return missingKey, leftDelim, rightDelim, nil
}

// buildTemplateData constructs the template data map from resolver context, iteration context, and additional data.
func (p *GoTemplateProvider) buildTemplateData(ctx context.Context, inputs map[string]any) map[string]any {
	templateData := make(map[string]any)

	// Get resolver data from context
	if resolverData, ok := provider.ResolverContextFromContext(ctx); ok && resolverData != nil {
		maps.Copy(templateData, resolverData)
	}

	// Extract iteration context if present and merge iteration variables
	if iterCtx, ok := provider.IterationContextFromContext(ctx); ok && iterCtx != nil {
		if iterCtx.ItemAlias != "" {
			templateData[iterCtx.ItemAlias] = iterCtx.Item
		}
		if iterCtx.IndexAlias != "" {
			templateData[iterCtx.IndexAlias] = iterCtx.Index
		}
		templateData["__item"] = iterCtx.Item
		templateData["__index"] = iterCtx.Index
	}

	// Merge additional data from inputs (overrides resolver data if same key)
	if data, ok := inputs["data"].(map[string]any); ok {
		maps.Copy(templateData, data)
	}

	return templateData
}

// ignoredBlockConfig holds a parsed ignored block entry.
type ignoredBlockConfig struct {
	start string
	end   string
	line  string
}

// parseIgnoredBlocksConfig parses the ignoredBlocks input into a config slice without
// applying it to a specific template string. The actual replacements are built per-entry.
func (p *GoTemplateProvider) parseIgnoredBlocksConfig(inputs map[string]any) []ignoredBlockConfig {
	blocks, ok := inputs["ignoredBlocks"].([]any)
	if !ok {
		return nil
	}

	var configs []ignoredBlockConfig
	for _, block := range blocks {
		blockMap, ok := block.(map[string]any)
		if !ok {
			continue
		}

		start, _ := blockMap["start"].(string)
		end, _ := blockMap["end"].(string)
		line, _ := blockMap["line"].(string)

		configs = append(configs, ignoredBlockConfig{
			start: start,
			end:   end,
			line:  line,
		})
	}

	return configs
}

// buildIgnoredBlockReplacements builds gotmpl.Replacement entries for a specific template
// string based on the parsed ignored block config.
func (p *GoTemplateProvider) buildIgnoredBlockReplacements(templateStr string, configs []ignoredBlockConfig) []gotmpl.Replacement {
	var replacements []gotmpl.Replacement

	for _, cfg := range configs {
		hasStartEnd := cfg.start != "" || cfg.end != ""
		hasLine := cfg.line != ""

		if hasLine {
			for _, templateLine := range strings.Split(templateStr, "\n") {
				if strings.Contains(templateLine, cfg.line) {
					replacements = append(replacements, gotmpl.Replacement{Find: templateLine})
				}
			}
			continue
		}

		if !hasStartEnd || cfg.start == "" || cfg.end == "" {
			continue
		}

		remaining := templateStr
		for {
			startIdx := strings.Index(remaining, cfg.start)
			if startIdx < 0 {
				break
			}
			afterStart := remaining[startIdx+len(cfg.start):]
			endIdx := strings.Index(afterStart, cfg.end)
			if endIdx < 0 {
				break
			}
			fullBlock := remaining[startIdx : startIdx+len(cfg.start)+endIdx+len(cfg.end)]
			replacements = append(replacements, gotmpl.Replacement{Find: fullBlock})
			remaining = remaining[startIdx+len(cfg.start)+endIdx+len(cfg.end):]
		}
	}

	return replacements
}

// extractDependencies extracts resolver references from the go-template provider inputs.
// Handles both the "render" operation (template input) and the "render-tree" operation
// (entries/data inputs). It respects custom delimiters specified in leftDelim/rightDelim
// if present.
func extractDependencies(inputs map[string]any) []string {
	seen := make(map[string]bool)
	var deps []string

	addDep := func(name string) {
		if name != "" && !seen[name] {
			seen[name] = true
			deps = append(deps, name)
		}
	}

	// Extract rslvr/expr references from ValueRef-shaped inputs (entries, data, template, etc.)
	for _, key := range []string{"entries", "data", "template"} {
		raw, ok := inputs[key]
		if !ok {
			continue
		}
		if m, ok := raw.(map[string]any); ok {
			if rslvr, ok := m["rslvr"].(string); ok {
				// Extract base resolver name (strip dotted sub-path)
				if idx := strings.Index(rslvr, "."); idx > 0 {
					addDep(rslvr[:idx])
				} else {
					addDep(rslvr)
				}
			}
			if expr, ok := m["expr"].(string); ok {
				extractCELDeps(expr, addDep)
			}
		}
	}

	// For the "render" operation, also extract Go template references from
	// the template content (if provided as a literal string).
	templateContent, ok := inputs["template"].(string)
	if !ok {
		return deps
	}

	// Get delimiters (default to standard Go template delimiters)
	leftDelim := "{{"
	rightDelim := "}}"

	if ld, ok := inputs["leftDelim"].(string); ok && ld != "" {
		leftDelim = ld
	}
	if rd, ok := inputs["rightDelim"].(string); ok && rd != "" {
		rightDelim = rd
	}

	// Use gotmpl package to extract references with the specified delimiters
	refs, err := gotmpl.GetGoTemplateReferences(templateContent, leftDelim, rightDelim)
	if err != nil {
		// On parse error, fall back to no dependencies - the error will be caught during execution
		return deps
	}

	// Build set of keys provided by the data input so we can exclude them
	// from resolver dependencies. The data map's keys become top-level
	// template context variables (e.g., data: {config: ...} provides .config)
	// and should not be treated as resolver references.
	dataKeys := make(map[string]bool)
	if dataMap, ok := inputs["data"].(map[string]any); ok {
		for k := range dataMap {
			dataKeys[k] = true
		}
	}

	// Extract the first segment of each reference path as the dependency name
	// e.g., ".config.host" -> "config", "._.name" -> "name"
	for _, ref := range refs {
		path := ref.Path
		// Strip leading dot if present
		path = strings.TrimPrefix(path, ".")
		// Handle underscore prefix for resolver context (e.g., "_.name" -> "name")
		path = strings.TrimPrefix(path, "_.")
		path = strings.TrimPrefix(path, "_")

		// Get first segment (before any dots)
		if idx := strings.Index(path, "."); idx > 0 {
			path = path[:idx]
		}

		// Skip references satisfied by the data input
		if dataKeys[path] {
			continue
		}

		addDep(path)
	}

	return deps
}

// extractCELDeps extracts resolver references from a CEL expression string.
// It looks for _.resolverName patterns and calls addDep for each found.
func extractCELDeps(expr string, addDep func(string)) {
	// Simple pattern: find _.identifier patterns
	// Full CEL parsing is done by the CEL provider; this is a lightweight check.
	for i := 0; i < len(expr)-2; i++ {
		if expr[i] == '_' && expr[i+1] == '.' {
			// Extract identifier after _.
			start := i + 2
			end := start
			for end < len(expr) && (expr[end] == '_' || (expr[end] >= 'a' && expr[end] <= 'z') || (expr[end] >= 'A' && expr[end] <= 'Z') || (expr[end] >= '0' && expr[end] <= '9')) {
				end++
			}
			if end > start {
				addDep(expr[start:end])
			}
		}
	}
}
