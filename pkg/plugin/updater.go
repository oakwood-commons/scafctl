// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// UpdateTarget specifies the semver boundary for updates.
type UpdateTarget string

const (
	// UpdateTargetLatest allows updates to any newer version.
	UpdateTargetLatest UpdateTarget = "latest"

	// UpdateTargetMinor constrains updates within the same major version (^).
	// For 0.x versions, constrains to same minor (0.x.y only).
	UpdateTargetMinor UpdateTarget = "minor"

	// UpdateTargetPatch constrains updates within the same minor version (~).
	UpdateTargetPatch UpdateTarget = "patch"
)

// UpdateOptions configures an update operation.
type UpdateOptions struct {
	// Names limits updates to the specified plugin cache keys.
	// If empty and All is true, all cached plugins are updated.
	Names []string

	// All updates all cached plugins. Required when Names is empty.
	All bool

	// Target specifies the semver constraint boundary.
	Target UpdateTarget

	// Platform overrides the target platform. If empty, CurrentPlatform() is used.
	Platform string
}

// UpdatePlan describes what an update operation would do.
type UpdatePlan struct {
	Updates  []UpdateEntry `json:"updates" yaml:"updates" doc:"Plugins with available updates"`
	UpToDate []string      `json:"upToDate" yaml:"upToDate" doc:"Plugins already at latest"`
	Failed   []UpdateError `json:"failed,omitempty" yaml:"failed,omitempty" doc:"Plugins that failed resolution"`
}

// UpdateEntry describes a single pending update.
type UpdateEntry struct {
	Name       string `json:"name" yaml:"name" doc:"Plugin cache key"`
	OldVersion string `json:"oldVersion" yaml:"oldVersion" doc:"Currently cached version"`
	NewVersion string `json:"newVersion" yaml:"newVersion" doc:"Available version"`
	Kind       string `json:"kind" yaml:"kind" doc:"Plugin kind (provider or auth-handler)"`
}

// UpdateError describes a resolution failure for a plugin.
type UpdateError struct {
	Name  string `json:"name" yaml:"name" doc:"Plugin cache key"`
	Error string `json:"error" yaml:"error" doc:"Error message"`
}

// PluginKindFromCacheKey infers the plugin kind and bare name from a cache key.
// Cache keys for auth-handlers are prefixed with "auth-handler-".
func PluginKindFromCacheKey(cacheKey string) (name string, kind solution.PluginKind) {
	if strings.HasPrefix(cacheKey, "auth-handler-") {
		return strings.TrimPrefix(cacheKey, "auth-handler-"), solution.PluginKindAuthHandler
	}
	return cacheKey, solution.PluginKindProvider
}

// PlanUpdates checks the catalog for newer versions of cached plugins and
// returns an UpdatePlan describing what would change.
//
// If catalogFetcher is nil, all plugins that require catalog resolution are
// reported as failed with a "no remote catalogs configured" error.
func PlanUpdates(ctx context.Context, cache *Cache, catalogFetcher *catalog.PluginFetcher, opts UpdateOptions) (*UpdatePlan, error) {
	if len(opts.Names) == 0 && !opts.All {
		return nil, fmt.Errorf("no plugin names specified; use positional args or --all")
	}

	platform := opts.Platform
	if platform == "" {
		platform = CurrentPlatform()
	}

	target := opts.Target
	if target == "" {
		target = UpdateTargetLatest
	}

	// Determine which plugins to check.
	var cacheKeys []string
	if opts.All {
		all, err := cache.List()
		if err != nil {
			return nil, fmt.Errorf("listing cached plugins: %w", err)
		}
		// Deduplicate names that have binaries for the target platform.
		seen := make(map[string]bool)
		for _, p := range all {
			if p.Platform == platform && !seen[p.Name] {
				seen[p.Name] = true
				cacheKeys = append(cacheKeys, p.Name)
			}
		}
		sort.Strings(cacheKeys)
	} else {
		cacheKeys = opts.Names
	}

	plan := &UpdatePlan{}

	for _, cacheKey := range cacheKeys {
		_, currentVer, ok := cache.GetLatestCached(cacheKey, platform)
		if !ok {
			if opts.All {
				// Skip silently when iterating all — the plugin may
				// exist for a different platform.
				continue
			}
			return nil, fmt.Errorf("plugin %q not found in cache; use 'plugins install' to add it", cacheKey)
		}

		name, kind := PluginKindFromCacheKey(cacheKey)
		artifactKind := pluginKindToArtifactKind(kind)

		if catalogFetcher == nil {
			plan.Failed = append(plan.Failed, UpdateError{
				Name:  cacheKey,
				Error: "no remote catalogs configured; cannot check for updates",
			})
			continue
		}

		// Build version constraint based on target.
		constraint := buildUpdateConstraint(currentVer, target)

		info, err := catalogFetcher.ResolvePlugin(ctx, name, artifactKind, constraint)
		if err != nil {
			plan.Failed = append(plan.Failed, UpdateError{
				Name:  cacheKey,
				Error: err.Error(),
			})
			continue
		}

		var resolvedVer string
		if info.Reference.Version != nil {
			resolvedVer = info.Reference.Version.String()
		}

		if resolvedVer == "" || resolvedVer == currentVer {
			plan.UpToDate = append(plan.UpToDate, cacheKey)
			continue
		}

		// Compare to ensure it's actually newer.
		current, err := semver.NewVersion(currentVer)
		if err != nil {
			plan.UpToDate = append(plan.UpToDate, cacheKey)
			continue
		}
		resolved, err := semver.NewVersion(resolvedVer)
		if err != nil {
			plan.UpToDate = append(plan.UpToDate, cacheKey)
			continue
		}
		if !resolved.GreaterThan(current) {
			plan.UpToDate = append(plan.UpToDate, cacheKey)
			continue
		}

		plan.Updates = append(plan.Updates, UpdateEntry{
			Name:       cacheKey,
			OldVersion: currentVer,
			NewVersion: resolvedVer,
			Kind:       string(kind),
		})
	}

	return plan, nil
}

// buildUpdateConstraint returns a semver constraint string based on the
// current version and the target boundary.
func buildUpdateConstraint(currentVer string, target UpdateTarget) string {
	switch target {
	case UpdateTargetLatest:
		return ""

	case UpdateTargetMinor:
		v, err := semver.NewVersion(currentVer)
		if err != nil {
			return "" // latest
		}
		// For 0.x: constrain to same minor (equivalent to ~0.x.0).
		if v.Major() == 0 {
			return fmt.Sprintf(">=%s, <0.%d.0", currentVer, v.Minor()+1)
		}
		// For 1.x+: constrain to same major (^).
		return fmt.Sprintf(">=%s, <%d.0.0", currentVer, v.Major()+1)

	case UpdateTargetPatch:
		v, err := semver.NewVersion(currentVer)
		if err != nil {
			return "" // latest
		}
		// Constrain to same minor (~).
		return fmt.Sprintf(">=%s, <%d.%d.0", currentVer, v.Major(), v.Minor()+1)
	}

	return ""
}
