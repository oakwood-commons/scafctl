// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/scaffold"
)

// registerScaffoldTools registers solution scaffolding MCP tools.
func (s *Server) registerScaffoldTools() {
	// scaffold_solution
	scaffoldSolutionTool := mcp.NewTool("scaffold_solution",
		mcp.WithDescription("Generate a valid skeleton solution YAML from parameters. Produces a guaranteed-valid starting point with the correct structure, including metadata, resolvers, workflow, and tests based on selected features. The generated YAML can be immediately linted and customized."),
		mcp.WithTitleAnnotation("Scaffold Solution"),
		mcp.WithToolIcons(toolIcons["scaffold"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Solution name (lowercase with hyphens, 3-60 chars, e.g., 'my-solution')"),
		),
		mcp.WithString("description",
			mcp.Required(),
			mcp.Description("Brief description of what the solution does"),
		),
		mcp.WithString("version",
			mcp.Description("Semantic version (default: '1.0.0')"),
		),
		mcp.WithArray("features",
			mcp.Description("Features to include in the scaffold. Options: parameters, resolvers, actions, transforms, validation, tests, composition"),
			mcp.WithStringItems(),
		),
		mcp.WithArray("providers",
			mcp.Description("Specific providers to include examples for (e.g., ['http', 'exec', 'cel']). Use list_providers to see available providers."),
			mcp.WithStringItems(),
		),
	)
	s.mcpServer.AddTool(scaffoldSolutionTool, s.handleScaffoldSolution)
}

// handleScaffoldSolution generates a skeleton solution YAML.
func (s *Server) handleScaffoldSolution(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("name"),
			WithSuggestion("Provide a name for the solution (lowercase, hyphens allowed)"),
		), nil
	}

	description, err := request.RequireString("description")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("description"),
			WithSuggestion("Provide a brief description of what the solution does"),
		), nil
	}

	version := request.GetString("version", "1.0.0")

	// Parse features
	features := make(map[string]bool)
	args := request.GetArguments()
	if featuresRaw, ok := args["features"]; ok && featuresRaw != nil {
		if featureSlice, ok := featuresRaw.([]any); ok {
			for _, f := range featureSlice {
				if fs, ok := f.(string); ok {
					features[fs] = true
				}
			}
		}
	}

	// Parse providers
	var providerNames []string
	if providersRaw, ok := args["providers"]; ok && providersRaw != nil {
		if providerSlice, ok := providersRaw.([]any); ok {
			for _, p := range providerSlice {
				if ps, ok := p.(string); ok {
					providerNames = append(providerNames, ps)
				}
			}
		}
	}

	result := scaffold.Solution(scaffold.Options{
		Name:        name,
		Description: description,
		Version:     version,
		Features:    features,
		Providers:   providerNames,
	})

	return mcp.NewToolResultJSON(map[string]any{
		"yaml":      result.YAML,
		"filename":  result.Filename,
		"features":  result.Features,
		"nextSteps": result.NextSteps,
	})
}
