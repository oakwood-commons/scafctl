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

// ExpandGlobs resolves glob patterns against the filesystem, returning
// a sorted, deduplicated list of matching file paths (relative to baseDir).
// Supports ** for recursive matching via doublestar.
// Returns ErrPatternInvalid for malformed patterns and ErrNoMatches when
// a pattern resolves to zero files.
func ExpandGlobs(baseDir string, patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{})
	var result []string

	for _, pattern := range patterns {
		if !doublestar.ValidatePattern(pattern) {
			return nil, fmt.Errorf("%w: %q", ErrPatternInvalid, pattern)
		}

		// Reject absolute patterns and path traversal to prevent
		// fingerprinting files outside the solution tree.
		if filepath.IsAbs(pattern) {
			return nil, fmt.Errorf("%w: absolute patterns not allowed: %q", ErrPatternInvalid, pattern)
		}
		for _, seg := range strings.Split(filepath.Clean(pattern), string(filepath.Separator)) {
			if seg == ".." {
				return nil, fmt.Errorf("%w: path traversal not allowed: %q", ErrPatternInvalid, pattern)
			}
		}

		absPattern := filepath.Join(baseDir, pattern)
		matches, err := doublestar.FilepathGlob(absPattern, doublestar.WithFilesOnly())
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrPatternInvalid, pattern, err)
		}

		if len(matches) == 0 {
			return nil, fmt.Errorf("%w: %q", ErrNoMatches, pattern)
		}

		for _, m := range matches {
			rel, relErr := filepath.Rel(baseDir, m)
			if relErr != nil {
				return nil, fmt.Errorf("%w: match %q escapes base directory", ErrPatternInvalid, m)
			}
			// Ensure the match doesn't escape baseDir via ..
			if rel == ".." || strings.HasPrefix(rel, "../") || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("%w: match %q escapes base directory", ErrPatternInvalid, m)
			}
			if _, exists := seen[rel]; !exists {
				seen[rel] = struct{}{}
				result = append(result, rel)
			}
		}
	}

	sort.Strings(result)
	return result, nil
}
