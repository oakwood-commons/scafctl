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
}

// handleCatalogList lists entries in the local catalog.
func (s *Server) handleCatalogList(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	kind := request.GetString("kind", "")
	name := request.GetString("name", "")

	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to initialize local catalog: %v", err)), nil
	}

	// If kind is specified, list just that kind
	if kind != "" {
		artifactKind, ok := catalog.ParseArtifactKind(kind)
		if !ok {
			return mcp.NewToolResultError(fmt.Sprintf("invalid kind %q. Valid kinds: solution, provider, auth-handler", kind)), nil
		}

		items, err := localCatalog.List(s.ctx, artifactKind, name)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to list catalog entries: %v", err)), nil
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
