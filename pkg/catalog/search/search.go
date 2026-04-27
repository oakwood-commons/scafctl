// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package search provides catalog search and discovery functionality.
// It supports free-text queries, filtering by category/provider/tags,
// and relevance-scored results. When an enriched index is available,
// search results are augmented with pre-extracted metadata (description,
// category, tags) without fetching individual solution YAMLs.
package search

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/solution"
)

// SolutionEntry holds rich metadata for a catalog solution, extracted by
// parsing the full solution YAML or from an enriched catalog index.
type SolutionEntry struct {
	Name        string   `json:"name"                     yaml:"name"                     doc:"Solution name" example:"cloud-run-deploy" maxLength:"255"`
	Version     string   `json:"version"                  yaml:"version"                  doc:"Latest version" example:"1.0.0"`
	Description string   `json:"description,omitempty"    yaml:"description,omitempty"    doc:"Solution description" maxLength:"5000"`
	Category    string   `json:"category,omitempty"       yaml:"category,omitempty"       doc:"Solution category" example:"deployment" maxLength:"30"`
	Tags        []string `json:"tags,omitempty"           yaml:"tags,omitempty"           doc:"Solution tags" maxItems:"100"`
	DisplayName string   `json:"displayName,omitempty"    yaml:"displayName,omitempty"    doc:"Human-friendly display name" maxLength:"80"`
	Parameters  []string `json:"parameters,omitempty"     yaml:"parameters,omitempty"     doc:"Parameter resolver names" maxItems:"50"`
	Providers   []string `json:"providers,omitempty"      yaml:"providers,omitempty"      doc:"Providers used by the solution" maxItems:"50"`
	Maintainers []string `json:"maintainers,omitempty"    yaml:"maintainers,omitempty"    doc:"Maintainer names" maxItems:"10"`
	Catalog     string   `json:"catalog,omitempty"        yaml:"catalog,omitempty"        doc:"Source catalog name" example:"local"`
}

// Result is a single match from a catalog search.
type Result struct {
	SolutionEntry
	Score float64 `json:"score" yaml:"score" doc:"Relevance score (0-1)" example:"0.95"`
}

// Options configures a catalog search.
type Options struct {
	Query    string   `json:"query,omitempty"    yaml:"query,omitempty"    doc:"Free-text search" maxLength:"200"`
	Tags     []string `json:"tags,omitempty"     yaml:"tags,omitempty"     doc:"Filter by tags (AND)" maxItems:"20"`
	Category string   `json:"category,omitempty" yaml:"category,omitempty" doc:"Filter by category" maxLength:"100"`
	Provider string   `json:"provider,omitempty" yaml:"provider,omitempty" doc:"Filter by provider" maxLength:"100"`
}

// Catalog searches the local catalog for solutions matching the given
// criteria. Results are sorted by relevance score (highest first).
func Catalog(ctx context.Context, lgr logr.Logger, lc *catalog.LocalCatalog, opts Options) ([]Result, error) {
	entries, err := ListSolutionEntries(ctx, lgr, lc)
	if err != nil {
		return nil, err
	}

	var results []Result
	for _, entry := range entries {
		score := matchEntry(entry, opts)
		if score > 0 {
			results = append(results, Result{
				SolutionEntry: entry,
				Score:         score,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Name < results[j].Name
	})

	return results, nil
}

// ListSolutionEntries fetches all solutions from the local catalog and
// extracts rich metadata from each one.
func ListSolutionEntries(ctx context.Context, lgr logr.Logger, lc *catalog.LocalCatalog) ([]SolutionEntry, error) {
	items, err := lc.List(ctx, catalog.ArtifactKindSolution, "")
	if err != nil {
		return nil, fmt.Errorf("listing catalog solutions: %w", err)
	}

	if len(items) == 0 {
		return nil, nil
	}

	latest := latestByName(items)

	var entries []SolutionEntry
	for _, info := range latest {
		content, _, err := lc.Fetch(ctx, info.Reference)
		if err != nil {
			lgr.V(1).Info("skipping solution: fetch failed", "name", info.Reference.Name, "error", err)
			continue
		}

		sol := &solution.Solution{}
		if err := sol.FromYAML(content); err != nil {
			lgr.V(1).Info("skipping solution: parse failed", "name", info.Reference.Name, "error", err)
			continue
		}

		entry := extractEntry(sol, info)
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

// EnrichArtifacts populates Description, DisplayName, Category, and Tags on
// solution-kind DiscoveredArtifacts by fetching and parsing each solution YAML
// from the remote catalog. Non-solution artifacts and fetch/parse failures are
// silently skipped (logged at V(1)).
//
// When existingIndex is non-nil, artifacts whose name and LatestVersion match
// an entry in the existing index reuse the cached metadata instead of fetching
// the full solution YAML. This avoids expensive fetches for unchanged artifacts.
func EnrichArtifacts(ctx context.Context, lgr logr.Logger, rc catalog.Catalog, artifacts, existingIndex []catalog.DiscoveredArtifact) {
	indexMap := buildIndexMap(existingIndex)

	seen := make(map[string]bool)
	for i := range artifacts {
		a := &artifacts[i]

		if a.Kind != catalog.ArtifactKindSolution {
			continue
		}

		// Skip duplicates (same name already enriched).
		if seen[a.Name] {
			continue
		}
		seen[a.Name] = true

		// Reuse cached metadata from the existing index when the version matches.
		if cached, ok := indexMap[a.Name]; ok && cached.LatestVersion == a.LatestVersion {
			copyEnrichedMetadata(a, cached)
			lgr.V(1).Info("reused cached metadata", "name", a.Name, "version", a.LatestVersion)
			continue
		}

		ref := catalog.Reference{
			Kind: catalog.ArtifactKindSolution,
			Name: a.Name,
		}

		// Pin to the discovered latest version so we enrich the correct release.
		if a.LatestVersion != "" {
			if v, err := semver.NewVersion(a.LatestVersion); err == nil {
				ref.Version = v
			}
		}

		content, _, err := rc.Fetch(ctx, ref)
		if err != nil {
			lgr.V(1).Info("skipping enrichment: fetch failed", "name", a.Name, "error", err)
			continue
		}

		sol := &solution.Solution{}
		if err := sol.FromYAML(content); err != nil {
			lgr.V(1).Info("skipping enrichment: parse failed", "name", a.Name, "error", err)
			continue
		}

		a.Description = sol.Metadata.Description
		a.DisplayName = sol.Metadata.DisplayName
		a.Category = sol.Metadata.Category
		a.Tags = sol.Metadata.Tags

		// Extended metadata for MCP and rich discovery.
		for _, m := range sol.Metadata.Maintainers {
			if m.Name != "" {
				a.Maintainers = append(a.Maintainers, m.Name)
			}
		}
		for _, l := range sol.Metadata.Links {
			if l.Name != "" && l.URL != "" {
				a.Links = append(a.Links, catalog.DiscoveredLink{
					Name: l.Name, URL: l.URL,
				})
			}
		}
		if sol.Spec.HasResolvers() {
			a.Parameters = extractParameterNames(sol)
			a.Providers = extractProviderNames(sol)
		}
	}
}

// extractEntry builds a SolutionEntry from a parsed solution and its catalog info.
func extractEntry(sol *solution.Solution, info catalog.ArtifactInfo) SolutionEntry {
	entry := SolutionEntry{
		Name:        info.Reference.Name,
		Description: sol.Metadata.Description,
		DisplayName: sol.Metadata.DisplayName,
		Category:    sol.Metadata.Category,
		Tags:        sol.Metadata.Tags,
		Catalog:     info.Catalog,
	}

	if info.Reference.Version != nil {
		entry.Version = info.Reference.Version.String()
	}

	for _, m := range sol.Metadata.Maintainers {
		if m.Name != "" {
			entry.Maintainers = append(entry.Maintainers, m.Name)
		}
	}

	if sol.Spec.HasResolvers() {
		entry.Parameters = extractParameterNames(sol)
		entry.Providers = extractProviderNames(sol)
	}

	return entry
}

// extractParameterNames returns resolver names that use the parameter provider.
func extractParameterNames(sol *solution.Solution) []string {
	var names []string
	for name, r := range sol.Spec.Resolvers {
		if r == nil || r.Resolve == nil || len(r.Resolve.With) == 0 {
			continue
		}
		if r.Resolve.With[0].Provider == "parameter" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// extractProviderNames returns unique non-trivial provider names used by the
// solution's resolvers (excludes "static" and "parameter").
func extractProviderNames(sol *solution.Solution) []string {
	seen := make(map[string]bool)
	for _, r := range sol.Spec.Resolvers {
		if r == nil || r.Resolve == nil {
			continue
		}
		for _, src := range r.Resolve.With {
			if src.Provider != "" && src.Provider != "static" && src.Provider != "parameter" {
				seen[src.Provider] = true
			}
		}
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Relevance score constants for query matching.
const (
	scoreExactName      = 1.0
	scoreNameContains   = 0.9
	scoreDisplayName    = 0.85
	scoreTagExact       = 0.8
	scoreCategory       = 0.75
	scoreDescription    = 0.6
	scoreTagPartial     = 0.55
	scoreProvider       = 0.5
	scoreMultiWord      = 0.4
	scoreNoQuery        = 0.5
	scoreHardFilterOnly = 0.8
)

// matchEntry scores a solution entry against search options.
// Returns 0 if the entry does not match, otherwise a score in (0, 1].
func matchEntry(entry SolutionEntry, opts Options) float64 {
	if opts.Query == "" && len(opts.Tags) == 0 && opts.Category == "" && opts.Provider == "" {
		return scoreNoQuery
	}

	// Apply hard filters first (tags, category, provider).
	if opts.Category != "" && !strings.EqualFold(entry.Category, opts.Category) {
		return 0
	}

	if opts.Provider != "" {
		found := false
		for _, p := range entry.Providers {
			if strings.EqualFold(p, opts.Provider) {
				found = true
				break
			}
		}
		if !found {
			return 0
		}
	}

	if len(opts.Tags) > 0 {
		entryTags := make(map[string]bool, len(entry.Tags))
		for _, t := range entry.Tags {
			entryTags[strings.ToLower(t)] = true
		}
		for _, required := range opts.Tags {
			if !entryTags[strings.ToLower(required)] {
				return 0
			}
		}
	}

	if opts.Query == "" {
		return scoreHardFilterOnly
	}

	return scoreQuery(entry, strings.ToLower(opts.Query))
}

// scoreQuery computes a relevance score for a free-text query against solution fields.
func scoreQuery(entry SolutionEntry, query string) float64 {
	var score float64

	nameLower := strings.ToLower(entry.Name)
	descLower := strings.ToLower(entry.Description)
	displayLower := strings.ToLower(entry.DisplayName)
	catLower := strings.ToLower(entry.Category)

	if nameLower == query {
		score = max(score, scoreExactName)
	}

	if strings.Contains(nameLower, query) {
		score = max(score, scoreNameContains)
	}

	if displayLower != "" && strings.Contains(displayLower, query) {
		score = max(score, scoreDisplayName)
	}

	for _, t := range entry.Tags {
		if strings.EqualFold(t, query) {
			score = max(score, scoreTagExact)
			break
		}
	}

	if catLower != "" && strings.Contains(catLower, query) {
		score = max(score, scoreCategory)
	}

	if descLower != "" && strings.Contains(descLower, query) {
		score = max(score, scoreDescription)
	}

	for _, t := range entry.Tags {
		if strings.Contains(strings.ToLower(t), query) {
			score = max(score, scoreTagPartial)
			break
		}
	}

	for _, p := range entry.Providers {
		if strings.Contains(strings.ToLower(p), query) {
			score = max(score, scoreProvider)
			break
		}
	}

	// Multi-word: check if all words appear somewhere.
	words := strings.Fields(query)
	if len(words) > 1 && score == 0 {
		searchable := strings.Join([]string{
			nameLower, descLower, displayLower, catLower,
			strings.Join(entry.Tags, " "),
			strings.Join(entry.Providers, " "),
		}, " ")

		allFound := true
		for _, w := range words {
			if !strings.Contains(searchable, strings.ToLower(w)) {
				allFound = false
				break
			}
		}
		if allFound {
			score = scoreMultiWord
		}
	}

	return score
}

// ParseTags splits a comma-separated tag string into a trimmed slice.
func ParseTags(raw string) []string {
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// latestByName deduplicates artifacts keeping only the highest version of each name.
func latestByName(items []catalog.ArtifactInfo) []catalog.ArtifactInfo {
	best := make(map[string]catalog.ArtifactInfo)
	for _, item := range items {
		name := item.Reference.Name
		existing, ok := best[name]
		if !ok {
			best[name] = item
			continue
		}
		// A versioned item always beats an unversioned one.
		if existing.Reference.Version == nil && item.Reference.Version != nil {
			best[name] = item
		} else if item.Reference.Version != nil && existing.Reference.Version != nil {
			if item.Reference.Version.GreaterThan(existing.Reference.Version) {
				best[name] = item
			}
		}
	}

	result := make([]catalog.ArtifactInfo, 0, len(best))
	for _, v := range best {
		result = append(result, v)
	}
	return result
}

// buildIndexMap creates a name-keyed lookup from a slice of DiscoveredArtifacts.
// Only solution-kind entries are included.
func buildIndexMap(index []catalog.DiscoveredArtifact) map[string]catalog.DiscoveredArtifact {
	m := make(map[string]catalog.DiscoveredArtifact, len(index))
	for _, a := range index {
		if a.Kind == catalog.ArtifactKindSolution {
			m[a.Name] = a
		}
	}
	return m
}

// copyEnrichedMetadata copies enriched metadata fields from a cached index
// entry to a target artifact, avoiding a full fetch+parse cycle.
func copyEnrichedMetadata(dst *catalog.DiscoveredArtifact, src catalog.DiscoveredArtifact) {
	dst.Description = src.Description
	dst.DisplayName = src.DisplayName
	dst.Category = src.Category
	dst.Tags = src.Tags
	dst.Maintainers = src.Maintainers
	dst.Links = src.Links
	dst.Providers = src.Providers
	dst.Parameters = src.Parameters
}
