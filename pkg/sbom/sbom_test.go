package sbom
// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package sbom

import (
	"encoding/json"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/resolver"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_MinimalSolution(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:    "test-sol",
			Version: semver.MustParse("1.0.0"),
		},
	}

	data, err := Generate(sol, GenerateOptions{BinaryName: "scafctl"})
	require.NoError(t, err)

	var doc Document
	require.NoError(t, json.Unmarshal(data, &doc))

	assert.Equal(t, "SPDX-2.3", doc.SPDXVersion)
	assert.Equal(t, "CC0-1.0", doc.DataLicense)
	assert.Equal(t, "test-sol-1.0.0", doc.Name)
	assert.Contains(t, doc.CreationInfo.Creators, "Tool: scafctl")
	require.Len(t, doc.Packages, 1)
	assert.Equal(t, "test-sol", doc.Packages[0].Name)
	assert.Equal(t, "1.0.0", doc.Packages[0].Version)
	require.Len(t, doc.Relationships, 1)
	assert.Equal(t, "DESCRIBES", doc.Relationships[0].Type)
}

func TestGenerate_NilSolution(t *testing.T) {
	t.Parallel()

	_, err := Generate(nil, GenerateOptions{})
	assert.Error(t, err)
}

func TestGenerate_WithBundle(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:    "bundled-sol",
			Version: semver.MustParse("2.0.0"),
		},
	}

	data, err := Generate(sol, GenerateOptions{
		ContentDigest: "abc123",
		BundleDigest:  "def456",
	})
	require.NoError(t, err)

	var doc Document
	require.NoError(t, json.Unmarshal(data, &doc))

	require.Len(t, doc.Packages, 2)
	assert.Equal(t, "SPDXRef-Solution", doc.Packages[0].SPDXID)
	assert.Equal(t, "SPDXRef-Bundle", doc.Packages[1].SPDXID)
	require.Len(t, doc.Packages[0].Checksums, 1)
	assert.Equal(t, "abc123", doc.Packages[0].Checksums[0].Value)
	require.Len(t, doc.Packages[1].Checksums, 1)
	assert.Equal(t, "def456", doc.Packages[1].Checksums[0].Value)

	// DESCRIBES + CONTAINS
	assert.Len(t, doc.Relationships, 2)
	assert.Equal(t, "CONTAINS", doc.Relationships[1].Type)
}

func TestGenerate_WithProviders(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:    "prov-sol",
			Version: semver.MustParse("1.0.0"),
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "parameter"},
							{Provider: "cel"},
						},
					},
				},
				"cfg": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "parameter"}, // duplicate, should be deduped
						},
					},
				},
			},
		},
	}

	data, err := Generate(sol, GenerateOptions{})
	require.NoError(t, err)

	var doc Document
	require.NoError(t, json.Unmarshal(data, &doc))

	// Root + 2 unique providers (parameter, cel)
	assert.Len(t, doc.Packages, 3)

	provNames := make(map[string]bool)
	for _, pkg := range doc.Packages[1:] {
		provNames[pkg.Name] = true
	}
	assert.True(t, provNames["parameter"])
	assert.True(t, provNames["cel"])
}

func TestGenerate_WithPluginDependencies(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:    "plugin-sol",
			Version: semver.MustParse("1.0.0"),
		},
		Bundle: solution.Bundle{
			Plugins: []solution.PluginDependency{
				{Name: "aws-provider", Kind: solution.PluginKindProvider, Version: "^1.5.0"},
			},
		},
	}

	data, err := Generate(sol, GenerateOptions{})
	require.NoError(t, err)

	var doc Document
	require.NoError(t, json.Unmarshal(data, &doc))

	// Root + 1 plugin
	require.Len(t, doc.Packages, 2)
	assert.Equal(t, "aws-provider", doc.Packages[1].Name)
	assert.Equal(t, "^1.5.0", doc.Packages[1].Version)
}

func TestGenerate_CustomNamespace(t *testing.T) {
	t.Parallel()

	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:    "ns-sol",
			Version: semver.MustParse("1.0.0"),
		},
	}

	data, err := Generate(sol, GenerateOptions{
		Namespace: "https://example.com/sbom/test",
	})
	require.NoError(t, err)

	var doc Document
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, "https://example.com/sbom/test", doc.Namespace)
}

func TestSanitizeSPDXID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with-dash", "with-dash"},
		{"with.dot", "with.dot"},
		{"with spaces", "with-spaces"},
		{"with/slash", "with-slash"},
		{"with@at", "with-at"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, sanitizeSPDXID(tt.input), "input: %s", tt.input)
	}
}

func BenchmarkGenerate(b *testing.B) {
	sol := &solution.Solution{
		Metadata: solution.Metadata{
			Name:    "bench-sol",
			Version: semver.MustParse("1.0.0"),
		},
		Spec: solution.Spec{
			Resolvers: map[string]*resolver.Resolver{
				"env": {
					Resolve: &resolver.ResolvePhase{
						With: []resolver.ProviderSource{
							{Provider: "parameter"},
						},
					},
				},
			},
		},
	}

	b.ResetTimer()
	for range b.N {
		_, _ = Generate(sol, GenerateOptions{ContentDigest: "abc123"})
	}
}
