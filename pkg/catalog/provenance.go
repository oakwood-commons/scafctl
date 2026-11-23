// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"slices"
	"strings"
)

// ArtifactLocation names where an artifact's content exists.
type ArtifactLocation string

const (
	// LocationLocal means the content is in the local catalog store, either
	// built here or cached from a remote catalog.
	LocationLocal ArtifactLocation = "local"
	// LocationRemote means the content is published in a remote catalog.
	LocationRemote ArtifactLocation = "remote"
)

// locationOrder is the canonical display order for locations.
var locationOrder = []ArtifactLocation{LocationLocal, LocationRemote}

// locationSeparator joins locations for display.
const locationSeparator = ", "

// originCatalogPrefix namespaces the origin key of a non-local row that has no
// canonical identity, so it can never collide with a real canonical value.
const originCatalogPrefix = "catalog:"

// IsLocalStore reports whether the row was read from the local catalog store.
func (a ArtifactInfo) IsLocalStore() bool {
	return a.Catalog == LocalCatalogName
}

// IsBuilt reports whether the row is a local artifact built on this machine,
// i.e. a local store row with no remote provenance.
func (a ArtifactInfo) IsBuilt() bool {
	return a.IsLocalStore() && a.Canonical == ""
}

// OriginKey returns a stable key for the artifact's origin: the lowercased
// canonical registry identity for remote rows and cached copies, the empty
// string for locally built artifacts, and a namespaced catalog name for
// non-local rows that carry no canonical identity. Rows with equal origin keys
// describe the same published artifact and may be merged.
func (a ArtifactInfo) OriginKey() string {
	if a.Canonical != "" {
		return strings.ToLower(a.Canonical)
	}
	if a.IsLocalStore() {
		return ""
	}
	return originCatalogPrefix + a.Catalog
}

// ResolvedLocations returns the row's locations in canonical order. When
// Locations is unset, the location is derived from the source catalog.
func (a ArtifactInfo) ResolvedLocations() []ArtifactLocation {
	if len(a.Locations) == 0 {
		if a.IsLocalStore() {
			return []ArtifactLocation{LocationLocal}
		}
		return []ArtifactLocation{LocationRemote}
	}
	return sortLocations(a.Locations)
}

// HasLocation reports whether the row's content exists at loc.
func (a ArtifactInfo) HasLocation(loc ArtifactLocation) bool {
	return slices.Contains(a.ResolvedLocations(), loc)
}

// FormatLocations renders locations in canonical order, comma separated.
func FormatLocations(locs []ArtifactLocation) string {
	sorted := sortLocations(locs)
	parts := make([]string, len(sorted))
	for i, l := range sorted {
		parts[i] = string(l)
	}
	return strings.Join(parts, locationSeparator)
}

// sortLocations returns a de-duplicated copy of locs in canonical order.
// Unknown locations are dropped.
func sortLocations(locs []ArtifactLocation) []ArtifactLocation {
	out := make([]ArtifactLocation, 0, len(locationOrder))
	for _, l := range locationOrder {
		if slices.Contains(locs, l) {
			out = append(out, l)
		}
	}
	return out
}
