// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package versionindex provides a generic, per-key index of semver versions
// kept sorted in descending order. It backs the plugin pool's mapping from a
// plugin identity to the set of concrete versions currently loaded, supporting
// exact lookups and highest-satisfying-constraint resolution.
package versionindex

import (
	"iter"
	"sort"

	"github.com/Masterminds/semver/v3"
)

// Index maps each key to a slice of semver versions kept sorted in descending
// order (highest first) with no duplicates.
//
// Invariants (per key):
//   - the versions slice is sorted in descending order (highest version first);
//   - the versions slice contains no duplicates;
//   - the versions slice contains only valid semver versions.
type Index[T comparable] struct {
	entries map[T][]*semver.Version
}

// New returns an empty Index.
func New[T comparable]() *Index[T] {
	return &Index[T]{
		entries: make(map[T][]*semver.Version),
	}
}

// AddVersion inserts an already-parsed version into the index. Callers holding
// a *semver.Version avoid re-parsing and the operation cannot fail. Inserting a
// duplicate is a no-op; otherwise the version is placed to preserve the
// descending-sort invariant.
func (vi *Index[T]) AddVersion(key T, sv *semver.Version) {
	entry := vi.entries[key]
	idx, found := search(entry, sv)
	if found {
		return // duplicate, do not insert
	}
	entry = append(entry, nil)
	copy(entry[idx+1:], entry[idx:])
	entry[idx] = sv
	vi.entries[key] = entry
}

// DeleteVersion removes an already-parsed version from the index. Callers
// holding a *semver.Version avoid re-parsing and the operation cannot fail.
// Removing an absent version (or a version under an absent key) is a no-op.
func (vi *Index[T]) DeleteVersion(key T, sv *semver.Version) {
	entry := vi.entries[key]
	idx := findExact(entry, sv)
	if idx >= 0 {
		entry = append(entry[:idx], entry[idx+1:]...)
		if len(entry) == 0 {
			delete(vi.entries, key)
		} else {
			vi.entries[key] = entry
		}
	}
}

// GetVersion performs an exact, parse-free lookup of an already-parsed version.
// It returns the indexed version equal to sv, or nil if that version (or its
// key) is absent. It never interprets ranges or "latest".
func (vi *Index[T]) GetVersion(key T, sv *semver.Version) *semver.Version {
	entry := vi.entries[key]
	if idx := findExact(entry, sv); idx >= 0 {
		return entry[idx]
	}
	return nil
}

func (vi *Index[T]) GetLatest(key T) *semver.Version {
	entry := vi.entries[key]
	if len(entry) == 0 {
		return nil
	}
	return entry[0]
}

// GetConstraints returns the highest indexed version satisfying c, or nil if
// none does (or the key is absent). Callers hold a pre-compiled
// *semver.Constraints; because entries are sorted descending, the first match
// is the highest satisfying version.
func (vi *Index[T]) GetConstraints(key T, c *semver.Constraints) *semver.Version {
	for _, v := range vi.entries[key] {
		if c.Check(v) {
			return v
		}
	}
	return nil
}

// Has reports whether the index holds any version under key.
func (vi *Index[T]) Has(key T) bool {
	_, ok := vi.entries[key]
	return ok
}

// All returns an iterator over every key and its versions slice (in stored,
// descending order). The yielded slice is the index's internal storage and must
// not be mutated by the caller.
func (vi *Index[T]) All() iter.Seq2[T, []*semver.Version] {
	return func(yield func(T, []*semver.Version) bool) {
		for key, versions := range vi.entries {
			if !yield(key, versions) {
				return
			}
		}
	}
}

// search binary-searches entry (sorted descending, the Index invariant) for sv.
// It returns the index where sv is or would be inserted to preserve order, and
// whether an equal version was found at that index. Callers that insert use idx
// on the miss path; callers that look up or remove use idx only when found is
// true.
func search(entry []*semver.Version, sv *semver.Version) (idx int, found bool) {
	idx = sort.Search(len(entry), func(i int) bool {
		return !entry[i].GreaterThan(sv)
	})
	return idx, idx < len(entry) && entry[idx].Equal(sv)
}

// findExact returns the index of the version in entry equal to sv, or -1 if
// absent. entry must be sorted in descending order (the Index invariant),
// enabling a binary search instead of a linear scan.
func findExact(entry []*semver.Version, sv *semver.Version) int {
	if idx, found := search(entry, sv); found {
		return idx
	}
	return -1
}

func ParseVersionOrConstraint(s string) (sv *semver.Version, c *semver.Constraints, err error) {
	// StrictNewVersion rejects partials ("1.2") and ranges (">=1.2.3"), so a
	// success unambiguously means an exact version was given.
	if v, verr := semver.StrictNewVersion(s); verr == nil {
		return v, nil, nil
	}
	cc, cerr := semver.NewConstraint(s)
	if cerr != nil {
		return nil, nil, cerr
	}
	return nil, cc, nil
}
