// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeMinimalDescriptor(name string) provider.Descriptor {
	return provider.Descriptor{
		Name:        name,
		DisplayName: name + "-display",
		APIVersion:  "v1",
		Version:     semver.MustParse("1.0.0"),
		Description: "A test provider for unit tests.",
		Schema:      &jsonschema.Schema{},
		OutputSchemas: map[provider.Capability]*jsonschema.Schema{
			provider.CapabilityFrom: {},
		},
		Capabilities: []provider.Capability{provider.CapabilityFrom},
	}
}

func TestCapabilitiesToStrings_Empty(t *testing.T) {
	result := CapabilitiesToStrings(nil)
	assert.Empty(t, result)
}

func TestCapabilitiesToStrings_Multiple(t *testing.T) {
	caps := []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform, provider.CapabilityValidation}
	result := CapabilitiesToStrings(caps)
	assert.Equal(t, []string{"from", "transform", "validation"}, result)
}

func TestSchemaPlaceholder_Nil(t *testing.T) {
	result := SchemaPlaceholder("myField", nil)
	assert.Equal(t, "<value>", result)
}

func TestSchemaPlaceholder_WithExample(t *testing.T) {
	prop := &jsonschema.Schema{Examples: []any{"hello"}}
	result := SchemaPlaceholder("field", prop)
	assert.Contains(t, result, "hello")
}

func TestSchemaPlaceholder_WithEnum(t *testing.T) {
	prop := &jsonschema.Schema{Enum: []any{"option1"}}
	result := SchemaPlaceholder("field", prop)
	assert.Contains(t, result, "option1")
}

func TestSchemaPlaceholder_TypeString(t *testing.T) {
	prop := &jsonschema.Schema{Type: "string"}
	result := SchemaPlaceholder("myField", prop)
	assert.Equal(t, "<myField>", result)
}

func TestSchemaPlaceholder_TypeInteger(t *testing.T) {
	prop := &jsonschema.Schema{Type: "integer"}
	result := SchemaPlaceholder("count", prop)
	assert.Equal(t, "0", result)
}

func TestSchemaPlaceholder_TypeNumber(t *testing.T) {
	prop := &jsonschema.Schema{Type: "number"}
	result := SchemaPlaceholder("amount", prop)
	assert.Equal(t, "0", result)
}

func TestSchemaPlaceholder_TypeBoolean(t *testing.T) {
	prop := &jsonschema.Schema{Type: "boolean"}
	result := SchemaPlaceholder("enabled", prop)
	assert.Equal(t, "true", result)
}

func TestSchemaPlaceholder_TypeArray(t *testing.T) {
	prop := &jsonschema.Schema{Type: "array"}
	result := SchemaPlaceholder("items", prop)
	assert.Contains(t, result, "items")
}

func TestSchemaPlaceholder_TypeObject(t *testing.T) {
	prop := &jsonschema.Schema{Type: "object"}
	result := SchemaPlaceholder("config", prop)
	assert.Equal(t, "@config.yaml", result)
}

func TestSchemaPlaceholder_TypeDefault(t *testing.T) {
	prop := &jsonschema.Schema{Type: "unknown-type"}
	result := SchemaPlaceholder("val", prop)
	assert.Equal(t, "<val>", result)
}

func TestBuildSchemaOutput_Nil(t *testing.T) {
	result := BuildSchemaOutput(nil)
	assert.Nil(t, result)
}

func TestBuildSchemaOutput_EmptyProperties(t *testing.T) {
	result := BuildSchemaOutput(&jsonschema.Schema{})
	assert.Nil(t, result)
}

func TestBuildSchemaOutput_WithProperties(t *testing.T) {
	schema := &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"name": {Type: "string", Description: "The name field"},
		},
		Required: []string{"name"},
	}
	result := BuildSchemaOutput(schema)
	require.NotNil(t, result)
	props, ok := result["properties"].(map[string]any)
	require.True(t, ok)
	nameField, ok := props["name"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", nameField["type"])
	assert.Equal(t, "The name field", nameField["description"])
	assert.Equal(t, true, nameField["required"])
}

func TestGenerateCLIExamples_NilSchema(t *testing.T) {
	desc := makeMinimalDescriptor("test")
	desc.Schema = nil
	result := GenerateCLIExamples(&desc)
	assert.Nil(t, result)
}

func TestGenerateCLIExamples_EmptyProperties(t *testing.T) {
	desc := makeMinimalDescriptor("test")
	// makeMinimalDescriptor sets Schema with no properties, so GenerateCLIExamples returns nil
	result := GenerateCLIExamples(&desc)
	assert.Nil(t, result)
}

func TestGenerateCLIExamples_WithRequiredFields(t *testing.T) {
	desc := makeMinimalDescriptor("test")
	desc.Schema = &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"url": {Type: "string"},
		},
		Required: []string{"url"},
	}
	result := GenerateCLIExamples(&desc)
	require.NotEmpty(t, result)
	assert.Contains(t, result[0], "url=<url>")
	assert.Contains(t, result[0], "scafctl run provider test")
}

func TestGenerateCLIExamples_MultipleCapabilities(t *testing.T) {
	desc := makeMinimalDescriptor("multi")
	desc.Capabilities = []provider.Capability{provider.CapabilityFrom, provider.CapabilityTransform}
	desc.Schema = &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"field": {Type: "string"},
		},
	}
	result := GenerateCLIExamples(&desc)
	require.GreaterOrEqual(t, len(result), 2)
	// Should have an example with --capability transform
	hasCapExample := false
	for _, ex := range result {
		if contains(ex, "--capability transform") {
			hasCapExample = true
			break
		}
	}
	assert.True(t, hasCapExample, "expected an example with --capability transform")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && searchString(s, substr))
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestBuildProviderDetail_Minimal(t *testing.T) {
	desc := makeMinimalDescriptor("myprovider")
	result := BuildProviderDetail(desc)
	assert.Equal(t, "myprovider", result["name"])
	assert.Equal(t, "myprovider-display", result["displayName"])
	assert.Equal(t, "v1", result["apiVersion"])
	assert.Equal(t, "1.0.0", result["version"])
	assert.Equal(t, "A test provider for unit tests.", result["description"])
}

func TestBuildProviderDetail_WithAllFields(t *testing.T) {
	desc := makeMinimalDescriptor("full")
	desc.Category = "network"
	desc.Tags = []string{"http", "rest"}
	desc.Icon = "https://example.com/icon.svg"
	desc.IsDeprecated = true
	desc.Beta = true
	desc.Links = []provider.Link{{Name: "Docs", URL: "https://docs.example.com"}}
	desc.Examples = []provider.Example{{Name: "Basic", Description: "Example", YAML: "provider: full\n"}}
	desc.Maintainers = []provider.Contact{{Name: "Test User", Email: "test@example.com"}}
	desc.Schema = &jsonschema.Schema{
		Properties: map[string]*jsonschema.Schema{
			"field": {Type: "string"},
		},
	}
	desc.OutputSchemas = map[provider.Capability]*jsonschema.Schema{
		provider.CapabilityFrom: {
			Properties: map[string]*jsonschema.Schema{
				"result": {Type: "string"},
			},
		},
	}

	result := BuildProviderDetail(desc)
	assert.Equal(t, "network", result["category"])
	assert.Equal(t, true, result["deprecated"])
	assert.Equal(t, true, result["beta"])
	assert.NotNil(t, result["links"])
	assert.NotNil(t, result["examples"])
	assert.NotNil(t, result["maintainers"])
	assert.NotNil(t, result["schema"])
	assert.NotNil(t, result["outputSchemas"])
	assert.NotNil(t, result["cliUsage"])
}
