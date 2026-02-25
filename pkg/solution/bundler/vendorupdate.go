// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// UpdateEntry tracks the state of a single dependency during a vendor update check.
type UpdateEntry struct {
	// Ref is the original lock file reference (e.g., "deploy-to-k8s@2.0.0").
	Ref string `json:"ref" yaml:"ref" doc:"Original catalog reference" maxLength:"255" example:"deploy-to-k8s@2.0.0"`
	// LockedVersion is the version from the existing lock file.
	LockedVersion string `json:"lockedVersion" yaml:"lockedVersion" doc:"Version from the existing lock file" maxLength:"50" example:"2.0.0"`
	// LockedDigest is the digest from the existing lock file.
	LockedDigest string `json:"lockedDigest" yaml:"lockedDigest" doc:"Digest from the existing lock file" maxLength:"128" example:"sha256:abc123..."`
	// LatestVersion is the latest resolved version from catalog.
	LatestVersion string `json:"latestVersion" yaml:"latestVersion" doc:"Latest resolved version from catalog" maxLength:"50" example:"2.1.0"`
	// LatestDigest is the digest of the latest content.
	LatestDigest string `json:"latestDigest" yaml:"latestDigest" doc:"Digest of the latest content" maxLength:"128" example:"sha256:def456..."`
	// Content is the raw bytes of the latest fetched content.
	Content []byte `json:"-" yaml:"-"`
	// Info is the artifact info from the catalog resolution.
	Info catalog.ArtifactInfo `json:"-" yaml:"-"`
	// NeedsUpdate indicates whether this entry needs updating.
	NeedsUpdate bool `json:"needsUpdate" yaml:"needsUpdate" doc:"Whether this entry needs updating"`
}

// VendorUpdateOptions contains the domain-level options for a vendor update operation.
type VendorUpdateOptions struct {
	// SolutionPath is the path to the solution YAML file.
	SolutionPath string `json:"solutionPath" yaml:"solutionPath" doc:"Path to the solution YAML file" maxLength:"500" example:"./solution.yaml"`
	// Dependencies restricts the update to only these dependency refs.
	Dependencies []string `json:"dependencies,omitempty" yaml:"dependencies,omitempty" doc:"Filter to these dependency refs" maxItems:"100"`
	// DryRun previews changes without writing.
	DryRun bool `json:"dryRun" yaml:"dryRun" doc:"Preview without writing"`
	// LockOnly updates the lock file without re-vendoring files.
	LockOnly bool `json:"lockOnly" yaml:"lockOnly" doc:"Update lock file only"`
	// PreRelease includes pre-release versions when resolving.
	PreRelease bool `json:"preRelease" yaml:"preRelease" doc:"Include pre-release versions"`
}

// VendorUpdateResult describes the outcome of a vendor update check.
type VendorUpdateResult struct {
	// Entries are the resolved update entries for each dependency.
	Entries []UpdateEntry `json:"entries" yaml:"entries" doc:"Resolved update entries" maxItems:"1000"`
	// UpdateCount is the number of entries that need updating.
	UpdateCount int `json:"updateCount" yaml:"updateCount" doc:"Number of entries needing update" example:"3"`
	// Messages collects informational messages produced during the operation.
	Messages []string `json:"messages,omitempty" yaml:"messages,omitempty" doc:"Informational messages" maxItems:"500"`
	// PluginMessages collects plugin status messages.
	PluginMessages []string `json:"pluginMessages,omitempty" yaml:"pluginMessages,omitempty" doc:"Plugin status messages" maxItems:"100"`
}

// CheckForUpdates re-resolves each dependency against the catalog and
// returns UpdateEntry values indicating which entries need updating.
func CheckForUpdates(ctx context.Context, deps []LockDependency, fetcher CatalogFetcher, lgr logr.Logger) ([]UpdateEntry, error) {
	var entries []UpdateEntry

	for _, dep := range deps {
		entry := UpdateEntry{
			Ref:           dep.Ref,
			LockedVersion: ExtractVersionFromRef(dep.Ref),
			LockedDigest:  dep.Digest,
		}

		// Re-resolve from catalog
		latestContent, latestInfo, fetchErr := fetcher.FetchSolution(ctx, dep.Ref)
		if fetchErr != nil {
			lgr.V(1).Info("failed to re-resolve dependency", "ref", dep.Ref, "error", fetchErr)
			// Keep entry with NeedsUpdate=false so caller can report the error
			entries = append(entries, entry)
			continue
		}

		latestDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(latestContent))
		entry.LatestVersion = ExtractVersionFromRef(dep.Ref)
		if latestInfo.Reference.Version != nil {
			entry.LatestVersion = latestInfo.Reference.Version.String()
		}
		entry.LatestDigest = latestDigest
		entry.Content = latestContent
		entry.Info = latestInfo

		if latestDigest != dep.Digest {
			entry.NeedsUpdate = true
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// ApplyUpdates writes vendored files for entries that need updating and
// builds a new LockFile reflecting the updates.
func ApplyUpdates(entries []UpdateEntry, existingLock *LockFile, bundleRoot string, lockOnly bool, lgr logr.Logger) (*LockFile, error) {
	// Write updated vendored files
	for _, entry := range entries {
		if !entry.NeedsUpdate {
			continue
		}
		if !lockOnly {
			vendorDir := filepath.Join(bundleRoot, VendorDirName)
			vendoredName := VendorFileNameFromRef(entry.Ref, entry.Info)
			vendorPath := filepath.Join(vendorDir, vendoredName)

			if err := os.MkdirAll(filepath.Dir(vendorPath), 0o755); err != nil {
				return nil, fmt.Errorf("failed to create vendor directory: %w", err)
			}
			if err := os.WriteFile(vendorPath, entry.Content, 0o600); err != nil {
				return nil, fmt.Errorf("failed to write vendored file: %w", err)
			}
			lgr.V(1).Info("wrote vendored file", "path", vendorPath)
		}
	}

	// Build new lock file
	newLock := &LockFile{
		Version:      LockFileVersion,
		Dependencies: make([]LockDependency, 0, len(existingLock.Dependencies)),
		Plugins:      existingLock.Plugins,
	}

	for _, dep := range existingLock.Dependencies {
		updated := false
		for _, entry := range entries {
			if entry.Ref == dep.Ref && entry.NeedsUpdate {
				newLock.Dependencies = append(newLock.Dependencies, LockDependency{
					Ref:          entry.Ref,
					Digest:       entry.LatestDigest,
					ResolvedFrom: entry.Info.Catalog,
					VendoredAt:   dep.VendoredAt,
				})
				updated = true
				break
			}
		}
		if !updated {
			newLock.Dependencies = append(newLock.Dependencies, dep)
		}
	}

	return newLock, nil
}

// FilterDependencies returns only the dependencies whose refs match the filter list.
// Returns an error if any filter value is not found in the deps slice.
func FilterDependencies(deps []LockDependency, filter []string) ([]LockDependency, error) {
	filterSet := make(map[string]bool, len(filter))
	for _, f := range filter {
		filterSet[f] = true
	}

	var result []LockDependency
	for _, dep := range deps {
		if filterSet[dep.Ref] {
			result = append(result, dep)
			delete(filterSet, dep.Ref)
		}
	}

	for f := range filterSet {
		return nil, fmt.Errorf("dependency %q not found in lock file", f)
	}

	return result, nil
}

// ExtractVersionFromRef extracts the version part after the last '@' in a catalog reference.
// Returns "latest" if no '@' is found.
func ExtractVersionFromRef(ref string) string {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == '@' {
			return ref[i+1:]
		}
	}
	return "latest"
}

// TruncateDigest truncates a digest string for display purposes.
func TruncateDigest(digest string) string {
	if len(digest) > 19 {
		return digest[:19] + "..."
	}
	return digest
}

// CheckPluginUpdates returns formatted status messages for plugin dependencies.
func CheckPluginUpdates(lock *LockFile) []string {
	if lock == nil {
		return nil
	}
	var messages []string
	for _, p := range lock.Plugins {
		messages = append(messages, fmt.Sprintf("  • %s (%s): %s (locked at %s)", p.Name, p.Kind, p.Version, TruncateDigest(p.Digest)))
	}
	return messages
}

// LocalCatalogFetcherAdapter adapts a catalog.LocalCatalog to the CatalogFetcher interface.
type LocalCatalogFetcherAdapter struct {
	// Catalog is the local catalog instance.
	Catalog *catalog.LocalCatalog
}

// FetchSolution retrieves a solution by name[@version] from the local catalog.
func (a *LocalCatalogFetcherAdapter) FetchSolution(ctx context.Context, nameWithVersion string) ([]byte, catalog.ArtifactInfo, error) {
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, nameWithVersion)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, fmt.Errorf("invalid reference %q: %w", nameWithVersion, err)
	}

	content, info, err := a.Catalog.Fetch(ctx, ref)
	if err != nil {
		return nil, catalog.ArtifactInfo{}, err
	}

	return content, info, nil
}

// ListSolutions returns all available versions of a named solution artifact from the local catalog.
func (a *LocalCatalogFetcherAdapter) ListSolutions(ctx context.Context, name string) ([]catalog.ArtifactInfo, error) {
	return a.Catalog.List(ctx, catalog.ArtifactKindSolution, name)
}
