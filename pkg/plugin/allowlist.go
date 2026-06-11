// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// allowedPluginPattern validates the "catalog/plugin" format.
// Both segments must be lowercase alphanumeric with hyphens.
// Also accepts "catalog/*" as a wildcard (all plugins from that catalog).
var allowedPluginPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]/(\*|[a-z0-9][a-z0-9-]*[a-z0-9])$`)

// AllowedPlugin represents a qualified plugin reference in "catalog/pluginName" format.
// It encodes both the catalog a plugin may be fetched from and the plugin name.
type AllowedPlugin string

// Validate checks that the AllowedPlugin matches the required "catalog/plugin" format.
func (a AllowedPlugin) Validate() error {
	s := string(a)
	if s == "" {
		return fmt.Errorf("allowed plugin must not be empty")
	}
	if !strings.Contains(s, "/") {
		return fmt.Errorf("allowed plugin %q must be in catalog/plugin format", s)
	}
	if !allowedPluginPattern.MatchString(s) {
		return fmt.Errorf("allowed plugin %q does not match required pattern (lowercase alphanumeric and hyphens, e.g. official/exec)", s)
	}
	return nil
}

// Catalog returns the catalog segment (left of the slash).
func (a AllowedPlugin) Catalog() string {
	parts := strings.SplitN(string(a), "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[0]
}

// Plugin returns the plugin name segment (right of the slash).
func (a AllowedPlugin) Plugin() string {
	parts := strings.SplitN(string(a), "/", 2)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// IsWildcard returns true if this entry uses the "catalog/*" wildcard syntax,
// meaning all plugins from that catalog are permitted.
func (a AllowedPlugin) IsWildcard() bool {
	return a.Plugin() == "*"
}

// ParseAllowedPlugins parses and validates a slice of raw "catalog/plugin" strings.
func ParseAllowedPlugins(raw []string) ([]AllowedPlugin, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	result := make([]AllowedPlugin, 0, len(raw))
	for _, s := range raw {
		ap := AllowedPlugin(s)
		if err := ap.Validate(); err != nil {
			return nil, fmt.Errorf("parsing allowed plugins: %w", err)
		}
		result = append(result, ap)
	}
	return result, nil
}

// GroupByCatalog groups parsed AllowedPlugin entries by catalog name.
// Wildcard entries ("catalog/*") set AllowAll=true for that catalog.
// Explicit entries after a wildcard for the same catalog are ignored.
func GroupByCatalog(plugins []AllowedPlugin) map[string]catalog.PluginPolicy {
	if len(plugins) == 0 {
		return nil
	}
	result := make(map[string]catalog.PluginPolicy, len(plugins))
	for _, p := range plugins {
		cat := p.Catalog()
		if p.IsWildcard() {
			result[cat] = catalog.PluginPolicy{AllowAll: true}
			continue
		}
		existing := result[cat]
		if !existing.AllowAll {
			existing.Plugins = append(existing.Plugins, p.Plugin())
			result[cat] = existing
		}
	}
	return result
}

// PolicyPlugins returns the plugin list for a specific catalog from the policy map.
// For wildcard catalogs (AllowAll=true), returns nil (meaning unrestricted).
// For absent catalogs, returns nil.
func PolicyPlugins(policies map[string]catalog.PluginPolicy, catalogName string) []string {
	if policies == nil {
		return nil
	}
	policy, ok := policies[catalogName]
	if !ok {
		return nil
	}
	if policy.AllowAll {
		return nil
	}
	return policy.Plugins
}

// BareNames extracts just the plugin name (right of the slash) from each entry.
// Wildcard entries are skipped since they don't map to a single plugin name.
// Used for pool-level and preload-level fast-reject allowlists that don't need
// catalog qualification.
func BareNames(plugins []AllowedPlugin) []string {
	if len(plugins) == 0 {
		return nil
	}
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		if p.IsWildcard() {
			continue
		}
		names = append(names, p.Plugin())
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// CatalogNames extracts the unique catalog names from the plugin list.
// Used to derive the WithAllowedCatalogs set for chain construction.
func CatalogNames(plugins []AllowedPlugin) []string {
	if len(plugins) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(plugins))
	names := make([]string, 0, len(plugins))
	for _, p := range plugins {
		cat := p.Catalog()
		if !seen[cat] {
			seen[cat] = true
			names = append(names, cat)
		}
	}
	return names
}
