// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
)

// contextualToolFilter dynamically shows/hides tools based on the current
// server configuration. Tools that require unavailable capabilities are
// filtered out so AI agents don't call tools that will always fail.
func contextualToolFilter(s *Server) func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	return func(_ context.Context, tools []mcp.Tool) []mcp.Tool {
		// Determine what's available
		hasAuth := s.authReg != nil && len(s.authReg.List()) > 0
		hasCatalog := s.config != nil && len(s.config.Catalogs) > 0
		hasRegistry := s.registry != nil

		// Tools that require specific capabilities
		authTools := map[string]bool{
			"auth_status":        true,
			"list_auth_handlers": true,
		}
		catalogTools := map[string]bool{
			"catalog_list":    true,
			"catalog_inspect": true,
			"list_solutions":  true,
		}
		registryTools := map[string]bool{
			"list_providers":      true,
			"get_provider_schema": true,
		}

		filtered := make([]mcp.Tool, 0, len(tools))
		for _, tool := range tools {
			// Hide auth tools when no auth handlers configured
			if !hasAuth && authTools[tool.Name] {
				continue
			}
			// Hide catalog tools when no catalogs configured
			if !hasCatalog && catalogTools[tool.Name] {
				continue
			}
			// Hide provider tools when no registry available
			if !hasRegistry && registryTools[tool.Name] {
				continue
			}
			filtered = append(filtered, tool)
		}

		return filtered
	}
}
