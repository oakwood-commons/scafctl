package action

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Error type constants for categorization.
const (
	// ErrorTypeHTTP indicates an HTTP-related error with a status code.
	ErrorTypeHTTP = "http"

	// ErrorTypeExec indicates a process execution error with an exit code.
	ErrorTypeExec = "exec"

	// ErrorTypeTimeout indicates a timeout or deadline exceeded error.
	ErrorTypeTimeout = "timeout"

	// ErrorTypeValidation indicates a validation error.
	ErrorTypeValidation = "validation"

	// ErrorTypeUnknown indicates an unclassified error.
	ErrorTypeUnknown = "unknown"
)

// ErrorContext provides error information for retryIf CEL expressions.
// It is exposed as __error in the CEL evaluation context.
type ErrorContext struct {
	// Message is the error message string.
	Message string `json:"message"`

	// Type categorizes the error source.
	// Values: "http", "exec", "timeout", "validation", "unknown"
	Type string `json:"type"`

	// StatusCode is the HTTP status code (0 if not an HTTP error).
	StatusCode int `json:"statusCode"`

	// ExitCode is the process exit code (0 if not an exec error).
	ExitCode int `json:"exitCode"`

	// Attempt is the current attempt number (1-based).
	// First attempt is 1, first retry is 2, etc.
	Attempt int `json:"attempt"`

	// MaxAttempts is the maximum attempts configured.
	MaxAttempts int `json:"maxAttempts"`
}

// NewErrorContext creates an ErrorContext from an error and attempt info.
// It inspects the error to determine the type, status code, and exit code.
func NewErrorContext(err error, attempt, maxAttempts int) *ErrorContext {
	ctx := &ErrorContext{
		Message:     err.Error(),
		Type:        ErrorTypeUnknown,
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
	}

	// Detect HTTP errors
	if httpErr := extractHTTPError(err); httpErr != nil {
		ctx.Type = ErrorTypeHTTP
		ctx.StatusCode = httpErr.StatusCode
		return ctx
	}

	// Detect exec errors
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		ctx.Type = ErrorTypeExec
		ctx.ExitCode = exitErr.ExitCode()
		return ctx
	}

	// Detect timeout errors
	if errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(strings.ToLower(err.Error()), "timeout") ||
		strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		ctx.Type = ErrorTypeTimeout
		return ctx
	}

	// Detect validation errors
	if strings.Contains(strings.ToLower(err.Error()), "validation") ||
		strings.Contains(strings.ToLower(err.Error()), "invalid") {
		ctx.Type = ErrorTypeValidation
		return ctx
	}

	return ctx
}

// ToMap converts ErrorContext to a map for CEL evaluation.
func (e *ErrorContext) ToMap() map[string]any {
	return map[string]any{
		"message":     e.Message,
		"type":        e.Type,
		"statusCode":  e.StatusCode,
		"exitCode":    e.ExitCode,
		"attempt":     e.Attempt,
		"maxAttempts": e.MaxAttempts,
	}
}

// HTTPError represents an HTTP error with a status code.
// Providers can return this error type to enable status code-based retry logic.
type HTTPError struct {
	StatusCode int
	Message    string
}

// Error implements the error interface.
func (e *HTTPError) Error() string {
	return e.Message
}

// extractHTTPError attempts to extract an HTTPError from an error chain.
func extractHTTPError(err error) *HTTPError {
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr
	}
	return nil
}
