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
