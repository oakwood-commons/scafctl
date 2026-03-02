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
	require.Contains(t, props, "template")
	require.Contains(t, props, "name")
	require.Contains(t, props, "missingKey")
	require.Contains(t, props, "leftDelim")
	require.Contains(t, props, "rightDelim")
	require.Contains(t, props, "data")
	require.Contains(t, props, "ignoredBlocks")

	// Check required fields
	assert.Contains(t, desc.Schema.Required, "template")
	assert.Contains(t, desc.Schema.Required, "name")

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
