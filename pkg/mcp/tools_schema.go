// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/schema"
)

// registerSchemaTools registers schema-related MCP tools.
func (s *Server) registerSchemaTools() {
	// get_solution_schema — full JSON Schema for the Solution YAML format
	getSolutionSchemaTool := mcp.NewTool("get_solution_schema",
		mcp.WithDescription("Get the full JSON Schema for a scafctl solution YAML file. This describes ALL valid fields, types, validation rules, and documentation for authoring a solution. Use this before creating or editing solution files."),
		mcp.WithTitleAnnotation("Get Solution Schema"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("field",
			mcp.Description("Optional: get schema for a specific field path (dot-separated, e.g., 'metadata', 'spec.resolvers', 'spec.workflow.actions'). Omit to get the full schema."),
		),
	)
	s.mcpServer.AddTool(getSolutionSchemaTool, s.handleGetSolutionSchema)

	// explain_kind — introspect any registered kind (solution, resolver, action, etc.)
	explainKindTool := mcp.NewTool("explain_kind",
		mcp.WithDescription("Get detailed field documentation for a scafctl type (kind). Works like 'kubectl explain' — shows field names, types, descriptions, validation rules, and examples. Available kinds: solution, resolver, action, workflow, spec, provider, schema, retry."),
		mcp.WithTitleAnnotation("Explain Kind"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("kind",
			mcp.Required(),
			mcp.Description("The kind to explain: solution, resolver, action, workflow, spec, provider, schema, retry"),
		),
		mcp.WithString("field",
			mcp.Description("Optional field path to drill into (dot-separated, e.g., 'metadata', 'resolve.with')"),
		),
	)
	s.mcpServer.AddTool(explainKindTool, s.handleExplainKind)
}

// handleGetSolutionSchema returns the full JSON Schema for a solution YAML file.
func (s *Server) handleGetSolutionSchema(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	field := request.GetString("field", "")

	if field != "" {
		return s.handleGetSolutionSchemaField(field)
	}

	schemaBytes, err := schema.GenerateSolutionSchema()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to generate solution schema: %v", err)), nil
	}

	return mcp.NewToolResultText(string(schemaBytes)), nil
}

// handleGetSolutionSchemaField returns schema for a specific field path.
func (s *Server) handleGetSolutionSchemaField(field string) (*mcp.CallToolResult, error) {
	schemaBytes, err := schema.GenerateSolutionSchema()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to generate solution schema: %v", err)), nil
	}

	var doc map[string]any
	if err := json.Unmarshal(schemaBytes, &doc); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to parse schema: %v", err)), nil
	}

	// Navigate the schema following the field path
	parts := strings.Split(field, ".")
	current := doc
	for _, part := range parts {
		props, ok := current["properties"].(map[string]any)
		if !ok {
			// Check if there's a $ref that needs resolving
			if ref, ok := current["$ref"].(string); ok {
				resolved := resolveRef(doc, ref)
				if resolved == nil {
					return mcp.NewToolResultError(fmt.Sprintf("could not resolve $ref %q for field %q", ref, field)), nil
				}
				current = resolved
				props, ok = current["properties"].(map[string]any)
				if !ok {
					return mcp.NewToolResultError(fmt.Sprintf("field %q is not an object type", field)), nil
				}
			} else {
				return mcp.NewToolResultError(fmt.Sprintf("field %q is not an object type (no properties)", field)), nil
			}
		}

		fieldSchema, ok := props[part].(map[string]any)
		if !ok {
			available := make([]string, 0, len(props))
			for k := range props {
				available = append(available, k)
			}
			sort.Strings(available)
			return mcp.NewToolResultError(fmt.Sprintf("field %q not found. Available fields: %s", part, strings.Join(available, ", "))), nil
		}
		current = fieldSchema
	}

	// If the result is a $ref, resolve it
	if ref, ok := current["$ref"].(string); ok {
		resolved := resolveRef(doc, ref)
		if resolved != nil {
			current = resolved
		}
	}

	result, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to marshal field schema: %v", err)), nil
	}

	return mcp.NewToolResultText(string(result)), nil
}

// resolveRef resolves a JSON Schema $ref within the document.
func resolveRef(doc map[string]any, ref string) map[string]any {
	// Handle #/$defs/Name refs
	prefix := "#/$defs/"
	if !strings.HasPrefix(ref, prefix) {
		return nil
	}
	name := ref[len(prefix):]
	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		return nil
	}
	resolved, ok := defs[name].(map[string]any)
	if !ok {
		return nil
	}
	return resolved
}

// handleExplainKind returns detailed field documentation for a scafctl type.
func (s *Server) handleExplainKind(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kindName, err := request.RequireString("kind")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	field := request.GetString("field", "")

	kindDef, ok := schema.GetKind(kindName)
	if !ok {
		names := schema.GetGlobalRegistry().Names()
		sort.Strings(names)
		return mcp.NewToolResultError(fmt.Sprintf("kind %q not found. Available kinds: %s", kindName, strings.Join(names, ", "))), nil
	}

	if field != "" {
		fieldInfo, err := schema.IntrospectField(kindDef.TypeInstance, field)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("field %q not found in %s: %v", field, kindName, err)), nil
		}
		return mcp.NewToolResultJSON(fieldInfo)
	}

	// Return full type info
	info := kindDef.TypeInfo
	if info == nil {
		return mcp.NewToolResultError(fmt.Sprintf("no type info available for kind %q", kindName)), nil
	}

	// Build a structured response
	result := map[string]any{
		"kind":        kindDef.Name,
		"description": info.Description,
		"type":        info.Kind.String(),
	}

	if len(info.Fields) > 0 {
		fields := make([]map[string]any, 0, len(info.Fields))
		for _, f := range info.Fields {
			fields = append(fields, fieldInfoToMap(f))
		}
		result["fields"] = fields
	}

	return mcp.NewToolResultJSON(result)
}

// fieldInfoToMap converts a FieldInfo to a map for JSON output.
func fieldInfoToMap(f schema.FieldInfo) map[string]any {
	m := map[string]any{
		"name": f.Name,
		"type": formatFieldType(f),
	}
	if f.Description != "" {
		m["description"] = f.Description
	}
	if f.Required {
		m["required"] = true
	}
	if f.Example != "" {
		m["example"] = f.Example
	}
	if f.Default != "" {
		m["default"] = f.Default
	}
	if f.Pattern != "" {
		m["pattern"] = f.Pattern
	}
	if f.MinLength != nil {
		m["minLength"] = *f.MinLength
	}
	if f.MaxLength != nil {
		m["maxLength"] = *f.MaxLength
	}
	if f.MinItems != nil {
		m["minItems"] = *f.MinItems
	}
	if f.MaxItems != nil {
		m["maxItems"] = *f.MaxItems
	}
	if f.Minimum != nil {
		m["minimum"] = *f.Minimum
	}
	if f.Maximum != nil {
		m["maximum"] = *f.Maximum
	}
	if len(f.Enum) > 0 {
		m["enum"] = f.Enum
	}
	if f.Deprecated {
		m["deprecated"] = true
	}
	if len(f.NestedFields) > 0 {
		nested := make([]map[string]any, 0, len(f.NestedFields))
		for _, nf := range f.NestedFields {
			nested = append(nested, fieldInfoToMap(nf))
		}
		m["fields"] = nested
	}
	return m
}

// formatFieldType returns a human-readable type string.
func formatFieldType(f schema.FieldInfo) string {
	switch f.Kind { //nolint:exhaustive // only slice/array/map need special handling
	case reflect.Slice, reflect.Array:
		if f.ElemType != "" {
			return "[]" + f.ElemType
		}
		return "[]" + f.Type
	case reflect.Map:
		if f.KeyType != "" && f.ElemType != "" {
			return "map[" + f.KeyType + "]" + f.ElemType
		}
		return f.Type
	default:
		if f.Type != "" {
			return f.Type
		}
		return f.Kind.String()
	}
}
