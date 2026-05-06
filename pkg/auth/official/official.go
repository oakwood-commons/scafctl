// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package official defines the registry of first-party auth handlers that will
// be extracted from scafctl's built-in set into standalone plugin repos. The
// registry drives auto-resolution: when a CLI command or solution references an
// auth handler that isn't registered (built-in, config-defined, or plugin), the
// runtime checks this list and auto-fetches from the official OCI catalog.
package official

import (
	"context"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/solution"
)

type contextKey struct{}

// WithRegistry stores an official auth handler registry in the context.
// Used by RootOptions to propagate the embedder's registry to downstream code.
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	return context.WithValue(ctx, contextKey{}, r)
}

// RegistryFromContext retrieves the official auth handler registry from the
// context. Returns nil if not set.
func RegistryFromContext(ctx context.Context) *Registry {
	r, _ := ctx.Value(contextKey{}).(*Registry)
	return r
}

// AuthHandler describes an official first-party auth handler available from the
// oakwood-commons OCI catalog.
type AuthHandler struct {
	// Name is the auth handler name used in CLI commands and solution YAML
	// (e.g., "github").
	Name string

	// CatalogRef is the OCI artifact name within the catalog
	// (e.g., "github" resolves to ghcr.io/oakwood-commons/auth-handlers/github).
	CatalogRef string

	// DefaultVersion is the semver constraint applied when auto-resolving
	// (e.g., ">=0.1.0").
	DefaultVersion string

	// MinSDKVersion is the minimum plugin SDK version required for this handler.
	MinSDKVersion string

	// TrustedVerificationDomains are known-good identity provider domains for
	// device code URL validation. These are hardcoded for official handlers and
	// merged with user-configurable domains at runtime.
	TrustedVerificationDomains []string
}

// defaultAuthHandlers is the canonical list of all 3 auth handlers planned for
// extraction. Sorted alphabetically by name.
//
// DefaultVersion is "latest" so the catalog resolver picks the newest
// available version. This avoids hard-coding a concrete semver that must
// be bumped after every handler release.
var defaultAuthHandlers = []AuthHandler{
	{
		Name:           "entra",
		CatalogRef:     "entra",
		DefaultVersion: "latest",
		MinSDKVersion:  "0.2.0",
		TrustedVerificationDomains: []string{
			"login.microsoftonline.com",
			"login.microsoft.com",
		},
	},
	{
		Name:           "gcp",
		CatalogRef:     "gcp",
		DefaultVersion: "latest",
		MinSDKVersion:  "0.2.0",
		TrustedVerificationDomains: []string{
			"accounts.google.com",
		},
	},
	{
		Name:           "github",
		CatalogRef:     "github",
		DefaultVersion: "latest",
		MinSDKVersion:  "0.2.0",
		TrustedVerificationDomains: []string{
			"github.com",
		},
	},
}

// Registry holds the set of known official auth handlers.
type Registry struct {
	handlers map[string]AuthHandler
}

// NewRegistry returns the default official auth handler registry containing
// all 3 auth handlers planned for extraction.
func NewRegistry() *Registry {
	return NewRegistryFrom(defaultAuthHandlers)
}

// NewRegistryFrom creates a registry from a custom auth handler list.
// Embedders use this via RootOptions.OfficialAuthHandlers to extend or
// replace the default set.
func NewRegistryFrom(handlers []AuthHandler) *Registry {
	m := make(map[string]AuthHandler, len(handlers))
	for _, h := range handlers {
		m[h.Name] = h
	}
	return &Registry{handlers: m}
}

// DefaultAuthHandlers returns a copy of the 3 official auth handler entries.
// Embedders can append their own entries and pass the result to
// NewRegistryFrom to extend rather than replace the defaults.
func DefaultAuthHandlers() []AuthHandler {
	out := make([]AuthHandler, len(defaultAuthHandlers))
	copy(out, defaultAuthHandlers)
	return out
}

// Get returns the official auth handler entry and true if name is a known
// official auth handler, or a zero AuthHandler and false otherwise.
func (r *Registry) Get(name string) (AuthHandler, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

// MustGet returns the official auth handler entry for name, panicking if not found.
// Use only when the caller has already verified existence via Has().
func (r *Registry) MustGet(name string) AuthHandler {
	h, ok := r.handlers[name]
	if !ok {
		panic("official auth handler not found: " + name)
	}
	return h
}

// Has returns true if name is a known official auth handler.
func (r *Registry) Has(name string) bool {
	_, ok := r.handlers[name]
	return ok
}

// Names returns a sorted list of all official auth handler names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.handlers))
	for name := range r.handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Len returns the number of auth handlers in the registry.
func (r *Registry) Len() int {
	return len(r.handlers)
}

// ToPluginDependency converts an official auth handler entry to a
// PluginDependency suitable for the existing plugin auto-fetch pipeline.
func (h AuthHandler) ToPluginDependency() solution.PluginDependency {
	return solution.PluginDependency{
		Name:    h.CatalogRef,
		Kind:    solution.PluginKindAuthHandler,
		Version: h.DefaultVersion,
	}
}
