// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"time"

	"github.com/go-logr/logr"
)

// CachedPluginInfo describes a cached plugin with metadata obtained by
// starting the plugin binary and querying its providers.
type CachedPluginInfo struct {
	Name        string `json:"name" yaml:"name" doc:"Plugin name"`
	Version     string `json:"version" yaml:"version" doc:"Plugin version"`
	Path        string `json:"path" yaml:"path" doc:"Absolute path to cached binary"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Human-readable summary of the provider"`
}

// DescribeCachedPlugins starts each cached plugin binary, queries its
// provider descriptors to obtain descriptions, then shuts it down.
// Plugins that fail to start or respond are excluded from the result
// (they may be non-provider artifacts such as auth-handler binaries).
// The skip set contains provider names that should be
// excluded from the result (e.g., already-listed builtin/official names).
func DescribeCachedPlugins(ctx context.Context, cached []CachedPlugin, skip map[string]bool) []CachedPluginInfo {
	lgr := logr.FromContextOrDiscard(ctx)
	var result []CachedPluginInfo

	for _, cp := range cached {
		if skip[cp.Name] {
			lgr.V(2).Info("skipping cached plugin (already listed)", "name", cp.Name)
			continue
		}

		info := CachedPluginInfo{
			Name:    cp.Name,
			Version: cp.Version,
			Path:    cp.Path,
		}

		// Best-effort: start the plugin to get its description.
		// Skip plugins that fail probing — they may be auth-handler binaries
		// or other non-provider artifacts in the cache.
		desc, ok := ProbePluginDescription(ctx, cp.Path, cp.Name)
		if !ok {
			lgr.V(1).Info("skipping cached plugin (probe failed)", "name", cp.Name)
			continue
		}
		if desc != "" {
			info.Description = desc
		}

		result = append(result, info)
	}

	return result
}

// ProbePluginDescription starts a plugin binary, queries the first
// provider's descriptor for its description, then kills the process.
// Returns empty string and false on any failure.
func ProbePluginDescription(ctx context.Context, binaryPath, name string) (string, bool) {
	lgr := logr.FromContextOrDiscard(ctx)

	// Guard against malfunctioning plugins that hang indefinitely.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := NewClient(binaryPath, WithStartTimeout(10*time.Second), WithSanitizedEnv())
	if err != nil {
		lgr.V(1).Info("failed to start plugin for description probe", "name", name, "error", err)
		return "", false
	}
	defer client.Kill()

	providers, err := client.GetProviders(ctx)
	if err != nil || len(providers) == 0 {
		lgr.V(1).Info("failed to get providers from plugin", "name", name, "error", err)
		return "", false
	}

	// Query the first provider's descriptor.
	wrapper, err := NewProviderWrapper(client, providers[0], WithContext(ctx))
	if err != nil {
		lgr.V(1).Info("failed to get descriptor from plugin", "name", name, "error", err)
		return "", false
	}

	return wrapper.Descriptor().Description, true
}
