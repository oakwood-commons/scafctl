// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test structs for introspection
type SimpleStruct struct {
	Name        string `json:"name" doc:"The name field" example:"test-name" required:"true"`
	Description string `json:"description,omitempty" doc:"Optional description" maxLength:"500"`
	Count       int    `json:"count" doc:"Item count" minimum:"0" maximum:"100" example:"42"`
}

type NestedStruct struct {
	ID       string       `json:"id" doc:"Unique identifier" required:"true"`
	Metadata MetadataInfo `json:"metadata" doc:"Metadata information"`
}

type MetadataInfo struct {
	Labels      map[string]string `json:"labels,omitempty" doc:"Key-value labels"`
	Annotations []string          `json:"annotations,omitempty" doc:"Annotations list" maxItems:"10"`
}

type PointerStruct struct {
	Required *string `json:"required" doc:"Required pointer field" required:"true"`
	Optional *int    `json:"optional,omitempty" doc:"Optional pointer field"`
}

type DeprecatedStruct struct {
	OldField string `json:"oldField" doc:"Deprecated field" deprecated:"true"`
	NewField string `json:"newField" doc:"New replacement field"`
}

type ValidationStruct struct {
	Email   string   `json:"email" doc:"Email address" format:"email" pattern:"^[a-z@.]+$" patternDescription:"lowercase email"`
	URL     string   `json:"url" doc:"Website URL" format:"uri"`
	Tags    []string `json:"tags" doc:"Tag list" minItems:"1" maxItems:"5"`
	Content string   `json:"content" doc:"Content body" minLength:"10" maxLength:"1000"`
}

func TestIntrospectType_SimpleStruct(t *testing.T) {
	info, err := IntrospectType(SimpleStruct{})
	require.NoError(t, err)

	assert.Equal(t, "SimpleStruct", info.Name)
	assert.Equal(t, "github.com/oakwood-commons/scafctl/pkg/schema", info.Package)
	require.Len(t, info.Fields, 3)

	// Check name field
	nameField := findField(info.Fields, "name")
	require.NotNil(t, nameField)
	assert.Equal(t, "The name field", nameField.Description)
	assert.Equal(t, "test-name", nameField.Example)
	assert.True(t, nameField.Required)
	assert.Equal(t, "string", nameField.Type)

	// Check count field
	countField := findField(info.Fields, "count")
	require.NotNil(t, countField)
	assert.Equal(t, "Item count", countField.Description)
	assert.NotNil(t, countField.Minimum)
	assert.Equal(t, float64(0), *countField.Minimum)
	assert.NotNil(t, countField.Maximum)
	assert.Equal(t, float64(100), *countField.Maximum)
}

func TestIntrospectType_NestedStruct(t *testing.T) {
	info, err := IntrospectType(NestedStruct{})
	require.NoError(t, err)

	assert.Equal(t, "NestedStruct", info.Name)
	require.Len(t, info.Fields, 2)

	// Check metadata field has nested fields
	metadataField := findField(info.Fields, "metadata")
	require.NotNil(t, metadataField)
	assert.Equal(t, "MetadataInfo", metadataField.Type)
	require.Len(t, metadataField.NestedFields, 2)

	// Check nested labels field
	labelsField := findField(metadataField.NestedFields, "labels")
	require.NotNil(t, labelsField)
	assert.Contains(t, labelsField.Type, "map[string]string")
}

func TestIntrospectType_PointerFields(t *testing.T) {
	info, err := IntrospectType(PointerStruct{})
	require.NoError(t, err)

	require.Len(t, info.Fields, 2)

	// Required pointer should still be required
	reqField := findField(info.Fields, "required")
	require.NotNil(t, reqField)
	assert.True(t, reqField.Required)
	assert.Equal(t, "string", reqField.Type)
	assert.True(t, reqField.IsPointer)

	// Optional pointer
	optField := findField(info.Fields, "optional")
	require.NotNil(t, optField)
	assert.False(t, optField.Required)
	assert.Equal(t, "int", optField.Type)
	assert.True(t, optField.IsPointer)
}

func TestIntrospectType_DeprecatedField(t *testing.T) {
	info, err := IntrospectType(DeprecatedStruct{})
	require.NoError(t, err)

	oldField := findField(info.Fields, "oldField")
	require.NotNil(t, oldField)
	assert.True(t, oldField.Deprecated)

	newField := findField(info.Fields, "newField")
	require.NotNil(t, newField)
	assert.False(t, newField.Deprecated)
}

func TestIntrospectType_ValidationTags(t *testing.T) {
	info, err := IntrospectType(ValidationStruct{})
	require.NoError(t, err)

	// Email field
	emailField := findField(info.Fields, "email")
	require.NotNil(t, emailField)
	assert.Equal(t, "email", emailField.Format)
	assert.Equal(t, "^[a-z@.]+$", emailField.Pattern)
	assert.Equal(t, "lowercase email", emailField.PatternDescription)

	// URL field
	urlField := findField(info.Fields, "url")
	require.NotNil(t, urlField)
	assert.Equal(t, "uri", urlField.Format)

	// Tags field (array validation)
	tagsField := findField(info.Fields, "tags")
	require.NotNil(t, tagsField)
	require.NotNil(t, tagsField.MinItems)
	assert.Equal(t, 1, *tagsField.MinItems)
	require.NotNil(t, tagsField.MaxItems)
	assert.Equal(t, 5, *tagsField.MaxItems)

	// Content field (string length)
	contentField := findField(info.Fields, "content")
	require.NotNil(t, contentField)
	require.NotNil(t, contentField.MinLength)
	assert.Equal(t, 10, *contentField.MinLength)
	require.NotNil(t, contentField.MaxLength)
	assert.Equal(t, 1000, *contentField.MaxLength)
}

func TestIntrospectField_NavigatePath(t *testing.T) {
	// Navigate to nested field
	info, err := IntrospectField(NestedStruct{}, "metadata.labels")
	require.NoError(t, err)
	assert.Equal(t, "labels", info.Name)
	assert.Contains(t, info.Type, "map[string]string")
}

func TestIntrospectField_InvalidPath(t *testing.T) {
	_, err := IntrospectField(NestedStruct{}, "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	_, err = IntrospectField(NestedStruct{}, "metadata.nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestIntrospectField_EmptyPath(t *testing.T) {
	// Empty path should error - use IntrospectType instead for top-level
	_, err := IntrospectField(SimpleStruct{}, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Helper to find a field by JSON name
func findField(fields []FieldInfo, name string) *FieldInfo {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}

func TestIntrospectType_MapFields(t *testing.T) {
	type MapStruct struct {
		StringMap map[string]string      `json:"stringMap" doc:"String map"`
		AnyMap    map[string]interface{} `json:"anyMap" doc:"Any map"`
	}

	info, err := IntrospectType(MapStruct{})
	require.NoError(t, err)
	require.Len(t, info.Fields, 2)

	stringMapField := findField(info.Fields, "stringMap")
	require.NotNil(t, stringMapField)
	assert.Contains(t, stringMapField.Type, "map[string]string")

	anyMapField := findField(info.Fields, "anyMap")
	require.NotNil(t, anyMapField)
	assert.Contains(t, anyMapField.Type, "map")
}

func TestIntrospectType_SliceFields(t *testing.T) {
	type SliceStruct struct {
		StringSlice []string `json:"stringSlice" doc:"String slice"`
		IntSlice    []int    `json:"intSlice" doc:"Int slice"`
	}

	info, err := IntrospectType(SliceStruct{})
	require.NoError(t, err)
	require.Len(t, info.Fields, 2)

	stringSliceField := findField(info.Fields, "stringSlice")
	require.NotNil(t, stringSliceField)
	assert.Equal(t, "[]string", stringSliceField.Type)

	intSliceField := findField(info.Fields, "intSlice")
	require.NotNil(t, intSliceField)
	assert.Equal(t, "[]int", intSliceField.Type)
}

func TestIntrospectType_AnonymousEmbedded(t *testing.T) {
	type BaseStruct struct {
		BaseField string `json:"baseField" doc:"Base field"`
	}
	type EmbeddedStruct struct {
		BaseStruct
		OwnField string `json:"ownField" doc:"Own field"`
	}

	info, err := IntrospectType(EmbeddedStruct{})
	require.NoError(t, err)

	// The embedded struct appears as a field named "BaseStruct" (anonymous embedding)
	// Note: Go reflection treats anonymous embedded structs as fields with the type name
	ownField := findField(info.Fields, "ownField")
	assert.NotNil(t, ownField, "own field should be present")

	// The embedded struct may appear as a nested field
	// This behavior depends on implementation - either as "BaseStruct" field
	// or with its fields flattened at the top level
	require.True(t, len(info.Fields) >= 1, "should have at least own field")
}

func TestIntrospectType_PrivateFieldsIgnored(t *testing.T) {
	type MixedStruct struct {
		PublicField  string `json:"publicField" doc:"Public"`
		privateField string //nolint:unused
	}

	info, err := IntrospectType(MixedStruct{})
	require.NoError(t, err)

	// Should only have public field
	assert.Len(t, info.Fields, 1)
	assert.Equal(t, "publicField", info.Fields[0].Name)
}

func TestIntrospectType_JsonOmitField(t *testing.T) {
	type OmitStruct struct {
		Visible string `json:"visible" doc:"Visible field"`
		Hidden  string `json:"-" doc:"Hidden field"`
	}

	info, err := IntrospectType(OmitStruct{})
	require.NoError(t, err)

	// Should only have visible field
	assert.Len(t, info.Fields, 1)
	assert.Equal(t, "visible", info.Fields[0].Name)
}

func TestIntrospectType_PointerInput(t *testing.T) {
	// Test that pointer input works
	info, err := IntrospectType(&SimpleStruct{})
	require.NoError(t, err)
	assert.Equal(t, "SimpleStruct", info.Name)
}

func TestIntrospectType_NilPointer(t *testing.T) {
	// Test that nil pointer of type works (common pattern)
	info, err := IntrospectType((*SimpleStruct)(nil))
	require.NoError(t, err)
	assert.Equal(t, "SimpleStruct", info.Name)
}
