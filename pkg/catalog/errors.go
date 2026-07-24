// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"errors"
	"fmt"
)

// ErrArtifactNotFound is returned when an artifact cannot be found.
var ErrArtifactNotFound = errors.New("artifact not found")

// ErrArtifactExists is returned when storing an artifact that already exists.
var ErrArtifactExists = errors.New("artifact already exists")

// ErrInvalidReference is returned when a reference is malformed.
var ErrInvalidReference = errors.New("invalid reference")

// ArtifactNotFoundError provides details about a missing artifact.
type ArtifactNotFoundError struct {
	Reference Reference
	Catalog   string // Optional: which catalog was checked
}

// Error implements the error interface.
func (e *ArtifactNotFoundError) Error() string {
	if e.Catalog != "" {
		return fmt.Sprintf("artifact %q not found in catalog %q", e.Reference.String(), e.Catalog)
	}
	return fmt.Sprintf("artifact %q not found", e.Reference.String())
}

// Unwrap returns the base error for errors.Is support.
func (e *ArtifactNotFoundError) Unwrap() error {
	return ErrArtifactNotFound
}

// ArtifactExistsError provides details about a duplicate artifact.
type ArtifactExistsError struct {
	Reference Reference
	Catalog   string
}

// Error implements the error interface.
func (e *ArtifactExistsError) Error() string {
	return fmt.Sprintf("artifact %q already exists in catalog %q (use --force to overwrite)", e.Reference.String(), e.Catalog)
}

// Unwrap returns the base error for errors.Is support.
func (e *ArtifactExistsError) Unwrap() error {
	return ErrArtifactExists
}

// InvalidReferenceError provides details about an invalid reference.
type InvalidReferenceError struct {
	Input   string
	Message string
}

// Error implements the error interface.
func (e *InvalidReferenceError) Error() string {
	return fmt.Sprintf("invalid reference %q: %s", e.Input, e.Message)
}

// Unwrap returns the base error for errors.Is support.
func (e *InvalidReferenceError) Unwrap() error {
	return ErrInvalidReference
}

// IsNotFound returns true if the error indicates an artifact was not found.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrArtifactNotFound)
}

// IsArtifactNotFoundError is an alias for IsNotFound.
func IsArtifactNotFoundError(err error) bool {
	return IsNotFound(err)
}

// ErrPlatformNotFound is returned when no matching platform is found in an image index.
var ErrPlatformNotFound = errors.New("platform not found")

// PlatformNotFoundError provides details about a missing platform in an image index.
type PlatformNotFoundError struct {
	Platform  string   // The requested platform (e.g. "linux/amd64")
	Available []string // Available platforms in the index
}

// Error implements the error interface.
func (e *PlatformNotFoundError) Error() string {
	if len(e.Available) > 0 {
		return fmt.Sprintf("platform %q not found in image index (available: %v)", e.Platform, e.Available)
	}
	return fmt.Sprintf("platform %q not found in image index", e.Platform)
}

// Unwrap returns the base error for errors.Is support.
func (e *PlatformNotFoundError) Unwrap() error {
	return ErrPlatformNotFound
}

// IsPlatformNotFound returns true if the error indicates a missing platform.
func IsPlatformNotFound(err error) bool {
	return errors.Is(err, ErrPlatformNotFound)
}

// IsExists returns true if the error indicates an artifact already exists.
func IsExists(err error) bool {
	return errors.Is(err, ErrArtifactExists)
}

// IsInvalidReference returns true if the error indicates an invalid reference.
func IsInvalidReference(err error) bool {
	return errors.Is(err, ErrInvalidReference)
}

// ErrEnumerationNotSupported is returned when a registry does not support
// the _catalog endpoint for repository enumeration.
var ErrEnumerationNotSupported = errors.New("registry does not support repository enumeration")

// IsEnumerationNotSupported returns true if the error indicates the registry
// does not support listing all repositories.
func IsEnumerationNotSupported(err error) bool {
	return errors.Is(err, ErrEnumerationNotSupported)
}

// AuthDegradedError indicates that stored credentials for a registry were
// rejected and the catalog silently fell back to anonymous access. The listing
// it produced is therefore degraded/incomplete rather than authoritatively
// empty. The message is data-only (registry and credential source); callers
// (e.g. the CLI) are responsible for adding a binary-name-specific fix hint,
// since the binary name is context-driven.
type AuthDegradedError struct {
	// Registry is the registry host whose credentials were rejected.
	Registry string
	// Handler is the auth handler that provided the rejected credentials
	// (may be empty when credentials came from another source).
	Handler string
	// CredentialSource is a human-readable description of the rejected
	// credential source (may be empty).
	CredentialSource string
}

// Error implements the error interface with a data-only message.
func (e *AuthDegradedError) Error() string {
	source := e.CredentialSource
	if source == "" {
		if e.Handler != "" {
			source = fmt.Sprintf("%s auth handler credentials", e.Handler)
		} else {
			source = "stored credentials"
		}
	}
	return fmt.Sprintf("authentication required for registry %q: %s were rejected; fell back to anonymous access", e.Registry, source)
}

// staleCredentialReporter is the subset of RemoteCatalog needed to build an
// AuthDegradedError. It lets NewAuthDegradedError be tested without a full
// RemoteCatalog and keeps the dependency direction clean.
type staleCredentialReporter interface {
	HasStaleCredentials() bool
	Registry() string
	AuthHandlerUsed() string
	CredentialSource() string
}

// NewAuthDegradedError builds an AuthDegradedError from a catalog that fell
// back to anonymous access. It returns nil when the catalog's credentials are
// not stale, so callers can write:
//
//	if degraded := catalog.NewAuthDegradedError(rc); degraded != nil { ... }
func NewAuthDegradedError(rc staleCredentialReporter) *AuthDegradedError {
	if rc == nil || !rc.HasStaleCredentials() {
		return nil
	}
	return &AuthDegradedError{
		Registry:         rc.Registry(),
		Handler:          rc.AuthHandlerUsed(),
		CredentialSource: rc.CredentialSource(),
	}
}

// IsAuthDegraded reports whether err (or any error it wraps) is an
// AuthDegradedError, returning the typed value when it is.
func IsAuthDegraded(err error) (*AuthDegradedError, bool) {
	var e *AuthDegradedError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
