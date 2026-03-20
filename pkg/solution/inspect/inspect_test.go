// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/action"
	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractProviderNames(t *testing.T) {
	t.Run("extracts providers from all phases", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http"},
					{Provider: "static"},
				},
			},
			Transform: &resolver.TransformPhase{
				With: []resolver.ProviderTransform{
					{Provider: "jq"},
				},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{
					{Provider: "schema"},
				},
			},
		}

		providers := extractProviderNames(r)

		assert.Len(t, providers, 4)
		assert.Contains(t, providers, "http")
		assert.Contains(t, providers, "static")
		assert.Contains(t, providers, "jq")
		assert.Contains(t, providers, "schema")
	})

	t.Run("removes duplicates", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{
					{Provider: "http"},
					{Provider: "http"},
				},
			},
		}

		providers := extractProviderNames(r)
		assert.Len(t, providers, 1)
		assert.Equal(t, "http", providers[0])
	})

	t.Run("returns empty slice for empty resolver", func(t *testing.T) {
		r := &resolver.Resolver{}

		providers := extractProviderNames(r)
		assert.Empty(t, providers)
	})
}

func TestExtractPhases(t *testing.T) {
	t.Run("identifies all phases", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
			Transform: &resolver.TransformPhase{
				With: []resolver.ProviderTransform{{Provider: "jq"}},
			},
			Validate: &resolver.ValidatePhase{
				With: []resolver.ProviderValidation{{Provider: "schema"}},
			},
		}

		phases := extractPhases(r)
		assert.Len(t, phases, 3)
		assert.Equal(t, []string{"resolve", "transform", "validate"}, phases)
	})

	t.Run("identifies single phase", func(t *testing.T) {
		r := &resolver.Resolver{
			Resolve: &resolver.ResolvePhase{
				With: []resolver.ProviderSource{{Provider: "http"}},
			},
		}

		phases := extractPhases(r)
		assert.Equal(t, []string{"resolve"}, phases)
	})

	t.Run("returns empty slice for empty resolver", func(t *testing.T) {
		r := &resolver.Resolver{}

		phases := extractPhases(r)
		assert.Empty(t, phases)
	})
}

func TestBuildSolutionExplanation_Minimal(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "test-solution"

	exp := BuildSolutionExplanation(sol)
	require.NotNil(t, exp)
	assert.Equal(t, "test-solution", exp.Name)
	assert.Equal(t, "unknown", exp.Version)
}

func TestBuildSolutionExplanation_WithVersion(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "versioned"
	sol.Metadata.Version = semver.MustParse("2.1.0")

	exp := BuildSolutionExplanation(sol)
	assert.Equal(t, "2.1.0", exp.Version)
}

func TestBuildSolutionExplanation_WithDisplayName(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "sol"
	sol.Metadata.DisplayName = "My Solution"
	sol.Metadata.Description = "A test solution"
	sol.Metadata.Category = "infra"
	sol.Metadata.Tags = []string{"a", "b"}

	exp := BuildSolutionExplanation(sol)
	assert.Equal(t, "My Solution", exp.DisplayName)
	assert.Equal(t, "A test solution", exp.Description)
	assert.Equal(t, "infra", exp.Category)
	assert.Equal(t, []string{"a", "b"}, exp.Tags)
}

func TestBuildSolutionExplanation_WithLinks(t *testing.T) {
	sol := &solution.Solution{}
	sol.Metadata.Name = "sol"
	sol.Metadata.Links = []solution.Link{{Name: "Docs", URL: "https://example.com"}}
	sol.Metadata.Maintainers = []solution.Contact{{Name: "Alice", Email: "alice@example.com"}}

	exp := BuildSolutionExplanation(sol)
	require.Len(t, exp.Links, 1)
	assert.Equal(t, "Docs", exp.Links[0].Name)
	require.Len(t, exp.Maintainers, 1)
	assert.Equal(t, "Alice", exp.Maintainers[0].Name)
}

func TestBuildResolverInfos_Empty(t *testing.T) {
	sol := &solution.Solution{}
	infos := buildResolverInfos(sol, nil)
	assert.Empty(t, infos)
}

func TestBuildResolverInfos_WithResolvers(t *testing.T) {
	sol := &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"alpha": {
					Name: "alpha",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{{Provider: "parameter"}},
					},
				},
			},
		},
	}
	infos := buildResolverInfos(sol, nil)
	require.Len(t, infos, 1)
	assert.Equal(t, "alpha", infos[0].Name)
	assert.Contains(t, infos[0].Providers, "parameter")
}

func TestBuildActionInfos_Empty(t *testing.T) {
	infos := buildActionInfos(nil, nil, "spec.workflow.actions")
	assert.Nil(t, infos)
}

func TestBuildActionInfos_WithActions(t *testing.T) {
	actions := map[string]*action.Action{
		"deploy": {
			Name:     "deploy",
			Provider: "shell",
		},
	}
	infos := buildActionInfos(actions, nil, "spec.workflow.actions")
	require.Len(t, infos, 1)
	assert.Equal(t, "deploy", infos[0].Name)
	assert.Equal(t, "shell", infos[0].Provider)
}

func TestBuildActionInfos_UnknownProvider(t *testing.T) {
	actions := map[string]*action.Action{
		"run": {Name: "run"},
	}
	infos := buildActionInfos(actions, nil, "spec.workflow.actions")
	require.Len(t, infos, 1)
	assert.Equal(t, "unknown", infos[0].Provider)
}

func TestLookupProvider_Found(t *testing.T) {
	reg := provider.NewRegistry()
	v := semver.MustParse("1.0.0")
	p := &testProvider{
		desc: &provider.Descriptor{
			Name:         "test-prov",
			APIVersion:   "v1",
			Version:      v,
			Description:  "test provider",
			Capabilities: []provider.Capability{provider.CapabilityFrom},
			OutputSchemas: map[provider.Capability]*jsonschema.Schema{
				provider.CapabilityFrom: {
					Type: "object",
					Properties: map[string]*jsonschema.Schema{
						"result": {Type: "string"},
					},
				},
			},
		},
	}
	require.NoError(t, reg.Register(p))

	desc, err := LookupProvider(context.Background(), "test-prov", reg)
	require.NoError(t, err)
	assert.Equal(t, "test-prov", desc.Name)
}

func TestLookupProvider_NotFound(t *testing.T) {
	reg := provider.NewRegistry()

	_, err := LookupProvider(context.Background(), "nonexistent", reg)
	assert.Error(t, err)
}

// testProvider is a minimal provider for testing LookupProvider.
type testProvider struct {
	desc *provider.Descriptor
}

func (p *testProvider) Descriptor() *provider.Descriptor { return p.desc }
func (p *testProvider) Execute(_ context.Context, _ any) (*provider.Output, error) {
	return &provider.Output{}, nil
}
