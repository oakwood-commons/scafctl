// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/refactor"
	"github.com/oakwood-commons/scafctl/pkg/refindex"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// registerRefactorTools registers positioned reference-finding and rename tools.
func (s *Server) registerRefactorTools() {
	s.addTool(newFindReferencesTool(
		"find_resolver_references", "Find Resolver References", "resolver",
		"Find every reference to a resolver in a solution file -- its definition and all uses (dependsOn entries, rslvr values, CEL '_.name' expressions, and explicit template '._.name') with source locations. Use this to understand the impact of changing a resolver before editing, or to navigate a solution. Distinct from extract_resolver_refs, which analyses a single expression rather than a whole solution.",
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.handleFindReferences(ctx, request, refindex.SymbolResolver, "resolver")
	})

	s.addTool(newRenameTool(
		"rename_resolver", "Rename Resolver", "resolver",
		"Rename a resolver and every reference to it in a solution file, returning the rewritten content with comments and formatting preserved (only the affected identifiers change). Refuses without changes if the new name is invalid, collides with an existing resolver, or any reference cannot be located byte-exact -- so it never produces a partial, broken rename. Returns the new content for you to write back; operates on a single solution file (not composed/bundled solutions).",
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.handleRename(ctx, request, refindex.SymbolResolver, "resolver")
	})

	s.addTool(newFindReferencesTool(
		"find_action_references", "Find Action References", "action",
		"Find every reference to a workflow action in a solution file -- its definition and all uses (dependsOn entries, CEL '__actions.name' expressions, and explicit template '.__actions.name') with source locations. Use this to understand the impact of changing an action before editing, or to navigate a solution's workflow. Note: an action's 'alias' is a separate name and is not reported as a reference to the action.",
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.handleFindReferences(ctx, request, refindex.SymbolAction, "action")
	})

	s.addTool(newRenameTool(
		"rename_action", "Rename Action", "action",
		"Rename a workflow action and every reference to it in a solution file, returning the rewritten content with comments and formatting preserved (only the affected identifiers change). Rewrites dependsOn entries, CEL '__actions.name' uses, explicit template '.__actions.name' uses, and the definition. Refuses without changes if the new name is invalid, collides with an existing action, or any reference cannot be located byte-exact -- so it never produces a partial, broken rename. An action's 'alias' is independent and is not changed. Returns the new content for you to write back; operates on a single solution file (not composed/bundled solutions).",
	), func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.handleRename(ctx, request, refindex.SymbolAction, "action")
	})
}

// newFindReferencesTool builds a find-references tool for a given symbol kind.
// symbolParam is both the tool's required parameter name and appears in the
// description ("resolver"/"action").
func newFindReferencesTool(name, title, symbolParam, description string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithTitleAnnotation(title),
		mcp.WithToolIcons(toolIcons["refs"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("file",
			mcp.Description("Path to the solution YAML file"),
			mcp.Required(),
		),
		mcp.WithString(symbolParam,
			mcp.Description(fmt.Sprintf("Name of the %s to find references for", symbolParam)),
			mcp.Required(),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescFilePaths),
		),
	)
}

// newRenameTool builds a rename tool for a given symbol kind. symbolLabel
// ("resolver"/"action") appears only in parameter descriptions.
func newRenameTool(name, title, symbolLabel, description string) mcp.Tool {
	return mcp.NewTool(name,
		mcp.WithDescription(description),
		mcp.WithTitleAnnotation(title),
		mcp.WithToolIcons(toolIcons["refs"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("file",
			mcp.Description("Path to the solution YAML file"),
			mcp.Required(),
		),
		mcp.WithString("old_name",
			mcp.Description(fmt.Sprintf("Current %s name", symbolLabel)),
			mcp.Required(),
		),
		mcp.WithString("new_name",
			mcp.Description(fmt.Sprintf("New %s name", symbolLabel)),
			mcp.Required(),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescFilePaths),
		),
	)
}

// handleFindReferences returns the definition and all references of a symbol of
// the given kind within a solution file. symbolParam names the request field and
// the primary key in the JSON result ("resolver"/"action").
func (s *Server) handleFindReferences(_ context.Context, request mcp.CallToolRequest, kind refindex.SymbolKind, symbolParam string) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(), WithField("file"),
			WithSuggestion("Provide the path to a solution YAML file")), nil
	}
	name, err := request.RequireString(symbolParam)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(), WithField(symbolParam),
			WithSuggestion(fmt.Sprintf("Provide the %s name to find references for", symbolParam))), nil
	}
	cwd := request.GetString("cwd", "")

	sol, loadErr := s.loadSolutionRaw(file, cwd)
	if loadErr != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", loadErr),
			WithField("file"), WithSuggestion("Check the file exists and contains valid solution YAML")), nil
	}

	idx, err := refindex.Build(sol)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("indexing solution: %v", err), WithField("file")), nil
	}

	def, defined := idx.Definition(kind, name)
	result := map[string]any{
		symbolParam:  name,
		"defined":    defined,
		"references": referencesJSON(idx.References(kind, name)),
		"unresolved": idx.UnresolvedFor(kind, name),
	}
	if defined {
		result["definition"] = locationJSON(def)
	}
	return mcp.NewToolResultJSON(result)
}

// handleRename computes the rewritten solution content for a rename of the given
// symbol kind, or a structured error explaining why the rename was refused.
func (s *Server) handleRename(_ context.Context, request mcp.CallToolRequest, kind refindex.SymbolKind, symbolLabel string) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(), WithField("file"),
			WithSuggestion("Provide the path to a solution YAML file")), nil
	}
	oldName, err := request.RequireString("old_name")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(), WithField("old_name")), nil
	}
	newName, err := request.RequireString("new_name")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(), WithField("new_name")), nil
	}
	cwd := request.GetString("cwd", "")

	sol, loadErr := s.loadSolutionRaw(file, cwd)
	if loadErr != nil {
		return newStructuredError(ErrCodeLoadFailed, fmt.Sprintf("loading solution: %v", loadErr),
			WithField("file"), WithSuggestion("Check the file exists and contains valid solution YAML")), nil
	}

	renameResult, err := refactor.RenameSymbol(sol, kind, oldName, newName)
	if err != nil {
		relatedTool := "find_resolver_references"
		if kind == refindex.SymbolAction {
			relatedTool = "find_action_references"
		}
		return newStructuredError(ErrCodeValidationError, err.Error(),
			WithSuggestion(fmt.Sprintf("Fix the new name, resolve the collision, or make ambiguous %s references explicit, then retry", symbolLabel)),
			WithRelatedTools(relatedTool)), nil
	}

	newContent, err := renameResult.Apply(sol.RawContent())
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("applying rename: %v", err), WithField("file")), nil
	}

	occurrences := make([]map[string]any, 0, len(renameResult.Edits))
	for _, e := range renameResult.Edits {
		occurrences = append(occurrences, map[string]any{
			"line":   e.Range.Start.Line,
			"column": e.Range.Start.Column,
		})
	}

	return mcp.NewToolResultJSON(map[string]any{
		"oldName":     oldName,
		"newName":     newName,
		"occurrences": occurrences,
		"content":     string(newContent),
	})
}

// loadSolutionRaw reads the raw solution file (resolved relative to cwd when the
// path is relative) and unmarshals it, preserving RawContent for byte-exact
// positions. It intentionally reads the file directly -- like the CLI rename
// command -- so edits align with the on-disk bytes.
func (s *Server) loadSolutionRaw(file, cwd string) (*solution.Solution, error) {
	path := file
	if cwd != "" && !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	raw, err := os.ReadFile(path) //nolint:gosec // path is a user-provided solution reference
	if err != nil {
		return nil, err
	}
	sol := &solution.Solution{}
	// Set the path before unmarshalling so the SourceMap/Range positions built
	// during FromYAML carry a non-empty file, keeping positioned outputs and
	// refindex metadata accurate.
	sol.SetPath(path)
	if err := sol.UnmarshalFromBytes(raw); err != nil {
		return nil, err
	}
	return sol, nil
}

// referencesJSON converts positioned references to a JSON-friendly slice.
func referencesJSON(refs []refindex.Reference) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for _, r := range refs {
		out = append(out, locationJSON(r))
	}
	return out
}

// locationJSON renders a reference as a location with its origin.
func locationJSON(r refindex.Reference) map[string]any {
	return map[string]any{
		"line":   r.Range.Start.Line,
		"column": r.Range.Start.Column,
		"origin": r.Origin.String(),
	}
}
