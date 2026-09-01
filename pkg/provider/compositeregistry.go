package provider

import (
	"fmt"
	"sync"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/store"
	"github.com/oakwood-commons/scafctl/pkg/versionindex"
)

// TODO Duplicate registration still silently overwrites (unchanged)
const VersionLatest = "latest"

type identity struct {
	name    string
	catalog string
}

type versionIdentity struct {
	identity
	version string
}
type VersionedRegistry struct {
	mu                 sync.RWMutex
	index              versionindex.Index[identity]
	store              store.Storer[versionIdentity, Provider]
	validateDescriptor func(desc *Descriptor) error
}

// NewVersionedRegistry returns an empty VersionedRegistry ready for use.
func NewVersionedRegistry() *VersionedRegistry {
	return &VersionedRegistry{
		index: *versionindex.New[identity](),
		store: store.New[versionIdentity, Provider](),
	}
}

type VersionedRegistryOption struct {
	catalogName         string
	versionOrConstraint string
	registrationVersion *semver.Version // authoritative key used by Register; overrides desc.Version
}

type VersionedRegistryOptionFunc func(*VersionedRegistryOption)

// WithCatalogName scopes a registry operation to the given catalog.
func WithCatalogName(name string) VersionedRegistryOptionFunc {
	return func(opt *VersionedRegistryOption) {
		opt.catalogName = name
	}
}

// WithVersionOrConstraint sets a version string or semver constraint used to
// resolve a provider during Get. It is ignored during Register.
func WithVersionOrConstraint(versionOrConstraint string) VersionedRegistryOptionFunc {
	return func(opt *VersionedRegistryOption) {
		opt.versionOrConstraint = versionOrConstraint
	}
}

// WithRegistrationVersion sets the authoritative catalog version used as the
// identity key when registering an external provider. It is required by
// Register and must be the concrete resolved version from the plugin catalog —
// never the provider's self-reported descriptor version.
func WithRegistrationVersion(v *semver.Version) VersionedRegistryOptionFunc {
	return func(opt *VersionedRegistryOption) {
		opt.registrationVersion = v
	}
}

func applyOpts(opts []VersionedRegistryOptionFunc) *VersionedRegistryOption {
	optStruct := &VersionedRegistryOption{}
	for _, opt := range opts {
		opt(optStruct)
	}
	return optStruct
}

// Register adds a provider to the registry under its descriptor name and the
// concrete version supplied via WithRegistrationVersion. A nil provider, a nil
// descriptor, or a missing registration version is rejected with an error.
func (vr *VersionedRegistry) Register(provider Provider, opts ...VersionedRegistryOptionFunc) error {
	optStruct := applyOpts(opts)

	if provider == nil {
		return fmt.Errorf("cannot register nil provider")
	}

	desc := provider.Descriptor()
	if desc == nil {
		return fmt.Errorf("provider descriptor cannot be nil")
	}

	if vr.validateDescriptor != nil {
		if err := vr.validateDescriptor(desc); err != nil {
			return fmt.Errorf("provider descriptor validation failed: %w", err)
		}
	}

	if optStruct.registrationVersion == nil {
		return fmt.Errorf("provider %q: registration version is required (use WithRegistrationVersion)", desc.Name)
	}

	id := identity{
		name:    desc.Name,
		catalog: optStruct.catalogName,
	}

	vr.mu.Lock()
	vr.index.AddVersion(id, optStruct.registrationVersion)
	vr.store.Set(versionIdentity{identity: id, version: optStruct.registrationVersion.String()}, provider)
	vr.mu.Unlock()

	return nil
}

// Get resolves a provider by name. If WithRegistrationVersion is set it takes
// precedence for an exact-version lookup. Otherwise the versionOrConstraint
// string is parsed: an empty string or "latest" returns the highest registered
// version, an exact version returns that version, and a constraint returns the
// highest satisfying version.
func (vr *VersionedRegistry) Get(name string, opts ...VersionedRegistryOptionFunc) (Provider, bool) {
	optStruct := applyOpts(opts)

	id := identity{
		name:    name,
		catalog: optStruct.catalogName,
	}
	if optStruct.registrationVersion != nil { // takes precedence over versionOrConstraint; allows exact lookup by the concrete registered version
		return vr.getByVersion(id, optStruct.registrationVersion)
	}
	versionStr := optStruct.versionOrConstraint
	if versionStr == "" || versionStr == VersionLatest {
		return vr.getLatest(id)
	}
	v, c, err := versionindex.ParseVersionOrConstraint(versionStr)
	if err != nil {
		return nil, false
	}

	switch {
	case v != nil:
		return vr.getByVersion(id, v)
	case c != nil:
		return vr.getByConstraints(id, c)
	}
	return nil, false
}

// Has reports whether a provider is registered under name that satisfies the
// requested catalog and version/constraint. It is Get-shaped, not index-shaped:
// an entry exists only when a concrete version resolves (empty/latest picks the
// highest, otherwise the exact version or the highest satisfying a constraint).
// An unparseable version/constraint reports false.
func (vr *VersionedRegistry) Has(name string, opts ...VersionedRegistryOptionFunc) bool {
	_, ok := vr.Get(name, opts...)
	return ok
}

func (vr *VersionedRegistry) getByVersion(id identity, v *semver.Version) (Provider, bool) {
	vr.mu.RLock()
	defer vr.mu.RUnlock()
	resolvedVersion := vr.index.GetVersion(id, v)
	if resolvedVersion == nil {
		return nil, false
	}
	provider, ok := vr.store.Get(versionIdentity{identity: id, version: resolvedVersion.String()})

	return provider, ok
}

func (vr *VersionedRegistry) getByConstraints(id identity, c *semver.Constraints) (Provider, bool) {
	vr.mu.RLock()
	defer vr.mu.RUnlock()
	resolvedVersion := vr.index.GetConstraints(id, c)
	if resolvedVersion == nil {
		return nil, false
	}
	provider, ok := vr.store.Get(versionIdentity{identity: id, version: resolvedVersion.String()})
	return provider, ok
}

func (vr *VersionedRegistry) getLatest(id identity) (Provider, bool) {
	vr.mu.RLock()
	defer vr.mu.RUnlock()
	v := vr.index.GetLatest(id)
	if v == nil {
		return nil, false
	}
	provider, ok := vr.store.Get(versionIdentity{identity: id, version: v.String()})
	return provider, ok
}

// Unregister removes the provider registered under name at the given version.
// A nil version is rejected with an error.
func (vr *VersionedRegistry) Unregister(name string, v *semver.Version, opts ...VersionedRegistryOptionFunc) error {
	if v == nil {
		return fmt.Errorf("version cannot be nil for unregistering provider %q", name)
	}
	optStruct := applyOpts(opts)

	id := identity{
		name:    name,
		catalog: optStruct.catalogName,
	}

	vr.mu.Lock()
	defer vr.mu.Unlock()

	vr.index.DeleteVersion(id, v)
	vr.store.Delete(versionIdentity{identity: id, version: v.String()})

	return nil
}

// CompositeRegistry is the single lookup surface for both built-in and external
// providers. It is two tiers behind one door:
//
//   - builtins: name-only identity, written once before the pool exists, never
//     evicted. The descriptor may carry a version, but it is metadata:
//     built-ins are never matched against a version constraint.
//   - external: {catalog, name, version} identity, churned by the plugin pool
//     as plugins are spawned and evicted.
//
// Both tiers are internally synchronized, so CompositeRegistry holds no lock of
// its own: it is a pure router. Built-in-vs-external classification happens
// ABOVE this type (at the reference boundary); the caller states which tier it
// means and the registry never guesses.
type CompositeRegistry struct {
	base     *Registry
	external *VersionedRegistry
}

// NewCompositeRegistryFromBase creates a CompositeRegistry that wraps an
// existing built-in Registry and a fresh VersionedRegistry for externals.
func NewCompositeRegistryFromBase(builtins *Registry) *CompositeRegistry {
	return &CompositeRegistry{
		base:     builtins,
		external: NewVersionedRegistry(),
	}
}

// NewCompositeRegistry creates a CompositeRegistry with empty built-in and
// external registries.
func NewCompositeRegistry() *CompositeRegistry {
	return &CompositeRegistry{
		base:     NewRegistry(),
		external: NewVersionedRegistry(),
	}
}

// RegisterBase adds a built-in provider under its bare name. It is called at
// startup, before the pool is created. A second built-in of the same name is a
// programmer error (there is only ever one `shell`, one `env`), so it is
// rejected rather than silently overwritten.
func (r *CompositeRegistry) RegisterBase(p Provider) error {
	return r.base.Register(p)
}

// GetBase resolves a built-in by name. Version/constraint is intentionally
// absent: built-ins are not versioned in the identity sense.
func (r *CompositeRegistry) GetBase(name string) (Provider, bool) {
	return r.base.Get(name)
}

// RegisterExternal adds an external (plugin-provided) provider to the versioned
// tier of the registry. The caller must supply WithRegistrationVersion.
func (r *CompositeRegistry) RegisterExternal(p Provider, opts ...VersionedRegistryOptionFunc) error {
	return r.external.Register(p, opts...)
}

// GetExternal resolves an external provider by name, catalog, and
// version/constraint.
func (r *CompositeRegistry) GetExternal(name string, opts ...VersionedRegistryOptionFunc) (Provider, bool) {
	return r.external.Get(name, opts...)
}

// HasExternal reports whether an external provider satisfying the requested
// catalog and version/constraint is registered. It is the dedup check the
// plugin pool performs before registering a freshly spawned provider.
func (r *CompositeRegistry) HasExternal(name string, opts ...VersionedRegistryOptionFunc) bool {
	return r.external.Has(name, opts...)
}

// UnregisterExternal removes an external provider registered at the given
// version. Delegates to VersionedRegistry.Unregister.
func (r *CompositeRegistry) UnregisterExternal(name string, v *semver.Version, opts ...VersionedRegistryOptionFunc) error {
	return r.external.Unregister(name, v, opts...)
}

// HasBase reports whether a built-in/base provider with the given name is
// registered.
func (r *CompositeRegistry) HasBase(name string) bool {
	return r.base.Has(name)
}

// ListBase returns the names of all registered built-in/base providers.
func (r *CompositeRegistry) ListBase() []string {
	return r.base.List()
}
