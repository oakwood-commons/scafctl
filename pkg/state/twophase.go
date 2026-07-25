// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/spec"
)

// ResolverRunner executes a subset of resolvers and returns the resolver context
// holding their results. It is injected into LoadTwoPhase so the CLI and render
// code paths can supply their own execution wiring (they build executors
// differently). It is only invoked when the pre-load set is non-empty.
type ResolverRunner func(ctx context.Context, subset []*resolver.Resolver) (*resolver.Context, error)

// TwoPhaseInput bundles the resolver-graph metadata and execution callback that
// the two-phase state pre-load needs.
type TwoPhaseInput struct {
	// Resolvers is the full set of solution resolvers.
	Resolvers []*resolver.Resolver

	// Lookup resolves provider descriptors for dependency extraction. May be nil.
	Lookup resolver.DescriptorLookup

	// Calls are the solution's reusable call definitions. May be nil.
	Calls map[string]*spec.Call

	// StateProviderName is the name of the state read provider. A resolver using
	// it in any phase is state-dependent. Defaults to ReadProviderName when empty,
	// so CLI callers need not set it; it is exposed mainly for tests.
	StateProviderName string

	// RunResolvers executes the given subset and returns the resolver context
	// holding their results. Required only when a load-time state field
	// references a resolver.
	RunResolvers ResolverRunner
}

// TwoPhaseResult is the outcome of LoadTwoPhase: the standard load result plus
// the Phase-A resolver results to seed into the main run.
type TwoPhaseResult struct {
	// LoadResult is the standard load outcome (context, data, merged params, skip).
	*LoadResult

	// Seed maps Phase-A resolver names to the results they produced during the
	// pre-load. Pass to the main resolver execution via resolver.WithSeededResults
	// so those resolvers are reused rather than re-executed. Empty when no
	// pre-load was needed.
	Seed map[string]*resolver.ExecutionResult
}

// LoadTwoPhase executes the state pre-load, supporting state config fields
// (enabled and backend inputs) that reference state-independent resolvers.
//
// It:
//  1. Validates that no load-time field references a state-dependent resolver
//     (returns *CycleError) or a non-existent one (*UnknownStateRefError).
//  2. Runs the minimal set of resolvers those fields transitively require
//     (Phase A), via the injected RunResolvers callback.
//  3. Loads state with the Phase-A values exposed as _.
//
// Phase-A results are returned in Seed so the caller can avoid re-running them
// in the main pass. When no load-time field references any resolver, LoadTwoPhase
// is exactly equivalent to Load (no resolvers run, empty Seed), so the common
// case pays no extra cost.
//
// Phase A runs with the ORIGINAL CLI params (the ones passed here), before state
// is loaded and merged. A seeded resolver therefore reflects the CLI parameter
// values, not the replayed (saved) values that a non-seeded resolver would see
// via MergedParams. This is intentional: a resolver that locates or gates state
// must resolve before that state -- and its saved params -- exist, so its value
// is fixed from the CLI inputs to keep the loaded state consistent.
func (m *Manager) LoadTwoPhase(ctx context.Context, params map[string]any, command CommandInfo, in TwoPhaseInput) (*TwoPhaseResult, error) {
	if m.config == nil {
		res, err := m.load(ctx, nil, params, command)
		if err != nil {
			return nil, err
		}
		return &TwoPhaseResult{LoadResult: res}, nil
	}

	stateProviderName := in.StateProviderName
	if stateProviderName == "" {
		stateProviderName = ReadProviderName
	}
	part := BuildPartition(in.Resolvers, in.Lookup, in.Calls, stateProviderName)

	// Fail fast on circular / unknown references before running anything.
	if err := ValidateStateRefs(m.config, part); err != nil {
		return nil, err
	}

	roots := PhaseARoots(m.config, part)
	if len(roots) == 0 {
		// No resolver references at load time: identical to single-phase Load.
		res, err := m.load(ctx, nil, params, command)
		if err != nil {
			return nil, err
		}
		return &TwoPhaseResult{LoadResult: res}, nil
	}

	// Minimal Phase A: the roots plus everything they transitively depend on.
	// The acyclic guarantee (enforced above) means this closure is entirely
	// state-independent, so it can run before state is loaded.
	phaseASet := resolver.TransitiveDependencies(in.Resolvers, in.Lookup, in.Calls, roots)
	subset := filterResolvers(in.Resolvers, phaseASet)

	if in.RunResolvers == nil {
		return nil, fmt.Errorf("state: two-phase load requires a resolver runner but none was provided")
	}

	resolverCtx, err := in.RunResolvers(ctx, subset)
	if err != nil {
		return nil, fmt.Errorf("state: pre-load resolvers: %w", err)
	}
	if resolverCtx == nil {
		return nil, fmt.Errorf("state: pre-load resolvers returned no context")
	}

	resolverData := resolverCtx.ToMap()
	seed := collectSeed(resolverCtx, phaseASet)

	res, err := m.load(ctx, resolverData, params, command)
	if err != nil {
		return nil, err
	}
	return &TwoPhaseResult{LoadResult: res, Seed: seed}, nil
}

// filterResolvers returns the resolvers whose names are in the include set,
// preserving the original order.
func filterResolvers(resolvers []*resolver.Resolver, include map[string]bool) []*resolver.Resolver {
	subset := make([]*resolver.Resolver, 0, len(include))
	for _, r := range resolvers {
		if r != nil && include[r.Name] {
			subset = append(subset, r)
		}
	}
	return subset
}

// collectSeed extracts the ExecutionResult for each Phase-A resolver from the
// resolver context so the main run can seed (and thus skip) them.
func collectSeed(resolverCtx *resolver.Context, phaseASet map[string]bool) map[string]*resolver.ExecutionResult {
	seed := make(map[string]*resolver.ExecutionResult, len(phaseASet))
	for name := range phaseASet {
		if res, ok := resolverCtx.GetResult(name); ok {
			seed[name] = res
		}
	}
	return seed
}
