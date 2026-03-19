// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package bundler

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergePluginDefaults_NoPlugins(t *testing.T) {
	sol := &solution.Solution{}
	MergePluginDefaults(sol) // should not panic
}

func TestMergePluginDefaults_WithPluginDefaults(t *testing.T) {
	defaultVal := &spec.ValueRef{Literal: "default-value"}
	existingVal := &spec.ValueRef{Literal: "existing"}

	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{
					Name:    "myprovider",
					Kind:    solution.PluginKindProvider,
					Version: "^1.0.0",
					Defaults: map[string]*spec.ValueRef{
						"key1": defaultVal,
						"key2": defaultVal,
					},
				},
			},
		},
		Spec: solution.Spec{
			Workflow: &action.Workflow{
				Actions: map[string]*action.Action{
					"step1": {
						Provider: "myprovider",
						Inputs: map[string]*spec.ValueRef{
							"key1": existingVal, // should NOT be overwritten
						},
					},
				},
			},
		},
	}

	MergePluginDefaults(sol)

	act := sol.Spec.Workflow.Actions["step1"]
	assert.Equal(t, existingVal, act.Inputs["key1"], "existing input should not be overwritten")
	assert.Equal(t, defaultVal, act.Inputs["key2"], "missing input should get default")
}

func TestMergePluginDefaults_MergesResolverInputs(t *testing.T) {
	defaultVal := &spec.ValueRef{Literal: "default-value"}

	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{
					Name:    "myprovider",
					Kind:    solution.PluginKindProvider,
					Version: "^1.0.0",
					Defaults: map[string]*spec.ValueRef{
						"key1": defaultVal,
					},
				},
			},
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{
								Provider: "myprovider",
								Inputs:   map[string]*spec.ValueRef{},
							},
						},
					},
					Transform: &resolver.TransformPhase{
						With: []resolver.ProviderTransform{
							{
								Provider: "myprovider",
								Inputs:   map[string]*spec.ValueRef{},
							},
						},
					},
					Validate: &resolver.ValidatePhase{
						With: []resolver.ProviderValidation{
							{
								Provider: "myprovider",
								Inputs:   map[string]*spec.ValueRef{},
							},
						},
					},
				},
			},
		},
	}

	MergePluginDefaults(sol)

	assert.Equal(t, defaultVal, sol.Spec.Resolvers["r1"].Resolve.With[0].Inputs["key1"])
	assert.Equal(t, defaultVal, sol.Spec.Resolvers["r1"].Transform.With[0].Inputs["key1"])
	assert.Equal(t, defaultVal, sol.Spec.Resolvers["r1"].Validate.With[0].Inputs["key1"])
}

func TestMergePluginDefaults_NonProviderPlugin(t *testing.T) {
	// auth-handler plugins don't have defaults merged into provider inputs
	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{
					Name:    "myauth",
					Kind:    solution.PluginKindAuthHandler,
					Version: "^1.0.0",
					Defaults: map[string]*spec.ValueRef{
						"key1": {Literal: "val"},
					},
				},
			},
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"r1": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "myauth", Inputs: map[string]*spec.ValueRef{}},
						},
					},
				},
			},
		},
	}

	MergePluginDefaults(sol)
	// auth-handler should not have defaults merged
	assert.Empty(t, sol.Spec.Resolvers["r1"].Resolve.With[0].Inputs)
}

func TestValidatePlugins_Valid(t *testing.T) {
	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "myplugin", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
			},
		},
	}
	err := ValidatePlugins(sol)
	assert.NoError(t, err)
}

func TestValidatePlugins_EmptyName(t *testing.T) {
	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
			},
		},
	}
	err := ValidatePlugins(sol)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestValidatePlugins_InvalidKind(t *testing.T) {
	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "p", Kind: "invalid-kind", Version: "^1.0.0"},
			},
		},
	}
	err := ValidatePlugins(sol)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
}

func TestValidatePlugins_EmptyVersion(t *testing.T) {
	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "p", Kind: solution.PluginKindProvider, Version: ""},
			},
		},
	}
	err := ValidatePlugins(sol)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "version constraint is required")
}

func TestValidatePlugins_InvalidVersion(t *testing.T) {
	sol := &solution.Solution{
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "p", Kind: solution.PluginKindProvider, Version: "not-a-semver!!!"},
			},
		},
	}
	err := ValidatePlugins(sol)
	assert.Error(t, err)
}

func TestPluginsToBundleEntries(t *testing.T) {
	plugins := []solution.PluginDependency{
		{Name: "p1", Kind: solution.PluginKindProvider, Version: "^1.0.0"},
		{Name: "p2", Kind: solution.PluginKindAuthHandler, Version: ">=2.0.0"},
	}
	entries := PluginsToBundleEntries(plugins)
	require.Len(t, entries, 2)
	assert.Equal(t, "p1", entries[0].Name)
	assert.Equal(t, string(solution.PluginKindProvider), entries[0].Kind)
	assert.Equal(t, "^1.0.0", entries[0].Version)
	assert.Equal(t, "p2", entries[1].Name)
}

func TestPluginsToBundleEntries_Empty(t *testing.T) {
	entries := PluginsToBundleEntries(nil)
	assert.Empty(t, entries)
}
