// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// Violation represents a single validation error found when validating
// data against the solution JSON schema.
type Violation struct {
	// Path is the dot-notation location of the violation (e.g., "spec.resolvers.env.resolve.with[0]").
	Path string `json:"path" yaml:"path"`
	// Message is a human-readable description of the violation.
	Message string `json:"message" yaml:"message"`
}

// ValidateSolutionAgainstSchema validates raw data (parsed from YAML/JSON as map[string]any)
// against the generated solution JSON schema. It returns a list of violations, or an error
// if the schema itself could not be compiled.
//
// The data parameter should be the result of unmarshalling YAML into a plain any
// (not into a typed Solution struct), so that unknown fields are preserved for detection.
func ValidateSolutionAgainstSchema(data any) ([]Violation, error) {
	schemaBytes, err := GenerateSolutionSchema()
	if err != nil {
		return nil, fmt.Errorf("generating solution schema: %w", err)
	}

	var schema jsonschema.Schema
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		return nil, fmt.Errorf("unmarshalling solution schema: %w", err)
	}

	resolved, err := schema.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("resolving solution schema: %w", err)
	}

	if err := resolved.Validate(data); err != nil {
		return parseValidationErrors(err), nil
	}

	return nil, nil
}

// parseValidationErrors converts a jsonschema validation error into structured violations.
// The google/jsonschema-go library returns multi-line error strings.
func parseValidationErrors(err error) []Violation {
	if err == nil {
		return nil
	}

	errMsg := err.Error()
	lines := strings.Split(errMsg, "\n")

	var violations []Violation
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		path, message := parseSchemaErrorLine(line)
		violations = append(violations, Violation{
			Path:    path,
			Message: message,
		})
	}

	if len(violations) == 0 {
		violations = append(violations, Violation{
			Path:    "",
			Message: errMsg,
		})
	}

	return violations
}

// parseSchemaErrorLine attempts to extract a path and message from a validation error line.
// The google/jsonschema-go library formats errors with path info in various ways.
func parseSchemaErrorLine(line string) (path, message string) {
	// Try to extract path from common patterns like:
	//   "/path/to/field: error message"
	//   "at /path/to/field: error message"
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "at ")

	if idx := strings.Index(line, ": "); idx > 0 {
		candidate := line[:idx]
		// JSON pointer paths start with /
		if strings.HasPrefix(candidate, "/") {
			return jsonPointerToDotPath(candidate), line[idx+2:]
		}
	}

	return "", line
}

// jsonPointerToDotPath converts a JSON pointer (e.g., "/spec/resolvers/env") to
// dot-notation (e.g., "spec.resolvers.env"), which matches the lint Finding.Location format.
func jsonPointerToDotPath(ptr string) string {
	ptr = strings.TrimPrefix(ptr, "/")
	if ptr == "" {
		return ""
	}

	parts := strings.Split(ptr, "/")
	var result strings.Builder
	for i, part := range parts {
		// Unescape JSON pointer encoding
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")

		if i > 0 {
			// If part is numeric, use array bracket notation
			if isNumeric(part) {
				result.WriteString("[")
				result.WriteString(part)
				result.WriteString("]")
				continue
			}
			result.WriteString(".")
		}
		result.WriteString(part)
	}
	return result.String()
}

// isNumeric returns true if s consists entirely of digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
