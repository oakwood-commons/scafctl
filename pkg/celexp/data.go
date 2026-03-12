// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package celexp

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// BuildDataContext creates a data context from inline data (JSON string) or a
// file path (JSON/YAML). At most one of data or file may be non-empty. Returns
// nil when both are empty.
func BuildDataContext(data, file string) (any, error) {
	if data != "" && file != "" {
		return nil, fmt.Errorf("cannot use both --data and --file")
	}

	if data == "" && file == "" {
		return nil, nil
	}

	if data != "" {
		return unmarshalJSONOrYAML([]byte(data))
	}

	return LoadDataFile(file)
}

// LoadDataFile reads a file and unmarshals it as JSON or YAML.
func LoadDataFile(path string) (any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", path, err)
	}
	return unmarshalJSONOrYAML(raw)
}

// ParseVars converts key=value string pairs into a map. Values that are valid
// JSON are unmarshaled; otherwise they are kept as plain strings.
func ParseVars(vars []string) (map[string]any, error) {
	if len(vars) == 0 {
		return nil, nil
	}

	result := make(map[string]any, len(vars))
	for _, v := range vars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid variable format %q; expected key=value", v)
		}
		key := strings.TrimSpace(parts[0])
		value := parts[1]

		// Try to parse as JSON for complex values
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			result[key] = parsed
		} else {
			result[key] = value
		}
	}

	return result, nil
}

// unmarshalJSONOrYAML tries JSON first, then YAML. Returns the parsed value.
func unmarshalJSONOrYAML(raw []byte) (any, error) {
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		if err2 := yaml.Unmarshal(raw, &result); err2 != nil {
			return nil, fmt.Errorf("data is not valid JSON or YAML: %w", err)
		}
	}
	return result, nil
}
