// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package official defines the registry of first-party providers that were
// extracted from scafctl's built-in set into standalone plugin repos. The
// registry drives auto-resolution: when a solution references a provider
// that isn't a built-in or an explicitly declared plugin, the runtime
// checks this list and auto-fetches from the official OCI catalog.
package official

import (
	"context"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/solution"
)

type contextKey struct{}

// WithRegistry stores an official provider registry in the context.
// Used by RootOptions to propagate the embedder's registry to downstream code.
func WithRegistry(ctx context.Context, r *Registry) context.Context {
	return context.WithValue(ctx, contextKey{}, r)
}

// RegistryFromContext retrieves the official provider registry from the
// context. Returns nil if not set.
func RegistryFromContext(ctx context.Context) *Registry {
	r, _ := ctx.Value(contextKey{}).(*Registry)
	return r
}

// Provider describes an official first-party provider available from the
// oakwood-commons OCI catalog.
type Provider struct {
	// Name is the provider name used in solution YAML (e.g., "static").
	Name string

	// CatalogRef is the OCI artifact name within the catalog
	// (e.g., "static" resolves to ghcr.io/oakwood-commons/providers/static).
	CatalogRef string

	// DefaultVersion is the semver constraint applied when auto-resolving
	// (e.g., ">=0.1.0").
	DefaultVersion string
}

// defaultProviders is the canonical list of all 10 extracted first-party
// providers. Sorted alphabetically by name.
//
// DefaultVersion is "latest" so the catalog resolver picks the newest
// available version. This avoids hard-coding a concrete semver that must
// be bumped after every provider release.
//
// NOTE: "static" and "parameter" were brought back as built-ins due to
// performance overhead -- their sub-microsecond execution time was dominated
// by gRPC serialization cost when used as plugins.
var defaultProviders = []Provider{
	{Name: "directory", CatalogRef: "directory", DefaultVersion: "latest"},
	{Name: "env", CatalogRef: "env", DefaultVersion: "latest"},
	{Name: "exec", CatalogRef: "exec", DefaultVersion: "latest"},
	{Name: "git", CatalogRef: "git", DefaultVersion: "latest"},
	{Name: "github", CatalogRef: "github", DefaultVersion: "latest"},
	{Name: "hcl", CatalogRef: "hcl", DefaultVersion: "latest"},
	{Name: "identity", CatalogRef: "identity", DefaultVersion: "latest"},
	{Name: "metadata", CatalogRef: "metadata", DefaultVersion: "latest"},
	{Name: "secret", CatalogRef: "secret", DefaultVersion: "latest"},
	{Name: "sleep", CatalogRef: "sleep", DefaultVersion: "latest"},
}

// Registry holds the set of known official providers.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry returns the default official provider registry containing
// all 10 extracted first-party providers.
func NewRegistry() *Registry {
	return NewRegistryFrom(defaultProviders)
}

// NewRegistryFrom creates a registry from a custom provider list.
// Embedders use this via RootOptions.OfficialProviders to extend or
// replace the default set.
func NewRegistryFrom(providers []Provider) *Registry {
	m := make(map[string]Provider, len(providers))
	for _, p := range providers {
		m[p.Name] = p
	}
	return &Registry{providers: m}
}

// DefaultProviders returns a copy of the 10 official provider entries.
// Embedders can append their own entries and pass the result to
// NewRegistryFrom to extend rather than replace the defaults.
func DefaultProviders() []Provider {
	out := make([]Provider, len(defaultProviders))
	copy(out, defaultProviders)
	return out
}

// Get returns the official provider entry and true if name is a known
// official provider, or a zero Provider and false otherwise.
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]
	return p, ok
}

// Has returns true if name is a known official provider.
func (r *Registry) Has(name string) bool {
	_, ok := r.providers[name]
	return ok
}

// Names returns a sorted list of all official provider names.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Len returns the number of providers in the registry.
func (r *Registry) Len() int {
	return len(r.providers)
}

// ToPluginDependency converts an official provider entry to a
// PluginDependency suitable for the existing plugin auto-fetch pipeline.
func (p Provider) ToPluginDependency() solution.PluginDependency {
	return solution.PluginDependency{
		Name:    p.CatalogRef,
		Kind:    solution.PluginKindProvider,
		Version: p.DefaultVersion,
	}
}
