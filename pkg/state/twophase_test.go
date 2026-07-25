// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package state

import (
	"context"
	"errors"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func twoPhaseCommand() CommandInfo {
	return CommandInfo{Subcommand: "run solution", Parameters: map[string]string{}}
}

// seedRunner returns a ResolverRunner that records the subset it was asked to
// run and produces a context seeded with the given resolver values.
func seedRunner(captured *[]*resolver.Resolver, values map[string]any) ResolverRunner {
	return func(_ context.Context, subset []*resolver.Resolver) (*resolver.Context, error) {
		if captured != nil {
			*captured = subset
		}
		rctx := resolver.NewContext()
		for name, val := range values {
			rctx.SetResult(name, &resolver.ExecutionResult{Value: val, Status: resolver.ExecutionStatusSuccess})
		}
		return rctx, nil
	}
}

func newTwoPhaseManager(t *testing.T, cfg *Config) *Manager {
	t.Helper()
	reg := newTestRegistry(t, &mockBackendProvider{})
	return NewManager(cfg, reg, settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})
}

func TestLoadTwoPhase_NilConfig(t *testing.T) {
	mgr := NewManager(nil, provider.NewRegistry(), settings.RuntimeProvenance{EngineName: "scafctl", EngineVersion: "test-version"})

	res, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Skipped)
	assert.Empty(t, res.Seed)
}

func TestLoadTwoPhase_NoRootsIdenticalToLoad(t *testing.T) {
	cfg := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("s.json")}},
	}
	mgr := newTwoPhaseManager(t, cfg)

	runnerCalled := false
	res, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{
		Resolvers: []*resolver.Resolver{staticResolver("unused")},
		RunResolvers: func(context.Context, []*resolver.Resolver) (*resolver.Context, error) {
			runnerCalled = true
			return resolver.NewContext(), nil
		},
	})
	require.NoError(t, err)
	assert.False(t, res.Skipped)
	assert.Empty(t, res.Seed)
	assert.False(t, runnerCalled, "runner must not be called when no load-time field references a resolver")
}

func TestLoadTwoPhase_RunsPhaseAAndSeeds(t *testing.T) {
	cfg := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": rslvrRef("app_name")}},
	}
	mgr := newTwoPhaseManager(t, cfg)

	var captured []*resolver.Resolver
	res, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{
		Resolvers:    []*resolver.Resolver{staticResolver("app_name"), staticResolver("unrelated")},
		RunResolvers: seedRunner(&captured, map[string]any{"app_name": "my-app.json"}),
	})
	require.NoError(t, err)
	assert.False(t, res.Skipped)

	// Only the referenced root is in the pre-load subset (not "unrelated").
	require.Len(t, captured, 1)
	assert.Equal(t, "app_name", captured[0].Name)

	// Seed carries the Phase-A result for reuse in the main run.
	require.Contains(t, res.Seed, "app_name")
	assert.Equal(t, "my-app.json", res.Seed["app_name"].Value)
}

func TestLoadTwoPhase_IncludesTransitiveDependencies(t *testing.T) {
	// app_name depends on prefix via a CEL ref; both must be in Phase A.
	appName := &resolver.Resolver{
		Name:    "app_name",
		Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel", Inputs: map[string]*resolver.ValueRef{"expr": {Expr: exprPtr("_.prefix + '.json'")}}}}},
	}
	cfg := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": rslvrRef("app_name")}},
	}
	mgr := newTwoPhaseManager(t, cfg)

	var captured []*resolver.Resolver
	res, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{
		Resolvers:    []*resolver.Resolver{staticResolver("prefix"), appName},
		RunResolvers: seedRunner(&captured, map[string]any{"prefix": "app", "app_name": "app.json"}),
	})
	require.NoError(t, err)

	names := map[string]bool{}
	for _, r := range captured {
		names[r.Name] = true
	}
	assert.True(t, names["app_name"], "root must be in Phase A subset")
	assert.True(t, names["prefix"], "transitive dependency must be in Phase A subset")

	assert.Contains(t, res.Seed, "app_name")
	assert.Contains(t, res.Seed, "prefix")
}

func TestLoadTwoPhase_RejectsStateDependent(t *testing.T) {
	cfg := &Config{
		Enabled: rslvrRef("saved"),
		Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("s.json")}},
	}
	mgr := newTwoPhaseManager(t, cfg)

	runnerCalled := false
	_, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{
		Resolvers: []*resolver.Resolver{stateReadingResolver("saved")},
		RunResolvers: func(context.Context, []*resolver.Resolver) (*resolver.Context, error) {
			runnerCalled = true
			return resolver.NewContext(), nil
		},
	})
	require.Error(t, err)
	var cycleErr *CycleError
	assert.True(t, errors.As(err, &cycleErr))
	assert.False(t, runnerCalled, "resolvers must not run when a cycle is detected")
}

func TestLoadTwoPhase_RejectsUnknown(t *testing.T) {
	cfg := &Config{
		Enabled: rslvrRef("ghost"),
		Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": literalValueRef("s.json")}},
	}
	mgr := newTwoPhaseManager(t, cfg)

	_, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{
		Resolvers:    []*resolver.Resolver{staticResolver("known")},
		RunResolvers: seedRunner(nil, nil),
	})
	require.Error(t, err)
	var unknownErr *UnknownStateRefError
	assert.True(t, errors.As(err, &unknownErr))
}

func TestLoadTwoPhase_NilRunnerErrors(t *testing.T) {
	cfg := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": rslvrRef("app_name")}},
	}
	mgr := newTwoPhaseManager(t, cfg)

	_, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{
		Resolvers:    []*resolver.Resolver{staticResolver("app_name")},
		RunResolvers: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolver runner")
}

func TestLoadTwoPhase_RunnerReturnsNilContext(t *testing.T) {
	cfg := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": rslvrRef("app_name")}},
	}
	mgr := newTwoPhaseManager(t, cfg)

	_, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{
		Resolvers: []*resolver.Resolver{staticResolver("app_name")},
		RunResolvers: func(context.Context, []*resolver.Resolver) (*resolver.Context, error) {
			return nil, nil
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no context")
}

func TestLoadTwoPhase_RunnerError(t *testing.T) {
	cfg := &Config{
		Enabled: literalValueRef(true),
		Backend: Backend{Provider: "mock-state", Inputs: map[string]*spec.ValueRef{"path": rslvrRef("app_name")}},
	}
	mgr := newTwoPhaseManager(t, cfg)

	_, err := mgr.LoadTwoPhase(context.Background(), nil, twoPhaseCommand(), TwoPhaseInput{
		Resolvers: []*resolver.Resolver{staticResolver("app_name")},
		RunResolvers: func(context.Context, []*resolver.Resolver) (*resolver.Context, error) {
			return nil, errors.New("boom")
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
