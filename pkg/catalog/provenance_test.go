// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestArtifactInfo_OriginKey(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		info ArtifactInfo
		want string
	}{
		"built local row has empty origin": {
			info: ArtifactInfo{Catalog: LocalCatalogName},
			want: "",
		},
		"cached local row uses canonical": {
			info: ArtifactInfo{Catalog: LocalCatalogName, Canonical: "ghcr.io/org/solutions"},
			want: "ghcr.io/org/solutions",
		},
		"remote row uses canonical": {
			info: ArtifactInfo{Catalog: "default", Canonical: "ghcr.io/org/solutions"},
			want: "ghcr.io/org/solutions",
		},
		"canonical is lowercased": {
			info: ArtifactInfo{Catalog: "default", Canonical: "GHCR.io/Org/Solutions"},
			want: "ghcr.io/org/solutions",
		},
		"non-local row without canonical falls back to catalog name": {
			info: ArtifactInfo{Catalog: "fs-cat"},
			want: "catalog:fs-cat",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.info.OriginKey())
		})
	}
}

func TestArtifactInfo_IsBuilt(t *testing.T) {
	t.Parallel()

	assert.True(t, ArtifactInfo{Catalog: LocalCatalogName}.IsBuilt())
	assert.False(t, ArtifactInfo{Catalog: LocalCatalogName, Canonical: "ghcr.io/org"}.IsBuilt())
	assert.False(t, ArtifactInfo{Catalog: "default"}.IsBuilt())
}

func TestArtifactInfo_ResolvedLocations(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		info ArtifactInfo
		want []ArtifactLocation
	}{
		"local store row": {
			info: ArtifactInfo{Catalog: LocalCatalogName},
			want: []ArtifactLocation{LocationLocal},
		},
		"remote row": {
			info: ArtifactInfo{Catalog: "default"},
			want: []ArtifactLocation{LocationRemote},
		},
		"explicit locations win": {
			info: ArtifactInfo{Catalog: "default", Locations: []ArtifactLocation{LocationRemote, LocationLocal}},
			want: []ArtifactLocation{LocationLocal, LocationRemote},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.info.ResolvedLocations())
		})
	}
}

func TestArtifactInfo_HasLocation(t *testing.T) {
	t.Parallel()

	merged := ArtifactInfo{Locations: []ArtifactLocation{LocationLocal, LocationRemote}}
	assert.True(t, merged.HasLocation(LocationLocal))
	assert.True(t, merged.HasLocation(LocationRemote))

	remote := ArtifactInfo{Catalog: "default"}
	assert.False(t, remote.HasLocation(LocationLocal))
	assert.True(t, remote.HasLocation(LocationRemote))
}

func TestFormatLocations(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   []ArtifactLocation
		want string
	}{
		"empty":      {in: nil, want: ""},
		"local":      {in: []ArtifactLocation{LocationLocal}, want: "local"},
		"remote":     {in: []ArtifactLocation{LocationRemote}, want: "remote"},
		"both":       {in: []ArtifactLocation{LocationLocal, LocationRemote}, want: "local, remote"},
		"both order": {in: []ArtifactLocation{LocationRemote, LocationLocal}, want: "local, remote"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, FormatLocations(tc.in))
		})
	}
}
