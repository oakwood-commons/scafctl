// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	provdetail "github.com/oakwood-commons/scafctl/pkg/provider/detail"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
)

// registerProviderTools registers all provider-related MCP tools.
func (s *Server) registerProviderTools() {
	// list_providers
	listProvidersTool := mcp.NewTool("list_providers",
		mcp.WithDescription("List all available solution providers (e.g. http, static, file, cel, exec, directory). Solution providers are the building blocks of solutions — they fetch data, transform values, validate inputs, and execute actions. Returns name, description, capabilities, and category for each provider. To get full input/output schemas, examples, and CLI usage for a specific provider, call get_provider_schema with the provider name."),
		mcp.WithTitleAnnotation("List Providers"),
		mcp.WithToolIcons(toolIcons["provider"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("capability",
			mcp.Description("Filter by capability: from, transform, validation, authentication, action"),
			mcp.Enum("from", "transform", "validation", "authentication", "action"),
		),
		mcp.WithString("category",
			mcp.Description("Filter by category"),
		),
	)
	s.mcpServer.AddTool(listProvidersTool, s.handleListProviders)

	// get_provider_schema
	getProviderSchemaTool := mcp.NewTool("get_provider_schema",
		mcp.WithDescription("Get comprehensive information about a provider: input schema (properties with types, required/optional, defaults, validation), output schemas per capability, YAML usage examples, CLI usage examples, capabilities, and version info. ALWAYS call this before writing action or resolver YAML to verify exact field names, types, and which fields are required."),
		mcp.WithTitleAnnotation("Get Provider Schema"),
		mcp.WithToolIcons(toolIcons["provider"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Provider name (e.g., http, static, file, cel, parameter)"),
		),
	)
	s.mcpServer.AddTool(getProviderSchemaTool, s.handleGetProviderSchema)

	// get_provider_output_shape
	getProviderOutputShapeTool := mcp.NewTool("get_provider_output_shape",
		mcp.WithDescription("Get the output shape (field names, types) for a specific provider and capability. Use this to discover what fields a resolver produces after execution — essential for writing CEL expressions that reference resolver output. Returns the output schema for the requested capability, or all capabilities if none specified."),
		mcp.WithTitleAnnotation("Get Provider Output Shape"),
		mcp.WithToolIcons(toolIcons["provider"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("name",
			mcp.Required(),
			mcp.Description("Provider name (e.g., http, static, file, cel, exec)"),
		),
		mcp.WithString("capability",
			mcp.Description("Optional capability to filter output schema (from, transform, validation, authentication, action). Omit for all capabilities."),
			mcp.Enum("from", "transform", "validation", "authentication", "action"),
		),
	)
	s.mcpServer.AddTool(getProviderOutputShapeTool, s.handleGetProviderOutputShape)
}

// providerItem is a structured response for provider listings.
type providerItem struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"displayName,omitempty"`
	Description  string   `json:"description,omitempty"`
	Category     string   `json:"category,omitempty"`
	Capabilities []string `json:"capabilities"`
	Version      string   `json:"version,omitempty"`
	Deprecated   bool     `json:"deprecated,omitempty"`
	Beta         bool     `json:"beta,omitempty"`
}

// handleListProviders lists available providers with optional filtering.
func (s *Server) handleListProviders(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	capability := request.GetString("capability", "")
	category := request.GetString("category", "")

	if s.registry == nil {
		return newStructuredError(ErrCodeConfigError, "provider registry not available",
			WithSuggestion("Ensure the server was started with a provider registry"),
		), nil
	}

	var providers []provider.Provider
	switch {
	case capability != "":
		providers = s.registry.ListByCapability(provider.Capability(capability))
	case category != "":
		providers = s.registry.ListByCategory(category)
	default:
		providers = s.registry.ListProviders()
	}

	items := make([]providerItem, 0, len(providers))
	for _, p := range providers {
		d := p.Descriptor()
		caps := make([]string, 0, len(d.Capabilities))
		for _, c := range d.Capabilities {
			caps = append(caps, string(c))
		}
		item := providerItem{
			Name:         d.Name,
			DisplayName:  d.DisplayName,
			Description:  d.Description,
			Category:     d.Category,
			Capabilities: caps,
			Deprecated:   d.IsDeprecated,
			Beta:         d.Beta,
		}
		if d.Version != nil {
			item.Version = d.Version.String()
		}
		items = append(items, item)
	}

	result, err := mcp.NewToolResultJSON(items)
	if err != nil {
		return nil, err
	}
	result.Content = append(result.Content,
		mcp.NewResourceLink("provider://reference", "Provider Reference", "Compact reference of all providers", "application/json"),
	)
	return result, nil
}

// handleGetProviderSchema returns comprehensive provider information including
// input schema with required/optional annotations, output schemas, examples,
// CLI usage, and capabilities. Uses the same structured format as
// `scafctl get provider <name> -o json`.
func (s *Server) handleGetProviderSchema(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("name"),
			WithSuggestion("Use list_providers to see available provider names"),
			WithRelatedTools("list_providers"),
		), nil
	}

	desc, err := inspect.LookupProvider(s.ctx, name, s.registry)
	if err != nil {
		// Build a helpful error with available provider names
		availableNames := ""
		if s.registry != nil {
			names := s.registry.List()
			if len(names) > 0 {
				availableNames = fmt.Sprintf(". Available providers: %v", names)
			}
		}
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("provider %q not found%s", name, availableNames),
			WithField("name"),
			WithSuggestion("Use list_providers to see available provider names"),
			WithRelatedTools("list_providers"),
		), nil
	}

	// Use BuildProviderDetail for a structured, AI-friendly response that includes:
	// - schema with per-property "required" annotations (easier than parsing JSON Schema required array)
	// - output schemas per capability
	// - examples with YAML
	// - CLI usage examples
	// - version, capabilities, category, tags, links, maintainers
	detail := provdetail.BuildProviderDetail(*desc)

	return mcp.NewToolResultJSON(detail)
}

// handleGetProviderOutputShape returns the output schema for a provider, optionally
// filtered by capability. This makes it easy for agents to discover what fields
// resolver results contain after execution.
func (s *Server) handleGetProviderOutputShape(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("name")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("name"),
			WithSuggestion("Use list_providers to see available provider names"),
			WithRelatedTools("list_providers"),
		), nil
	}
	capability := request.GetString("capability", "")

	desc, err := inspect.LookupProvider(s.ctx, name, s.registry)
	if err != nil {
		availableNames := ""
		if s.registry != nil {
			names := s.registry.List()
			if len(names) > 0 {
				availableNames = fmt.Sprintf(". Available providers: %v", names)
			}
		}
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("provider %q not found%s", name, availableNames),
			WithField("name"),
			WithSuggestion("Use list_providers to see available provider names"),
			WithRelatedTools("list_providers"),
		), nil
	}

	if len(desc.OutputSchemas) == 0 {
		return newStructuredError(ErrCodeNotFound, fmt.Sprintf("provider %q has no output schemas defined", name),
			WithSuggestion("Not all providers define output schemas. Use get_provider_schema for full details."),
			WithRelatedTools("get_provider_schema"),
		), nil
	}

	result := map[string]any{
		"provider": name,
	}

	if capability != "" {
		provCap := provider.Capability(capability)
		schema, ok := desc.OutputSchemas[provCap]
		if !ok {
			availableCaps := make([]string, 0, len(desc.OutputSchemas))
			for c := range desc.OutputSchemas {
				availableCaps = append(availableCaps, string(c))
			}
			return newStructuredError(ErrCodeNotFound,
				fmt.Sprintf("provider %q has no output schema for capability %q. Available: %v", name, capability, availableCaps),
				WithField("capability"),
				WithSuggestion("Check the capability name against the available capabilities"),
			), nil
		}
		result["capability"] = capability
		result["outputSchema"] = provdetail.BuildSchemaOutput(schema)
	} else {
		outputSchemas := make(map[string]any, len(desc.OutputSchemas))
		for cap, schema := range desc.OutputSchemas {
			outputSchemas[string(cap)] = provdetail.BuildSchemaOutput(schema)
		}
		result["outputSchemas"] = outputSchemas
	}

	return mcp.NewToolResultJSON(result)
}
