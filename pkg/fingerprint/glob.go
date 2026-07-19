// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package fingerprint

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ExpandResult reports the outcome of expanding a set of glob patterns.
type ExpandResult struct {
	// Files is the sorted, deduplicated list of matching file paths
	// (relative to baseDir, slash-separated).
	Files []string

	// EmptyPatterns lists the input patterns that matched zero files, in
	// input order. A non-empty EmptyPatterns with a non-empty Files means a
	// partial match (some patterns matched, some did not).
	EmptyPatterns []string
}

// AllEmpty reports whether every pattern matched zero files (i.e. there are no
// files to track). It is false when there were no patterns to begin with.
func (r ExpandResult) AllEmpty() bool {
	return len(r.Files) == 0 && len(r.EmptyPatterns) > 0
}

// ExpandGlobsReport resolves glob patterns against the filesystem, tolerating
// individual patterns that match no files. It returns the sorted, deduplicated
// list of matching files together with the patterns that matched nothing, so
// callers can decide how to surface partial or total no-match situations.
//
// Supports ** for recursive matching via doublestar. Malformed, absolute, or
// path-traversing patterns return ErrPatternInvalid (these are user or security
// errors, not "no files"). ExpandGlobsReport never returns ErrNoMatches -- an
// all-empty result is reported via ExpandResult.AllEmpty instead.
//
// This mirrors the lenient sources handling of build tools like go-task, where
// a glob matching nothing simply contributes no files to the checksum rather
// than aborting the up-to-date check.
func ExpandGlobsReport(baseDir string, patterns []string) (ExpandResult, error) {
	if len(patterns) == 0 {
		return ExpandResult{}, nil
	}

	seen := make(map[string]struct{})
	result := ExpandResult{}

	for _, pattern := range patterns {
		if !doublestar.ValidatePattern(pattern) {
			return ExpandResult{}, fmt.Errorf("%w: %q", ErrPatternInvalid, pattern)
		}

		// Reject absolute patterns and path traversal to prevent
		// fingerprinting files outside the solution tree.
		if filepath.IsAbs(pattern) || strings.HasPrefix(pattern, "/") {
			return ExpandResult{}, fmt.Errorf("%w: absolute patterns not allowed: %q", ErrPatternInvalid, pattern)
		}
		for _, seg := range strings.Split(filepath.Clean(pattern), string(filepath.Separator)) {
			if seg == ".." {
				return ExpandResult{}, fmt.Errorf("%w: path traversal not allowed: %q", ErrPatternInvalid, pattern)
			}
		}

		absPattern := filepath.Join(baseDir, pattern)
		matches, err := doublestar.FilepathGlob(absPattern, doublestar.WithFilesOnly())
		if err != nil {
			return ExpandResult{}, fmt.Errorf("%w: %q: %w", ErrPatternInvalid, pattern, err)
		}

		if len(matches) == 0 {
			// A pattern matching nothing is not fatal -- record it and move on.
			result.EmptyPatterns = append(result.EmptyPatterns, pattern)
			continue
		}

		for _, m := range matches {
			rel, relErr := filepath.Rel(baseDir, m)
			if relErr != nil {
				return ExpandResult{}, fmt.Errorf("%w: match %q escapes base directory", ErrPatternInvalid, m)
			}
			// Ensure the match doesn't escape baseDir via ..
			if rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return ExpandResult{}, fmt.Errorf("%w: match %q escapes base directory", ErrPatternInvalid, m)
			}
			normalized := filepath.ToSlash(rel)
			if _, exists := seen[normalized]; !exists {
				seen[normalized] = struct{}{}
				result.Files = append(result.Files, normalized)
			}
		}
	}

	sort.Strings(result.Files)
	return result, nil
}

// ExpandGlobs resolves glob patterns against the filesystem, returning a sorted,
// deduplicated list of matching file paths (relative to baseDir).
//
// Individual patterns that match no files are tolerated; ExpandGlobs only
// returns ErrNoMatches when the combined result is empty (every pattern matched
// nothing), i.e. there are genuinely no files to track. Malformed, absolute, or
// path-traversing patterns return ErrPatternInvalid.
//
// Callers that need to distinguish partial from total no-match (e.g. to warn a
// user about a typo'd source) should use ExpandGlobsReport instead.
func ExpandGlobs(baseDir string, patterns []string) ([]string, error) {
	report, err := ExpandGlobsReport(baseDir, patterns)
	if err != nil {
		return nil, err
	}
	if report.AllEmpty() {
		return nil, fmt.Errorf("%w: %q", ErrNoMatches, strings.Join(report.EmptyPatterns, ", "))
	}
	return report.Files, nil
}
