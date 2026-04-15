// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package schemahelper provides ergonomic builder functions for constructing
// jsonschema.Schema objects used in provider descriptors. These helpers reduce
// the verbosity of creating JSON Schema definitions in Go code and ensure
// consistent patterns across all provider implementations.
package schemahelper

import (
	"github.com/google/jsonschema-go/jsonschema"
	sdkhelper "github.com/oakwood-commons/scafctl-plugin-sdk/provider/schemahelper"
)

// PropOption is a functional option for configuring a Schema property.
type PropOption = sdkhelper.PropOption

// WithDescription sets the property description.
func WithDescription(desc string) PropOption { return sdkhelper.WithDescription(desc) }

// WithExample sets the property examples.
func WithExample(examples ...any) PropOption { return sdkhelper.WithExample(examples...) }

// WithDefault sets the property default value.
func WithDefault(val any) PropOption { return sdkhelper.WithDefault(val) }

// WithEnum sets the allowed values.
func WithEnum(vals ...any) PropOption { return sdkhelper.WithEnum(vals...) }

// WithPattern sets a regex pattern for string validation.
func WithPattern(pattern string) PropOption { return sdkhelper.WithPattern(pattern) }

// WithMinLength sets the minimum string length.
func WithMinLength(n int) PropOption { return sdkhelper.WithMinLength(n) }

// WithMaxLength sets the maximum string length.
func WithMaxLength(n int) PropOption { return sdkhelper.WithMaxLength(n) }

// WithMinimum sets the minimum numeric value.
func WithMinimum(n float64) PropOption { return sdkhelper.WithMinimum(n) }

// WithMaximum sets the maximum numeric value.
func WithMaximum(n float64) PropOption { return sdkhelper.WithMaximum(n) }

// WithMinItems sets the minimum array items.
func WithMinItems(n int) PropOption { return sdkhelper.WithMinItems(n) }

// WithMaxItems sets the maximum array items.
func WithMaxItems(n int) PropOption { return sdkhelper.WithMaxItems(n) }

// WithFormat sets the format hint (uri, email, date, uuid, etc.).
func WithFormat(format string) PropOption { return sdkhelper.WithFormat(format) }

// WithDeprecated marks the property as deprecated.
func WithDeprecated() PropOption { return sdkhelper.WithDeprecated() }

// WithWriteOnly marks the property as write-only (suitable for secrets).
func WithWriteOnly() PropOption { return sdkhelper.WithWriteOnly() }

// WithTitle sets the property title.
func WithTitle(title string) PropOption { return sdkhelper.WithTitle(title) }

// WithItems sets the items schema for an array property.
func WithItems(itemSchema *jsonschema.Schema) PropOption { return sdkhelper.WithItems(itemSchema) }

// WithAdditionalProperties sets the additionalProperties schema for an object/map.
func WithAdditionalProperties(schema *jsonschema.Schema) PropOption {
	return sdkhelper.WithAdditionalProperties(schema)
}

// StringProp creates a string property schema.
func StringProp(desc string, opts ...PropOption) *jsonschema.Schema {
	return sdkhelper.StringProp(desc, opts...)
}

// IntProp creates an integer property schema.
func IntProp(desc string, opts ...PropOption) *jsonschema.Schema {
	return sdkhelper.IntProp(desc, opts...)
}

// NumberProp creates a number property schema (float).
func NumberProp(desc string, opts ...PropOption) *jsonschema.Schema {
	return sdkhelper.NumberProp(desc, opts...)
}

// BoolProp creates a boolean property schema.
func BoolProp(desc string, opts ...PropOption) *jsonschema.Schema {
	return sdkhelper.BoolProp(desc, opts...)
}

// ArrayProp creates an array property schema.
func ArrayProp(desc string, opts ...PropOption) *jsonschema.Schema {
	return sdkhelper.ArrayProp(desc, opts...)
}

// AnyProp creates a property schema with no type constraint (accepts any type).
func AnyProp(desc string, opts ...PropOption) *jsonschema.Schema {
	return sdkhelper.AnyProp(desc, opts...)
}

// ObjectProp creates an object property schema with nested properties.
func ObjectProp(desc string, required []string, props map[string]*jsonschema.Schema, opts ...PropOption) *jsonschema.Schema {
	return sdkhelper.ObjectProp(desc, required, props, opts...)
}

// ObjectSchema creates a top-level object schema with properties and required fields.
func ObjectSchema(required []string, props map[string]*jsonschema.Schema) *jsonschema.Schema {
	return sdkhelper.ObjectSchema(required, props)
}
