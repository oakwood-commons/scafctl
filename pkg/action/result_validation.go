// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package action

import (
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// ValidateResult validates a provider result against the JSON Schema.
// Returns nil if validation passes or schema is nil.
func ValidateResult(result any, schema *jsonschema.Schema) error {
	if schema == nil {
		return nil
	}

	// Resolve the schema to enable validation
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return fmt.Errorf("failed to resolve result schema: %w", err)
	}

	// Validate the result against the resolved schema
	return resolved.Validate(result)
}
