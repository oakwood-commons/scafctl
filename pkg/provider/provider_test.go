package provider

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPropertyType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		propType PropertyType
		want     bool
	}{
		{"valid string", PropertyTypeString, true},
		{"valid int", PropertyTypeInt, true},
		{"valid float", PropertyTypeFloat, true},
		{"valid bool", PropertyTypeBool, true},
		{"valid array", PropertyTypeArray, true},
		{"valid any", PropertyTypeAny, true},
		{"invalid type", PropertyType("invalid"), false},
		{"empty type", PropertyType(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.propType.IsValid())
		})
	}
}

func TestPropertyType_String(t *testing.T) {
	tests := []struct {
		name     string
		propType PropertyType
		want     string
	}{
		{"string type", PropertyTypeString, "string"},
		{"int type", PropertyTypeInt, "int"},
		{"custom type", PropertyType("custom"), "custom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.propType.String())
		})
	}
}

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
	minLen := 3
	maxLen := 100
	minValue := 0.0
	maxValue := 100.0
	minItems := 1
	maxItems := 10

	version, err := semver.NewVersion("1.2.3")
	require.NoError(t, err)

	descriptor := &Descriptor{
		Name:        "test-provider",
		DisplayName: "Test Provider",
		APIVersion:  "v1",
		Version:     version,
		Description: "A test provider",
		Schema: SchemaDefinition{
			Properties: map[string]PropertyDefinition{
				"name": {
					Type:        PropertyTypeString,
					Required:    true,
					Description: "Resource name",
					MinLength:   &minLen,
					MaxLength:   &maxLen,
					Pattern:     "^[a-z0-9-]+$",
					Example:     "test-resource",
				},
				"count": {
					Type:        PropertyTypeInt,
					Description: "Number of resources",
					Minimum:     &minValue,
					Maximum:     &maxValue,
					Default:     1,
					Example:     5,
				},
				"tags": {
					Type:        PropertyTypeArray,
					Description: "Resource tags",
					MinItems:    &minItems,
					MaxItems:    &maxItems,
				},
			},
		},
		OutputSchema: SchemaDefinition{
			Properties: map[string]PropertyDefinition{
				"id": {
					Type:        PropertyTypeString,
					Description: "Resource ID",
					Example:     "res-123",
				},
				"status": {
					Type:        PropertyTypeString,
					Description: "Resource status",
					Enum:        []any{"pending", "active", "deleted"},
					Example:     "active",
				},
			},
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

func TestSchemaDefinition_Empty(t *testing.T) {
	schema := SchemaDefinition{}
	assert.Nil(t, schema.Properties)

	schema.Properties = make(map[string]PropertyDefinition)
	assert.NotNil(t, schema.Properties)
	assert.Len(t, schema.Properties, 0)
}

func TestPropertyDefinition_Pointers(t *testing.T) {
	minLen := 5
	maxLen := 100
	minValue := 0.0
	maxValue := 100.0
	minItems := 1
	maxItems := 10

	prop := PropertyDefinition{
		Type:      PropertyTypeString,
		MinLength: &minLen,
		MaxLength: &maxLen,
		Minimum:   &minValue,
		Maximum:   &maxValue,
		MinItems:  &minItems,
		MaxItems:  &maxItems,
	}

	require.NotNil(t, prop.MinLength)
	assert.Equal(t, 5, *prop.MinLength)
	assert.Equal(t, 100.0, *prop.Maximum)
}

// Benchmarks

func BenchmarkPropertyType_IsValid(b *testing.B) {
	pt := PropertyTypeString
	for i := 0; i < b.N; i++ {
		_ = pt.IsValid()
	}
}

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
			Schema:       SchemaDefinition{},
			Capabilities: []Capability{CapabilityFrom},
		}
	}
}
