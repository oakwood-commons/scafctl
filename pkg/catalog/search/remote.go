// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package search

import (
	"context"
	"sort"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

// CatalogInfo describes a registered catalog for discovery purposes.
type CatalogInfo struct {
	Name          string   `json:"name"                    yaml:"name"                    doc:"Catalog identifier" example:"official"`
	Type          string   `json:"type"                    yaml:"type"                    doc:"Catalog type (oci, filesystem)" example:"oci"`
	Registry      string   `json:"registry,omitempty"      yaml:"registry,omitempty"      doc:"OCI registry host" example:"ghcr.io"`
	ArtifactKinds []string `json:"artifactKinds,omitempty" yaml:"artifactKinds,omitempty" doc:"Artifact kinds served by this catalog (empty means all kinds)" maxItems:"3"`
	Default       bool     `json:"default,omitempty"       yaml:"default,omitempty"       doc:"Whether this is the default catalog"`
}

// RegisteredCatalogsFromConfig builds the sorted list of registered catalogs
// for discovery tools, always including the local filesystem catalog and
// omitting the official catalog when disabled.
func RegisteredCatalogsFromConfig(cfg *config.Config) []CatalogInfo {
	catalogs := []CatalogInfo{
		{
			Name: config.CatalogNameLocal,
			Type: config.CatalogTypeFilesystem,
		},
	}
	if cfg == nil {
		return catalogs
	}

	defaultCatalog := cfg.Settings.DefaultCatalog
	for _, catCfg := range cfg.Catalogs {
		if catCfg.Name == config.CatalogNameLocal {
			continue
		}
		if catCfg.Name == config.CatalogNameOfficial && cfg.Settings.DisableOfficialCatalog {
			continue
		}
		var kinds []string
		if len(catCfg.ArtifactKinds) > 0 {
			kinds = make([]string, len(catCfg.ArtifactKinds))
			copy(kinds, catCfg.ArtifactKinds)
		}
		info := CatalogInfo{
			Name:          catCfg.Name,
			Type:          catCfg.Type,
			ArtifactKinds: kinds,
			Default:       catCfg.Name == defaultCatalog,
		}
		if catCfg.Type == config.CatalogTypeOCI && catCfg.URL != "" {
			registry, _ := catalog.ParseCatalogURL(catCfg.URL)
			info.Registry = registry
		}
		catalogs = append(catalogs, info)
	}

	sort.Slice(catalogs, func(i, j int) bool {
		return catalogs[i].Name < catalogs[j].Name
	})
	return catalogs
}

// RemoteIndex fetches the catalog index from a remote catalog and returns
// solution entries that match the given search options. Non-solution artifacts
// are excluded. The catalog name is attached to each entry.
func RemoteIndex(ctx context.Context, _ logr.Logger, rc *catalog.RemoteCatalog, opts Options) ([]Result, error) {
	artifacts, err := rc.FetchIndex(ctx)
	if err != nil {
		return nil, err
	}

	entries := EntriesFromIndex(artifacts, rc.Name())
	return applySearch(entries, opts), nil
}

// ListRemoteIndex fetches the catalog index and returns all solution entries
// without filtering. Useful for "list all" operations.
func ListRemoteIndex(ctx context.Context, _ logr.Logger, rc *catalog.RemoteCatalog) ([]SolutionEntry, error) {
	artifacts, err := rc.FetchIndex(ctx)
	if err != nil {
		return nil, err
	}

	return EntriesFromIndex(artifacts, rc.Name()), nil
}

// EntriesFromIndex converts DiscoveredArtifact index entries to SolutionEntry
// items. Only solution-kind artifacts are included. The catalogName is attached
// to each entry for provenance.
func EntriesFromIndex(artifacts []catalog.DiscoveredArtifact, catalogName string) []SolutionEntry {
	var entries []SolutionEntry
	for _, a := range artifacts {
		if a.Kind != catalog.ArtifactKindSolution {
			continue
		}
		entries = append(entries, SolutionEntry{
			Name:        a.Name,
			Version:     a.LatestVersion,
			Description: a.Description,
			DisplayName: a.DisplayName,
			Category:    a.Category,
			Tags:        a.Tags,
			Parameters:  a.Parameters,
			Providers:   a.Providers,
			Maintainers: a.Maintainers,
			Catalog:     catalogName,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries
}

// applySearch scores and filters entries against the given options.
func applySearch(entries []SolutionEntry, opts Options) []Result {
	var results []Result
	for _, entry := range entries {
		score := matchEntry(entry, opts)
		if score > 0 {
			results = append(results, Result{
				SolutionEntry: entry,
				Score:         score,
			})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Name < results[j].Name
	})

	return results
}
