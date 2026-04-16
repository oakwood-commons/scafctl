// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package version

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterVersions(t *testing.T) {
	tests := []struct {
		name       string
		versions   []string
		constraint string
		want       []string
		wantErr    bool
	}{
		{
			name:       "exact match",
			versions:   []string{"1.0.0", "1.1.0", "2.0.0"},
			constraint: "1.1.0",
			want:       []string{"1.1.0"},
		},
		{
			name:       "caret range",
			versions:   []string{"1.0.0", "1.1.0", "1.2.3", "2.0.0"},
			constraint: "^1.0.0",
			want:       []string{"1.2.3", "1.1.0", "1.0.0"},
		},
		{
			name:       "tilde range",
			versions:   []string{"1.0.0", "1.0.5", "1.1.0", "2.0.0"},
			constraint: "~1.0.0",
			want:       []string{"1.0.5", "1.0.0"},
		},
		{
			name:       "greater than or equal",
			versions:   []string{"0.9.0", "1.0.0", "1.5.0", "2.0.0"},
			constraint: ">= 1.0.0",
			want:       []string{"2.0.0", "1.5.0", "1.0.0"},
		},
		{
			name:       "range between versions",
			versions:   []string{"0.9.0", "1.0.0", "1.5.0", "2.0.0", "2.1.0"},
			constraint: ">= 1.0.0, < 2.0.0",
			want:       []string{"1.5.0", "1.0.0"},
		},
		{
			name:       "terraform pessimistic ~> major.minor",
			versions:   []string{"1.0.0", "1.0.5", "1.1.0", "1.2.0", "2.0.0"},
			constraint: "~> 1.0",
			want:       []string{"1.0.5", "1.0.0"},
		},
		{
			name:       "no matches",
			versions:   []string{"1.0.0", "1.1.0"},
			constraint: ">= 2.0.0",
			want:       []string{},
		},
		{
			name:       "skip non-semver tags",
			versions:   []string{"1.0.0", "latest", "stable", "2.0.0", "not-a-version"},
			constraint: ">= 1.0.0",
			want:       []string{"2.0.0", "1.0.0"},
		},
		{
			name:       "empty versions list",
			versions:   []string{},
			constraint: "^1.0.0",
			want:       []string{},
		},
		{
			name:       "invalid constraint",
			versions:   []string{"1.0.0"},
			constraint: "not-valid!!!",
			wantErr:    true,
		},
		{
			name:       "wildcard",
			versions:   []string{"1.0.0", "2.0.0", "3.0.0"},
			constraint: "*",
			want:       []string{"3.0.0", "2.0.0", "1.0.0"},
		},
		{
			name:       "pre-release excluded by default",
			versions:   []string{"1.0.0", "1.1.0-alpha.1", "1.1.0", "2.0.0-rc.1"},
			constraint: "^1.0.0",
			want:       []string{"1.1.0", "1.0.0"},
		},
		{
			name:       "pre-release included with explicit constraint",
			versions:   []string{"1.0.0", "1.1.0-alpha.1", "1.1.0"},
			constraint: ">= 1.1.0-alpha.0, < 1.2.0",
			want:       []string{"1.1.0", "1.1.0-alpha.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := FilterVersions(tt.versions, tt.constraint)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if len(tt.want) == 0 {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestBestMatch(t *testing.T) {
	tests := []struct {
		name       string
		versions   []string
		constraint string
		want       string
		wantErr    bool
	}{
		{
			name:       "picks highest in range",
			versions:   []string{"1.0.0", "1.1.0", "1.2.0", "2.0.0"},
			constraint: "^1.0.0",
			want:       "1.2.0",
		},
		{
			name:       "no match returns empty",
			versions:   []string{"1.0.0"},
			constraint: ">= 2.0.0",
			want:       "",
		},
		{
			name:       "invalid constraint",
			versions:   []string{"1.0.0"},
			constraint: "bad!!!",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BestMatch(tt.versions, tt.constraint)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilterSemver(t *testing.T) {
	v100, _ := semver.NewVersion("1.0.0")
	v110, _ := semver.NewVersion("1.1.0")
	v200, _ := semver.NewVersion("2.0.0")

	versions := []*semver.Version{v100, nil, v110, v200}
	got, err := FilterSemver(versions, "^1.0.0")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "1.1.0", got[0].String())
	assert.Equal(t, "1.0.0", got[1].String())
}

func TestValidateConstraint(t *testing.T) {
	assert.NoError(t, ValidateConstraint("^1.0.0"))
	assert.NoError(t, ValidateConstraint(">= 1.0, < 2.0"))
	assert.NoError(t, ValidateConstraint("~> 1.0"))
	assert.Error(t, ValidateConstraint("bad!!!"))
}

func BenchmarkFilterVersions(b *testing.B) {
	versions := []string{
		"0.1.0", "0.2.0", "0.3.0", "1.0.0", "1.0.1", "1.0.2",
		"1.1.0", "1.2.0", "1.2.1", "1.3.0", "2.0.0", "2.1.0",
		"2.2.0", "3.0.0", "latest", "stable",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = FilterVersions(versions, "^1.0.0")
	}
}

func BenchmarkBestMatch(b *testing.B) {
	versions := []string{
		"0.1.0", "0.2.0", "1.0.0", "1.0.1", "1.1.0", "1.2.0",
		"2.0.0", "2.1.0", "3.0.0",
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = BestMatch(versions, ">= 1.0.0, < 2.0.0")
	}
}

// mockCatalog implements catalog.Catalog for testing ListCatalogVersions.
type mockCatalog struct {
	name      string
	artifacts []catalog.ArtifactInfo
	err       error
}

func (m *mockCatalog) Name() string { return m.name }

func (m *mockCatalog) List(_ context.Context, _ catalog.ArtifactKind, _ string) ([]catalog.ArtifactInfo, error) {
	return m.artifacts, m.err
}

func (m *mockCatalog) Store(context.Context, catalog.Reference, []byte, []byte, map[string]string, bool) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, nil
}

func (m *mockCatalog) Fetch(context.Context, catalog.Reference) ([]byte, catalog.ArtifactInfo, error) {
	return nil, catalog.ArtifactInfo{}, nil
}

func (m *mockCatalog) FetchWithBundle(context.Context, catalog.Reference) ([]byte, []byte, catalog.ArtifactInfo, error) {
	return nil, nil, catalog.ArtifactInfo{}, nil
}

func (m *mockCatalog) Resolve(context.Context, catalog.Reference) (catalog.ArtifactInfo, error) {
	return catalog.ArtifactInfo{}, nil
}

func (m *mockCatalog) Exists(context.Context, catalog.Reference) (bool, error) {
	return false, nil
}

func (m *mockCatalog) Delete(context.Context, catalog.Reference) error {
	return nil
}

func TestListCatalogVersions(t *testing.T) {
	v100 := semver.MustParse("1.0.0")
	v110 := semver.MustParse("1.1.0")
	v200 := semver.MustParse("2.0.0")

	makeArtifacts := func(versions ...*semver.Version) []catalog.ArtifactInfo {
		out := make([]catalog.ArtifactInfo, 0, len(versions))
		for _, v := range versions {
			out = append(out, catalog.ArtifactInfo{
				Reference: catalog.Reference{Name: "myapp", Version: v},
			})
		}
		return out
	}

	tests := []struct {
		name     string
		catalogs []catalog.Catalog
		want     []string
		wantErr  string
	}{
		{
			name: "first catalog returns versions",
			catalogs: []catalog.Catalog{
				&mockCatalog{name: "local", artifacts: makeArtifacts(v100, v110)},
			},
			want: []string{"1.0.0", "1.1.0"},
		},
		{
			name: "falls back to second catalog",
			catalogs: []catalog.Catalog{
				&mockCatalog{name: "local", artifacts: nil},
				&mockCatalog{name: "remote", artifacts: makeArtifacts(v200)},
			},
			want: []string{"2.0.0"},
		},
		{
			name: "skips erroring catalog and uses next",
			catalogs: []catalog.Catalog{
				&mockCatalog{name: "bad", err: errors.New("network error")},
				&mockCatalog{name: "good", artifacts: makeArtifacts(v110)},
			},
			want: []string{"1.1.0"},
		},
		{
			name: "all catalogs fail includes last error",
			catalogs: []catalog.Catalog{
				&mockCatalog{name: "bad1", err: errors.New("timeout")},
				&mockCatalog{name: "bad2", err: errors.New("auth failed")},
			},
			wantErr: "auth failed",
		},
		{
			name:     "no catalogs returns not found",
			catalogs: []catalog.Catalog{},
			wantErr:  "no versions of",
		},
		{
			name: "empty results from all catalogs",
			catalogs: []catalog.Catalog{
				&mockCatalog{name: "local", artifacts: nil},
				&mockCatalog{name: "remote", artifacts: nil},
			},
			wantErr: "no versions of",
		},
		{
			name: "skips catalog with artifacts but no semver versions",
			catalogs: []catalog.Catalog{
				&mockCatalog{name: "digest-only", artifacts: []catalog.ArtifactInfo{
					{Reference: catalog.Reference{Name: "myapp", Version: nil}},
				}},
				&mockCatalog{name: "versioned", artifacts: makeArtifacts(v100)},
			},
			want: []string{"1.0.0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ListCatalogVersions(context.Background(), tt.catalogs, catalog.ArtifactKindSolution, "myapp")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
