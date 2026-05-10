// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	provdetail "github.com/oakwood-commons/scafctl/pkg/provider/detail"
	"github.com/oakwood-commons/scafctl/pkg/provider/official"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
)

// ensureProvider attempts to auto-resolve a provider that is not in the
// registry by looking it up in the official provider registry and loading
// it via the plugin pool. On success, returns a non-nil release function
// that the caller must defer. Returns an error if the provider cannot be resolved.
func (s *Server) ensureProvider(ctx context.Context, name string) (release func(), err error) {
	if s.pluginPool == nil {
		return nil, fmt.Errorf("provider %q not found and plugin pool not configured", name)
	}
	officialReg := official.RegistryFromContext(s.ctx)
	if officialReg == nil {
		return nil, fmt.Errorf("provider %q not found and official registry not available", name)
	}
	op, found := officialReg.Get(name)
	if !found {
		return nil, fmt.Errorf("provider %q is not a known official provider", name)
	}
	dep := op.ToPluginDependency()
	rel, err := s.pluginPool.EnsureAndAcquire(ctx, []solution.PluginDependency{dep})
	if err != nil {
		return nil, fmt.Errorf("loading plugin for provider %q: %w", name, err)
	}
	return rel, nil
}

// providerSource returns "official" if the provider is in the official
// registry, otherwise "builtin".
func (s *Server) providerSource(name string) string {
	if officialReg := official.RegistryFromContext(s.ctx); officialReg != nil {
		if _, found := officialReg.Get(name); found {
			return "official"
		}
	}
	return "builtin"
}

// registerProviderTools registers all provider-related MCP tools.
func (s *Server) registerProviderTools() {
	// list_providers
	listProvidersTool := mcp.NewTool("list_providers",
		mcp.WithDescription("List all available solution providers (e.g. http, static, file, cel, exec, directory). Solution providers are the building blocks of solutions -- they fetch data, transform values, validate inputs, and execute actions. Returns name, displayName, description, source, capabilities, category, and version for each provider. The 'source' field indicates whether a provider is 'builtin' or 'official' (auto-fetched from catalog). Official providers may have empty capabilities and category fields. To get full input/output schemas, examples, and CLI usage for a specific provider, call get_provider_schema with the provider name."),
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
	s.addTool(listProvidersTool, s.handleListProviders)

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
	s.addTool(getProviderSchemaTool, s.handleGetProviderSchema)

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
	s.addTool(getProviderOutputShapeTool, s.handleGetProviderOutputShape)

	// run_provider
	runProviderTool := mcp.NewTool("run_provider",
		mcp.WithDescription(fmt.Sprintf(
			"Execute a provider directly and return structured JSON output. "+
				"Providers are the building blocks of %s — they fetch data (http, file, env), "+
				"transform values (cel, static), validate inputs, and perform actions (exec, github, file). "+
				"Use list_providers and get_provider_schema to discover available providers and their input schemas. "+
				"NOTE: Some providers have side effects (e.g., exec runs commands, github creates issues). "+
				"Use dry_run=true to preview what would happen without executing.", s.name)),
		mcp.WithTitleAnnotation("Run Provider"),
		mcp.WithToolIcons(toolIcons["provider"]),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("provider",
			mcp.Required(),
			mcp.Description("Provider name (e.g., http, static, file, cel, exec, github, env)"),
		),
		mcp.WithObject("inputs",
			mcp.Description("Provider input parameters as key-value pairs. Use get_provider_schema to discover required and optional fields."),
		),
		mcp.WithString("capability",
			mcp.Description("Capability to execute. Defaults to the provider's first declared capability."),
			mcp.Enum("from", "transform", "validation", "authentication", "action"),
		),
		mcp.WithBoolean("dry_run",
			mcp.Description("Preview what would happen without executing. Defaults to false."),
		),
		mcp.WithString("expression",
			mcp.Description("Optional CEL expression to filter/transform the provider output before returning. "+
				"The result object is bound to '_' (e.g., '_.data', 'size(_.data)', '_.data.filter(x, x.status == \"open\")'). "+
				"Use this to reduce large outputs to only the fields you need."),
		),
	)
	s.addTool(runProviderTool, s.handleRunProvider)
}

// providerItem is a structured response for provider listings.
type providerItem struct {
	Name         string   `json:"name"`
	DisplayName  string   `json:"displayName,omitempty"`
	Description  string   `json:"description,omitempty"`
	Source       string   `json:"source"`
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
			Source:       "builtin",
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

	// Include official plugin providers only when no filters are active
	// (official providers don't expose capability/category metadata).
	if capability == "" && category == "" {
		for _, oi := range official.ListItems(s.ctx) {
			items = append(items, providerItem{
				Name:         oi.Name,
				DisplayName:  oi.DisplayName,
				Description:  oi.Description,
				Source:       oi.Source,
				Capabilities: oi.Capabilities,
				Version:      oi.Version,
			})
		}
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
func (s *Server) handleGetProviderSchema(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		// Try auto-resolving via the plugin pool before falling back
		release, resolveErr := s.ensureProvider(ctx, name)
		if resolveErr == nil {
			defer release()
			desc, err = inspect.LookupProvider(s.ctx, name, s.registry)
		}
	}
	if err != nil {
		// Check official provider registry before returning error
		if officialReg := official.RegistryFromContext(s.ctx); officialReg != nil {
			if op, found := officialReg.Get(name); found {
				detail := official.Detail(op)
				detail["schemaAvailable"] = false
				detail["hint"] = "Full schema, examples, and capabilities are unavailable until the plugin is fetched. Run 'plugins install " + op.CatalogRef + "' to fetch it."
				return mcp.NewToolResultJSON(detail)
			}
		}
		// Build a helpful error with available provider names
		availableNames := ""
		if s.registry != nil {
			names := s.registry.List()
			if len(names) > 0 {
				availableNames = fmt.Sprintf(". Available built-in providers: %v", names)
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
	detail["source"] = s.providerSource(name)

	return mcp.NewToolResultJSON(detail)
}

// handleGetProviderOutputShape returns the output schema for a provider, optionally
// filtered by capability. This makes it easy for agents to discover what fields
// resolver results contain after execution.
func (s *Server) handleGetProviderOutputShape(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		// Try auto-resolving via the plugin pool
		release, resolveErr := s.ensureProvider(ctx, name)
		if resolveErr == nil {
			defer release()
			desc, err = inspect.LookupProvider(s.ctx, name, s.registry)
		}
	}
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
			sort.Strings(availableCaps)
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

// handleRunProvider executes a provider directly and returns structured output.
func (s *Server) handleRunProvider(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name, err := request.RequireString("provider")
	if err != nil {
		return newStructuredError(ErrCodeInvalidInput, err.Error(),
			WithField("provider"),
			WithSuggestion("Use list_providers to see available provider names"),
			WithRelatedTools("list_providers"),
		), nil
	}

	inputsRaw := request.GetArguments()["inputs"]
	inputs, ok := inputsRaw.(map[string]any)
	if !ok && inputsRaw != nil {
		return newStructuredError(ErrCodeInvalidInput, "inputs must be a JSON object",
			WithField("inputs"),
			WithSuggestion("Pass inputs as key-value pairs, e.g. {\"url\": \"https://...\"}"),
		), nil
	}
	if inputs == nil {
		inputs = make(map[string]any)
	}

	capability := request.GetString("capability", "")
	dryRun := request.GetBool("dry_run", false)

	if s.registry == nil {
		return newStructuredError(ErrCodeConfigError, "provider registry not available",
			WithSuggestion("Ensure the server was started with a provider registry"),
		), nil
	}

	prov, ok := s.registry.Get(name)
	if !ok {
		// Try auto-resolving via the plugin pool
		release, resolveErr := s.ensureProvider(ctx, name)
		if resolveErr != nil {
			return newStructuredError(ErrCodeLoadFailed,
				fmt.Sprintf("failed to load provider %q: %v", name, resolveErr),
				WithField("provider"),
				WithSuggestion("Check plugin pool configuration and provider availability"),
				WithRelatedTools("list_providers"),
			), nil
		}
		defer release()
		prov, ok = s.registry.Get(name)
	}
	if !ok {
		availableNames := ""
		if names := s.registry.List(); len(names) > 0 {
			availableNames = fmt.Sprintf(". Available providers: %v", names)
		}
		return newStructuredError(ErrCodeNotFound,
			fmt.Sprintf("provider %q not found%s", name, availableNames),
			WithField("provider"),
			WithSuggestion("Use list_providers to see available provider names"),
			WithRelatedTools("list_providers"),
		), nil
	}

	result, err := provider.RunProvider(ctx, provider.RunOptions{
		Provider:   prov,
		Inputs:     inputs,
		Capability: capability,
		DryRun:     dryRun,
	})
	if err != nil {
		return newStructuredError(ErrCodeExecFailed, err.Error(),
			WithSuggestion("Check inputs with get_provider_schema and retry"),
			WithRelatedTools("get_provider_schema"),
		), nil
	}

	// Apply optional CEL expression to transform/filter the result.
	expression := request.GetString("expression", "")
	if expression != "" {
		// Convert struct to map for CEL access.
		resultMap, marshalErr := structToMap(result)
		if marshalErr != nil {
			return newStructuredError(ErrCodeExecFailed,
				fmt.Sprintf("failed to prepare result for CEL evaluation: %v", marshalErr),
			), nil
		}
		transformed, celErr := celexp.EvaluateExpression(ctx, expression, resultMap, nil)
		if celErr != nil {
			return newStructuredError(ErrCodeExecFailed,
				fmt.Sprintf("CEL expression evaluation failed: %v", celErr),
				WithField("expression"),
				WithSuggestion("Use validate_expression to check CEL syntax first"),
				WithRelatedTools("validate_expression", "list_cel_functions"),
			), nil
		}
		return mcp.NewToolResultJSON(transformed)
	}

	return mcp.NewToolResultJSON(result)
}

// structToMap converts a Go struct to a map[string]any via JSON round-trip
// so that CEL expressions can access fields by name.
func structToMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}
