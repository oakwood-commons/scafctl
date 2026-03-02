// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package collections provides Go template extension functions for filtering
// and projecting lists of maps, enabling common data transformation patterns
// directly within Go templates.
package collections

import (
	"fmt"
	"reflect"
	"text/template"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// WhereFunc returns an ExtFunction that filters a list of maps by a key/value match.
//
// Example usage in a Go template:
//
//	{{ .items | where "status" "active" }}
func WhereFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name: "where",
		Description: "Filters a list of maps, returning only entries where the given key " +
			"equals the given value. Non-map entries and entries missing the key are excluded. " +
			"Returns an empty list if no matches are found.",
		Custom: true,
		Examples: []gotmpl.Example{
			{
				Description: "Filter active items",
				Template:    `{{ .items | where "status" "active" }}`,
			},
			{
				Description: "Filter by type",
				Template:    `{{ .resources | where "kind" "Deployment" }}`,
			},
			{
				Description: "Chain with range",
				Template:    `{{ range (.items | where "enabled" true) }}{{ .name }}{{ end }}`,
			},
		},
		Func: template.FuncMap{
			"where": Where,
		},
	}
}

// SelectFunc returns an ExtFunction that projects a single field from a list of maps.
//
// Example usage in a Go template:
//
//	{{ .items | selectField "name" }}
func SelectFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name: "selectField",
		Description: "Extracts a single field from each map in a list, returning a flat list of values. " +
			"Non-map entries are skipped. Entries missing the key produce nil in the output.",
		Custom: true,
		Examples: []gotmpl.Example{
			{
				Description: "Extract names from a list of items",
				Template:    `{{ .items | selectField "name" }}`,
			},
			{
				Description: "Get all IDs and join them",
				Template:    `{{ .items | selectField "id" | join ", " }}`,
			},
			{
				Description: "Extract nested field for further processing",
				Template:    `{{ range (.items | selectField "email") }}{{ . }}{{ end }}`,
			},
		},
		Func: template.FuncMap{
			"selectField": SelectField,
		},
	}
}

// Where filters a list, returning only entries where list[i][key] == value.
//
// The list can be []any, []map[string]any, or any slice type.
// Non-map entries are silently skipped. The comparison uses reflect.DeepEqual
// so it works with strings, numbers, booleans, and nested structures.
//
// Returns an empty []any if the input is nil, empty, or has no matches.
func Where(key string, value, list any) ([]any, error) {
	items, err := toSlice(list)
	if err != nil {
		return nil, fmt.Errorf("where: %w", err)
	}

	var result []any
	for _, item := range items {
		m, ok := toMap(item)
		if !ok {
			continue
		}
		v, exists := m[key]
		if exists && reflect.DeepEqual(v, value) {
			result = append(result, item)
		}
	}

	if result == nil {
		return []any{}, nil
	}
	return result, nil
}

// SelectField extracts a single field from each map in a list.
//
// The list can be []any, []map[string]any, or any slice type.
// Non-map entries are silently skipped. If a map doesn't have the specified
// key, nil is included in the output to preserve positional alignment.
//
// Returns an empty []any if the input is nil or empty.
func SelectField(key string, list any) ([]any, error) {
	items, err := toSlice(list)
	if err != nil {
		return nil, fmt.Errorf("selectField: %w", err)
	}

	var result []any
	for _, item := range items {
		m, ok := toMap(item)
		if !ok {
			continue
		}
		result = append(result, m[key])
	}

	if result == nil {
		return []any{}, nil
	}
	return result, nil
}

// toSlice converts any slice or array to []any using reflection.
// Returns nil, nil for nil input. Returns an error for non-slice/array types.
func toSlice(v any) ([]any, error) {
	if v == nil {
		return nil, nil
	}

	// Fast path for common types
	switch typed := v.(type) {
	case []any:
		return typed, nil
	case []map[string]any:
		result := make([]any, len(typed))
		for i, m := range typed {
			result[i] = m
		}
		return result, nil
	}

	// Reflection fallback for other slice/array types
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("expected a list/array, got %T", v)
	}

	result := make([]any, rv.Len())
	for i := range rv.Len() {
		result[i] = rv.Index(i).Interface()
	}
	return result, nil
}

// toMap attempts to convert a value to map[string]any.
// Returns the map and true if successful, nil and false otherwise.
func toMap(v any) (map[string]any, bool) {
	if v == nil {
		return nil, false
	}

	// Fast path
	if m, ok := v.(map[string]any); ok {
		return m, true
	}

	// Reflection fallback for map types with string keys
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Map {
		return nil, false
	}

	if rv.Type().Key().Kind() != reflect.String {
		return nil, false
	}

	result := make(map[string]any, rv.Len())
	for _, key := range rv.MapKeys() {
		result[key.String()] = rv.MapIndex(key).Interface()
	}
	return result, true
}
