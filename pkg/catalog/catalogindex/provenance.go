// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalogindex

import (
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// DisplayCatalog returns the user-facing catalog name for an artifact row:
//
//   - built artifacts (local store, no canonical origin) are "local";
//   - rows whose canonical origin is a configured remote catalog show that
//     catalog's alias (first-wins when several aliases share an origin);
//   - remote rows from an unconfigured origin keep the catalog they were
//     listed from;
//   - cached copies of an unconfigured origin have no alias ("").
//
// Safe to call on a nil Index (no alias lookups succeed).
func (idx *Index) DisplayCatalog(a catalog.ArtifactInfo) string {
	if a.IsBuilt() {
		return catalog.LocalCatalogName
	}
	if a.Canonical != "" {
		if alias, ok := idx.AliasForRegistry(a.Canonical); ok {
			return alias
		}
	}
	if a.IsLocalStore() {
		return ""
	}
	return a.Catalog
}

// MatchesCatalog reports whether an artifact row belongs to the named catalog,
// matched case-insensitively. "local" selects every row with content in the
// local store (built and cached). Any other name selects rows whose display
// catalog or listed remote catalog equals it. An empty name matches nothing.
func (idx *Index) MatchesCatalog(a catalog.ArtifactInfo, name string) bool {
	if name == "" {
		return false
	}
	if strings.EqualFold(name, catalog.LocalCatalogName) {
		return a.HasLocation(catalog.LocationLocal)
	}
	if !a.IsLocalStore() && strings.EqualFold(a.Catalog, name) {
		return true
	}
	return strings.EqualFold(idx.DisplayCatalog(a), name)
}
