// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Masterminds/semver/v3"
)

// PruneOptions configures a prune operation.
type PruneOptions struct {
	// Keep is the number of most recent versions to retain per plugin.
	// Defaults to 1 if <= 0.
	Keep int

	// Names limits pruning to the specified plugin names.
	// If empty, all cached plugins are pruned.
	Names []string

	// Platform limits pruning to the specified platform.
	// If empty, all platforms are pruned.
	Platform string

	// All removes all cached plugins (requires Force).
	All bool

	// Force skips confirmation for destructive operations.
	Force bool
}

// PruneResult describes the outcome of a prune operation for a single plugin version.
type PruneResult struct {
	Name     string `json:"name" yaml:"name" doc:"Plugin name"`
	Version  string `json:"version" yaml:"version" doc:"Removed version"`
	Platform string `json:"platform" yaml:"platform" doc:"Platform"`
	Path     string `json:"path" yaml:"path" doc:"Removed path"`
	Size     int64  `json:"size" yaml:"size" doc:"Freed bytes"`
}

// PruneSummary holds the full result of a prune operation.
type PruneSummary struct {
	Removed    []PruneResult `json:"removed" yaml:"removed" doc:"Removed entries"`
	TotalFreed int64         `json:"totalFreed" yaml:"totalFreed" doc:"Total bytes freed"`
}

// Prune removes old cached plugin versions according to the given options.
// It returns the list of removed entries. If dryRun is true, nothing is
// deleted but the results reflect what would be removed.
func (c *Cache) Prune(opts PruneOptions, dryRun bool) (*PruneSummary, error) {
	if opts.Keep <= 0 {
		opts.Keep = 1
	}

	if opts.All && !opts.Force {
		return nil, fmt.Errorf("--all requires --force to confirm removal of all cached plugins")
	}

	if opts.All {
		return c.pruneAll(dryRun)
	}

	all, err := c.List()
	if err != nil {
		return nil, fmt.Errorf("listing cached plugins: %w", err)
	}

	// Filter by names if specified.
	nameSet := make(map[string]bool, len(opts.Names))
	for _, n := range opts.Names {
		nameSet[n] = true
	}

	// Group by (name, platform).
	type groupKey struct {
		name     string
		platform string
	}
	groups := make(map[groupKey][]CachedPlugin)
	for _, p := range all {
		if len(nameSet) > 0 && !nameSet[p.Name] {
			continue
		}
		if opts.Platform != "" && p.Platform != opts.Platform {
			continue
		}
		key := groupKey{name: p.Name, platform: p.Platform}
		groups[key] = append(groups[key], p)
	}

	var summary PruneSummary

	// Sort group keys for deterministic output ordering.
	keys := make([]groupKey, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].name != keys[j].name {
			return keys[i].name < keys[j].name
		}
		return keys[i].platform < keys[j].platform
	})

	for _, key := range keys {
		plugins := groups[key]
		removals := selectPruneTargets(plugins, opts.Keep)
		for _, r := range removals {
			if !dryRun {
				versionDir := filepath.Dir(filepath.Dir(c.binaryPath(r.Name, r.Version, r.Platform)))
				platformDir := filepath.Dir(c.binaryPath(r.Name, r.Version, r.Platform))
				if err := os.RemoveAll(platformDir); err != nil {
					return nil, fmt.Errorf("removing %s@%s (%s): %w", r.Name, r.Version, r.Platform, err)
				}
				// Clean up empty version directory.
				cleanEmptyDir(versionDir)
			}
			summary.Removed = append(summary.Removed, PruneResult(r))
			summary.TotalFreed += r.Size
		}
	}

	return &summary, nil
}

// pruneAll removes the entire cache contents.
func (c *Cache) pruneAll(dryRun bool) (*PruneSummary, error) {
	all, err := c.List()
	if err != nil {
		return nil, fmt.Errorf("listing cached plugins: %w", err)
	}

	var summary PruneSummary
	for _, p := range all {
		summary.Removed = append(summary.Removed, PruneResult(p))
		summary.TotalFreed += p.Size
	}

	if !dryRun {
		entries, err := os.ReadDir(c.dir)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading cache directory: %w", err)
		}
		for _, entry := range entries {
			if err := os.RemoveAll(filepath.Join(c.dir, entry.Name())); err != nil {
				return nil, fmt.Errorf("removing %s: %w", entry.Name(), err)
			}
		}
	}

	return &summary, nil
}

// selectPruneTargets selects which plugins from a same-name, same-platform
// group should be removed (keeping the newest `keep` versions).
func selectPruneTargets(plugins []CachedPlugin, keep int) []CachedPlugin {
	if len(plugins) <= keep {
		return nil
	}

	// Sort by semver descending (newest first).
	sort.Slice(plugins, func(i, j int) bool {
		vi, ei := semver.NewVersion(plugins[i].Version)
		vj, ej := semver.NewVersion(plugins[j].Version)
		if ei != nil || ej != nil {
			// Fallback to lexicographic for non-semver.
			return plugins[i].Version > plugins[j].Version
		}
		return vi.GreaterThan(vj)
	})

	// Everything after the first `keep` entries gets removed.
	return plugins[keep:]
}

// cleanEmptyDir removes a directory if it is empty.
func cleanEmptyDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if len(entries) == 0 {
		os.Remove(dir)
	}
}
