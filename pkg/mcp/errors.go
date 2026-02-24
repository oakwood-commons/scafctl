// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// ToolError represents a structured error response from an MCP tool.
// It provides machine-readable error codes, actionable suggestions,
// and references to related tools that may help resolve the issue.
type ToolError struct {
	// Code is a machine-readable error code (e.g., "INVALID_INPUT", "NOT_FOUND").
	Code string `json:"code"`
	// Message is a human-readable error description.
	Message string `json:"message"`
	// Field is the input field that caused the error (optional).
	Field string `json:"field,omitempty"`
	// Suggestion is an actionable hint for fixing the error (optional).
	Suggestion string `json:"suggestion,omitempty"`
	// Related lists tool names that may help resolve the issue (optional).
	Related []string `json:"related,omitempty"`
}

// ErrorOption configures a ToolError.
type ErrorOption func(*ToolError)

// WithField sets the field that caused the error.
func WithField(field string) ErrorOption {
	return func(e *ToolError) {
		e.Field = field
	}
}

// WithSuggestion sets an actionable fix suggestion.
func WithSuggestion(suggestion string) ErrorOption {
	return func(e *ToolError) {
		e.Suggestion = suggestion
	}
}

// WithRelatedTools sets related tools that may help.
func WithRelatedTools(tools ...string) ErrorOption {
	return func(e *ToolError) {
		e.Related = tools
	}
}

// newStructuredError creates a structured error result with machine-readable context.
// Use this instead of mcp.NewToolResultError for errors that benefit from
// additional context (e.g., field-specific validation, fix suggestions).
func newStructuredError(code, message string, opts ...ErrorOption) *mcp.CallToolResult {
	te := &ToolError{
		Code:    code,
		Message: message,
	}
	for _, opt := range opts {
		opt(te)
	}

	data, err := json.Marshal(te)
	if err != nil {
		// Fall back to plain text error
		return mcp.NewToolResultError(message)
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.TextContent{
				Type: "text",
				Text: string(data),
			},
		},
		IsError: true,
	}
}

// Common error codes for MCP tools.
const (
	ErrCodeInvalidInput    = "INVALID_INPUT"
	ErrCodeNotFound        = "NOT_FOUND"
	ErrCodeValidationError = "VALIDATION_ERROR"
	ErrCodeLoadFailed      = "LOAD_FAILED"
	ErrCodeExecFailed      = "EXECUTION_FAILED"
	ErrCodeAuthRequired    = "AUTH_REQUIRED"
	ErrCodeConfigError     = "CONFIG_ERROR"
)
