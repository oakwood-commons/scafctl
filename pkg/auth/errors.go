// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"fmt"
)

// Sentinel errors for the auth package.
var (
	ErrNotAuthenticated     = errors.New("not authenticated: please run 'scafctl auth login entra'")
	ErrAuthenticationFailed = errors.New("authentication failed")
	ErrTokenExpired         = errors.New("credentials expired: please run 'scafctl auth login entra'")
	ErrInvalidScope         = errors.New("invalid scope: scope cannot be empty")
	ErrHandlerNotFound      = errors.New("auth handler not found")
	ErrFlowNotSupported     = errors.New("authentication flow not supported")
	ErrUserCancelled        = errors.New("authentication cancelled by user")
	ErrTimeout              = errors.New("authentication timed out")
	ErrAlreadyAuthenticated = errors.New("already authenticated")
)

// Error wraps authentication errors with additional context.
type Error struct {
	Handler   string
	Operation string
	Cause     error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("auth %s failed for %s: %v", e.Operation, e.Handler, e.Cause)
	}
	return fmt.Sprintf("auth %s failed for %s", e.Operation, e.Handler)
}

// Unwrap returns the underlying cause.
func (e *Error) Unwrap() error {
	return e.Cause
}

// NewError creates a new Error.
func NewError(handler, operation string, cause error) *Error {
	return &Error{Handler: handler, Operation: operation, Cause: cause}
}

// IsNotAuthenticated returns true if the error indicates the user is not authenticated.
func IsNotAuthenticated(err error) bool {
	return errors.Is(err, ErrNotAuthenticated)
}

// IsTokenExpired returns true if the error indicates the token has expired.
func IsTokenExpired(err error) bool {
	return errors.Is(err, ErrTokenExpired)
}

// IsHandlerNotFound returns true if the error indicates the handler was not found.
func IsHandlerNotFound(err error) bool {
	return errors.Is(err, ErrHandlerNotFound)
}

// IsTimeout returns true if the error indicates a timeout occurred.
func IsTimeout(err error) bool {
	return errors.Is(err, ErrTimeout)
}

// IsUserCancelled returns true if the error indicates the user cancelled.
func IsUserCancelled(err error) bool {
	return errors.Is(err, ErrUserCancelled)
}
