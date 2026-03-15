// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
	"github.com/stretchr/testify/assert"
)

func TestFormatProviderInputHelp(t *testing.T) {
	version := semver.MustParse("1.0.0")

	t.Run("returns empty for nil descriptor", func(t *testing.T) {
		result := FormatProviderInputHelp(nil)
		assert.Empty(t, result)
	})

	t.Run("returns empty for nil schema", func(t *testing.T) {
		desc := &provider.Descriptor{
			Name:    "test",
			Version: version,
			Schema:  nil,
		}
		result := FormatProviderInputHelp(desc)
		assert.Empty(t, result)
	})

	t.Run("returns empty for schema with no properties", func(t *testing.T) {
		desc := &provider.Descriptor{
			Name:    "test",
			Version: version,
			Schema:  &jsonschema.Schema{},
		}
		result := FormatProviderInputHelp(desc)
		assert.Empty(t, result)
	})

	t.Run("formats single required property", func(t *testing.T) {
		desc := &provider.Descriptor{
			Name:    "static",
			Version: version,
			Schema: schemahelper.ObjectSchema([]string{"value"}, map[string]*jsonschema.Schema{
				"value": schemahelper.AnyProp("The static value to return"),
			}),
		}
		result := FormatProviderInputHelp(desc)
		assert.Contains(t, result, "Provider Inputs (static):")
		assert.Contains(t, result, "value")
		assert.Contains(t, result, "(required)")
		assert.Contains(t, result, "The static value to return")
	})

	t.Run("formats multiple properties with types", func(t *testing.T) {
		desc := &provider.Descriptor{
			Name:    "http",
			Version: version,
			Schema: schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
				"url":    schemahelper.StringProp("The URL to request"),
				"method": schemahelper.StringProp("HTTP method"),
				"timeout": schemahelper.IntProp("Request timeout in seconds",
					schemahelper.WithDefault(30)),
			}),
		}
		result := FormatProviderInputHelp(desc)
		assert.Contains(t, result, "Provider Inputs (http):")
		assert.Contains(t, result, "url")
		assert.Contains(t, result, "string")
		assert.Contains(t, result, "(required)")
		assert.Contains(t, result, "method")
		assert.Contains(t, result, "timeout")
		assert.Contains(t, result, "integer")
		assert.Contains(t, result, "(default: 30)")
	})

	t.Run("formats enum properties", func(t *testing.T) {
		desc := &provider.Descriptor{
			Name:    "test",
			Version: version,
			Schema: schemahelper.ObjectSchema([]string{"op"}, map[string]*jsonschema.Schema{
				"op": schemahelper.StringProp("Operation to perform",
					schemahelper.WithEnum("get", "set", "list")),
			}),
		}
		result := FormatProviderInputHelp(desc)
		assert.Contains(t, result, "string[get|set|list]")
		assert.Contains(t, result, "(required)")
	})

	t.Run("sorts properties alphabetically", func(t *testing.T) {
		desc := &provider.Descriptor{
			Name:    "test",
			Version: version,
			Schema: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"zebra": schemahelper.StringProp("Last"),
				"alpha": schemahelper.StringProp("First"),
				"mid":   schemahelper.StringProp("Middle"),
			}),
		}
		result := FormatProviderInputHelp(desc)
		alphaIdx := strings.Index(result, "alpha")
		midIdx := strings.Index(result, "mid")
		zebraIdx := strings.Index(result, "zebra")
		assert.Less(t, alphaIdx, midIdx)
		assert.Less(t, midIdx, zebraIdx)
	})
}

func TestFormatSchemaType(t *testing.T) {
	t.Run("returns any for nil prop", func(t *testing.T) {
		assert.Equal(t, "any", formatSchemaType(nil))
	})

	t.Run("returns type for simple property", func(t *testing.T) {
		prop := schemahelper.StringProp("test")
		assert.Equal(t, "string", formatSchemaType(prop))
	})

	t.Run("returns type with enum", func(t *testing.T) {
		prop := schemahelper.StringProp("test", schemahelper.WithEnum("a", "b"))
		assert.Equal(t, "string[a|b]", formatSchemaType(prop))
	})

	t.Run("returns any for empty type", func(t *testing.T) {
		prop := &jsonschema.Schema{}
		assert.Equal(t, "any", formatSchemaType(prop))
	})
}

func BenchmarkFormatProviderInputHelp(b *testing.B) {
	version := semver.MustParse("1.0.0")
	desc := &provider.Descriptor{
		Name:    "http",
		Version: version,
		Schema: schemahelper.ObjectSchema([]string{"url"}, map[string]*jsonschema.Schema{
			"url":     schemahelper.StringProp("The URL to request"),
			"method":  schemahelper.StringProp("HTTP method"),
			"timeout": schemahelper.IntProp("Request timeout in seconds", schemahelper.WithDefault(30)),
			"headers": schemahelper.AnyProp("HTTP headers as key-value pairs"),
			"body":    schemahelper.StringProp("Request body"),
		}),
	}

	b.ResetTimer()
	for b.Loop() {
		FormatProviderInputHelp(desc)
	}
}
