// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package soltesting

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

// SolutionTests groups the tests extracted from a single solution file.
type SolutionTests struct {
	// SolutionName is the metadata.name from the solution.
	SolutionName string `json:"solutionName"`
	// Cases contains the test definitions keyed by test name.
	Cases map[string]*TestCase `json:"cases"`
	// Config holds the solution-level test configuration.
	Config *TestConfig `json:"config,omitempty"`
	// FilePath is the absolute path to the solution file.
	FilePath string `json:"filePath"`
	// BundleIncludes contains the bundle.include patterns from the solution spec.
	// These are resolved and copied into every test sandbox.
	BundleIncludes []string `json:"bundleIncludes,omitempty"`
	// DetectedFiles contains file patterns auto-detected from resolver specs
	// (e.g., directory provider paths). Used to populate builtin test files.
	DetectedFiles []string `json:"detectedFiles,omitempty"`
}

// FilterOptions specifies how to filter discovered tests.
type FilterOptions struct {
	// NamePatterns are glob patterns matched against test names.
	// If a pattern contains "/", it is split into solution-glob/test-glob.
	NamePatterns []string
	// Tags filters tests that have at least one matching tag (any-match).
	Tags []string
	// SolutionPatterns are glob patterns matched against solution names.
	SolutionPatterns []string
}

// DiscoverSolutions recursively finds solution files in the given path,
// parses their specs, and extracts tests. The path can be a file or directory.
func DiscoverSolutions(testsPath string) ([]SolutionTests, error) {
	info, err := os.Stat(testsPath)
	if err != nil {
		return nil, fmt.Errorf("stat %q: %w", testsPath, err)
	}

	if !info.IsDir() {
		st, err := DiscoverFromFile(testsPath)
		if err != nil {
			return nil, err
		}
		if st == nil {
			return nil, nil
		}
		return []SolutionTests{*st}, nil
	}

	var results []SolutionTests
	err = filepath.Walk(testsPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.IsDir() {
			return nil
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		st, parseErr := DiscoverFromFile(path)
		if parseErr != nil {
			// Skip files that aren't valid solutions
			return nil //nolint:nilerr // intentionally skip non-solution files
		}
		if st != nil {
			results = append(results, *st)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %q: %w", testsPath, err)
	}

	// Sort by solution name for deterministic ordering
	sort.Slice(results, func(i, j int) bool {
		return results[i].SolutionName < results[j].SolutionName
	})

	return results, nil
}

// DiscoverFromFile parses a single solution file and extracts tests.
// Returns nil if the file has no tests defined.
func DiscoverFromFile(filePath string) (*SolutionTests, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading %q: %w", filePath, err)
	}

	// Parse just enough to extract tests — we use a minimal struct
	// to avoid importing the full solution package (which would create a cycle).
	var doc struct {
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
		Compose []string `yaml:"compose"`
		Bundle  struct {
			Include []string `yaml:"include"`
		} `yaml:"bundle"`
		Spec struct {
			Testing   *TestSuite `yaml:"testing"`
			Resolvers yaml.Node  `yaml:"resolvers"`
		} `yaml:"spec"`
	}

	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing %q: %w", filePath, err)
	}

	// Best-effort parse of resolvers for file dependency detection.
	// Only map-shaped resolvers are supported — arrays or other forms are silently ignored.
	var resolverSpecs map[string]minimalResolver
	if doc.Spec.Resolvers.Kind == yaml.MappingNode {
		_ = doc.Spec.Resolvers.Decode(&resolverSpecs) // ignore errors — best-effort
	}

	// Process compose files — merge tests from referenced files
	if len(doc.Compose) > 0 {
		solutionDir := filepath.Dir(filePath)
		for _, pattern := range doc.Compose {
			absPattern := pattern
			if !filepath.IsAbs(pattern) {
				absPattern = filepath.Join(solutionDir, pattern)
			}
			matches, globErr := doublestar.FilepathGlob(absPattern)
			if globErr != nil {
				return nil, fmt.Errorf("compose glob %q: %w", pattern, globErr)
			}
			for _, match := range matches {
				composeData, readErr := os.ReadFile(match)
				if readErr != nil {
					return nil, fmt.Errorf("reading compose file %q: %w", match, readErr)
				}
				var composePart struct {
					Spec struct {
						Testing   *TestSuite `yaml:"testing"`
						Resolvers yaml.Node  `yaml:"resolvers"`
					} `yaml:"spec"`
				}
				if unmarshalErr := yaml.Unmarshal(composeData, &composePart); unmarshalErr != nil {
					return nil, fmt.Errorf("parsing compose file %q: %w", match, unmarshalErr)
				}
				// Merge resolvers from compose file for dependency detection
				if composePart.Spec.Resolvers.Kind == yaml.MappingNode {
					var composeResolvers map[string]minimalResolver
					if decErr := composePart.Spec.Resolvers.Decode(&composeResolvers); decErr == nil {
						if resolverSpecs == nil {
							resolverSpecs = make(map[string]minimalResolver)
						}
						for k, v := range composeResolvers {
							resolverSpecs[k] = v
						}
					}
				}

				// Initialize doc testing if nil
				if doc.Spec.Testing == nil {
					doc.Spec.Testing = &TestSuite{}
				}
				// Merge cases from compose file
				if composePart.Spec.Testing != nil {
					if doc.Spec.Testing.Cases == nil {
						doc.Spec.Testing.Cases = make(map[string]*TestCase)
					}
					for name, tc := range composePart.Spec.Testing.Cases {
						if _, exists := doc.Spec.Testing.Cases[name]; exists {
							return nil, fmt.Errorf("compose file %q: duplicate test name %q", match, name)
						}
						doc.Spec.Testing.Cases[name] = tc
					}
					// Merge config from compose file
					if composePart.Spec.Testing.Config != nil {
						if doc.Spec.Testing.Config == nil {
							doc.Spec.Testing.Config = composePart.Spec.Testing.Config
						} else {
							mergeTestConfig(doc.Spec.Testing.Config, composePart.Spec.Testing.Config)
						}
					}
				}
			}
		}
	}

	if doc.Spec.Testing == nil || len(doc.Spec.Testing.Cases) == 0 {
		return nil, nil
	}

	// Set names from map keys
	for name, tc := range doc.Spec.Testing.Cases {
		tc.Name = name
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	return &SolutionTests{
		SolutionName:   doc.Metadata.Name,
		Cases:          doc.Spec.Testing.Cases,
		Config:         doc.Spec.Testing.Config,
		FilePath:       absPath,
		BundleIncludes: doc.Bundle.Include,
		DetectedFiles:  detectFileDependencies(resolverSpecs),
	}, nil
}

// mergeTestConfig merges src into dst following compose merge rules.
func mergeTestConfig(dst, src *TestConfig) {
	// SkipBuiltins: true wins; lists are unioned
	if src.SkipBuiltins.All {
		dst.SkipBuiltins.All = true
	} else if len(src.SkipBuiltins.Names) > 0 && !dst.SkipBuiltins.All {
		seen := make(map[string]bool, len(dst.SkipBuiltins.Names))
		for _, n := range dst.SkipBuiltins.Names {
			seen[n] = true
		}
		for _, n := range src.SkipBuiltins.Names {
			if !seen[n] {
				dst.SkipBuiltins.Names = append(dst.SkipBuiltins.Names, n)
			}
		}
	}
	// Setup/cleanup: appended in compose-file order
	dst.Setup = append(dst.Setup, src.Setup...)
	dst.Cleanup = append(dst.Cleanup, src.Cleanup...)
	// Env: merged (last compose file wins on key conflict)
	if len(src.Env) > 0 {
		if dst.Env == nil {
			dst.Env = make(map[string]string)
		}
		for k, v := range src.Env {
			dst.Env[k] = v
		}
	}
}

// FilterTests applies the filter options to the discovered tests.
// All filter criteria are ANDed together. Returns a new slice with filtered results.
// Template tests (names starting with _) are excluded from results.
func FilterTests(solutions []SolutionTests, opts FilterOptions) []SolutionTests {
	var result []SolutionTests

	for _, st := range solutions {
		// Check solution-level filters
		if !matchesSolutionFilter(st.SolutionName, opts.SolutionPatterns) {
			continue
		}

		filtered := make(map[string]*TestCase)
		for name, tc := range st.Cases {
			// Exclude templates from execution
			if tc.IsTemplate() {
				continue
			}

			if !matchesNameFilter(st.SolutionName, name, opts.NamePatterns) {
				continue
			}

			if !matchesTagFilter(tc.Tags, opts.Tags) {
				continue
			}

			filtered[name] = tc
		}

		if len(filtered) > 0 {
			result = append(result, SolutionTests{
				SolutionName:   st.SolutionName,
				Cases:          filtered,
				Config:         st.Config,
				FilePath:       st.FilePath,
				BundleIncludes: st.BundleIncludes,
				DetectedFiles:  st.DetectedFiles,
			})
		}
	}

	// Sort solutions by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].SolutionName < result[j].SolutionName
	})

	return result
}

// SortedTestNames returns the test names from a SolutionTests in sorted order.
// Builtin tests (prefixed with "builtin:") sort first, then alphabetical.
func SortedTestNames(st SolutionTests) []string {
	names := make([]string, 0, len(st.Cases))
	for name := range st.Cases {
		names = append(names, name)
	}

	sort.Slice(names, func(i, j int) bool {
		iBuiltin := strings.HasPrefix(names[i], "builtin:")
		jBuiltin := strings.HasPrefix(names[j], "builtin:")

		if iBuiltin != jBuiltin {
			return iBuiltin // builtins first
		}
		return names[i] < names[j]
	})

	return names
}

// matchesSolutionFilter checks if the solution name matches any solution pattern.
// Empty patterns match everything.
func matchesSolutionFilter(solutionName string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		matched, err := doublestar.Match(p, solutionName)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// matchesNameFilter checks if the test name matches any name pattern.
// If a pattern contains "/", it is split into solution-glob/test-glob.
// Empty patterns match everything.
func matchesNameFilter(solutionName, testName string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if strings.Contains(p, "/") {
			parts := strings.SplitN(p, "/", 2)
			solMatch, err1 := doublestar.Match(parts[0], solutionName)
			testMatch, err2 := doublestar.Match(parts[1], testName)
			if err1 == nil && err2 == nil && solMatch && testMatch {
				return true
			}
		} else {
			matched, err := doublestar.Match(p, testName)
			if err == nil && matched {
				return true
			}
		}
	}
	return false
}

// matchesTagFilter checks if the test has at least one matching tag (any-match).
// Empty filter tags match everything.
func matchesTagFilter(testTags, filterTags []string) bool {
	if len(filterTags) == 0 {
		return true
	}
	tagSet := make(map[string]bool, len(testTags))
	for _, t := range testTags {
		tagSet[t] = true
	}
	for _, ft := range filterTags {
		if tagSet[ft] {
			return true
		}
	}
	return false
}

// minimalResolver is a lightweight struct for extracting directory provider
// paths without importing the full resolver package.
type minimalResolver struct {
	Resolve *struct {
		With []struct {
			Provider string         `yaml:"provider"`
			Inputs   map[string]any `yaml:"inputs"`
		} `yaml:"with"`
	} `yaml:"resolve"`
}

// detectFileDependencies scans minimally-parsed resolver specs for directory
// provider path inputs and returns glob patterns covering those paths.
// This enables builtin tests to auto-detect file dependencies.
func detectFileDependencies(resolvers map[string]minimalResolver) []string {
	seen := make(map[string]bool)
	var patterns []string

	for _, res := range resolvers {
		if res.Resolve == nil {
			continue
		}
		for _, step := range res.Resolve.With {
			if step.Provider != "directory" {
				continue
			}
			pathVal, ok := step.Inputs["path"]
			if !ok {
				continue
			}
			pathStr, ok := pathVal.(string)
			if !ok || pathStr == "" {
				continue
			}
			// Skip absolute paths and dynamic references
			if filepath.IsAbs(pathStr) ||
				strings.Contains(pathStr, "{{") ||
				strings.HasPrefix(pathStr, "expr:") ||
				strings.HasPrefix(pathStr, "rslvr:") ||
				strings.HasPrefix(pathStr, "tmpl:") {
				continue
			}
			// Convert directory path to a recursive glob
			pattern := strings.TrimSuffix(pathStr, "/") + "/**"
			if !seen[pattern] {
				seen[pattern] = true
				patterns = append(patterns, pattern)
			}
		}
	}

	sort.Strings(patterns)
	return patterns
}
