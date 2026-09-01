// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package catalogindex provides a single, config-derived topology of the
// configured catalogs. It consolidates every mapping between a catalog's
// user-facing alias, its canonical origin (OCI "registry/repository" for remote
// catalogs, or the absolute filesystem path for local ones), and its registry
// hash into one immutable object built once from config.
//
// The Index replaces the previously separate CatalogAliasResolver (alias <->
// registry) and the ad-hoc CatalogIdentity machinery, offering bidirectional
// lookups by alias or canonical, kind-strict remote/local accessors, and a
// stable sorted listing.
package catalogindex

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// CatalogKind distinguishes remote (OCI) catalogs from local (filesystem) ones.
// The kind governs how a canonical value is normalized for lookups: remote OCI
// names are case-insensitive (lowercased), while filesystem paths are
// case-sensitive on some platforms and preserved as-is.
type CatalogKind uint8

const (
	// CatalogKindRemote identifies an OCI registry-backed catalog whose
	// canonical origin is "registry" or "registry/repository".
	CatalogKindRemote CatalogKind = iota
	// CatalogKindLocal identifies a filesystem catalog whose canonical origin
	// is an absolute path.
	CatalogKindLocal
)

// registryHashLen is the number of SHA-256 bytes used to form the 16-character
// hex registry hash directory name (8 bytes = 16 hex chars).
const registryHashLen = 8

// CatalogIdentity pairs a configured catalog's user-facing alias with its
// stable canonical origin and kind. The canonical form is derived from the
// catalog's actual location (not the alias), making it rename-proof and
// portable across machines. Construct instances through an Index; the registry
// hash is precomputed at that point.
type CatalogIdentity struct {
	// Alias is the user-facing name from the config file (e.g. "production").
	// Used for display and lookups only -- never as an on-disk cache key.
	Alias string

	// Canonical is the stable, machine-independent identifier derived from the
	// catalog's actual location. For remote catalogs this is a lowercased
	// "registry/repository" (e.g. "ghcr.io/acme/plugins") -- OCI names are
	// case-insensitive, so the registry hash is computed over the lowercased
	// form. For local catalogs this is the absolute filesystem path, preserved
	// in its original case (paths are case-sensitive on some platforms).
	Canonical string

	// Kind reports whether this is a remote (OCI) or local (filesystem) catalog.
	Kind CatalogKind

	// hash is the precomputed 16-char hex registry hash, populated at
	// construction to avoid repeated SHA-256 computation.
	hash string
}

// newIdentity builds a CatalogIdentity with a precomputed registry hash.
func newIdentity(alias, canonical string, kind CatalogKind) CatalogIdentity {
	id := CatalogIdentity{Alias: alias, Canonical: canonical, Kind: kind}
	id.hash = id.computeHash()
	return id
}

// RegistryHash returns a collision-resistant hash of the canonical origin. The
// result is a 16-character lowercase hex string derived from SHA-256, used as a
// directory level to isolate same-name plugins from different catalogs. Returns
// an empty string for zero identities (no canonical known).
func (id CatalogIdentity) RegistryHash() string {
	if id.hash != "" {
		return id.hash
	}
	return id.computeHash()
}

// computeHash derives the registry hash from the canonical value (already
// lowercased for remote catalogs at construction).
func (id CatalogIdentity) computeHash() string {
	if id.Canonical == "" {
		return ""
	}
	h := sha256.Sum256([]byte(id.Canonical))
	return hex.EncodeToString(h[:registryHashLen])
}

// IsZero reports whether the identity has no canonical value set.
func (id CatalogIdentity) IsZero() bool { return id.Canonical == "" }

// IsRemote reports whether the identity is a remote (OCI) catalog.
func (id CatalogIdentity) IsRemote() bool { return id.Kind == CatalogKindRemote }

// IsLocal reports whether the identity is a local (filesystem) catalog.
func (id CatalogIdentity) IsLocal() bool { return id.Kind == CatalogKindLocal }

// String returns the canonical origin for logging and display. When an alias
// distinct from the canonical is available, it is appended in parentheses.
func (id CatalogIdentity) String() string {
	if id.Alias != "" && id.Alias != id.Canonical {
		return id.Canonical + " (" + id.Alias + ")"
	}
	return id.Canonical
}

// normKey normalizes a canonical value for map lookups. Remote (OCI) canonicals
// are case-insensitive per spec and are lowercased; local filesystem paths are
// case-sensitive on some platforms and preserved as-is.
func normKey(canonical string, kind CatalogKind) string {
	if kind == CatalogKindRemote {
		return strings.ToLower(canonical)
	}
	return canonical
}

// Index is a config-derived topology of the configured catalogs. It answers
// lookups in both directions (alias <-> canonical), kind-strict remote and
// local accessors, and a listing in definition order, and it can gate fetches
// through an optional catalog allowlist. Build one with FromConfig (from the
// static config) or FromChain (from a live catalog chain); attach a gate with
// WithAllowed, which returns a COPY so the base Index can be built once and
// shared (see WithIndex/FromContext). All methods are safe on a nil receiver
// (every lookup misses and the gate allows everything).
type Index struct {
	byAlias     map[string]CatalogIdentity // lower(alias) -> identity
	byCanonical map[string]CatalogIdentity // normKey(canonical, kind) -> identity
	ordered     []CatalogIdentity          // definition order, precomputed
	allowed     map[string]bool            // lower(alias) -> allowed; nil = allow all
	// pluginPolicies gates which plugin NAMES each catalog may serve, keyed by
	// lower(alias). nil means no per-catalog plugin restriction (all plugins
	// allowed from any catalog). When non-nil, a catalog absent from the map is
	// deny-all, mirroring catalog.BuildCatalogChain's chain-side behavior.
	pluginPolicies map[string]catalog.PluginPolicy
}

// FromConfig builds an Index from the configured catalogs in a single pass.
// Remote catalogs (URL-bearing) and local catalogs (Path-bearing) are both
// indexed; a catalog with neither is skipped. When two catalogs share a
// canonical origin the first configured alias wins, and alias keys are likewise
// first-wins. The listing preserves config-definition order. A nil config
// yields an empty Index whose lookups all miss.
func FromConfig(cfg *config.Config) *Index {
	idx := newIndex()
	if cfg == nil {
		return idx
	}
	for _, c := range cfg.Catalogs {
		id, ok := identityFromCatalogConfig(c)
		if !ok {
			continue
		}
		idx.add(id)
	}
	return idx
}

// FromChain builds an Index by enumerating the catalogs in a (possibly chained)
// catalog, deriving each identity from the live catalog via type assertions:
// remote OCI catalogs expose Registry()+Repository(), local catalogs expose
// Path(). This mirrors the exact set the runtime fetches from (including the
// official and local catalogs injected into the chain). The listing preserves
// chain order. A nil catalog yields an empty Index whose lookups all miss.
func FromChain(cat catalog.Catalog) *Index {
	idx := newIndex()
	if cat == nil {
		return idx
	}
	type catalogsLister interface {
		Catalogs() []catalog.Catalog
	}
	var cats []catalog.Catalog
	if chain, ok := cat.(catalogsLister); ok {
		cats = chain.Catalogs()
	} else {
		cats = []catalog.Catalog{cat}
	}
	for _, c := range cats {
		if id, ok := identityFromCatalog(c); ok {
			idx.add(id)
		}
	}
	return idx
}

// newIndex allocates an empty Index with initialized lookup maps.
func newIndex() *Index {
	return &Index{
		byAlias:     make(map[string]CatalogIdentity),
		byCanonical: make(map[string]CatalogIdentity),
	}
}

// registryRepositoryCatalog is satisfied by remote OCI catalog implementations
// that expose their registry and repository (e.g. RemoteCatalog).
type registryRepositoryCatalog interface {
	Name() string
	Registry() string
	Repository() string
}

// pathCatalog is satisfied by filesystem catalog implementations that expose a
// local path (e.g. LocalCatalog).
type pathCatalog interface {
	Name() string
	Path() string
}

// identityFromCatalog derives a CatalogIdentity from a live catalog via type
// assertions. Remote OCI catalogs (Registry+Repository) yield a lowercased
// "registry/repository" canonical; filesystem catalogs (Path) yield the path.
// Decorator catalogs (e.g. AllowlistCatalog) are unwrapped via Inner() to reach
// the concrete implementation; the alias is taken from the outermost wrapper.
// The bool is false when the catalog exposes neither a usable registry nor a
// path.
func identityFromCatalog(cat interface{ Name() string }) (CatalogIdentity, bool) {
	alias := cat.Name()
	// Unwrap single-catalog decorators (e.g. AllowlistCatalog) to reach the
	// concrete catalog that exposes Registry()/Repository() or Path().
	inner := unwrapCatalog(cat)
	if rc, ok := inner.(registryRepositoryCatalog); ok {
		canonical := rc.Registry()
		if repo := rc.Repository(); repo != "" {
			canonical += "/" + repo
		}
		if canonical == "" {
			return CatalogIdentity{}, false
		}
		return newIdentity(alias, strings.ToLower(canonical), CatalogKindRemote), true
	}
	if pc, ok := inner.(pathCatalog); ok {
		if pc.Path() == "" {
			return CatalogIdentity{}, false
		}
		return newIdentity(alias, pc.Path(), CatalogKindLocal), true
	}
	return CatalogIdentity{}, false
}

// unwrapCatalog peels single-catalog decorator layers (e.g. AllowlistCatalog)
// by following Inner() until the concrete catalog is reached. The returned value
// is the innermost catalog that does not implement Inner().
func unwrapCatalog(cat interface{}) interface{} {
	for {
		u, ok := cat.(interface{ Inner() catalog.Catalog })
		if !ok {
			return cat
		}
		cat = u.Inner()
	}
}

// identityFromCatalogConfig derives a CatalogIdentity from a single catalog
// config entry. A URL marks it remote (canonical lowercased); otherwise a Path
// marks it local (case preserved). The bool is false when the entry has neither
// a usable URL nor a path.
func identityFromCatalogConfig(c config.CatalogConfig) (CatalogIdentity, bool) {
	if c.URL != "" {
		registry, repository := catalog.ParseCatalogURL(c.URL)
		if registry == "" {
			return CatalogIdentity{}, false
		}
		canonical := registry
		if repository != "" {
			canonical += "/" + repository
		}
		return newIdentity(c.Name, strings.ToLower(canonical), CatalogKindRemote), true
	}
	if c.Path != "" {
		// Normalize to a cleaned absolute path so the same catalog always yields
		// the same identity/hash regardless of the relative form configured, and
		// distinct catalogs in different working directories never collide.
		canonical := filepath.Clean(c.Path)
		if abs, err := filepath.Abs(c.Path); err == nil {
			canonical = abs
		}
		return newIdentity(c.Name, canonical, CatalogKindLocal), true
	}
	return CatalogIdentity{}, false
}

// add inserts an identity into both lookup maps (first-wins) and appends it to
// the config-order listing.
func (idx *Index) add(id CatalogIdentity) {
	if aliasKey := strings.ToLower(id.Alias); aliasKey != "" {
		if _, exists := idx.byAlias[aliasKey]; !exists {
			idx.byAlias[aliasKey] = id
		}
	}
	if canonKey := normKey(id.Canonical, id.Kind); canonKey != "" {
		if _, exists := idx.byCanonical[canonKey]; !exists {
			idx.byCanonical[canonKey] = id
		}
	}
	idx.ordered = append(idx.ordered, id)
}

// AliasForRegistry returns the configured alias for a raw OCI origin, matched
// case-insensitively. Kind-strict: only remote identities match. The bool is
// false when the origin names no configured remote catalog -- a fully-qualified
// reference against it must be rejected, not fetched.
func (idx *Index) AliasForRegistry(registry string) (string, bool) {
	if idx == nil {
		return "", false
	}
	id, ok := idx.byCanonical[normKey(registry, CatalogKindRemote)]
	if !ok || !id.IsRemote() {
		return "", false
	}
	return id.Alias, true
}

// RegistryForAlias returns the canonical OCI origin for a catalog alias, matched
// case-insensitively. Kind-strict: the bool is false when the alias names no
// remote catalog (e.g. a filesystem catalog or an unknown alias).
func (idx *Index) RegistryForAlias(alias string) (string, bool) {
	if idx == nil {
		return "", false
	}
	id, ok := idx.byAlias[strings.ToLower(alias)]
	if !ok || !id.IsRemote() {
		return "", false
	}
	return id.Canonical, true
}

// AliasForFile returns the configured alias for a local filesystem path, matched
// case-sensitively. Kind-strict: only local identities match. The bool is false
// when the path names no configured filesystem catalog.
func (idx *Index) AliasForFile(path string) (string, bool) {
	if idx == nil {
		return "", false
	}
	id, ok := idx.byCanonical[normKey(path, CatalogKindLocal)]
	if !ok || !id.IsLocal() {
		return "", false
	}
	return id.Alias, true
}

// FileForAlias returns the canonical filesystem path for a catalog alias,
// matched case-insensitively on the alias. Kind-strict: the bool is false when
// the alias names no local catalog.
func (idx *Index) FileForAlias(alias string) (string, bool) {
	if idx == nil {
		return "", false
	}
	id, ok := idx.byAlias[strings.ToLower(alias)]
	if !ok || !id.IsLocal() {
		return "", false
	}
	return id.Canonical, true
}

// IdentityForAlias returns the full identity for a catalog alias, matched
// case-insensitively, regardless of kind. The bool is false when the alias names
// no configured catalog.
func (idx *Index) IdentityForAlias(alias string) (CatalogIdentity, bool) {
	if idx == nil {
		return CatalogIdentity{}, false
	}
	id, ok := idx.byAlias[strings.ToLower(alias)]
	return id, ok
}

// IdentityForCanonical returns the full identity for a canonical origin,
// regardless of kind. Because the caller does not declare the kind, both the
// remote (lowercased) and local (case-preserving) normalizations are tried; a
// remote match wins when a value is ambiguous. The bool is false when no
// configured catalog has that canonical origin.
func (idx *Index) IdentityForCanonical(canonical string) (CatalogIdentity, bool) {
	if idx == nil {
		return CatalogIdentity{}, false
	}
	if id, ok := idx.byCanonical[strings.ToLower(canonical)]; ok {
		return id, true
	}
	if id, ok := idx.byCanonical[canonical]; ok {
		return id, true
	}
	return CatalogIdentity{}, false
}

// All returns the configured identities in config-definition order. The returned
// slice is the Index's own; treat it as read-only.
func (idx *Index) All() []CatalogIdentity {
	if idx == nil {
		return nil
	}
	return idx.ordered
}

// WithAllowed returns a COPY of the Index carrying the given allowlist gate,
// leaving the receiver unmodified. This is what makes an Index safe to build
// once and share: the shared topology (the alias/canonical maps) is read-only
// and reused by the copy, while the per-consumer allowlist policy lives only on
// the returned copy. Names are matched case-insensitively on the catalog alias;
// passing no names (or an all-empty list) leaves the gate open (every catalog
// allowed), matching an unset allowlist. Safe on a nil receiver (returns nil).
func (idx *Index) WithAllowed(names []string) *Index {
	if idx == nil {
		return nil
	}
	// Shallow copy: byAlias/byCanonical/ordered are shared read-only with the
	// receiver; only the allowed gate differs on the copy.
	clone := *idx
	allowed := make(map[string]bool, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		allowed[strings.ToLower(name)] = true
	}
	if len(allowed) == 0 {
		clone.allowed = nil
	} else {
		clone.allowed = allowed
	}
	return &clone
}

// HasAllowlist reports whether a non-empty allowlist gate is configured. When
// false, every catalog is permitted.
func (idx *Index) HasAllowlist() bool {
	return idx != nil && idx.allowed != nil
}

// CheckAllowed returns an error if the named catalog (an alias) is not permitted
// by the allowlist gate. With no gate configured every catalog is allowed. An
// empty alias is rejected when a gate is set, since an unverifiable origin must
// not pass a restrictive allowlist. Safe on a nil receiver (allows everything).
func (idx *Index) CheckAllowed(alias string) error {
	if idx == nil || idx.allowed == nil {
		return nil
	}
	if !idx.allowed[strings.ToLower(alias)] {
		return fmt.Errorf("catalog %q is not in the allowed catalogs list", alias)
	}
	return nil
}

// WithPluginPolicies returns a COPY of the Index carrying a per-catalog plugin
// allowlist gate, keyed on catalog alias (matched case-insensitively), leaving
// the receiver unmodified. Like WithAllowed, the shared topology maps are reused
// read-only and only the policy differs on the copy, so the base Index stays
// safe to build once and share. Passing no policies (nil or empty) leaves the
// gate open (every plugin allowed from every catalog). Safe on a nil receiver
// (returns nil).
//
// The policy semantics mirror catalog.BuildCatalogChain so cache-served and
// chain-served fetches agree: an unset map allows all; when the map is set, a
// catalog present with AllowAll is unrestricted, a catalog present with an
// explicit list serves only those plugin names, and a catalog ABSENT from the
// map is deny-all.
func (idx *Index) WithPluginPolicies(policies map[string]catalog.PluginPolicy) *Index {
	if idx == nil {
		return nil
	}
	// Shallow copy: topology maps are shared read-only; only the policy differs.
	clone := *idx
	if len(policies) == 0 {
		clone.pluginPolicies = nil
		return &clone
	}
	m := make(map[string]catalog.PluginPolicy, len(policies))
	for name, p := range policies {
		m[strings.ToLower(name)] = p
	}
	clone.pluginPolicies = m
	return &clone
}

// HasPluginPolicies reports whether a per-catalog plugin allowlist gate is
// configured. When false, every plugin is permitted from every catalog.
func (idx *Index) HasPluginPolicies() bool {
	return idx != nil && idx.pluginPolicies != nil
}

// CheckPluginAllowed returns an error if pluginName may not be served by the
// catalog named by alias, per the per-catalog plugin allowlist gate. This is
// the cache-safe complement to the chain's AllowlistCatalog decorator: it must
// be consulted on EVERY plugin return path (including cache and lock hits, which
// never touch the chain) so a tightened per-catalog policy cannot be bypassed by
// an already-cached binary.
//
// With no policy gate configured every plugin is allowed. When a gate is set:
// an empty alias is rejected (an unverifiable origin must not pass a restrictive
// policy), a catalog absent from the policy map is deny-all (parity with
// catalog.BuildCatalogChain), a catalog with AllowAll is unrestricted, and
// otherwise the plugin name must appear in the catalog's explicit list. Safe on
// a nil receiver (allows everything).
func (idx *Index) CheckPluginAllowed(alias, pluginName string) error {
	if idx == nil {
		return nil
	}
	return catalog.CheckPluginPolicy(idx.pluginPolicies, alias, pluginName)
}
