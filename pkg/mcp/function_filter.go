// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// filterAndReturnNamedFunctions filters a slice of named items by substring match on Name,
// then returns the result as JSON. This deduplicates the shared filter-by-name logic
// between handleListCELFunctions and handleListGoTemplateFunctions.
//
// Parameters:
//   - functions: the slice to filter (elements must implement GetName())
//   - name: name substring to filter by (empty means no filter)
//   - funcKind: human-readable kind for error messages (e.g., "CEL function", "Go template function")
//   - toolName: MCP tool name for error suggestions
func filterAndReturnNamedFunctions[T interface{ GetName() string }](
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
