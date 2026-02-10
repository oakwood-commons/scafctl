// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages registered auth handlers.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
}

// NewRegistry creates a new auth handler registry.
func NewRegistry() *Registry {
	return &Registry{handlers: make(map[string]Handler)}
}

// Register adds an auth handler to the registry.
func (r *Registry) Register(handler Handler) error {
	if handler == nil {
		return fmt.Errorf("cannot register nil handler")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	name := handler.Name()
	if name == "" {
		return fmt.Errorf("handler name cannot be empty")
	}

	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("auth handler %q already registered", name)
	}

	r.handlers[name] = handler
	return nil
}

// Unregister removes an auth handler from the registry.
func (r *Registry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.handlers[name]; !exists {
		return fmt.Errorf("%w: %s", ErrHandlerNotFound, name)
	}

	delete(r.handlers, name)
	return nil
}

// Get retrieves an auth handler by name.
func (r *Registry) Get(name string) (Handler, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handler, exists := r.handlers[name]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrHandlerNotFound, name)
	}

	return handler, nil
}

// List returns the names of all registered handlers in sorted order.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Has returns true if a handler with the given name is registered.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.handlers[name]
	return exists
}

// Count returns the number of registered handlers.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers)
}

// All returns all registered handlers as a map.
func (r *Registry) All() map[string]Handler {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string]Handler, len(r.handlers))
	for name, handler := range r.handlers {
		result[name] = handler
	}
	return result
}
