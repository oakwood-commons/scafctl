// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
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
// The google/jsonschema-go library formats errors as chains of "validating X:" prefixes
// followed by the actual error message. This function collapses that chain into a dot-path.
func parseSchemaErrorLine(line string) (path, message string) {
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimPrefix(line, "at ")

	// Handle "validating X: validating Y: ... actual message" chains
	if strings.HasPrefix(line, "validating ") {
		return parseValidatingChain(line)
	}

	if idx := strings.Index(line, ": "); idx > 0 {
		candidate := line[:idx]
		// JSON pointer paths start with /
		if strings.HasPrefix(candidate, "/") {
			return jsonPointerToDotPath(candidate), cleanSchemaMessage(line[idx+2:])
		}
	}

	return "", cleanSchemaMessage(line)
}

// validatingPrefix is the prefix used by google/jsonschema-go in error chains.
const validatingPrefix = "validating "

// parseValidatingChain collapses a "validating X: validating Y: ... message" chain
// into a dot-path and final message. It skips schema URLs, $defs type references,
// and structural JSON pointer segments (properties, additionalProperties, items).
func parseValidatingChain(line string) (string, string) {
	var segments []string
	remaining := line

	for strings.HasPrefix(remaining, validatingPrefix) {
		remaining = remaining[len(validatingPrefix):]

		colonIdx := strings.Index(remaining, ": ")
		if colonIdx < 0 {
			// No more colons — the rest is the message
			break
		}

		segment := remaining[:colonIdx]
		remaining = remaining[colonIdx+2:]

		// Skip schema URLs
		if strings.HasPrefix(segment, "http://") || strings.HasPrefix(segment, "https://") {
			continue
		}

		// Handle JSON pointer segments (e.g., /properties/spec, /$defs/SolutionSpec/properties/resolvers)
		if strings.HasPrefix(segment, "/") {
			parts := strings.Split(strings.TrimPrefix(segment, "/"), "/")
			// Extract only property-name segments, skipping structural parts
			skipNext := false
			for _, part := range parts {
				if skipNext {
					// Skip the type name after $defs
					skipNext = false
					continue
				}
				switch part {
				case "$defs":
					skipNext = true // next segment is a type name
				case "properties", "additionalProperties", "items":
					// structural — skip
				default:
					segments = appendDedupe(segments, part)
				}
			}
			continue
		}

		// Skip CamelCase type names (schema type references like SolutionSpec, ResolverResolver)
		if len(segment) > 0 && segment[0] >= 'A' && segment[0] <= 'Z' {
			continue
		}

		// Keep lowercase property names as path segments
		if segment != "" {
			segments = appendDedupe(segments, segment)
		}
	}

	dotPath := strings.Join(segments, ".")
	message := cleanSchemaMessage(remaining)

	return dotPath, message
}

// appendDedupe appends s to the slice only if it differs from the last element.
func appendDedupe(slice []string, s string) []string {
	if len(slice) > 0 && slice[len(slice)-1] == s {
		return slice
	}
	return append(slice, s)
}

// defsPattern matches JSON schema $defs references like:
//
//	$defs/SolutionSpec/properties/resolvers/additionalProperties
//	#/$defs/Resolver/properties/resolve
var defsPattern = regexp.MustCompile(`#?/?\$defs/[A-Za-z0-9_]+(?:/[A-Za-z0-9_\[\]]+)*`)

// additionalPropsPattern matches "unexpected additional properties [\"key1\", \"key2\"]"
// and rewrites it to the cleaner "unknown key \"key1\", \"key2\"" format.
var additionalPropsPattern = regexp.MustCompile(`unexpected additional properties \[([^\]]+)\]`)

// cleanSchemaMessage strips verbose $defs/... references and rewrites common
// schema validation phrases to produce cleaner, user-facing output.
func cleanSchemaMessage(msg string) string {
	cleaned := defsPattern.ReplaceAllStringFunc(msg, func(match string) string {
		// Extract the last meaningful segment as a simplified reference
		parts := strings.Split(match, "/")
		// Find last non-structural segment (skip "properties", "additionalProperties", "items", "$defs")
		for i := len(parts) - 1; i >= 0; i-- {
			p := parts[i]
			switch p {
			case "properties", "additionalProperties", "items", "$defs", "#":
				continue
			default:
				return p
			}
		}
		return match
	})
	// Collapse multiple spaces that may result from replacements
	for strings.Contains(cleaned, "  ") {
		cleaned = strings.ReplaceAll(cleaned, "  ", " ")
	}

	// Rewrite "unexpected additional properties" to "unknown key"
	cleaned = additionalPropsPattern.ReplaceAllString(cleaned, "unknown key $1")

	return strings.TrimSpace(cleaned)
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
