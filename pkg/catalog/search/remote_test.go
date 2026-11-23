// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package search

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEntriesFromIndex(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.DiscoveredArtifact{
		{
			Kind:          catalog.ArtifactKindSolution,
			Name:          "hello-world",
			LatestVersion: "1.0.0",
			Description:   "A hello world solution",
			DisplayName:   "Hello World",
			Category:      "getting-started",
			Tags:          []string{"tutorial", "beginner"},
			Parameters:    []string{"name"},
			Providers:     []string{"exec"},
			Maintainers:   []string{"Alice"},
		},
		{
			Kind:          "provider",
			Name:          "sleep",
			LatestVersion: "0.1.0",
		},
		{
			Kind:          catalog.ArtifactKindSolution,
			Name:          "cloud-deploy",
			LatestVersion: "2.0.0",
			Category:      "deployment",
			Providers:     []string{"gcp", "http"},
		},
	}

	entries := EntriesFromIndex(artifacts, "official")

	// Only solutions, sorted by name.
	assert.Len(t, entries, 2)
	assert.Equal(t, "cloud-deploy", entries[0].Name)
	assert.Equal(t, "hello-world", entries[1].Name)

	// Check field mapping.
	assert.Equal(t, "1.0.0", entries[1].Version)
	assert.Equal(t, "A hello world solution", entries[1].Description)
	assert.Equal(t, "Hello World", entries[1].DisplayName)
	assert.Equal(t, "getting-started", entries[1].Category)
	assert.Equal(t, []string{"tutorial", "beginner"}, entries[1].Tags)
	assert.Equal(t, []string{"name"}, entries[1].Parameters)
	assert.Equal(t, []string{"exec"}, entries[1].Providers)
	assert.Equal(t, []string{"Alice"}, entries[1].Maintainers)
	assert.Equal(t, "official", entries[1].Catalog)
}

func TestEntriesFromIndex_Empty(t *testing.T) {
	t.Parallel()

	entries := EntriesFromIndex(nil, "test")
	assert.Nil(t, entries)
}

func TestEntriesFromIndex_NoSolutions(t *testing.T) {
	t.Parallel()

	artifacts := []catalog.DiscoveredArtifact{
		{Kind: "provider", Name: "exec", LatestVersion: "1.0.0"},
	}
	entries := EntriesFromIndex(artifacts, "test")
	assert.Nil(t, entries)
}

func TestApplySearch(t *testing.T) {
	t.Parallel()

	entries := []SolutionEntry{
		{Name: "cloud-deploy", Category: "deployment", Providers: []string{"gcp"}},
		{Name: "hello-world", Category: "getting-started", Tags: []string{"tutorial"}},
		{Name: "web-app", Category: "deployment", Providers: []string{"http"}},
	}

	t.Run("query matches", func(t *testing.T) {
		t.Parallel()
		results := applySearch(entries, Options{Query: "cloud"})
		assert.Len(t, results, 1)
		assert.Equal(t, "cloud-deploy", results[0].Name)
	})

	t.Run("category filter", func(t *testing.T) {
		t.Parallel()
		results := applySearch(entries, Options{Category: "deployment"})
		assert.Len(t, results, 2)
	})

	t.Run("provider filter", func(t *testing.T) {
		t.Parallel()
		results := applySearch(entries, Options{Provider: "gcp"})
		assert.Len(t, results, 1)
		assert.Equal(t, "cloud-deploy", results[0].Name)
	})

	t.Run("no match", func(t *testing.T) {
		t.Parallel()
		results := applySearch(entries, Options{Query: "nonexistent"})
		assert.Empty(t, results)
	})

	t.Run("empty options returns all", func(t *testing.T) {
		t.Parallel()
		results := applySearch(entries, Options{})
		assert.Len(t, results, 3)
	})
}

func BenchmarkEntriesFromIndex(b *testing.B) {
	artifacts := make([]catalog.DiscoveredArtifact, 100)
	for i := range artifacts {
		artifacts[i] = catalog.DiscoveredArtifact{
			Kind:          catalog.ArtifactKindSolution,
			Name:          "solution-" + string(rune('a'+i%26)),
			LatestVersion: "1.0.0",
			Description:   "A test solution",
			Category:      "test",
		}
	}

	b.ResetTimer()
	for b.Loop() {
		EntriesFromIndex(artifacts, "bench")
	}
}

func TestRegisteredCatalogsFromConfig(t *testing.T) {
	t.Parallel()

	// Nil config returns only the local filesystem catalog.
	nilList := RegisteredCatalogsFromConfig(nil)
	require.Len(t, nilList, 1)
	assert.Equal(t, config.CatalogNameLocal, nilList[0].Name)
	assert.Equal(t, config.CatalogTypeFilesystem, nilList[0].Type)

	kinds := []string{config.CatalogArtifactKindSolution}
	cfg := &config.Config{
		Settings: config.Settings{DefaultCatalog: "corp"},
		Catalogs: []config.CatalogConfig{
			{Name: config.CatalogNameLocal, Type: config.CatalogTypeFilesystem},
			{
				Name:          "corp",
				Type:          config.CatalogTypeOCI,
				URL:           "oci://registry.example.com/solutions",
				ArtifactKinds: kinds,
			},
			{
				Name: config.CatalogNameOfficial,
				Type: config.CatalogTypeOCI,
				URL:  config.DefaultOfficialCatalogURL,
			},
		},
	}

	got := RegisteredCatalogsFromConfig(cfg)
	require.Len(t, got, 3)
	// Sorted alphabetically: corp, local, official.
	assert.Equal(t, "corp", got[0].Name)
	assert.Equal(t, "registry.example.com", got[0].Registry)
	assert.Equal(t, []string{"solution"}, got[0].ArtifactKinds)
	assert.True(t, got[0].Default)
	assert.Equal(t, "local", got[1].Name)
	assert.Equal(t, "official", got[2].Name)

	// Mutating original slice does not mutate returned CatalogInfo.
	kinds[0] = "provider"
	assert.Equal(t, []string{"solution"}, got[0].ArtifactKinds)

	// DisableOfficialCatalog omits official.
	disabledCfg := *cfg
	disabledCfg.Settings.DisableOfficialCatalog = true
	disabled := RegisteredCatalogsFromConfig(&disabledCfg)
	require.Len(t, disabled, 2)
	assert.Equal(t, "corp", disabled[0].Name)
	assert.Equal(t, "local", disabled[1].Name)
}
