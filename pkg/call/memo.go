// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package call

import "sync"

// Memo is a per-run, in-memory memo for opt-in call de-duplication. It is never
// persisted and is scoped to a single run. Each key computes its value exactly
// once, even under concurrent access, so identical bound arguments within a run
// reuse the first result instead of re-invoking the provider.
type Memo struct {
	mu      sync.Mutex
	entries map[string]*memoEntry
}

type memoEntry struct {
	once sync.Once
	val  any
	err  error
}

// NewMemo creates an empty de-duplication memo.
func NewMemo() *Memo {
	return &Memo{entries: make(map[string]*memoEntry)}
}

// Do returns the memoized result for key, computing it via fn on first access.
// Concurrent callers sharing a key block until the first computation completes
// and then observe the same result.
func (m *Memo) Do(key string, fn func() (any, error)) (any, error) {
	m.mu.Lock()
	entry, ok := m.entries[key]
	if !ok {
		entry = &memoEntry{}
		m.entries[key] = entry
	}
	m.mu.Unlock()

	entry.once.Do(func() {
		entry.val, entry.err = fn()
	})
	return entry.val, entry.err
}
