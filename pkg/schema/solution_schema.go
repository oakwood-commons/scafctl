// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/oakwood-commons/scafctl/pkg/duration"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// SolutionSchemaID is the JSON Schema ID for the solution file format.
const SolutionSchemaID = "https://scafctl.dev/schemas/v1/solution.json"

// ComposePartSchemaID is the JSON Schema ID for composed partial YAML files.
const ComposePartSchemaID = "https://scafctl.dev/schemas/v1/compose-part.json"

var (
	solutionSchemaMu          sync.Mutex
	solutionSchemaInitialized bool
	solutionSchemaJSON        []byte
	solutionSchemaErr         error

	composePartSchemaMu          sync.Mutex
	composePartSchemaInitialized bool
	composePartSchemaJSON        []byte
	composePartSchemaErr         error
)

// durationAlias is a simple string type used as a Huma type alias for
// time.Duration and duration.Duration, which Huma cannot natively reflect
// (it panics on example:"30s" because it tries to parse it as an integer).
type durationAlias string

// GenerateSolutionSchema generates a JSON Schema for the Solution struct
// using Huma's schema generation which reads the struct tags (doc, example,
// pattern, maxLength, etc.) to produce a full OpenAPI-compatible schema.
func GenerateSolutionSchema() ([]byte, error) {
	solutionSchemaMu.Lock()
	defer solutionSchemaMu.Unlock()
	if !solutionSchemaInitialized {
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
			solutionSchemaInitialized = true
			return solutionSchemaJSON, solutionSchemaErr
		}
		var rootMap map[string]any
		if err := json.Unmarshal(rootBytes, &rootMap); err != nil {
			solutionSchemaErr = err
			solutionSchemaInitialized = true
			return solutionSchemaJSON, solutionSchemaErr
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
		solutionSchemaInitialized = true
	}
	return solutionSchemaJSON, solutionSchemaErr
}

// resetSolutionSchemaForTesting resets the solution and compose-part schema
// caches for testing. Acquires each mutex independently to avoid lock-order
// coupling.
// WARNING: This should only be called from tests.
func resetSolutionSchemaForTesting() {
	solutionSchemaMu.Lock()
	solutionSchemaInitialized = false
	solutionSchemaJSON = nil
	solutionSchemaErr = nil
	solutionSchemaMu.Unlock()

	composePartSchemaMu.Lock()
	composePartSchemaInitialized = false
	composePartSchemaJSON = nil
	composePartSchemaErr = nil
	composePartSchemaMu.Unlock()
}

// GenerateComposePartSchema generates a JSON Schema for a partial YAML file
// referenced in a solution's compose list. It reuses the patched component
// $defs from GenerateSolutionSchema and restricts the document envelope to the
// sections bundler.Compose merges: spec.resolvers, spec.workflow.{actions,finally},
// spec.testing, and bundle.include, with additionalProperties: false at every
// wrapper object.
func GenerateComposePartSchema() ([]byte, error) {
	composePartSchemaMu.Lock()
	defer composePartSchemaMu.Unlock()
	if !composePartSchemaInitialized {
		composePartSchemaJSON, composePartSchemaErr = buildComposePartSchema()
		composePartSchemaInitialized = true
	}
	if composePartSchemaErr != nil {
		return nil, composePartSchemaErr
	}
	return slices.Clone(composePartSchemaJSON), nil
}

func buildComposePartSchema() ([]byte, error) {
	fullBytes, err := GenerateSolutionSchema()
	if err != nil {
		return nil, fmt.Errorf("generating base solution schema: %w", err)
	}

	var baseDoc map[string]any
	if err := json.Unmarshal(fullBytes, &baseDoc); err != nil {
		return nil, fmt.Errorf("unmarshalling base solution schema: %w", err)
	}

	defs, ok := baseDoc["$defs"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("base solution schema missing $defs")
	}

	specProps, err := defProperties(defs, "SolutionSpec")
	if err != nil {
		return nil, err
	}
	resolversProp, ok := specProps["resolvers"]
	if !ok {
		return nil, fmt.Errorf("SolutionSpec schema missing resolvers property")
	}
	testingProp, ok := specProps["testing"]
	if !ok {
		return nil, fmt.Errorf("SolutionSpec schema missing testing property")
	}

	wfProps, err := defProperties(defs, "ActionWorkflow")
	if err != nil {
		return nil, err
	}
	actionsProp, ok := wfProps["actions"]
	if !ok {
		return nil, fmt.Errorf("ActionWorkflow schema missing actions property")
	}
	finallyProp, ok := wfProps["finally"]
	if !ok {
		return nil, fmt.Errorf("ActionWorkflow schema missing finally property")
	}

	bundleProps, err := defProperties(defs, "SolutionBundle")
	if err != nil {
		return nil, err
	}
	includeProp, ok := bundleProps["include"]
	if !ok {
		return nil, fmt.Errorf("SolutionBundle schema missing include property")
	}

	doc := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  ComposePartSchemaID,
		"title":                "scafctl Compose Part",
		"description":          "Schema for a partial YAML file merged into a scafctl solution via compose.",
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"spec": map[string]any{
				"type":                 "object",
				"description":          "Partial execution specification contributed by a composed file.",
				"additionalProperties": false,
				"properties": map[string]any{
					"resolvers": resolversProp,
					"workflow": map[string]any{
						"type":                 "object",
						"description":          "Partial workflow specification contributed by a composed file.",
						"additionalProperties": false,
						"properties": map[string]any{
							"actions": actionsProp,
							"finally": finallyProp,
						},
					},
					"testing": testingProp,
				},
			},
			"bundle": map[string]any{
				"type":                 "object",
				"description":          "Partial build-time bundling configuration contributed by a composed file.",
				"additionalProperties": false,
				"properties": map[string]any{
					"include": includeProp,
				},
			},
		},
		"$defs": defs,
	}

	return json.MarshalIndent(doc, "", "  ")
}

func defProperties(defs map[string]any, name string) (map[string]any, error) {
	defRaw, ok := defs[name]
	if !ok {
		if key := findDefKey(defs, name); key != "" {
			defRaw = defs[key]
			ok = true
		}
	}
	if !ok {
		return nil, fmt.Errorf("schema missing $defs.%s", name)
	}
	def, ok := defRaw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema $defs.%s is not an object", name)
	}
	props, ok := def["properties"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schema $defs.%s missing properties", name)
	}
	return props, nil
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
	for t.Kind() == reflect.Pointer {
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
	patchSkipValue(defs)
	patchMapKeyNames(defs)
	patchJSONSchemaType(defs)
	patchCondition(defs)
}

// patchCondition replaces every "*Condition" $def with a schema that accepts
// all forms supported by Condition.UnmarshalYAML: a non-empty CEL expression
// string, a boolean literal, or the explicit object form using either an
// "expr" or "expression" key. Huma only reflects the object form's "expr"
// field from the struct, which causes false-positive schema violations on the
// string and boolean shorthands (and on the "expression" alias) used by when,
// until, and continueOnError. Empty strings are excluded because
// Condition.UnmarshalYAML rejects them.
func patchCondition(defs map[string]any) {
	for key := range defs {
		if !strings.HasSuffix(key, "Condition") {
			continue
		}
		objectForm, ok := defs[key].(map[string]any)
		if !ok {
			continue
		}
		defs[key] = map[string]any{
			"description": "A condition: a non-empty CEL expression string, a boolean " +
				"literal, or an object {expr: \"...\"} (\"expression\" is also accepted).",
			"anyOf": []any{
				map[string]any{"type": "string", "minLength": 1},
				map[string]any{"type": "boolean"},
				conditionObjectSchema(objectForm),
			},
		}
	}
}

// conditionObjectSchema returns the object form for a Condition, extending the
// Huma-reflected schema (which only exposes "expr") to also accept the
// "expression" key alias supported by Condition.UnmarshalYAML. Either key
// satisfies the requirement, matching the runtime which accepts one or the
// other (but not both).
func conditionObjectSchema(objectForm map[string]any) map[string]any {
	props, ok := objectForm["properties"].(map[string]any)
	if !ok {
		return objectForm
	}
	exprSchema, ok := props["expr"]
	if !ok {
		return objectForm
	}
	if _, exists := props["expression"]; !exists {
		props["expression"] = exprSchema
	}
	// Replace the reflected "required: [expr]" with a constraint that accepts
	// either the "expr" or "expression" key, but not both (the runtime
	// Condition unmarshallers reject specifying both keys).
	delete(objectForm, "required")
	objectForm["anyOf"] = []any{
		map[string]any{"required": []any{"expr"}},
		map[string]any{"required": []any{"expression"}},
	}
	objectForm["not"] = map[string]any{
		"required": []any{"expr", "expression"},
	}
	return objectForm
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

// patchValueRef replaces the ValueRef $def with a schema that accurately models
// the custom UnmarshalYAML/UnmarshalJSON behavior:
//  1. Scalar literal (string, number, boolean, null)
//  2. Array literal
//  3. Map literal (object with no special ValueRef keys)
//  4. Structured ref: object with exactly one of {rslvr, expr, tmpl, scrt, tkn}
func patchValueRef(defs map[string]any) {
	// Find the ValueRef key — the namer prepends the package name.
	key := findDefKey(defs, "ValueRef")
	if key == "" {
		return
	}

	tokenRefObject := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"handler"},
		"properties": map[string]any{
			"handler": map[string]any{
				"type":        "string",
				"description": "Auth handler name",
				"minLength":   1,
				"maxLength":   128,
				"pattern":     "^[a-z0-9][a-z0-9-]*$",
			},
			"scope": map[string]any{
				"type":        "string",
				"description": "OAuth2/OIDC scope(s) for handlers supporting per-request scopes",
				"maxLength":   2048,
			},
			"profile": map[string]any{
				"type":        "string",
				"description": "Named auth profile override",
				"maxLength":   128,
			},
			"hostname": map[string]any{
				"type":        "string",
				"description": "Target cluster/server hostname or alias (requires token_hostname capability)",
				"maxLength":   512,
			},
			"minValidFor": map[string]any{
				"type":        "string",
				"description": "Minimum remaining validity duration (e.g. 5m, 30s)",
				"maxLength":   64,
			},
		},
	}

	if tokenKey := findDefKey(defs, "TokenRef"); tokenKey != "" {
		defs[tokenKey] = map[string]any{
			"description": "Auth token reference minted by an auth handler (string shorthand or structured object).",
			"anyOf": []any{
				map[string]any{
					"type":        "string",
					"description": "Auth handler name shorthand (e.g. github)",
					"minLength":   1,
					"maxLength":   128,
					"pattern":     "^[a-z0-9][a-z0-9-]*$",
				},
				tokenRefObject,
			},
		}
	}

	// Build the structured reference schema (object with exactly one of rslvr/expr/tmpl/scrt/tkn).
	structuredRef := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"minProperties":        1,
		"maxProperties":        1,
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
			"scrt": map[string]any{
				"type":        "string",
				"description": "Secret name from the host secret store",
				"minLength":   1,
				"maxLength":   255,
			},
			"tkn": map[string]any{
				"description": "Auth token reference minted by an auth handler",
				"anyOf": []any{
					map[string]any{
						"type":        "string",
						"description": "Auth handler name shorthand",
						"minLength":   1,
						"maxLength":   128,
						"pattern":     "^[a-z0-9][a-z0-9-]*$",
					},
					tokenRefObject,
				},
			},
		},
	}

	literalMap := map[string]any{
		"type": "object",
		"not": map[string]any{
			"anyOf": []any{
				map[string]any{"required": []any{"rslvr"}},
				map[string]any{"required": []any{"expr"}},
				map[string]any{"required": []any{"tmpl"}},
				map[string]any{"required": []any{"scrt"}},
				map[string]any{"required": []any{"tkn"}},
			},
		},
	}

	defs[key] = map[string]any{
		"description": "A value that can be a literal (string, number, boolean, array, object), " +
			"a resolver reference (rslvr), a CEL expression (expr), a Go template (tmpl), " +
			"a host secret reference (scrt), or an auth token reference (tkn).",
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "number"},
			map[string]any{"type": "integer"},
			map[string]any{"type": "boolean"},
			map[string]any{"type": "null"},
			map[string]any{"type": "array"},
			structuredRef,
			literalMap,
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

// patchSkipValue replaces the SkipValue $def with oneOf
// since it supports both bool and string (CEL expression) via custom UnmarshalYAML
// but Huma sees an empty object (all fields are json:"-").
func patchSkipValue(defs map[string]any) {
	key := findDefKey(defs, "SkipValue")
	if key == "" {
		return
	}

	defs[key] = map[string]any{
		"description": "When true, unconditionally skips the test. When a string, evaluated as a CEL expression for conditional skipping.",
		"oneOf": []any{
			map[string]any{"type": "boolean"},
			map[string]any{"type": "string"},
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
