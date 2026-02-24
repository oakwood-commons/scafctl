// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/filepath"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	solutionget "github.com/oakwood-commons/scafctl/pkg/solution/get"
)

// discoverWorkspaceRoots asks the MCP client for its workspace root directories
// and returns file paths. If the client doesn't support roots or the request fails,
// it returns nil (callers should fall back to explicit paths).
func (s *Server) discoverWorkspaceRoots(ctx context.Context) []string {
	roots, err := s.RequestRoots(ctx)
	if err != nil {
		s.logger.V(1).Info("failed to request workspace roots", "error", err)
		return nil
	}

	paths := make([]string, 0, len(roots))
	for _, root := range roots {
		uri := root.URI
		// Convert file:// URIs to filesystem paths
		if strings.HasPrefix(uri, "file://") {
			paths = append(paths, strings.TrimPrefix(uri, "file://"))
		}
	}

	return paths
}

// discoverSolutionFiles searches for solution files using the same conventions
// as the CLI (settings.RootSolutionFolders + settings.SolutionFileNames). It also
// searches MCP workspace roots if provided by the client. Returns all discovered
// file paths, or nil if none found.
func (s *Server) discoverSolutionFiles(ctx context.Context) []string {
	var files []string

	// 1. Use the canonical CLI discovery logic (searches CWD-relative paths)
	getter := solutionget.NewGetter(solutionget.WithLogger(s.logger))
	if found := getter.FindSolution(); found != "" {
		files = append(files, found)
	}

	// 2. Also search MCP workspace roots using the same file name patterns
	roots := s.discoverWorkspaceRoots(ctx)
	for _, root := range roots {
		for _, folder := range settings.RootSolutionFolders {
			for _, filename := range settings.SolutionFileNames {
				fullPath := filepath.Join(root, folder, filename)
				if filepath.PathExists(fullPath, nil) {
					// Deduplicate against already-found files
					if !containsPath(files, fullPath) {
						files = append(files, fullPath)
					}
				}
			}
		}
	}

	if len(files) == 0 {
		return nil
	}
	return files
}

// containsPath checks if a path is already in the slice.
func containsPath(paths []string, target string) bool {
	for _, p := range paths {
		if p == target {
			return true
		}
	}
	return false
}

// elicitMissingParams uses the MCP elicitation capability to prompt the user
// for missing required parameters. Returns a map of parameter names to values
// provided by the user, or nil if elicitation is not supported/declined.
func (s *Server) elicitMissingParams(ctx context.Context, paramNames []string, descriptions map[string]string) map[string]string {
	if len(paramNames) == 0 {
		return nil
	}

	// Build the elicitation schema with properties for each missing parameter
	properties := make(map[string]map[string]string, len(paramNames))
	for _, name := range paramNames {
		prop := map[string]string{"type": "string"}
		if desc, ok := descriptions[name]; ok {
			prop["description"] = desc
		} else {
			prop["description"] = fmt.Sprintf("Value for parameter %q", name)
		}
		properties[name] = prop
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   paramNames,
	}

	req := mcp.ElicitationRequest{}
	req.Params.Message = "The following parameters are required to run this solution. Please provide values:"
	req.Params.RequestedSchema = schema

	result, err := s.RequestElicitation(ctx, req)
	if err != nil {
		s.logger.V(1).Info("elicitation request failed", "error", err)
		return nil
	}

	if result == nil || result.Action != "accept" {
		return nil
	}

	// Extract the parameter values from the elicitation result
	values := make(map[string]string, len(paramNames))
	if result.Content != nil {
		if contentMap, ok := result.Content.(map[string]any); ok {
			for _, name := range paramNames {
				if val, ok := contentMap[name]; ok {
					values[name] = fmt.Sprintf("%v", val)
				}
			}
		}
	}

	return values
}
