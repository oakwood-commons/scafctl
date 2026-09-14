// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandleError(t *testing.T) {
	err := HandleError(context.Background(), assert.AnError, "test-op", http.StatusBadRequest, "bad request")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad request")
}

// errSensitive stands in for an internal error whose text must never reach a
// caller -- a stack trace, a filesystem path, a backend hostname. The sentinel
// string is deliberately distinctive so a leak cannot hide inside an unrelated
// substring match.
var errSensitive = errors.New("/srv/internal/secret-path.go:42 backend=db-prod-01 leaked")

// renderedBody returns what the caller actually receives over the wire.
//
// Asserting on err.Error() would be worthless here: huma renders only the
// caller-facing detail there, so the raw error is absent from that string even
// when it IS attached and served. The leak lives in the serialized body's
// "errors" array, so that is the surface the redaction contract must be pinned
// against.
func renderedBody(t *testing.T, err error) string {
	t.Helper()
	b, marshalErr := json.Marshal(err)
	assert.NoError(t, marshalErr)
	return string(b)
}

// TestHandleError_RedactsRawErrorByStatus pins the leak-prevention contract of
// HandleError: the raw error is suppressed for every 4xx and 5xx response and
// preserved outside that range, where it is a debugging aid rather than a leak.
// Without this the redaction can regress silently -- the pre-existing test only
// asserted that the user-facing message is present, which stays true even if
// the raw error is served alongside it.
func TestHandleError_RedactsRawErrorByStatus(t *testing.T) {
	const userMessage = "request rejected"

	tests := []struct {
		name         string
		statusCode   int
		wantRedacted bool
	}{
		{name: "399 below the redaction floor", statusCode: 399, wantRedacted: false},
		{name: "400 lower boundary of 4xx", statusCode: http.StatusBadRequest, wantRedacted: true},
		{name: "401 unauthorized", statusCode: http.StatusUnauthorized, wantRedacted: true},
		{name: "403 forbidden", statusCode: http.StatusForbidden, wantRedacted: true},
		{name: "404 not found", statusCode: http.StatusNotFound, wantRedacted: true},
		{name: "422 unprocessable entity", statusCode: http.StatusUnprocessableEntity, wantRedacted: true},
		{name: "499 upper boundary of 4xx", statusCode: 499, wantRedacted: true},
		{name: "500 lower boundary of 5xx", statusCode: http.StatusInternalServerError, wantRedacted: true},
		{name: "503 service unavailable", statusCode: http.StatusServiceUnavailable, wantRedacted: true},
		{name: "599 upper boundary of 5xx", statusCode: 599, wantRedacted: true},
		{name: "600 above the redaction ceiling", statusCode: 600, wantRedacted: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := HandleError(context.Background(), errSensitive, "test-op", tt.statusCode, userMessage)
			assert.Error(t, err)

			body := renderedBody(t, err)
			assert.Contains(t, body, userMessage, "caller-facing message must always survive")

			if tt.wantRedacted {
				assert.NotContains(t, body, errSensitive.Error(),
					"raw error must never reach the caller for status %d", tt.statusCode)
			} else {
				assert.Contains(t, body, errSensitive.Error(),
					"raw error is retained outside the 4xx-5xx range for status %d", tt.statusCode)
			}
		})
	}
}

// TestHandleError_NilErrorIsSafe covers the branch that logs "API error with nil
// error": a nil error must not panic and must still produce the caller message.
func TestHandleError_NilErrorIsSafe(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusInternalServerError} {
		err := HandleError(context.Background(), nil, "test-op", status, "request rejected")
		assert.Error(t, err)
		assert.Contains(t, renderedBody(t, err), "request rejected")
	}
}

func TestHandleValidationError(t *testing.T) {
	err := HandleValidationError(context.Background(), "field", "must be set")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestNotFoundError(t *testing.T) {
	err := NotFoundError("provider", "http")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), `"http" not found`)
}

func TestInternalError(t *testing.T) {
	err := InternalError(context.Background(), assert.AnError, "test-op")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "internal server error")
}
