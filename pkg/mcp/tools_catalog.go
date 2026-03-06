// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// registerCatalogTools registers all catalog-related MCP tools.
func (s *Server) registerCatalogTools() {
	catalogListTool := mcp.NewTool("catalog_list",
		mcp.WithDescription("List entries in the local catalog, optionally filtered by kind and name. The catalog stores solutions, providers, and auth handlers that have been published locally."),
		mcp.WithTitleAnnotation("Catalog List"),
		mcp.WithToolIcons(toolIcons["catalog"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("kind",
			mcp.Description("Filter by artifact kind: solution, provider, auth-handler"),
			mcp.Enum("solution", "provider", "auth-handler"),
		),
		mcp.WithString("name",
			mcp.Description("Filter by name (exact match)"),
		),
	)
	s.mcpServer.AddTool(catalogListTool, s.handleCatalogList)

	catalogInspectTool := mcp.NewTool("catalog_inspect",
		mcp.WithDescription("Show detailed metadata about a specific catalog artifact. Returns kind, name, version, digest, size, creation time, and annotations. Use 'catalog_list' first to discover available artifacts, then inspect a specific one for details."),
		mcp.WithTitleAnnotation("Catalog Inspect"),
		mcp.WithToolIcons(toolIcons["catalog"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("reference",
			mcp.Required(),
			mcp.Description("Artifact reference in the format 'name' or 'name@version' (e.g., 'my-solution', 'my-solution@1.2.3')"),
		),
		mcp.WithString("kind",
			mcp.Required(),
			mcp.Description("Artifact kind: solution, provider, auth-handler"),
			mcp.Enum("solution", "provider", "auth-handler"),
		),
	)
	s.mcpServer.AddTool(catalogInspectTool, s.handleCatalogInspect)
}

// handleCatalogList lists entries in the local catalog.
func (s *Server) handleCatalogList(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kind := request.GetString("kind", "")
	name := request.GetString("name", "")

	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to initialize local catalog: %v", err),
			WithSuggestion("Ensure the catalog directory exists and is accessible"),
		), nil
	}

	// If kind is specified, list just that kind
	if kind != "" {
		artifactKind, ok := catalog.ParseArtifactKind(kind)
		if !ok {
			return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid kind %q. Valid kinds: solution, provider, auth-handler", kind),
				WithField("kind"),
				WithSuggestion("Use one of: solution, provider, auth-handler"),
			), nil
		}

		items, err := localCatalog.List(s.ctx, artifactKind, name)
		if err != nil {
			return newStructuredError(ErrCodeExecFailed, fmt.Sprintf("failed to list catalog entries: %v", err),
				WithSuggestion("Check the catalog configuration"),
			), nil
		}

		return mcp.NewToolResultJSON(map[string]any{
			"kind":    kind,
			"entries": items,
			"count":   len(items),
		})
	}

	// No kind specified — list all kinds
	allKinds := []catalog.ArtifactKind{
		catalog.ArtifactKindSolution,
		catalog.ArtifactKindProvider,
		catalog.ArtifactKindAuthHandler,
	}

	type kindResult struct {
		Kind    string                 `json:"kind"`
		Entries []catalog.ArtifactInfo `json:"entries"`
		Count   int                    `json:"count"`
	}

	var results []kindResult
	totalCount := 0
	for _, k := range allKinds {
		items, err := localCatalog.List(s.ctx, k, name)
		if err != nil {
			s.logger.V(1).Info("failed to list catalog entries", "kind", k, "error", err)
			continue
		}
		results = append(results, kindResult{
			Kind:    string(k),
			Entries: items,
			Count:   len(items),
		})
		totalCount += len(items)
	}

	return mcp.NewToolResultJSON(map[string]any{
		"kinds":      results,
		"totalCount": totalCount,
	})
}

// handleCatalogInspect returns detailed metadata about a specific catalog artifact.
func (s *Server) handleCatalogInspect(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	reference, err := request.RequireString("reference")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("reference"),
			WithSuggestion("Provide a catalog reference (e.g., 'my-solution@1.0.0' or 'my-solution')"),
			WithRelatedTools("list_catalog"),
		), nil
	}

	kindStr, err := request.RequireString("kind")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("kind"),
			WithSuggestion("Provide an artifact kind: 'solution', 'provider', or 'auth-handler'"),
		), nil
	}

	artifactKind, ok := catalog.ParseArtifactKind(kindStr)
	if !ok {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid kind %q", kindStr),
			WithField("kind"),
			WithSuggestion("Valid kinds: solution, provider, auth-handler"),
			WithRelatedTools("list_catalog"),
		), nil
	}

	ref, err := catalog.ParseReference(artifactKind, reference)
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, fmt.Sprintf("invalid reference %q: %v", reference, err),
			WithField("reference"),
			WithSuggestion("Use format 'name@version' or just 'name' for latest"),
			WithRelatedTools("list_catalog"),
		), nil
	}

	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		return newStructuredError(ErrCodeConfigError, fmt.Sprintf("failed to initialize local catalog: %v", err),
			WithSuggestion("Check catalog configuration with get_config"),
			WithRelatedTools("get_config"),
		), nil
	}

	info, err := localCatalog.Resolve(s.ctx, ref)
	if err != nil {
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("artifact not found: %v", err),
			WithField("reference"),
			WithSuggestion("Use list_catalog to see available artifacts"),
			WithRelatedTools("list_catalog"),
		), nil
	}

	result := map[string]any{
		"kind":      string(info.Reference.Kind),
		"name":      info.Reference.Name,
		"digest":    info.Digest,
		"size":      info.Size,
		"createdAt": info.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		"catalog":   info.Catalog,
	}
	if info.Reference.Version != nil {
		result["version"] = info.Reference.Version.String()
	}
	if len(info.Annotations) > 0 {
		result["annotations"] = info.Annotations
	}

	// Surface multi-platform info for plugin artifacts
	if artifactKind == catalog.ArtifactKindProvider || artifactKind == catalog.ArtifactKindAuthHandler {
		platforms, err := localCatalog.ListPlatforms(s.ctx, ref)
		if err == nil {
			result["isMultiPlatform"] = len(platforms) > 0
			if len(platforms) > 0 {
				result["platforms"] = platforms
				result["platformCount"] = len(platforms)
			}
		}
	}

	return mcp.NewToolResultJSON(result)
}
