// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRunCommand_WorkflowSolution(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{
			"deploy": {},
		},
	}

	info, err := BuildRunCommand(sol, "solutions/deploy.yaml")
	require.NoError(t, err)

	assert.Equal(t, "scafctl run solution", info.Subcommand)
	assert.Contains(t, info.Command, "scafctl run solution -f ./solutions/deploy.yaml")
	assert.True(t, info.HasWorkflow)
	assert.Empty(t, info.Parameters)
}

func TestBuildRunCommand_ResolverOnlySolution(t *testing.T) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"env": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "parameter"}},
			},
			Description: "Environment name",
			Example:     "prod",
		},
		"region": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "parameter"}},
			},
		},
		"config": {
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "cel"}},
			},
		},
	}

	info, err := BuildRunCommand(sol, "/abs/path/solution.yaml")
	require.NoError(t, err)

	assert.Equal(t, "scafctl run resolver", info.Subcommand)
	assert.True(t, info.HasResolvers)
	assert.False(t, info.HasWorkflow)
	// Only parameter-type resolvers listed
	assert.Len(t, info.Parameters, 2)
	// Command includes positional parameter values (resolver-only uses positional syntax)
	assert.Contains(t, info.Command, "env=prod")
	assert.Contains(t, info.Command, "region=<value>")
	// Should NOT use -r flags for resolver-only solutions
	assert.NotContains(t, info.Command, "-r env=")
	assert.NotContains(t, info.Command, "-r region=")
	// Non-parameter resolver not included
	for _, p := range info.Parameters {
		assert.NotEqual(t, "config", p.Name)
	}
}

func TestBuildRunCommand_EmptySolution(t *testing.T) {
	sol := &solution.Solution{}

	info, err := BuildRunCommand(sol, "empty.yaml")

	assert.Nil(t, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "neither resolvers nor a workflow")
}

func TestBuildRunCommand_PathPrefixing(t *testing.T) {
	tests := []struct {
		name        string
		path        string
		expectInCmd string
	}{
		{"relative bare", "solution.yaml", "-f ./solution.yaml"},
		{"relative dotslash", "./solution.yaml", "-f ./solution.yaml"},
		{"relative dotdot", "../solution.yaml", "-f ../solution.yaml"},
		{"absolute", "/home/user/solution.yaml", "-f /home/user/solution.yaml"},
		{"url", "https://example.com/sol.yaml", "-f https://example.com/sol.yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sol := &solution.Solution{}
			sol.Spec.Resolvers = map[string]*resolver.Resolver{
				"x": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel"}}}},
			}

			info, err := BuildRunCommand(sol, tt.path)
			require.NoError(t, err)
			assert.Contains(t, info.Command, tt.expectInCmd)
		})
	}
}

func BenchmarkBuildRunCommand(b *testing.B) {
	sol := &solution.Solution{}
	sol.Spec.Resolvers = map[string]*resolver.Resolver{
		"env":    {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}}},
		"region": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "parameter"}}}},
		"config": {Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "cel"}}}},
	}
	sol.Spec.Workflow = &action.Workflow{
		Actions: map[string]*action.Action{"deploy": {}},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildRunCommand(sol, "solution.yaml") //nolint:errcheck
	}
}
