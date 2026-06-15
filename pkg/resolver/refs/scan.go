// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package refs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

// DefaultTemplateGlobs are the file extensions scanned when no glob is specified.
var DefaultTemplateGlobs = []string{"*.tpl", "*.tmpl", "*.gotmpl"}

// FileRefs holds extracted resolver references for a single file.
type FileRefs struct {
	Path       string   `json:"path"`
	References []string `json:"references"`
	Details    []Detail `json:"details"`
}

// Detail represents a resolver and its referenced fields.
type Detail struct {
	Resolver string   `json:"resolver"`
	Fields   []string `json:"fields"`
}

// ScanResult holds the aggregated result of a directory scan.
type ScanResult struct {
	References []string   `json:"references"`
	Count      int        `json:"count"`
	Details    []Detail   `json:"details"`
	Files      []FileRefs `json:"files"`
	FilesCount int        `json:"filesCount"`
	Warnings   []string   `json:"warnings,omitempty"`
}

// ScanDirectory recursively scans a directory for template files matching the
// given glob patterns and returns aggregated resolver references from all files.
// The exprType must be "go-template" or "cel".
func ScanDirectory(ctx context.Context, dir string, globs []string, exprType string) (*ScanResult, error) {
	if exprType != "go-template" && exprType != "cel" {
		return nil, fmt.Errorf("unsupported expression type %q: use 'go-template' or 'cel'", exprType)
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("directory %q not found: %w", dir, err)
		}
		return nil, fmt.Errorf("cannot access directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%q is not a directory", dir)
	}

	if len(globs) == 0 {
		globs = DefaultTemplateGlobs
	}

	// Validate glob patterns up-front
	for _, g := range globs {
		if _, err := filepath.Match(g, ""); err != nil {
			return nil, fmt.Errorf("invalid glob pattern %q: %w", g, err)
		}
	}

	// Open a root-scoped handle to prevent symlink TOCTOU traversal (gosec G122)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to open directory root: %w", err)
	}
	defer root.Close()

	var files []FileRefs
	var warnings []string
	allRefs := make(map[string][]string) // resolver → fields (aggregated)

	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			relPath, _ := filepath.Rel(dir, path)
			warnings = append(warnings, fmt.Sprintf("skipped %q: %v", relPath, walkErr))
			return nil //nolint:nilerr // surface as warning, continue scanning
		}
		if d.IsDir() {
			return nil
		}

		// Check if file matches any glob pattern (matched against base filename)
		name := d.Name()
		matched := false
		for _, g := range globs {
			if m, _ := filepath.Match(g, name); m {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}

		// Read file via root-scoped FS to avoid symlink TOCTOU
		relPath, _ := filepath.Rel(dir, path)
		fsPath := filepath.ToSlash(relPath) // io/fs requires forward slashes
		data, readErr := fs.ReadFile(root.FS(), fsPath)
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("could not read %q: %v", relPath, readErr))
			return nil //nolint:nilerr // surface as warning, continue scanning
		}

		var paths []string
		var extractErr error
		switch exprType {
		case "go-template":
			paths, extractErr = extractTemplatePaths(string(data))
		case "cel":
			paths, extractErr = ExtractFromCEL(ctx, string(data))
		}
		if extractErr != nil {
			warnings = append(warnings, fmt.Sprintf("parse error in %q: %v", relPath, extractErr))
		}

		if len(paths) == 0 {
			return nil
		}

		details := BuildDetails(paths)
		files = append(files, FileRefs{
			Path:       relPath,
			References: detailNames(details),
			Details:    details,
		})

		// Aggregate into global map
		for _, d := range details {
			existing := allRefs[d.Resolver]
			for _, f := range d.Fields {
				found := false
				for _, ef := range existing {
					if ef == f {
						found = true
						break
					}
				}
				if !found {
					existing = append(existing, f)
				}
			}
			allRefs[d.Resolver] = existing
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan directory: %w", err)
	}

	// Build aggregated details
	aggregated := make([]Detail, 0, len(allRefs))
	for name, fields := range allRefs {
		sort.Strings(fields)
		aggregated = append(aggregated, Detail{Resolver: name, Fields: fields})
	}
	sort.Slice(aggregated, func(i, j int) bool {
		return aggregated[i].Resolver < aggregated[j].Resolver
	})

	return &ScanResult{
		References: detailNames(aggregated),
		Count:      len(aggregated),
		Details:    aggregated,
		Files:      files,
		FilesCount: len(files),
		Warnings:   warnings,
	}, nil
}

// extractTemplatePaths returns full resolver dot-paths (e.g. "config.host")
// from a Go template, preserving field access information for BuildDetails.
func extractTemplatePaths(content string) ([]string, error) {
	refs, err := gotmpl.GetGoTemplateReferences(content, "", "")
	if err != nil {
		return nil, err
	}

	var paths []string
	for _, ref := range refs {
		if ref.Scoped {
			continue
		}

		path := strings.TrimPrefix(ref.Path, ".")
		if strings.HasPrefix(path, "_.") {
			paths = append(paths, strings.TrimPrefix(path, "_."))
		}
	}

	return paths, nil
}

// ParseGlobs splits a comma-separated glob string into trimmed patterns.
// Returns DefaultTemplateGlobs if the input is empty.
func ParseGlobs(glob string) []string {
	if glob == "" {
		return DefaultTemplateGlobs
	}
	parts := strings.Split(glob, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// BuildDetails groups raw resolver paths into resolver → fields mapping.
func BuildDetails(paths []string) []Detail {
	resolverFields := make(map[string][]string)
	for _, p := range paths {
		parts := strings.SplitN(p, ".", 2)
		resolverName := parts[0]
		if len(parts) > 1 {
			field := parts[1]
			found := false
			for _, f := range resolverFields[resolverName] {
				if f == field {
					found = true
					break
				}
			}
			if !found {
				resolverFields[resolverName] = append(resolverFields[resolverName], field)
			}
		} else {
			if _, exists := resolverFields[resolverName]; !exists {
				resolverFields[resolverName] = []string{}
			}
		}
	}

	details := make([]Detail, 0, len(resolverFields))
	for name, fields := range resolverFields {
		sort.Strings(fields)
		details = append(details, Detail{Resolver: name, Fields: fields})
	}
	sort.Slice(details, func(i, j int) bool {
		return details[i].Resolver < details[j].Resolver
	})
	return details
}

// detailNames extracts resolver names from a slice of Detail.
func detailNames(details []Detail) []string {
	names := make([]string, len(details))
	for i, d := range details {
		names[i] = d.Resolver
	}
	return names
}
