// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/oakwood-commons/scafctl/pkg/logger"
)

// HandleError logs the error and returns a Huma error with the given status code.
// For 5xx status codes the raw error is never included in the response body to
// prevent leaking internal implementation details, stack traces, or file paths.
func HandleError(ctx context.Context, err error, operation string, statusCode int, userMessage string) error {
	lgr := logger.FromContext(ctx)
	if err != nil {
		lgr.Error(err, "API error", "operation", operation, "statusCode", statusCode)
	} else {
		lgr.Info("API error with nil error", "operation", operation, "statusCode", statusCode)
	}
	// Server-side errors: log only, never expose raw error to caller.
	if statusCode >= http.StatusInternalServerError {
		return huma.NewError(statusCode, userMessage)
	}
	return huma.NewError(statusCode, userMessage, err)
}

// HandleValidationError returns a 422 Unprocessable Entity for validation failures.
func HandleValidationError(_ context.Context, fieldName, message string) error {
	detail := &huma.ErrorDetail{
		Message:  message,
		Location: fieldName,
		Value:    fieldName,
	}
	return huma.NewError(http.StatusUnprocessableEntity, fmt.Sprintf("validation failed: %s", message), detail)
}

// NotFoundError returns a 404 Not Found error.
func NotFoundError(resource, identifier string) error {
	return huma.NewError(http.StatusNotFound, fmt.Sprintf("%s %q not found", resource, identifier))
}

// InternalError returns a 500 Internal Server Error.
func InternalError(ctx context.Context, err error, operation string) error {
	return HandleError(ctx, err, operation, http.StatusInternalServerError, "internal server error")
}
