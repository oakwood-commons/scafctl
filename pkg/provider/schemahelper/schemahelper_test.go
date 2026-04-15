// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schemahelper

import (
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReExports_ResolveCorrectly(t *testing.T) {
	tests := []struct {
		name     string
		schema   func() *jsonschema.Schema
		wantType string
	}{
		{
			name:     "StringProp",
			schema:   func() *jsonschema.Schema { return StringProp("a string field") },
			wantType: "string",
		},
		{
			name:     "IntProp",
			schema:   func() *jsonschema.Schema { return IntProp("an integer field") },
			wantType: "integer",
		},
		{
			name:     "NumberProp",
			schema:   func() *jsonschema.Schema { return NumberProp("a number field") },
			wantType: "number",
		},
		{
			name:     "BoolProp",
			schema:   func() *jsonschema.Schema { return BoolProp("a boolean field") },
			wantType: "boolean",
		},
		{
			name:     "ArrayProp",
			schema:   func() *jsonschema.Schema { return ArrayProp("an array field") },
			wantType: "array",
		},
		{
			name:     "AnyProp",
			schema:   func() *jsonschema.Schema { return AnyProp("an any field") },
			wantType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.schema()
			require.NotNil(t, s)
			assert.Equal(t, tt.wantType, s.Type)
		})
	}
}

func TestObjectSchema(t *testing.T) {
	s := ObjectSchema(
		[]string{"name"},
		map[string]*jsonschema.Schema{
			"name": StringProp("a name"),
		},
	)
	require.NotNil(t, s)
	assert.Equal(t, "object", s.Type)
	assert.Contains(t, s.Required, "name")
}

func TestPropOptions(t *testing.T) {
	s := StringProp("test",
		WithDescription("desc"),
		WithExample("ex"),
		WithPattern("^[a-z]+$"),
		WithMinLength(1),
		WithMaxLength(100),
		WithFormat("uri"),
		WithTitle("Title"),
	)
	require.NotNil(t, s)
	assert.Equal(t, "desc", s.Description)
	assert.Equal(t, "^[a-z]+$", s.Pattern)
	assert.Equal(t, "uri", s.Format)
	assert.Equal(t, "Title", s.Title)
	assert.Equal(t, 1, *s.MinLength)
	assert.Equal(t, 100, *s.MaxLength)
}

func TestNumericPropOptions(t *testing.T) {
	s := NumberProp("test",
		WithMinimum(0),
		WithMaximum(100),
	)
	require.NotNil(t, s)
	assert.Equal(t, float64(0), *s.Minimum)
	assert.Equal(t, float64(100), *s.Maximum)
}

func TestArrayPropOptions(t *testing.T) {
	s := ArrayProp("test",
		WithMinItems(1),
		WithMaxItems(10),
		WithItems(StringProp("item")),
	)
	require.NotNil(t, s)
	assert.Equal(t, 1, *s.MinItems)
	assert.Equal(t, 10, *s.MaxItems)
	assert.NotNil(t, s.Items)
}

func TestDeprecatedAndWriteOnly(t *testing.T) {
	s := StringProp("test", WithDeprecated(), WithWriteOnly())
	assert.True(t, s.Deprecated)
	assert.True(t, s.WriteOnly)
}

func TestWithEnum(t *testing.T) {
	s := StringProp("test", WithEnum("a", "b", "c"))
	assert.Len(t, s.Enum, 3)
}

func TestWithDefault(t *testing.T) {
	s := StringProp("test", WithDefault("hello"))
	assert.NotNil(t, s.Default)
}

func TestObjectProp(t *testing.T) {
	s := ObjectProp("an object", []string{"key"}, map[string]*jsonschema.Schema{
		"key": StringProp("a key"),
	})
	require.NotNil(t, s)
	assert.Equal(t, "object", s.Type)
	assert.Contains(t, s.Required, "key")
}

func TestWithAdditionalProperties(t *testing.T) {
	s := ObjectProp("map", nil, nil, WithAdditionalProperties(StringProp("value")))
	require.NotNil(t, s)
	assert.NotNil(t, s.AdditionalProperties)
}
