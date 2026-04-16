// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package version

import (
	"context"
	"fmt"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

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
