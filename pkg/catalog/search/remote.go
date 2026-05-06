// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package search

import (
	"context"
	"sort"

	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
)

// CatalogInfo describes a registered catalog for discovery purposes.
type CatalogInfo struct {
	Name     string `json:"name"               yaml:"name"               doc:"Catalog identifier" example:"official"`
	Type     string `json:"type"               yaml:"type"               doc:"Catalog type (oci, filesystem)" example:"oci"`
	Registry string `json:"registry,omitempty"  yaml:"registry,omitempty" doc:"OCI registry host" example:"ghcr.io"`
	Default  bool   `json:"default,omitempty"   yaml:"default,omitempty"  doc:"Whether this is the default catalog"`
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
