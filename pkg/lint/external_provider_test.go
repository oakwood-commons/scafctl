// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package lint

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/provider"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// solutionReferencingProvider builds a minimal solution whose single resolver
// references the given provider in its resolve phase, so the provider name is
// picked up by Spec.ReferencedProviderNames().
func solutionReferencingProvider(providerName string, plugins ...solution.PluginDependency) *solution.Solution {
	return &solution.Solution{
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"data": {
					Name: "data",
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: providerName},
						},
					},
				},
			},
		},
		Bundle: solution.Bundle{Plugins: plugins},
	}
}

func newExternalProviderResult() *Result {
	return &Result{Findings: make([]*Finding, 0)}
}

func TestLintValidateExternalProviders(t *testing.T) {
	t.Run("undeclared external provider is flagged", func(t *testing.T) {
		sol := solutionReferencingProvider("terraform")
		result := newExternalProviderResult()

		lintValidateExternalProviders(sol, result, provider.NewRegistry())

		findings := filterFindingsByRule(result, "external-provider-validation")
		require.Len(t, findings, 1)
		assert.Equal(t, SeverityError, findings[0].Severity)
		assert.Equal(t, "provider", findings[0].Category)
		assert.Contains(t, findings[0].Message, "terraform")
	})

	t.Run("provider declared in bundle.plugins is clean", func(t *testing.T) {
		sol := solutionReferencingProvider("terraform",
			solution.PluginDependency{Name: "terraform", Kind: solution.PluginKindProvider, Version: ">=1.0.0"})
		result := newExternalProviderResult()

		lintValidateExternalProviders(sol, result, provider.NewRegistry())

		assert.Empty(t, filterFindingsByRule(result, "external-provider-validation"))
	})

	t.Run("provider present in the registry is clean", func(t *testing.T) {
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(newFakeProvider("terraform", nil)))
		sol := solutionReferencingProvider("terraform")
		result := newExternalProviderResult()

		lintValidateExternalProviders(sol, result, reg)

		assert.Empty(t, filterFindingsByRule(result, "external-provider-validation"))
	})

	t.Run("builtin provider available via registry is clean", func(t *testing.T) {
		reg := provider.NewRegistry()
		require.NoError(t, reg.Register(newFakeProvider("cel", nil)))
		sol := solutionReferencingProvider("cel")
		result := newExternalProviderResult()

		lintValidateExternalProviders(sol, result, reg)

		assert.Empty(t, filterFindingsByRule(result, "external-provider-validation"))
	})

	t.Run("multiple undeclared providers are reported in a single finding", func(t *testing.T) {
		sol := &solution.Solution{
			Spec: solution.Spec{
				Resolvers: map[string]*resolver.Resolver{
					"a": {
						Name:    "a",
						Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "terraform"}}},
					},
					"b": {
						Name:    "b",
						Resolve: &resolver.ResolvePhase{With: []resolver.ProviderSource{{Provider: "pulumi"}}},
					},
				},
			},
		}
		result := newExternalProviderResult()

		lintValidateExternalProviders(sol, result, provider.NewRegistry())

		findings := filterFindingsByRule(result, "external-provider-validation")
		require.Len(t, findings, 1)
		assert.Contains(t, findings[0].Message, "terraform")
		assert.Contains(t, findings[0].Message, "pulumi")
	})
}
