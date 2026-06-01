// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package tokenprovider

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Registry is a thread-safe map of named TokenProviders, built once at startup.
type Registry struct {
	mu      sync.RWMutex
	sources map[string]TokenProvider
}

// NewRegistry creates an empty token provider registry.
func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]TokenProvider)}
}

// Register adds a TokenProvider to the registry. Returns an error if the source
// is nil or a source with the same name is already registered.
func (r *Registry) Register(source TokenProvider) error {
	if source == nil {
		return errors.New("tokenprovider: cannot register nil source")
	}

	name := source.Name()
	if name == "" {
		return errors.New("tokenprovider: source name cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.sources[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateSource, name)
	}

	r.sources[name] = source
	return nil
}

// Get returns the TokenProvider registered under name and true,
// or nil and false if no source is registered.
func (r *Registry) Get(name string) (TokenProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[name]
	return s, ok
}

// MustGet returns the TokenProvider or panics if not found.
// Use only during startup wiring.
func (r *Registry) MustGet(name string) TokenProvider {
	s, ok := r.Get(name)
	if !ok {
		panic(fmt.Sprintf("tokenprovider: source %q not registered", name))
	}
	return s
}

// Names returns a sorted list of all registered source names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.sources))
	for name := range r.sources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Len returns the number of registered sources.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sources)
}
