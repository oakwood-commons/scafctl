// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package store provides a minimal, fully generic, in-memory key/value store.
//
// The store is intentionally NOT safe for concurrent use and imposes no bound
// on the number of entries. It is a thin, allocation-light building block meant
// to be wrapped by higher-level types that add their own synchronization,
// indexing, or eviction policy. Callers needing TTL, bounded capacity, or
// thread safety should use the pkg/cache packages instead.
package store

// Store is an unbounded, generic key/value store backed by a map.
//
// Store is NOT safe for concurrent use. Wrap it with a type that provides
// synchronization if it is shared across goroutines.
type Store[K comparable, V any] struct {
	items map[K]V

	// OnEvict, if non-nil, is invoked with the removed value whenever an entry
	// leaves the store via Delete. It fires only when a matching key was
	// present, and always AFTER the entry has been removed from the map.
	// Overwriting an existing key via Set does NOT fire OnEvict.
	//
	// The callback runs synchronously and lock-free: the store never takes a
	// lock. If a wrapper synchronizes access to the store, OnEvict executes
	// while that wrapper's lock is still held, so the callback MUST NOT attempt
	// to acquire that lock or re-enter the store, or it will deadlock.
	OnEvict func(value V)
}

// New creates an empty Store.
func New[K comparable, V any]() *Store[K, V] {
	return &Store[K, V]{items: make(map[K]V)}
}

// Get returns the value stored under key and reports whether it was present.
func (s *Store[K, V]) Get(key K) (V, bool) {
	v, ok := s.items[key]
	return v, ok
}

// Set stores value under key, overwriting any existing value.
func (s *Store[K, V]) Set(key K, value V) {
	s.items[key] = value
}

// Delete removes the entry stored under key. Deleting an absent key is a no-op.
// If the key was present and OnEvict is set, OnEvict is invoked with the removed
// value after the entry is removed from the map.
func (s *Store[K, V]) Delete(key K) {
	value, ok := s.items[key]
	if !ok {
		return
	}
	delete(s.items, key)
	if s.OnEvict != nil {
		s.OnEvict(value)
	}
}

// Len returns the number of entries in the store. This is a constant-time
// operation: it reads the map's live element count without iterating.
func (s *Store[K, V]) Len() int {
	return len(s.items)
}
