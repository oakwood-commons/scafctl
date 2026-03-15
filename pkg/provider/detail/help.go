// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// FormatProviderInputHelp generates a human-readable help section describing
// a provider's input parameters from its JSON Schema. The output is formatted
// for inclusion in CLI help text.
//
// Example output:
//
//	Provider Inputs (http):
//	  url        string   (required)  The URL to request
//	  method     string               HTTP method (default: GET)
//	  headers    object               Request headers
func FormatProviderInputHelp(desc *provider.Descriptor) string {
	if desc == nil || desc.Schema == nil || len(desc.Schema.Properties) == 0 {
		return ""
	}

	var sb strings.Builder

	fmt.Fprintf(&sb, "Provider Inputs (%s):\n", desc.Name)

	// Build required set
	requiredSet := make(map[string]bool, len(desc.Schema.Required))
	for _, name := range desc.Schema.Required {
		requiredSet[name] = true
	}

	// Collect and sort property names for deterministic output
	propNames := make([]string, 0, len(desc.Schema.Properties))
	for name := range desc.Schema.Properties {
		propNames = append(propNames, name)
	}
	slices.Sort(propNames)

	// Calculate column widths for alignment
	maxNameLen := 0
	maxTypeLen := 0
	for _, name := range propNames {
		if len(name) > maxNameLen {
			maxNameLen = len(name)
		}
		typeStr := formatSchemaType(desc.Schema.Properties[name])
		if len(typeStr) > maxTypeLen {
			maxTypeLen = len(typeStr)
		}
	}

	// Render each property
	for _, name := range propNames {
		prop := desc.Schema.Properties[name]
		typeStr := formatSchemaType(prop)

		// Build the required/default annotation
		annotation := ""
		if requiredSet[name] {
			annotation = "(required)"
		} else if prop.Default != nil {
			var def any
			if err := json.Unmarshal(prop.Default, &def); err == nil {
				annotation = fmt.Sprintf("(default: %v)", def)
			}
		}

		// Build the description suffix
		descStr := ""
		if prop.Description != "" {
			descStr = prop.Description
		}

		// Format: "  name    type   annotation  description"
		line := fmt.Sprintf("  %-*s  %-*s  %-12s %s",
			maxNameLen, name,
			maxTypeLen, typeStr,
			annotation, descStr)

		sb.WriteString(strings.TrimRight(line, " ") + "\n")
	}

	return sb.String()
}

// formatSchemaType returns a compact type representation for a schema property.
func formatSchemaType(prop *jsonschema.Schema) string {
	if prop == nil {
		return "any"
	}

	typeStr := prop.Type
	if typeStr == "" {
		typeStr = "any"
	}

	if len(prop.Enum) > 0 {
		enumStrs := make([]string, len(prop.Enum))
		for i, e := range prop.Enum {
			enumStrs[i] = fmt.Sprint(e)
		}
		typeStr = fmt.Sprintf("%s[%s]", typeStr, strings.Join(enumStrs, "|"))
	}

	return typeStr
}
