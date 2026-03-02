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
	Version = "1.0.0"
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
			Name:         ProviderName,
			DisplayName:  "Go Template Provider",
			APIVersion:   "v1",
			Description:  "Transform and render data using Go text/template syntax with resolver data from context",
			Version:      version,
			Category:     "data",
			MockBehavior: "Returns a placeholder indicating the template was not executed (same as CEL provider dry-run behavior)",
			Capabilities: []provider.Capability{
				provider.CapabilityTransform,
				provider.CapabilityAction,
			},
			Schema: schemahelper.ObjectSchema([]string{"template", "name"}, map[string]*jsonschema.Schema{
				"template": schemahelper.StringProp("Go template content to render. Resolver data is available as the root context (e.g., .name, .config.host). Use {{.fieldName}} to access values.",
					schemahelper.WithExample("Hello, {{.name}}!"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(65536))),
				"name": schemahelper.StringProp("Name for the template, used in error messages and logging",
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
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.StringProp("The rendered template output", schemahelper.WithExample("Hello, World!")),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp("Whether the template rendered successfully"),
					"result":  schemahelper.StringProp("The rendered template output", schemahelper.WithExample("Hello, World!")),
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

	lgr.V(1).Info("executing provider", "provider", ProviderName)

	// Check for dry-run mode
	if provider.DryRunFromContext(ctx) {
		return p.executeDryRun(inputs)
	}

	// Extract template (required)
	templateStr, ok := inputs["template"].(string)
	if !ok || templateStr == "" {
		return nil, fmt.Errorf("%s: template is required and must be a string", ProviderName)
	}

	// Extract name (required)
	templateName, ok := inputs["name"].(string)
	if !ok || templateName == "" {
		return nil, fmt.Errorf("%s: name is required and must be a string", ProviderName)
	}

	// Extract missing key behavior
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
			return nil, fmt.Errorf("%s: invalid missingKey value %q, must be 'default', 'zero', or 'error'", ProviderName, mk)
		}
	}

	// Extract delimiters
	leftDelim := gotmpl.DefaultLeftDelim
	if ld, ok := inputs["leftDelim"].(string); ok && ld != "" {
		leftDelim = ld
	}
	rightDelim := gotmpl.DefaultRightDelim
	if rd, ok := inputs["rightDelim"].(string); ok && rd != "" {
		rightDelim = rd
	}

	// Build template data from resolver context and additional data
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
		// Also set __item and __index for standard access
		templateData["__item"] = iterCtx.Item
		templateData["__index"] = iterCtx.Index
	}

	// Merge additional data from inputs (overrides resolver data if same key)
	if data, ok := inputs["data"].(map[string]any); ok {
		maps.Copy(templateData, data)
	}

	lgr.V(2).Info("executing template",
		"name", templateName,
		"templateLength", len(templateStr),
		"dataKeys", len(templateData),
		"missingKey", missingKey,
		"leftDelim", leftDelim,
		"rightDelim", rightDelim)

	// Extract ignored blocks for literal pass-through
	// Supports two modes:
	//   1. start/end — multi-line block markers (content between markers is preserved)
	//   2. line — every line containing the marker substring is preserved
	var replacements []gotmpl.Replacement
	if blocks, ok := inputs["ignoredBlocks"].([]any); ok {
		for i, block := range blocks {
			blockMap, ok := block.(map[string]any)
			if !ok {
				continue
			}

			start, _ := blockMap["start"].(string)
			end, _ := blockMap["end"].(string)
			line, _ := blockMap["line"].(string)

			// Mutual exclusion: line and start/end cannot be used together
			hasStartEnd := start != "" || end != ""
			hasLine := line != ""

			if hasLine && hasStartEnd {
				return nil, fmt.Errorf("%s: ignoredBlocks[%d]: 'line' and 'start'/'end' are mutually exclusive — use one mode per entry", ProviderName, i)
			}

			if hasLine {
				// Line mode: find every line that contains the marker and preserve it
				for _, templateLine := range strings.Split(templateStr, "\n") {
					if strings.Contains(templateLine, line) {
						replacements = append(replacements, gotmpl.Replacement{Find: templateLine})
					}
				}
				continue
			}

			if start == "" || end == "" {
				continue
			}

			// Start/end mode: find all occurrences of start...end and create a replacement for each full block
			remaining := templateStr
			for {
				startIdx := strings.Index(remaining, start)
				if startIdx < 0 {
					break
				}
				afterStart := remaining[startIdx+len(start):]
				endIdx := strings.Index(afterStart, end)
				if endIdx < 0 {
					break
				}
				// Extract the full block: start marker + content + end marker
				fullBlock := remaining[startIdx : startIdx+len(start)+endIdx+len(end)]
				replacements = append(replacements, gotmpl.Replacement{Find: fullBlock})
				remaining = remaining[startIdx+len(start)+endIdx+len(end):]
			}
		}
	}

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

// extractDependencies extracts resolver references from the go-template provider inputs.
// It respects custom delimiters specified in leftDelim/rightDelim if present.
func extractDependencies(inputs map[string]any) []string {
	// Get template content
	templateContent, ok := inputs["template"].(string)
	if !ok {
		return nil
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
		return nil
	}

	// Extract the first segment of each reference path as the dependency name
	// e.g., ".config.host" -> "config", "._.name" -> "name"
	seen := make(map[string]bool)
	var deps []string

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

		// Skip empty paths
		if path == "" {
			continue
		}

		// Deduplicate
		if !seen[path] {
			seen[path] = true
			deps = append(deps, path)
		}
	}

	return deps
}
