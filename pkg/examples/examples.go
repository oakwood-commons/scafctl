// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package examples provides access to embedded scafctl example files.
//
// Examples are embedded at compile time via go:embed (see the examplefiles
// package, which sits at the root of the examples/ tree so its directive can
// reach the files). This makes them available in distributed binaries and in
// the published Go module with no build-time copy step and no filesystem
// fallback -- the same content ships to a `go get` library consumer as to the
// released CLI, independent of the working directory.
package examples

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"

	examplefiles "github.com/oakwood-commons/scafctl/examples"
	"gopkg.in/yaml.v3"
)

// ErrPathTraversal is returned when an example lookup path is unsafe: it either
// contains a ".." traversal component or is an absolute/UNC/drive-letter path.
// Example paths are always relative (e.g. "resolvers/hello-world.yaml").
var ErrPathTraversal = errors.New("example path must be relative and must not contain '..'")

// ErrAmbiguousExample is returned when a lookup query matches more than one
// example. It is a sentinel error: ResolveExample wraps it with the candidate
// paths in the error string (e.g. `...: "hello-world" matches a.yaml, b.yaml`),
// so callers detect it with errors.Is and can surface the message to the user.
var ErrAmbiguousExample = errors.New("ambiguous example query")

// ErrExampleNotFound is returned when a lookup query matches no example.
var ErrExampleNotFound = errors.New("example not found")

// EmbeddedExamples is the in-place embedded example tree (see the examplefiles
// package). Its files are rooted at the examples/ directory, so paths are of
// the form "actions/action-alias.yaml".
var EmbeddedExamples = examplefiles.FS

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
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	// Name is the solution's metadata.name (its stable identifier).
	Name string `json:"name" yaml:"name"`
	// Category is metadata.category (falls back to the top-level directory).
	Category string `json:"category,omitempty" yaml:"category,omitempty"`
	// Tags are metadata.tags.
	Tags []string `json:"tags,omitempty" yaml:"tags,omitempty"`
	// Description is metadata.description (first line, trimmed).
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// Path is the embedded-FS path used as the unambiguous fetch handle for
	// `get examples <path>` (metadata.name is not unique across examples).
	Path string `json:"path" yaml:"path"`
	// Content is the full example file content. It powers the interactive (-i)
	// detail view so a user can read the solution without leaving the browser.
	// Omitted from the default table (see the command's column hints).
	Content string `json:"content,omitempty" yaml:"content,omitempty"`
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

		// Skip rendered "effective" golden artifacts (produced by
		// `render solution --effective`). They are kind: Solution documents but
		// are committed fixtures for fidelity diffing, not runnable examples, and
		// would collide by name with their source solution.
		if strings.HasSuffix(relPath, ".effective.yaml") || strings.HasSuffix(relPath, ".effective.yml") {
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
			Content:     string(content),
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

// MatchExamples resolves a user-supplied query to the example(s) it identifies.
// The query may be an exact embedded path, a metadata.name, or a file basename.
// It returns:
//
//   - exactly one Example when the query names a single example (by exact path,
//     or a unique name/basename),
//   - more than one Example when a name/basename matches several (the caller
//     should present them as a list to choose from),
//   - ErrExampleNotFound when nothing matches,
//   - ErrPathTraversal when the query is an unsafe path.
//
// Exact-path matches also resolve non-solution embedded files (e.g. kind:
// Config) that the solution-only listing excludes; those are returned as a
// single Example carrying just the Path.
func MatchExamples(query string) ([]Example, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrExampleNotFound
	}
	// Slash-normalize WITHOUT cleaning first, so a ".." component cannot be
	// resolved away before the traversal check (path.Clean("foo/../x") == "x").
	slashed := strings.ReplaceAll(filepath.ToSlash(query), "\\", "/")
	if isUnsafeExamplePath(slashed) {
		return nil, ErrPathTraversal
	}
	normalized := path.Clean(slashed)

	items, err := Scan("")
	if err != nil {
		return nil, err
	}

	// 1. Exact path match wins immediately (paths are unique): a listed solution
	//    is returned with full metadata...
	for _, it := range items {
		if it.Path == normalized {
			return []Example{it}, nil
		}
	}
	// ...and any other embedded example file by exact path (non-solution kinds
	// the listing excludes) is still fetchable.
	if exampleFileExists(normalized) {
		return []Example{{Name: normalized, Path: normalized}}, nil
	}

	// 2. Match by metadata.name or file basename (may match several).
	var matches []Example
	for _, it := range items {
		base := strings.TrimSuffix(path.Base(it.Path), path.Ext(it.Path))
		if it.Name == query || base == query || base == normalized {
			matches = append(matches, it)
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("%w: %q", ErrExampleNotFound, query)
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Path < matches[j].Path })
	return matches, nil
}

// ResolveExample resolves a query to a single example path. It is a thin wrapper
// over MatchExamples for callers requiring exactly one result: when the query
// matches more than one example, ErrAmbiguousExample is returned wrapped with
// the candidate paths.
func ResolveExample(query string) (string, error) {
	matches, err := MatchExamples(query)
	if err != nil {
		return "", err
	}
	if len(matches) > 1 {
		paths := make([]string, len(matches))
		for i, m := range matches {
			paths[i] = m.Path
		}
		return "", fmt.Errorf("%w: %q matches %s", ErrAmbiguousExample, query, strings.Join(paths, ", "))
	}
	return matches[0].Path, nil
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

// exampleFileExists reports whether relPath names a regular file in the
// embedded examples filesystem. relPath must already be traversal-checked and
// path.Clean'd (forward slashes). It is used by ResolveExample to allow
// exact-path fetching of any embedded example file, including non-solution
// files that the solution-only listing excludes.
func exampleFileExists(relPath string) bool {
	examplesFS, root, err := getExamplesFS()
	if err != nil {
		return false
	}
	info, err := fs.Stat(examplesFS, path.Join(root, relPath))
	return err == nil && !info.IsDir()
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

// Categories returns the sorted, de-duplicated list of example categories.
//
// It returns an error when the underlying example scan fails, so an
// empty-examples condition is diagnosable rather than silently collapsing to an
// empty list (which previously produced downstream defects such as an invalid
// "enum": null MCP schema -- see issue #819). A successful scan that finds no
// categories returns an empty (non-nil) slice and a nil error.
func Categories() ([]string, error) {
	items, err := Scan("")
	if err != nil {
		return nil, fmt.Errorf("failed to scan examples for categories: %w", err)
	}

	seen := make(map[string]bool)
	cats := make([]string, 0)
	for _, item := range items {
		if item.Category != "" && !seen[item.Category] {
			seen[item.Category] = true
			cats = append(cats, item.Category)
		}
	}
	sort.Strings(cats)
	return cats, nil
}

// getExamplesFS returns the in-place embedded example tree, rooted at ".".
//
// The examples are embedded directly from the examples/ directory (see the
// examplefiles package), so they are always present in the compiled binary and
// in the published Go module -- there is no build-time copy step and no
// working-directory-dependent filesystem fallback. An error is returned if the
// embedded tree is unreadable OR empty, which can only happen as a build defect
// (e.g. a future refactor that breaks the go:embed directive); surfacing it lets
// Categories()/Scan() report the condition instead of silently returning an
// empty list -- the failure mode behind issue #819.
func getExamplesFS() (fs.FS, string, error) {
	return examplesFSFrom(EmbeddedExamples)
}

// examplesFSFrom validates fsys as the example source and returns it rooted at
// ".". It is separated from getExamplesFS so the empty/unreadable build-defect
// paths can be exercised with an injected FS in tests.
func examplesFSFrom(fsys fs.FS) (fs.FS, string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, "", fmt.Errorf("examples not available: embedded example tree is unreadable: %w", err)
	}
	// A zero-value or mis-wired embed.FS reads as an empty directory with no
	// error, so a missing example source -- not a read error -- is the
	// detectable symptom of a broken embed. Emptiness is not enough on its own:
	// the embedding package sits at the root of the tree (examples/embed.go) and
	// //go:embed * captures that package's own Go source too, so a broken embed
	// that matched only the Go file(s) would still be non-empty. Require at least
	// one real example source -- a subdirectory (example category) or a top-level
	// YAML file -- so the guard cannot be satisfied by the inert Go source alone.
	if !hasExampleSource(entries) {
		return nil, "", errors.New("examples not available: embedded example tree contains no example files (build defect: the go:embed directive matched no example YAML files or category directories)")
	}
	return fsys, ".", nil
}

// hasExampleSource reports whether the root directory entries include at least
// one genuine example source: a subdirectory (an example category) or a
// top-level .yaml/.yml file. Non-example root files (e.g. the embedding
// package's own Go source captured by //go:embed *) do not count, so this
// distinguishes a healthy embed from a broken one that matched only inert files.
func hasExampleSource(entries []fs.DirEntry) bool {
	for _, e := range entries {
		if e.IsDir() {
			return true
		}
		if ext := strings.ToLower(filepath.Ext(e.Name())); ext == ".yaml" || ext == ".yml" {
			return true
		}
	}
	return false
}
