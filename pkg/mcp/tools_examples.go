// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// registerExampleTools registers example-related MCP tools.
func (s *Server) registerExampleTools() {
	// list_examples — list available example files
	listExamplesTool := mcp.NewTool("list_examples",
		mcp.WithDescription("List available scafctl example files. Examples demonstrate best practices for solutions, resolvers, actions, providers, and more. Filter by category (solutions, resolvers, actions, providers, exec, config, mcp, snapshots, catalog) or get all."),
		mcp.WithTitleAnnotation("List Examples"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("category",
			mcp.Description("Filter by category: solutions, resolvers, actions, providers, exec, config, mcp, snapshots, catalog. Omit to list all."),
			mcp.Enum("solutions", "resolvers", "actions", "providers", "exec", "config", "mcp", "snapshots", "catalog"),
		),
	)
	s.mcpServer.AddTool(listExamplesTool, s.handleListExamples)

	// get_example — read an example file's contents
	getExampleTool := mcp.NewTool("get_example",
		mcp.WithDescription("Read the contents of a scafctl example file. Use list_examples first to find available examples, then use the path returned there."),
		mcp.WithTitleAnnotation("Get Example"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("path",
			mcp.Required(),
			mcp.Description("Path of the example file (as returned by list_examples, e.g., 'solutions/email-notifier/solution.yaml')"),
		),
	)
	s.mcpServer.AddTool(getExampleTool, s.handleGetExample)
}

// exampleItem represents an example file in the listing.
type exampleItem struct {
	Path        string `json:"path"`
	Category    string `json:"category"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// handleListExamples lists available example files.
func (s *Server) handleListExamples(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	category := request.GetString("category", "")

	examplesDir, err := findExamplesDir()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("examples directory not found: %v", err)), nil
	}

	items, err := scanExamples(examplesDir, category)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to scan examples: %v", err)), nil
	}

	if len(items) == 0 {
		msg := "No examples found"
		if category != "" {
			msg += fmt.Sprintf(" in category %q", category)
		}
		return mcp.NewToolResultJSON(map[string]any{
			"examples": []any{},
			"message":  msg,
		})
	}

	return mcp.NewToolResultJSON(map[string]any{
		"examples": items,
		"count":    len(items),
	})
}

// handleGetExample reads and returns the contents of an example file.
func (s *Server) handleGetExample(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, err := request.RequireString("path")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	examplesDir, err := findExamplesDir()
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("examples directory not found: %v", err)), nil
	}

	// Security: ensure the path doesn't escape the examples directory
	cleanPath := filepath.Clean(path)
	if strings.Contains(cleanPath, "..") {
		return mcp.NewToolResultError("path must not contain '..'"), nil
	}

	fullPath := filepath.Join(examplesDir, cleanPath)

	// Verify the resolved path is under the examples directory
	resolvedPath, err := filepath.Abs(fullPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid path: %v", err)), nil
	}
	resolvedDir, err := filepath.Abs(examplesDir)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("invalid examples dir: %v", err)), nil
	}
	if !strings.HasPrefix(resolvedPath, resolvedDir) {
		return mcp.NewToolResultError("path must be within the examples directory"), nil
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read example %q: %v", path, err)), nil
	}

	return mcp.NewToolResultText(string(content)), nil
}

// findExamplesDir locates the examples directory relative to the package source.
// It walks up from the compiled binary's source location to find the project root.
func findExamplesDir() (string, error) {
	// Strategy 1: Find relative to this source file (works in development/testing)
	_, thisFile, _, ok := runtime.Caller(0)
	if ok {
		// This file is at pkg/mcp/tools_examples.go
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

// scanExamples walks the examples directory and returns structured items.
func scanExamples(dir, categoryFilter string) ([]exampleItem, error) {
	var items []exampleItem

	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}

		// Only include YAML files
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		// Get the relative path from the examples dir
		relPath, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}

		// Determine category from the first directory component
		parts := strings.SplitN(relPath, string(filepath.Separator), 2)
		category := ""
		name := relPath
		if len(parts) > 1 {
			category = parts[0]
			name = parts[1]
		}

		// Apply category filter
		if categoryFilter != "" && category != categoryFilter {
			return nil
		}

		// Skip intentionally bad examples
		if strings.Contains(relPath, "bad-solution") {
			return nil
		}

		item := exampleItem{
			Path:     relPath,
			Category: category,
			Name:     name,
		}

		// Generate a description from the filename
		item.Description = descriptionFromPath(relPath)

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

// descriptionFromPath generates a human-readable description from a file path.
func descriptionFromPath(path string) string {
	descriptions := map[string]string{
		// Solutions
		"solutions/comprehensive/solution.yaml":   "Comprehensive solution demonstrating all features (resolvers, actions, transforms, validation, etc.)",
		"solutions/email-notifier/solution.yaml":  "Email notification solution with parameters, validation, and action workflow",
		"solutions/terraform/solution.yaml":       "Terraform infrastructure scaffolding solution",
		"solutions/k8s-clusters/solution.yaml":    "Kubernetes cluster provisioning solution",
		"solutions/directory/solution.yaml":       "Directory provider examples (list, read, create, copy)",
		"solutions/scaffold-demo/solution.yaml":   "Scaffold/template rendering demo solution",
		"solutions/github-auth/solution.yaml":     "GitHub authentication and API access solution",
		"solutions/composition/parent.yaml":       "Solution composition - parent that composes children",
		"solutions/composition/child.yaml":        "Solution composition - child partial solution",
		"solutions/taskfile/solution.yaml":        "Taskfile-based workflow solution",
		"solutions/tested-solution/solution.yaml": "Solution with functional tests defined in spec.testing.cases",

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
