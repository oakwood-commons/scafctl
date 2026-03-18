// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package detail provides shared business logic for building structured
// provider information. This package is used by CLI, MCP, and future API consumers.
package detail

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider"
)

// BuildProviderDetail builds a detailed map for a single provider descriptor.
// The returned map includes schema, output schemas, examples, CLI usage, and metadata.
func BuildProviderDetail(desc provider.Descriptor) map[string]any {
	output := map[string]any{
		"name":         desc.Name,
		"displayName":  desc.DisplayName,
		"apiVersion":   desc.APIVersion,
		"version":      desc.Version.String(),
		"description":  desc.Description,
		"capabilities": CapabilitiesToStrings(desc.Capabilities),
	}

	if desc.Category != "" {
		output["category"] = desc.Category
	}
	if len(desc.Tags) > 0 {
		output["tags"] = desc.Tags
	}
	if desc.Icon != "" {
		output["icon"] = desc.Icon
	}
	if desc.IsDeprecated {
		output["deprecated"] = true
	}
	if desc.Beta {
		output["beta"] = true
	}

	// Add schema information
	if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
		output["schema"] = BuildSchemaOutput(desc.Schema)
	}

	// Add output schemas
	if len(desc.OutputSchemas) > 0 {
		outputSchemas := make(map[string]any)
		for cap, schema := range desc.OutputSchemas {
			outputSchemas[string(cap)] = BuildSchemaOutput(schema)
		}
		output["outputSchemas"] = outputSchemas
	}

	// Add links
	if len(desc.Links) > 0 {
		links := make([]map[string]string, 0, len(desc.Links))
		for _, link := range desc.Links {
			links = append(links, map[string]string{
				"name": link.Name,
				"url":  link.URL,
			})
		}
		output["links"] = links
	}

	// Add examples
	if len(desc.Examples) > 0 {
		examples := make([]map[string]any, 0, len(desc.Examples))
		for _, ex := range desc.Examples {
			examples = append(examples, map[string]any{
				"name":        ex.Name,
				"description": ex.Description,
				"yaml":        ex.YAML,
			})
		}
		output["examples"] = examples
	}

	// Add maintainers
	if len(desc.Maintainers) > 0 {
		maintainers := make([]map[string]string, 0, len(desc.Maintainers))
		for _, m := range desc.Maintainers {
			maintainers = append(maintainers, map[string]string{
				"name":  m.Name,
				"email": m.Email,
			})
		}
		output["maintainers"] = maintainers
	}

	// Add CLI usage examples
	cliExamples := GenerateCLIExamples(&desc)
	if len(cliExamples) > 0 {
		output["cliUsage"] = cliExamples
	}

	return output
}

// GenerateCLIExamples auto-generates CLI usage examples from the provider's schema.
// It builds "scafctl run provider <name>" commands using required fields and
// type-appropriate placeholder values.
func GenerateCLIExamples(desc *provider.Descriptor) []string {
	if desc.Schema == nil || len(desc.Schema.Properties) == 0 {
		return nil
	}

	// Build required set
	requiredSet := make(map[string]bool, len(desc.Schema.Required))
	for _, name := range desc.Schema.Required {
		requiredSet[name] = true
	}

	// Collect required fields with placeholder values, sorted for deterministic output
	requiredNames := make([]string, 0)
	for name := range desc.Schema.Properties {
		if requiredSet[name] {
			requiredNames = append(requiredNames, name)
		}
	}
	slices.Sort(requiredNames)

	// Build input flags for required fields
	inputFlags := make([]string, 0, len(requiredNames))
	for _, name := range requiredNames {
		prop := desc.Schema.Properties[name]
		placeholder := SchemaPlaceholder(name, prop)
		inputFlags = append(inputFlags, fmt.Sprintf("%s=%s", name, placeholder))
	}

	var examples []string

	// Example with required fields only
	if len(inputFlags) > 0 {
		cmd := fmt.Sprintf("scafctl run provider %s %s", desc.Name, strings.Join(inputFlags, " "))
		examples = append(examples, cmd)
	} else {
		// No required fields - show a minimal example
		examples = append(examples, fmt.Sprintf("scafctl run provider %s", desc.Name))
	}

	// If multiple capabilities, show an example with --capability for non-first capabilities
	if len(desc.Capabilities) > 1 {
		for _, cap := range desc.Capabilities[1:] {
			baseCmdParts := []string{fmt.Sprintf("scafctl run provider %s", desc.Name)}
			if len(inputFlags) > 0 {
				baseCmdParts = append(baseCmdParts, inputFlags...)
			}
			baseCmdParts = append(baseCmdParts, fmt.Sprintf("--capability %s", cap))
			examples = append(examples, strings.Join(baseCmdParts, " "))
		}
	}

	// Add file-based input example
	examples = append(examples, fmt.Sprintf("scafctl run provider %s @inputs.yaml", desc.Name))

	return examples
}

// SchemaPlaceholder returns a reasonable placeholder value for a schema property.
func SchemaPlaceholder(name string, prop *jsonschema.Schema) string {
	if prop == nil {
		return "<value>"
	}

	// Use the first example if available
	if len(prop.Examples) > 0 {
		return fmt.Sprintf("%v", prop.Examples[0])
	}

	// Use the first enum value if available
	if len(prop.Enum) > 0 {
		return fmt.Sprintf("%v", prop.Enum[0])
	}

	// Type-based placeholder
	switch prop.Type {
	case "string":
		return fmt.Sprintf("<%s>", name)
	case "integer", "number":
		return "0"
	case "boolean":
		return "true"
	case "array":
		return fmt.Sprintf("<%s1>,<%s2>", name, name)
	case "object":
		return fmt.Sprintf("@%s.yaml", name)
	default:
		return fmt.Sprintf("<%s>", name)
	}
}

// BuildSchemaOutput converts a JSON Schema to a map for structured output.
func BuildSchemaOutput(schema *jsonschema.Schema) map[string]any {
	if schema == nil || len(schema.Properties) == 0 {
		return nil
	}

	// Build required set
	requiredSet := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		requiredSet[name] = true
	}

	properties := make(map[string]any)
	for name, prop := range schema.Properties {
		propMap := map[string]any{
			"type": prop.Type,
		}
		if prop.Description != "" {
			propMap["description"] = prop.Description
		}
		if requiredSet[name] {
			propMap["required"] = true
		}
		if prop.Default != nil {
			var def any
			_ = json.Unmarshal(prop.Default, &def)
			propMap["default"] = def
		}
		if len(prop.Examples) > 0 {
			propMap["example"] = prop.Examples[0]
		}
		if len(prop.Enum) > 0 {
			propMap["enum"] = prop.Enum
		}
		properties[name] = propMap
	}

	return map[string]any{"properties": properties}
}

// CapabilitiesToStrings converts []Capability to []string.
func CapabilitiesToStrings(caps []provider.Capability) []string {
	result := make([]string, 0, len(caps))
	for _, cap := range caps {
		result = append(result, string(cap))
	}
	return result
}
