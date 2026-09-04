// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplprovider

import (
	"context"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"text/template"

	"github.com/Masterminds/semver/v3"
	"github.com/bmatcuk/doublestar/v4"
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

	// DefaultRawStart is the built-in, zero-config fence start marker. Content
	// between DefaultRawStart and DefaultRawEnd is preserved verbatim (never
	// parsed as a Go template) and the markers themselves are stripped from the
	// output. It uses Go template comment syntax so the template remains valid
	// even when the fence is unused. No ignoredBlocks declaration is required.
	DefaultRawStart = "{{/* scafctl:ignore:start */}}"
	// DefaultRawEnd is the built-in, zero-config fence end marker. See DefaultRawStart.
	DefaultRawEnd = "{{/* scafctl:ignore:end */}}"
	// DefaultRawLine is the built-in, zero-config per-line marker. Any line
	// containing this substring is preserved verbatim (marker included, as a
	// harmless trailing comment). No ignoredBlocks declaration is required.
	DefaultRawLine = "# scafctl:ignore"

	// MaxRawGlobs caps the number of rawGlobs patterns accepted by the
	// render-tree operation, mirroring the ignoredBlocks maxItems limit.
	MaxRawGlobs = 20
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
				"name": schemahelper.StringProp("Optional name for the template, used in error messages and logging. Defaults to 'template' for 'render' and 'render-tree' for the batch operation.",
					schemahelper.WithExample("greeting-template"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
				"missingKey": schemahelper.StringProp("Behavior when a map key is missing: 'default' (prints <no value>), 'zero' (returns zero value), 'error' (stops with error). Default: error",
					schemahelper.WithDefault("error"),
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
					"Additional literal blocks to preserve without template parsing, on top of the always-on built-in markers. Built-in (zero-config) markers are recognized without any declaration: fence '{{/* scafctl:ignore:start */}}' ... '{{/* scafctl:ignore:end */}}' (markers stripped from output) and per-line '# scafctl:ignore'. Use this list to declare EXTRA markers. Each entry uses EXACTLY ONE mode: start/end (multi-line, markers preserved), line (single-line matches), or token (every literal occurrence of a substring). Useful for templates containing syntax like Terraform for_each or GitHub Actions expressions that conflict with Go template delimiters.",
					schemahelper.WithItems(schemahelper.ObjectProp(
						"A block to exclude from template parsing. Use 'start'+'end' for multi-line ranges, 'line' for single-line matches, or 'token' for every occurrence of a literal substring. These modes are mutually exclusive.",
						nil,
						map[string]*jsonschema.Schema{
							"start": schemahelper.StringProp("Start marker for a multi-line ignored block (e.g., '/*scafctl:ignore:start*/'). Must be paired with 'end'. Mutually exclusive with 'line' and 'token'.",
								schemahelper.WithExample("/*scafctl:ignore:start*/"),
								schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
							"end": schemahelper.StringProp("End marker for a multi-line ignored block (e.g., '/*scafctl:ignore:end*/'). Must be paired with 'start'. Mutually exclusive with 'line' and 'token'.",
								schemahelper.WithExample("/*scafctl:ignore:end*/"),
								schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
							"line": schemahelper.StringProp("Marker that identifies lines to ignore individually. Every line containing this substring is preserved literally. Mutually exclusive with 'start'/'end' and 'token'.",
								schemahelper.WithExample("# scafctl:ignore"),
								schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
							"token": schemahelper.StringProp("Literal token to preserve wherever it appears. Every occurrence of this exact substring is passed through unchanged. Mutually exclusive with 'start'/'end' and 'line'.",
								schemahelper.WithExample("${LITERAL}"),
								schemahelper.WithMaxLength(*ptrs.IntPtr(255))),
						},
					)),
					schemahelper.WithMaxItems(20),
				),
				"rawGlobs": schemahelper.ArrayProp(
					"Optional list of doublestar glob patterns (render-tree only) matched against each entry's 'path'. A matching entry is emitted verbatim -- its content is copied byte-for-byte with no template parsing, no delimiter handling, no ignoredBlocks processing, and any per-entry 'data' ignored. Patterns match the full relative path, so '*.tf' matches only top-level files while '**/*.tf' also matches nested ones. Mirrors Cookiecutter's _copy_without_render. A per-entry 'raw' field takes precedence: 'raw: true' forces verbatim even without a glob match, and an explicit 'raw: false' forces rendering even if a glob matches.",
					schemahelper.WithItems(schemahelper.StringProp("A doublestar glob pattern matched against an entry's relative path.",
						schemahelper.WithExample("**/*.tpl.raw"),
						schemahelper.WithMaxLength(*ptrs.IntPtr(255)))),
					schemahelper.WithMaxItems(MaxRawGlobs),
				),
				"entries": schemahelper.ArrayProp("Array of file entry objects to render (required for 'render-tree' operation). Each entry must have a 'path' (string) field; 'content' (string) is optional -- entries without string content (e.g., binary files or directories) are skipped with a warning. Each entry may also include an optional 'data' (map) field and an optional 'raw' (bool) field. Typically produced by the directory provider with includeContent: true.",
					schemahelper.WithItems(schemahelper.ObjectProp(
						"A file entry with a required path and optional content to render as a Go template, plus optional per-entry data and a raw passthrough flag. Entries without string content are skipped.",
						[]string{"path"},
						map[string]*jsonschema.Schema{
							"path":    schemahelper.StringProp("Relative file path (preserved in output for downstream use)"),
							"content": schemahelper.StringProp("File content to render as a Go template. Optional: entries without string content are skipped with a warning."),
							"data": schemahelper.ObjectProp("Optional per-entry data map, shallow-merged over the shared top-level 'data' for this entry only. On key conflicts, per-entry values win over shared data, iteration variables, and resolver context. Enables fan-out: one template rendered per entry, each with its own variables and output path, without a separate forEach render resolver. Ignored for entries emitted verbatim (raw).",
								nil, nil, schemahelper.WithAdditionalProperties(schemahelper.AnyProp("Per-entry data value of any type"))),
							"raw": schemahelper.BoolProp("Optional per-entry verbatim flag. When true, the entry's content is copied byte-for-byte with no template parsing (per-entry 'data' is ignored). When false, the entry is rendered even if it matches a rawGlobs pattern. When omitted, rawGlobs decides. Per-entry 'raw' always wins over rawGlobs."),
						},
					))),
				"forEach": schemahelper.ObjectProp(
					"Optional per-item fan-out (render-tree only). When set, every entry is rendered once per element of 'in', producing a single flat output array (item x entries). Each iteration injects the current element (as the 'item' alias and __item) and its 0-based index (as the 'index' alias and __index) into the per-entry template data, and 'pathTemplate' (required with forEach) computes each entry's output path so items never collide. Mirrors the forEach clause on resolve/action steps and Terraform's module for_each. Omit for the default one-pass behavior.",
					[]string{"in"},
					map[string]*jsonschema.Schema{
						"item": schemahelper.StringProp("Variable name alias for the current element, available in entry content, per-entry data, and pathTemplate (also exposed as __item).",
							schemahelper.WithExample("env"),
							schemahelper.WithPattern("^[a-zA-Z_][a-zA-Z0-9_]*$"),
							schemahelper.WithMaxLength(*ptrs.IntPtr(50))),
						"index": schemahelper.StringProp("Optional variable name alias for the current 0-based index (also exposed as __index).",
							schemahelper.WithExample("i"),
							schemahelper.WithPattern("^[a-zA-Z_][a-zA-Z0-9_]*$"),
							schemahelper.WithMaxLength(*ptrs.IntPtr(50))),
						"in": schemahelper.AnyProp("The collection to fan out over. A ValueRef ({rslvr: name}, {expr: CEL}, {literal: [...]}, {tmpl: ...}) or a literal array; it must resolve to an array. One rendered copy of every entry is produced per element."),
					},
				),
				"pathTemplate": schemahelper.StringProp(
					"Go template that computes each entry's output path during fan-out (required when 'forEach' is set; ignored otherwise). Rendered per (item, entry) with the forEach aliases, __item/__index, resolver context, shared data, and the per-entry path parts __filePath, __fileName, __fileStem, __fileExtension, __fileDir (same variables as the file provider's write-tree outputPath). Must produce a distinct, non-empty path for every item so outputs never collide.",
					schemahelper.WithExample("envs/{{ .env.name }}/{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}"),
					schemahelper.WithMaxLength(*ptrs.IntPtr(1024)),
				),
			}),
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityTransform: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"result": schemahelper.AnyProp("The resolver value, auto-unwrapped. For the 'render' operation this is the rendered template string. For the 'render-tree' operation this is the array of rendered file entries (each an object with 'path' and 'content' strings). Reference the value directly as _.<resolver> -- the render-tree value is the bare array, there is no .entries wrapper.",
						schemahelper.WithExample("Hello, World!")),
				}),
				provider.CapabilityAction: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
					"success": schemahelper.BoolProp("Whether the template rendered successfully"),
					"result": schemahelper.AnyProp("The rendered output. For the 'render' operation this is the rendered template string. For the 'render-tree' operation this is the array of rendered file entries (each an object with 'path' and 'content' strings).",
						schemahelper.WithExample("Hello, World!")),
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
					Name:        "Built-in fence (zero-config, markers stripped)",
					Description: "Preserve a block literally using the always-on built-in fence. No ignoredBlocks declaration is needed, and the fence markers are stripped from the output.",
					YAML: `name: builtin-fence
provider: go-template
inputs:
  name: terraform-config
  template: |
    name = "{{.appName}}"
    {{/* scafctl:ignore:start */}}
    for_each = { for k, v in var.items : k => v }
    {{/* scafctl:ignore:end */}}`,
				},
				{
					Name:        "Token mode (literal substring pass-through)",
					Description: "Preserve every occurrence of a literal substring without wrapping each in markers. Useful when a placeholder like ${LITERAL} must survive rendering wherever it appears.",
					YAML: `name: literal-tokens
provider: go-template
inputs:
  name: tokens
  template: |
    service: {{.appName}}
    url: ${LITERAL}/api
    healthcheck: ${LITERAL}/health
  ignoredBlocks:
    - token: "${LITERAL}"`,
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
				{
					Name:        "Fan-out with per-entry data",
					Description: "Render one template per collection item, each with its own variables and output path, in a single resolver. Per-entry 'data' is shallow-merged over the shared 'data' and wins on conflicts. Feeds the file provider's write-tree operation unchanged.",
					YAML: `name: backend-configs
provider: go-template
inputs:
  operation: render-tree
  data:
    platformAppName: my-app
  entries:
    expr: |
      _.environments.map(env, {
        "path":    "envs/" + env.name + "/backend.tf",
        "content": _.backendTemplate.entries[0].content,
        "data":    {"environment": env}
      })`,
				},
				{
					Name:        "Tree fan-out (forEach + pathTemplate)",
					Description: "Render a whole template tree once per item in a collection (Terraform module for_each style). forEach.in is resolved by the spec layer to an array; pathTemplate routes each item's copy to a distinct path using the item alias, __index, and the reserved __file* path parts. Rendered paths must be unique across the fan-out.",
					YAML: `name: per-env-tree
provider: go-template
inputs:
  operation: render-tree
  entries:
    expr: '_.templateFiles.entries'
  forEach:
    item: env
    in:
      rslvr: environments
  pathTemplate: >-
    envs/{{ .env.name }}/{{ if .__fileDir }}{{ .__fileDir }}/{{ end }}{{ .__fileStem }}`,
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
		return p.executeDryRun(ctx, inputs)
	}

	// Extract template (required for render)
	templateStr, ok := inputs["template"].(string)
	if !ok || templateStr == "" {
		return nil, fmt.Errorf("%s: template is required and must be a string", ProviderName)
	}

	// Extract name (optional, defaults to "template")
	templateName, _ := inputs["name"].(string)
	if templateName == "" {
		templateName = "template"
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
	ignoredBlocksCfg := parseIgnoredBlocksConfig(inputs)

	// Validate mutual exclusion for ignored blocks
	if err := validateIgnoredBlocks(inputs); err != nil {
		return nil, err
	}

	replacements := buildIgnoredBlockReplacements(templateStr, ignoredBlocksCfg)

	// Bind any solution-author-defined helper functions (spec.functions).
	authorFuncs, authorFuncsFP := authorTemplateFuncs(ctx)

	// Execute the template
	result, err := p.service.Execute(ctx, gotmpl.TemplateOptions{
		Content:          templateStr,
		Name:             templateName,
		Data:             templateData,
		MissingKey:       missingKey,
		LeftDelim:        leftDelim,
		RightDelim:       rightDelim,
		Replacements:     replacements,
		Funcs:            authorFuncs,
		FuncsFingerprint: authorFuncsFP,
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

func (p *GoTemplateProvider) executeDryRun(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	// Check if this is a render-tree dry-run
	operation := OperationRender
	if op, ok := inputs["operation"].(string); ok && op != "" {
		operation = op
	}

	if operation == OperationRenderTree {
		return p.executeDryRunRenderTree(ctx, inputs)
	}

	templateStr, _ := inputs["template"].(string)
	templateName, _ := inputs["name"].(string)
	if templateName == "" {
		templateName = "template"
	}

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
		return p.executeDryRunRenderTree(ctx, inputs)
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

	// Validate mutual exclusion for ignored blocks
	if err := validateIgnoredBlocks(inputs); err != nil {
		return nil, err
	}

	// Parse ignored blocks configuration (shared with render)
	ignoredBlocksCfg := parseIgnoredBlocksConfig(inputs)

	// Parse rawGlobs: verbatim passthrough patterns matched against entry paths.
	rawGlobs, err := parseRawGlobs(inputs)
	if err != nil {
		return nil, err
	}

	// Build base template data from resolver context + additional data
	baseData := p.buildTemplateData(ctx, inputs)

	// Bind any solution-author-defined helper functions (spec.functions) once
	// for the whole tree.
	authorFuncs, authorFuncsFP := authorTemplateFuncs(ctx)

	// Parse optional per-item fan-out. When present, every entry is rendered
	// once per item into a single flat output array.
	fanOut, err := p.parseRenderTreeFanOut(ctx, inputs)
	if err != nil {
		return nil, err
	}

	opts := &renderTreeOpts{
		templateName:     templateName,
		baseData:         baseData,
		rawGlobs:         rawGlobs,
		ignoredBlocksCfg: ignoredBlocksCfg,
		missingKey:       missingKey,
		leftDelim:        leftDelim,
		rightDelim:       rightDelim,
		authorFuncs:      authorFuncs,
		authorFuncsFP:    authorFuncsFP,
	}

	if fanOut != nil {
		lgr.V(1).Info("executing render-tree fan-out",
			"name", templateName,
			"entryCount", len(entries),
			"itemCount", len(fanOut.items),
			"dataKeys", len(baseData),
			"rawGlobs", len(rawGlobs),
		)
		return p.renderTreeFanOut(ctx, entries, opts, fanOut)
	}

	lgr.V(1).Info("executing render-tree",
		"name", templateName,
		"entryCount", len(entries),
		"dataKeys", len(baseData),
		"rawGlobs", len(rawGlobs),
	)

	// Handle empty entries
	if len(entries) == 0 {
		return emptyRenderTreeOutput(templateName), nil
	}

	// Render each entry
	var warnings []string
	results := make([]map[string]any, 0, len(entries))

	for i, entryRaw := range entries {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: entries[%d] must be a map, got %T", ProviderName, i, entryRaw)
		}

		out, warn, rerr := p.renderTreeEntry(ctx, opts, entry, i, nil, "")
		if rerr != nil {
			return nil, rerr
		}
		if warn != "" {
			warnings = append(warnings, warn)
		}
		if out != nil {
			results = append(results, out)
		}
	}

	lgr.V(1).Info("render-tree completed",
		"name", templateName,
		"renderedCount", len(results),
		"warningCount", len(warnings),
	)

	return renderTreeOutput(templateName, results, warnings), nil
}

// renderTreeOpts holds the shared rendering configuration for a render-tree
// invocation, so the per-entry renderer can be reused by both the default
// one-pass path and the per-item fan-out path.
type renderTreeOpts struct {
	templateName     string
	baseData         map[string]any
	rawGlobs         []string
	ignoredBlocksCfg []ignoredBlockConfig
	missingKey       gotmpl.MissingKeyOption
	leftDelim        string
	rightDelim       string
	authorFuncs      template.FuncMap
	authorFuncsFP    string
}

// renderTreeFanOutConfig is the resolved per-item fan-out configuration.
type renderTreeFanOutConfig struct {
	itemAlias    string
	indexAlias   string
	pathTemplate string
	items        []any
}

// renderTreeFanOut renders every entry once per fan-out item, producing a single
// flat output array (item x entries). Each item's rendered entries are routed to
// distinct paths via pathTemplate; a collision is a hard error so a mis-written
// pathTemplate can never silently collapse the output to a single item.
func (p *GoTemplateProvider) renderTreeFanOut(ctx context.Context, entries []any, opts *renderTreeOpts, fanOut *renderTreeFanOutConfig) (*provider.Output, error) {
	lgr := logger.FromContext(ctx)

	// An empty collection (or no entries) yields an empty flat result, matching
	// the empty-array behavior of forEach on resolve steps.
	if len(fanOut.items) == 0 || len(entries) == 0 {
		return emptyRenderTreeOutput(opts.templateName), nil
	}

	var warnings []string
	// seenWarn dedupes identical per-entry warnings (e.g. a skipped entry or a
	// raw entry carrying data) so one problematic entry does not emit the same
	// message once per fan-out item.
	seenWarn := make(map[string]struct{})
	results := make([]map[string]any, 0, len(entries))
	// seen maps an output path to a human-readable origin for collision errors.
	seen := make(map[string]string, len(entries))

	for idx, item := range fanOut.items {
		itemData := make(map[string]any, 4)
		if fanOut.itemAlias != "" {
			itemData[fanOut.itemAlias] = item
		}
		if fanOut.indexAlias != "" {
			itemData[fanOut.indexAlias] = idx
		}
		itemData["__item"] = item
		itemData["__index"] = idx

		for j, entryRaw := range entries {
			entry, ok := entryRaw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s: entries[%d] must be a map, got %T", ProviderName, j, entryRaw)
			}

			out, warn, err := p.renderTreeEntry(ctx, opts, entry, j, itemData, fanOut.pathTemplate)
			if err != nil {
				return nil, fmt.Errorf("%s: forEach item %d: %w", ProviderName, idx, err)
			}
			if warn != "" {
				if _, dup := seenWarn[warn]; !dup {
					seenWarn[warn] = struct{}{}
					warnings = append(warnings, warn)
				}
			}
			if out == nil {
				continue
			}

			outPath, _ := out["path"].(string)
			origin := fmt.Sprintf("item %d / entries[%d]", idx, j)
			if prev, dup := seen[outPath]; dup {
				return nil, fmt.Errorf("%s: forEach produced duplicate output path %q (from %s and %s); make pathTemplate yield a distinct path per item", ProviderName, outPath, prev, origin)
			}
			seen[outPath] = origin
			results = append(results, out)
		}
	}

	lgr.V(1).Info("render-tree fan-out completed",
		"name", opts.templateName,
		"renderedCount", len(results),
		"warningCount", len(warnings),
	)

	return renderTreeOutput(opts.templateName, results, warnings), nil
}

// renderTreeEntry renders one render-tree entry. itemData carries the fan-out
// iteration variables (nil for the default one-pass path). When pathTemplate is
// non-empty (fan-out), the entry's output path is computed from it using the
// entry's __file* path parts plus the merged template data; otherwise the
// entry's own path is preserved. It returns the rendered {path, content} map, or
// a nil map plus a warning when the entry is skipped (no string content).
func (p *GoTemplateProvider) renderTreeEntry(ctx context.Context, opts *renderTreeOpts, entry map[string]any, idx int, itemData map[string]any, pathTemplate string) (map[string]any, string, error) {
	entryPath, ok := entry["path"].(string)
	if !ok || entryPath == "" {
		return nil, "", fmt.Errorf("%s: entries[%d].path is required and must be a string", ProviderName, idx)
	}

	entryContent, ok := entry["content"].(string)
	if !ok {
		// Skip entries without content (e.g., binary files, directories).
		return nil, fmt.Sprintf("skipped %s: no string content", entryPath), nil
	}

	entryData, hasEntryData, err := entryDataMap(entry, idx, entryPath)
	if err != nil {
		return nil, "", err
	}

	// Decide whether this entry is emitted verbatim. Per-entry "raw" wins over
	// rawGlobs; a non-bool "raw" is an input error.
	raw, badType := isRawEntry(entryPath, entry, opts.rawGlobs)
	if badType {
		return nil, "", fmt.Errorf("%s: entries[%d].raw must be a bool when present, got %T (path %q)", ProviderName, idx, entry["raw"], entryPath)
	}

	// Compute the output path. Fan-out always routes through pathTemplate so each
	// item lands in a distinct location; the default pass preserves the path.
	outPath := entryPath
	if pathTemplate != "" {
		pathData := make(map[string]any, len(opts.baseData))
		maps.Copy(pathData, opts.baseData)
		maps.Copy(pathData, itemData)
		if hasEntryData {
			maps.Copy(pathData, entryData)
		}
		// Reserved __file* parts win over any colliding user keys.
		maps.Copy(pathData, fileTemplateVars(entryPath))

		rendered, perr := p.renderPathTemplate(ctx, opts, pathTemplate, pathData)
		if perr != nil {
			return nil, "", fmt.Errorf("%s: pathTemplate failed for entries[%d] (%s): %w", ProviderName, idx, entryPath, perr)
		}
		if rendered == "" {
			return nil, "", fmt.Errorf("%s: pathTemplate resolved to an empty path for entries[%d] (%s)", ProviderName, idx, entryPath)
		}
		outPath = rendered
	}

	if raw {
		// Copy content byte-for-byte: no parse, no data merge, no ignored
		// blocks. Per-entry data is meaningless without rendering, so warn if it
		// was supplied to surface a likely authoring mistake.
		warn := ""
		if hasEntryData {
			warn = fmt.Sprintf("%s: per-entry data ignored for raw (verbatim) entry", entryPath)
		}
		return map[string]any{"path": outPath, "content": entryContent}, warn, nil
	}

	// Build per-entry template data: base data + fan-out vars + per-entry data
	// overrides (shallow; per-entry keys win). baseData is left untouched so
	// entries never leak values into one another.
	templateData := make(map[string]any, len(opts.baseData))
	maps.Copy(templateData, opts.baseData)
	maps.Copy(templateData, itemData)
	if hasEntryData {
		maps.Copy(templateData, entryData)
	}

	// Build ignored block replacements for this entry's content.
	replacements := buildIgnoredBlockReplacements(entryContent, opts.ignoredBlocksCfg)

	// Render the entry content as a Go template.
	entryTemplateName := fmt.Sprintf("%s/%s", opts.templateName, entryPath)
	result, renderErr := p.service.Execute(ctx, gotmpl.TemplateOptions{
		Content:          entryContent,
		Name:             entryTemplateName,
		Data:             templateData,
		MissingKey:       opts.missingKey,
		LeftDelim:        opts.leftDelim,
		RightDelim:       opts.rightDelim,
		Replacements:     replacements,
		Funcs:            opts.authorFuncs,
		FuncsFingerprint: opts.authorFuncsFP,
	})
	if renderErr != nil {
		return nil, "", fmt.Errorf("%s: failed to render %s: %w", ProviderName, entryPath, renderErr)
	}

	return map[string]any{"path": outPath, "content": result.Output}, "", nil
}

// renderPathTemplate renders a fan-out pathTemplate to a cleaned relative path.
func (p *GoTemplateProvider) renderPathTemplate(ctx context.Context, opts *renderTreeOpts, pathTemplate string, data map[string]any) (string, error) {
	result, err := p.service.Execute(ctx, gotmpl.TemplateOptions{
		Content:          pathTemplate,
		Name:             opts.templateName + "/pathTemplate",
		Data:             data,
		MissingKey:       opts.missingKey,
		LeftDelim:        opts.leftDelim,
		RightDelim:       opts.rightDelim,
		Funcs:            opts.authorFuncs,
		FuncsFingerprint: opts.authorFuncsFP,
	})
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(result.Output)
	if out == "" {
		return "", nil
	}
	// Canonicalize so textually-distinct-but-equivalent paths ("envs//dev/x",
	// "./envs/dev/x") collapse to one key -- otherwise the duplicate-path guard
	// would treat them as distinct yet write-tree would overwrite one with the
	// other, defeating the hard-error guarantee.
	out = path.Clean(filepath.ToSlash(out))
	out = strings.TrimPrefix(out, "/")
	return out, nil
}

// parseRenderTreeFanOut extracts the optional render-tree fan-out configuration.
// It returns (nil, nil) when no forEach is declared. forEach.in is expected to
// already be a concrete array: like every other provider input, nested value
// references (rslvr/expr/tmpl) are resolved by the spec layer before the
// provider runs, so the provider never re-resolves them. pathTemplate is
// required whenever forEach is set so every item routes to a distinct path.
func (p *GoTemplateProvider) parseRenderTreeFanOut(_ context.Context, inputs map[string]any) (*renderTreeFanOutConfig, error) {
	raw, present := inputs["forEach"]
	if !present || raw == nil {
		return nil, nil
	}

	fe, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: forEach must be a map, got %T", ProviderName, raw)
	}

	itemAlias, _ := fe["item"].(string)
	indexAlias, _ := fe["index"].(string)

	pathTemplate, _ := inputs["pathTemplate"].(string)
	if strings.TrimSpace(pathTemplate) == "" {
		return nil, fmt.Errorf("%s: pathTemplate is required when forEach is set on render-tree", ProviderName)
	}

	inRaw, ok := fe["in"]
	if !ok || inRaw == nil {
		return nil, fmt.Errorf("%s: forEach.in is required for render-tree fan-out", ProviderName)
	}

	items, ok := toAnySlice(inRaw)
	if !ok {
		return nil, fmt.Errorf("%s: forEach.in must resolve to an array, got %T", ProviderName, inRaw)
	}

	return &renderTreeFanOutConfig{
		itemAlias:    itemAlias,
		indexAlias:   indexAlias,
		pathTemplate: pathTemplate,
		items:        items,
	}, nil
}

// entryDataMap extracts and validates the optional per-entry "data" map.
func entryDataMap(entry map[string]any, idx int, entryPath string) (map[string]any, bool, error) {
	raw, present := entry["data"]
	if !present || raw == nil {
		return nil, false, nil
	}
	data, ok := raw.(map[string]any)
	if !ok {
		return nil, false, fmt.Errorf("%s: entries[%d].data must be a map when present, got %T (path %q)", ProviderName, idx, raw, entryPath)
	}
	return data, true, nil
}

// fileTemplateVars computes the reserved __file* path-part variables for a
// relative entry path, matching the file provider's write-tree outputPath so a
// pathTemplate can reuse the exact same variables.
func fileTemplateVars(relPath string) map[string]any {
	name := filepath.Base(relPath)
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	dir := filepath.ToSlash(filepath.Dir(relPath))
	if dir == "." {
		dir = ""
	}
	return map[string]any{
		"__filePath":      filepath.ToSlash(relPath),
		"__fileName":      name,
		"__fileStem":      stem,
		"__fileExtension": ext,
		"__fileDir":       dir,
	}
}

// toAnySlice normalizes any slice/array value into []any, reporting false for
// non-slice inputs. It accepts the []any that CEL and literal inputs produce as
// well as typed slices (e.g. []map[string]any) that resolver bindings may carry.
func toAnySlice(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	if s, ok := v.([]any); ok {
		return s, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, true
}

// emptyRenderTreeOutput returns the standard empty render-tree result.
func emptyRenderTreeOutput(templateName string) *provider.Output {
	return &provider.Output{
		Data: []map[string]any{},
		Metadata: map[string]any{
			"templateName": templateName,
			"entryCount":   0,
		},
	}
}

// renderTreeOutput builds a render-tree provider output from rendered results.
func renderTreeOutput(templateName string, results []map[string]any, warnings []string) *provider.Output {
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
	return output
}

// executeDryRunRenderTree returns a dry-run placeholder for render-tree. When
// fan-out is configured it previews one placeholder per (item, entry), routing
// each through pathTemplate so the planned output tree is visible; resolution
// and path-render errors are ignored here since they surface on the real run.
func (p *GoTemplateProvider) executeDryRunRenderTree(ctx context.Context, inputs map[string]any) (*provider.Output, error) {
	templateName, _ := inputs["name"].(string)

	// Best-effort rawGlobs parse: dry-run should reflect verbatim entries, but a
	// malformed rawGlobs surfaces on the real run, so ignore parse errors here.
	rawGlobs, _ := parseRawGlobs(inputs)

	entries, _ := inputs["entries"].([]any)

	// Best-effort fan-out preview: ignore config errors so a dry-run never fails
	// on a misconfiguration the real run will report.
	fanOut, _ := p.parseRenderTreeFanOut(ctx, inputs)

	results := make([]map[string]any, 0, len(entries))

	dryContent := func(entryPath string, entry map[string]any) string {
		if raw, badType := isRawEntry(entryPath, entry, rawGlobs); raw && !badType {
			return "[dry-run raw copy]"
		}
		return "[dry-run rendered]"
	}

	if fanOut != nil && len(fanOut.items) > 0 {
		baseData := p.buildTemplateData(ctx, inputs)
		opts := &renderTreeOpts{templateName: templateName, baseData: baseData, missingKey: gotmpl.MissingKeyDefault}
		for idx, item := range fanOut.items {
			itemData := map[string]any{"__item": item, "__index": idx}
			if fanOut.itemAlias != "" {
				itemData[fanOut.itemAlias] = item
			}
			if fanOut.indexAlias != "" {
				itemData[fanOut.indexAlias] = idx
			}
			for _, entryRaw := range entries {
				entry, ok := entryRaw.(map[string]any)
				if !ok {
					continue
				}
				entryPath, _ := entry["path"].(string)
				if entryPath == "" {
					continue
				}
				pathData := make(map[string]any, len(baseData))
				maps.Copy(pathData, baseData)
				maps.Copy(pathData, itemData)
				maps.Copy(pathData, fileTemplateVars(entryPath))
				outPath := entryPath
				if rendered, err := p.renderPathTemplate(ctx, opts, fanOut.pathTemplate, pathData); err == nil && rendered != "" {
					outPath = rendered
				}
				results = append(results, map[string]any{
					"path":    outPath,
					"content": dryContent(entryPath, entry),
				})
			}
		}

		return &provider.Output{
			Data: results,
			Metadata: map[string]any{
				"dryRun":       true,
				"templateName": templateName,
				"entryCount":   len(results),
				"fanOutItems":  len(fanOut.items),
			},
		}, nil
	}

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
			"content": dryContent(entryPath, entry),
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
	missingKey := gotmpl.MissingKeyError
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

// authorTemplateFuncs binds the solution-author-defined helper functions
// (spec.functions) carried on the provider context, if any. It returns the
// bound FuncMap and its fingerprint (used to isolate template cache entries),
// or (nil, "") when no functions are declared.
func authorTemplateFuncs(ctx context.Context) (template.FuncMap, string) {
	binder, ok := provider.TemplateFuncBinderFromContext(ctx)
	if !ok || binder == nil {
		return nil, ""
	}
	return binder.Bind(ctx), binder.Fingerprint()
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
	token string
	// stripMarkers indicates the start/end markers should be removed from the
	// output, restoring only the inner content. Used by the built-in fence.
	stripMarkers bool
}

// builtinIgnoredBlockConfigs returns the always-on, zero-config ignored block
// markers that are recognized without any ignoredBlocks declaration:
//   - a fence ('{{/* scafctl:ignore:start */}}' ... '{{/* scafctl:ignore:end */}}')
//     whose markers are stripped from the output, and
//   - a per-line marker ('# scafctl:ignore') whose line is preserved verbatim.
func builtinIgnoredBlockConfigs() []ignoredBlockConfig {
	return []ignoredBlockConfig{
		{start: DefaultRawStart, end: DefaultRawEnd, stripMarkers: true},
		{line: DefaultRawLine},
	}
}

// validateIgnoredBlocks enforces that each ignoredBlocks entry uses exactly one
// mode: start/end, line, or token. The modes are mutually exclusive, at least
// one mode must be set, and start/end must be declared as a pair.
func validateIgnoredBlocks(inputs map[string]any) error {
	blocks, ok := inputs["ignoredBlocks"].([]any)
	if !ok {
		return nil
	}
	for i, block := range blocks {
		blockMap, ok := block.(map[string]any)
		if !ok {
			continue
		}
		start, _ := blockMap["start"].(string)
		end, _ := blockMap["end"].(string)
		line, _ := blockMap["line"].(string)
		token, _ := blockMap["token"].(string)

		hasStartEnd := start != "" || end != ""
		hasLine := line != ""
		hasToken := token != ""

		modes := 0
		if hasStartEnd {
			modes++
		}
		if hasLine {
			modes++
		}
		if hasToken {
			modes++
		}
		switch {
		case modes == 0:
			return fmt.Errorf("%s: ignoredBlocks[%d]: no mode set -- use exactly one of 'start'/'end', 'line', or 'token'", ProviderName, i)
		case modes > 1:
			return fmt.Errorf("%s: ignoredBlocks[%d]: 'start'/'end', 'line', and 'token' are mutually exclusive -- use one mode per entry", ProviderName, i)
		case hasStartEnd && (start == "" || end == ""):
			return fmt.Errorf("%s: ignoredBlocks[%d]: 'start' and 'end' must both be set for start/end mode", ProviderName, i)
		}
	}
	return nil
}

// parseRawGlobs extracts and validates the render-tree rawGlobs input. Each
// element must be a non-empty string and a syntactically valid doublestar
// pattern; the number of patterns is capped at MaxRawGlobs. A nil/absent input
// yields no patterns and no error. Patterns are matched against each entry's
// full relative path in isRawEntry.
func parseRawGlobs(inputs map[string]any) ([]string, error) {
	raw, ok := inputs["rawGlobs"]
	if !ok || raw == nil {
		return nil, nil
	}

	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: rawGlobs must be an array of strings, got %T", ProviderName, raw)
	}
	if len(list) > MaxRawGlobs {
		return nil, fmt.Errorf("%s: rawGlobs has %d patterns, exceeding the maximum of %d", ProviderName, len(list), MaxRawGlobs)
	}

	patterns := make([]string, 0, len(list))
	for i, item := range list {
		pattern, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s: rawGlobs[%d] must be a string, got %T", ProviderName, i, item)
		}
		if pattern == "" {
			return nil, fmt.Errorf("%s: rawGlobs[%d] must not be empty", ProviderName, i)
		}
		// Probe the pattern for validity; doublestar reports malformed patterns
		// via ErrBadPattern regardless of the input string being matched.
		if _, err := doublestar.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("%s: rawGlobs[%d] is not a valid glob pattern %q: %w", ProviderName, i, pattern, err)
		}
		patterns = append(patterns, pattern)
	}
	return patterns, nil
}

// isRawEntry reports whether a render-tree entry should be emitted verbatim.
// A per-entry "raw" boolean always takes precedence over rawGlobs: an explicit
// true forces verbatim, an explicit false forces rendering even if a pattern
// matches. When "raw" is absent, the entry is verbatim if its path matches any
// rawGlobs pattern. The second return value reports whether the per-entry "raw"
// field was present but not a bool, which the caller treats as an input error.
func isRawEntry(path string, entry map[string]any, rawGlobs []string) (raw, badType bool) {
	if v, present := entry["raw"]; present {
		b, ok := v.(bool)
		if !ok {
			return false, true
		}
		return b, false
	}
	// Normalize to forward slashes so glob matching is consistent across
	// platforms and matches the lint rule's semantics (filterOutRawTemplateFiles
	// also ToSlash-normalizes). doublestar.Match always treats "/" as the
	// separator.
	path = filepath.ToSlash(path)
	for _, pattern := range rawGlobs {
		// Invalid patterns are rejected up front by parseRawGlobs, so a match
		// error here is not expected; treat it as non-matching, best-effort.
		if matched, err := doublestar.Match(pattern, path); err == nil && matched {
			return true, false
		}
	}
	return false, false
}

// parseIgnoredBlocksConfig parses the ignoredBlocks input into a config slice without
// applying it to a specific template string. The actual replacements are built per-entry.
func parseIgnoredBlocksConfig(inputs map[string]any) []ignoredBlockConfig {
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
		token, _ := blockMap["token"].(string)

		configs = append(configs, ignoredBlockConfig{
			start: start,
			end:   end,
			line:  line,
			token: token,
		})
	}

	return configs
}

// buildIgnoredBlockReplacements builds gotmpl.Replacement entries for a specific template
// string based on the parsed ignored block config. The always-on built-in markers are
// applied in addition to the user-declared configs.
//
// Replacements are emitted in two passes: all start/end (fence) replacements first,
// then all line/token replacements. Because applyReplacements consumes replacements in
// order, this guarantees fenced regions are protected before any line/token scan runs,
// so a token/line entry that overlaps a fence cannot mutate the fenced text and prevent
// the fence from matching -- regardless of user entry order.
//
// Within each pass the always-on built-in markers are emitted before user-declared
// configs. This preserves the "always-on" contract: a user cannot shadow or override
// the built-in fence stripping (or the built-in per-line marker) by declaring the same
// markers in ignoredBlocks, because the built-in replacement is consumed first.
func buildIgnoredBlockReplacements(templateStr string, configs []ignoredBlockConfig) []gotmpl.Replacement {
	var replacements []gotmpl.Replacement

	// Always-on built-in markers first, then user-declared configs, so built-ins
	// take priority and cannot be shadowed/mutated by user entries.
	effective := make([]ignoredBlockConfig, 0, len(configs)+2)
	effective = append(effective, builtinIgnoredBlockConfigs()...)
	effective = append(effective, configs...)

	// Pass 1: start/end fence replacements take precedence.
	for _, cfg := range effective {
		if cfg.start == "" || cfg.end == "" {
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
			inner := afterStart[:endIdx]
			fullBlock := remaining[startIdx : startIdx+len(cfg.start)+endIdx+len(cfg.end)]
			repl := gotmpl.Replacement{Find: fullBlock}
			if cfg.stripMarkers {
				// Restore only the inner content, dropping the fence markers.
				repl.RestoreAs = inner
			}
			replacements = append(replacements, repl)
			remaining = remaining[startIdx+len(cfg.start)+endIdx+len(cfg.end):]
		}
	}

	// Pass 2: line/token replacements, applied after fences are protected.
	for _, cfg := range effective {
		switch {
		case cfg.token != "":
			if strings.Contains(templateStr, cfg.token) {
				replacements = append(replacements, gotmpl.Replacement{Find: cfg.token})
			}

		case cfg.line != "":
			for _, templateLine := range strings.Split(templateStr, "\n") {
				if strings.Contains(templateLine, cfg.line) {
					replacements = append(replacements, gotmpl.Replacement{Find: templateLine})
				}
			}
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

	// Fan-out (render-tree): forEach.in may reference resolvers, and pathTemplate
	// is a Go template that may reference resolvers. The fan-out aliases
	// (forEach.item/index) are locally bound, never resolver dependencies.
	fanOutAliases := extractFanOutDeps(inputs, addDep)

	// Get delimiters (default to standard Go template delimiters), shared by the
	// pathTemplate and inline-template scans below.
	leftDelim := "{{"
	rightDelim := "}}"
	if ld, ok := inputs["leftDelim"].(string); ok && ld != "" {
		leftDelim = ld
	}
	if rd, ok := inputs["rightDelim"].(string); ok && rd != "" {
		rightDelim = rd
	}

	// pathTemplate references (render-tree fan-out): scan for resolver deps,
	// excluding the fan-out aliases and reserved __* variables.
	if pathTmpl, ok := inputs["pathTemplate"].(string); ok && pathTmpl != "" {
		scan := resolveDepScanContext(inputs)
		if scan.Aliases == nil {
			scan.Aliases = make(map[string]bool, len(fanOutAliases))
		}
		for a := range fanOutAliases {
			scan.Aliases[a] = true
		}
		scan.Template = pathTmpl
		scan.LeftDelim = leftDelim
		scan.RightDelim = rightDelim
		for _, name := range gotmpl.ExtractResolverDeps(scan) {
			addDep(name)
		}
	}

	// For the "render" operation, also extract Go template references from
	// the template content (if provided as a literal string).
	templateContent, ok := inputs["template"].(string)
	if !ok {
		return deps
	}

	// Strip ignored/raw regions (built-in markers + declared ignoredBlocks)
	// so that references inside literal pass-through blocks are not mistaken
	// for resolver dependencies.
	ignoredCfg := parseIgnoredBlocksConfig(inputs)
	for _, repl := range buildIgnoredBlockReplacements(templateContent, ignoredCfg) {
		templateContent = strings.ReplaceAll(templateContent, repl.Find, "")
	}

	// Determine the template scan context (data keys + forEach aliases). Prefer
	// the context injected by the dependency-graph builder (which can see the
	// forEach clause and statically analyse CEL data expressions); otherwise
	// fall back to best-effort local computation from a literal data map.
	scan := resolveDepScanContext(inputs)
	scan.Template = templateContent
	scan.LeftDelim = leftDelim
	scan.RightDelim = rightDelim

	for _, name := range gotmpl.ExtractResolverDeps(scan) {
		addDep(name)
	}

	return deps
}

// extractFanOutDeps scans render-tree fan-out inputs (forEach.in) for resolver
// references via addDep and returns the fan-out alias names (item/index) so the
// caller can exclude them when scanning pathTemplate. A missing or non-map
// forEach yields no dependencies and an empty alias set.
func extractFanOutDeps(inputs map[string]any, addDep func(string)) map[string]bool {
	aliases := make(map[string]bool, 2)
	fe, ok := inputs["forEach"].(map[string]any)
	if !ok {
		return aliases
	}
	if item, ok := fe["item"].(string); ok && item != "" {
		aliases[item] = true
	}
	if index, ok := fe["index"].(string); ok && index != "" {
		aliases[index] = true
	}
	inRaw, ok := fe["in"].(map[string]any)
	if !ok {
		return aliases
	}
	if rslvr, ok := inRaw["rslvr"].(string); ok {
		if idx := strings.Index(rslvr, "."); idx > 0 {
			addDep(rslvr[:idx])
		} else {
			addDep(rslvr)
		}
	}
	if expr, ok := inRaw["expr"].(string); ok {
		extractCELDeps(expr, addDep)
	}
	// A {tmpl: ...} form of forEach.in is intentionally not scanned: a rendered
	// template yields a string, which fails the "must resolve to an array"
	// check at runtime, so it is never a valid fan-out collection.
	return aliases
}

// resolveDepScanContext builds the resolver-dependency scan context for the
// inline template. It prefers a DepScanContext injected under
// gotmpl.DepScanContextKey; when absent, it derives a best-effort context from a
// literal data map in the inputs.
func resolveDepScanContext(inputs map[string]any) gotmpl.ResolverDepsInput {
	if injected, ok := inputs[gotmpl.DepScanContextKey].(gotmpl.DepScanContext); ok {
		return gotmpl.ResolverDepsInput{
			HasDataInput:     injected.HasDataInput,
			DataKeys:         injected.DataKeys,
			DataKeysComplete: injected.DataKeysComplete,
			Aliases:          injected.Aliases,
		}
	}

	dataMap, hasData := inputs["data"].(map[string]any)
	if !hasData {
		return gotmpl.ResolverDepsInput{}
	}
	// A ValueRef-shaped data map (e.g. {expr: "..."}, {rslvr: "vars"},
	// {tmpl: "..."}) does not expose its runtime keys statically, so treat the
	// key set as dynamic/incomplete rather than mistaking the control key
	// (expr/rslvr/tmpl) for a data key.
	if isValueRefShapedData(dataMap) {
		return gotmpl.ResolverDepsInput{HasDataInput: true, DataKeysComplete: false}
	}
	dataKeys := make(map[string]bool, len(dataMap))
	for k := range dataMap {
		dataKeys[k] = true
	}
	return gotmpl.ResolverDepsInput{
		HasDataInput:     true,
		DataKeys:         dataKeys,
		DataKeysComplete: true,
	}
}

// isValueRefShapedData reports whether a raw data map is a ValueRef wrapper
// (expr/rslvr/tmpl) rather than a literal map of template context keys. Such
// wrappers resolve to their value at runtime, so their control key must not be
// treated as a data-context key.
func isValueRefShapedData(dataMap map[string]any) bool {
	if _, ok := dataMap["expr"]; ok {
		return true
	}
	if _, ok := dataMap["rslvr"]; ok {
		return true
	}
	if _, ok := dataMap["tmpl"]; ok {
		return true
	}
	return false
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
