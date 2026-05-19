// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package authdelegation

import (
	"sort"
	"sync"
)

// DelegatorRegistry maps auth provider names to TokenDelegator implementations.
// Registration happens at server startup; lookups happen per-request concurrently.
type DelegatorRegistry struct {
	mu         sync.RWMutex
	delegators map[string]TokenDelegator
}

// NewDelegatorRegistry creates an empty registry.
func NewDelegatorRegistry() *DelegatorRegistry {
	return &DelegatorRegistry{delegators: make(map[string]TokenDelegator)}
}

// Register adds a delegator under the given name.
// Panics if name is empty (startup-time programming error).
func (r *DelegatorRegistry) Register(name string, d TokenDelegator) {
	if name == "" {
		panic("authdelegation: Register called with empty name")
	}
	r.mu.Lock()
	r.delegators[name] = d
	r.mu.Unlock()
}

// Get returns the delegator registered under name.
// Returns nil, false if no delegator is registered for that name.
func (r *DelegatorRegistry) Get(name string) (TokenDelegator, bool) {
	r.mu.RLock()
	d, ok := r.delegators[name]
	r.mu.RUnlock()
	return d, ok
}

// Has reports whether a delegator is registered under name.
func (r *DelegatorRegistry) Has(name string) bool {
	r.mu.RLock()
	_, ok := r.delegators[name]
	r.mu.RUnlock()
	return ok
}

// Names returns a sorted list of all registered delegator names.
func (r *DelegatorRegistry) Names() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.delegators))
	for name := range r.delegators {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}
