// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	provdetail "github.com/oakwood-commons/scafctl/pkg/provider/detail"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
	"github.com/oakwood-commons/scafctl/pkg/solution/soltesting"
)

// registerResourceTemplates registers MCP resource templates on the server.
func (s *Server) registerResourceTemplates() {
	// solution://{+name} — unified solution resource template using RFC 6570 reserved expansion
	// so that file paths containing slashes (e.g., solution:///Users/.../file.yaml) match.
	// Suffix-based routing dispatches to content, schema, graph, or tests handlers.
	solutionTemplate := mcp.NewResourceTemplate(
		"solution://{+name}",
		"Solution Resource",
		mcp.WithTemplateDescription("Access solution content, schema, dependency graph, or tests. "+
			"Use the solution's local file path, catalog name, or URL as the name. "+
			"Append /schema for the input JSON Schema, /graph for the resolver dependency graph, "+
			"or /tests for the functional test cases. "+
			"Examples: solution://path/to/solution.yaml, solution://my-solution/schema, "+
			"solution:///abs/path/solution.yaml/tests"),
		mcp.WithTemplateMIMEType("application/yaml"),
		mcp.WithTemplateIcons(resourceIcons["solution"]),
		mcp.WithTemplateAnnotations([]mcp.Role{mcp.RoleUser, mcp.RoleAssistant}, 0.7, ""),
	)
	s.mcpServer.AddResourceTemplate(solutionTemplate, s.routeSolutionResource)

	// provider://{name} — provider details including schema, examples, capabilities
	providerTemplate := mcp.NewResourceTemplate(
		"provider://{name}",
		"Provider Details",
		mcp.WithTemplateDescription("Returns detailed information about a provider including its input schema (with required/optional properties, types, defaults, examples), output schemas per capability, YAML usage examples, CLI usage examples, and capabilities. Use the provider name (e.g., exec, http, static, file, cel, directory, parameter) as the {name} parameter."),
		mcp.WithTemplateMIMEType("application/json"),
		mcp.WithTemplateIcons(resourceIcons["provider"]),
		mcp.WithTemplateAnnotations([]mcp.Role{mcp.RoleAssistant}, 0.6, ""),
	)
	s.mcpServer.AddResourceTemplate(providerTemplate, s.handleProviderResource)

	// provider://reference — compact reference of all providers and their key properties
	s.mcpServer.AddResource(
		mcp.NewResource(
			"provider://reference",
			"Provider Quick Reference",
			mcp.WithResourceDescription("Returns a compact reference of all registered providers with their required inputs, capabilities, and descriptions. Use this to quickly understand what providers are available and what inputs they need, without calling get_provider_schema for each one individually."),
			mcp.WithMIMEType("application/json"),
			mcp.WithResourceIcons(resourceIcons["provider"]),
			mcp.WithAnnotations([]mcp.Role{mcp.RoleAssistant}, 0.8, ""),
		),
		s.handleProviderReferenceResource,
	)
}

// routeSolutionResource dispatches solution resource requests based on URI suffix.
// This unified router avoids map iteration ordering issues when multiple templates
// with {+name} (reserved expansion) could match the same URI.
func (s *Server) routeSolutionResource(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name := extractNameFromURI(request.Params.URI, "solution://")

	switch {
	case strings.HasSuffix(name, "/tests"):
		return s.handleSolutionTestsResource(ctx, request)
	case strings.HasSuffix(name, "/schema"):
		return s.handleSolutionSchemaResource(ctx, request)
	case strings.HasSuffix(name, "/graph"):
		return s.handleSolutionGraphResource(ctx, request)
	default:
		return s.handleSolutionResource(ctx, request)
	}
}

// handleSolutionResource returns the raw YAML content of a solution.
func (s *Server) handleSolutionResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name := extractNameFromURI(request.Params.URI, "solution://")
	if name == "" {
		return nil, fmt.Errorf("solution name is required in URI (e.g., solution://path/to/solution.yaml)")
	}

	sol, err := inspect.LoadSolution(s.ctx, name)
	if err != nil {
		return nil, fmt.Errorf("loading solution %q: %w", name, err)
	}

	yamlBytes, err := sol.ToYAML()
	if err != nil {
		return nil, fmt.Errorf("marshaling solution to YAML: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/yaml",
			Text:     string(yamlBytes),
		},
	}, nil
}

// handleSolutionSchemaResource returns a JSON Schema for a solution's input parameters.
func (s *Server) handleSolutionSchemaResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name := extractNameFromURI(request.Params.URI, "solution://")
	name = strings.TrimSuffix(name, "/schema")
	if name == "" {
		return nil, fmt.Errorf("solution name is required in URI (e.g., solution://path/to/solution.yaml/schema)")
	}

	sol, err := inspect.LoadSolution(s.ctx, name)
	if err != nil {
		return nil, fmt.Errorf("loading solution %q: %w", name, err)
	}

	schema := generateSolutionInputSchema(sol)
	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling schema to JSON: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(schemaJSON),
		},
	}, nil
}

// handleSolutionGraphResource returns the resolver dependency graph for a solution.
func (s *Server) handleSolutionGraphResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name := extractNameFromURI(request.Params.URI, "solution://")
	name = strings.TrimSuffix(name, "/graph")
	if name == "" {
		return nil, fmt.Errorf("solution name is required in URI (e.g., solution://path/to/solution.yaml/graph)")
	}

	sol, err := inspect.LoadSolution(s.ctx, name)
	if err != nil {
		return nil, fmt.Errorf("loading solution %q: %w", name, err)
	}

	result, err := s.renderResolverGraph(sol, s.registry)
	if err != nil {
		return nil, fmt.Errorf("rendering resolver graph: %w", err)
	}

	// Extract the JSON text from the tool result
	if result.IsError {
		tc, ok := result.Content[0].(mcp.TextContent)
		if !ok {
			return nil, fmt.Errorf("building graph: unexpected content type")
		}
		return nil, fmt.Errorf("building graph: %s", tc.Text)
	}

	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		return nil, fmt.Errorf("unexpected content type in graph result")
	}
	graphJSON := tc.Text

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     graphJSON,
		},
	}, nil
}

// handleProviderResource returns detailed information about a single provider.
func (s *Server) handleProviderResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name := extractNameFromURI(request.Params.URI, "provider://")
	if name == "" {
		return nil, fmt.Errorf("provider name is required in URI (e.g., provider://exec)")
	}

	if s.registry == nil {
		return nil, fmt.Errorf("provider registry not available")
	}

	p, ok := s.registry.Get(name)
	if !ok {
		names := s.registry.List()
		return nil, fmt.Errorf("provider %q not found. Available providers: %v", name, names)
	}

	desc := p.Descriptor()
	detail := provdetail.BuildProviderDetail(*desc)

	detailJSON, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling provider detail to JSON: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(detailJSON),
		},
	}, nil
}

// handleProviderReferenceResource returns a compact reference of all providers.
func (s *Server) handleProviderReferenceResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	if s.registry == nil {
		return nil, fmt.Errorf("provider registry not available")
	}

	providers := s.registry.ListProviders()

	reference := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		desc := p.Descriptor()

		entry := map[string]any{
			"name":         desc.Name,
			"description":  desc.Description,
			"capabilities": provdetail.CapabilitiesToStrings(desc.Capabilities),
		}

		// Include required and optional input fields
		if desc.Schema != nil && len(desc.Schema.Properties) > 0 {
			requiredSet := make(map[string]bool, len(desc.Schema.Required))
			for _, n := range desc.Schema.Required {
				requiredSet[n] = true
			}

			requiredFields := make(map[string]string)
			optionalFields := make(map[string]string)
			for propName, prop := range desc.Schema.Properties {
				typeStr := prop.Type
				if typeStr == "" {
					typeStr = "any"
				}
				summary := typeStr
				if prop.Description != "" {
					// Truncate long descriptions for compact reference
					d := prop.Description
					if len(d) > 80 {
						d = d[:77] + "..."
					}
					summary = typeStr + " — " + d
				}
				if requiredSet[propName] {
					requiredFields[propName] = summary
				} else {
					optionalFields[propName] = summary
				}
			}

			if len(requiredFields) > 0 {
				entry["requiredInputs"] = requiredFields
			}
			if len(optionalFields) > 0 {
				entry["optionalInputs"] = optionalFields
			}
		}

		// Include first example YAML if available, for quick reference
		if len(desc.Examples) > 0 {
			entry["exampleYAML"] = desc.Examples[0].YAML
		}

		reference = append(reference, entry)
	}

	refJSON, err := json.MarshalIndent(reference, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling provider reference to JSON: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(refJSON),
		},
	}, nil
}

// handleSolutionTestsResource returns the functional test cases defined in a solution.
func (s *Server) handleSolutionTestsResource(_ context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
	name := extractNameFromURI(request.Params.URI, "solution://")
	name = strings.TrimSuffix(name, "/tests")
	if name == "" {
		return nil, fmt.Errorf("solution name is required in URI (e.g., solution://path/to/solution.yaml/tests)")
	}

	st, err := soltesting.DiscoverFromFile(name)
	if err != nil {
		return nil, fmt.Errorf("discovering tests in %q: %w", name, err)
	}

	if len(st.Cases) == 0 {
		result := map[string]any{
			"solutionName": st.SolutionName,
			"filePath":     st.FilePath,
			"testCount":    0,
			"cases":        map[string]any{},
		}
		resultJSON, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("marshaling test data: %w", err)
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      request.Params.URI,
				MIMEType: "application/json",
				Text:     string(resultJSON),
			},
		}, nil
	}

	// Build structured test data
	cases := make(map[string]any, len(st.Cases))
	testNames := soltesting.SortedTestNames(*st)
	for _, tName := range testNames {
		tc := st.Cases[tName]
		caseData := map[string]any{
			"command": tc.Command,
		}
		if tc.Description != "" {
			caseData["description"] = tc.Description
		}
		if len(tc.Args) > 0 {
			caseData["args"] = tc.Args
		}
		if len(tc.Assertions) > 0 {
			caseData["assertions"] = tc.Assertions
		}
		if len(tc.Tags) > 0 {
			caseData["tags"] = tc.Tags
		}
		if !tc.Skip.IsZero() {
			caseData["skip"] = tc.Skip.String()
			if tc.SkipReason != "" {
				caseData["skipReason"] = tc.SkipReason
			}
		}
		cases[tName] = caseData
	}

	result := map[string]any{
		"solutionName": st.SolutionName,
		"filePath":     st.FilePath,
		"testCount":    len(st.Cases),
		"cases":        cases,
	}
	if st.Config != nil {
		result["config"] = st.Config
	}

	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling test data: %w", err)
	}

	return []mcp.ResourceContents{
		mcp.TextResourceContents{
			URI:      request.Params.URI,
			MIMEType: "application/json",
			Text:     string(resultJSON),
		},
	}, nil
}

// extractNameFromURI extracts the resource name from a URI by stripping the scheme prefix.
func extractNameFromURI(uri, scheme string) string {
	if !strings.HasPrefix(uri, scheme) {
		return ""
	}
	return strings.TrimPrefix(uri, scheme)
}

// generateSolutionInputSchema builds a JSON Schema from a solution's resolver definitions.
// It introspects the resolvers to determine which ones accept user-supplied parameters
// and generates a schema that describes those inputs.
func generateSolutionInputSchema(sol *solution.Solution) map[string]any {
	schema := map[string]any{
		"$schema":     "https://json-schema.org/draft/2020-12/schema",
		"title":       fmt.Sprintf("%s Input Parameters", sol.Metadata.Name),
		"description": fmt.Sprintf("Input parameters for the %s solution", sol.Metadata.Name),
		"type":        "object",
	}

	if !sol.Spec.HasResolvers() {
		schema["properties"] = map[string]any{}
		return schema
	}

	properties := map[string]any{}
	var required []string

	// Sort resolver names for deterministic output
	names := make([]string, 0, len(sol.Spec.Resolvers))
	for name := range sol.Spec.Resolvers {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		r := sol.Spec.Resolvers[name]
		if r == nil {
			continue
		}

		// Only include resolvers that use the "parameter" provider as a source,
		// as these are the ones that accept user-supplied input.
		if isParameterResolver(r) {
			prop := buildResolverProperty(r)
			properties[name] = prop

			// If a resolver has no fallback sources (only parameter provider),
			// it's effectively required.
			if isRequiredParameter(r) {
				required = append(required, name)
			}
		}
	}

	schema["properties"] = properties
	if len(required) > 0 {
		schema["required"] = required
	}

	return schema
}

// isParameterResolver checks if a resolver uses the "parameter" provider
// in its resolve phase.
func isParameterResolver(r *resolver.Resolver) bool {
	if r.Resolve == nil {
		return false
	}
	for _, src := range r.Resolve.With {
		if src.Provider == "parameter" {
			return true
		}
	}
	return false
}

// isRequiredParameter checks if a resolver's only source is the "parameter" provider
// (meaning there's no fallback chain and the parameter is effectively required).
func isRequiredParameter(r *resolver.Resolver) bool {
	if r.Resolve == nil {
		return false
	}
	// If there's only one source and it's a parameter, it's required
	// (unless there's a conditional that might skip it)
	if len(r.Resolve.With) == 1 && r.Resolve.With[0].Provider == "parameter" && r.When == nil {
		return true
	}
	return false
}

// buildResolverProperty builds a JSON Schema property for a resolver.
func buildResolverProperty(r *resolver.Resolver) map[string]any {
	prop := map[string]any{}

	// Map resolver type to JSON Schema type
	jsonType := resolverTypeToJSONSchemaType(r.Type)
	if jsonType != "" {
		prop["type"] = jsonType
	}

	// Add description
	if r.Description != "" {
		prop["description"] = r.Description
	} else if r.DisplayName != "" {
		prop["description"] = r.DisplayName
	}

	// Add example
	if r.Example != nil {
		prop["examples"] = []any{r.Example}
	}

	return prop
}

// resolverTypeToJSONSchemaType maps a resolver type to a JSON Schema type.
func resolverTypeToJSONSchemaType(t resolver.Type) string {
	switch t {
	case "string":
		return "string"
	case "int":
		return "integer"
	case "float":
		return "number"
	case "bool":
		return "boolean"
	case "array":
		return "array"
	case "object":
		return "object"
	case "time":
		return "string" // ISO 8601 format
	case "duration":
		return "string" // Go duration format
	default:
		return "" // Unknown/any — omit type constraint
	}
}
