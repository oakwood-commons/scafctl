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

	// ErrIncompatibleSchemaVersion indicates the state file was written by an
	// older, no-longer-supported schema version whose layout cannot be safely
	// read by this build (e.g. a breaking change dropped a field). The file must
	// be deleted and recreated.
	ErrIncompatibleSchemaVersion = errors.New("incompatible state schema version")
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

// CycleError is returned when a state configuration field (enabled or a
// backend input) references a resolver that cannot be resolved before state is
// loaded -- i.e. a resolver that itself reads state (via the state provider) or
// transitively depends on one that does. Honouring such a reference would
// require running the resolver before the state it depends on has been loaded,
// a circular dependency. The referenced resolvers are listed so the author can
// see exactly which references break the acyclic guarantee.
type CycleError struct {
	// Location is the config path of the offending field, e.g. "state.enabled"
	// or "state.backend.inputs.path".
	Location string `json:"location" yaml:"location" doc:"Config path of the offending field"`

	// Refs is the sorted list of state-dependent resolver names referenced at
	// Location.
	Refs []string `json:"refs" yaml:"refs" doc:"State-dependent resolver names referenced"`
}

func (e *CycleError) Error() string {
	return fmt.Sprintf(
		"%s references state-dependent resolver(s) [%s]: those resolvers read state (or depend on one that does), so they cannot run before state is loaded (circular dependency)",
		e.Location, strings.Join(e.Refs, ", "),
	)
}

// UnknownStateRefError is returned when a state configuration field references a
// resolver name that does not exist in the solution. This is almost always a
// typo; catching it at load time yields a clear message instead of a silent
// null/empty value.
type UnknownStateRefError struct {
	// Location is the config path of the offending field.
	Location string `json:"location" yaml:"location" doc:"Config path of the offending field"`

	// Refs is the sorted list of unknown resolver names referenced at Location.
	Refs []string `json:"refs" yaml:"refs" doc:"Unknown resolver names referenced"`
}

func (e *UnknownStateRefError) Error() string {
	return fmt.Sprintf(
		"%s references unknown resolver(s) [%s]: no such resolver is defined in the solution",
		e.Location, strings.Join(e.Refs, ", "),
	)
}
