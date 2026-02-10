// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package exitcode

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExitCodes(t *testing.T) {
	t.Parallel()

	// Verify exit codes have expected values (document contract)
	assert.Equal(t, 0, Success)
	assert.Equal(t, 1, GeneralError)
	assert.Equal(t, 2, ValidationFailed)
	assert.Equal(t, 3, InvalidInput)
	assert.Equal(t, 4, FileNotFound)
	assert.Equal(t, 5, RenderFailed)
	assert.Equal(t, 6, ActionFailed)
	assert.Equal(t, 7, ConfigError)
	assert.Equal(t, 8, CatalogError)
	assert.Equal(t, 9, TimeoutError)
	assert.Equal(t, 10, PermissionDenied)
}

func TestDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		code     int
		expected string
	}{
		{Success, "success"},
		{GeneralError, "general error"},
		{ValidationFailed, "validation failed"},
		{InvalidInput, "invalid input"},
		{FileNotFound, "file not found"},
		{RenderFailed, "render failed"},
		{ActionFailed, "action failed"},
		{ConfigError, "configuration error"},
		{CatalogError, "catalog error"},
		{TimeoutError, "timeout"},
		{PermissionDenied, "permission denied"},
		{999, "unknown error"},
		{-1, "unknown error"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, Description(tt.code))
	}
}

func TestExitError(t *testing.T) {
	t.Run("Error returns underlying error message", func(t *testing.T) {
		err := WithCode(errors.New("something failed"), GeneralError)
		assert.Equal(t, "something failed", err.Error())
	})

	t.Run("Error returns empty string for nil error", func(t *testing.T) {
		err := &ExitError{Err: nil, Code: GeneralError}
		assert.Equal(t, "", err.Error())
	})

	t.Run("Unwrap returns underlying error", func(t *testing.T) {
		underlying := errors.New("underlying")
		err := WithCode(underlying, GeneralError)
		assert.Equal(t, underlying, err.Unwrap())
	})

	t.Run("errors.Is works with wrapped error", func(t *testing.T) {
		sentinel := errors.New("sentinel")
		err := WithCode(fmt.Errorf("wrapped: %w", sentinel), GeneralError)
		assert.True(t, errors.Is(err, sentinel))
	})
}

func TestWithCode(t *testing.T) {
	t.Run("creates ExitError with specified code", func(t *testing.T) {
		err := WithCode(errors.New("test"), ValidationFailed)
		assert.Equal(t, ValidationFailed, err.Code)
		assert.Equal(t, "test", err.Error())
	})
}

func TestAsError(t *testing.T) {
	t.Run("creates ExitError with GeneralError code", func(t *testing.T) {
		err := AsError(errors.New("test"))
		assert.Equal(t, GeneralError, err.Code)
	})
}

func TestErrorf(t *testing.T) {
	t.Run("creates formatted ExitError with GeneralError code", func(t *testing.T) {
		err := Errorf("failed: %s", "reason")
		assert.Equal(t, GeneralError, err.Code)
		assert.Equal(t, "failed: reason", err.Error())
	})
}

func TestGetCode(t *testing.T) {
	t.Run("returns Success for nil", func(t *testing.T) {
		assert.Equal(t, Success, GetCode(nil))
	})

	t.Run("returns code from ExitError", func(t *testing.T) {
		err := WithCode(errors.New("test"), ValidationFailed)
		assert.Equal(t, ValidationFailed, GetCode(err))
	})

	t.Run("returns GeneralError for plain error", func(t *testing.T) {
		err := errors.New("plain error")
		assert.Equal(t, GeneralError, GetCode(err))
	})

	t.Run("extracts code from wrapped ExitError", func(t *testing.T) {
		exitErr := WithCode(errors.New("test"), ActionFailed)
		wrapped := fmt.Errorf("wrapper: %w", exitErr)
		assert.Equal(t, ActionFailed, GetCode(wrapped))
	})
}
