// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/concepts"
	"github.com/oakwood-commons/scafctl/pkg/contextvars"
)

// registerConceptTools registers concept explanation MCP tools.
func (s *Server) registerConceptTools() {
	tool := mcp.NewTool("explain_concepts",
		mcp.WithDescription("Look up and explain scafctl concepts such as resolvers, providers, actions, testing, CEL expressions, composition, context variables, and more. Use without arguments to list all concepts, or provide a name or search query to get detailed explanations with examples. The 'context' category covers the runtime evaluation environment (context variables, phase execution, CEL cost model, template dependency inference, snapshot masking, and the authoring workflow)."),
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
			mcp.Description("Filter concepts by category (e.g., 'resolvers', 'testing', 'providers', 'actions', 'context')"),
		),
	)
	s.addTool(tool, s.handleExplainConcepts)

	ctxVarsTool := mcp.NewTool("list_context_variables",
		mcp.WithDescription("List the special context variables injected into scafctl CEL and Go-template evaluation (e.g. _, __self, __item, __plan, __execution, __actions, __cwd, __params, __error, and the Go-template __file* path parts), with the phase each is available in. No function list covers these injected variables. Optionally filter by phase to see only what is available in a given evaluation context. For narrative guidance use explain_concepts name='context-variables'."),
		mcp.WithTitleAnnotation("List Context Variables"),
		mcp.WithToolIcons(toolIcons["help"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("phase",
			mcp.Description("Filter to variables available in a specific evaluation phase."),
			mcp.Enum(
				contextvars.PhaseResolve,
				contextvars.PhaseTransform,
				contextvars.PhaseValidate,
				contextvars.PhaseForEach,
				contextvars.PhaseAction,
				contextvars.PhaseStateBackend,
				contextvars.PhaseError,
				contextvars.PhaseTemplateFile,
			),
		),
	)
	s.addTool(ctxVarsTool, s.handleListContextVariables)
}

// handleListContextVariables handles the list_context_variables MCP tool.
func (s *Server) handleListContextVariables(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	phase := request.GetString("phase", "")

	vars := contextvars.List()
	if phase != "" {
		vars = contextvars.ByPhase(phase)
		if len(vars) == 0 {
			return newStructuredError(ErrCodeInvalidInput,
				fmt.Sprintf("unknown phase %q; no context variables are available in it", phase),
				WithSuggestion("Call list_context_variables without a phase to see all variables and their phases"),
				WithField("phase"),
			), nil
		}
	}

	result := map[string]any{
		"variables": vars,
		"phases":    contextvars.Phases(),
		"hint":      "Use explain_concepts name='context-variables' for narrative guidance and examples.",
	}
	if phase != "" {
		result["phase"] = phase
	}
	return mcp.NewToolResultJSON(result)
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
