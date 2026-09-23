// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
)

// CheckMissingLocks guards against silently replacing immutable locks. It
// returns a *MissingLocksError when a loaded state document carries parameters
// but no creation timestamp -- the shape of an intent document, or of a
// hand-built replay document, rather than a full state file -- and one or more
// of the solution's immutable resolvers has no lock in it. Running on would
// re-derive those values and overwrite any previously saved locks.
//
// A document written by a full-format save always carries a creation
// timestamp, so a genuine full state file never trips this check -- including
// when a newer solution version adds its first immutable resolver. Call it
// right after load, before the main resolver pass. The caller decides the
// policy: fail unless explicitly allowed (run), or warn (render).
func CheckMissingLocks(lr *LoadResult, resolvers []*resolver.Resolver) error {
	if lr == nil || lr.Skipped || !lr.Loaded || lr.Data == nil {
		return nil
	}
	if !lr.Data.Metadata.CreatedAt.IsZero() || lr.LoadedParams == 0 {
		return nil
	}

	var missing []string
	for _, r := range resolvers {
		if r == nil || !r.Immutable {
			continue
		}
		if entry, ok := lr.Data.Resolvers[r.Name]; ok && entry != nil && entry.Immutable {
			continue
		}
		missing = append(missing, r.Name)
	}
	if len(missing) == 0 {
		return nil
	}

	sort.Strings(missing)
	return &MissingLocksError{Location: lr.Location, Provider: lr.Provider, Resolvers: missing}
}

// PersistResolvers records resolver values into state after execution. For each
// resolver marked persist: true or immutable: true (immutable implies persist):
//
//   - Persist-only resolvers have their value overwritten each run (UpdatedAt
//     advances; CreatedAt is preserved from any prior entry).
//   - Immutable resolvers are locked on first write; on subsequent runs the
//     resolved value is verified against the locked value and an error is
//     returned on mismatch.
//
// Only resolvers that completed with status Success are recorded; skipped or
// failed resolvers leave any prior entry untouched.
//
// Resolvers whose names are present in skip are not locked on first run: a new
// immutable value is not persisted when its deferred (cross-resolver) validation
// failed. Existing locks are still verified so an immutable value cannot drift
// undetected.
func PersistResolvers(stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver, skip map[string]bool) error {
	if stateData == nil {
		return nil
	}

	if stateData.Resolvers == nil {
		stateData.Resolvers = make(map[string]*PersistedEntry)
	}

	now := time.Now().UTC()
	for _, r := range resolvers {
		if !r.Immutable && !r.Persist {
			continue
		}

		result, ok := resolverCtx.GetResult(r.Name)
		if !ok || result.Status != resolver.ExecutionStatusSuccess {
			continue
		}

		existing, exists := stateData.Resolvers[r.Name]

		if r.Immutable {
			if !exists || !existing.Immutable {
				if skip[r.Name] {
					// Deferred (cross-resolver) validation failed for this resolver:
					// do not lock a new immutable value. Any existing lock is still
					// verified below so a value cannot drift undetected.
					continue
				}
				// First run as immutable (no prior entry, or the prior entry was
				// persist-only and is being promoted to immutable): lock the
				// current value rather than verifying against an unlocked entry.
				createdAt := now
				if exists {
					createdAt = existing.CreatedAt
				}
				stateData.Resolvers[r.Name] = &PersistedEntry{
					Value:     result.Value,
					Type:      string(r.Type),
					Immutable: true,
					CreatedAt: createdAt,
					UpdatedAt: now,
				}
				continue
			}

			// Subsequent run: verify the value matches.
			if !immutableValuesEqual(existing.Value, result.Value) {
				return immutableMismatchError(r.Name)
			}
			continue
		}

		// Persist-only: overwrite the value each run, preserving CreatedAt.
		// If a prior entry is immutable (the resolver was previously locked and
		// is now switched to persist-only), leave it untouched rather than
		// silently downgrading/unlocking it. Removing the lock requires an
		// explicit state delete.
		if exists && existing.Immutable {
			continue
		}

		createdAt := now
		if exists {
			createdAt = existing.CreatedAt
		}
		stateData.Resolvers[r.Name] = &PersistedEntry{
			Value:     result.Value,
			Type:      string(r.Type),
			Immutable: false,
			CreatedAt: createdAt,
			UpdatedAt: now,
		}
	}

	return nil
}

// VerifyImmutables checks resolved immutable values against previously locked
// state WITHOUT mutating state or locking new values. It is intended to run
// after resolver execution but BEFORE action execution, so that a violated
// immutable aborts the run before any side effects (file scaffolding, external
// calls, etc.) occur.
//
// Only resolvers with an existing locked entry are verified. New immutables
// (no prior entry) are ignored here -- they are locked later by PersistResolvers
// at save time. Returns an error on the first mismatch.
func VerifyImmutables(stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver) error {
	if stateData == nil || stateData.Resolvers == nil {
		return nil
	}

	for _, r := range resolvers {
		if !r.Immutable {
			continue
		}

		existing, exists := stateData.Resolvers[r.Name]
		if !exists || !existing.Immutable {
			continue
		}

		result, ok := resolverCtx.GetResult(r.Name)
		if !ok || result.Status != resolver.ExecutionStatusSuccess {
			continue
		}

		if !immutableValuesEqual(existing.Value, result.Value) {
			return immutableMismatchError(r.Name)
		}
	}

	return nil
}

// immutableMismatchError builds the standard error returned when a resolved
// immutable value differs from its locked value.
func immutableMismatchError(name string) error {
	return fmt.Errorf("%w %q: resolved value differs from locked value; use the state delete command to remove it first", ErrImmutableEntry, name)
}

// immutableValuesEqual compares two values for equality using JSON serialization
// to handle type-agnostic comparison (e.g., float64 vs int from JSON round-trips).
func immutableValuesEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	aj, aerr := json.Marshal(a)
	bj, berr := json.Marshal(b)
	if aerr != nil || berr != nil {
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
	return string(aj) == string(bj)
}
