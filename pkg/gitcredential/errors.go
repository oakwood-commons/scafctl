// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gitcredential implements the Git credential helper protocol,
// exposing encrypted credentials and dynamic OAuth tokens to git over HTTPS.
package gitcredential

import (
	"errors"
	"fmt"
)

// Sentinel errors returned by Git credential helper operations.
var (
	// ErrCredentialsNotFound indicates no matching credentials exist for the target remote.
	ErrCredentialsNotFound = errors.New("credentials not found")

	// ErrInsecureTransport indicates plain HTTP was requested for a non-loopback host
	// without explicit opt-in.
	ErrInsecureTransport = errors.New("refusing to serve git credentials over insecure http transport")

	// ErrInvalidProtocolInput indicates malformed or unsafe git-credential stdin/stdout attributes.
	ErrInvalidProtocolInput = errors.New("invalid git credential protocol input")

	// ErrMissingHost indicates a git-credential request did not include a target host.
	ErrMissingHost = errors.New("git credential request missing host")

	// ErrForeignHelper indicates a gitconfig entry or shim file is owned by another tool.
	ErrForeignHelper = errors.New("existing credential helper is not managed by this CLI")
)

// ReauthRequiredError indicates an auth handler matched the target Git remote,
// but acquiring a valid token failed non-interactively. Callers render an
// actionable "<binary> auth login <handler>" hint on stderr.
type ReauthRequiredError struct {
	// Handler is the matched auth handler name (e.g. "github", "entra", "gcp").
	Handler string

	// Profile is the resolved auth profile name when non-default.
	Profile string

	// ServerURL is the canonical display URL for the target Git remote.
	ServerURL string

	// Err is the underlying token acquisition error.
	Err error
}

func (e *ReauthRequiredError) Error() string {
	if e.Profile != "" {
		return fmt.Sprintf("no valid token for %q via auth handler %q (profile %q): %v", e.ServerURL, e.Handler, e.Profile, e.Err)
	}
	return fmt.Sprintf("no valid token for %q via auth handler %q: %v", e.ServerURL, e.Handler, e.Err)
}

// Unwrap exposes the underlying error for errors.Is/As chains.
func (e *ReauthRequiredError) Unwrap() error { return e.Err }

// ForeignHelperError reports an existing non-managed gitconfig helper or shim file.
type ForeignHelperError struct {
	// Target identifies the gitconfig key or filesystem path that has a conflict.
	Target string

	// Existing is the foreign helper command or path description.
	Existing string
}

func (e *ForeignHelperError) Error() string {
	if e.Existing != "" {
		return fmt.Sprintf("%s: %s already configured as %q (pass --force to replace)", ErrForeignHelper.Error(), e.Target, e.Existing)
	}
	return fmt.Sprintf("%s: %s", ErrForeignHelper.Error(), e.Target)
}

// Unwrap returns ErrForeignHelper so callers can match via errors.Is.
func (e *ForeignHelperError) Unwrap() error { return ErrForeignHelper }
