// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package inspect

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/stretchr/testify/assert"
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
