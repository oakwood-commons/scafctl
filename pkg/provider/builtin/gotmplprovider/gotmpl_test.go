// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gotmplprovider

import (
	"context"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGoTemplateProvider(t *testing.T) {
	p := NewGoTemplateProvider()
	require.NotNil(t, p)

	desc := p.Descriptor()
	require.NotNil(t, desc)

	assert.Equal(t, ProviderName, desc.Name)
	assert.Equal(t, "Go Template Provider", desc.DisplayName)
	assert.Equal(t, "v1", desc.APIVersion)
	assert.NotNil(t, desc.Version)
	assert.Contains(t, desc.Capabilities, provider.CapabilityTransform)
	assert.Contains(t, desc.Capabilities, provider.CapabilityAction)
}

func TestGoTemplateProvider_Execute_SimpleTemplate(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	// Set resolver context with test data
	ctx = provider.WithResolverContext(ctx, map[string]any{
		"name": "World",
	})

	inputs := map[string]any{
		"name":     "simple-test",
		"template": "Hello, {{.name}}!",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "Hello, World!", output.Data)
}

func TestGoTemplateProvider_Execute_WithName(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"value": "test",
	})

	inputs := map[string]any{
		"template": "Value: {{.value}}",
		"name":     "my-template",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	assert.Equal(t, "Value: test", output.Data)
	assert.Equal(t, "my-template", output.Metadata["templateName"])
}

func TestGoTemplateProvider_Execute_Conditional(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	tests := []struct {
		name     string
		env      string
		expected string
	}{
		{
			name:     "production",
			env:      "production",
			expected: "PROD",
		},
		{
			name:     "development",
			env:      "development",
			expected: "DEV",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx = provider.WithResolverContext(ctx, map[string]any{
				"environment": tt.env,
			})

			inputs := map[string]any{
				"name":     "conditional-test",
				"template": `{{if eq .environment "production"}}PROD{{else}}DEV{{end}}`,
			}

			output, err := p.Execute(ctx, inputs)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, output.Data)
		})
	}
}

func TestGoTemplateProvider_Execute_Range(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"items": []string{"a", "b", "c"},
	})

	inputs := map[string]any{
		"name":     "range-test",
		"template": "{{range .items}}[{{.}}]{{end}}",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "[a][b][c]", output.Data)
}

func TestGoTemplateProvider_Execute_CustomDelimiters(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"name": "test",
	})

	inputs := map[string]any{
		"name":       "custom-delim-test",
		"template":   `{"name": "<%.name%>", "brackets": "{{literal}}"}`,
		"leftDelim":  "<%",
		"rightDelim": "%>",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, `{"name": "test", "brackets": "{{literal}}"}`, output.Data)
}

func TestGoTemplateProvider_Execute_MissingKeyDefault(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{})

	inputs := map[string]any{
		"name":       "missing-key-default-test",
		"template":   "Value: {{.missing}}",
		"missingKey": "default",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "Value: <no value>", output.Data)
}

func TestGoTemplateProvider_Execute_MissingKeyZero(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	// With missingKey=zero, a missing key in a map returns the zero value for the map's value type
	// Since our map is map[string]any, the zero value is nil, which still prints "<no value>"
	// This behavior is consistent with Go's text/template package
	ctx = provider.WithResolverContext(ctx, map[string]any{})

	inputs := map[string]any{
		"name":       "missing-key-zero-test",
		"template":   "Value: {{.missing}}",
		"missingKey": "zero",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	// For map[string]any, zero value of any is nil, which prints "<no value>"
	assert.Equal(t, "Value: <no value>", output.Data)
}

func TestGoTemplateProvider_Execute_MissingKeyError(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{})

	inputs := map[string]any{
		"name":       "missing-key-error-test",
		"template":   "Value: {{.missing}}",
		"missingKey": "error",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing")
}

func TestGoTemplateProvider_Execute_InvalidMissingKey(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"name":       "invalid-missing-key-test",
		"template":   "test",
		"missingKey": "invalid",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid missingKey")
}

func TestGoTemplateProvider_Execute_WithAdditionalData(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"name": "World",
	})

	inputs := map[string]any{
		"name":     "additional-data-test",
		"template": "{{.prefix}}{{.name}}{{.suffix}}",
		"data": map[string]any{
			"prefix": "Hello, ",
			"suffix": "!",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", output.Data)
}

func TestGoTemplateProvider_Execute_DataOverridesResolverContext(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"value": "original",
	})

	inputs := map[string]any{
		"name":     "override-test",
		"template": "Value: {{.value}}",
		"data": map[string]any{
			"value": "overridden",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "Value: overridden", output.Data)
}

func TestGoTemplateProvider_Execute_InvalidInput(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	_, err := p.Execute(ctx, "not a map")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected map[string]any")
}

func TestGoTemplateProvider_Execute_MissingTemplate(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	_, err := p.Execute(ctx, map[string]any{
		"name": "test",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "template is required")
}

func TestGoTemplateProvider_Execute_MissingName(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	_, err := p.Execute(ctx, map[string]any{
		"template": "Hello",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestGoTemplateProvider_Execute_EmptyTemplate(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	_, err := p.Execute(ctx, map[string]any{
		"name":     "empty-test",
		"template": "",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "template is required")
}

func TestGoTemplateProvider_Execute_InvalidTemplate(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"name":     "invalid-template-test",
		"template": "{{.unclosed",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
}

func TestGoTemplateProvider_Execute_DryRun(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()
	ctx = provider.WithDryRun(ctx, true)

	inputs := map[string]any{
		"template": "Hello, {{.name}}!",
		"name":     "test-template",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	// Should return dry-run placeholder
	result, ok := output.Data.(string)
	require.True(t, ok)
	assert.Contains(t, result, "[DRY-RUN]")
	assert.Contains(t, result, "Template not rendered")

	// Metadata should indicate dry-run
	assert.True(t, output.Metadata["dryRun"].(bool))
	assert.Equal(t, "test-template", output.Metadata["templateName"])
}

func TestGoTemplateProvider_Execute_DryRunLongTemplate(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()
	ctx = provider.WithDryRun(ctx, true)

	// Create a template longer than 100 characters
	longTemplate := strings.Repeat("x", 150)

	inputs := map[string]any{
		"template": longTemplate,
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	result, ok := output.Data.(string)
	require.True(t, ok)
	// Should be truncated with "..."
	assert.Contains(t, result, "...")
}

func TestGoTemplateProvider_Execute_NestedData(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"config": map[string]any{
			"server": map[string]any{
				"host": "localhost",
				"port": 8080,
			},
		},
	})

	inputs := map[string]any{
		"name":     "nested-data-test",
		"template": "{{.config.server.host}}:{{.config.server.port}}",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "localhost:8080", output.Data)
}

func TestGoTemplateProvider_Execute_WithIterationContext(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"prefix": "Item",
	})

	ctx = provider.WithIterationContext(ctx, &provider.IterationContext{
		Item:       "test-item",
		Index:      5,
		ItemAlias:  "current",
		IndexAlias: "idx",
	})

	inputs := map[string]any{
		"name":     "iteration-test",
		"template": "{{.prefix}} {{.idx}}: {{.current}}",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "Item 5: test-item", output.Data)
}

func TestGoTemplateProvider_Descriptor_Validation(t *testing.T) {
	p := NewGoTemplateProvider()
	desc := p.Descriptor()

	// Validate the descriptor meets requirements
	err := provider.ValidateDescriptor(desc)
	require.NoError(t, err)
}

func TestGoTemplateProvider_Descriptor_Schema(t *testing.T) {
	p := NewGoTemplateProvider()
	desc := p.Descriptor()

	// Check required properties exist
	props := desc.Schema.Properties
	require.Contains(t, props, "operation")
	require.Contains(t, props, "template")
	require.Contains(t, props, "name")
	require.Contains(t, props, "missingKey")
	require.Contains(t, props, "leftDelim")
	require.Contains(t, props, "rightDelim")
	require.Contains(t, props, "data")
	require.Contains(t, props, "ignoredBlocks")
	require.Contains(t, props, "entries")

	// Check required fields - name is no longer globally required (optional for render-tree)
	// No fields are globally required (template or entries depend on operation, name defaults for render-tree)
	assert.Empty(t, desc.Schema.Required)

	// template is no longer globally required (only for render operation)
	assert.NotContains(t, desc.Schema.Required, "template")

	// Check optional fields are not required
	assert.NotContains(t, desc.Schema.Required, "missingKey")
	assert.NotContains(t, desc.Schema.Required, "data")
	assert.NotContains(t, desc.Schema.Required, "ignoredBlocks")
}

func TestGoTemplateProvider_Execute_IgnoredBlocks(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"name":     "my-resource",
		"location": "eastus",
	})

	inputs := map[string]any{
		"name": "ignored-blocks-test",
		"template": `resource "azurerm_resource_group" "rg" {
  name     = "{{.name}}"
  location = "{{.location}}"
  /*scafctl:ignore:start*/
  for_each = { for k, v in var.items : k => v }
  /*scafctl:ignore:end*/
}`,
		"ignoredBlocks": []any{
			map[string]any{
				"start": "/*scafctl:ignore:start*/",
				"end":   "/*scafctl:ignore:end*/",
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(string)
	require.True(t, ok)

	// Verify template variables were rendered
	assert.Contains(t, result, `name     = "my-resource"`)
	assert.Contains(t, result, `location = "eastus"`)

	// Verify ignored block markers and content were preserved literally
	assert.Contains(t, result, "/*scafctl:ignore:start*/")
	assert.Contains(t, result, "/*scafctl:ignore:end*/")
	assert.Contains(t, result, "for_each = { for k, v in var.items : k => v }")
}

func TestGoTemplateProvider_Execute_IgnoredBlocks_Multiple(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"value": "rendered",
	})

	inputs := map[string]any{
		"name":     "multi-blocks-test",
		"template": `before-{{.value}}-<!--preserve-->{{ .broken }}<!--/preserve-->-after-{{.value}}`,
		"ignoredBlocks": []any{
			map[string]any{
				"start": "<!--preserve-->",
				"end":   "<!--/preserve-->",
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	result, ok := output.Data.(string)
	require.True(t, ok)

	assert.Contains(t, result, "before-rendered-")
	assert.Contains(t, result, "<!--preserve-->{{ .broken }}<!--/preserve-->")
	assert.Contains(t, result, "-after-rendered")
}

func TestGoTemplateProvider_Execute_IgnoredBlocks_Empty(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"name": "test",
	})

	// Empty/invalid blocks should be silently skipped
	inputs := map[string]any{
		"name":     "empty-blocks-test",
		"template": "Hello, {{.name}}!",
		"ignoredBlocks": []any{
			map[string]any{"start": "", "end": ""},
			map[string]any{"start": "something", "end": ""},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "Hello, test!", output.Data)
}

func TestGoTemplateProvider_Execute_IgnoredBlocks_Line(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"appName": "my-app",
	})

	inputs := map[string]any{
		"name": "line-ignore-test",
		"template": `name: {{.appName}}
steps:
  - run: echo ${{ secrets.TOKEN }}  # scafctl:ignore
  - run: echo "deployed"`,
		"ignoredBlocks": []any{
			map[string]any{
				"line": "# scafctl:ignore",
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(string)
	require.True(t, ok)

	// Template variable should be rendered
	assert.Contains(t, result, "name: my-app")
	// The ignored line should be preserved literally (including the ${{ }} syntax)
	assert.Contains(t, result, "${{ secrets.TOKEN }}")
	assert.Contains(t, result, "# scafctl:ignore")
	// Non-ignored line should still be present
	assert.Contains(t, result, `echo "deployed"`)
}

func TestGoTemplateProvider_Execute_IgnoredBlocks_Line_Multiple(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"env": "prod",
	})

	inputs := map[string]any{
		"name": "multi-line-ignore-test",
		"template": `env: {{.env}}
line1: ${{ secrets.A }}  # scafctl:ignore
line2: normal content
line3: ${{ secrets.B }}  # scafctl:ignore`,
		"ignoredBlocks": []any{
			map[string]any{
				"line": "# scafctl:ignore",
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(string)
	require.True(t, ok)

	assert.Contains(t, result, "env: prod")
	assert.Contains(t, result, "${{ secrets.A }}")
	assert.Contains(t, result, "${{ secrets.B }}")
	assert.Contains(t, result, "normal content")
}

func TestGoTemplateProvider_Execute_IgnoredBlocks_Line_MutualExclusion(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"name": "test",
	})

	// Using both line and start/end should produce an error
	inputs := map[string]any{
		"name":     "mutual-exclusion-test",
		"template": "Hello, {{.name}}!",
		"ignoredBlocks": []any{
			map[string]any{
				"line":  "# scafctl:ignore",
				"start": "/*start*/",
				"end":   "/*end*/",
			},
		},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestGoTemplateProvider_Execute_IgnoredBlocks_Line_MixedEntries(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"name": "my-resource",
	})

	// Different entries can use different modes
	inputs := map[string]any{
		"name": "mixed-entries-test",
		"template": `resource "aws_instance" "main" {
  name = "{{.name}}"
  secret = ${{ secrets.KEY }}  # scafctl:ignore
  /*preserve:start*/
  tags = { for k, v in var.x : k => v }
  /*preserve:end*/
}`,
		"ignoredBlocks": []any{
			map[string]any{
				"line": "# scafctl:ignore",
			},
			map[string]any{
				"start": "/*preserve:start*/",
				"end":   "/*preserve:end*/",
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	result, ok := output.Data.(string)
	require.True(t, ok)

	// Template variable rendered
	assert.Contains(t, result, `name = "my-resource"`)
	// Line-ignored content preserved
	assert.Contains(t, result, "${{ secrets.KEY }}")
	// Block-ignored content preserved
	assert.Contains(t, result, "for k, v in var.x : k => v")
}

// --- render-tree tests ---

func TestGoTemplateProvider_RenderTree_BasicMultipleEntries(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{})

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "test-tree",
		"entries": []any{
			map[string]any{
				"path":    "app/deployment.yaml.tpl",
				"content": "name: {{ .appName }}\nreplicas: {{ .replicas }}",
			},
			map[string]any{
				"path":    "app/service.yaml.tpl",
				"content": "service: {{ .appName }}-svc\nport: {{ .port }}",
			},
		},
		"data": map[string]any{
			"appName":  "my-app",
			"replicas": 3,
			"port":     8080,
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	results, ok := output.Data.([]map[string]any)
	require.True(t, ok)
	require.Len(t, results, 2)

	// First entry
	assert.Equal(t, "app/deployment.yaml.tpl", results[0]["path"])
	assert.Equal(t, "name: my-app\nreplicas: 3", results[0]["content"])

	// Second entry
	assert.Equal(t, "app/service.yaml.tpl", results[1]["path"])
	assert.Equal(t, "service: my-app-svc\nport: 8080", results[1]["content"])

	// Metadata
	assert.Equal(t, "test-tree", output.Metadata["templateName"])
	assert.Equal(t, 2, output.Metadata["entryCount"])
}

func TestGoTemplateProvider_RenderTree_EmptyEntries(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "empty-tree",
		"entries":   []any{},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	results, ok := output.Data.([]map[string]any)
	require.True(t, ok)
	assert.Empty(t, results)
	assert.Equal(t, 0, output.Metadata["entryCount"])
}

func TestGoTemplateProvider_RenderTree_MissingEntries(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "missing-entries",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entries is required")
}

func TestGoTemplateProvider_RenderTree_InvalidEntryType(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "invalid-entries",
		"entries":   []any{"not-a-map"},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entries[0] must be a map")
}

func TestGoTemplateProvider_RenderTree_MissingEntryPath(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "no-path",
		"entries": []any{
			map[string]any{
				"content": "hello",
			},
		},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "entries[0].path is required")
}

func TestGoTemplateProvider_RenderTree_SkipsEntriesWithoutContent(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "skip-no-content",
		"entries": []any{
			map[string]any{
				"path":    "good.txt",
				"content": "Hello {{ .name }}",
			},
			map[string]any{
				"path": "no-content.bin",
				// no content field
			},
		},
		"data": map[string]any{"name": "World"},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	results, ok := output.Data.([]map[string]any)
	require.True(t, ok)
	assert.Len(t, results, 1)
	assert.Equal(t, "good.txt", results[0]["path"])
	assert.Equal(t, "Hello World", results[0]["content"])

	// Should have a warning for the skipped entry
	assert.Len(t, output.Warnings, 1)
	assert.Contains(t, output.Warnings[0], "no-content.bin")
}

func TestGoTemplateProvider_RenderTree_ErrorIncludesPath(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation":  "render-tree",
		"name":       "error-path",
		"missingKey": "error",
		"entries": []any{
			map[string]any{
				"path":    "broken/template.tpl",
				"content": "{{ .nonExistentVar }}",
			},
		},
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken/template.tpl")
}

func TestGoTemplateProvider_RenderTree_WithResolverContext(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"env": "production",
	})

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "resolver-ctx",
		"entries": []any{
			map[string]any{
				"path":    "config.yaml",
				"content": "environment: {{ .env }}",
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	results, ok := output.Data.([]map[string]any)
	require.True(t, ok)
	require.Len(t, results, 1)
	assert.Equal(t, "environment: production", results[0]["content"])
}

func TestGoTemplateProvider_RenderTree_DataOverridesResolverContext(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"env": "staging",
	})

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "data-override",
		"entries": []any{
			map[string]any{
				"path":    "config.yaml",
				"content": "environment: {{ .env }}",
			},
		},
		"data": map[string]any{
			"env": "production",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	results, ok := output.Data.([]map[string]any)
	require.True(t, ok)
	require.Len(t, results, 1)
	assert.Equal(t, "environment: production", results[0]["content"])
}

func TestGoTemplateProvider_RenderTree_DryRun(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()
	ctx = provider.WithDryRun(ctx, true)

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "dry-run-tree",
		"entries": []any{
			map[string]any{
				"path":    "a.tpl",
				"content": "{{ .val }}",
			},
			map[string]any{
				"path":    "b/c.tpl",
				"content": "{{ .val }}",
			},
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	require.NotNil(t, output)

	results, ok := output.Data.([]map[string]any)
	require.True(t, ok)
	assert.Len(t, results, 2)
	assert.Equal(t, "a.tpl", results[0]["path"])
	assert.Equal(t, "[dry-run rendered]", results[0]["content"])
	assert.Equal(t, "b/c.tpl", results[1]["path"])
	assert.Equal(t, "[dry-run rendered]", results[1]["content"])

	assert.True(t, output.Metadata["dryRun"].(bool))
}

func TestGoTemplateProvider_RenderTree_PathsPassThrough(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "render-tree",
		"name":      "path-passthrough",
		"entries": []any{
			map[string]any{
				"path":    "child1/grandchild1/app.deployment.tpl",
				"content": "name: {{ .appName }}",
			},
			map[string]any{
				"path":    "child2/service.yaml.tpl",
				"content": "svc: {{ .appName }}",
			},
		},
		"data": map[string]any{
			"appName": "test",
		},
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)

	results, ok := output.Data.([]map[string]any)
	require.True(t, ok)
	require.Len(t, results, 2)

	// Paths are preserved exactly as provided
	assert.Equal(t, "child1/grandchild1/app.deployment.tpl", results[0]["path"])
	assert.Equal(t, "child2/service.yaml.tpl", results[1]["path"])
}

func TestGoTemplateProvider_RenderTree_DefaultOperationIsRender(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	ctx = provider.WithResolverContext(ctx, map[string]any{
		"name": "World",
	})

	// No operation specified - should default to render
	inputs := map[string]any{
		"name":     "default-op",
		"template": "Hello, {{.name}}!",
	}

	output, err := p.Execute(ctx, inputs)
	require.NoError(t, err)
	assert.Equal(t, "Hello, World!", output.Data)
}

func TestGoTemplateProvider_RenderTree_UnsupportedOperation(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()

	inputs := map[string]any{
		"operation": "invalid-op",
		"name":      "bad-op",
	}

	_, err := p.Execute(ctx, inputs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported operation")
}

func TestGoTemplateProvider_WhatIf_Operations(t *testing.T) {
	p := NewGoTemplateProvider()
	ctx := context.Background()
	desc := p.Descriptor()
	require.NotNil(t, desc.WhatIf)

	tests := []struct {
		name     string
		input    any
		contains string
	}{
		{
			name:     "render with name",
			input:    map[string]any{"operation": "render", "name": "my-template"},
			contains: "my-template",
		},
		{
			name:     "render without name",
			input:    map[string]any{"operation": "render"},
			contains: "Go template",
		},
		{
			name:     "render-tree with name",
			input:    map[string]any{"operation": "render-tree", "name": "my-tree"},
			contains: "my-tree",
		},
		{
			name:     "render-tree without name",
			input:    map[string]any{"operation": "render-tree"},
			contains: "template tree",
		},
		{
			name:     "default operation (empty string defaults to render)",
			input:    map[string]any{"operation": "", "name": ""},
			contains: "Go template",
		},
		{
			name:     "non-map input returns empty",
			input:    "not-a-map",
			contains: "",
		},
		{
			name:     "unknown operation",
			input:    map[string]any{"operation": "generate"},
			contains: "generate",
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

func TestExtractDependencies(t *testing.T) {
	t.Run("empty inputs", func(t *testing.T) {
		deps := extractDependencies(map[string]any{})
		assert.Empty(t, deps)
	})

	t.Run("rslvr in data with dot path", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"data": map[string]any{
				"rslvr": "myresolver.field",
			},
		})
		assert.Equal(t, []string{"myresolver"}, deps)
	})

	t.Run("rslvr in entries without dot", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"entries": map[string]any{
				"rslvr": "simpleresolver",
			},
		})
		assert.Equal(t, []string{"simpleresolver"}, deps)
	})

	t.Run("expr in data with CEL reference", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"data": map[string]any{
				"expr": "_.myresolver + _.other",
			},
		})
		assert.Contains(t, deps, "myresolver")
		assert.Contains(t, deps, "other")
	})

	t.Run("template string with resolver references", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"template": "Hello {{ ._.config }}!",
		})
		assert.Contains(t, deps, "config")
	})

	t.Run("template key is non-string map (rslvr)", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"template": map[string]any{
				"rslvr": "tplresolver",
			},
		})
		assert.Contains(t, deps, "tplresolver")
	})

	t.Run("template string with custom delimiters", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"template":   "Hello [[ ._.mydata ]]!",
			"leftDelim":  "[[",
			"rightDelim": "]]",
		})
		assert.Contains(t, deps, "mydata")
	})

	t.Run("non-map value for entries key skipped", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"entries": "not-a-map",
		})
		assert.Empty(t, deps)
	})

	t.Run("deduplication", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"data": map[string]any{
				"rslvr": "myresolver",
			},
			"template": "{{ ._.myresolver.field }}",
		})
		count := 0
		for _, d := range deps {
			if d == "myresolver" {
				count++
			}
		}
		assert.Equal(t, 1, count, "should deduplicate myresolver")
	})
}

func TestExtractCELDeps(t *testing.T) {
	t.Run("basic reference", func(t *testing.T) {
		var found []string
		extractCELDeps("_.config.host", func(name string) {
			found = append(found, name)
		})
		assert.Equal(t, []string{"config"}, found)
	})

	t.Run("multiple references", func(t *testing.T) {
		var found []string
		extractCELDeps("_.alpha + _.beta", func(name string) {
			found = append(found, name)
		})
		assert.Contains(t, found, "alpha")
		assert.Contains(t, found, "beta")
	})

	t.Run("no references", func(t *testing.T) {
		var found []string
		extractCELDeps("someValue == 42", func(name string) {
			found = append(found, name)
		})
		assert.Empty(t, found)
	})

	t.Run("short string no panic", func(t *testing.T) {
		var found []string
		extractCELDeps("_.", func(name string) {
			found = append(found, name)
		})
		assert.Empty(t, found)
	})
}

func TestExtractDependencies_DataKeyExclusion(t *testing.T) {
	t.Run("data keys excluded from template refs", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"template": "{{ toYaml .config }}",
			"data": map[string]any{
				"config": map[string]any{"port": 8080},
			},
		})
		for _, d := range deps {
			assert.NotEqual(t, "config", d, "config should be excluded (provided by data)")
		}
	})

	t.Run("resolver ref in template not excluded", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"template": "{{ ._.environment }} {{ .config }}",
			"data": map[string]any{
				"config": map[string]any{"port": 8080},
			},
		})
		assert.Contains(t, deps, "environment", "resolver ref should be extracted")
		for _, d := range deps {
			assert.NotEqual(t, "config", d, "config should be excluded")
		}
	})

	t.Run("no data input does not exclude", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"template": "{{ .config }}",
		})
		assert.Contains(t, deps, "config")
	})

	t.Run("multiple data keys all excluded", func(t *testing.T) {
		deps := extractDependencies(map[string]any{
			"template": "{{ .config }} {{ .labels }} {{ ._.resolver1 }}",
			"data": map[string]any{
				"config": map[string]any{"port": 8080},
				"labels": map[string]any{"app": "test"},
			},
		})
		assert.Contains(t, deps, "resolver1")
		for _, d := range deps {
			assert.NotEqual(t, "config", d)
			assert.NotEqual(t, "labels", d)
		}
	})
}
