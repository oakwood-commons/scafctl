// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package get

import (
	"context"
	"fmt"
	pathlib "path/filepath"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
)

// DiscoveryRisk controls how multi-match ambiguity is handled during
// solution auto-discovery.
type DiscoveryRisk int

const (
	// DiscoveryRiskLow uses the first match and emits a warning when multiple
	// solution files are found. Suitable for read-only or low-consequence
	// commands like run, lint, and test.
	DiscoveryRiskLow DiscoveryRisk = iota

	// DiscoveryRiskHigh returns an error when multiple solution files are
	// found, requiring the user to specify -f explicitly. Suitable for
	// destructive or publishing commands like build and catalog push.
	DiscoveryRiskHigh
)

// ResolveOptions configures the unified resolution chain.
type ResolveOptions struct {
	// Risk controls ambiguity handling when multiple files are discovered.
	Risk DiscoveryRisk
	// DiscoveryMode controls which file names auto-discovery searches for.
	DiscoveryMode settings.DiscoveryMode
}

// Resolve implements the unified solution resolution chain:
//  1. If file is non-empty, return it (explicit -f flag takes precedence).
//  2. If positionalArg is non-empty, return it as-is (catalog/registry reference).
//  3. Otherwise, auto-discover with ambiguity handling based on risk level.
//
// When auto-discovery finds a solution, the resolved path is printed to stderr
// via the writer in context. When multiple matches exist, behavior depends on Risk.
func Resolve(ctx context.Context, getter *Getter, file, positionalArg string, opts ResolveOptions) (string, error) {
	// Step 1: explicit -f flag (takes precedence over positional arg)
	if file != "" {
		return file, nil
	}

	// Step 2: positional catalog/registry reference (returned as-is for
	// downstream resolution by the catalog client)
	if positionalArg != "" {
		return positionalArg, nil
	}

	// Step 3: auto-discovery with ambiguity handling
	if opts.DiscoveryMode != settings.DiscoveryModeDefault {
		getter.SetDiscoveryMode(opts.DiscoveryMode)
	}

	matches := getter.FindAllSolutions()
	if len(matches) == 0 {
		return "", ErrNoSolutionFound
	}

	resolved := matches[0].Path

	// Handle multi-match ambiguity before emitting any messages.
	// DiscoveryRiskHigh returns an error, so we must not print "Using" in that case.
	if len(matches) > 1 {
		others := make([]string, 0, len(matches)-1)
		for _, m := range matches[1:] {
			others = append(others, m.Path)
		}

		switch opts.Risk {
		case DiscoveryRiskLow:
			if w := writer.FromContext(ctx); w != nil {
				w.Verbosef("Multiple solution files found (also: %s); using first match", strings.Join(others, ", "))
			}
		case DiscoveryRiskHigh:
			return "", fmt.Errorf(
				"multiple solution files found: %s; use -f/--file to specify which one",
				strings.Join(allPaths(matches), ", "),
			)
		}
	}

	// Emit "Using <path>" to stderr in verbose mode only.
	// Written to stderr so it doesn't corrupt structured stdout (-o json).
	if w := writer.FromContext(ctx); w != nil {
		w.Verbosef("Using %s", resolved)
	}

	return resolved, nil
}

// FindAllSolutions searches for all solution files across configured discovery
// paths and returns all matches in priority order. Unlike FindSolution which
// stops at the first match, this returns every discoverable solution file.
func (o *Getter) FindAllSolutions() []DiscoveryResult {
	fileNames := o.solutionFileNames
	if o.discoveryMode != settings.DiscoveryModeDefault {
		binaryName := settings.CliBinaryName
		for _, fn := range o.solutionFileNames {
			if fn != "solution.yaml" && fn != "solution.yml" &&
				fn != "solution.json" && fn != "actions.yaml" && fn != "actions.yml" &&
				fn != "taskfile.yaml" && fn != "taskfile.yml" {
				binaryName = strings.TrimSuffix(strings.TrimSuffix(fn, ".yaml"), ".yml")
				binaryName = strings.TrimSuffix(binaryName, ".json")
				break
			}
		}
		fileNames = settings.FileNamesForMode(o.discoveryMode, binaryName, o.customActionFiles)
	}

	o.logger.V(1).Info("searching for all solution files",
		"folders", o.solutionFolders,
		"fileNames", fileNames,
		"mode", o.discoveryMode)

	var results []DiscoveryResult

	// Track which canonical paths we've already added to avoid duplicates.
	seen := make(map[string]bool)

	for _, folder := range o.solutionFolders {
		for _, filename := range fileNames {
			fullPath := filepath.NormalizeFilePath(pathlib.Join(folder, filename))
			if filepath.PathExists(fullPath, o.statFunc) {
				// Resolve to absolute path to detect duplicates from different
				// relative paths pointing to the same file.
				canonical := fullPath
				if abs, err := pathlib.Abs(fullPath); err == nil {
					canonical = abs
				}
				if seen[canonical] {
					continue
				}
				seen[canonical] = true

				results = append(results, DiscoveryResult{
					Path:         fullPath,
					IsActionFile: settings.IsActionFile(filename),
					Mode:         o.discoveryMode,
				})
			}
		}
	}

	// When in action mode, ensure action files sort before solution files
	// regardless of folder priority. Without this, a solution.yaml in a
	// higher-priority folder (e.g. scafctl/) would beat an actions.yaml
	// in the root directory.
	if o.discoveryMode == settings.DiscoveryModeAction && len(results) > 1 {
		sort.SliceStable(results, func(i, j int) bool {
			return results[i].IsActionFile && !results[j].IsActionFile
		})
	}

	// Update lastDiscovery with first result for backward compatibility.
	if len(results) > 0 {
		o.lastDiscovery = results[0]
	} else {
		o.lastDiscovery = DiscoveryResult{Mode: o.discoveryMode}
	}

	return results
}

// SearchedPaths returns a human-readable list of all paths that would be
// checked during auto-discovery. Useful for error messages.
func (o *Getter) SearchedPaths() []string {
	fileNames := o.solutionFileNames
	if o.discoveryMode != settings.DiscoveryModeDefault {
		binaryName := settings.CliBinaryName
		for _, fn := range o.solutionFileNames {
			if fn != "solution.yaml" && fn != "solution.yml" &&
				fn != "solution.json" && fn != "actions.yaml" && fn != "actions.yml" &&
				fn != "taskfile.yaml" && fn != "taskfile.yml" {
				binaryName = strings.TrimSuffix(strings.TrimSuffix(fn, ".yaml"), ".yml")
				binaryName = strings.TrimSuffix(binaryName, ".json")
				break
			}
		}
		fileNames = settings.FileNamesForMode(o.discoveryMode, binaryName, o.customActionFiles)
	}

	paths := make([]string, 0, len(o.solutionFolders)*len(fileNames))
	for _, folder := range o.solutionFolders {
		for _, filename := range fileNames {
			paths = append(paths, filepath.NormalizeFilePath(pathlib.Join(folder, filename)))
		}
	}
	return paths
}

// allPaths extracts the Path field from a slice of DiscoveryResults.
func allPaths(results []DiscoveryResult) []string {
	paths := make([]string, len(results))
	for i, r := range results {
		paths[i] = r.Path
	}
	return paths
}
