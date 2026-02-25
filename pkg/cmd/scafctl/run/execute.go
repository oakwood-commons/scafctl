// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package run provides the CLI run commands.
// Business logic has been extracted to pkg/solution/execute for reuse across
// CLI, MCP, and future API layers.
package run

import (
	"context"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	execute "github.com/oakwood-commons/scafctl/pkg/solution/execute"
)

// Type aliases re-exporting from pkg/solution/execute for backward compatibility.
// Callers that import this package continue to work without modification.
type (
	SolutionValidationResult = execute.SolutionValidationResult
	ResolverExecutionConfig  = execute.ResolverExecutionConfig
	ResolverExecutionResult  = execute.ResolverExecutionResult
)

// ValidateSolution delegates to pkg/solution/execute.ValidateSolution.
func ValidateSolution(ctx context.Context, sol *solution.Solution, reg *provider.Registry) *SolutionValidationResult {
	return execute.ValidateSolution(ctx, sol, reg)
}

// ExecuteResolvers delegates to pkg/solution/execute.Resolvers.
func ExecuteResolvers(
	ctx context.Context,
	sol *solution.Solution,
	params map[string]any,
	reg *provider.Registry,
	cfg ResolverExecutionConfig,
) (*ResolverExecutionResult, error) {
	return execute.Resolvers(ctx, sol, params, reg, cfg)
}

// ResolverExecutionConfigFromContext delegates to pkg/solution/execute.ResolverExecutionConfigFromContext.
func ResolverExecutionConfigFromContext(ctx context.Context) ResolverExecutionConfig {
	return execute.ResolverExecutionConfigFromContext(ctx)
}
