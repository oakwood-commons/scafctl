// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// CatalogIdentity pairs a stable canonical origin with the user-facing alias.
// The canonical form is derived from the actual registry location (not the
// config alias), making it rename-proof and portable across machines.
//
// Create instances via NewCatalogIdentity or IdentityFromCatalog to ensure
// the registry hash is precomputed.
type CatalogIdentity struct {
	// Canonical is the stable, machine-independent identifier derived from
	// the catalog's actual location. For OCI catalogs this is
	// "registry/repository" (e.g. "ghcr.io/acme/plugins"). For filesystem
	// catalogs this is the absolute path.
	Canonical string

	// Alias is the user-facing name from the config file (e.g. "production").
	// Used for display and logging only — never as a cache key.
	Alias string

	// hash is the precomputed registry hash (16-char hex). Populated at
	// construction time to avoid repeated SHA-256 computation.
	hash string
}

// NewCatalogIdentity creates a CatalogIdentity with a precomputed registry hash.
func NewCatalogIdentity(canonical, alias string) CatalogIdentity {
	id := CatalogIdentity{Canonical: canonical, Alias: alias}
	id.hash = id.computeHash()
	return id
}

// registryHashLen is the number of bytes from the SHA-256 hash used to form
// the 16-character hex registry hash directory name (8 bytes = 16 hex chars).
const registryHashLen = 8

// RegistryHash returns a collision-resistant hash of the canonical registry ID.
// The result is a 16-character lowercase hex string derived from SHA-256,
// used as a directory level to isolate same-name plugins from different catalogs.
// Returns an empty string for zero identities (no registry known).
//
// When constructed via NewCatalogIdentity or IdentityFromCatalog, this returns
// the precomputed value with no allocation. For bare struct literals (e.g. in
// tests), it computes and returns the hash without caching.
func (id CatalogIdentity) RegistryHash() string {
	if id.hash != "" {
		return id.hash
	}
	return id.computeHash()
}

// computeHash derives the registry hash from the canonical field.
func (id CatalogIdentity) computeHash() string {
	if id.Canonical == "" {
		return ""
	}
	h := sha256.Sum256([]byte(id.Canonical))
	return hex.EncodeToString(h[:registryHashLen])
}

// CanonicalFromCacheKey is no longer applicable with hash-based cache keys
// (hashes are one-way). This function is retained for backward compatibility
// with the tilde-encoded path segments used in the on-disk layout.
func CanonicalFromCacheKey(cacheKey string) string {
	return strings.ReplaceAll(cacheKey, cacheKeySeparatorReplacement, "/")
}

// cacheKeySeparatorReplacement is used to encode path separators in canonical
// IDs for the on-disk directory segment (kept for disk path layout).
const cacheKeySeparatorReplacement = "~"

// IsZero reports whether the identity has no canonical value set.
func (id CatalogIdentity) IsZero() bool {
	return id.Canonical == ""
}

// String returns the canonical ID for logging and display. If an alias is
// available, it is included in parentheses.
func (id CatalogIdentity) String() string {
	if id.Alias != "" && id.Alias != id.Canonical {
		return id.Canonical + " (" + id.Alias + ")"
	}
	return id.Canonical
}

// registryRepositoryCatalog is satisfied by catalog implementations that expose
// their OCI registry and repository (e.g. RemoteCatalog).
type registryRepositoryCatalog interface {
	Name() string
	Registry() string
	Repository() string
}

// pathCatalog is satisfied by catalog implementations that expose a local
// filesystem path (e.g. LocalCatalog).
type pathCatalog interface {
	Name() string
	Path() string
}

// IdentityFromCatalog derives a CatalogIdentity from a catalog implementation.
// It uses type assertions to extract the canonical location:
//   - OCI catalogs (Registry+Repository): canonical = "registry/repository"
//   - Filesystem catalogs (Path): canonical = absolute path
//   - Fallback: canonical = catalog.Name() (best effort)
func IdentityFromCatalog(cat interface{ Name() string }) CatalogIdentity {
	alias := cat.Name()

	if rc, ok := cat.(registryRepositoryCatalog); ok {
		canonical := rc.Registry()
		if repo := rc.Repository(); repo != "" {
			canonical += "/" + repo
		}
		return NewCatalogIdentity(canonical, alias)
	}

	if pc, ok := cat.(pathCatalog); ok {
		return NewCatalogIdentity(pc.Path(), alias)
	}

	// Fallback: use the name as both canonical and alias.
	return NewCatalogIdentity(alias, alias)
}

// ResolveAllowedCanonicals converts a list of user-provided catalog names
// (which may be aliases or canonical URLs) into a set of canonical IDs for
// fast lookup. It resolves aliases via the provided catalogs slice.
//
// Names that don't match any known catalog alias are treated as literal
// canonical IDs (allowing users to specify URLs directly in the allowlist).
func ResolveAllowedCanonicals(allowed []string, catalogs []interface{ Name() string }) map[string]bool {
	if len(allowed) == 0 {
		return nil
	}

	// Build alias → canonical mapping.
	byAlias := make(map[string]string, len(catalogs))
	for _, cat := range catalogs {
		id := IdentityFromCatalog(cat)
		byAlias[strings.ToLower(id.Alias)] = strings.ToLower(id.Canonical)
	}

	result := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		lower := strings.ToLower(name)
		if canonical, ok := byAlias[lower]; ok {
			result[canonical] = true
		} else {
			// Not a known alias — treat as literal canonical.
			result[lower] = true
		}
	}
	return result
}
