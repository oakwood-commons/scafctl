// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandleError(t *testing.T) {
	err := HandleError(context.Background(), assert.AnError, "test-op", http.StatusBadRequest, "bad request")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad request")
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
