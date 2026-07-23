// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package validate provides the domain orchestration behind the 'validate'
// command group. It is a leaf package: it depends on pkg/lint and the solution
// loader, but nothing in those packages depends on it, avoiding an import
// cycle (pkg/lint already imports pkg/schema, so this helper cannot live in
// pkg/schema).
package validate

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/lint"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/solution/get"
)

// SolutionResult captures the outcome of validating a solution. It bundles the
// resolved file path with the lint result so callers can render findings and
// decide pass/fail via Passed.
type SolutionResult struct {
	// File is the resolved solution file path that was validated.
	File string
	// Lint is the lint result produced by lint.Solution.
	Lint *lint.Result
}

// Passed reports whether validation passed. A solution passes when it has no
// lint errors. When strict is true, warnings are also treated as fatal, so any
// warning causes Passed to return false.
func (r *SolutionResult) Passed(strict bool) bool {
	if r == nil || r.Lint == nil {
		return false
	}
	if r.Lint.ErrorCount > 0 {
		return false
	}
	if strict && r.Lint.WarnCount > 0 {
		return false
	}
	return true
}

// Solution loads a solution and runs lint against it, returning a
// SolutionResult. It reuses the same resolution chain as the lint command
// (explicit file > positional arg > auto-discovery) so behavior matches
// 'scafctl lint'. Schema conformance is checked by lint.Solution itself, so
// callers must NOT additionally invoke the standalone schema validator.
//
// The registry is used for provider-aware lint rules; when nil, lint falls
// back to a default registry.
func Solution(ctx context.Context, filePath string, registry *provider.Registry) (*SolutionResult, error) {
	getter := newGetter(ctx)

	resolvedPath, err := get.Resolve(ctx, getter, filePath, "", get.ResolveOptions{
		Risk: get.DiscoveryRiskLow,
	})
	if err != nil {
		return nil, fmt.Errorf("resolving solution path: %w", err)
	}

	sol, err := getter.Get(ctx, resolvedPath)
	if err != nil {
		return nil, fmt.Errorf("loading solution: %w", err)
	}

	return LoadedSolution(sol, resolvedPath, registry), nil
}

// LoadedSolution runs lint against an already-loaded solution. It is
// the pure orchestration seam used by Solution and is convenient for
// tests that construct a solution in memory.
func LoadedSolution(sol *solution.Solution, filePath string, registry *provider.Registry) *SolutionResult {
	result := lint.Solution(sol, filePath, registry)
	return &SolutionResult{
		File: filePath,
		Lint: result,
	}
}

// newGetter builds a solution getter wired with the local (and any remote)
// catalog resolver so bare catalog names resolve, mirroring the lint command.
// It loads leniently so structural problems (e.g. an undefined dependsOn
// target) surface as lint findings rather than aborting the load; the error
// severity of those findings still drives a non-zero validation result.
func newGetter(ctx context.Context) *get.Getter {
	lgr := logger.FromContext(ctx)

	getterOpts := []get.Option{get.WithLenientValidation()}
	if localCatalog, err := catalog.NewLocalCatalog(*lgr); err == nil {
		resolver := catalog.NewSolutionResolver(localCatalog, *lgr,
			catalog.WithResolverRemoteCatalogs(catalog.RemoteCatalogsFromContext(ctx, *lgr)),
		)
		getterOpts = append(getterOpts, get.WithCatalogResolver(resolver))
	} else {
		lgr.V(1).Info("catalog not available for solution resolution", "error", err)
	}

	return get.NewGetterFromContext(ctx, getterOpts...)
}
