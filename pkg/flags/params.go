// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package flags

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadParameterFile loads parameters from a YAML or JSON file.
// The file format is auto-detected based on extension, or by trying
// YAML first then JSON if the extension is not recognized.
func LoadParameterFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read parameter file %q: %w", path, err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	result := make(map[string]any)

	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse YAML parameter file %q: %w", path, err)
		}
	case ".json":
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to parse JSON parameter file %q: %w", path, err)
		}
	default:
		// Try YAML first, then JSON
		if yamlErr := yaml.Unmarshal(data, &result); yamlErr != nil {
			if jsonErr := json.Unmarshal(data, &result); jsonErr != nil {
				return nil, fmt.Errorf("failed to parse parameter file %q (tried YAML and JSON): %w", path, errors.Join(yamlErr, jsonErr))
			}
		}
	}

	return result, nil
}

// ParseResolverFlags parses -r flag values, handling both key=value syntax
// and @file.yaml syntax for loading parameters from files.
//
// Supported formats:
//   - key=value: Simple key-value pair
//   - key=value1,value2: Multiple values (becomes an array)
//   - @file.yaml: Load all parameters from a YAML file
//   - @file.json: Load all parameters from a JSON file
//
// Multiple values for the same key are automatically combined into an array.
func ParseResolverFlags(values []string) (map[string]any, error) {
	result := make(map[string]any)

	for _, v := range values {
		if strings.HasPrefix(v, "@") {
			// Load from file
			filePath := strings.TrimPrefix(v, "@")
			fileParams, err := LoadParameterFile(filePath)
			if err != nil {
				return nil, err
			}
			// Merge file params into result
			for k, val := range fileParams {
				result[k] = MergeValue(result[k], val)
			}
		} else {
			// Parse key=value using ParseKeyValueCSV
			parsed, err := ParseKeyValueCSV([]string{v})
			if err != nil {
				return nil, fmt.Errorf("failed to parse parameter %q: %w", v, err)
			}
			// Merge parsed values
			for k, vals := range parsed {
				// Convert []string to appropriate type
				if len(vals) == 1 {
					result[k] = MergeValue(result[k], vals[0])
				} else {
					// Multiple values - convert to []any
					anyVals := make([]any, len(vals))
					for i, s := range vals {
						anyVals[i] = s
					}
					result[k] = MergeValue(result[k], anyVals)
				}
			}
		}
	}

	return result, nil
}

// ParseDynamicInputArgs normalises raw CLI arguments into key=value strings
// suitable for ParseResolverFlags.
//
// Three forms are recognised:
//
//	--key=value    → strip the leading "--" → "key=value"
//	key=value      → passed through unchanged
//	@file.yaml     → passed through unchanged (file reference)
//
// A bare "--key" (no "=") is rejected because we cannot distinguish a
// boolean flag from a flag that expects the next token as a value.
// Single-dash forms ("-k=v") are also rejected to avoid collisions with
// existing short flags.
func ParseDynamicInputArgs(args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--") && strings.Contains(a, "="):
			// --key=value → key=value
			out = append(out, strings.TrimPrefix(a, "--"))

		case strings.HasPrefix(a, "--"):
			// --key without "=" — ambiguous
			return nil, fmt.Errorf("dynamic flag %q must use --key=value syntax (= is required)", a)

		case strings.HasPrefix(a, "-"):
			// -k or -k=v — reject to avoid short-flag collisions
			return nil, fmt.Errorf("single-dash flag %q is not supported for dynamic inputs; use --key=value or key=value", a)

		case strings.HasPrefix(a, "@"):
			// @file.yaml — file reference, pass through
			out = append(out, a)

		case strings.Contains(a, "="):
			// key=value positional — pass through
			out = append(out, a)

		default:
			return nil, fmt.Errorf("unexpected argument %q: use key=value or --key=value for provider inputs", a)
		}
	}
	return out, nil
}

// MergeValue merges a new value with an existing value, creating arrays as needed.
// If existing is nil, returns newVal. If both are slices, concatenates them.
// If existing is a scalar and newVal is provided, creates a slice.
func MergeValue(existing, newVal any) any {
	if existing == nil {
		return newVal
	}

	// Handle existing slice
	switch e := existing.(type) {
	case []any:
		switch n := newVal.(type) {
		case []any:
			return append(e, n...)
		default:
			return append(e, n)
		}
	case []string:
		// Convert to []any first
		anySlice := make([]any, 0, len(e))
		for _, s := range e {
			anySlice = append(anySlice, s)
		}
		switch n := newVal.(type) {
		case []any:
			return append(anySlice, n...)
		case []string:
			for _, s := range n {
				anySlice = append(anySlice, s)
			}
			return anySlice
		default:
			return append(anySlice, n)
		}
	default:
		// Existing is a scalar
		switch n := newVal.(type) {
		case []any:
			return append([]any{e}, n...)
		default:
			return []any{e, n}
		}
	}
}
