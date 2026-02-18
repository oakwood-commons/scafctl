// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
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
		for k, v := range rootMap {
			doc[k] = v
		}

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
