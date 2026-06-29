// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidBackend indicates the configured backend provider lacks CapabilityState.
	ErrInvalidBackend = errors.New("state backend provider does not have CapabilityState")

	// ErrKeyNotFound indicates a requested state key does not exist.
	ErrKeyNotFound = errors.New("state key not found")

	// ErrImmutableEntry indicates an attempt to overwrite an immutable state entry.
	ErrImmutableEntry = errors.New("cannot overwrite immutable state entry")

	// ErrUnsupportedSchemaVersion indicates the state file was written by a newer
	// version of scafctl and cannot be safely read by this version.
	ErrUnsupportedSchemaVersion = errors.New("unsupported state schema version")
)

// MissingParamsError is returned when state load fails because the state
// configuration references __params keys that were not supplied via
// CLI -r flags. It wraps the original evaluation error and includes the list
// of missing parameter names so callers can produce actionable messages.
type MissingParamsError struct {
	// Missing is the sorted list of __params keys not found in the supplied params.
	Missing []string `json:"missing" yaml:"missing" doc:"Parameter names required by state configuration"`

	// Original is the underlying evaluation error.
	Original error `json:"-" yaml:"-" doc:"Underlying evaluation error"`
}

func (e *MissingParamsError) Error() string {
	return fmt.Sprintf(
		"state configuration requires parameters [%s] that were not supplied: %v",
		strings.Join(e.Missing, ", "), e.Original,
	)
}

func (e *MissingParamsError) Unwrap() error {
	return e.Original
}
