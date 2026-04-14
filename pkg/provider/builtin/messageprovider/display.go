// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package messageprovider

import (
	"encoding/json"
	"fmt"
	"strings"
)

// displayKeyMapping maps solution YAML display keys to x-kvx-* vendor extension keys
// used by kvx's ParseSchemaWithDisplay.
var displayKeyMapping = map[string]string{
	"list":            "x-kvx-list",
	"detail":          "x-kvx-detail",
	"collectionTitle": "x-kvx-collectionTitle",
	"icon":            "x-kvx-icon",
	"version":         "x-kvx-version",
}

// displaySchemaFromMap converts a display config map from solution YAML into JSON
// with x-kvx-* vendor extensions suitable for kvx.WithDisplaySchemaJSON().
//
// Input keys map to x-kvx extensions:
//
//	display.list           -> x-kvx-list
//	display.detail         -> x-kvx-detail
//	display.collectionTitle -> x-kvx-collectionTitle
//	display.icon           -> x-kvx-icon
func displaySchemaFromMap(display map[string]any) ([]byte, error) {
	if len(display) == 0 {
		return nil, fmt.Errorf("display config must not be empty")
	}

	mapped := make(map[string]any, len(display))
	for key, val := range display {
		if xkvxKey, ok := displayKeyMapping[key]; ok {
			mapped[xkvxKey] = val
		} else if strings.HasPrefix(key, "x-kvx-") {
			// Already prefixed -- pass through unchanged.
			mapped[key] = val
		} else {
			// Pass through any unknown keys with the x-kvx- prefix
			// to support future kvx extensions without code changes.
			mapped["x-kvx-"+key] = val
		}
	}

	data, err := json.Marshal(mapped)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal display schema: %w", err)
	}
	return data, nil
}

// columnHintsToJSON converts a columnHints map from solution YAML into a JSON Schema
// document suitable for kvx.WithSchemaJSON(). The input shape is:
//
//	properties:
//	  fieldName:
//	    x-kvx-header: "Display Name"
//	    x-kvx-maxWidth: 30
//	    x-kvx-visible: false
//
// The output is a JSON Schema with those x-kvx-* extensions preserved, which
// tui.ParseSchema uses to derive column hints.
func columnHintsToJSON(hints map[string]any) ([]byte, error) {
	if len(hints) == 0 {
		return nil, fmt.Errorf("column hints must not be empty")
	}

	data, err := json.Marshal(hints)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal column hints: %w", err)
	}
	return data, nil
}
