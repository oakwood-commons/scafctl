// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"errors"
	"fmt"
)

// Sentinel errors for the auth package.
var (
	ErrNotAuthenticated       = errors.New("not authenticated")
	ErrAuthenticationFailed   = errors.New("authentication failed")
	ErrTokenExpired           = errors.New("credentials expired")
	ErrConsentRequired        = errors.New("consent required: please login with the required scope")
	ErrInvalidScope           = errors.New("invalid scope: scope cannot be empty")
	ErrHandlerNotFound        = errors.New("auth handler not found")
	ErrFlowNotSupported       = errors.New("authentication flow not supported")
	ErrUserCancelled          = errors.New("authentication cancelled by user")
	ErrTimeout                = errors.New("authentication timed out")
	ErrAlreadyAuthenticated   = errors.New("already authenticated")
	ErrGrantInvalid           = errors.New("invalid grant: the refresh token is invalid or has been revoked")
	ErrCapabilityNotSupported = errors.New("capability not supported by this auth handler")
)

// Error wraps authentication errors with additional context.
type Error struct {
	Handler   string `json:"handler" yaml:"handler" doc:"Name of the auth handler" example:"entra" maxLength:"64"`
	Operation string `json:"operation" yaml:"operation" doc:"Operation that failed" example:"login" maxLength:"64"`
	Cause     error  `json:"-" yaml:"-"`
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

// IsConsentRequired returns true if the error indicates consent is required for the requested scope.
func IsConsentRequired(err error) bool {
	return errors.Is(err, ErrConsentRequired)
}

// IsUserCancelled returns true if the error indicates the user cancelled.
func IsUserCancelled(err error) bool {
	return errors.Is(err, ErrUserCancelled)
}

// IsCapabilityNotSupported returns true if the error indicates a capability is not supported.
func IsCapabilityNotSupported(err error) bool {
	return errors.Is(err, ErrCapabilityNotSupported)
}

// IsGrantInvalid returns true if the error indicates the grant (refresh token) is invalid or revoked.
func IsGrantInvalid(err error) bool {
	return errors.Is(err, ErrGrantInvalid)
}

// IsAlreadyAuthenticated returns true if the error indicates the user is already authenticated.
func IsAlreadyAuthenticated(err error) bool {
	return errors.Is(err, ErrAlreadyAuthenticated)
}

// IsFlowNotSupported returns true if the error indicates the flow is not supported.
func IsFlowNotSupported(err error) bool {
	return errors.Is(err, ErrFlowNotSupported)
}

// IsAuthenticationFailed returns true if the error indicates authentication failed.
func IsAuthenticationFailed(err error) bool {
	return errors.Is(err, ErrAuthenticationFailed)
}

// IsInvalidScope returns true if the error indicates the scope is invalid.
func IsInvalidScope(err error) bool {
	return errors.Is(err, ErrInvalidScope)
}
