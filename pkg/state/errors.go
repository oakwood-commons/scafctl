// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import "errors"

var (
	// ErrInvalidBackend indicates the configured backend provider lacks CapabilityState.
	ErrInvalidBackend = errors.New("state backend provider does not have CapabilityState")

	// ErrKeyNotFound indicates a requested state key does not exist.
	ErrKeyNotFound = errors.New("state key not found")

	// ErrImmutableEntry indicates an attempt to overwrite an immutable state entry.
	ErrImmutableEntry = errors.New("cannot overwrite immutable state entry")
)
