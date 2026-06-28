// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package tokenprovider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// FallbackResolver lazily resolves a TokenProvider by name when it is not
// already registered. It is consulted by GetContext on a cache miss, allowing
// official plugin-backed sources to be fetched and registered on demand instead
// of failing immediately. Returning a nil provider with a nil error is treated
// as "not found".
type FallbackResolver func(ctx context.Context, name string) (TokenProvider, error)

// Registry is a thread-safe map of named TokenProviders. Eagerly-registered
// sources are populated at startup; an optional FallbackResolver resolves
// additional sources lazily on first use.
type Registry struct {
	mu       sync.RWMutex
	sources  map[string]TokenProvider
	fallback FallbackResolver
}

// NewRegistry creates an empty token provider registry.
func NewRegistry() *Registry {
	return &Registry{sources: make(map[string]TokenProvider)}
}

// SetFallback configures the lazy fallback resolver consulted by GetContext
// when a source is not already registered. Passing nil clears it.
func (r *Registry) SetFallback(fn FallbackResolver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fallback = fn
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
// or nil and false if no source is registered. Get never consults the
// fallback resolver; use GetContext for lazy resolution.
func (r *Registry) Get(name string) (TokenProvider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sources[name]
	return s, ok
}

// GetContext returns the TokenProvider registered under name. On a cache miss
// it consults the fallback resolver (if configured), which may lazily resolve
// and download an official plugin-backed source. A successfully resolved source
// is cached for subsequent lookups. Returns an error wrapping ErrSourceNotFound
// when the source is neither registered nor resolvable.
func (r *Registry) GetContext(ctx context.Context, name string) (TokenProvider, error) {
	r.mu.RLock()
	s, ok := r.sources[name]
	fallback := r.fallback
	r.mu.RUnlock()
	if ok {
		return s, nil
	}
	if fallback == nil {
		return nil, fmt.Errorf("%w: %s", ErrSourceNotFound, name)
	}

	resolved, err := fallback(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSourceNotFound, name, err)
	}
	if resolved == nil {
		return nil, fmt.Errorf("%w: %s", ErrSourceNotFound, name)
	}

	// Enforce the same name invariant as Register before caching: the resolved
	// provider must report a name matching the requested name. This prevents a
	// buggy fallback from caching a provider with an empty or mismatched Name(),
	// which would leave the registry internally inconsistent.
	if resolvedName := resolved.Name(); resolvedName != name {
		return nil, fmt.Errorf("%w: %s: fallback resolved provider with mismatched name %q", ErrSourceNotFound, name, resolvedName)
	}

	// Cache for subsequent lookups, tolerating a concurrent resolution that
	// may have registered the same name first.
	r.mu.Lock()
	if existing, exists := r.sources[name]; exists {
		resolved = existing
	} else {
		r.sources[name] = resolved
	}
	r.mu.Unlock()
	return resolved, nil
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
