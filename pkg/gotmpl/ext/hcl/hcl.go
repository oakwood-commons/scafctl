// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package hcl provides a Go template extension function for converting
// Go objects into HCL (HashiCorp Configuration Language) format.
package hcl

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// ToHclFunc returns an ExtFunction that converts a Go object into HCL format.
//
// The function accepts any Go value (struct, map, slice, primitive) and returns
// its HCL string representation. Structs and maps are converted to HCL attribute
// assignments, slices become HCL lists, and nested structs/maps become HCL blocks.
//
// Example usage in a Go template:
//
//	{{ .data | toHcl }}
//	{{ toHcl .config }}
func ToHclFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name:        "toHcl",
		Description: "Converts a Go object into HCL (HashiCorp Configuration Language) format. Accepts structs, maps, slices, and primitives. Returns the HCL representation as a string.",
		Custom:      true,
		Links:       []string{"https://github.com/hashicorp/hcl"},
		Examples: []gotmpl.Example{
			{
				Description: "Convert a map to HCL",
				Template:    `{{ dict "name" "myapp" "port" 8080 | toHcl }}`,
			},
			{
				Description: "Convert template data to HCL",
				Template:    `{{ .config | toHcl }}`,
			},
			{
				Description: "Convert a nested structure to HCL",
				Template:    `{{ dict "server" (dict "host" "localhost" "port" 443) | toHcl }}`,
			},
		},
		Func: template.FuncMap{
			"toHcl": ToHcl,
		},
	}
}

// ToHclValueFunc returns an ExtFunction that converts a Go object into an HCL
// value expression.
//
// Unlike toHcl (which renders a map as an HCL body of bare attributes and blocks
// for generating .tf/.tfvars file bodies), toHclValue always renders a valid HCL
// value expression suitable for the right-hand side of an assignment: scalars
// become literals, lists become tuples ([...]), maps become object literals
// ({ k = v, ... }), and lists of objects become tuples of object literals
// ([{ ... }, ...]).
//
// Example usage in a Go template:
//
//	labels = {{ .labels | toHclValue }}
//	bindings = {{ .wifBindings | toHclValue }}
func ToHclValueFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name:        "toHclValue",
		Description: "Converts a Go object into an HCL value expression for the right-hand side of an assignment. Scalars become literals, lists become tuples, maps become object literals, and lists of objects become tuples of object literals. Use toHcl instead to generate an HCL file body of bare attributes and blocks.",
		Custom:      true,
		Links:       []string{"https://github.com/hashicorp/hcl"},
		Examples: []gotmpl.Example{
			{
				Description: "Convert a map to an HCL object literal",
				Template:    `labels = {{ dict "env" "prod" "tier" "web" | toHclValue }}`,
			},
			{
				Description: "Convert a list of objects to an HCL tuple of object literals",
				Template:    `bindings = {{ .wifBindings | toHclValue }}`,
			},
			{
				Description: "Convert a scalar list to an HCL tuple",
				Template:    `tags = {{ list "web" "prod" | toHclValue }}`,
			},
		},
		Func: template.FuncMap{
			"toHclValue": ToHclValue,
		},
	}
}

// ToHcl converts a Go object to its HCL string representation.
//
// For example:
//
//	obj := map[string]any{"key": "value"}
//	ToHcl(obj) // returns: key = "value"\n
//
// Parameters:
//
//	obj any: The Go object to convert. Supports structs, maps, slices, and primitives.
//
// Returns:
//
//	string: The HCL representation of the Go object.
//	error: An error if the conversion fails.
func ToHcl(obj any) (string, error) {
	if obj == nil {
		return "", nil
	}

	// Normalize the input: convert structs and complex types to map[string]any
	// via JSON round-trip for consistent handling
	normalized, err := normalize(obj)
	if err != nil {
		return "", fmt.Errorf("toHcl: failed to normalize input: %w", err)
	}

	var buf strings.Builder
	if err := writeHcl(&buf, normalized, 0); err != nil {
		return "", fmt.Errorf("toHcl: %w", err)
	}

	return buf.String(), nil
}

// ToHclValue converts a Go object to a valid HCL value expression.
//
// Unlike ToHcl, a map is rendered as an inline object literal ({ k = v, ... })
// rather than a bare body, so the result is always valid on the right-hand side
// of an HCL assignment.
//
// For example:
//
//	obj := []any{map[string]any{"ns": "y1"}, map[string]any{"ns": "y2"}}
//	ToHclValue(obj) // returns: [{ ns = "y1" }, { ns = "y2" }]
//
// Parameters:
//
//	obj any: The Go object to convert. Supports structs, maps, slices, and primitives.
//
// Returns:
//
//	string: The HCL value expression for the Go object.
//	error: An error if the conversion fails.
func ToHclValue(obj any) (string, error) {
	if obj == nil {
		return "null", nil
	}

	normalized, err := normalize(obj)
	if err != nil {
		return "", fmt.Errorf("toHclValue: failed to normalize input: %w", err)
	}

	var buf strings.Builder
	if err := writeHclValue(&buf, normalized); err != nil {
		return "", fmt.Errorf("toHclValue: %w", err)
	}

	return buf.String(), nil
}

// normalize converts any Go value to a JSON-friendly representation
// (map[string]any, []any, or primitives) via JSON round-trip.
func normalize(obj any) (any, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal: %w", err)
	}

	var result any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal: %w", err)
	}

	return result, nil
}

// writeHcl recursively writes HCL-formatted output for a normalized value.
//
// A top-level map is rendered as an HCL body (bare attributes and nested
// blocks), which is the common case for generating .tf/.tfvars file bodies. A
// top-level list of objects has no key to act as a block label, so it cannot be
// a valid body; it is rendered as a tuple of object literals ([{ ... }, ...]).
// To render an arbitrary value as an HCL value expression (for the right-hand
// side of an assignment), use ToHclValue instead.
func writeHcl(buf *strings.Builder, value any, indent int) error {
	switch v := value.(type) {
	case map[string]any:
		return writeHclMap(buf, v, indent)
	case []any:
		// A keyless top-level list cannot be an HCL body; render it as a value
		// expression (scalar tuple or tuple of object literals).
		return writeHclListValue(buf, v)
	default:
		// Scalar at top level — emit as a bare HCL literal
		buf.WriteString(formatHclValue(v))
		return nil
	}
}

// writeHclMap writes a map as HCL attributes and blocks.
func writeHclMap(buf *strings.Builder, m map[string]any, indent int) error {
	prefix := strings.Repeat("  ", indent)

	// Sort keys for deterministic output
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		val := m[key]

		switch v := val.(type) {
		case map[string]any:
			// Nested map becomes an HCL block
			fmt.Fprintf(buf, "%s%s {\n", prefix, key)
			if err := writeHclMap(buf, v, indent+1); err != nil {
				return err
			}
			fmt.Fprintf(buf, "%s}\n", prefix)

		case []any:
			if isListOfMaps(v) {
				// List of maps becomes repeated blocks
				for _, item := range v {
					itemMap, ok := item.(map[string]any)
					if !ok {
						return fmt.Errorf("expected map in list of maps for key %q, got %T", key, item)
					}
					fmt.Fprintf(buf, "%s%s {\n", prefix, key)
					if err := writeHclMap(buf, itemMap, indent+1); err != nil {
						return err
					}
					fmt.Fprintf(buf, "%s}\n", prefix)
				}
			} else {
				// Simple list becomes an HCL list attribute
				fmt.Fprintf(buf, "%s%s = ", prefix, key)
				if err := writeHclListValue(buf, v); err != nil {
					return err
				}
				buf.WriteString("\n")
			}

		default:
			// Primitive attribute
			fmt.Fprintf(buf, "%s%s = %s\n", prefix, key, formatHclValue(val))
		}
	}

	return nil
}

// writeHclValue writes any normalized value as an HCL value expression: scalars
// become literals, lists become tuples ([...]), and maps become object literals
// ({ k = v, ... }). Unlike writeHcl, a map is never rendered as a bare body, so
// the result is always valid on the right-hand side of an assignment.
func writeHclValue(buf *strings.Builder, value any) error {
	switch v := value.(type) {
	case map[string]any:
		return writeHclObjectLiteral(buf, v)
	case []any:
		return writeHclListValue(buf, v)
	default:
		buf.WriteString(formatHclValue(v))
		return nil
	}
}

// writeHclObjectLiteral writes a map as an inline HCL object literal:
// { key = value, ... }. Keys are sorted for deterministic output.
func writeHclObjectLiteral(buf *strings.Builder, m map[string]any) error {
	if len(m) == 0 {
		buf.WriteString("{}")
		return nil
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf.WriteString("{ ")
	for i, key := range keys {
		if i > 0 {
			buf.WriteString(", ")
		}
		fmt.Fprintf(buf, "%s = ", key)
		if err := writeHclValue(buf, m[key]); err != nil {
			return err
		}
	}
	buf.WriteString(" }")
	return nil
}

// writeHclListValue writes a list as an HCL tuple: [val1, val2, ...]. Elements
// may be scalars, nested lists, or maps (rendered as object literals).
func writeHclListValue(buf *strings.Builder, list []any) error {
	buf.WriteString("[")
	for i, item := range list {
		if i > 0 {
			buf.WriteString(", ")
		}
		if err := writeHclValue(buf, item); err != nil {
			return err
		}
	}
	buf.WriteString("]")
	return nil
}

// formatHclValue formats a primitive Go value as an HCL literal.
func formatHclValue(val any) string {
	if val == nil {
		return "null"
	}

	v := reflect.ValueOf(val)

	//exhaustive:enforce
	switch v.Kind() {
	case reflect.String:
		return fmt.Sprintf("%q", v.String())
	case reflect.Bool:
		return fmt.Sprintf("%t", v.Bool())
	case reflect.Float64, reflect.Float32:
		f := v.Float()
		// If the float is actually an integer value, format without decimal
		if f == float64(int64(f)) {
			return fmt.Sprintf("%d", int64(f))
		}
		return fmt.Sprintf("%g", f)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return fmt.Sprintf("%d", v.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return fmt.Sprintf("%d", v.Uint())
	case reflect.Invalid,
		reflect.Uintptr,
		reflect.Complex64,
		reflect.Complex128,
		reflect.Array,
		reflect.Chan,
		reflect.Func,
		reflect.Interface,
		reflect.Map,
		reflect.Pointer,
		reflect.Slice,
		reflect.Struct,
		reflect.UnsafePointer:
		return fmt.Sprintf("%q", fmt.Sprintf("%v", val))
	}

	return fmt.Sprintf("%q", fmt.Sprintf("%v", val))
}

// isListOfMaps returns true if all elements in the list are maps.
func isListOfMaps(list []any) bool {
	if len(list) == 0 {
		return false
	}
	for _, item := range list {
		if _, ok := item.(map[string]any); !ok {
			return false
		}
	}
	return true
}
