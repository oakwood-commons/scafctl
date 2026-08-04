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
	findRefsTool := mcp.NewTool("find_resolver_references",
		mcp.WithDescription("Find every reference to a resolver in a solution file -- its definition and all uses (dependsOn entries, rslvr values, CEL '_.name' expressions, and explicit template '._.name') with source locations. Use this to understand the impact of changing a resolver before editing, or to navigate a solution. Distinct from extract_resolver_refs, which analyses a single expression rather than a whole solution."),
		mcp.WithTitleAnnotation("Find Resolver References"),
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
		mcp.WithString("resolver",
			mcp.Description("Name of the resolver to find references for"),
			mcp.Required(),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescFilePaths),
		),
	)
	s.addTool(findRefsTool, s.handleFindResolverReferences)

	renameTool := mcp.NewTool("rename_resolver",
		mcp.WithDescription("Rename a resolver and every reference to it in a solution file, returning the rewritten content with comments and formatting preserved (only the affected identifiers change). Refuses without changes if the new name is invalid, collides with an existing resolver, or any reference cannot be located byte-exact -- so it never produces a partial, broken rename. Returns the new content for you to write back; operates on a single solution file (not composed/bundled solutions)."),
		mcp.WithTitleAnnotation("Rename Resolver"),
		mcp.WithToolIcons(toolIcons["refs"]),
		mcp.WithDeferLoading(true),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("file",
			mcp.Description("Path to the solution YAML file"),
			mcp.Required(),
		),
		mcp.WithString("old_name",
			mcp.Description("Current resolver name"),
			mcp.Required(),
		),
		mcp.WithString("new_name",
			mcp.Description("New resolver name"),
			mcp.Required(),
		),
		mcp.WithString("cwd",
			mcp.Description(cwdDescFilePaths),
		),
	)
	s.addTool(renameTool, s.handleRenameResolver)
}

// handleFindResolverReferences returns the definition and all references of a
// resolver within a solution file.
func (s *Server) handleFindResolverReferences(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	file, err := request.RequireString("file")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(), WithField("file"),
			WithSuggestion("Provide the path to a solution YAML file")), nil
	}
	name, err := request.RequireString("resolver")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(), WithField("resolver"),
			WithSuggestion("Provide the resolver name to find references for")), nil
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

	def, defined := idx.Definition(name)
	result := map[string]any{
		"resolver":   name,
		"defined":    defined,
		"references": referencesJSON(idx.References(name)),
		"unresolved": idx.UnresolvedFor(name),
	}
	if defined {
		result["definition"] = locationJSON(def)
	}
	return mcp.NewToolResultJSON(result)
}

// handleRenameResolver computes the rewritten solution content for a resolver
// rename, or a structured error explaining why the rename was refused.
func (s *Server) handleRenameResolver(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

	renameResult, err := refactor.RenameResolver(sol, oldName, newName)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithSuggestion("Fix the new name, resolve the collision, or make ambiguous references explicit (_.name in CEL, ._.name in templates), then retry"),
			WithRelatedTools("find_resolver_references")), nil
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
