// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	pkglint "github.com/oakwood-commons/scafctl/pkg/lint"
)

// registerLintTools registers lint-related MCP tools.
func (s *Server) registerLintTools() {
	// list_lint_rules
	listLintRulesTool := mcp.NewTool("list_lint_rules",
		mcp.WithDescription("List all known lint rules with their severity, category, and a short description. Use this to discover available rules before calling explain_lint_rule for details."),
		mcp.WithTitleAnnotation("List Lint Rules"),
		mcp.WithToolIcons(toolIcons["lint"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("severity",
			mcp.Description("Filter by severity level: 'error', 'warning', or 'info'. Omit to list all."),
		),
		mcp.WithString("category",
			mcp.Description("Filter by category (e.g., 'structure', 'naming', 'provider', 'expression', 'schema'). Omit to list all."),
		),
	)
	s.mcpServer.AddTool(listLintRulesTool, s.handleListLintRules)

	// explain_lint_rule
	explainLintRuleTool := mcp.NewTool("explain_lint_rule",
		mcp.WithDescription("Get a detailed explanation and fix suggestions for a specific lint rule. When lint_solution returns findings with a ruleName, use this tool to understand what the rule checks for, why it matters, and how to fix it. Call list_lint_rules first to discover available rule names."),
		mcp.WithTitleAnnotation("Explain Lint Rule"),
		mcp.WithToolIcons(toolIcons["lint"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("rule",
			mcp.Required(),
			mcp.Description("The lint rule name to explain (e.g., 'unused-resolver', 'invalid-expression', 'missing-provider')"),
		),
	)
	s.mcpServer.AddTool(explainLintRuleTool, s.handleExplainLintRule)
}

// lintRuleSummary is a compact view of a lint rule for listing.
type lintRuleSummary struct {
	Rule        string `json:"rule"`
	Severity    string `json:"severity"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

// handleListLintRules returns all known lint rules with optional filtering.
// Rules are sourced from the canonical pkglint.KnownRules registry.
func (s *Server) handleListLintRules(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	severityFilter := request.GetString("severity", "")
	categoryFilter := request.GetString("category", "")

	allRules := pkglint.ListRules() // already sorted by severity, then name
	rules := make([]lintRuleSummary, 0, len(allRules))
	for _, r := range allRules {
		if severityFilter != "" && r.Severity != severityFilter {
			continue
		}
		if categoryFilter != "" && r.Category != categoryFilter {
			continue
		}
		rules = append(rules, lintRuleSummary{
			Rule:        r.Rule,
			Severity:    r.Severity,
			Category:    r.Category,
			Description: r.Description,
		})
	}

	return mcp.NewToolResultJSON(map[string]any{
		"count": len(rules),
		"rules": rules,
	})
}

// handleExplainLintRule returns a detailed explanation for a specific lint rule.
// Data is sourced from the canonical pkglint.KnownRules registry.
func (s *Server) handleExplainLintRule(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	rule, err := request.RequireString("rule")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("rule"),
			WithSuggestion("Use list_lint_rules to see all available rule names"),
			WithRelatedTools("list_lint_rules"),
		), nil
	}

	meta, ok := pkglint.GetRule(rule)
	if !ok {
		// List all known rules
		allRules := pkglint.ListRules()
		known := make([]string, 0, len(allRules))
		for _, r := range allRules {
			known = append(known, r.Rule)
		}
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("unknown lint rule %q. Known rules: %s", rule, strings.Join(known, ", ")),
			WithField("rule"),
			WithSuggestion("Use list_lint_rules to see all available rule names"),
			WithRelatedTools("list_lint_rules"),
		), nil
	}

	return mcp.NewToolResultJSON(meta)
}
