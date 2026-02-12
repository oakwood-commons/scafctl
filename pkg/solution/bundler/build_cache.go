// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BuildCacheEntry records a successful build result for cache hit detection.
type BuildCacheEntry struct {
	// Fingerprint is the SHA-256 hash of all build inputs.
	Fingerprint string `json:"fingerprint"`

	// ArtifactName is the name of the built artifact.
	ArtifactName string `json:"artifactName"`

	// ArtifactVersion is the version of the built artifact.
	ArtifactVersion string `json:"artifactVersion"`

	// ArtifactDigest is the OCI digest of the stored artifact.
	ArtifactDigest string `json:"artifactDigest"`

	// CreatedAt is when this cache entry was written.
	CreatedAt time.Time `json:"createdAt"`

	// InputFiles records the number of input files that contributed to the fingerprint.
	InputFiles int `json:"inputFiles"`
}

// ComputeBuildFingerprint computes a SHA-256 fingerprint from the solution content,
// discovered file contents, plugin versions, and lock file digest.
//
// The fingerprint changes when any input to the build changes, enabling
// incremental builds by skipping the entire pipeline when inputs are unchanged.
func ComputeBuildFingerprint(solutionContent []byte, bundleRoot string, discoveredFiles []FileEntry, plugins []BundlePluginEntry, lockDigest string) (string, error) {
	h := sha256.New()

	// Include solution content
	h.Write([]byte("solution:"))
	h.Write(solutionContent)
	h.Write([]byte{0})

	// Include discovered file contents (sorted for determinism)
	sortedFiles := make([]FileEntry, len(discoveredFiles))
	copy(sortedFiles, discoveredFiles)
	sort.Slice(sortedFiles, func(i, j int) bool {
		return sortedFiles[i].RelPath < sortedFiles[j].RelPath
	})

	for _, f := range sortedFiles {
		absPath := filepath.Join(bundleRoot, f.RelPath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			// File might not exist yet (e.g., vendored files not yet created)
			// Include the path name to ensure fingerprint changes if the file appears later
			h.Write([]byte("missing:" + f.RelPath))
			h.Write([]byte{0})
			continue
		}
		h.Write([]byte("file:" + f.RelPath + ":"))
		h.Write(content)
		h.Write([]byte{0})
	}

	// Include plugin versions (sorted for determinism)
	sortedPlugins := make([]BundlePluginEntry, len(plugins))
	copy(sortedPlugins, plugins)
	sort.Slice(sortedPlugins, func(i, j int) bool {
		if sortedPlugins[i].Name != sortedPlugins[j].Name {
			return sortedPlugins[i].Name < sortedPlugins[j].Name
		}
		return sortedPlugins[i].Kind < sortedPlugins[j].Kind
	})

	for _, p := range sortedPlugins {
		h.Write([]byte("plugin:" + p.Name + ":" + p.Kind + ":" + p.Version))
		h.Write([]byte{0})
	}

	// Include lock file digest
	if lockDigest != "" {
		h.Write([]byte("lock:" + lockDigest))
		h.Write([]byte{0})
	}

	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

// CheckBuildCache checks if a build cache entry exists for the given fingerprint.
// Returns the cache entry and true if found, nil and false if not.
func CheckBuildCache(cacheDir, fingerprint string) (*BuildCacheEntry, bool) {
	path := buildCacheEntryPath(cacheDir, fingerprint)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	var entry BuildCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, false
	}

	// Verify the fingerprint matches (extra safety)
	if entry.Fingerprint != fingerprint {
		return nil, false
	}

	return &entry, true
}

// WriteBuildCache writes a build cache entry for the given fingerprint.
func WriteBuildCache(cacheDir, fingerprint string, entry *BuildCacheEntry) error {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return fmt.Errorf("creating build cache directory: %w", err)
	}

	entry.Fingerprint = fingerprint

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("serializing build cache entry: %w", err)
	}

	path := buildCacheEntryPath(cacheDir, fingerprint)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("writing build cache entry: %w", err)
	}

	return nil
}

// CleanBuildCache removes all entries from the build cache directory.
func CleanBuildCache(cacheDir string) error {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading build cache directory: %w", err)
	}

	removed := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(cacheDir, entry.Name())); err != nil {
			return fmt.Errorf("removing cache entry %s: %w", entry.Name(), err)
		}
		removed++
	}

	return nil
}

// CountBuildCacheEntries returns the number of cache entries in the directory.
func CountBuildCacheEntries(cacheDir string) int {
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count
}

// buildCacheEntryPath returns the file path for a cache entry.
func buildCacheEntryPath(cacheDir, fingerprint string) string {
	// Use the fingerprint (without the "sha256:" prefix) as the filename
	name := strings.TrimPrefix(fingerprint, "sha256:")
	return filepath.Join(cacheDir, name+".json")
}
