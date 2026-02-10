// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreChecker determines whether a file path should be excluded from bundling.
type IgnoreChecker interface {
	// IsIgnored returns true if the given relative path should be excluded.
	IsIgnored(relPath string) bool
}

// ScafctlIgnore implements IgnoreChecker using .scafctlignore rules.
// It supports a subset of .gitignore syntax:
//   - Blank lines and lines starting with # are ignored.
//   - Patterns are matched against relative paths using filepath.Match.
//   - A trailing / matches only directories (not supported yet — treated as prefix match).
//   - A leading / anchors to the bundle root.
//   - ** is supported via doublestar matching.
type ScafctlIgnore struct {
	patterns []ignorePattern
}

type ignorePattern struct {
	pattern  string
	negated  bool
	anchored bool
	dirOnly  bool
}

// LoadScafctlIgnore reads a .scafctlignore file and returns an IgnoreChecker.
// If the file does not exist, a no-op checker is returned (nothing is ignored).
func LoadScafctlIgnore(bundleRoot string) (IgnoreChecker, error) {
	return LoadScafctlIgnoreFrom(filepath.Join(bundleRoot, ".scafctlignore"))
}

// LoadScafctlIgnoreFrom reads ignore patterns from a specific file path.
func LoadScafctlIgnoreFrom(path string) (IgnoreChecker, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &noopIgnoreChecker{}, nil
		}
		return nil, err
	}
	defer f.Close()

	var patterns []ignorePattern
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := ignorePattern{}

		// Handle negation
		if strings.HasPrefix(line, "!") {
			p.negated = true
			line = line[1:]
		}

		// Handle anchored patterns (leading /)
		if strings.HasPrefix(line, "/") {
			p.anchored = true
			line = line[1:]
		}

		// Handle directory patterns (trailing /) — match as prefix
		isDir := strings.HasSuffix(line, "/")
		line = strings.TrimRight(line, "/")

		p.pattern = line
		p.dirOnly = isDir
		patterns = append(patterns, p)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &ScafctlIgnore{patterns: patterns}, nil
}

// ParseIgnorePatterns creates an IgnoreChecker from a list of pattern strings.
// Useful for testing.
func ParseIgnorePatterns(patterns []string) IgnoreChecker {
	var parsed []ignorePattern
	for _, line := range patterns {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		p := ignorePattern{}
		if strings.HasPrefix(line, "!") {
			p.negated = true
			line = line[1:]
		}
		if strings.HasPrefix(line, "/") {
			p.anchored = true
			line = line[1:]
		}
		isDir := strings.HasSuffix(line, "/")
		line = strings.TrimRight(line, "/")
		p.pattern = line
		p.dirOnly = isDir
		parsed = append(parsed, p)
	}
	return &ScafctlIgnore{patterns: parsed}
}

// IsIgnored returns true if the relative path matches any ignore pattern.
func (si *ScafctlIgnore) IsIgnored(relPath string) bool {
	if len(si.patterns) == 0 {
		return false
	}

	// Normalize to forward slashes for matching
	relPath = filepath.ToSlash(relPath)
	ignored := false

	for _, p := range si.patterns {
		if matchIgnorePattern(relPath, p) {
			ignored = !p.negated
		}
	}

	return ignored
}

// matchIgnorePattern checks if a path matches a single ignore pattern.
func matchIgnorePattern(relPath string, p ignorePattern) bool {
	pattern := p.pattern

	// Directory patterns match any file under that directory
	if p.dirOnly {
		if p.anchored {
			return strings.HasPrefix(relPath, pattern+"/") || relPath == pattern
		}
		// Non-anchored: match at any level
		if strings.HasPrefix(relPath, pattern+"/") || relPath == pattern {
			return true
		}
		if strings.Contains(relPath, "/"+pattern+"/") {
			return true
		}
		return false
	}

	// Anchored patterns match from root only
	if p.anchored {
		return matchPath(relPath, pattern)
	}

	// Non-anchored patterns match against any path segment
	// Try matching against the full path
	if matchPath(relPath, pattern) {
		return true
	}

	// Try matching against the basename
	base := filepath.Base(relPath)
	if matchPath(base, pattern) {
		return true
	}

	// Try matching against every suffix of the path
	parts := strings.Split(relPath, "/")
	for i := 1; i < len(parts); i++ {
		suffix := strings.Join(parts[i:], "/")
		if matchPath(suffix, pattern) {
			return true
		}
	}

	return false
}

// matchPath matches a path against a pattern that may contain ** globs.
func matchPath(path, pattern string) bool {
	if strings.Contains(pattern, "**") {
		return matchDoublestar(path, pattern)
	}

	matched, err := filepath.Match(pattern, path)
	return err == nil && matched
}

// matchDoublestar handles ** glob patterns.
func matchDoublestar(path, pattern string) bool {
	// Simple implementation: ** matches zero or more path segments
	parts := strings.Split(pattern, "**")
	if len(parts) == 1 {
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// Check if prefix matches
	prefix := parts[0]
	suffix := parts[len(parts)-1]

	if prefix != "" {
		prefix = strings.TrimRight(prefix, "/")
		if !strings.HasPrefix(path, prefix) {
			return false
		}
	}

	if suffix != "" {
		suffix = strings.TrimLeft(suffix, "/")
		matched, _ := filepath.Match(suffix, filepath.Base(path))
		if !matched {
			return false
		}
	}

	return true
}

// noopIgnoreChecker is an IgnoreChecker that never ignores any files.
type noopIgnoreChecker struct{}

func (n *noopIgnoreChecker) IsIgnored(_ string) bool {
	return false
}
