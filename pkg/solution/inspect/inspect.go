// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package inspect provides business logic for inspecting and explaining solutions.
// This package is the shared domain layer used by CLI, MCP, and future API consumers.
package inspect

import (
	"context"
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/provider/builtin"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
	"github.com/oakwood-commons/scafctl/pkg/sourcepos"
)

// SolutionExplanation holds structured explanation data for a solution.
// This can be serialized to JSON/YAML or formatted for terminal output.
type SolutionExplanation struct {
	Name        string `json:"name" yaml:"name" doc:"Solution name"`
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" doc:"Human-readable display name"`
	Version     string `json:"version" yaml:"version" doc:"Solution version"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" doc:"Solution description"`
	Category    string `json:"category,omitempty" yaml:"category,omitempty" doc:"Solution category"`
	Path        string `json:"path,omitempty" yaml:"path,omitempty" doc:"Source file path"`

	Catalog *CatalogInfo `json:"catalog,omitempty" yaml:"catalog,omitempty" doc:"Catalog visibility info"`

	Resolvers []ResolverInfo `json:"resolvers,omitempty" yaml:"resolvers,omitempty" doc:"Resolver configurations"`
	Actions   []ActionInfo   `json:"actions,omitempty" yaml:"actions,omitempty" doc:"Action configurations"`
	Finally   []ActionInfo   `json:"finally,omitempty" yaml:"finally,omitempty" doc:"Finally/cleanup actions"`

	Tags        []string         `json:"tags,omitempty" yaml:"tags,omitempty" doc:"Solution tags"`
	Links       []LinkInfo       `json:"links,omitempty" yaml:"links,omitempty" doc:"Related links"`
	Maintainers []MaintainerInfo `json:"maintainers,omitempty" yaml:"maintainers,omitempty" doc:"Solution maintainers"`
}

// CatalogInfo holds catalog metadata.
type CatalogInfo struct {
	Visibility string `json:"visibility,omitempty" yaml:"visibility,omitempty" doc:"Catalog visibility level"`
	Beta       bool   `json:"beta,omitempty" yaml:"beta,omitempty" doc:"Whether the solution is in beta"`
	Disabled   bool   `json:"disabled,omitempty" yaml:"disabled,omitempty" doc:"Whether the solution is disabled"`
}

// ResolverInfo holds structured information about a resolver.
type ResolverInfo struct {
	Name        string              `json:"name" yaml:"name" doc:"Resolver name"`
	Providers   []string            `json:"providers,omitempty" yaml:"providers,omitempty" doc:"Provider names used"`
	DependsOn   []string            `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty" doc:"Resolver dependencies"`
	Conditional bool                `json:"conditional,omitempty" yaml:"conditional,omitempty" doc:"Whether resolver has a when condition"`
	Phases      []string            `json:"phases,omitempty" yaml:"phases,omitempty" doc:"Configured phases (resolve, transform, validate)"`
	SourcePos   *sourcepos.Position `json:"sourcePos,omitempty" yaml:"sourcePos,omitempty" doc:"Source file location"`
}

// ActionInfo holds structured information about an action.
type ActionInfo struct {
	Name        string              `json:"name" yaml:"name" doc:"Action name"`
	Provider    string              `json:"provider" yaml:"provider" doc:"Provider used by this action"`
	DependsOn   []string            `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty" doc:"Action dependencies"`
	Conditional bool                `json:"conditional,omitempty" yaml:"conditional,omitempty" doc:"Whether action has a when condition"`
	HasRetry    bool                `json:"hasRetry,omitempty" yaml:"hasRetry,omitempty" doc:"Whether retry is configured"`
	HasForEach  bool                `json:"hasForEach,omitempty" yaml:"hasForEach,omitempty" doc:"Whether forEach is configured"`
	SourcePos   *sourcepos.Position `json:"sourcePos,omitempty" yaml:"sourcePos,omitempty" doc:"Source file location"`
}

// LinkInfo holds a named link.
type LinkInfo struct {
	Name string `json:"name" yaml:"name" doc:"Link display name"`
	URL  string `json:"url" yaml:"url" doc:"Link URL"`
}

// MaintainerInfo holds maintainer contact info.
type MaintainerInfo struct {
	Name  string `json:"name" yaml:"name" doc:"Maintainer name"`
	Email string `json:"email,omitempty" yaml:"email,omitempty" doc:"Maintainer email"`
}

// LoadSolution loads a solution from a path using the standard loader with
// catalog resolution. This function is reusable by CLI, MCP, and future API.
func LoadSolution(ctx context.Context, path string) (*solution.Solution, error) {
	lgr := logger.FromContext(ctx)

	var getterOpts []get.Option
	localCatalog, err := catalog.NewLocalCatalog(*lgr)
	if err == nil {
		catResolver := catalog.NewSolutionResolver(localCatalog, *lgr)
		getterOpts = append(getterOpts, get.WithCatalogResolver(catResolver))
	} else {
		lgr.V(1).Info("catalog not available for solution resolution", "error", err)
	}

	getter := get.NewGetter(getterOpts...)
	sol, err := getter.Get(ctx, path)
	if err != nil {
		return nil, exitcode.WithCode(
			fmt.Errorf("failed to load solution: %w", err),
			exitcode.FileNotFound,
		)
	}

	return sol, nil
}

// BuildSolutionExplanation builds a structured explanation from a loaded solution.
// This returns data that can be serialized (JSON/YAML) or formatted for display.
func BuildSolutionExplanation(sol *solution.Solution) *SolutionExplanation {
	exp := &SolutionExplanation{
		Name: sol.Metadata.Name,
		Path: sol.GetPath(),
	}

	if sol.Metadata.DisplayName != "" {
		exp.DisplayName = sol.Metadata.DisplayName
	}
	if sol.Metadata.Version != nil {
		exp.Version = sol.Metadata.Version.String()
	} else {
		exp.Version = "unknown"
	}
	if sol.Metadata.Description != "" {
		exp.Description = sol.Metadata.Description
	}
	if sol.Metadata.Category != "" {
		exp.Category = sol.Metadata.Category
	}

	// Catalog info
	if sol.Catalog.Visibility != "" || sol.Catalog.Beta || sol.Catalog.Disabled {
		exp.Catalog = &CatalogInfo{
			Visibility: string(sol.Catalog.Visibility),
			Beta:       sol.Catalog.Beta,
			Disabled:   sol.Catalog.Disabled,
		}
	}

	// Resolvers
	sm := sol.SourceMap()
	if sol.Spec.HasResolvers() {
		exp.Resolvers = buildResolverInfos(sol, sm)
	}

	// Actions
	if sol.Spec.HasActions() && sol.Spec.Workflow != nil {
		exp.Actions = buildActionInfos(sol.Spec.Workflow.Actions, sm, "spec.workflow.actions")
		exp.Finally = buildActionInfos(sol.Spec.Workflow.Finally, sm, "spec.workflow.finally")
	}

	// Tags
	if len(sol.Metadata.Tags) > 0 {
		exp.Tags = sol.Metadata.Tags
	}

	// Links
	for _, link := range sol.Metadata.Links {
		exp.Links = append(exp.Links, LinkInfo{
			Name: link.Name,
			URL:  link.URL,
		})
	}

	// Maintainers
	for _, m := range sol.Metadata.Maintainers {
		exp.Maintainers = append(exp.Maintainers, MaintainerInfo{
			Name:  m.Name,
			Email: m.Email,
		})
	}

	return exp
}

// LookupProvider looks up a provider by name and returns its descriptor.
// This is a standalone function reusable by CLI, MCP, and future API.
func LookupProvider(ctx context.Context, name string, reg *provider.Registry) (*provider.Descriptor, error) {
	if reg == nil {
		var err error
		reg, err = builtin.DefaultRegistry(ctx)
		if err != nil {
			reg = provider.GetGlobalRegistry()
		}
	}

	p, ok := reg.Get(name)
	if !ok {
		return nil, exitcode.WithCode(
			fmt.Errorf("provider %q not found", name),
			exitcode.FileNotFound,
		)
	}

	return p.Descriptor(), nil
}

// buildResolverInfos extracts structured resolver information from a solution.
func buildResolverInfos(sol *solution.Solution, sm *sourcepos.SourceMap) []ResolverInfo {
	names := make([]string, 0, len(sol.Spec.Resolvers))
	for name := range sol.Spec.Resolvers {
		names = append(names, name)
	}
	sort.Strings(names)

	infos := make([]ResolverInfo, 0, len(names))
	for _, name := range names {
		r := sol.Spec.Resolvers[name]
		if r == nil {
			continue
		}

		info := ResolverInfo{
			Name:        name,
			Providers:   extractProviderNames(r),
			DependsOn:   r.DependsOn,
			Conditional: r.When != nil,
			Phases:      extractPhases(r),
		}

		// Enrich with source position
		if sm != nil {
			if pos, ok := sm.Get("spec.resolvers." + name); ok {
				info.SourcePos = &pos
			}
		}

		infos = append(infos, info)
	}

	return infos
}

// buildActionInfos extracts structured action information.
func buildActionInfos(actions map[string]*action.Action, sm *sourcepos.SourceMap, basePath string) []ActionInfo {
	if len(actions) == 0 {
		return nil
	}

	names := make([]string, 0, len(actions))
	for name := range actions {
		names = append(names, name)
	}
	sort.Strings(names)

	infos := make([]ActionInfo, 0, len(names))
	for _, name := range names {
		act := actions[name]
		if act == nil {
			continue
		}

		prov := act.Provider
		if prov == "" {
			prov = "unknown"
		}

		info := ActionInfo{
			Name:        name,
			Provider:    prov,
			DependsOn:   act.DependsOn,
			Conditional: act.When != nil,
			HasRetry:    act.Retry != nil,
			HasForEach:  act.ForEach != nil,
		}

		// Enrich with source position
		if sm != nil {
			if pos, ok := sm.Get(basePath + "." + name); ok {
				info.SourcePos = &pos
			}
		}

		infos = append(infos, info)
	}

	return infos
}

// extractProviderNames returns sorted unique provider names from a resolver.
func extractProviderNames(r *resolver.Resolver) []string {
	providers := make(map[string]bool)

	if r.Resolve != nil {
		for _, source := range r.Resolve.With {
			if source.Provider != "" {
				providers[source.Provider] = true
			}
		}
	}

	if r.Transform != nil {
		for _, transform := range r.Transform.With {
			if transform.Provider != "" {
				providers[transform.Provider] = true
			}
		}
	}

	if r.Validate != nil {
		for _, validation := range r.Validate.With {
			if validation.Provider != "" {
				providers[validation.Provider] = true
			}
		}
	}

	result := make([]string, 0, len(providers))
	for p := range providers {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

// extractPhases returns which phases are configured on a resolver.
func extractPhases(r *resolver.Resolver) []string {
	var phases []string
	if r.Resolve != nil && len(r.Resolve.With) > 0 {
		phases = append(phases, "resolve")
	}
	if r.Transform != nil && len(r.Transform.With) > 0 {
		phases = append(phases, "transform")
	}
	if r.Validate != nil && len(r.Validate.With) > 0 {
		phases = append(phases, "validate")
	}
	return phases
}
