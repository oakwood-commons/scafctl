// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package explain provides the CLI explain commands.
// Business logic has been extracted to pkg/solution/inspect for reuse across
// CLI, MCP, and future API layers.
package explain

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/inspect"
)

// Type aliases re-exporting from pkg/solution/inspect for backward compatibility.
// Callers that import this package continue to work without modification.
type (
	SolutionExplanation = inspect.SolutionExplanation
	CatalogInfo         = inspect.CatalogInfo
	ResolverInfo        = inspect.ResolverInfo
	ActionInfo          = inspect.ActionInfo
	LinkInfo            = inspect.LinkInfo
	MaintainerInfo      = inspect.MaintainerInfo
)

// LoadSolution delegates to pkg/solution/inspect.LoadSolution.
func LoadSolution(ctx context.Context, path string) (*solution.Solution, error) {
	return inspect.LoadSolution(ctx, path)
}

// BuildSolutionExplanation delegates to pkg/solution/inspect.BuildSolutionExplanation.
func BuildSolutionExplanation(sol *solution.Solution) *SolutionExplanation {
	return inspect.BuildSolutionExplanation(sol)
}

// LookupProvider delegates to pkg/solution/inspect.LookupProvider.
func LookupProvider(ctx context.Context, name string, reg *provider.Registry) (*provider.Descriptor, error) {
	return inspect.LookupProvider(ctx, name, reg)
}
