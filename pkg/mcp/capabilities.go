// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	pathlib "path/filepath"
	"sort"
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

// discoverSolutionFiles searches for solution files using the unified resolution
// chain (FindAllSolutions) which respects taskfile.yaml/yml and returns matches
// in priority order. It also searches MCP workspace roots if provided by the
// client. Returns all discovered file paths, or nil if none found.
func (s *Server) discoverSolutionFiles(ctx context.Context) []string {
	var files []string

	// 1. Use the unified discovery logic (searches CWD-relative paths)
	solutionFolders := settings.SolutionFoldersFor(s.name)
	solutionFileNames := settings.SolutionFileNamesFor(s.name)
	getter := solutionget.NewGetter(
		solutionget.WithLogger(s.logger),
		solutionget.WithSolutionDiscovery(solutionFolders, solutionFileNames),
	)
	for _, result := range getter.FindAllSolutions() {
		files = append(files, result.Path)
	}

	// 2. Also search MCP workspace roots using the same file name patterns
	roots := s.discoverWorkspaceRoots(ctx)
	for _, root := range roots {
		for _, folder := range solutionFolders {
			for _, filename := range solutionFileNames {
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

// containsPath checks if a path is already in the slice using absolute path
// comparison to handle cases where CWD-relative and workspace-root-relative
// paths resolve to the same file.
func containsPath(paths []string, target string) bool {
	absTarget := target
	if abs, err := pathlib.Abs(target); err == nil {
		absTarget = abs
	}
	for _, p := range paths {
		absP := p
		if abs, err := pathlib.Abs(p); err == nil {
			absP = abs
		}
		if absP == absTarget {
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

// CapabilityKind distinguishes tools from prompts.
type CapabilityKind string

const (
	// CapabilityTool is an MCP tool (callable by agents).
	CapabilityTool CapabilityKind = "tool"
	// CapabilityPrompt is an MCP prompt (template for agent interactions).
	CapabilityPrompt CapabilityKind = "prompt"
)

// CapabilitySource identifies where a capability was registered.
type CapabilitySource string

const (
	// SourceCore marks capabilities registered by scafctl itself.
	SourceCore CapabilitySource = "core"
	// SourcePlugin marks capabilities registered by plugins or embedders.
	SourcePlugin CapabilitySource = "plugin"
)

// Capability describes a single MCP tool or prompt.
type Capability struct {
	Kind        CapabilityKind   `json:"kind"        yaml:"kind"`
	Name        string           `json:"name"        yaml:"name"`
	Title       string           `json:"title"       yaml:"title"`
	Description string           `json:"description" yaml:"description"`
	Source      CapabilitySource `json:"source"      yaml:"source"`
	ReadOnly    bool             `json:"readOnly"    yaml:"readOnly"`
	Destructive bool             `json:"destructive" yaml:"destructive"`
}

// ListCapabilities returns all tools and tracked prompts registered on
// the server. Prompts must be registered via Server.AddPrompt (or the
// internal addCorePrompt) to appear; prompts added directly through
// MCPServer().AddPrompt() are not tracked.
// Core capabilities are tagged with SourceCore; embedder/plugin
// capabilities are tagged with SourcePlugin.
// The contextual tool filter is applied, so results match what MCP
// clients would discover at runtime.
func (s *Server) ListCapabilities() []Capability {
	// Tools — apply the same contextual filter used by the MCP protocol
	registered := s.mcpServer.ListTools()
	caps := make([]Capability, 0, len(registered)+len(s.prompts))
	allTools := make([]mcp.Tool, 0, len(registered))
	for _, st := range registered {
		allTools = append(allTools, st.Tool)
	}

	filterFn := contextualToolFilter(s)
	visible := filterFn(context.Background(), allTools)

	for _, t := range visible {
		source := SourcePlugin
		if _, ok := s.coreTools[t.Name]; ok {
			source = SourceCore
		}
		c := Capability{
			Kind:        CapabilityTool,
			Name:        t.Name,
			Title:       t.Annotations.Title,
			Description: t.Description,
			Source:      source,
		}
		if t.Annotations.ReadOnlyHint != nil {
			c.ReadOnly = *t.Annotations.ReadOnlyHint
		}
		if t.Annotations.DestructiveHint != nil {
			c.Destructive = *t.Annotations.DestructiveHint
		}
		caps = append(caps, c)
	}

	// Prompts — sourced from our tracked list
	for _, p := range s.prompts {
		source := SourcePlugin
		if _, ok := s.corePrompts[p.Name]; ok {
			source = SourceCore
		}
		caps = append(caps, Capability{
			Kind:        CapabilityPrompt,
			Name:        p.Name,
			Description: p.Description,
			Source:      source,
			ReadOnly:    true,
		})
	}

	sort.Slice(caps, func(i, j int) bool {
		if caps[i].Kind != caps[j].Kind {
			return caps[i].Kind < caps[j].Kind
		}
		return caps[i].Name < caps[j].Name
	})

	return caps
}
