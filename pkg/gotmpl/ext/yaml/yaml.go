// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package yaml provides Go template extension functions for YAML
// serialization and deserialization.
//
// These functions fill the gap left by Sprig v3.3.0, which removed
// toYaml/fromYaml when it dropped the gopkg.in/yaml.v2 dependency.
// This package re-implements them using gopkg.in/yaml.v3.
package yaml

import (
	"fmt"
	"strings"
	"text/template"

	goyaml "gopkg.in/yaml.v3"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// ToYamlFunc returns an ExtFunction that encodes a Go value as a YAML string.
//
// Example usage in a Go template:
//
//	{{ .data | toYaml }}
//	{{ toYaml .config }}
func ToYamlFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name:        "toYaml",
		Description: "Encodes a value as a YAML string. Accepts any Go value (maps, slices, structs, primitives) and returns its YAML representation.",
		Custom:      true,
		Links:       []string{"https://pkg.go.dev/gopkg.in/yaml.v3"},
		Examples: []gotmpl.Example{
			{
				Description: "Convert a map to YAML",
				Template:    `{{ dict "name" "myapp" "port" 8080 | toYaml }}`,
			},
			{
				Description: "Convert template data to YAML",
				Template:    `{{ .config | toYaml }}`,
			},
			{
				Description: "Combined with indent for nesting",
				Template:    `{{ .data | toYaml | indent 4 }}`,
			},
		},
		Func: template.FuncMap{
			"toYaml": ToYaml,
		},
	}
}

// FromYamlFunc returns an ExtFunction that decodes a YAML string into a map.
//
// Example usage in a Go template:
//
//	{{ .yamlString | fromYaml }}
//	{{ (fromYaml .yamlString).fieldName }}
func FromYamlFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name:        "fromYaml",
		Description: "Decodes a YAML string into a map[string]any. Returns an empty map if the input is empty.",
		Custom:      true,
		Links:       []string{"https://pkg.go.dev/gopkg.in/yaml.v3"},
		Examples: []gotmpl.Example{
			{
				Description: "Parse a YAML string and access a field",
				Template:    `{{ "name: myapp\nport: 8080" | fromYaml | get "name" }}`,
			},
			{
				Description: "Round-trip: create YAML and parse it back",
				Template:    `{{ dict "env" "prod" | toYaml | fromYaml | get "env" }}`,
			},
		},
		Func: template.FuncMap{
			"fromYaml": FromYaml,
		},
	}
}

// MustToYamlFunc returns an ExtFunction identical to toYaml.
// In Go templates, both toYaml and mustToYaml return (string, error);
// the template engine surfaces errors automatically. This variant exists
// for naming compatibility with Helm conventions.
func MustToYamlFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name:        "mustToYaml",
		Description: "Encodes a value as a YAML string, returning an error on failure. Identical to toYaml in behavior (Go templates always propagate errors).",
		Custom:      true,
		Links:       []string{"https://pkg.go.dev/gopkg.in/yaml.v3"},
		Examples: []gotmpl.Example{
			{
				Description: "Encode a value as YAML (error-propagating)",
				Template:    `{{ .config | mustToYaml }}`,
			},
		},
		Func: template.FuncMap{
			"mustToYaml": ToYaml,
		},
	}
}

// MustFromYamlFunc returns an ExtFunction identical to fromYaml.
// In Go templates, both fromYaml and mustFromYaml return (map[string]any, error);
// the template engine surfaces errors automatically. This variant exists
// for naming compatibility with Helm conventions.
func MustFromYamlFunc() gotmpl.ExtFunction {
	return gotmpl.ExtFunction{
		Name:        "mustFromYaml",
		Description: "Decodes a YAML string into a map[string]any, returning an error on failure. Identical to fromYaml in behavior (Go templates always propagate errors).",
		Custom:      true,
		Links:       []string{"https://pkg.go.dev/gopkg.in/yaml.v3"},
		Examples: []gotmpl.Example{
			{
				Description: "Parse YAML with error propagation",
				Template:    `{{ .yamlInput | mustFromYaml }}`,
			},
		},
		Func: template.FuncMap{
			"mustFromYaml": FromYaml,
		},
	}
}

// ToYaml encodes a Go value as a YAML string.
// Returns an empty string for nil input. The trailing newline added by
// the YAML encoder is trimmed for cleaner template composition.
//
// Parameters:
//
//	v any: The Go value to encode. Supports maps, slices, structs, and primitives.
//
// Returns:
//
//	string: The YAML representation of the value.
//	error: An error if encoding fails.
func ToYaml(v any) (string, error) {
	if v == nil {
		return "", nil
	}

	data, err := goyaml.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("toYaml: failed to marshal: %w", err)
	}

	// Trim the trailing newline that yaml.Marshal always appends,
	// so templates can control whitespace themselves.
	return strings.TrimSuffix(string(data), "\n"), nil
}

// FromYaml decodes a YAML string into a map[string]any.
// Returns an empty map for empty input.
//
// Parameters:
//
//	str string: The YAML string to decode.
//
// Returns:
//
//	map[string]any: The decoded YAML data.
//	error: An error if decoding fails.
func FromYaml(str string) (map[string]any, error) {
	if strings.TrimSpace(str) == "" {
		return map[string]any{}, nil
	}

	var result map[string]any
	if err := goyaml.Unmarshal([]byte(str), &result); err != nil {
		return nil, fmt.Errorf("fromYaml: failed to unmarshal: %w", err)
	}

	if result == nil {
		return map[string]any{}, nil
	}

	return result, nil
}
