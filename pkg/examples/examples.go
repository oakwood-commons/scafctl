// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package examples provides access to embedded scafctl example files.
//
// Examples are embedded at build time via go:embed, making them available
// in distributed binaries without filesystem access to the source repo.
//
// For development, examples are also looked up from the filesystem as a
// fallback when the embedded filesystem is empty or when the examples
// weren't copied at build time.
package examples

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrPathTraversal is returned when a path contains ".." components.
var ErrPathTraversal = errors.New("path must not contain '..'")

// ErrAmbiguousExample is returned when a lookup query matches more than one
// example. It is a sentinel error: ResolveExample wraps it with the candidate
// paths in the error string (e.g. `...: "hello-world" matches a.yaml, b.yaml`),
// so callers detect it with errors.Is and can surface the message to the user.
var ErrAmbiguousExample = errors.New("ambiguous example query")

// ErrExampleNotFound is returned when a lookup query matches no example.
var ErrExampleNotFound = errors.New("example not found")

//go:embed files/*
var EmbeddedExamples embed.FS

// solutionKind is the only artifact kind surfaced as a runnable example. Config
// files, auth-handler configs, compose partials, and rendered templates that
// live under examples/ are intentionally excluded from the listing.
const solutionKind = "Solution"

// Example represents a runnable example solution in the listing. Fields are
// sourced from the solution's own metadata block (not the filename), so the
// listing reflects what the author declared.
type Example struct {
	// DisplayName is the human-friendly name from metadata.displayName.
	// Falls back to metadata.name when unset.
	DisplayName string `json:"displayName,omitempty"`
	// Name is the solution's metadata.name (its stable identifier).
	Name string `json:"name"`
	// Category is metadata.category (falls back to the top-level directory).
	Category string `json:"category,omitempty"`
	// Tags are metadata.tags.
	Tags []string `json:"tags,omitempty"`
	// Description is metadata.description (first line, trimmed).
	Description string `json:"description,omitempty"`
	// Path is the embedded-FS path used as the unambiguous fetch handle for
	// `get examples <path>` (metadata.name is not unique across examples).
	Path string `json:"path"`
}

// exampleMeta is a lightweight view of a solution used only to populate the
// listing. It deliberately parses just the fields the listing needs so a quirky
// (but valid) example never breaks scanning.
type exampleMeta struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name        string   `yaml:"name"`
		DisplayName string   `yaml:"displayName"`
		Category    string   `yaml:"category"`
		Description string   `yaml:"description"`
		Tags        []string `yaml:"tags"`
	} `yaml:"metadata"`
}

// Scan walks the examples filesystem and returns matching example solutions.
// Only kind: Solution files are returned; non-solution YAML (configs, partials,
// templates) is skipped. If category is non-empty, only examples in that
// category are returned. Metadata is read from each solution's own metadata
// block.
func Scan(category string) ([]Example, error) {
	examplesFS, root, err := getExamplesFS()
	if err != nil {
		return nil, err
	}

	var items []Example
	err = fs.WalkDir(examplesFS, root, func(fpath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		ext := path.Ext(fpath)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		relPath := relFromRoot(fpath, root)

		// Skip intentionally invalid/demo examples that exist to trigger errors
		// or stress lint rules -- they are not runnable reference examples.
		if strings.Contains(relPath, "bad-solution") || strings.Contains(relPath, "lint-stress-test") {
			return nil
		}

		content, err := fs.ReadFile(examplesFS, fpath)
		if err != nil {
			return nil //nolint:nilerr // skip unreadable file, keep scanning
		}

		var meta exampleMeta
		if err := yaml.Unmarshal(content, &meta); err != nil {
			// Not valid YAML (e.g. a Go-template partial) -- not a listable example.
			return nil //nolint:nilerr
		}
		if meta.Kind != solutionKind {
			return nil
		}

		// Category from metadata, falling back to the first path component.
		cat := meta.Metadata.Category
		if cat == "" {
			if parts := strings.SplitN(relPath, "/", 2); len(parts) > 1 {
				cat = parts[0]
			}
		}
		if category != "" && cat != category {
			return nil
		}

		items = append(items, Example{
			DisplayName: displayNameOf(meta),
			Name:        meta.Metadata.Name,
			Category:    cat,
			Tags:        meta.Metadata.Tags,
			Description: firstLine(meta.Metadata.Description),
			Path:        relPath,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort by category, then displayName, then path (stable, path is unique).
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		if items[i].DisplayName != items[j].DisplayName {
			return items[i].DisplayName < items[j].DisplayName
		}
		return items[i].Path < items[j].Path
	})

	return items, nil
}

// ResolveExample resolves a user-supplied query to a single example path. The
// query may be an exact path, a metadata.name, or a file basename. When the
// query matches exactly one example, its path is returned. When it matches more
// than one, ErrAmbiguousExample is returned wrapped with the candidate paths so
// the caller can prompt for a precise path. When nothing matches,
// ErrExampleNotFound is returned.
func ResolveExample(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", ErrExampleNotFound
	}
	// Slash-normalize WITHOUT cleaning first, so a ".." component cannot be
	// resolved away before the traversal check (path.Clean("foo/../x") == "x").
	slashed := strings.ReplaceAll(filepath.ToSlash(query), "\\", "/")

	// Security: reject any ".." path segment before touching the filesystem. A
	// filename that merely contains ".." (e.g. foo..bar) is safe and allowed.
	if isUnsafeExamplePath(slashed) {
		return "", ErrPathTraversal
	}
	normalized := path.Clean(slashed)

	items, err := Scan("")
	if err != nil {
		return "", err
	}

	// 1. Exact path match wins immediately (paths are unique).
	for _, it := range items {
		if it.Path == normalized {
			return it.Path, nil
		}
	}

	// 2. Match by metadata.name or file basename (may be ambiguous).
	var matches []string
	for _, it := range items {
		base := strings.TrimSuffix(path.Base(it.Path), path.Ext(it.Path))
		if it.Name == query || base == query || base == normalized {
			matches = append(matches, it.Path)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("%w: %q", ErrExampleNotFound, query)
	case 1:
		return matches[0], nil
	default:
		sort.Strings(matches)
		return "", fmt.Errorf("%w: %q matches %s", ErrAmbiguousExample, query, strings.Join(matches, ", "))
	}
}

// displayNameOf returns metadata.displayName, falling back to metadata.name.
func displayNameOf(m exampleMeta) string {
	if m.Metadata.DisplayName != "" {
		return m.Metadata.DisplayName
	}
	return m.Metadata.Name
}

// firstLine returns the first non-empty line of s, trimmed. Multi-line
// descriptions (block scalars) collapse to their first line for the listing.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// isUnsafeExamplePath reports whether a slash-separated path is unsafe to use as
// an example lookup: either it contains a ".." traversal segment, or it is an
// absolute path (leading "/", or a Windows drive/UNC form). The input must be
// forward-slash separated but NOT path.Clean'd, so a ".." component cannot be
// normalized away (path.Clean("foo/../x") == "x") before the check runs.
// Example paths are always relative (e.g. "resolvers/hello-world.yaml"); an
// absolute input never names an embedded example and is rejected up front to
// keep the guard complete for any reuse of this resolver logic.
func isUnsafeExamplePath(p string) bool {
	if hasDotDotSegment(p) {
		return true
	}
	if strings.HasPrefix(p, "/") {
		return true // absolute (including UNC "\\\\server" -> "//server")
	}
	// Windows drive-letter absolute path, e.g. "C:/x".
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	return false
}

// hasDotDotSegment reports whether a slash-separated path contains a ".."
// component (a traversal segment), as opposed to merely containing the literal
// ".." inside a filename (e.g. "foo..bar.yaml", which is safe). The input must
// be forward-slash separated but must NOT have been path.Clean'd: callers invoke
// this before cleaning, precisely so a ".." component cannot be normalized away
// (path.Clean("foo/../x") == "x") before the traversal check runs.
func hasDotDotSegment(p string) bool {
	if p == ".." {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// relFromRoot returns the forward-slash relative path of fpath under root,
// handling both embed.FS (forward slashes) and OS filesystems (native
// separators) consistently.
func relFromRoot(fpath, root string) string {
	relPath := strings.TrimPrefix(fpath, root+"/")
	if relPath == fpath {
		if rel, err := filepath.Rel(root, fpath); err == nil {
			relPath = filepath.ToSlash(rel)
		}
	}
	return relPath
}

// Read returns the contents of an example file.
func Read(exPath string) (string, error) {
	examplesFS, root, err := getExamplesFS()
	if err != nil {
		return "", err
	}

	// Normalize to forward slashes WITHOUT cleaning first: embed.FS uses forward
	// slashes, a caller may pass Windows-style backslashes, and path.Clean would
	// resolve a ".." component away (path.Clean("foo/../x") == "x") before the
	// traversal check. filepath.ToSlash is a no-op for backslashes off Windows.
	slashed := strings.ReplaceAll(filepath.ToSlash(exPath), "\\", "/")

	// Security: reject any ".." path segment. A filename containing ".."
	// (e.g. foo..bar.yaml) is safe and allowed.
	if isUnsafeExamplePath(slashed) {
		return "", ErrPathTraversal
	}
	cleanPath := path.Clean(slashed)

	fullPath := path.Join(root, cleanPath)
	content, err := fs.ReadFile(examplesFS, fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read example %q: %w", exPath, err)
	}

	return string(content), nil
}

// Categories returns the list of available example categories.
func Categories() []string {
	items, err := Scan("")
	if err != nil {
		return nil
	}

	seen := make(map[string]bool)
	var cats []string
	for _, item := range items {
		if item.Category != "" && !seen[item.Category] {
			seen[item.Category] = true
			cats = append(cats, item.Category)
		}
	}
	sort.Strings(cats)
	return cats
}

// getExamplesFS returns either the embedded FS or a fallback OS FS.
func getExamplesFS() (fs.FS, string, error) {
	// Try embedded examples first — check for actual content (not just .gitkeep)
	entries, err := fs.ReadDir(EmbeddedExamples, "files")
	if err == nil {
		hasContent := false
		for _, e := range entries {
			if e.IsDir() || (e.Name() != ".gitkeep" && e.Name() != ".keep") {
				hasContent = true
				break
			}
		}
		if hasContent {
			return EmbeddedExamples, "files", nil
		}
	}

	// Fallback: find examples directory on the filesystem (development mode)
	dir, err := findExamplesDir()
	if err != nil {
		return nil, "", fmt.Errorf("examples not available: embedded examples not found and filesystem fallback failed: %w", err)
	}
	return os.DirFS(dir), ".", nil
}

// findExamplesDir locates the examples directory relative to the package source.
func findExamplesDir() (string, error) {
	// Strategy 1: Find relative to this source file (works in development/testing)
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		// This file is at pkg/examples/examples.go
		// Project root is ../../ from here
		pkgDir := filepath.Dir(thisFile)
		projectRoot := filepath.Join(pkgDir, "..", "..")
		examplesDir := filepath.Join(projectRoot, "examples")
		if info, err := os.Stat(examplesDir); err == nil && info.IsDir() {
			return examplesDir, nil
		}
	}

	// Strategy 2: Check current working directory
	cwd, err := os.Getwd()
	if err == nil {
		examplesDir := filepath.Join(cwd, "examples")
		if info, err := os.Stat(examplesDir); err == nil && info.IsDir() {
			return examplesDir, nil
		}
	}

	// Strategy 3: Walk up from cwd looking for examples/
	if err == nil {
		dir := cwd
		for i := 0; i < 10; i++ {
			examplesDir := filepath.Join(dir, "examples")
			if info, err := os.Stat(examplesDir); err == nil && info.IsDir() {
				return examplesDir, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return "", fmt.Errorf("could not locate examples directory")
}
