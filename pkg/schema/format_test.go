// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
)

func newTestFormatter(out *bytes.Buffer) *Formatter {
	streams := terminal.IOStreams{Out: out}
	w := writer.New(&streams, &settings.Run{NoColor: true})
	return NewFormatterWithWriter(w)
}

func TestDefaultFormatOptions(t *testing.T) {
	opts := DefaultFormatOptions()
	assert.True(t, opts.ShowNestedFields)
	assert.Equal(t, 2, opts.MaxDepth)
	assert.True(t, opts.ShowValidation)
	assert.False(t, opts.Compact)
}

func TestNewFormatterWithWriter(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	assert.NotNil(t, f)
	assert.True(t, f.opts.ShowNestedFields)
}

func TestWithOptions(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	opts := FormatOptions{ShowNestedFields: false, MaxDepth: 5, ShowValidation: false, Compact: true}
	result := f.WithOptions(opts)
	assert.Equal(t, opts, f.opts)
	assert.Same(t, f, result) // returns self
}

func TestFormatType_Basic(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	info := &TypeInfo{
		Name:        "MyType",
		Package:     "pkg/mypackage",
		Description: "This is a test type",
		Fields: []FieldInfo{
			{Name: "name", Type: "string", Kind: reflect.String, Description: "The name"},
		},
	}
	f.FormatType(info)
	out := buf.String()
	assert.Contains(t, out, "MyType")
	assert.Contains(t, out, "pkg/mypackage")
	assert.Contains(t, out, "This is a test type")
	assert.Contains(t, out, "name")
}

func TestFormatType_NoDescription(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	info := &TypeInfo{Name: "Simple", Fields: []FieldInfo{}}
	f.FormatType(info)
	assert.Contains(t, buf.String(), "Simple")
}

func TestFormatField_Full(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)

	minLen := 1
	maxLen := 100
	info := &FieldInfo{
		Name:        "email",
		Type:        "string",
		Kind:        reflect.String,
		Required:    true,
		Description: "Email address",
		Example:     "user@example.com",
		Default:     "none",
		Format:      "email",
		MinLength:   &minLen,
		MaxLength:   &maxLen,
	}
	f.FormatField(info)
	out := buf.String()
	assert.Contains(t, out, "email")
	assert.Contains(t, out, "-required-")
	assert.Contains(t, out, "Email address")
	assert.Contains(t, out, "user@example.com")
	assert.Contains(t, out, "none")
	assert.Contains(t, out, "email")
}

func TestFormatField_WithNestedFields(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	info := &FieldInfo{
		Name: "config",
		Type: "Config",
		Kind: reflect.Struct,
		NestedFields: []FieldInfo{
			{Name: "host", Type: "string", Kind: reflect.String},
		},
	}
	f.FormatField(info)
	assert.Contains(t, buf.String(), "host")
}

func TestFormatField_NoValidation(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	f.opts.ShowValidation = false
	minLen := 1
	info := &FieldInfo{
		Name:      "field",
		Type:      "string",
		Kind:      reflect.String,
		MinLength: &minLen,
	}
	f.FormatField(info)
	// With ShowValidation false, no validation section output
	assert.NotContains(t, buf.String(), "minLength")
}

func TestFormatFields_MaxDepth(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	f.opts.MaxDepth = 0 // should output nothing
	fields := []FieldInfo{{Name: "hidden", Type: "string", Kind: reflect.String}}
	f.formatFields(fields, 0)
	assert.Empty(t, buf.String())
}

func TestFormatFields_Deprecated(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	fields := []FieldInfo{
		{Name: "oldField", Type: "string", Kind: reflect.String, Deprecated: true},
	}
	f.formatFields(fields, 0)
	assert.Contains(t, buf.String(), "DEPRECATED")
}

func TestFormatFields_WithExample(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	fields := []FieldInfo{
		{Name: "url", Type: "string", Kind: reflect.String, Example: "https://example.com"},
	}
	f.formatFields(fields, 0)
	assert.Contains(t, buf.String(), "https://example.com")
}

func TestFormatTypeString_AllKinds(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)

	tests := []struct {
		field    FieldInfo
		contains string
	}{
		{FieldInfo{Kind: reflect.Slice, ElemType: "string"}, "[]string"},
		{FieldInfo{Kind: reflect.Slice, Type: "any"}, "[]any"},
		{FieldInfo{Kind: reflect.Array, ElemType: "int"}, "[]int"},
		{FieldInfo{Kind: reflect.Map, KeyType: "string", ElemType: "any"}, "map[string]any"},
		{FieldInfo{Kind: reflect.Map, Type: "map"}, "map"},
		{FieldInfo{Kind: reflect.Ptr, Type: "Config"}, "*Config"},
		{FieldInfo{Kind: reflect.Interface, Type: ""}, "any"},
		{FieldInfo{Kind: reflect.Interface, Type: "MyInterface"}, "MyInterface"},
		{FieldInfo{Kind: reflect.String, Type: "string"}, "string"},
		{FieldInfo{Kind: reflect.String, Type: ""}, "string"}, // falls back to Kind.String()
	}

	for _, tt := range tests {
		field := tt.field
		result := f.formatTypeString(&field)
		assert.Contains(t, result, tt.contains, "field: %+v", tt.field)
	}
}

func TestHasValidation(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)

	minLen := 1
	maxLen := 10
	minVal := 0.0
	maxVal := 100.0
	minItems := 1
	maxItems := 5

	assert.False(t, f.hasValidation(&FieldInfo{}))
	assert.True(t, f.hasValidation(&FieldInfo{MinLength: &minLen}))
	assert.True(t, f.hasValidation(&FieldInfo{MaxLength: &maxLen}))
	assert.True(t, f.hasValidation(&FieldInfo{Minimum: &minVal}))
	assert.True(t, f.hasValidation(&FieldInfo{Maximum: &maxVal}))
	assert.True(t, f.hasValidation(&FieldInfo{MinItems: &minItems}))
	assert.True(t, f.hasValidation(&FieldInfo{MaxItems: &maxItems}))
	assert.True(t, f.hasValidation(&FieldInfo{Pattern: "^[a-z]+$"}))
	assert.True(t, f.hasValidation(&FieldInfo{Format: "email"}))
	assert.True(t, f.hasValidation(&FieldInfo{Enum: []string{"a", "b"}}))
}

func TestFormatValidation_AllFields(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)

	minLen := 5
	maxLen := 100
	minVal := 1.5
	maxVal := 99.9
	minItems := 2
	maxItems := 10

	info := &FieldInfo{
		MinLength:          &minLen,
		MaxLength:          &maxLen,
		Minimum:            &minVal,
		Maximum:            &maxVal,
		MinItems:           &minItems,
		MaxItems:           &maxItems,
		Pattern:            "^[a-z]+$",
		PatternDescription: "lowercase only",
		Format:             "uri",
		Enum:               []string{"a", "b", "c"},
	}
	f.formatValidation(info, "  ")
	out := buf.String()
	assert.Contains(t, out, "minLength: 5")
	assert.Contains(t, out, "maxLength: 100")
	assert.Contains(t, out, "minimum: 1.5")
	assert.Contains(t, out, "maximum: 99.9")
	assert.Contains(t, out, "minItems: 2")
	assert.Contains(t, out, "maxItems: 10")
	assert.Contains(t, out, "pattern:")
	assert.Contains(t, out, "lowercase only")
	assert.Contains(t, out, "format: uri")
	assert.Contains(t, out, "enum:")
}

func TestFormatValidationCompact(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)

	minLen := 1
	maxLen := 50
	minVal := 0.0
	maxVal := 10.0
	minItems := 1
	maxItems := 3

	info := &FieldInfo{
		MinLength:          &minLen,
		MaxLength:          &maxLen,
		Minimum:            &minVal,
		Maximum:            &maxVal,
		MinItems:           &minItems,
		MaxItems:           &maxItems,
		Pattern:            "^[a-z]+$",
		PatternDescription: "lowercase",
		Format:             "email",
		Enum:               []string{"x", "y"},
	}
	f.formatValidationCompact(info, "  ")
	out := buf.String()
	assert.Contains(t, out, "Validation:")
	assert.Contains(t, out, "length:")
	assert.Contains(t, out, "range:")
	assert.Contains(t, out, "items:")
	assert.Contains(t, out, "lowercase")
	assert.Contains(t, out, "email")
	assert.Contains(t, out, "enum:")
}

func TestFormatValidationCompact_NoValidation(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	f.formatValidationCompact(&FieldInfo{}, "  ")
	assert.Empty(t, strings.TrimSpace(buf.String()))
}

func TestAppendLengthValidation(t *testing.T) {
	minVal := 1
	maxVal := 10

	parts := appendLengthValidation(nil, &minVal, &maxVal)
	assert.Equal(t, []string{"length: 1-10"}, parts)

	parts = appendLengthValidation(nil, &minVal, nil)
	assert.Equal(t, []string{"minLength: 1"}, parts)

	parts = appendLengthValidation(nil, nil, &maxVal)
	assert.Equal(t, []string{"maxLength: 10"}, parts)

	parts = appendLengthValidation(nil, nil, nil)
	assert.Empty(t, parts)
}

func TestAppendRangeValidation(t *testing.T) {
	minVal := 0.5
	maxVal := 9.9

	parts := appendRangeValidation(nil, &minVal, &maxVal)
	assert.Equal(t, []string{"range: 0.5-9.9"}, parts)

	parts = appendRangeValidation(nil, &minVal, nil)
	assert.Equal(t, []string{"min: 0.5"}, parts)

	parts = appendRangeValidation(nil, nil, &maxVal)
	assert.Equal(t, []string{"max: 9.9"}, parts)

	parts = appendRangeValidation(nil, nil, nil)
	assert.Empty(t, parts)
}

func TestAppendItemsValidation(t *testing.T) {
	minVal := 1
	maxVal := 5

	parts := appendItemsValidation(nil, &minVal, &maxVal)
	assert.Equal(t, []string{"items: 1-5"}, parts)

	parts = appendItemsValidation(nil, &minVal, nil)
	assert.Equal(t, []string{"minItems: 1"}, parts)

	parts = appendItemsValidation(nil, nil, &maxVal)
	assert.Equal(t, []string{"maxItems: 5"}, parts)

	parts = appendItemsValidation(nil, nil, nil)
	assert.Empty(t, parts)
}

func TestFormatField_WithPatternNoDescription(t *testing.T) {
	var buf bytes.Buffer
	f := newTestFormatter(&buf)
	info := &FieldInfo{
		Name:    "code",
		Type:    "string",
		Kind:    reflect.String,
		Pattern: "^[A-Z]+$",
	}
	f.FormatField(info)
	assert.Contains(t, buf.String(), "^[A-Z]+$")
}
