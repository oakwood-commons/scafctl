package catalog

import (
	"context"
	"fmt"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// resolveCatalogURL resolves a catalog URL from the --catalog flag or config defaults.
//
// Resolution order:
//  1. If catalogFlag is a URL (contains "." or ":"), use it directly.
//  2. If catalogFlag is a non-empty string, treat it as a catalog name and look it up in config.
//  3. If catalogFlag is empty, use the default catalog from config.
//
// Returns the resolved catalog URL string, or an error if no catalog can be resolved.
func resolveCatalogURL(ctx context.Context, catalogFlag string) (string, error) {
	// Case 1 & 2: explicit --catalog flag was provided
	if catalogFlag != "" {
		if looksLikeCatalogURL(catalogFlag) {
			return catalogFlag, nil
		}
		// Treat as a catalog name → look up in config
		return lookupCatalogURL(ctx, catalogFlag)
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

	url := catalogURLFromConfig(cat)
	if url == "" {
		return "", fmt.Errorf("default catalog %q has no URL configured", cat.Name)
	}

	return url, nil
}

// lookupCatalogURL looks up a catalog by name in config and returns its URL.
func lookupCatalogURL(ctx context.Context, name string) (string, error) {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return "", fmt.Errorf("catalog %q not found: no configuration loaded", name)
	}

	cat, ok := cfg.GetCatalog(name)
	if !ok {
		return "", fmt.Errorf("catalog %q not found in configuration\n\nTo add it:\n  scafctl config add-catalog %s --type oci --url <registry-url>", name, name)
	}

	url := catalogURLFromConfig(cat)
	if url == "" {
		return "", fmt.Errorf("catalog %q has no URL configured", name)
	}

	return url, nil
}

// catalogURLFromConfig extracts a usable URL from a CatalogConfig.
// For OCI catalogs, returns the URL. For filesystem catalogs, returns the path.
func catalogURLFromConfig(cat *config.CatalogConfig) string {
	if cat.URL != "" {
		return cat.URL
	}
	return cat.Path
}

// looksLikeCatalogURL returns true if the string looks like a URL/registry
// rather than a simple catalog name. A URL contains "." (domain separator)
// or ":" (port separator), or starts with a scheme prefix.
func looksLikeCatalogURL(s string) bool {
	s = strings.TrimPrefix(s, "oci://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	return strings.Contains(s, ".") || strings.Contains(s, ":")
}
