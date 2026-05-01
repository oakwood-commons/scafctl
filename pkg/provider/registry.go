// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Registry manages provider registration and discovery.
// It ensures global name uniqueness and maintains the latest stable version of each provider.
type Registry struct {
	mu         sync.RWMutex
	providers  map[string]Provider // key is provider name
	knownNames map[string]bool     // names marked as known without a provider implementation
	options    registryOptions
}

// registryOptions contains configuration for the registry.
type registryOptions struct {
	allowOverwrite bool // Allow overwriting existing providers (for testing)
}

// RegistryOption is a functional option for configuring a Registry.
type RegistryOption func(*registryOptions)

// WithAllowOverwrite allows overwriting existing providers in the registry.
// This is primarily for testing purposes and should not be used in production.
func WithAllowOverwrite(allow bool) RegistryOption {
	return func(opts *registryOptions) {
		opts.allowOverwrite = allow
	}
}

// NewRegistry creates a new provider registry with the given options.
func NewRegistry(opts ...RegistryOption) *Registry {
	options := registryOptions{
		allowOverwrite: false,
	}

	for _, opt := range opts {
		opt(&options)
	}

	return &Registry{
		providers:  make(map[string]Provider),
		knownNames: make(map[string]bool),
		options:    options,
	}
}

// Register registers a provider in the registry.
// Returns an error if:
// - Provider is nil
// - Provider descriptor is invalid
// - Provider name already exists (unless allowOverwrite is true)
// - Provider version is older than existing version
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return fmt.Errorf("cannot register nil provider")
	}

	desc := p.Descriptor()
	if desc == nil {
		return fmt.Errorf("provider descriptor cannot be nil")
	}

	// Validate descriptor
	if err := r.validateDescriptor(desc); err != nil {
		return fmt.Errorf("invalid provider descriptor: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if provider already exists
	if existing, exists := r.providers[desc.Name]; exists {
		if !r.options.allowOverwrite {
			existingDesc := existing.Descriptor()
			return fmt.Errorf("provider %q already registered with version %s", desc.Name, existingDesc.Version)
		}

		// If overwrite is allowed, check version
		existingDesc := existing.Descriptor()
		if desc.Version != nil && existingDesc.Version != nil {
			if desc.Version.LessThan(existingDesc.Version) {
				return fmt.Errorf(
					"cannot register provider %q: version %s is older than existing version %s",
					desc.Name,
					desc.Version,
					existingDesc.Version,
				)
			}
		}
	}

	// Register the provider
	r.providers[desc.Name] = p

	return nil
}

// MarkKnown records that a provider name exists without registering a full
// implementation. This is intended for lint/validation where we need to
// suppress missing-provider warnings for providers that will be fetched at
// runtime (e.g. bundle.plugins). Get still returns nil, false for these names;
// only Has returns true.
func (r *Registry) MarkKnown(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[name]; !exists {
		r.knownNames[name] = true
	}
}

// ShallowClone creates a new Registry that shares the same provider
// implementations but has an independent knownNames set. Use this when you
// need to call MarkKnown without mutating the original registry (e.g., lint).
func (r *Registry) ShallowClone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make(map[string]Provider, len(r.providers))
	for k, v := range r.providers {
		providers[k] = v
	}
	knownNames := make(map[string]bool, len(r.knownNames))
	for k, v := range r.knownNames {
		knownNames[k] = v
	}
	return &Registry{
		providers:  providers,
		knownNames: knownNames,
		options:    r.options,
	}
}

// Get retrieves a provider by name.
// Returns the provider and true if found, nil and false otherwise.
func (r *Registry) Get(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.providers[name]
	return p, exists
}

// Has checks if a provider with the given name is registered or marked as known.
func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if _, exists := r.providers[name]; exists {
		return true
	}
	return r.knownNames[name]
}

// List returns a list of all registered provider names.
// The list is sorted alphabetically.
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}

	// Sort for deterministic output
	sort.Strings(names)
	return names
}

// ListProviders returns a list of all registered providers.
// The list is sorted alphabetically by provider name.
func (r *Registry) ListProviders() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		providers = append(providers, p)
	}

	// Sort by name
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].Descriptor().Name < providers[j].Descriptor().Name
	})

	return providers
}

// ListByCapability returns providers that support the given capability.
// The list is sorted alphabetically by provider name.
func (r *Registry) ListByCapability(capability Capability) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Provider
	for _, p := range r.providers {
		desc := p.Descriptor()
		for _, cap := range desc.Capabilities {
			if cap == capability {
				result = append(result, p)
				break
			}
		}
	}

	// Sort by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Descriptor().Name < result[j].Descriptor().Name
	})

	return result
}

// ListByCategory returns providers in the given category.
// The list is sorted alphabetically by provider name.
func (r *Registry) ListByCategory(category string) []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []Provider
	for _, p := range r.providers {
		desc := p.Descriptor()
		if desc.Category == category {
			result = append(result, p)
		}
	}

	// Sort by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Descriptor().Name < result[j].Descriptor().Name
	})

	return result
}

// DescriptorLookup returns a function that looks up provider descriptors by name.
// This is used for dependency extraction during resolver phase building.
// The returned function returns nil if the provider is not found.
func (r *Registry) DescriptorLookup() func(name string) *Descriptor {
	return func(name string) *Descriptor {
		r.mu.RLock()
		defer r.mu.RUnlock()

		if p, exists := r.providers[name]; exists {
			return p.Descriptor()
		}
		return nil
	}
}

// Unregister removes a provider from the registry.
// Returns true if the provider was removed, false if it didn't exist.
// This is primarily for testing purposes.
func (r *Registry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.providers[name]; !exists {
		return false
	}

	delete(r.providers, name)
	return true
}

// Count returns the number of registered providers.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.providers)
}

// Clear removes all providers from the registry.
// This is primarily for testing purposes.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.providers = make(map[string]Provider)
}

// validateDescriptor validates a provider descriptor.
func (r *Registry) validateDescriptor(desc *Descriptor) error {
	// Name is required
	if desc.Name == "" {
		return fmt.Errorf("provider name cannot be empty")
	}

	// APIVersion is required
	if desc.APIVersion == "" {
		return fmt.Errorf("provider APIVersion cannot be empty")
	}

	// Version is required
	if desc.Version == nil {
		return fmt.Errorf("provider version cannot be nil")
	}

	// Description is required
	if desc.Description == "" {
		return fmt.Errorf("provider description cannot be empty")
	}

	// At least one capability is required
	if len(desc.Capabilities) == 0 {
		return fmt.Errorf("provider must declare at least one capability")
	}

	// Validate all capabilities
	for i, cap := range desc.Capabilities {
		if !cap.IsValid() {
			return fmt.Errorf("capability at index %d (%q) is not valid", i, cap)
		}
	}

	// Validate schema property types if schema is defined
	if desc.Schema != nil {
		validTypes := map[string]bool{
			"string": true, "integer": true, "number": true,
			"boolean": true, "array": true, "object": true,
			"": true, // empty type means "any"
		}
		for propName, propDef := range desc.Schema.Properties {
			if !validTypes[propDef.Type] {
				return fmt.Errorf("property %q has invalid type %q", propName, propDef.Type)
			}
		}
	}

	// Validate output schemas using the centralized validation function
	if err := ValidateDescriptor(desc); err != nil {
		return fmt.Errorf("output schema validation failed: %w", err)
	}

	return nil
}

// globalRegistry is the default package-level registry.
var globalRegistry = NewRegistry()

// Register registers a provider in the global registry.
func Register(p Provider) error {
	return globalRegistry.Register(p)
}

// Get retrieves a provider from the global registry.
func Get(name string) (Provider, bool) {
	return globalRegistry.Get(name)
}

// Has checks if a provider is registered in the global registry.
func Has(name string) bool {
	return globalRegistry.Has(name)
}

// List returns all provider names from the global registry.
func List() []string {
	return globalRegistry.List()
}

// ListProviders returns all providers from the global registry.
func ListProviders() []Provider {
	return globalRegistry.ListProviders()
}

// ListByCapability returns providers with the given capability from the global registry.
func ListByCapability(capability Capability) []Provider {
	return globalRegistry.ListByCapability(capability)
}

// ListByCategory returns providers in the given category from the global registry.
func ListByCategory(category string) []Provider {
	return globalRegistry.ListByCategory(category)
}

// Count returns the number of providers in the global registry.
func Count() int {
	return globalRegistry.Count()
}

// GetGlobalRegistry returns the global registry instance.
// This is useful for testing or when you need direct access to the registry.
func GetGlobalRegistry() *Registry {
	return globalRegistry
}

// ResetGlobalRegistry clears the global registry.
// This is primarily for testing purposes.
func ResetGlobalRegistry() {
	globalRegistry.Clear()
}
