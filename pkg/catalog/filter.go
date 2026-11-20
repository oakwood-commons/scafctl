// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/Masterminds/semver/v3"
)

// FilterByVersionConstraint filters artifacts by a semver constraint (e.g.
// ">=1.2.0", "~1.4"). An empty constraint returns the input unchanged. Matching
// artifacts are returned sorted by version descending (newest first). Artifacts
// without a parsed version are excluded when a constraint is supplied.
//
// This is the shared implementation used by both the CLI (`catalog list
// --name X <constraint>`) and the MCP catalog_list_plugins tool so version
// filtering behaves identically across surfaces.
func FilterByVersionConstraint(artifacts []ArtifactInfo, constraint string) ([]ArtifactInfo, error) {
	if constraint == "" {
		return artifacts, nil
	}

	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	result := make([]ArtifactInfo, 0, len(artifacts))
	for _, a := range artifacts {
		if a.Reference.Version == nil {
			continue
		}
		if c.Check(a.Reference.Version) {
			result = append(result, a)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		vi := result[i].Reference.Version
		vj := result[j].Reference.Version
		if vi == nil {
			return false
		}
		if vj == nil {
			return true
		}
		return vj.LessThan(vi) // descending
	})
	return result, nil
}

// DeduplicateArtifacts merges rows that describe the same published artifact:
// equal kind, name, tag, and origin (see ArtifactInfo.OriginKey). A cached
// local copy therefore merges with the remote row it was pulled from, while a
// locally built artifact never merges with a remote one, and the same version
// published to two registries stays as two rows.
//
// Within one key, rows merge when their digests are equal or either is unknown
// (remote list rows carry no digest). When both digests are known and differ,
// the rows stay separate and the local-only row is marked Stale.
//
// Every returned row has Locations set (local, remote, or both). Metadata the
// first row lacks (digest, createdAt, size, annotations) is filled from the
// rows merged into it, and a merged row reports the remote catalog name. The
// order of first appearance is preserved and inputs are not mutated.
//
// Shared by the CLI list rendering and the MCP catalog_list_plugins tool so a
// plugin present in multiple catalogs is reported identically on both surfaces.
func DeduplicateArtifacts(artifacts []ArtifactInfo) []ArtifactInfo {
	type dedupKey struct {
		name   string
		tag    string
		origin string
		kind   ArtifactKind
	}

	groups := make(map[dedupKey][]int, len(artifacts)) // key -> indices in result
	order := make([]dedupKey, 0, len(artifacts))
	result := make([]ArtifactInfo, 0, len(artifacts))

	for _, a := range artifacts {
		k := dedupKey{name: a.Reference.Name, tag: artifactTag(a), origin: a.OriginKey(), kind: a.Reference.Kind}
		merged := false
		for _, idx := range groups[k] {
			if digestsCompatible(result[idx].Digest, a.Digest) {
				mergeArtifact(&result[idx], a)
				merged = true
				break
			}
		}
		if merged {
			continue
		}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], len(result))
		result = append(result, cloneArtifact(a))
	}

	for _, k := range order {
		markStale(result, groups[k])
	}
	return result
}

// artifactTag returns the row's tag, falling back to its version.
func artifactTag(a ArtifactInfo) string {
	if a.Tag == "" && a.Reference.Version != nil {
		return a.Reference.Version.String()
	}
	return a.Tag
}

// digestsCompatible reports whether two rows may describe the same content:
// their digests are equal, or at least one is unknown.
func digestsCompatible(a, b string) bool {
	return a == "" || b == "" || a == b
}

// cloneArtifact copies a row so merging never mutates the caller's data, and
// resolves its locations.
func cloneArtifact(a ArtifactInfo) ArtifactInfo {
	a.Locations = a.ResolvedLocations()
	if a.Annotations != nil {
		a.Annotations = maps.Clone(a.Annotations)
	}
	return a
}

// mergeArtifact folds src into dst: locations are unioned and metadata dst
// lacks is filled from src. A merged row reports the remote catalog name.
func mergeArtifact(dst *ArtifactInfo, src ArtifactInfo) {
	dst.Locations = sortLocations(append(dst.Locations, src.ResolvedLocations()...))
	if dst.IsLocalStore() && !src.IsLocalStore() {
		dst.Catalog = src.Catalog
	}
	if dst.Canonical == "" {
		dst.Canonical = src.Canonical
	}
	if dst.Digest == "" {
		dst.Digest = src.Digest
	}
	if dst.CreatedAt.IsZero() {
		dst.CreatedAt = src.CreatedAt
	}
	if dst.Size == 0 {
		dst.Size = src.Size
	}
	if dst.ImageRef == "" {
		dst.ImageRef = src.ImageRef
	}
	for k, v := range src.Annotations {
		if dst.Annotations == nil {
			dst.Annotations = make(map[string]string, len(src.Annotations))
		}
		if _, found := dst.Annotations[k]; !found {
			dst.Annotations[k] = v
		}
	}
	// Content present in both places is, by construction, not stale.
	if dst.HasLocation(LocationLocal) && dst.HasLocation(LocationRemote) {
		dst.Stale = false
	}
}

// markStale flags local-only rows in one dedup group whose known digest
// differs from a remote row's known digest.
func markStale(rows []ArtifactInfo, idxs []int) {
	if len(idxs) < 2 {
		return
	}
	for _, i := range idxs {
		local := &rows[i]
		if local.Digest == "" || !local.HasLocation(LocationLocal) || local.HasLocation(LocationRemote) {
			continue
		}
		for _, j := range idxs {
			remote := rows[j]
			if j != i && remote.HasLocation(LocationRemote) && remote.Digest != "" && remote.Digest != local.Digest {
				local.Stale = true
				break
			}
		}
	}
}

// LatestPerOrigin keeps, for each kind + name + origin, every row at the
// highest version. Keeping all rows at the top version (not just one) means a
// stale cached copy stays visible next to the remote row it diverged from.
// Each origin keeps its own latest, so a locally built artifact and the
// published one are both shown. Rows without a version are dropped when the
// key has a versioned row and kept otherwise. Input order is preserved.
func LatestPerOrigin(artifacts []ArtifactInfo) []ArtifactInfo {
	type latestKey struct {
		name   string
		origin string
		kind   ArtifactKind
	}
	keyOf := func(a ArtifactInfo) latestKey {
		return latestKey{name: a.Reference.Name, origin: a.OriginKey(), kind: a.Reference.Kind}
	}

	best := make(map[latestKey]*semver.Version)
	for _, a := range artifacts {
		v := a.Reference.Version
		if v == nil {
			continue
		}
		k := keyOf(a)
		if cur, ok := best[k]; !ok || v.GreaterThan(cur) {
			best[k] = v
		}
	}

	result := make([]ArtifactInfo, 0, len(artifacts))
	for _, a := range artifacts {
		top, ok := best[keyOf(a)]
		if !ok || (a.Reference.Version != nil && a.Reference.Version.Equal(top)) {
			result = append(result, a)
		}
	}
	return result
}

// SortArtifacts orders rows for display: by name, kind, version (newest first,
// unversioned last), then origin and location so the order never depends on
// which catalog answered first.
func SortArtifacts(artifacts []ArtifactInfo) {
	slices.SortStableFunc(artifacts, compareArtifacts)
}

func compareArtifacts(a, b ArtifactInfo) int {
	if c := cmp.Compare(a.Reference.Name, b.Reference.Name); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Reference.Kind, b.Reference.Kind); c != 0 {
		return c
	}
	if c := compareVersionsDesc(a.Reference.Version, b.Reference.Version); c != 0 {
		return c
	}
	if c := cmp.Compare(a.OriginKey(), b.OriginKey()); c != 0 {
		return c
	}
	if c := cmp.Compare(FormatLocations(a.ResolvedLocations()), FormatLocations(b.ResolvedLocations())); c != 0 {
		return c
	}
	return cmp.Compare(artifactTag(a), artifactTag(b))
}

// compareVersionsDesc orders newer versions first and nil versions last.
func compareVersionsDesc(a, b *semver.Version) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	default:
		return b.Compare(a)
	}
}
