// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// namedFunction is the constraint for function types that can be filtered
// by name and searched by description.
type namedFunction interface {
	GetName() string
	GetDescription() string
}

// subNamed is an optional interface for functions that contain
// individually named sub-functions (e.g., CEL groups like "encoders"
// containing "base64.encode", "base64.decode").
type subNamed interface {
	GetSubNames() []string
}

// filterAndReturnNamedFunctions filters a slice of named items by substring match on Name,
// then returns the result as JSON. This deduplicates the shared filter-by-name logic
// between handleListCELFunctions and handleListGoTemplateFunctions.
//
// Parameters:
//   - functions: the slice to filter (elements must implement namedFunction)
//   - name: name substring to filter by (empty means no filter)
//   - funcKind: human-readable kind for error messages (e.g., "CEL function", "Go template function")
//   - toolName: MCP tool name for error suggestions
func filterAndReturnNamedFunctions[T namedFunction](
	functions []T,
	name string,
	funcKind string,
	toolName string,
) (*mcp.CallToolResult, error) {
	if name != "" {
		filtered := functions[:0:0]
		nameLower := strings.ToLower(name)
		for _, f := range functions {
			if strings.Contains(strings.ToLower(f.GetName()), nameLower) {
				filtered = append(filtered, f)
				continue
			}
			// Check individual function names within the group.
			if sn, ok := any(f).(subNamed); ok {
				for _, sub := range sn.GetSubNames() {
					if strings.Contains(strings.ToLower(sub), nameLower) {
						filtered = append(filtered, f)
						break
					}
				}
			}
		}
		if len(filtered) == 0 {
			return newStructuredError(ErrCodeNotFound, fmt.Sprintf("no %s matching %q found", funcKind, name),
				WithField("name"),
				WithSuggestion(fmt.Sprintf("Use %s without a name filter to see all available functions", toolName)),
				WithRelatedTools(toolName),
			), nil
		}
		functions = filtered
	}

	return mcp.NewToolResultJSON(functions)
}

// searchFunctions filters a slice by substring match on both Name and Description.
// Returns the filtered slice. If no match is found, returns an MCP error result.
func searchFunctions[T namedFunction](
	functions []T,
	query string,
	funcKind string,
	toolName string,
) ([]T, *mcp.CallToolResult) {
	if query == "" {
		return functions, nil
	}
	queryLower := strings.ToLower(query)
	filtered := functions[:0:0]
	for _, f := range functions {
		if strings.Contains(strings.ToLower(f.GetName()), queryLower) ||
			strings.Contains(strings.ToLower(f.GetDescription()), queryLower) {
			filtered = append(filtered, f)
			continue
		}
		// Check individual function names within the group.
		if sn, ok := any(f).(subNamed); ok {
			for _, sub := range sn.GetSubNames() {
				if strings.Contains(strings.ToLower(sub), queryLower) {
					filtered = append(filtered, f)
					break
				}
			}
		}
	}
	if len(filtered) == 0 {
		return nil, newStructuredError(ErrCodeNotFound, fmt.Sprintf("no %s matching search %q found", funcKind, query),
			WithField("search"),
			WithSuggestion(fmt.Sprintf("Try a broader search term, or use %s without filters to see all functions", toolName)),
			WithRelatedTools(toolName),
		)
	}
	return filtered, nil
}

// buildFunctionIndex builds a compact summary index grouped by category.
// The index lists category names and function names, providing a scannable
// overview without the full ~40KB function detail payload.
//
// subNamesFn optionally returns additional sub-function names for each item
// (e.g., "encoders" -> ["base64.encode", "base64.decode"]). Pass nil to skip.
func buildFunctionIndex[T interface {
	GetName() string
}](functions []T, categoryFn func(T) string, subNamesFn func(T) []string) string {
	// Group functions by category, expanding sub-names when available.
	groups := make(map[string][]string)
	for _, f := range functions {
		cat := categoryFn(f)
		if cat == "" {
			cat = "other"
		}
		if subNamesFn != nil {
			if subs := subNamesFn(f); len(subs) > 0 {
				groups[cat] = append(groups[cat], subs...)
				continue
			}
		}
		groups[cat] = append(groups[cat], f.GetName())
	}

	// Sort categories
	cats := make([]string, 0, len(groups))
	for cat := range groups {
		cats = append(cats, cat)
	}
	sort.Strings(cats)

	var b strings.Builder
	fmt.Fprintf(&b, "# Summary (%d functions)\n\n", len(functions))
	for _, cat := range cats {
		names := groups[cat]
		sort.Strings(names)
		fmt.Fprintf(&b, "## %s (%d)\n", cat, len(names))
		b.WriteString("  " + strings.Join(names, ", ") + "\n\n")
	}
	return b.String()
}
