// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapability_IsValid(t *testing.T) {
	tests := []struct {
		name       string
		capability Capability
		want       bool
	}{
		{"valid from", CapabilityFrom, true},
		{"valid transform", CapabilityTransform, true},
		{"valid validation", CapabilityValidation, true},
		{"valid authentication", CapabilityAuthentication, true},
		{"valid action", CapabilityAction, true},
		{"invalid capability", Capability("invalid"), false},
		{"empty capability", Capability(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.capability.IsValid())
		})
	}
}

func TestCapability_String(t *testing.T) {
	tests := []struct {
		name       string
		capability Capability
		want       string
	}{
		{"from capability", CapabilityFrom, "from"},
		{"validation capability", CapabilityValidation, "validation"},
		{"custom capability", Capability("custom"), "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.capability.String())
		})
	}
}

func TestDescriptor_Complete(t *testing.T) {
	version, err := semver.NewVersion("1.2.3")
	require.NoError(t, err)

	descriptor := &Descriptor{
		Name:        "test-provider",
		DisplayName: "Test Provider",
		APIVersion:  "v1",
		Version:     version,
		Description: "A test provider",
		Schema: schemahelper.ObjectSchema([]string{"name"}, map[string]*jsonschema.Schema{
			"name": schemahelper.StringProp("Resource name",
				schemahelper.WithMinLength(3),
				schemahelper.WithMaxLength(100),
				schemahelper.WithPattern("^[a-z0-9-]+$"),
				schemahelper.WithExample("test-resource"),
			),
			"count": schemahelper.IntProp("Number of resources",
				schemahelper.WithMinimum(0),
				schemahelper.WithMaximum(100),
				schemahelper.WithDefault(1),
				schemahelper.WithExample(5),
			),
			"tags": schemahelper.ArrayProp("Resource tags",
				schemahelper.WithMinItems(1),
				schemahelper.WithMaxItems(10),
			),
		}),
		OutputSchemas: map[Capability]*jsonschema.Schema{
			CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"id": schemahelper.StringProp("Resource ID",
					schemahelper.WithExample("res-123"),
				),
				"status": schemahelper.StringProp("Resource status",
					schemahelper.WithEnum("pending", "active", "deleted"),
					schemahelper.WithExample("active"),
				),
			}),
		},
		MockBehavior: "Returns mock resource",
		Capabilities: []Capability{CapabilityFrom, CapabilityAction},
		Category:     "Testing",
		Tags:         []string{"test", "mock"},
		Icon:         "test-icon",
		Links: []Link{
			{Name: "Documentation", URL: "https://example.com/docs"},
		},
		Examples: []Example{
			{
				Name:        "Basic usage",
				Description: "Creates a test resource",
				YAML:        "provider: test-provider\ninputs:\n  name: my-resource",
			},
		},
		Deprecated:  false,
		Beta:        true,
		Maintainers: []Contact{{Name: "Test User", Email: "test@example.com"}},
	}

	assert.Equal(t, "test-provider", descriptor.Name)
	assert.Equal(t, "1.2.3", descriptor.Version.String())
	assert.Len(t, descriptor.Schema.Properties, 3)
	assert.Len(t, descriptor.Capabilities, 2)
	assert.Contains(t, descriptor.Capabilities, CapabilityFrom)
	assert.True(t, descriptor.Beta)
}

func TestOutput_Structure(t *testing.T) {
	output := &Output{
		Data: map[string]any{
			"result": "success",
			"value":  42,
		},
		Warnings: []string{
			"This feature is deprecated",
			"Consider using the new API",
		},
		Metadata: map[string]any{
			"duration":    "100ms",
			"cached":      true,
			"executionId": "exec-123",
		},
	}

	assert.NotNil(t, output.Data)
	assert.Len(t, output.Warnings, 2)
	assert.Len(t, output.Metadata, 3)
	assert.Equal(t, "success", output.Data.(map[string]any)["result"])
	assert.Equal(t, true, output.Metadata["cached"])
}

func TestJsonSchema_Empty(t *testing.T) {
	schema := &jsonschema.Schema{Type: "object", Properties: map[string]*jsonschema.Schema{}}
	assert.NotNil(t, schema.Properties)
	assert.Len(t, schema.Properties, 0)
}

func TestJsonSchema_Pointers(t *testing.T) {
	prop := schemahelper.StringProp("",
		schemahelper.WithMinLength(5),
		schemahelper.WithMaxLength(100),
		schemahelper.WithMinimum(0),
		schemahelper.WithMaximum(100),
		schemahelper.WithMinItems(1),
		schemahelper.WithMaxItems(10),
	)

	require.NotNil(t, prop.MinLength)
	assert.Equal(t, 5, *prop.MinLength)
	assert.Equal(t, 100.0, *prop.Maximum)
}

// Benchmarks

func BenchmarkCapability_IsValid(b *testing.B) {
	capability := CapabilityFrom
	for i := 0; i < b.N; i++ {
		_ = capability.IsValid()
	}
}

func BenchmarkDescriptor_Creation(b *testing.B) {
	version, _ := semver.NewVersion("1.0.0")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &Descriptor{
			Name:         "test",
			APIVersion:   "v1",
			Version:      version,
			Description:  "Test provider",
			Schema:       schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{}),
			Capabilities: []Capability{CapabilityFrom},
		}
	}
}
