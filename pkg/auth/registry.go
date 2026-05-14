// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// FallbackResolverFunc is called by Get when a handler is not found in the
// registry. It receives the handler name and should return the resolved
// handler or an error. The resolver is invoked at most once per name —
// resolved handlers are automatically registered and subsequent Get calls
// return them directly without invoking the fallback again.
type FallbackResolverFunc func(ctx context.Context, name string) (Handler, error)

// RegistryOption configures a Registry during construction.
type RegistryOption func(*Registry)

// WithFallbackResolver sets a lazy fallback resolver that is called when
// Get cannot find a handler in the registry. This enables demand-driven
// plugin resolution — only handlers that are actually requested trigger
// network I/O or subprocess startup.
func WithFallbackResolver(fn FallbackResolverFunc) RegistryOption {
	return func(r *Registry) {
		r.fallback = fn
	}
}

// Registry manages registered auth handlers.
type Registry struct {
	mu       sync.RWMutex
	handlers map[string]Handler
	fallback FallbackResolverFunc
	// resolving tracks in-flight fallback resolutions to prevent recursive
	// calls and duplicate fetches for the same handler name.
	resolving sync.Map
}

// NewRegistry creates a new auth handler registry.
func NewRegistry(opts ...RegistryOption) *Registry {
	r := &Registry{handlers: make(map[string]Handler)}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// SetFallbackResolver sets or replaces the fallback resolver. This is safe
// to call after construction (e.g., from PersistentPreRun after the registry
// is already stored in context).
func (r *Registry) SetFallbackResolver(fn FallbackResolverFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = fn
}

// Sentinel errors for the registry.
var (
	// ErrNilHandler indicates a nil handler was passed to Register.
	ErrNilHandler = errors.New("cannot register nil handler")
	// ErrEmptyHandlerName indicates a handler with an empty name was passed to Register.
	ErrEmptyHandlerName = errors.New("handler name cannot be empty")
	// ErrHandlerAlreadyRegistered indicates a handler with the same name is already registered.
	ErrHandlerAlreadyRegistered = errors.New("auth handler already registered")
)

// Register adds an auth handler to the registry.
func (r *Registry) Register(handler Handler) error {
	if handler == nil {
		return ErrNilHandler
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	name := handler.Name()
	if name == "" {
		return ErrEmptyHandlerName
	}

	if _, exists := r.handlers[name]; exists {
		return fmt.Errorf("%w: %s", ErrHandlerAlreadyRegistered, name)
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

// Get retrieves an auth handler by name. If the handler is not found and a
// fallback resolver is configured, it invokes the resolver to lazily fetch
// the handler (e.g., by downloading and starting a plugin). Resolved handlers
// are automatically registered for subsequent lookups.
//
// Get uses context.Background() for the fallback resolver. Use GetContext to
// propagate caller cancellation into the fallback.
func (r *Registry) Get(name string) (Handler, error) {
	return r.GetContext(context.Background(), name)
}

// GetContext retrieves an auth handler by name, propagating the given context
// into the fallback resolver. This allows callers to cancel long-running
// fallback operations (e.g., plugin downloads) via context cancellation.
func (r *Registry) GetContext(ctx context.Context, name string) (Handler, error) {
	r.mu.RLock()
	handler, exists := r.handlers[name]
	fallback := r.fallback
	r.mu.RUnlock()

	if exists {
		return handler, nil
	}

	// No fallback configured — return not-found immediately.
	if fallback == nil {
		return nil, fmt.Errorf("%w: %s", ErrHandlerNotFound, name)
	}

	return r.resolveWithFallbackContext(ctx, name, fallback)
}

// resolveWithFallbackContext invokes the fallback resolver, deduplicating
// concurrent requests for the same handler name.
func (r *Registry) resolveWithFallbackContext(ctx context.Context, name string, fallback FallbackResolverFunc) (Handler, error) {
	// Use sync.Map to deduplicate concurrent resolutions for the same name.
	ch := make(chan struct{})
	actual, loaded := r.resolving.LoadOrStore(name, ch)
	if loaded {
		// Another goroutine is already resolving this handler — wait for it,
		// but respect context cancellation to avoid goroutine leaks.
		waitCh, ok := actual.(chan struct{})
		if !ok {
			return nil, fmt.Errorf("%w: %s (invalid resolving state)", ErrHandlerNotFound, name)
		}
		select {
		case <-waitCh:
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %s", ctx.Err(), name)
		}
		// Re-check the registry after the other goroutine finishes.
		r.mu.RLock()
		handler, exists := r.handlers[name]
		r.mu.RUnlock()
		if exists {
			return handler, nil
		}
		return nil, fmt.Errorf("%w: %s", ErrHandlerNotFound, name)
	}
	defer func() {
		r.resolving.Delete(name)
		close(ch)
	}()

	handler, err := fallback(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("fallback resolver for %q: %w", name, err)
	}
	if handler == nil {
		return nil, fmt.Errorf("%w: %s", ErrHandlerNotFound, name)
	}

	// Register the resolved handler so future Get calls are instant.
	// The fallback itself may have already registered the handler (e.g.,
	// via RegisterFetchedAuthHandlerPlugins), so only store if absent.
	r.mu.Lock()
	if _, exists := r.handlers[name]; !exists {
		r.handlers[name] = handler
	}
	r.mu.Unlock()

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

// GetRegistered retrieves a handler by name from the registry without
// invoking the fallback resolver. Returns the handler and true if found,
// or nil and false if not registered.
func (r *Registry) GetRegistered(name string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	handler, exists := r.handlers[name]
	return handler, exists
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
