// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/catalog/search"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// registerCatalogSearchTools registers catalog search and discovery MCP tools.
func (s *Server) registerCatalogSearchTools() {
	catalogSearchTool := mcp.NewTool("catalog_search",
		mcp.WithDescription(fmt.Sprintf(
			"Search for solutions in the default catalog and local catalog. "+
				"Use 'catalog' to target a specific registered catalog, or 'all' to search every registered catalog. "+
				"Returns relevance-scored results. Use 'inspect_solution' to get full details on a result. "+
				"Use 'catalog_list_registered' to see available catalogs. "+
				"Invoke with '%s run <name>' to execute a solution.", s.name)),
		mcp.WithTitleAnnotation("Catalog Search"),
		mcp.WithToolIcons(toolIcons["catalog"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("query",
			mcp.Description("Free-text search across name, description, tags, category, and providers"),
		),
		mcp.WithString("category",
			mcp.Description("Filter by solution category (exact match, case-insensitive)"),
		),
		mcp.WithString("provider",
			mcp.Description("Filter by provider name used in the solution (exact match, case-insensitive)"),
		),
		mcp.WithString("tags",
			mcp.Description("Comma-separated list of tags to filter by (AND logic)"),
		),
		mcp.WithString("catalog",
			mcp.Description("Catalog name to search. Omit for default catalog, use 'all' to search all registered catalogs, or 'local' for local only"),
		),
	)
	s.mcpServer.AddTool(catalogSearchTool, s.handleCatalogSearch)

	catalogListSolutionsTool := mcp.NewTool("catalog_list_solutions",
		mcp.WithDescription(fmt.Sprintf(
			"List solutions from the default catalog and local catalog with rich metadata "+
				"including description, category, tags, parameters, providers, and maintainers. "+
				"Searches remote catalog indexes for fast discovery. "+
				"Use 'catalog_search' to filter results or 'inspect_solution' for full details. "+
				"Invoke with '%s run <name>' to execute a solution.", s.name)),
		mcp.WithTitleAnnotation("Catalog List Solutions"),
		mcp.WithToolIcons(toolIcons["catalog"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("catalog",
			mcp.Description("Catalog name to list from. Omit for default catalog, use 'all' for all registered catalogs, or 'local' for local only"),
		),
	)
	s.mcpServer.AddTool(catalogListSolutionsTool, s.handleCatalogListSolutions)

	catalogListRegisteredTool := mcp.NewTool("catalog_list_registered",
		mcp.WithDescription("List all registered catalogs with their type, registry, and default status. Use this to discover which catalogs are available for search and listing."),
		mcp.WithTitleAnnotation("List Registered Catalogs"),
		mcp.WithToolIcons(toolIcons["catalog"]),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithOpenWorldHintAnnotation(false),
	)
	s.mcpServer.AddTool(catalogListRegisteredTool, s.handleCatalogListRegistered)
}

// handleCatalogSearch searches catalogs for solutions.
func (s *Server) handleCatalogSearch(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	opts := search.Options{
		Query:    request.GetString("query", ""),
		Category: request.GetString("category", ""),
		Provider: request.GetString("provider", ""),
	}
	if raw := request.GetString("tags", ""); raw != "" {
		opts.Tags = search.ParseTags(raw)
	}

	catalogFilter := request.GetString("catalog", "")

	var allResults []search.Result

	// Search local catalog when requested or when searching all.
	if catalogFilter == "" || strings.EqualFold(catalogFilter, "local") || strings.EqualFold(catalogFilter, "all") {
		localResults := s.searchLocal(opts)
		allResults = append(allResults, localResults...)
	}

	// Search remote catalogs via their indexes.
	if !strings.EqualFold(catalogFilter, "local") {
		remoteResults := s.searchRemoteCatalogs(catalogFilter, opts)
		allResults = append(allResults, remoteResults...)
	}

	// Deduplicate by name (prefer highest score).
	allResults = deduplicateResults(allResults)

	if len(allResults) == 0 {
		return newStructuredError(ErrCodeNotFound, "no solutions match the search criteria",
			WithSuggestion("Try a broader query or remove filters. Use 'catalog_list_solutions' to see all available solutions, or 'catalog_list_registered' to check which catalogs are configured."),
			WithRelatedTools("catalog_list_solutions", "catalog_list_registered"),
		), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"results": allResults,
		"count":   len(allResults),
	})
}

// handleCatalogListSolutions lists all solutions from catalogs.
func (s *Server) handleCatalogListSolutions(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	catalogFilter := request.GetString("catalog", "")

	var allEntries []search.SolutionEntry

	// Include local catalog when requested or when listing all.
	if catalogFilter == "" || strings.EqualFold(catalogFilter, "local") || strings.EqualFold(catalogFilter, "all") {
		localEntries := s.listLocal()
		allEntries = append(allEntries, localEntries...)
	}

	// Include remote catalogs via their indexes.
	if !strings.EqualFold(catalogFilter, "local") {
		remoteEntries := s.listRemoteCatalogs(catalogFilter)
		allEntries = append(allEntries, remoteEntries...)
	}

	// Deduplicate by name (prefer entry with version).
	allEntries = deduplicateEntries(allEntries)

	if len(allEntries) == 0 {
		return newStructuredError(ErrCodeNotFound, "no solutions found in any catalog",
			WithSuggestion("Use 'catalog_list_registered' to check which catalogs are configured. Pull solutions from a remote catalog or publish locally."),
			WithRelatedTools("catalog_list_registered"),
		), nil
	}

	return mcp.NewToolResultJSON(map[string]any{
		"solutions": allEntries,
		"count":     len(allEntries),
	})
}

// handleCatalogListRegistered lists all registered catalogs.
func (s *Server) handleCatalogListRegistered(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	catalogs := s.listRegisteredCatalogs()

	return mcp.NewToolResultJSON(map[string]any{
		"catalogs": catalogs,
		"count":    len(catalogs),
	})
}

// searchLocal searches the local catalog for solutions.
func (s *Server) searchLocal(opts search.Options) []search.Result {
	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		s.logger.V(1).Info("local catalog not available for search", "error", err)
		return nil
	}

	results, err := search.Catalog(s.ctx, s.logger, localCatalog, opts)
	if err != nil {
		s.logger.V(1).Info("local catalog search failed", "error", err)
		return nil
	}
	return results
}

// listLocal lists all solution entries from the local catalog.
func (s *Server) listLocal() []search.SolutionEntry {
	localCatalog, err := catalog.NewLocalCatalog(s.logger)
	if err != nil {
		s.logger.V(1).Info("local catalog not available for listing", "error", err)
		return nil
	}

	entries, err := search.ListSolutionEntries(s.ctx, s.logger, localCatalog)
	if err != nil {
		s.logger.V(1).Info("local catalog listing failed", "error", err)
		return nil
	}
	return entries
}

// searchRemoteCatalogs searches remote catalogs via their indexes.
// catalogFilter is "" (default only), "all", or a specific catalog name.
func (s *Server) searchRemoteCatalogs(catalogFilter string, opts search.Options) []search.Result {
	remoteCatalogs := s.buildRemoteCatalogs(catalogFilter)
	var allResults []search.Result
	for _, rc := range remoteCatalogs {
		results, err := search.RemoteIndex(s.ctx, s.logger, rc, opts)
		if err != nil {
			s.logger.V(1).Info("remote catalog search failed, skipping",
				"catalog", rc.Name(), "error", err)
			continue
		}
		allResults = append(allResults, results...)
	}
	return allResults
}

// listRemoteCatalogs lists solution entries from remote catalog indexes.
func (s *Server) listRemoteCatalogs(catalogFilter string) []search.SolutionEntry {
	remoteCatalogs := s.buildRemoteCatalogs(catalogFilter)
	var allEntries []search.SolutionEntry
	for _, rc := range remoteCatalogs {
		entries, err := search.ListRemoteIndex(s.ctx, s.logger, rc)
		if err != nil {
			s.logger.V(1).Info("remote catalog list failed, skipping",
				"catalog", rc.Name(), "error", err)
			continue
		}
		allEntries = append(allEntries, entries...)
	}
	return allEntries
}

// buildRemoteCatalogs creates RemoteCatalog instances for the requested scope.
// catalogFilter: "" = default catalog (or official fallback), "all" = every
// registered OCI catalog, specific name = that catalog only.
// "local" should not reach here.
func (s *Server) buildRemoteCatalogs(catalogFilter string) []*catalog.RemoteCatalog {
	cfg := s.resolveConfig()
	if cfg == nil {
		return nil
	}

	credStore, err := catalog.NewCredentialStore(s.logger)
	if err != nil {
		s.logger.V(1).Info("credential store not available for remote search", "error", err)
	}

	var targets []config.CatalogConfig

	switch {
	case catalogFilter == "":
		// Default: search only the default catalog (or official if no default is set).
		if catCfg, ok := cfg.GetDefaultCatalog(); ok && catCfg.Type == config.CatalogTypeOCI && catCfg.URL != "" {
			targets = append(targets, *catCfg)
		} else {
			// Fall back to the official catalog when no explicit default is configured.
			if catCfg, ok := cfg.GetCatalog(config.CatalogNameOfficial); ok &&
				catCfg.Type == config.CatalogTypeOCI && catCfg.URL != "" &&
				!cfg.Settings.DisableOfficialCatalog {
				targets = append(targets, *catCfg)
			}
		}
	case strings.EqualFold(catalogFilter, "all"):
		// Explicit "all": search every registered OCI catalog.
		for _, catCfg := range cfg.Catalogs {
			if catCfg.Type == config.CatalogTypeOCI && catCfg.URL != "" {
				if catCfg.Name == config.CatalogNameLocal {
					continue
				}
				if catCfg.Name == config.CatalogNameOfficial && cfg.Settings.DisableOfficialCatalog {
					continue
				}
				targets = append(targets, catCfg)
			}
		}
	default:
		// Specific catalog by name.
		if catCfg, ok := cfg.GetCatalog(catalogFilter); ok && catCfg.Type == config.CatalogTypeOCI && catCfg.URL != "" {
			targets = append(targets, *catCfg)
		}
	}

	var result []*catalog.RemoteCatalog
	for _, catCfg := range targets {
		rc, buildErr := catalog.BuildRemoteCatalogFromConfig(catCfg, credStore, s.authReg, s.logger)
		if buildErr != nil {
			s.logger.V(1).Info("failed to build remote catalog, skipping",
				"catalog", catCfg.Name, "error", buildErr)
			continue
		}
		result = append(result, rc)
	}
	return result
}

// listRegisteredCatalogs returns info about all configured catalogs.
func (s *Server) listRegisteredCatalogs() []search.CatalogInfo {
	var catalogs []search.CatalogInfo

	// Always include local.
	catalogs = append(catalogs, search.CatalogInfo{
		Name: config.CatalogNameLocal,
		Type: "filesystem",
	})

	cfg := s.resolveConfig()
	if cfg == nil {
		return catalogs
	}

	defaultCatalog := cfg.Settings.DefaultCatalog

	for _, catCfg := range cfg.Catalogs {
		if catCfg.Name == config.CatalogNameLocal {
			continue
		}
		if catCfg.Name == config.CatalogNameOfficial && cfg.Settings.DisableOfficialCatalog {
			continue
		}

		info := search.CatalogInfo{
			Name:    catCfg.Name,
			Type:    catCfg.Type,
			Default: catCfg.Name == defaultCatalog,
		}
		if catCfg.Type == config.CatalogTypeOCI && catCfg.URL != "" {
			registry, _ := catalog.ParseCatalogURL(catCfg.URL)
			info.Registry = registry
		}
		catalogs = append(catalogs, info)
	}

	sort.Slice(catalogs, func(i, j int) bool {
		return catalogs[i].Name < catalogs[j].Name
	})

	return catalogs
}

// deduplicateResults removes duplicate solution names, keeping the highest-scored entry.
func deduplicateResults(results []search.Result) []search.Result {
	// Sort by score descending first so first-seen is the best.
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Name < results[j].Name
	})

	seen := make(map[string]bool, len(results))
	deduped := make([]search.Result, 0, len(results))
	for _, r := range results {
		if seen[r.Name] {
			continue
		}
		seen[r.Name] = true
		deduped = append(deduped, r)
	}
	return deduped
}

// deduplicateEntries removes duplicate solution names, preferring entries with a version.
func deduplicateEntries(entries []search.SolutionEntry) []search.SolutionEntry {
	best := make(map[string]search.SolutionEntry, len(entries))
	for _, e := range entries {
		existing, ok := best[e.Name]
		if !ok || (e.Version != "" && existing.Version == "") {
			best[e.Name] = e
		}
	}

	result := make([]search.SolutionEntry, 0, len(best))
	for _, e := range best {
		result = append(result, e)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}
