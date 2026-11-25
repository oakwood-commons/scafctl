// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package version

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// NewConstraint parses a semver constraint string and configures whether range
// comparisons include pre-release versions. The "latest" alias
// (case-insensitive) is accepted as a wildcard ("*").
func NewConstraint(constraint string, includePreRelease bool) (*semver.Constraints, error) {
	trimmed := strings.TrimSpace(constraint)
	if trimmed == "" {
		return nil, fmt.Errorf("invalid version constraint %q: constraint cannot be empty", constraint)
	}
	if strings.EqualFold(trimmed, "latest") {
		trimmed = "*"
	}
	c, err := semver.NewConstraint(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}
	c.IncludePrerelease = includePreRelease
	return c, nil
}

// CheckConstraint reports whether resolvedVersion satisfies constraint. When
// includePreRelease is true, range constraints match pre-release versions in
// addition to stable releases.
func CheckConstraint(constraint, resolvedVersion string, includePreRelease bool) (bool, error) {
	c, err := NewConstraint(constraint, includePreRelease)
	if err != nil {
		return false, err
	}
	v, err := semver.NewVersion(resolvedVersion)
	if err != nil {
		return false, fmt.Errorf("invalid resolved version %q: %w", resolvedVersion, err)
	}
	return c.Check(v), nil
}

// FilterArtifacts filters catalog artifacts by a semver constraint, returning
// matching artifacts sorted by version descending (newest first). An empty
// constraint returns artifacts unchanged. Artifacts with nil versions are
// excluded when a non-empty constraint is provided.
func FilterArtifacts(artifacts []catalog.ArtifactInfo, constraint string, includePreRelease bool) ([]catalog.ArtifactInfo, error) {
	if constraint == "" {
		return artifacts, nil
	}
	c, err := NewConstraint(constraint, includePreRelease)
	if err != nil {
		return nil, err
	}

	result := make([]catalog.ArtifactInfo, 0, len(artifacts))
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
		return vj.LessThan(vi)
	})
	return result, nil
}

// FilterVersions filters a list of version strings by a semver constraint string.
// Non-semver strings (e.g., "latest", "stable") are silently excluded.
// Results are returned sorted descending (newest first).
func FilterVersions(versions []string, constraint string) ([]string, error) {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	var matched []*semver.Version
	for _, v := range versions {
		sv, parseErr := semver.NewVersion(v)
		if parseErr != nil {
			continue // skip non-semver tags
		}
		if c.Check(sv) {
			matched = append(matched, sv)
		}
	}

	sort.Sort(sort.Reverse(semver.Collection(matched)))

	result := make([]string, len(matched))
	for i, sv := range matched {
		result[i] = sv.Original()
	}
	return result, nil
}

// BestMatch returns the highest version from versions that satisfies the constraint.
// Returns empty string if no version matches.
func BestMatch(versions []string, constraint string) (string, error) {
	filtered, err := FilterVersions(versions, constraint)
	if err != nil {
		return "", err
	}
	if len(filtered) == 0 {
		return "", nil
	}
	return filtered[0], nil
}

// FilterSemver filters parsed semver.Version values by a constraint string.
// Results are returned sorted descending (newest first).
func FilterSemver(versions []*semver.Version, constraint string) ([]*semver.Version, error) {
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}

	var matched []*semver.Version
	for _, sv := range versions {
		if sv != nil && c.Check(sv) {
			matched = append(matched, sv)
		}
	}

	sort.Sort(sort.Reverse(semver.Collection(matched)))
	return matched, nil
}

// ValidateConstraint checks whether a constraint string is valid semver syntax.
func ValidateConstraint(constraint string) error {
	_, err := semver.NewConstraint(constraint)
	if err != nil {
		return fmt.Errorf("invalid version constraint %q: %w", constraint, err)
	}
	return nil
}

// ListCatalogVersions returns all semver version strings for the given artifact
// by querying catalogs in order. The first catalog to return results wins.
// If all catalogs fail, the last error is included in the returned error.
func ListCatalogVersions(ctx context.Context, catalogs []catalog.Catalog, kind catalog.ArtifactKind, name string) ([]string, error) {
	var lastErr error

	for _, cat := range catalogs {
		artifacts, err := cat.List(ctx, kind, name)
		if err != nil {
			lastErr = err
			continue
		}
		if len(artifacts) > 0 {
			var versions []string
			for _, a := range artifacts {
				if a.Reference.Version != nil {
					versions = append(versions, a.Reference.Version.Original())
				}
			}
			if len(versions) > 0 {
				return versions, nil
			}
			// Artifacts found but none have semver versions; try next catalog.
			continue
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("no versions of %q found in catalog (last error: %w)", name, lastErr)
	}
	return nil, fmt.Errorf("no versions of %q found in catalog", name)
}
