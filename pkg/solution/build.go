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
	"github.com/oakwood-commons/scafctl/pkg/logger"
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

// NextPatchVersion queries the local catalog for existing versions of the named
// artifact and returns the next patch increment. If no versions exist, it returns
// 0.0.1. This is used as the fallback when neither --version nor metadata.version
// is provided.
func NextPatchVersion(ctx context.Context, localCatalog *catalog.LocalCatalog, name string) *semver.Version {
	lgr := logger.FromContext(ctx)

	artifacts, err := localCatalog.List(ctx, catalog.ArtifactKindSolution, name)
	if err != nil {
		lgr.V(1).Info("failed to list artifacts for auto-increment", "name", name, "error", err)
		return semver.MustParse("0.0.1")
	}

	var versions []*semver.Version
	for _, a := range artifacts {
		if a.Reference.Version != nil {
			versions = append(versions, a.Reference.Version)
		}
	}

	if len(versions) == 0 {
		return semver.MustParse("0.0.1")
	}

	sort.Slice(versions, func(i, j int) bool {
		return versions[i].LessThan(versions[j])
	})

	latest := versions[len(versions)-1]
	next := semver.New(latest.Major(), latest.Minor(), latest.Patch()+1, "", "")
	lgr.V(1).Info("auto-incremented version", "latest", latest.String(), "next", next.String())
	return next
}
