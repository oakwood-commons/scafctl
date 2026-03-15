// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package detail

import (
	"fmt"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractResolverInfo_NilSolution(t *testing.T) {
	t.Parallel()
	infos := ExtractResolverInfo(nil)
	assert.Nil(t, infos)
}

func TestExtractResolverInfo_NoResolvers(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: nil,
		},
	}
	infos := ExtractResolverInfo(sol)
	assert.Nil(t, infos)
}

func TestExtractResolverInfo_ParameterResolver(t *testing.T) {
	t.Parallel()

	sol := buildTestSolution(map[string]*resolver.Resolver{
		"environment": {
			Name:        "environment",
			Type:        spec.TypeString,
			Description: "Target deployment environment",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*spec.ValueRef{
							"key": {Literal: "env"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*spec.ValueRef{
							"value": {Literal: "dev"},
						},
					},
				},
			},
		},
	})

	infos := ExtractResolverInfo(sol)
	require.Len(t, infos, 1)
	assert.Equal(t, "environment", infos[0].Name)
	assert.Equal(t, "string", infos[0].Type)
	assert.Equal(t, "Target deployment environment", infos[0].Description)
	assert.Equal(t, "env", infos[0].ParameterKey)
	assert.True(t, infos[0].HasDefault)
}

func TestExtractResolverInfo_ParameterNoDefault(t *testing.T) {
	t.Parallel()

	sol := buildTestSolution(map[string]*resolver.Resolver{
		"token": {
			Name:        "token",
			Type:        spec.TypeString,
			Description: "Auth token",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*spec.ValueRef{
							"key": {Literal: "token"},
						},
					},
				},
			},
		},
	})

	infos := ExtractResolverInfo(sol)
	require.Len(t, infos, 1)
	assert.Equal(t, "token", infos[0].ParameterKey)
	assert.False(t, infos[0].HasDefault)
}

func TestExtractResolverInfo_ComputedResolver(t *testing.T) {
	t.Parallel()

	sol := buildTestSolution(map[string]*resolver.Resolver{
		"greeting": {
			Name:        "greeting",
			Type:        spec.TypeString,
			Description: "Final greeting",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "cel",
						Inputs: map[string]*spec.ValueRef{
							"expression": {Literal: "'hello'"},
						},
					},
				},
			},
		},
	})

	infos := ExtractResolverInfo(sol)
	require.Len(t, infos, 1)
	assert.Equal(t, "greeting", infos[0].Name)
	assert.Empty(t, infos[0].ParameterKey)
	assert.False(t, infos[0].HasDefault)
}

func TestFormatResolverInputHelp_EmptyResolvers(t *testing.T) {
	t.Parallel()
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: nil,
		},
	}
	result := FormatResolverInputHelp(sol)
	assert.Empty(t, result)
}

func TestFormatResolverInputHelp_MixedResolvers(t *testing.T) {
	t.Parallel()

	sol := buildTestSolution(map[string]*resolver.Resolver{
		"environment": {
			Name:        "environment",
			Type:        spec.TypeString,
			Description: "Target environment",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*spec.ValueRef{
							"key": {Literal: "env"},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*spec.ValueRef{
							"value": {Literal: "dev"},
						},
					},
				},
			},
		},
		"greeting": {
			Name:        "greeting",
			Type:        spec.TypeString,
			Description: "Final greeting",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "cel",
						Inputs: map[string]*spec.ValueRef{
							"expression": {Literal: "'hello'"},
						},
					},
				},
			},
		},
	})
	sol.Metadata.Name = "test-solution"

	result := FormatResolverInputHelp(sol)
	assert.Contains(t, result, "Solution Resolvers (test-solution):")
	assert.Contains(t, result, "PARAMETER")
	assert.Contains(t, result, "TYPE")
	assert.Contains(t, result, "RESOLVER")
	assert.Contains(t, result, "DESCRIPTION")
	assert.Contains(t, result, "env")
	assert.Contains(t, result, "environment")
	assert.Contains(t, result, "has default")
	assert.Contains(t, result, "greeting")
	assert.Contains(t, result, "computed")
}

func TestFormatResolverInputHelp_OnlyParameters(t *testing.T) {
	t.Parallel()

	sol := buildTestSolution(map[string]*resolver.Resolver{
		"name": {
			Name:        "name",
			Type:        spec.TypeString,
			Description: "User name",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*spec.ValueRef{
							"key": {Literal: "name"},
						},
					},
				},
			},
		},
	})
	sol.Metadata.Name = "param-only"

	result := FormatResolverInputHelp(sol)
	assert.Contains(t, result, "Solution Resolvers (param-only):")
	assert.Contains(t, result, "name")
	assert.NotContains(t, result, "computed")
}

func TestFormatResolverInputHelp_NoType(t *testing.T) {
	t.Parallel()

	sol := buildTestSolution(map[string]*resolver.Resolver{
		"data": {
			Name:        "data",
			Description: "Some data",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*spec.ValueRef{
							"key": {Literal: "data"},
						},
					},
				},
			},
		},
	})
	sol.Metadata.Name = "test"

	result := FormatResolverInputHelp(sol)
	assert.Contains(t, result, "any")
}

func BenchmarkFormatResolverInputHelp(b *testing.B) {
	resolvers := make(map[string]*resolver.Resolver, 50)
	for i := range 50 {
		name := fmt.Sprintf("resolver_%02d", i)
		r := &resolver.Resolver{
			Name:        name,
			Type:        spec.TypeString,
			Description: "Test resolver",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*spec.ValueRef{
							"key": {Literal: name},
						},
					},
					{
						Provider: "static",
						Inputs: map[string]*spec.ValueRef{
							"value": {Literal: "default"},
						},
					},
				},
			},
		}
		resolvers[name] = r
	}
	sol := buildTestSolution(resolvers)
	sol.Metadata.Name = "benchmark-solution"

	b.ResetTimer()
	for range b.N {
		FormatResolverInputHelp(sol)
	}
}

func BenchmarkExtractResolverInfo(b *testing.B) {
	resolvers := make(map[string]*resolver.Resolver, 50)
	for i := range 50 {
		name := fmt.Sprintf("resolver_%02d", i)
		r := &resolver.Resolver{
			Name:        name,
			Type:        spec.TypeString,
			Description: "Test resolver",
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{
						Provider: "parameter",
						Inputs: map[string]*spec.ValueRef{
							"key": {Literal: name},
						},
					},
				},
			},
		}
		resolvers[name] = r
	}
	sol := buildTestSolution(resolvers)

	b.ResetTimer()
	for range b.N {
		ExtractResolverInfo(sol)
	}
}

func buildTestSolution(resolvers map[string]*resolver.Resolver) *solution.Solution {
	return &solution.Solution{
		Metadata: solution.Metadata{
			Name: "test",
		},
		Spec: solution.Spec{
			Resolvers: resolvers,
		},
	}
}
