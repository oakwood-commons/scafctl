// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
)

// CheckImmutables verifies immutable resolver values against state after execution.
// For each resolver with Immutable: true:
//   - If no prior value exists in state, the resolved value is saved.
//   - If a prior value exists and matches, no action is taken.
//   - If a prior value exists and differs, an error is returned.
//
// Returns the updated state data (with any newly locked immutables).
func CheckImmutables(stateData *Data, resolverCtx *resolver.Context, resolvers []*resolver.Resolver) error {
	if stateData == nil {
		return nil
	}

	if stateData.Immutables == nil {
		stateData.Immutables = make(map[string]*ImmutableEntry)
	}

	now := time.Now().UTC()
	for _, r := range resolvers {
		if !r.Immutable {
			continue
		}

		result, ok := resolverCtx.GetResult(r.Name)
		if !ok || result.Status != resolver.ExecutionStatusSuccess {
			continue
		}

		existing, exists := stateData.Immutables[r.Name]
		if !exists {
			// First run: save the value
			stateData.Immutables[r.Name] = &ImmutableEntry{
				Value:     result.Value,
				Type:      string(r.Type),
				CreatedAt: now,
			}
			continue
		}

		// Subsequent run: verify the value matches
		if !immutableValuesEqual(existing.Value, result.Value) {
			return fmt.Errorf("%w %q: resolved value differs from locked value; use the state delete command to remove it first", ErrImmutableEntry, r.Name)
		}
	}

	return nil
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
