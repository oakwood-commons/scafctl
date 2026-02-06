package secrets

import (
	"errors"
	"fmt"
)

// Sentinel errors for the secrets package.
var (
	// ErrNotFound is returned when a secret does not exist.
	ErrNotFound = errors.New("secret not found")

	// ErrInvalidName is returned when a secret name is invalid.
	ErrInvalidName = errors.New("invalid secret name")

	// ErrCorrupted is returned when a secret file is corrupted and cannot be decrypted.
	ErrCorrupted = errors.New("secret is corrupted")

	// ErrKeyringAccess is returned when the OS keyring cannot be accessed.
	ErrKeyringAccess = errors.New("cannot access keyring")
)

// KeyringError wraps a keyring access error with additional context.
type KeyringError struct {
	Operation string `json:"operation" yaml:"operation" doc:"The keyring operation that failed" example:"get"`
	Cause     error  `json:"-" yaml:"-" doc:"The underlying error"`
}

// Error implements the error interface.
func (e *KeyringError) Error() string {
	return fmt.Sprintf("keyring %s failed: %v", e.Operation, e.Cause)
}

// Unwrap returns the underlying cause for use with errors.Is and errors.As.
func (e *KeyringError) Unwrap() error {
	return e.Cause
}

// Is reports whether the target error is ErrKeyringAccess.
func (e *KeyringError) Is(target error) bool {
	return target == ErrKeyringAccess
}

// NewKeyringError creates a new KeyringError with the given operation and cause.
func NewKeyringError(operation string, cause error) *KeyringError {
	return &KeyringError{
		Operation: operation,
		Cause:     cause,
	}
}

// CorruptedSecretError provides details about a corrupted secret.
type CorruptedSecretError struct {
	Name   string `json:"name" yaml:"name" doc:"The name of the corrupted secret" example:"my-secret"`
	Reason string `json:"reason" yaml:"reason" doc:"The reason the secret is corrupted" example:"invalid version byte"`
	Cause  error  `json:"-" yaml:"-" doc:"The underlying error"`
}

// Error implements the error interface.
func (e *CorruptedSecretError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("secret %q is corrupted (%s): %v", e.Name, e.Reason, e.Cause)
	}
	return fmt.Sprintf("secret %q is corrupted: %s", e.Name, e.Reason)
}

// Unwrap returns the underlying cause for use with errors.Is and errors.As.
func (e *CorruptedSecretError) Unwrap() error {
	return e.Cause
}

// Is reports whether the target error is ErrCorrupted.
func (e *CorruptedSecretError) Is(target error) bool {
	return target == ErrCorrupted
}

// NewCorruptedSecretError creates a new CorruptedSecretError with the given name, reason, and cause.
func NewCorruptedSecretError(name, reason string, cause error) *CorruptedSecretError {
	return &CorruptedSecretError{
		Name:   name,
		Reason: reason,
		Cause:  cause,
	}
}

// InvalidNameError provides details about an invalid secret name.
type InvalidNameError struct {
	Name   string `json:"name" yaml:"name" doc:"The invalid secret name" example:".invalid-name"`
	Reason string `json:"reason" yaml:"reason" doc:"The reason the name is invalid" example:"cannot start with '.'"`
}

// Error implements the error interface.
func (e *InvalidNameError) Error() string {
	return fmt.Sprintf("invalid secret name %q: %s", e.Name, e.Reason)
}

// Is reports whether the target error is ErrInvalidName.
func (e *InvalidNameError) Is(target error) bool {
	return target == ErrInvalidName
}

// NewInvalidNameError creates a new InvalidNameError with the given name and reason.
func NewInvalidNameError(name, reason string) *InvalidNameError {
	return &InvalidNameError{
		Name:   name,
		Reason: reason,
	}
}
