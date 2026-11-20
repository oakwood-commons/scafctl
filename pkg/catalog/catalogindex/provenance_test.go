// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalogindex

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

const (
	provRegistry      = "ghcr.io/acme/plugins"
	provUnconfigured  = "unknown.io/nope"
	provAdHocCatalog  = "adhoc.io/team"
	provConfiguredAls = "Prod"
)

func provBuilt() catalog.ArtifactInfo {
	return catalog.ArtifactInfo{Catalog: catalog.LocalCatalogName}
}

func provCached(canonical string) catalog.ArtifactInfo {
	return catalog.ArtifactInfo{Catalog: catalog.LocalCatalogName, Canonical: canonical}
}

func provRemote(catalogName, canonical string) catalog.ArtifactInfo {
	return catalog.ArtifactInfo{Catalog: catalogName, Canonical: canonical}
}

func provMerged(catalogName string) catalog.ArtifactInfo {
	return catalog.ArtifactInfo{
		Catalog:   catalogName,
		Canonical: provRegistry,
		Locations: []catalog.ArtifactLocation{catalog.LocationLocal, catalog.LocationRemote},
	}
}

func TestIndex_DisplayCatalog(t *testing.T) {
	idx := FromConfig(testConfig())

	tests := []struct {
		name string
		in   catalog.ArtifactInfo
		want string
	}{
		{name: "built artifact is local", in: provBuilt(), want: catalog.LocalCatalogName},
		{name: "cached copy of configured registry shows alias", in: provCached(provRegistry), want: provConfiguredAls},
		{name: "cached canonical matched case-insensitively", in: provCached("GHCR.io/Acme/PLUGINS"), want: provConfiguredAls},
		{name: "cached copy of unconfigured registry has no alias", in: provCached(provUnconfigured), want: ""},
		{name: "remote row shows alias", in: provRemote("whatever", provRegistry), want: provConfiguredAls},
		{name: "merged row shows alias", in: provMerged("prod"), want: provConfiguredAls},
		{name: "remote row from unconfigured registry keeps its catalog", in: provRemote(provAdHocCatalog, provUnconfigured), want: provAdHocCatalog},
		{name: "remote row without canonical keeps its catalog", in: provRemote("staging", ""), want: "staging"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, idx.DisplayCatalog(tt.in))
		})
	}
}

func TestIndex_DisplayCatalog_NilIndex(t *testing.T) {
	var idx *Index
	assert.Equal(t, catalog.LocalCatalogName, idx.DisplayCatalog(provBuilt()))
	assert.Empty(t, idx.DisplayCatalog(provCached(provRegistry)))
	assert.Equal(t, "prod", idx.DisplayCatalog(provRemote("prod", provRegistry)))
}

func TestIndex_DisplayCatalog_FirstWinsOnSharedOrigin(t *testing.T) {
	idx := FromConfig(&config.Config{
		Catalogs: []config.CatalogConfig{
			{Name: "first", URL: "oci://" + provRegistry},
			{Name: "second", URL: "oci://" + provRegistry},
		},
	})
	assert.Equal(t, "first", idx.DisplayCatalog(provCached(provRegistry)))
}

func TestIndex_MatchesCatalog(t *testing.T) {
	idx := FromConfig(testConfig())

	tests := []struct {
		name   string
		in     catalog.ArtifactInfo
		filter string
		want   bool
	}{
		{name: "local matches built", in: provBuilt(), filter: catalog.LocalCatalogName, want: true},
		{name: "local matches cached", in: provCached(provRegistry), filter: catalog.LocalCatalogName, want: true},
		{name: "local matches merged", in: provMerged("Prod"), filter: catalog.LocalCatalogName, want: true},
		{name: "local filter is case-insensitive", in: provBuilt(), filter: "LOCAL", want: true},
		{name: "local does not match remote-only", in: provRemote("Prod", provRegistry), filter: catalog.LocalCatalogName, want: false},
		{name: "alias matches remote row", in: provRemote("Prod", provRegistry), filter: "prod", want: true},
		{name: "alias matches merged row", in: provMerged("Prod"), filter: "Prod", want: true},
		{name: "alias matches cached copy of its registry", in: provCached(provRegistry), filter: "prod", want: true},
		{name: "alias does not match built", in: provBuilt(), filter: "prod", want: false},
		{name: "alias does not match other registry", in: provCached(provUnconfigured), filter: "prod", want: false},
		{name: "listed catalog name matches second alias of shared registry", in: provRemote("second", provRegistry), filter: "second", want: true},
		{name: "other alias does not match", in: provRemote("Prod", provRegistry), filter: "staging", want: false},
		{name: "empty filter matches nothing", in: provBuilt(), filter: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, idx.MatchesCatalog(tt.in, tt.filter))
		})
	}
}

func TestIndex_MatchesCatalog_NilIndex(t *testing.T) {
	var idx *Index
	assert.True(t, idx.MatchesCatalog(provBuilt(), catalog.LocalCatalogName))
	assert.True(t, idx.MatchesCatalog(provRemote("prod", provRegistry), "prod"))
	assert.False(t, idx.MatchesCatalog(provCached(provRegistry), "prod"))
}

func BenchmarkIndex_DisplayCatalog(b *testing.B) {
	idx := FromConfig(testConfig())
	a := provMerged("Prod")
	b.ReportAllocs()
	for b.Loop() {
		_ = idx.DisplayCatalog(a)
	}
}
