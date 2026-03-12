// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// ParseCatalogURL parses a catalog URL into registry and repository parts,
// stripping any scheme prefix (oci://, https://, http://).
//
// Examples:
//   - "ghcr.io/myorg/scafctl" -> registry: "ghcr.io", repository: "myorg/scafctl"
//   - "ghcr.io/myorg" -> registry: "ghcr.io", repository: "myorg"
//   - "localhost:5000" -> registry: "localhost:5000", repository: ""
func ParseCatalogURL(rawURL string) (registry, repository string) {
	rawURL = strings.TrimPrefix(rawURL, "oci://")
	rawURL = strings.TrimPrefix(rawURL, "https://")
	rawURL = strings.TrimPrefix(rawURL, "http://")
	rawURL = strings.TrimSuffix(rawURL, "/")

	parts := strings.SplitN(rawURL, "/", 2)
	registry = parts[0]
	if len(parts) > 1 {
		repository = parts[1]
	}

	return registry, repository
}

// LooksLikeCatalogURL returns true if the string looks like a URL/registry
// rather than a simple catalog name. A URL contains "." (domain separator)
// or ":" (port separator), or starts with a scheme prefix.
func LooksLikeCatalogURL(s string) bool {
	s = strings.TrimPrefix(s, "oci://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.Contains(s, ".") || strings.Contains(s, ":")
}

// InferKindFromLocalCatalog searches the local catalog to determine an artifact's kind.
// It tries each known artifact kind in order and returns the first match.
func InferKindFromLocalCatalog(ctx context.Context, localCatalog *LocalCatalog, name, version string) (ArtifactKind, error) {
	kinds := []ArtifactKind{
		ArtifactKindSolution,
		ArtifactKindProvider,
		ArtifactKindAuthHandler,
	}

	for _, kind := range kinds {
		ref := Reference{
			Kind: kind,
			Name: name,
		}

		// If version specified, try to parse it
		if version != "" {
			parsedRef, err := ParseReference(kind, name+"@"+version)
			if err != nil {
				continue
			}
			ref = parsedRef
		}

		// Check if artifact exists with this kind
		exists, err := localCatalog.Exists(ctx, ref)
		if err != nil {
			continue
		}
		if exists {
			return kind, nil
		}
	}

	return "", fmt.Errorf("artifact %q not found in local catalog", name)
}

// ResolveCatalogURL resolves a catalog URL from a flag value or config defaults.
//
// Resolution order:
//  1. If catalogFlag is a URL (contains "." or ":"), use it directly.
//  2. If catalogFlag is a non-empty string, treat it as a catalog name and look it up in config.
//  3. If catalogFlag is empty, use the default catalog from config.
//
// Returns the resolved catalog URL string, or an error if no catalog can be resolved.
func ResolveCatalogURL(ctx context.Context, catalogFlag string) (string, error) {
	// Case 1 & 2: explicit --catalog flag was provided
	if catalogFlag != "" {
		if LooksLikeCatalogURL(catalogFlag) {
			return catalogFlag, nil
		}
		// Treat as a catalog name → look up in config
		return lookupCatalogURLFromConfig(ctx, catalogFlag)
	}

	// Case 3: no --catalog flag, use default catalog from config
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return "", fmt.Errorf("no --catalog specified and no configuration loaded")
	}

	cat, ok := cfg.GetDefaultCatalog()
	if !ok {
		return "", fmt.Errorf("no --catalog specified and no default catalog configured\n\nTo set a default catalog:\n  scafctl config add-catalog <name> --type oci --url <registry-url> --default\n\nOr specify one explicitly:\n  scafctl catalog push <artifact> --catalog <registry-url>")
	}

	url := catalogURLFromCatalogConfig(cat)
	if url == "" {
		return "", fmt.Errorf("default catalog %q has no URL configured", cat.Name)
	}

	return url, nil
}

// lookupCatalogURLFromConfig looks up a catalog by name in config and returns its URL.
func lookupCatalogURLFromConfig(ctx context.Context, name string) (string, error) {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return "", fmt.Errorf("catalog %q not found: no configuration loaded", name)
	}

	cat, ok := cfg.GetCatalog(name)
	if !ok {
		return "", fmt.Errorf("catalog %q not found in configuration\n\nTo add it:\n  scafctl config add-catalog %s --type oci --url <registry-url>", name, name)
	}

	url := catalogURLFromCatalogConfig(cat)
	if url == "" {
		return "", fmt.Errorf("catalog %q has no URL configured", name)
	}

	return url, nil
}

// catalogURLFromCatalogConfig extracts a usable URL from a CatalogConfig.
// For OCI catalogs, returns the URL. For filesystem catalogs, returns the path.
func catalogURLFromCatalogConfig(cat *config.CatalogConfig) string {
	if cat.URL != "" {
		return cat.URL
	}
	return cat.Path
}
