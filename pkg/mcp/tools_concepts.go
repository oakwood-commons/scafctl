// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/concepts"
)

// registerConceptTools registers concept explanation MCP tools.
func (s *Server) registerConceptTools() {
	tool := mcp.NewTool("explain_concepts",
		mcp.WithDescription("Look up and explain scafctl concepts such as resolvers, providers, actions, testing, CEL expressions, composition, and more. Use without arguments to list all concepts, or provide a name or search query to get detailed explanations with examples."),
		mcp.WithTitleAnnotation("Explain Concepts"),
		mcp.WithToolIcons(toolIcons["help"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name",
			mcp.Description("Exact concept name to look up (e.g., 'resolver', 'cel-expression', 'test-sandbox')"),
		),
		mcp.WithString("query",
			mcp.Description("Free-text search across concept names, titles, and summaries"),
		),
		mcp.WithString("category",
			mcp.Description("Filter concepts by category (e.g., 'resolvers', 'testing', 'providers', 'actions')"),
		),
	)
	s.mcpServer.AddTool(tool, s.handleExplainConcepts)
}

// handleExplainConcepts handles the explain_concepts MCP tool.
func (s *Server) handleExplainConcepts(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := request.GetString("name", "")
	query := request.GetString("query", "")
	category := request.GetString("category", "")

	// Exact name lookup — return detailed single concept.
	if name != "" {
		c, ok := concepts.Get(name)
		if !ok {
			available := concepts.List()
			names := make([]string, len(available))
			for i, a := range available {
				names[i] = a.Name
			}
			return newStructuredError(ErrCodeNotFound,
				fmt.Sprintf("concept %q not found", name),
				WithSuggestion("Use explain_concepts without arguments to list all concepts"),
				WithField("name"),
			), nil
		}
		return mcp.NewToolResultJSON(map[string]any{
			"concept": c,
		})
	}

	// Category filter.
	if category != "" {
		results := concepts.ByCategory(category)
		if len(results) == 0 {
			return mcp.NewToolResultJSON(map[string]any{
				"concepts":   []any{},
				"categories": concepts.Categories(),
				"message":    fmt.Sprintf("No concepts in category %q. Available categories listed above.", category),
			})
		}
		summaries := make([]map[string]string, len(results))
		for i, c := range results {
			summaries[i] = map[string]string{
				"name":    c.Name,
				"title":   c.Title,
				"summary": c.Summary,
			}
		}
		return mcp.NewToolResultJSON(map[string]any{
			"category": category,
			"concepts": summaries,
			"hint":     "Use explain_concepts with name='<concept>' for full details",
		})
	}

	// Free-text search.
	if query != "" {
		results := concepts.Search(query)
		if len(results) == 0 {
			return mcp.NewToolResultJSON(map[string]any{
				"concepts":   []any{},
				"categories": concepts.Categories(),
				"message":    fmt.Sprintf("No concepts matching %q. Try a broader query or list all with no arguments.", query),
			})
		}
		summaries := make([]map[string]string, len(results))
		for i, c := range results {
			summaries[i] = map[string]string{
				"name":     c.Name,
				"title":    c.Title,
				"category": c.Category,
				"summary":  c.Summary,
			}
		}
		return mcp.NewToolResultJSON(map[string]any{
			"query":    query,
			"concepts": summaries,
			"hint":     "Use explain_concepts with name='<concept>' for full details",
		})
	}

	// No arguments — list all concepts grouped by category.
	categories := concepts.Categories()
	grouped := make(map[string][]map[string]string)
	for _, cat := range categories {
		items := concepts.ByCategory(cat)
		summaries := make([]map[string]string, len(items))
		for i, c := range items {
			summaries[i] = map[string]string{
				"name":    c.Name,
				"summary": c.Summary,
			}
		}
		grouped[cat] = summaries
	}

	return mcp.NewToolResultJSON(map[string]any{
		"categories": grouped,
		"totalCount": len(concepts.List()),
		"hint":       "Use explain_concepts with name='<concept>' for full explanation and examples",
	})
}
