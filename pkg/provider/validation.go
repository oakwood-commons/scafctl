// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
)

// SchemaValidator provides validation for provider inputs and outputs against JSON Schema definitions.
type SchemaValidator struct{}

// NewSchemaValidator creates a new schema validator.
func NewSchemaValidator() *SchemaValidator {
	return &SchemaValidator{}
}

// ValidationError represents a single field validation error with contextual information.
type ValidationError struct {
	Field      string
	Value      any
	Constraint string
	Message    string
	Actual     string
	Expected   string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Actual != "" && e.Expected != "" {
		return fmt.Sprintf("%s: %s (actual: %s, expected: %s)", e.Field, e.Message, e.Actual, e.Expected)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []*ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	if len(e) == 1 {
		return e[0].Error()
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "validation failed with %d errors:\n", len(e))
	for _, err := range e {
		sb.WriteString("  - ")
		sb.WriteString(err.Error())
		sb.WriteString("\n")
	}
	return sb.String()
}

// ValidateInputs validates provider inputs against the JSON Schema definition.
func (sv *SchemaValidator) ValidateInputs(inputs map[string]any, schema *jsonschema.Schema) error {
	if schema == nil {
		return nil
	}
	return sv.validateAgainstSchema(inputs, schema, "inputs")
}

// ValidateOutput validates provider output data against the JSON Schema definition.
func (sv *SchemaValidator) ValidateOutput(output any, schema *jsonschema.Schema) error {
	if schema == nil {
		return nil
	}
	var outputMap map[string]any
	switch v := output.(type) {
	case map[string]any:
		outputMap = v
	case nil:
		outputMap = make(map[string]any)
	default:
		outputMap = map[string]any{"value": output}
	}
	return sv.validateAgainstSchema(outputMap, schema, "output")
}

func (sv *SchemaValidator) validateAgainstSchema(data map[string]any, schema *jsonschema.Schema, contextPath string) error {
	resolved, err := schema.Resolve(nil)
	if err != nil {
		return fmt.Errorf("failed to resolve %s schema: %w", contextPath, err)
	}

	if err := resolved.Validate(data); err != nil {
		// Convert the jsonschema validation error into our ValidationErrors format
		return sv.convertValidationError(err, contextPath)
	}

	return nil
}

// convertValidationError converts a jsonschema validation error into our ValidationErrors type.
func (sv *SchemaValidator) convertValidationError(err error, contextPath string) error {
	if err == nil {
		return nil
	}

	// The jsonschema library returns structured errors. We wrap them to preserve UX compatibility.
	errMsg := err.Error()
	lines := strings.Split(errMsg, "\n")

	var errors ValidationErrors
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		errors = append(errors, &ValidationError{
			Field:   contextPath,
			Message: line,
		})
	}

	if len(errors) == 0 {
		return &ValidationError{
			Field:   contextPath,
			Message: errMsg,
		}
	}

	return errors
}
