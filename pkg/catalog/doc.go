// Package catalog provides artifact storage and retrieval for scafctl.
//
// The catalog system manages solutions and plugins as OCI artifacts, enabling
// versioned storage, distribution, and dependency management.
//
// # Catalog Types
//
// There are two types of catalogs:
//
//   - Local catalog: Built-in catalog stored at XDG data path, always available
//   - Remote catalog: OCI registry-based catalog for distribution (future)
//
// # Built-in Local Catalog
//
// The local catalog is always available and is first in resolution order.
// It uses the XDG-compliant path from paths.CatalogDir():
//
//   - Linux: ~/.local/share/scafctl/catalog/
//   - macOS: ~/Library/Application Support/scafctl/catalog/
//   - Windows: %LOCALAPPDATA%\scafctl\catalog\
//
// The local catalog stores artifacts as OCI artifacts in OCI Image Layout format,
// providing content-addressable storage with full OCI compatibility.
//
// # Registry
//
// The Registry manages multiple catalogs with a defined resolution order:
//
//  1. Built-in local catalog (always first)
//  2. Configured remote catalogs (in config order)
//
// This enables seamless artifact resolution across local and remote sources.
//
// # Artifact Types
//
// Currently supported artifact types:
//
//   - Solutions: YAML configuration files (application/vnd.scafctl.solution.v1+yaml)
//   - Plugins: Binary executables (application/vnd.scafctl.plugin.v1+binary) [future]
//
// # Example Usage
//
//	// Create a registry with the built-in local catalog
//	reg, err := catalog.NewRegistry(logger)
//	if err != nil {
//	    return err
//	}
//
//	// Build a solution into the local catalog
//	ref := catalog.Reference{
//	    Kind:    catalog.ArtifactKindSolution,
//	    Name:    "my-solution",
//	    Version: semver.MustParse("1.0.0"),
//	}
//	info, err := reg.Local().Store(ctx, ref, content, annotations, false)
//
//	// Resolve an artifact (checks local first, then configured catalogs)
//	content, info, err := reg.Fetch(ctx, ref)
package catalog
