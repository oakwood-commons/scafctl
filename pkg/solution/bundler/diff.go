// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// DiffResult is the structured output of a bundle diff, describing all changes
// between two bundled solution versions including solution YAML, files,
// vendored dependencies, and plugin changes.
type DiffResult struct {
	RefA     string        `json:"refA" yaml:"refA"`
	RefB     string        `json:"refB" yaml:"refB"`
	Solution *SolutionDiff `json:"solution,omitempty" yaml:"solution,omitempty"`
	Files    *FilesDiff    `json:"files,omitempty" yaml:"files,omitempty"`
	Vendored *VendoredDiff `json:"vendoredDependencies,omitempty" yaml:"vendoredDependencies,omitempty"`
	Plugins  *PluginsDiff  `json:"plugins,omitempty" yaml:"plugins,omitempty"`
}

// SolutionDiff holds changes to the solution structure.
type SolutionDiff struct {
	Resolvers DiffSets `json:"resolvers,omitempty" yaml:"resolvers,omitempty"`
	Actions   DiffSets `json:"actions,omitempty" yaml:"actions,omitempty"`
}

// DiffSets contains added/removed/modified name lists.
type DiffSets struct {
	Added    []string `json:"added,omitempty" yaml:"added,omitempty"`
	Removed  []string `json:"removed,omitempty" yaml:"removed,omitempty"`
	Modified []string `json:"modified,omitempty" yaml:"modified,omitempty"`
}

// FilesDiff holds changes to bundled files.
type FilesDiff struct {
	Added    []FileDiffEntry `json:"added,omitempty" yaml:"added,omitempty"`
	Removed  []FileDiffEntry `json:"removed,omitempty" yaml:"removed,omitempty"`
	Modified []FileDiffEntry `json:"modified,omitempty" yaml:"modified,omitempty"`
}

// FileDiffEntry describes a changed file.
type FileDiffEntry struct {
	Path string `json:"path" yaml:"path"`
	Size int64  `json:"size,omitempty" yaml:"size,omitempty"`
}

// VendoredDiff lists vendored dependency changes.
type VendoredDiff struct {
	Added    []VendoredEntry   `json:"added,omitempty" yaml:"added,omitempty"`
	Removed  []VendoredEntry   `json:"removed,omitempty" yaml:"removed,omitempty"`
	Upgraded []VendoredUpgrade `json:"upgraded,omitempty" yaml:"upgraded,omitempty"`
}

// VendoredEntry describes a vendored dependency.
type VendoredEntry struct {
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

// VendoredUpgrade describes a vendored dependency version change.
type VendoredUpgrade struct {
	Name string `json:"name" yaml:"name"`
	From string `json:"from" yaml:"from"`
	To   string `json:"to" yaml:"to"`
}

// PluginsDiff lists plugin dependency changes.
type PluginsDiff struct {
	Added    []PluginDiffEntry `json:"added,omitempty" yaml:"added,omitempty"`
	Removed  []PluginDiffEntry `json:"removed,omitempty" yaml:"removed,omitempty"`
	Modified []PluginDiffEntry `json:"modified,omitempty" yaml:"modified,omitempty"`
}

// PluginDiffEntry describes a plugin change.
type PluginDiffEntry struct {
	Name        string `json:"name" yaml:"name"`
	VersionFrom string `json:"versionFrom,omitempty" yaml:"versionFrom,omitempty"`
	VersionTo   string `json:"versionTo,omitempty" yaml:"versionTo,omitempty"`
}

// ExtractedArtifact holds a fetched and parsed solution with its temporary directory.
type ExtractedArtifact struct {
	Sol    *solution.Solution
	TmpDir string
}

// Cleanup removes the temporary directory if one was created.
func (e *ExtractedArtifact) Cleanup() {
	if e.TmpDir != "" {
		os.RemoveAll(e.TmpDir)
	}
}

// FetchAndExtract fetches a solution artifact from the catalog, parses it, and
// extracts any bundle data into a temporary directory.
func FetchAndExtract(ctx context.Context, cat *catalog.LocalCatalog, refStr string) (*ExtractedArtifact, *BundleManifest, error) {
	ref, err := catalog.ParseReference(catalog.ArtifactKindSolution, refStr)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid reference %q: %w", refStr, err)
	}

	content, bundleData, _, err := cat.FetchWithBundle(ctx, ref)
	if err != nil {
		return nil, nil, err
	}

	var sol solution.Solution
	if err := sol.LoadFromBytes(content); err != nil {
		return nil, nil, fmt.Errorf("failed to parse solution: %w", err)
	}

	result := &ExtractedArtifact{Sol: &sol}
	var manifest *BundleManifest

	if len(bundleData) > 0 {
		tmpDir, err := os.MkdirTemp("", paths.AppName()+"-diff-*")
		if err != nil {
			return nil, nil, err
		}
		result.TmpDir = tmpDir

		manifest, err = ExtractBundleTar(bundleData, tmpDir)
		if err != nil {
			os.RemoveAll(tmpDir)
			return nil, nil, err
		}
	}

	if manifest == nil {
		manifest = &BundleManifest{Version: 1, Root: "."}
	}

	return result, manifest, nil
}

// DiffSolutions compares two solution structures and returns the differences.
func DiffSolutions(a, b *solution.Solution) *SolutionDiff {
	diff := &SolutionDiff{}

	// Compare resolvers
	resolversA := MapKeys(a.Spec.Resolvers)
	resolversB := MapKeys(b.Spec.Resolvers)
	diff.Resolvers = ComputeDiffSets(resolversA, resolversB)

	// Compare actions
	var actionsA, actionsB map[string]bool
	if a.Spec.Workflow != nil {
		actionsA = make(map[string]bool)
		for name := range a.Spec.Workflow.Actions {
			actionsA[name] = true
		}
	}
	if b.Spec.Workflow != nil {
		actionsB = make(map[string]bool)
		for name := range b.Spec.Workflow.Actions {
			actionsB[name] = true
		}
	}
	diff.Actions = ComputeDiffSetsFromBool(actionsA, actionsB)

	if len(diff.Resolvers.Added) == 0 && len(diff.Resolvers.Removed) == 0 &&
		len(diff.Resolvers.Modified) == 0 && len(diff.Actions.Added) == 0 &&
		len(diff.Actions.Removed) == 0 && len(diff.Actions.Modified) == 0 {
		return nil
	}

	return diff
}

// DiffFiles compares two bundle manifests and returns the file differences.
func DiffFiles(a, b *BundleManifest, ignore []string) *FilesDiff {
	diff := &FilesDiff{}

	filesA := make(map[string]BundleFileEntry)
	for _, f := range a.Files {
		filesA[f.Path] = f
	}
	filesB := make(map[string]BundleFileEntry)
	for _, f := range b.Files {
		filesB[f.Path] = f
	}

	// Ignore vendor and manifest paths for files diff
	isInternal := func(path string) bool {
		return strings.HasPrefix(path, ".scafctl/")
	}

	for path, fb := range filesB {
		if isInternal(path) {
			continue
		}
		if ShouldIgnore(path, ignore) {
			continue
		}
		if _, exists := filesA[path]; !exists {
			diff.Added = append(diff.Added, FileDiffEntry{Path: path, Size: fb.Size})
		}
	}

	for path := range filesA {
		if isInternal(path) {
			continue
		}
		if ShouldIgnore(path, ignore) {
			continue
		}
		if _, exists := filesB[path]; !exists {
			diff.Removed = append(diff.Removed, FileDiffEntry{Path: path})
		}
	}

	for path, fb := range filesB {
		if isInternal(path) {
			continue
		}
		if ShouldIgnore(path, ignore) {
			continue
		}
		fa, exists := filesA[path]
		if exists && fa.Digest != fb.Digest {
			diff.Modified = append(diff.Modified, FileDiffEntry{Path: path, Size: fb.Size})
		}
	}

	sort.Slice(diff.Added, func(i, j int) bool { return diff.Added[i].Path < diff.Added[j].Path })
	sort.Slice(diff.Removed, func(i, j int) bool { return diff.Removed[i].Path < diff.Removed[j].Path })
	sort.Slice(diff.Modified, func(i, j int) bool { return diff.Modified[i].Path < diff.Modified[j].Path })

	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Modified) == 0 {
		return nil
	}
	return diff
}

// DiffVendored compares vendored dependencies between two bundle manifests.
func DiffVendored(a, b *BundleManifest) *VendoredDiff {
	diff := &VendoredDiff{}

	vendorA := make(map[string]BundleFileEntry)
	vendorB := make(map[string]BundleFileEntry)

	for _, f := range a.Files {
		if strings.HasPrefix(f.Path, ".scafctl/vendor/") {
			name := strings.TrimPrefix(f.Path, ".scafctl/vendor/")
			name = strings.TrimSuffix(name, ".yaml")
			vendorA[name] = f
		}
	}
	for _, f := range b.Files {
		if strings.HasPrefix(f.Path, ".scafctl/vendor/") {
			name := strings.TrimPrefix(f.Path, ".scafctl/vendor/")
			name = strings.TrimSuffix(name, ".yaml")
			vendorB[name] = f
		}
	}

	for name := range vendorB {
		if _, exists := vendorA[name]; !exists {
			n, v := SplitNameVersion(name)
			diff.Added = append(diff.Added, VendoredEntry{Name: n, Version: v})
		}
	}

	for name := range vendorA {
		if _, exists := vendorB[name]; !exists {
			n, v := SplitNameVersion(name)
			diff.Removed = append(diff.Removed, VendoredEntry{Name: n, Version: v})
		}
	}

	// Check for version upgrades by comparing digests
	for name, fb := range vendorB {
		fa, exists := vendorA[name]
		if exists && fa.Digest != fb.Digest {
			n, v := SplitNameVersion(name)
			_, vOld := SplitNameVersion(name)
			diff.Upgraded = append(diff.Upgraded, VendoredUpgrade{Name: n, From: vOld, To: v})
		}
	}

	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Upgraded) == 0 {
		return nil
	}
	return diff
}

// DiffPlugins compares plugin dependencies between two bundle manifests.
func DiffPlugins(a, b *BundleManifest) *PluginsDiff {
	diff := &PluginsDiff{}

	pluginsA := make(map[string]BundlePluginEntry)
	for _, p := range a.Plugins {
		pluginsA[p.Name] = p
	}
	pluginsB := make(map[string]BundlePluginEntry)
	for _, p := range b.Plugins {
		pluginsB[p.Name] = p
	}

	for _, pb := range b.Plugins {
		if _, exists := pluginsA[pb.Name]; !exists {
			diff.Added = append(diff.Added, PluginDiffEntry{Name: pb.Name, VersionTo: pb.Version})
		}
	}

	for _, pa := range a.Plugins {
		if _, exists := pluginsB[pa.Name]; !exists {
			diff.Removed = append(diff.Removed, PluginDiffEntry{Name: pa.Name, VersionFrom: pa.Version})
		}
	}

	for _, pb := range b.Plugins {
		pa, exists := pluginsA[pb.Name]
		if exists && (pa.Version != pb.Version || pa.Kind != pb.Kind) {
			diff.Modified = append(diff.Modified, PluginDiffEntry{
				Name:        pb.Name,
				VersionFrom: pa.Version,
				VersionTo:   pb.Version,
			})
		}
	}

	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Modified) == 0 {
		return nil
	}
	return diff
}

// MapKeys converts a map with string keys to a map[string]bool.
func MapKeys[V any](m map[string]V) map[string]bool {
	result := make(map[string]bool, len(m))
	for k := range m {
		result[k] = true
	}
	return result
}

// ComputeDiffSets computes added/removed/modified sets from two maps.
func ComputeDiffSets(a, b map[string]bool) DiffSets {
	return ComputeDiffSetsFromBool(a, b)
}

// ComputeDiffSetsFromBool computes added/removed/modified sets from two bool maps.
func ComputeDiffSetsFromBool(a, b map[string]bool) DiffSets {
	var ds DiffSets
	for name := range b {
		if !a[name] {
			ds.Added = append(ds.Added, name)
		}
	}
	for name := range a {
		if !b[name] {
			ds.Removed = append(ds.Removed, name)
		}
	}
	// Items present in both are potentially modified (structural compare would be needed)
	for name := range b {
		if a[name] {
			ds.Modified = append(ds.Modified, name)
		}
	}
	sort.Strings(ds.Added)
	sort.Strings(ds.Removed)
	sort.Strings(ds.Modified)
	return ds
}

// SplitNameVersion splits a "name@version" string into name and version parts.
func SplitNameVersion(s string) (string, string) {
	if idx := strings.LastIndex(s, "@"); idx != -1 {
		return s[:idx], s[idx+1:]
	}
	return s, ""
}

// ShouldIgnore returns true if the given path matches any of the provided glob patterns.
func ShouldIgnore(path string, patterns []string) bool {
	for _, p := range patterns {
		if MatchGlob(p, path) {
			return true
		}
	}
	return false
}

// CountChanges returns a human-readable summary of all changes in a DiffResult.
func CountChanges(result *DiffResult) string {
	var parts []string
	if result.Solution != nil {
		resolverChanges := len(result.Solution.Resolvers.Added) + len(result.Solution.Resolvers.Removed)
		if resolverChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d resolver(s) changed", resolverChanges))
		}
		actionChanges := len(result.Solution.Actions.Added) + len(result.Solution.Actions.Removed)
		if actionChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d action(s) changed", actionChanges))
		}
	}
	if result.Files != nil {
		fileChanges := len(result.Files.Added) + len(result.Files.Removed) + len(result.Files.Modified)
		if fileChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d file(s) changed", fileChanges))
		}
	}
	if result.Vendored != nil {
		vendorChanges := len(result.Vendored.Added) + len(result.Vendored.Removed) + len(result.Vendored.Upgraded)
		if vendorChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d dependency(ies) changed", vendorChanges))
		}
	}
	if result.Plugins != nil {
		pluginChanges := len(result.Plugins.Added) + len(result.Plugins.Removed) + len(result.Plugins.Modified)
		if pluginChanges > 0 {
			parts = append(parts, fmt.Sprintf("%d plugin(s) changed", pluginChanges))
		}
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

// FormatSize returns a human-readable file size string.
func FormatSize(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// MatchGlob tests whether a path matches a glob pattern.
// Uses filepath.Match for single-level patterns and a simple recursive check for **.
func MatchGlob(pattern, path string) bool {
	matched, _ := filepath.Match(pattern, path)
	if matched {
		return true
	}
	// Simple ** support: if pattern contains **, try matching the suffix
	if len(pattern) > 2 {
		for i := range pattern {
			if i+1 < len(pattern) && pattern[i] == '*' && pattern[i+1] == '*' {
				suffix := pattern[i+2:]
				if len(suffix) > 0 && suffix[0] == '/' {
					suffix = suffix[1:]
				}
				// Try matching suffix against path and all subdirectories
				m, _ := filepath.Match(suffix, filepath.Base(path))
				if m {
					return true
				}
			}
		}
	}
	return false
}
