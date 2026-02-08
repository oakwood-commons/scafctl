// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package exitcode provides centralized exit codes for CLI commands.
// All commands should import this package to ensure consistent exit codes.
package exitcode

import (
	"errors"
	"fmt"
)

// Standard exit codes for CLI commands.
// These follow common Unix conventions where possible.
const (
	// Success indicates successful execution.
	Success = 0

	// GeneralError indicates an unspecified error occurred.
	GeneralError = 1

	// ValidationFailed indicates input validation failed.
	ValidationFailed = 2

	// InvalidInput indicates invalid solution structure (e.g., circular dependency).
	InvalidInput = 3

	// FileNotFound indicates a file was not found or could not be parsed.
	FileNotFound = 4

	// RenderFailed indicates rendering/transformation failed.
	RenderFailed = 5

	// ActionFailed indicates action/workflow execution failed.
	ActionFailed = 6

	// ConfigError indicates a configuration error.
	ConfigError = 7

	// CatalogError indicates a catalog operation failed.
	CatalogError = 8

	// TimeoutError indicates an operation timed out.
	TimeoutError = 9

	// PermissionDenied indicates insufficient permissions.
	PermissionDenied = 10
)

// Description returns a human-readable description of an exit code.
func Description(code int) string {
	switch code {
	case Success:
		return "success"
	case GeneralError:
		return "general error"
	case ValidationFailed:
		return "validation failed"
	case InvalidInput:
		return "invalid input"
	case FileNotFound:
		return "file not found"
	case RenderFailed:
		return "render failed"
	case ActionFailed:
		return "action failed"
	case ConfigError:
		return "configuration error"
	case CatalogError:
		return "catalog error"
	case TimeoutError:
		return "timeout"
	case PermissionDenied:
		return "permission denied"
	default:
		return "unknown error"
	}
}

// ExitError wraps an error with an exit code.
// Commands can return this to indicate a specific exit code should be used.
// The error message should already be printed by the command before returning.
type ExitError struct {
	Err  error
	Code int
}

// Error implements the error interface.
func (e *ExitError) Error() string {
	if e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *ExitError) Unwrap() error {
	return e.Err
}

// WithCode wraps an error with an exit code.
// Use this when a command needs to return a specific exit code.
// The command should print its own error message before calling this.
func WithCode(err error, code int) *ExitError {
	return &ExitError{Err: err, Code: code}
}

// AsError creates an ExitError with GeneralError (1) exit code.
// This is a convenience for commands that just need to signal failure
// after printing their own error message.
func AsError(err error) *ExitError {
	return WithCode(err, GeneralError)
}

// Errorf creates an ExitError with GeneralError (1) exit code from a formatted string.
func Errorf(format string, args ...any) *ExitError {
	return WithCode(fmt.Errorf(format, args...), GeneralError)
}

// GetCode extracts the exit code from an error.
// Returns GeneralError (1) if the error is not an ExitError.
// Returns Success (0) if err is nil.
func GetCode(err error) int {
	if err == nil {
		return Success
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	return GeneralError
}
