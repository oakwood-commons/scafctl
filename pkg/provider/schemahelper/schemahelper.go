// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package schemahelper provides ergonomic builder functions for constructing
// jsonschema.Schema objects used in provider descriptors. These helpers reduce
// the verbosity of creating JSON Schema definitions in Go code and ensure
// consistent patterns across all provider implementations.
package schemahelper

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
)

// PropOption is a functional option for configuring a Schema property.
type PropOption func(*jsonschema.Schema)

// WithDescription sets the property description.
func WithDescription(desc string) PropOption {
	return func(s *jsonschema.Schema) {
		s.Description = desc
	}
}

// WithExample sets the property examples.
func WithExample(examples ...any) PropOption {
	return func(s *jsonschema.Schema) {
		s.Examples = examples
	}
}

// WithDefault sets the property default value.
func WithDefault(val any) PropOption {
	return func(s *jsonschema.Schema) {
		data, err := json.Marshal(val)
		if err == nil {
			s.Default = data
		}
	}
}

// WithEnum sets the allowed values.
func WithEnum(vals ...any) PropOption {
	return func(s *jsonschema.Schema) {
		s.Enum = vals
	}
}

// WithPattern sets a regex pattern for string validation.
func WithPattern(pattern string) PropOption {
	return func(s *jsonschema.Schema) {
		s.Pattern = pattern
	}
}

// WithMinLength sets the minimum string length.
func WithMinLength(n int) PropOption {
	return func(s *jsonschema.Schema) {
		s.MinLength = &n
	}
}

// WithMaxLength sets the maximum string length.
func WithMaxLength(n int) PropOption {
	return func(s *jsonschema.Schema) {
		s.MaxLength = &n
	}
}

// WithMinimum sets the minimum numeric value.
func WithMinimum(n float64) PropOption {
	return func(s *jsonschema.Schema) {
		s.Minimum = &n
	}
}

// WithMaximum sets the maximum numeric value.
func WithMaximum(n float64) PropOption {
	return func(s *jsonschema.Schema) {
		s.Maximum = &n
	}
}

// WithMinItems sets the minimum array items.
func WithMinItems(n int) PropOption {
	return func(s *jsonschema.Schema) {
		s.MinItems = &n
	}
}

// WithMaxItems sets the maximum array items.
func WithMaxItems(n int) PropOption {
	return func(s *jsonschema.Schema) {
		s.MaxItems = &n
	}
}

// WithFormat sets the format hint (uri, email, date, uuid, etc.).
func WithFormat(format string) PropOption {
	return func(s *jsonschema.Schema) {
		s.Format = format
	}
}

// WithDeprecated marks the property as deprecated.
func WithDeprecated() PropOption {
	return func(s *jsonschema.Schema) {
		s.Deprecated = true
	}
}

// WithWriteOnly marks the property as write-only (suitable for secrets).
func WithWriteOnly() PropOption {
	return func(s *jsonschema.Schema) {
		s.WriteOnly = true
	}
}

// WithTitle sets the property title.
func WithTitle(title string) PropOption {
	return func(s *jsonschema.Schema) {
		s.Title = title
	}
}

// WithItems sets the items schema for an array property.
func WithItems(itemSchema *jsonschema.Schema) PropOption {
	return func(s *jsonschema.Schema) {
		s.Items = itemSchema
	}
}

// WithAdditionalProperties sets the additionalProperties schema for an object/map.
func WithAdditionalProperties(schema *jsonschema.Schema) PropOption {
	return func(s *jsonschema.Schema) {
		s.AdditionalProperties = schema
	}
}

// applyOpts applies all PropOptions to a schema.
func applyOpts(s *jsonschema.Schema, opts []PropOption) *jsonschema.Schema {
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// StringProp creates a string property schema.
func StringProp(desc string, opts ...PropOption) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:        "string",
		Description: desc,
	}
	return applyOpts(s, opts)
}

// IntProp creates an integer property schema.
func IntProp(desc string, opts ...PropOption) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:        "integer",
		Description: desc,
	}
	return applyOpts(s, opts)
}

// NumberProp creates a number property schema (float).
func NumberProp(desc string, opts ...PropOption) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:        "number",
		Description: desc,
	}
	return applyOpts(s, opts)
}

// BoolProp creates a boolean property schema.
func BoolProp(desc string, opts ...PropOption) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:        "boolean",
		Description: desc,
	}
	return applyOpts(s, opts)
}

// ArrayProp creates an array property schema.
func ArrayProp(desc string, opts ...PropOption) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:        "array",
		Description: desc,
	}
	return applyOpts(s, opts)
}

// AnyProp creates a property schema with no type constraint (accepts any type).
func AnyProp(desc string, opts ...PropOption) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Description: desc,
	}
	return applyOpts(s, opts)
}

// ObjectProp creates an object property schema with nested properties.
func ObjectProp(desc string, required []string, props map[string]*jsonschema.Schema, opts ...PropOption) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:        "object",
		Description: desc,
		Properties:  props,
	}
	if len(required) > 0 {
		s.Required = required
	}
	return applyOpts(s, opts)
}

// ObjectSchema creates a top-level object schema with properties and required fields.
// This is the primary entry point for constructing provider input/output schemas.
func ObjectSchema(required []string, props map[string]*jsonschema.Schema) *jsonschema.Schema {
	s := &jsonschema.Schema{
		Type:       "object",
		Properties: props,
	}
	if len(required) > 0 {
		s.Required = required
	}
	return s
}
