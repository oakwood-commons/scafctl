// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/oakwood-commons/scafctl/pkg/api"
	"github.com/oakwood-commons/scafctl/pkg/api/endpoints"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// registerAPITools registers REST API-related MCP tools.
func (s *Server) registerAPITools() {
	getOpenAPISpecTool := mcp.NewTool("get_openapi_spec",
		mcp.WithDescription("Generate the full OpenAPI specification for the scafctl REST API. Returns the complete spec including all endpoints, request/response schemas, authentication schemes, and documentation. Useful for API exploration, client generation, and integration planning."),
		mcp.WithTitleAnnotation("Get OpenAPI Spec"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithRawOutputSchema(outputSchemaOpenAPISpec),
	)
	s.mcpServer.AddTool(getOpenAPISpecTool, s.handleGetOpenAPISpec)

	getAPIEndpointsTool := mcp.NewTool("list_api_endpoints",
		mcp.WithDescription("List all available REST API endpoints with their HTTP method, path, summary, and tags. Provides a quick overview of the scafctl API surface without the full OpenAPI spec."),
		mcp.WithTitleAnnotation("List API Endpoints"),
		mcp.WithToolIcons(toolIcons["config"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithRawOutputSchema(outputSchemaAPIEndpoints),
	)
	s.mcpServer.AddTool(getAPIEndpointsTool, s.handleListAPIEndpoints)
}

// handleGetOpenAPISpec generates the full OpenAPI spec without starting the server.
func (s *Server) handleGetOpenAPISpec(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	router := chi.NewRouter()
	apiVersion := settings.DefaultAPIVersion
	humaConfig := api.BuildHumaConfig(apiVersion)

	humaAPI := humachi.New(router, humaConfig)
	endpoints.RegisterAllForExport(humaAPI)

	spec := humaAPI.OpenAPI()
	return mcp.NewToolResultJSON(spec)
}

// handleListAPIEndpoints returns a summary of all API endpoints.
func (s *Server) handleListAPIEndpoints(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	router := chi.NewRouter()
	apiVersion := settings.DefaultAPIVersion
	humaConfig := api.BuildHumaConfig(apiVersion)

	humaAPI := humachi.New(router, humaConfig)
	endpoints.RegisterAllForExport(humaAPI)

	spec := humaAPI.OpenAPI()

	type endpointSummary struct {
		Method  string   `json:"method"`
		Path    string   `json:"path"`
		Summary string   `json:"summary"`
		Tags    []string `json:"tags,omitempty"`
	}

	var result []endpointSummary
	for path, item := range spec.Paths {
		addOp := func(method string, op *huma.Operation) {
			if op == nil {
				return
			}
			result = append(result, endpointSummary{
				Method:  method,
				Path:    path,
				Summary: op.Summary,
				Tags:    op.Tags,
			})
		}
		addOp("get", item.Get)
		addOp("post", item.Post)
		addOp("put", item.Put)
		addOp("patch", item.Patch)
		addOp("delete", item.Delete)
		addOp("head", item.Head)
		addOp("options", item.Options)
	}

	return mcp.NewToolResultJSON(map[string]any{
		"count":     len(result),
		"endpoints": result,
	})
}
