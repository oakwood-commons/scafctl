// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package solution

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// ResolveArtifactName determines the artifact name using the following priority:
//  1. explicitName (e.g., --name flag)
//  2. metadataName (from solution metadata.name)
//  3. derived from filePath (e.g., "my-solution.yaml" → "my-solution")
//
// Returns an error if the resolved name is empty or fails catalog.IsValidName validation.
func ResolveArtifactName(explicitName, metadataName, filePath string) (string, error) {
	name := explicitName
	if name == "" {
		if metadataName != "" {
			name = metadataName
		} else {
			base := filepath.Base(filePath)
			ext := filepath.Ext(base)
			name = strings.TrimSuffix(base, ext)
		}
	}

	if name == "" {
		return "", fmt.Errorf("could not determine artifact name: provide --name or set metadata.name")
	}

	if !catalog.IsValidName(name) {
		return "", fmt.Errorf("invalid name %q: must be lowercase alphanumeric with hyphens (e.g., 'my-solution')", name)
	}

	return name, nil
}

// ResolveArtifactVersion determines the artifact version using the following priority:
//  1. explicitVersion (e.g., --version flag)
//  2. metadataVersion (from solution metadata.version)
//
// Returns the resolved version, whether an explicit version overrides a different
// metadata version (callers may want to log a warning), and any error.
func ResolveArtifactVersion(explicitVersion string, metadataVersion *semver.Version) (*semver.Version, bool, error) {
	if explicitVersion != "" {
		v, err := semver.NewVersion(explicitVersion)
		if err != nil {
			return nil, false, fmt.Errorf("invalid version %q: %w", explicitVersion, err)
		}

		overrides := metadataVersion != nil && !metadataVersion.Equal(v)
		return v, overrides, nil
	}

	if metadataVersion != nil {
		return metadataVersion, false, nil
	}

	return nil, false, fmt.Errorf("no version: solution has no version in metadata; provide --version or set metadata.version")
}

// NextPatchVersion queries the catalog for existing versions of the named artifact
// and returns the next patch version. If no versions exist, it returns 0.1.0.
func NextPatchVersion(ctx context.Context, cat catalog.Catalog, kind catalog.ArtifactKind, name string) (*semver.Version, error) {
	artifacts, err := cat.List(ctx, kind, name)
	if err != nil {
		return nil, fmt.Errorf("listing catalog versions for %q: %w", name, err)
	}

	var versions []*semver.Version
	for _, a := range artifacts {
		if a.Reference.Version != nil {
			versions = append(versions, a.Reference.Version)
		}
	}

	if len(versions) == 0 {
		return semver.MustParse("0.1.0"), nil
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].LessThan(versions[j])
	})

	highest := versions[len(versions)-1]
	next := semver.New(highest.Major(), highest.Minor(), highest.Patch()+1, "", "")
	return next, nil
}
