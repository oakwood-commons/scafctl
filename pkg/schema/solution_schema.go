// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
	"maps"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// SolutionSchemaID is the JSON Schema ID for the solution file format.
const SolutionSchemaID = "https://scafctl.dev/schemas/v1/solution.json"

var (
	solutionSchemaOnce sync.Once
	solutionSchemaJSON []byte
	solutionSchemaErr  error
)

// durationAlias is a simple string type used as a Huma type alias for
// time.Duration and duration.Duration, which Huma cannot natively reflect
// (it panics on example:"30s" because it tries to parse it as an integer).
type durationAlias string

// GenerateSolutionSchema generates a JSON Schema for the Solution struct
// using Huma's schema generation which reads the struct tags (doc, example,
// pattern, maxLength, etc.) to produce a full OpenAPI-compatible schema.
func GenerateSolutionSchema() ([]byte, error) {
	solutionSchemaOnce.Do(func() {
		// Use a custom namer that includes the package name to avoid collisions
		// between types with the same name from different packages (e.g.,
		// spec.Condition vs resolver.Condition).
		namer := func(t reflect.Type, hint string) string {
			t = derefType(t)
			pkg := t.PkgPath()
			name := t.Name()
			if name == "" {
				name = hint
			}
			// Extract just the last package segment
			if pkg != "" {
				parts := strings.Split(pkg, "/")
				pkgName := parts[len(parts)-1]
				// Capitalize package name for CamelCase
				if len(pkgName) > 0 {
					name = strings.ToUpper(pkgName[:1]) + pkgName[1:] + name
				}
			}
			return name
		}

		reg := huma.NewMapRegistry("#/components/schemas/", namer)

		// Register type aliases for types that Huma can't handle natively.
		// time.Duration and duration.Duration are stored as strings in YAML
		// (e.g., "30s", "5m") but Huma's reflector doesn't understand them.
		reg.RegisterTypeAlias(reflect.TypeOf(time.Duration(0)), reflect.TypeOf(durationAlias("")))
		reg.RegisterTypeAlias(reflect.TypeOf(duration.Duration{}), reflect.TypeOf(durationAlias("")))

		schema := reg.Schema(reflect.TypeOf(solution.Solution{}), false, "Solution")

		// Build a full schema document with $schema, title, description,
		// and inline the component schemas as $defs.
		doc := map[string]any{
			"$schema":     "https://json-schema.org/draft/2020-12/schema",
			"$id":         SolutionSchemaID,
			"title":       "scafctl Solution",
			"description": "Schema for a scafctl solution YAML file. Solutions are declarative units of behavior that combine resolvers (data resolution), templates (data to files), and actions (side effects).",
		}

		// Marshal the root schema and merge into doc
		rootBytes, err := json.Marshal(schema)
		if err != nil {
			solutionSchemaErr = err
			return
		}
		var rootMap map[string]any
		if err := json.Unmarshal(rootBytes, &rootMap); err != nil {
			solutionSchemaErr = err
			return
		}
		maps.Copy(doc, rootMap)

		// Add component schemas as $defs
		components := reg.Map()
		if len(components) > 0 {
			defs := make(map[string]any, len(components))
			for name, s := range components {
				defBytes, err := json.Marshal(s)
				if err != nil {
					continue
				}
				var defMap map[string]any
				if err := json.Unmarshal(defBytes, &defMap); err != nil {
					continue
				}
				defs[name] = defMap
			}
			doc["$defs"] = defs
		}

		// Rewrite $ref paths from #/components/schemas/ to #/$defs/
		rewriteRefs(doc)

		// Patch schemas that Huma cannot represent from struct tags alone.
		patchSchema(doc)

		solutionSchemaJSON, solutionSchemaErr = json.MarshalIndent(doc, "", "  ")
	})
	return solutionSchemaJSON, solutionSchemaErr
}

// GenerateSolutionSchemaCompact generates a compact (no indentation) JSON Schema.
func GenerateSolutionSchemaCompact() ([]byte, error) {
	full, err := GenerateSolutionSchema()
	if err != nil {
		return nil, err
	}
	// Re-marshal without indentation
	var doc any
	if err := json.Unmarshal(full, &doc); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// rewriteRefs recursively rewrites $ref values from #/components/schemas/
// to #/$defs/ so the schema is self-contained.
func rewriteRefs(v any) {
	switch val := v.(type) {
	case map[string]any:
		if ref, ok := val["$ref"]; ok {
			if refStr, ok := ref.(string); ok {
				if len(refStr) > len("#/components/schemas/") {
					val["$ref"] = "#/$defs/" + refStr[len("#/components/schemas/"):]
				}
			}
		}
		for _, child := range val {
			rewriteRefs(child)
		}
	case []any:
		for _, child := range val {
			rewriteRefs(child)
		}
	}
}

// derefType dereferences pointer types to get the underlying type.
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
}

// patchSchema applies targeted fixes to the generated schema for types
// that Huma cannot represent from struct tags alone. For example, ValueRef
// supports scalar/array/object literals via custom UnmarshalYAML, but
// Huma only sees the exported JSON-tagged fields (rslvr, expr, tmpl).
func patchSchema(doc map[string]any) {
	defs, ok := doc["$defs"].(map[string]any)
	if !ok {
		return
	}

	patchValueRef(defs)
	patchSkipBuiltinsValue(defs)
	patchMapKeyNames(defs)
	patchJSONSchemaType(defs)
}

// patchMapKeyNames removes "name" from the required list for types where
// the name is set from the map key at load time and is not expected in the
// YAML body. Types affected: Resolver, Action, TestCase.
func patchMapKeyNames(defs map[string]any) {
	for _, suffix := range []string{"Resolver", "Action", "TestCase"} {
		key := findDefKey(defs, suffix)
		if key == "" {
			continue
		}
		def, ok := defs[key].(map[string]any)
		if !ok {
			continue
		}
		required, ok := def["required"].([]any)
		if !ok {
			continue
		}
		filtered := make([]any, 0, len(required))
		for _, r := range required {
			if s, ok := r.(string); ok && s == "name" {
				continue
			}
			filtered = append(filtered, r)
		}
		if len(filtered) == 0 {
			delete(def, "required")
		} else {
			def["required"] = filtered
		}
	}
}

// patchJSONSchemaType replaces the google/jsonschema.Schema $def with an
// open schema. Huma cannot properly reflect jsonschema.Schema because many
// of its fields use json:"-" with custom MarshalJSON/UnmarshalJSON (e.g.,
// Type, Items). Replacing it with an open object lets users write standard
// JSON Schema in resultSchema fields without false positives.
func patchJSONSchemaType(defs map[string]any) {
	key := findDefKey(defs, "JsonschemaSchema")
	if key == "" {
		return
	}
	defs[key] = map[string]any{
		"description": "A JSON Schema object. Accepts any valid JSON Schema properties.",
		"type":        "object",
	}
}

// patchValueRef replaces the ValueRef $def with a schema that correctly
// represents all four forms accepted by ValueRef.UnmarshalYAML:
//  1. Scalar literal (string, number, boolean, null)
//  2. Array literal
//  3. Map literal (object with no special keys)
//  4. Structured ref: object with exactly one of {rslvr, expr, tmpl}
func patchValueRef(defs map[string]any) {
	// Find the ValueRef key — the namer prepends the package name.
	key := findDefKey(defs, "ValueRef")
	if key == "" {
		return
	}

	// Build the structured reference schema (object with rslvr/expr/tmpl).
	structuredRef := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"rslvr": map[string]any{
				"type":        "string",
				"description": "Reference to another resolver by name",
				"pattern":     "^[a-zA-Z_][a-zA-Z0-9_-]*$",
			},
			"expr": map[string]any{
				"type":        "string",
				"description": "CEL expression to evaluate",
			},
			"tmpl": map[string]any{
				"type":        "string",
				"description": "Go template to execute",
			},
		},
	}

	// ValueRef accepts any of: string, number, integer, boolean, null,
	// array, or an object (which could be a literal map or a structured ref
	// with rslvr/expr/tmpl). We use anyOf because an object with rslvr/expr/tmpl
	// keys would match multiple schemas in a strict oneOf.
	defs[key] = map[string]any{
		"description": "A value that can be a literal (string, number, boolean, array, object), " +
			"a resolver reference (rslvr), a CEL expression (expr), or a Go template (tmpl).",
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "number"},
			map[string]any{"type": "integer"},
			map[string]any{"type": "boolean"},
			map[string]any{"type": "null"},
			map[string]any{"type": "array"},
			structuredRef,
		},
	}
}

// patchSkipBuiltinsValue replaces the SkipBuiltinsValue $def with oneOf
// since it supports both bool and []string via custom UnmarshalYAML but
// Huma sees an empty object (all fields are json:"-").
func patchSkipBuiltinsValue(defs map[string]any) {
	key := findDefKey(defs, "SkipBuiltinsValue")
	if key == "" {
		return
	}

	defs[key] = map[string]any{
		"description": "When true, skips all builtins. When a list, skips only the named builtins.",
		"oneOf": []any{
			map[string]any{"type": "boolean"},
			map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
	}
}

// findDefKey searches the $defs map for a key that ends with the given suffix.
// The Huma namer prepends the package name (e.g., "SpecValueRef" for spec.ValueRef),
// so we match by suffix.
func findDefKey(defs map[string]any, suffix string) string {
	for k := range defs {
		if strings.HasSuffix(k, suffix) {
			return k
		}
	}
	return ""
}
