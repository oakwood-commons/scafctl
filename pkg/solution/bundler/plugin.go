// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// ValidatePlugins validates all plugin declarations in a solution's bundle.
// Returns an error if any plugin has invalid name, kind, or version constraint.
func ValidatePlugins(sol *solution.Solution) error {
	for i, p := range sol.Bundle.Plugins {
		if p.Name == "" {
			return fmt.Errorf("bundle.plugins[%d]: name is required", i)
		}

		if !p.Kind.IsValid() {
			return fmt.Errorf("bundle.plugins[%d] (%s): invalid kind %q, must be %q or %q",
				i, p.Name, p.Kind, solution.PluginKindProvider, solution.PluginKindAuthHandler)
		}

		if p.Version == "" {
			return fmt.Errorf("bundle.plugins[%d] (%s): version constraint is required", i, p.Name)
		}

		if _, err := parseVersionConstraint(p.Version); err != nil {
			return fmt.Errorf("bundle.plugins[%d] (%s): invalid version constraint %q: %w",
				i, p.Name, p.Version, err)
		}
	}

	return nil
}

// PluginsToBundleEntries converts solution plugin dependencies to bundle manifest entries.
func PluginsToBundleEntries(plugins []solution.PluginDependency) []BundlePluginEntry {
	entries := make([]BundlePluginEntry, 0, len(plugins))
	for _, p := range plugins {
		entries = append(entries, BundlePluginEntry{
			Name:    p.Name,
			Kind:    string(p.Kind),
			Version: p.Version,
		})
	}
	return entries
}

// CheckVersionConstraint checks if a resolved version satisfies a version constraint.
func CheckVersionConstraint(constraint, resolved string) (bool, error) {
	c, err := parseVersionConstraint(constraint)
	if err != nil {
		return false, err
	}

	v, err := semver.NewVersion(resolved)
	if err != nil {
		return false, fmt.Errorf("invalid resolved version %q: %w", resolved, err)
	}

	return c.Check(v), nil
}

// parseVersionConstraint parses a semver constraint string.
// Supports ^, ~, >=, <=, >, <, = prefixes and exact versions.
func parseVersionConstraint(constraint string) (*semver.Constraints, error) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return nil, fmt.Errorf("empty version constraint")
	}

	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("invalid semver constraint: %w", err)
	}

	return c, nil
}
