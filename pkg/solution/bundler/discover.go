// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	actionpkg "github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// DiscoverySource indicates how a file was discovered for bundling.
type DiscoverySource int

const (
	// StaticAnalysis means the file was discovered by walking provider inputs.
	StaticAnalysis DiscoverySource = iota
	// ExplicitInclude means the file was declared in bundle.include.
	ExplicitInclude
	// TestInclude means the file was referenced in spec.testing.cases[*].files.
	TestInclude
)

// String returns a human-readable label for the discovery source.
func (d DiscoverySource) String() string {
	switch d {
	case StaticAnalysis:
		return "static-analysis"
	case ExplicitInclude:
		return "explicit-include"
	case TestInclude:
		return "test-include"
	default:
		return "unknown"
	}
}

// FileEntry represents a local file to be bundled.
type FileEntry struct {
	// RelPath is the path relative to the bundle root.
	RelPath string
	// Source indicates how the file was discovered.
	Source DiscoverySource
}

// CatalogRefEntry represents a catalog dependency to vendor.
type CatalogRefEntry struct {
	// Ref is the original catalog reference (e.g., "deploy-to-k8s@2.0.0").
	Ref string
	// VendorPath is the path within the bundle where the vendored artifact is stored.
	VendorPath string
}

// DiscoveryResult contains all files and dependencies discovered during analysis.
type DiscoveryResult struct {
	// LocalFiles are local file paths relative to the bundle root.
	LocalFiles []FileEntry
	// CatalogRefs are catalog references to vendor.
	CatalogRefs []CatalogRefEntry
}

// DiscoverOption configures DiscoverFiles behavior.
type DiscoverOption func(*discoverConfig)

type discoverConfig struct {
	ignoreChecker IgnoreChecker
	statFunc      func(string) (os.FileInfo, error)
	readFile      func(string) ([]byte, error)
	walkDir       func(root string, fn filepath.WalkFunc) error
}

// WithIgnoreChecker sets a custom ignore checker for file exclusion.
func WithIgnoreChecker(ic IgnoreChecker) DiscoverOption {
	return func(c *discoverConfig) {
		c.ignoreChecker = ic
	}
}

// WithStatFunc overrides os.Stat for testing.
func WithStatFunc(fn func(string) (os.FileInfo, error)) DiscoverOption {
	return func(c *discoverConfig) {
		c.statFunc = fn
	}
}

// WithDiscoverReadFileFunc overrides os.ReadFile for testing.
func WithDiscoverReadFileFunc(fn func(string) ([]byte, error)) DiscoverOption {
	return func(c *discoverConfig) {
		c.readFile = fn
	}
}

// WithWalkDirFunc overrides filepath.Walk for testing.
func WithWalkDirFunc(fn func(string, filepath.WalkFunc) error) DiscoverOption {
	return func(c *discoverConfig) {
		c.walkDir = fn
	}
}

// DiscoverFiles performs static analysis on a parsed (and composed) solution
// to find local file references and catalog references, then combines them
// with explicit bundle includes.
//
// Returns deduplicated lists of local files and catalog references.
func DiscoverFiles(sol *solution.Solution, bundleRoot string, opts ...DiscoverOption) (*DiscoveryResult, error) {
	return discoverFilesRecursive(sol, bundleRoot, bundleRoot, nil, opts...)
}

// discoverFilesRecursive is the internal recursive implementation of DiscoverFiles.
// parentBundleRoot is the top-level bundle root used for path normalization.
// currentRoot is the directory containing the current solution being analyzed.
// visitedPaths tracks already-visited solution files to detect circular references.
func discoverFilesRecursive(sol *solution.Solution, parentBundleRoot, currentRoot string, visitedPaths map[string]bool, opts ...DiscoverOption) (*DiscoveryResult, error) {
	if sol == nil {
		return nil, fmt.Errorf("solution is nil")
	}

	cfg := &discoverConfig{
		ignoreChecker: &noopIgnoreChecker{},
		statFunc:      os.Stat,
		readFile:      os.ReadFile,
		walkDir:       filepath.Walk,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	if visitedPaths == nil {
		visitedPaths = make(map[string]bool)
	}

	result := &DiscoveryResult{}
	seen := make(map[string]bool)

	// Phase 1: Static analysis of provider inputs
	staticFiles, catalogRefs := analyzeProviderInputs(sol)

	// Separate local sub-solution files from other static files for recursive analysis.
	subSolutionFiles := identifySubSolutionFiles(sol)
	subSolutionSet := make(map[string]bool, len(subSolutionFiles))
	for _, sf := range subSolutionFiles {
		subSolutionSet[filepath.Clean(sf)] = true
	}

	for _, relPath := range staticFiles {
		// When analyzing a sub-solution, normalize paths relative to the parent bundle root.
		normalizedPath := relPath
		if currentRoot != parentBundleRoot {
			relFromParent, err := filepath.Rel(parentBundleRoot, filepath.Join(currentRoot, relPath))
			if err == nil {
				normalizedPath = relFromParent
			}
		}
		if err := addFileEntry(result, seen, cfg, parentBundleRoot, normalizedPath, StaticAnalysis); err != nil {
			return nil, fmt.Errorf("static analysis: %w", err)
		}
	}

	// Deduplicate catalog refs
	catalogSeen := make(map[string]bool)
	for _, ref := range catalogRefs {
		if !catalogSeen[ref.Ref] {
			result.CatalogRefs = append(result.CatalogRefs, ref)
			catalogSeen[ref.Ref] = true
		}
	}

	// Phase 1b: Recursively discover files from local sub-solutions
	for _, subPath := range subSolutionFiles {
		absSubPath := filepath.Join(currentRoot, subPath)
		cleanAbsSubPath := filepath.Clean(absSubPath)

		// Circular reference detection
		if visitedPaths[cleanAbsSubPath] {
			return nil, fmt.Errorf("circular sub-solution reference detected: %s", subPath)
		}
		visitedPaths[cleanAbsSubPath] = true

		// Read and parse the sub-solution
		subContent, err := cfg.readFile(absSubPath)
		if err != nil {
			// Sub-solution file not found — already added as a file entry, skip recursive analysis
			continue
		}

		var subSol solution.Solution
		if err := subSol.UnmarshalFromBytes(subContent); err != nil {
			// Not a valid solution YAML — skip recursive analysis
			continue
		}

		subRoot := filepath.Dir(absSubPath)
		subResult, err := discoverFilesRecursive(&subSol, parentBundleRoot, subRoot, visitedPaths, opts...)
		if err != nil {
			return nil, fmt.Errorf("recursive discovery of sub-solution %s: %w", subPath, err)
		}

		// Merge sub-solution discovery results into parent results
		for _, f := range subResult.LocalFiles {
			if !seen[f.RelPath] {
				seen[f.RelPath] = true
				result.LocalFiles = append(result.LocalFiles, f)
			}
		}
		for _, ref := range subResult.CatalogRefs {
			if !catalogSeen[ref.Ref] {
				catalogSeen[ref.Ref] = true
				result.CatalogRefs = append(result.CatalogRefs, ref)
			}
		}
	}

	// Phase 2: Expand explicit bundle.include globs
	if len(sol.Bundle.Include) > 0 {
		// For sub-solutions, expand globs relative to their own directory
		// but normalize results relative to the parent bundle root.
		expandRoot := currentRoot
		expanded, err := expandGlobs(expandRoot, sol.Bundle.Include, cfg)
		if err != nil {
			return nil, fmt.Errorf("bundle.include expansion: %w", err)
		}
		for _, relPath := range expanded {
			normalizedPath := relPath
			if currentRoot != parentBundleRoot {
				relFromParent, relErr := filepath.Rel(parentBundleRoot, filepath.Join(currentRoot, relPath))
				if relErr == nil {
					normalizedPath = relFromParent
				}
			}
			if err := addFileEntry(result, seen, cfg, parentBundleRoot, normalizedPath, ExplicitInclude); err != nil {
				return nil, fmt.Errorf("bundle.include: %w", err)
			}
		}
	}

	// Phase 3: Scan test file references from spec.testing.cases[*].files
	if sol.Spec.Testing != nil {
		for _, tc := range sol.Spec.Testing.Cases {
			if tc == nil {
				continue
			}
			for _, fileRef := range tc.Files {
				normalizedRef := fileRef
				if currentRoot != parentBundleRoot {
					relFromParent, relErr := filepath.Rel(parentBundleRoot, filepath.Join(currentRoot, fileRef))
					if relErr == nil {
						normalizedRef = relFromParent
					}
				}
				// Test file references may be globs — expand them
				if strings.ContainsAny(fileRef, "*?[{") {
					expanded, err := expandSingleGlob(currentRoot, fileRef, cfg)
					if err != nil {
						// Log warning but don't fail — globs are validated at test time
						continue
					}
					for _, relPath := range expanded {
						normalizedPath := relPath
						if currentRoot != parentBundleRoot {
							relFromParent, relErr := filepath.Rel(parentBundleRoot, filepath.Join(currentRoot, relPath))
							if relErr == nil {
								normalizedPath = relFromParent
							}
						}
						if err := addFileEntry(result, seen, cfg, parentBundleRoot, normalizedPath, TestInclude); err != nil {
							continue // skip files that don't exist yet
						}
					}
				} else {
					if err := addFileEntry(result, seen, cfg, parentBundleRoot, normalizedRef, TestInclude); err != nil {
						continue // skip files that don't exist yet
					}
				}
			}
		}
	}

	return result, nil
}

// identifySubSolutionFiles returns local file paths that are sub-solution references
// (solution provider with literal local source paths).
func identifySubSolutionFiles(sol *solution.Solution) []string {
	var subFiles []string
	subSeen := make(map[string]bool)

	for _, r := range sol.Spec.Resolvers {
		if r == nil || r.Resolve == nil {
			continue
		}
		for _, ps := range r.Resolve.With {
			if ps.Provider == "solution" {
				if source := extractLiteralString(ps.Inputs, "source"); source != "" && isLocalPath(source) {
					clean := filepath.Clean(source)
					if !subSeen[clean] {
						subSeen[clean] = true
						subFiles = append(subFiles, source)
					}
				}
			}
		}
	}

	// Also check actions
	if sol.Spec.Workflow != nil {
		collectSubSolutionActions(sol.Spec.Workflow.Actions, &subFiles, subSeen)
		collectSubSolutionActions(sol.Spec.Workflow.Finally, &subFiles, subSeen)
	}

	return subFiles
}

// collectSubSolutionActions scans actions for local sub-solution references.
func collectSubSolutionActions(actions map[string]*actionpkg.Action, subFiles *[]string, subSeen map[string]bool) {
	for _, a := range actions {
		if a == nil || a.Provider != "solution" {
			continue
		}
		if source := extractLiteralString(a.Inputs, "source"); source != "" && isLocalPath(source) {
			clean := filepath.Clean(source)
			if !subSeen[clean] {
				subSeen[clean] = true
				*subFiles = append(*subFiles, source)
			}
		}
	}
}

// addFileEntry validates and adds a file to the discovery result, respecting ignore rules.
func addFileEntry(result *DiscoveryResult, seen map[string]bool, cfg *discoverConfig, bundleRoot, relPath string, source DiscoverySource) error {
	// Normalize
	relPath = filepath.Clean(relPath)

	// Reject absolute paths
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("absolute path not allowed in bundle: %s", relPath)
	}

	// Reject path traversal above bundle root
	if strings.HasPrefix(relPath, ".."+string(filepath.Separator)) || relPath == ".." {
		return fmt.Errorf("path escapes bundle root: %s", relPath)
	}

	// Deduplicate
	if seen[relPath] {
		return nil
	}

	// Check ignore rules
	if cfg.ignoreChecker.IsIgnored(relPath) {
		return nil
	}

	// Verify the file exists
	absPath := filepath.Join(bundleRoot, relPath)
	info, err := cfg.statFunc(absPath)
	if err != nil {
		return fmt.Errorf("file not found: %s", relPath)
	}

	// Skip directories
	if info.IsDir() {
		return nil
	}

	// Check for symlinks that escape the bundle root
	resolved, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		// Resolve the bundle root as well so both sides are canonical
		absBundleRoot, errRoot := filepath.EvalSymlinks(bundleRoot)
		if errRoot != nil {
			absBundleRoot, _ = filepath.Abs(bundleRoot)
		} else {
			absBundleRoot, _ = filepath.Abs(absBundleRoot)
		}
		absResolved, _ := filepath.Abs(resolved)
		if !strings.HasPrefix(absResolved, absBundleRoot+string(filepath.Separator)) && absResolved != absBundleRoot {
			return fmt.Errorf("symlink escapes bundle root: %s -> %s", relPath, resolved)
		}
	}

	seen[relPath] = true
	result.LocalFiles = append(result.LocalFiles, FileEntry{
		RelPath: relPath,
		Source:  source,
	})
	return nil
}

// analyzeProviderInputs walks the solution spec to extract literal file paths
// and catalog references from known provider inputs.
func analyzeProviderInputs(sol *solution.Solution) (localFiles []string, catalogRefs []CatalogRefEntry) {
	// Walk resolver resolve.with entries
	for _, r := range sol.Spec.Resolvers {
		if r == nil || r.Resolve == nil {
			continue
		}
		for _, ps := range r.Resolve.With {
			switch ps.Provider {
			case "file":
				if path := extractLiteralString(ps.Inputs, "path"); path != "" {
					if op := extractLiteralString(ps.Inputs, "operation"); op == "" || op == "read" {
						if isLocalPath(path) {
							localFiles = append(localFiles, path)
						}
					}
				}
			case "solution":
				if source := extractLiteralString(ps.Inputs, "source"); source != "" {
					classifySource(source, &localFiles, &catalogRefs)
				}
			}
		}

		// Walk transform.with entries for file provider
		if r.Transform != nil {
			for _, pt := range r.Transform.With {
				if pt.Provider == "file" {
					if path := extractLiteralString(pt.Inputs, "path"); path != "" {
						if isLocalPath(path) {
							localFiles = append(localFiles, path)
						}
					}
				}
			}
		}
	}

	// Walk action inputs
	if sol.Spec.Workflow != nil {
		walkActionInputs(sol.Spec.Workflow.Actions, &localFiles, &catalogRefs)
		walkActionInputs(sol.Spec.Workflow.Finally, &localFiles, &catalogRefs)
	}

	return localFiles, catalogRefs
}

func walkActionInputs(actions map[string]*actionpkg.Action, localFiles *[]string, catalogRefs *[]CatalogRefEntry) {
	for _, a := range actions {
		if a == nil {
			continue
		}
		switch a.Provider {
		case "file":
			// Only bundle paths for read operations. Non-read operations
			// (write/delete/exists) are outputs or runtime checks and are
			// intentionally excluded from bundling.
			if op := extractLiteralString(a.Inputs, "operation"); op == "" || op == "read" {
				if path := extractLiteralString(a.Inputs, "path"); path != "" {
					if isLocalPath(path) {
						*localFiles = append(*localFiles, path)
					}
				}
			}
		case "solution":
			if source := extractLiteralString(a.Inputs, "source"); source != "" {
				classifySource(source, localFiles, catalogRefs)
			}
		}
	}
}

// extractLiteralString returns the literal string value from a ValueRef inputs map,
// or empty string if the key is missing, nil, or not a literal string.
func extractLiteralString(inputs map[string]*spec.ValueRef, key string) string {
	if inputs == nil {
		return ""
	}
	vr := inputs[key]
	if vr == nil {
		return ""
	}
	// Only literal values are analyzed — expr, tmpl, rslvr are skipped.
	if vr.Expr != nil || vr.Tmpl != nil || vr.Resolver != nil {
		return ""
	}
	s, ok := vr.Literal.(string)
	if !ok {
		return ""
	}
	return s
}

// classifySource determines whether a source string is a local path or catalog reference.
func classifySource(source string, localFiles *[]string, catalogRefs *[]CatalogRefEntry) {
	if isLocalPath(source) {
		*localFiles = append(*localFiles, source)
	} else if isCatalogRef(source) {
		vendorPath := ".scafctl/vendor/" + source + ".yaml"
		*catalogRefs = append(*catalogRefs, CatalogRefEntry{
			Ref:        source,
			VendorPath: vendorPath,
		})
	}
}

// isLocalPath returns true if a path looks like a local file reference.
// Local paths start with ./, ../, or are plain relative paths without @ or scheme.
func isLocalPath(path string) bool {
	if path == "" {
		return false
	}
	// URLs
	if strings.Contains(path, "://") {
		return false
	}
	// Catalog references (contain @)
	if strings.Contains(path, "@") {
		return false
	}
	// Absolute paths — technically these are local but forbidden in bundles
	if filepath.IsAbs(path) {
		return false
	}
	return true
}

// isCatalogRef returns true if a source string looks like a catalog reference.
// Catalog references contain @ (e.g., "deploy-to-k8s@2.0.0") or are bare names.
func isCatalogRef(source string) bool {
	if source == "" {
		return false
	}
	if strings.Contains(source, "://") {
		return false
	}
	// Has @ — versioned catalog reference
	if strings.Contains(source, "@") {
		return true
	}
	// Bare name without path separators or file extensions
	if !strings.Contains(source, "/") && !strings.Contains(source, "\\") {
		ext := filepath.Ext(source)
		if ext != ".yaml" && ext != ".yml" && ext != ".json" {
			return true
		}
	}
	return false
}

// expandGlobs expands glob patterns against the bundle root directory.
func expandGlobs(bundleRoot string, patterns []string, cfg *discoverConfig) ([]string, error) {
	var result []string

	for _, pattern := range patterns {
		// Normalize the pattern
		pattern = filepath.Clean(pattern)

		if strings.ContainsAny(pattern, "*?[{") {
			// Glob pattern — expand it
			matches, err := expandSingleGlob(bundleRoot, pattern, cfg)
			if err != nil {
				return nil, fmt.Errorf("failed to expand glob %q: %w", pattern, err)
			}
			// Warn-level: no matches is not an error but we log it
			result = append(result, matches...)
		} else {
			// Explicit file path
			result = append(result, pattern)
		}
	}

	return result, nil
}

// expandSingleGlob expands a single glob pattern against the bundle root.
func expandSingleGlob(bundleRoot, pattern string, cfg *discoverConfig) ([]string, error) {
	var matches []string

	err := cfg.walkDir(bundleRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr // skip inaccessible paths
		}
		if info.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(bundleRoot, path)
		if relErr != nil {
			return nil //nolint:nilerr // skip paths that can't be made relative
		}

		// Use forward slashes for matching consistency
		rel = filepath.ToSlash(rel)
		patternSlash := filepath.ToSlash(pattern)

		matched, matchErr := doublestar.Match(patternSlash, rel)
		if matchErr != nil {
			return nil //nolint:nilerr // skip invalid patterns
		}
		if matched {
			matches = append(matches, filepath.FromSlash(rel))
		}
		return nil
	})

	return matches, err
}
