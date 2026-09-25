// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidProvider indicates a configured state provider is missing or lacks CapabilityState.
	ErrInvalidProvider = errors.New("state provider does not have CapabilityState")

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

	// ErrLegacyStateConfig indicates a solution still uses state configuration
	// keys that were removed when state was split into load and save (for
	// example state.backend or state.emit). It is returned while the solution is
	// decoded, so every entrypoint fails loudly instead of silently ignoring the
	// old keys -- which would otherwise turn state off without any error.
	ErrLegacyStateConfig = errors.New("unsupported state configuration")
)

// NotFoundError is returned by Load when the manager requires existing state
// (see WithRequireExisting) and the load provider reports that none exists.
type NotFoundError struct {
	// Location is the resolved location that was read (e.g. a file path).
	// Empty when the provider has no path/url style input.
	Location string `json:"location" yaml:"location" doc:"Resolved location that was read"`

	// Provider is the load provider name.
	Provider string `json:"provider" yaml:"provider" doc:"Load provider name"`
}

func (e *NotFoundError) Error() string {
	if e.Location != "" {
		return fmt.Sprintf("state file %q does not exist", e.Location)
	}
	return fmt.Sprintf("no state exists at the %s load provider", e.Provider)
}

// MissingLocksError is returned by CheckMissingLocks when a run replays a state
// document that carries parameters but no immutable resolver locks -- typically
// an intent document -- through a solution that declares immutable resolvers.
// Proceeding would re-derive those values and replace any previously saved
// locks, so the caller must opt in explicitly.
type MissingLocksError struct {
	// Location is where the lock-less document was loaded from.
	Location string `json:"location" yaml:"location" doc:"Where the lock-less document was loaded from"`

	// Provider is the load provider name, used when Location is empty.
	Provider string `json:"provider" yaml:"provider" doc:"Load provider name"`

	// Resolvers is the sorted list of immutable resolver names whose locked
	// values would be re-derived.
	Resolvers []string `json:"resolvers" yaml:"resolvers" doc:"Immutable resolvers whose values would be re-derived"`
}

func (e *MissingLocksError) Error() string {
	where := fmt.Sprintf("%q", e.Location)
	if e.Location == "" {
		where = fmt.Sprintf("from the %s load provider", e.Provider)
	}
	return fmt.Sprintf(
		"state %s has parameters but no immutable locks: immutable resolver(s) [%s] would be re-derived instead of replayed from a saved lock",
		where, strings.Join(e.Resolvers, ", "),
	)
}

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

// CycleError is returned when a load-time state configuration field (enabled
// or a load input) references a resolver that cannot be resolved before state is
// loaded -- i.e. a resolver that itself reads state (via the state provider) or
// transitively depends on one that does. Honouring such a reference would
// require running the resolver before the state it depends on has been loaded,
// a circular dependency. The referenced resolvers are listed so the author can
// see exactly which references break the acyclic guarantee.
type CycleError struct {
	// Location is the config path of the offending field, e.g. "state.enabled"
	// or "state.load.inputs.path".
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
