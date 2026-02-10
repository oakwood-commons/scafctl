// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// ConfigSchemaID is the JSON Schema ID for the config file.
const ConfigSchemaID = "https://scafctl.dev/schemas/v1/config.json"

// GenerateConfigSchema generates a JSON Schema for the scafctl config file.
// It uses reflection to analyze the config.Config struct and its tags.
func GenerateConfigSchema() ([]byte, error) {
	schema, err := jsonschema.For[config.Config](nil)
	if err != nil {
		return nil, err
	}

	// Add metadata
	schema.ID = ConfigSchemaID
	schema.Title = "scafctl Configuration"
	schema.Description = "Configuration file for scafctl CLI (version 1)"

	return json.MarshalIndent(schema, "", "  ")
}

// GenerateConfigSchemaCompact generates a JSON Schema without indentation.
func GenerateConfigSchemaCompact() ([]byte, error) {
	schema, err := jsonschema.For[config.Config](nil)
	if err != nil {
		return nil, err
	}

	// Add metadata
	schema.ID = ConfigSchemaID
	schema.Title = "scafctl Configuration"
	schema.Description = "Configuration file for scafctl CLI (version 1)"

	return json.Marshal(schema)
}
