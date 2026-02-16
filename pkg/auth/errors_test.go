// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestError_Error(t *testing.T) {
	err := &Error{Handler: "entra", Operation: "login", Cause: errors.New("network error")}
	assert.Equal(t, "auth login failed for entra: network error", err.Error())

	err2 := &Error{Handler: "entra", Operation: "token"}
	assert.Equal(t, "auth token failed for entra", err2.Error())
}

func TestError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := &Error{Handler: "entra", Operation: "login", Cause: cause}
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
}

func TestNewError(t *testing.T) {
	cause := errors.New("test error")
	err := NewError("entra", "login", cause)
	assert.Equal(t, "entra", err.Handler)
	assert.Equal(t, "login", err.Operation)
	assert.Equal(t, cause, err.Cause)
}

func TestErrorHelpers(t *testing.T) {
	assert.True(t, IsNotAuthenticated(ErrNotAuthenticated))
	assert.True(t, IsNotAuthenticated(fmt.Errorf("failed: %w", ErrNotAuthenticated)))
	assert.False(t, IsNotAuthenticated(ErrTokenExpired))

	assert.True(t, IsTokenExpired(ErrTokenExpired))
	assert.True(t, IsTokenExpired(fmt.Errorf("AADSTS70008: %w", ErrTokenExpired)))
	assert.False(t, IsTokenExpired(ErrConsentRequired))

	assert.True(t, IsConsentRequired(ErrConsentRequired))
	assert.True(t, IsConsentRequired(fmt.Errorf("scope required: %w", ErrConsentRequired)))
	assert.False(t, IsConsentRequired(ErrTokenExpired))

	assert.True(t, IsHandlerNotFound(ErrHandlerNotFound))
	assert.True(t, IsTimeout(ErrTimeout))
	assert.True(t, IsUserCancelled(ErrUserCancelled))
}
