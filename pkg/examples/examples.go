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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

//go:embed files/*
var EmbeddedExamples embed.FS

// Example represents an example file in the listing.
type Example struct {
	Path        string `json:"path"`
	Category    string `json:"category"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Scan walks the examples filesystem and returns matching examples.
// If category is empty, returns all examples.
func Scan(category string) ([]Example, error) {
	examplesFS, root, err := getExamplesFS()
	if err != nil {
		return nil, err
	}

	var items []Example
	err = fs.WalkDir(examplesFS, root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		// Only include YAML files
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		// Get the relative path from the root
		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		// Determine category from the first directory component
		parts := strings.SplitN(relPath, "/", 2)
		cat := ""
		name := relPath
		if len(parts) > 1 {
			cat = parts[0]
			name = parts[1]
		}

		// Apply category filter
		if category != "" && cat != category {
			return nil
		}

		// Skip intentionally bad examples
		if strings.Contains(relPath, "bad-solution") {
			return nil
		}

		item := Example{
			Path:        relPath,
			Category:    cat,
			Name:        name,
			Description: DescriptionFromPath(relPath),
		}
		items = append(items, item)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort by category then name
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category != items[j].Category {
			return items[i].Category < items[j].Category
		}
		return items[i].Name < items[j].Name
	})

	return items, nil
}

// Read returns the contents of an example file.
func Read(path string) (string, error) {
	examplesFS, root, err := getExamplesFS()
	if err != nil {
		return "", err
	}

	// Security: ensure the path doesn't escape
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return "", fmt.Errorf("path must not contain '..'")
	}

	fullPath := filepath.Join(root, cleanPath)
	content, err := fs.ReadFile(examplesFS, fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to read example %q: %w", path, err)
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

// DescriptionFromPath generates a human-readable description from a file path.
func DescriptionFromPath(path string) string {
	descriptions := map[string]string{
		// Solutions
		"solutions/comprehensive/solution.yaml":      "Comprehensive solution demonstrating all features (resolvers, actions, transforms, validation, etc.)",
		"solutions/email-notifier/solution.yaml":     "Email notification solution with parameters, validation, and action workflow",
		"solutions/terraform/solution.yaml":          "Terraform infrastructure scaffolding solution",
		"solutions/k8s-clusters/solution.yaml":       "Kubernetes cluster provisioning solution",
		"solutions/directory/solution.yaml":          "Directory provider examples (list, read, create, copy)",
		"solutions/scaffold-demo/solution.yaml":      "Scaffold/template rendering demo solution",
		"solutions/github-auth/solution.yaml":        "GitHub authentication and API access solution",
		"solutions/composition/parent.yaml":          "Solution composition - parent that composes children",
		"solutions/composition/child.yaml":           "Solution composition - child partial solution",
		"solutions/taskfile/solution.yaml":           "Taskfile-based workflow solution",
		"solutions/tested-solution/solution.yaml":    "Solution with functional tests defined in spec.testing.cases",
		"solutions/template-functions/solution.yaml": "Demonstrates custom Go template functions: slugify, where, selectField, cel, toYaml, metadata provider",

		// Actions
		"actions/hello-world.yaml":              "Simple hello world action",
		"actions/sequential-chain.yaml":         "Actions executed sequentially using dependsOn",
		"actions/parallel-with-deps.yaml":       "Parallel actions with dependency ordering",
		"actions/conditional-execution.yaml":    "Actions with conditional execution (when clauses)",
		"actions/error-handling.yaml":           "Error handling with onError behavior",
		"actions/finally-cleanup.yaml":          "Finally block for cleanup actions",
		"actions/foreach-deploy.yaml":           "ForEach iteration over collections",
		"actions/retry-backoff.yaml":            "Retry with exponential backoff",
		"actions/conditional-retry.yaml":        "Retry with conditional retry logic (retryIf)",
		"actions/complex-workflow.yaml":         "Complex workflow with all action features",
		"actions/template-render.yaml":          "Template rendering action",
		"actions/go-template-inline.yaml":       "Inline Go template action",
		"actions/result-schema-validation.yaml": "Action result schema validation",

		// Resolvers
		"resolvers/hello-world.yaml":         "Simple static value resolver",
		"resolvers/parameters.yaml":          "Parameter provider for user input",
		"resolvers/dependencies.yaml":        "Resolver dependency chain (dependsOn)",
		"resolvers/env-config.yaml":          "Environment variable resolver",
		"resolvers/validation.yaml":          "Resolver validation rules",
		"resolvers/transform-pipeline.yaml":  "Multi-step transform pipeline",
		"resolvers/cel-basics.yaml":          "CEL expression basics in resolvers",
		"resolvers/cel-builtins.yaml":        "CEL built-in functions reference",
		"resolvers/cel-extensions.yaml":      "scafctl custom CEL extensions",
		"resolvers/cel-transforms.yaml":      "CEL-based transform examples",
		"resolvers/cel-common-patterns.yaml": "Common CEL patterns and recipes",
		"resolvers/feature-flags.yaml":       "Feature flag resolver pattern",
		"resolvers/identity.yaml":            "Identity/auth resolver pattern",
		"resolvers/secrets.yaml":             "Secrets resolver pattern",

		// Providers
		"providers/metadata-full.yaml":         "Metadata provider — returns runtime metadata about scafctl and the current solution",
		"providers/metadata-single-field.yaml": "Metadata provider — use CEL to extract a single field from runtime metadata",
	}

	if desc, ok := descriptions[path]; ok {
		return desc
	}

	// Fallback: generate from filename
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	return strings.Title(name) + " example" //nolint:staticcheck // strings.Title is fine for simple cases
}
