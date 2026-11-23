// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"strings"
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func artifact(name, version string, kind ArtifactKind, cat string) ArtifactInfo {
	return ArtifactInfo{
		Reference: Reference{Name: name, Kind: kind, Version: semver.MustParse(version)},
		Catalog:   cat,
	}
}

func TestFilterByVersionConstraint_EmptyReturnsAll(t *testing.T) {
	in := []ArtifactInfo{artifact("a", "1.0.0", ArtifactKindProvider, "c")}
	got, err := FilterByVersionConstraint(in, "")
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestFilterByVersionConstraint_Matches(t *testing.T) {
	in := []ArtifactInfo{
		artifact("gh", "1.0.0", ArtifactKindProvider, "c"),
		artifact("gh", "1.5.0", ArtifactKindProvider, "c"),
		artifact("gh", "2.0.0", ArtifactKindProvider, "c"),
	}
	got, err := FilterByVersionConstraint(in, ">=1.5.0")
	require.NoError(t, err)
	require.Len(t, got, 2)
	// descending order (newest first)
	assert.Equal(t, "2.0.0", got[0].Reference.Version.String())
	assert.Equal(t, "1.5.0", got[1].Reference.Version.String())
}

func TestFilterByVersionConstraint_InvalidConstraint(t *testing.T) {
	in := []ArtifactInfo{artifact("a", "1.0.0", ArtifactKindProvider, "c")}
	_, err := FilterByVersionConstraint(in, "not-a-constraint")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid version constraint")
}

func TestFilterByVersionConstraint_SkipsNilVersions(t *testing.T) {
	in := []ArtifactInfo{
		{Reference: Reference{Name: "novers", Kind: ArtifactKindProvider}}, // nil version
		artifact("gh", "1.0.0", ArtifactKindProvider, "c"),
	}
	got, err := FilterByVersionConstraint(in, ">=1.0.0")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "gh", got[0].Reference.Name)
}

const (
	testRegistry      = "ghcr.io/org/solutions"
	testOtherRegistry = "ghcr.io/other/solutions"
	testLocalDigest   = "sha256:aaaa"
	testRemoteDigest  = "sha256:bbbb"
)

// builtRow is a local-store row with no remote provenance.
func builtRow(name, version string) ArtifactInfo {
	a := artifact(name, version, ArtifactKindSolution, LocalCatalogName)
	a.Digest = testLocalDigest
	a.CreatedAt = time.Unix(1000, 0)
	return a
}

// cachedRow is a local-store copy of an artifact pulled from canonical.
func cachedRow(name, version, canonical, digest string) ArtifactInfo {
	a := artifact(name, version, ArtifactKindSolution, LocalCatalogName)
	a.Canonical = canonical
	a.Digest = digest
	a.CreatedAt = time.Unix(2000, 0)
	return a
}

// remoteRow is a row listed from a remote catalog (no digest, like List).
func remoteRow(name, version, alias, canonical string) ArtifactInfo {
	a := artifact(name, version, ArtifactKindSolution, alias)
	a.Canonical = canonical
	return a
}

func TestDeduplicateArtifacts(t *testing.T) {
	t.Parallel()

	type wantRow struct {
		catalog   string
		canonical string
		digest    string
		locations []ArtifactLocation
		stale     bool
	}

	stale := cachedRow("foo", "1.0.0", testRegistry, testLocalDigest)
	staleRemote := remoteRow("foo", "1.0.0", "default", testRegistry)
	staleRemote.Digest = testRemoteDigest

	sameDigestRemote := remoteRow("foo", "1.0.0", "default", testRegistry)
	sameDigestRemote.Digest = testLocalDigest

	tests := map[string]struct {
		in   []ArtifactInfo
		want []wantRow
	}{
		"1 built only": {
			in: []ArtifactInfo{builtRow("foo", "1.0.0")},
			want: []wantRow{
				{catalog: LocalCatalogName, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal}},
			},
		},
		"2 built is never merged with remote": {
			in: []ArtifactInfo{builtRow("foo", "1.0.0"), remoteRow("foo", "1.0.0", "default", testRegistry)},
			want: []wantRow{
				{catalog: LocalCatalogName, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal}},
				{catalog: "default", canonical: testRegistry, locations: []ArtifactLocation{LocationRemote}},
			},
		},
		"4 remote only": {
			in: []ArtifactInfo{remoteRow("foo", "1.0.0", "default", testRegistry)},
			want: []wantRow{
				{catalog: "default", canonical: testRegistry, locations: []ArtifactLocation{LocationRemote}},
			},
		},
		"5 same version in two registries": {
			in: []ArtifactInfo{
				remoteRow("foo", "1.0.0", "default", testRegistry),
				remoteRow("foo", "1.0.0", "other", testOtherRegistry),
			},
			want: []wantRow{
				{catalog: "default", canonical: testRegistry, locations: []ArtifactLocation{LocationRemote}},
				{catalog: "other", canonical: testOtherRegistry, locations: []ArtifactLocation{LocationRemote}},
			},
		},
		"7 cached and remote merge when remote digest unknown": {
			in: []ArtifactInfo{
				cachedRow("foo", "1.0.0", testRegistry, testLocalDigest),
				remoteRow("foo", "1.0.0", "default", testRegistry),
			},
			want: []wantRow{
				{catalog: "default", canonical: testRegistry, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal, LocationRemote}},
			},
		},
		"7 cached and remote merge when digests match": {
			in: []ArtifactInfo{cachedRow("foo", "1.0.0", testRegistry, testLocalDigest), sameDigestRemote},
			want: []wantRow{
				{catalog: "default", canonical: testRegistry, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal, LocationRemote}},
			},
		},
		"7 canonical match is case-insensitive": {
			in: []ArtifactInfo{
				cachedRow("foo", "1.0.0", "GHCR.io/Org/Solutions", testLocalDigest),
				remoteRow("foo", "1.0.0", "default", testRegistry),
			},
			want: []wantRow{
				{catalog: "default", canonical: testRegistry, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal, LocationRemote}},
			},
		},
		"8 differing known digests split and mark local stale": {
			in: []ArtifactInfo{stale, staleRemote},
			want: []wantRow{
				{catalog: LocalCatalogName, canonical: testRegistry, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal}, stale: true},
				{catalog: "default", canonical: testRegistry, digest: testRemoteDigest, locations: []ArtifactLocation{LocationRemote}},
			},
		},
		"9 cached only": {
			in: []ArtifactInfo{cachedRow("foo", "1.0.0", testRegistry, testLocalDigest)},
			want: []wantRow{
				{catalog: LocalCatalogName, canonical: testRegistry, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal}},
			},
		},
		"cached from another registry does not merge": {
			in: []ArtifactInfo{
				cachedRow("foo", "1.0.0", testOtherRegistry, testLocalDigest),
				remoteRow("foo", "1.0.0", "default", testRegistry),
			},
			want: []wantRow{
				{catalog: LocalCatalogName, canonical: testOtherRegistry, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal}},
				{catalog: "default", canonical: testRegistry, locations: []ArtifactLocation{LocationRemote}},
			},
		},
		"same remote listed twice collapses": {
			in: []ArtifactInfo{
				remoteRow("foo", "1.0.0", "default", testRegistry),
				remoteRow("foo", "1.0.0", "default", testRegistry),
			},
			want: []wantRow{
				{catalog: "default", canonical: testRegistry, locations: []ArtifactLocation{LocationRemote}},
			},
		},
		"26 non-local rows without canonical are keyed by catalog": {
			in: []ArtifactInfo{
				artifact("foo", "1.0.0", ArtifactKindSolution, "fs-a"),
				artifact("foo", "1.0.0", ArtifactKindSolution, "fs-b"),
				builtRow("foo", "1.0.0"),
			},
			want: []wantRow{
				{catalog: "fs-a", locations: []ArtifactLocation{LocationRemote}},
				{catalog: "fs-b", locations: []ArtifactLocation{LocationRemote}},
				{catalog: LocalCatalogName, digest: testLocalDigest, locations: []ArtifactLocation{LocationLocal}},
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := DeduplicateArtifacts(tc.in)
			require.Len(t, got, len(tc.want))
			for i, w := range tc.want {
				assert.Equal(t, w.catalog, got[i].Catalog, "row %d catalog", i)
				assert.Equal(t, w.canonical, strings.ToLower(got[i].Canonical), "row %d canonical", i)
				assert.Equal(t, w.digest, got[i].Digest, "row %d digest", i)
				assert.Equal(t, w.locations, got[i].Locations, "row %d locations", i)
				assert.Equal(t, w.stale, got[i].Stale, "row %d stale", i)
			}
		})
	}
}

func TestDeduplicateArtifacts_MergedRowKeepsLocalMetadata(t *testing.T) {
	t.Parallel()

	cached := cachedRow("foo", "1.0.0", testRegistry, testLocalDigest)
	cached.Annotations = map[string]string{"local": "1", "shared": "local"}
	remote := remoteRow("foo", "1.0.0", "default", testRegistry)
	remote.Annotations = map[string]string{"remote": "1", "shared": "remote"}

	got := DeduplicateArtifacts([]ArtifactInfo{remote, cached})
	require.Len(t, got, 1)
	assert.Equal(t, time.Unix(2000, 0), got[0].CreatedAt, "createdAt filled from cached copy")
	assert.Equal(t, "1", got[0].Annotations["local"])
	assert.Equal(t, "1", got[0].Annotations["remote"])
	assert.Equal(t, map[string]string{"remote": "1", "shared": "remote"}, remote.Annotations, "input annotations must not be mutated")
}

func TestDeduplicateArtifacts_InputOrderIndependent(t *testing.T) {
	t.Parallel()

	cached := cachedRow("foo", "1.0.0", testRegistry, testLocalDigest)
	remote := remoteRow("foo", "1.0.0", "default", testRegistry)

	a := DeduplicateArtifacts([]ArtifactInfo{cached, remote})
	b := DeduplicateArtifacts([]ArtifactInfo{remote, cached})
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	assert.Equal(t, a[0].Catalog, b[0].Catalog)
	assert.Equal(t, a[0].Digest, b[0].Digest)
	assert.Equal(t, a[0].Locations, b[0].Locations)
	assert.Equal(t, a[0].CreatedAt, b[0].CreatedAt)
}

func TestDeduplicateArtifacts_DistinctKindsKept(t *testing.T) {
	t.Parallel()

	// Same name+version but different kinds must NOT be merged.
	p := artifact("shared", "1.0.0", ArtifactKindProvider, "c")
	h := artifact("shared", "1.0.0", ArtifactKindAuthHandler, "c")
	got := DeduplicateArtifacts([]ArtifactInfo{p, h})
	assert.Len(t, got, 2)
}

func TestDeduplicateArtifacts_Idempotent(t *testing.T) {
	t.Parallel()

	in := []ArtifactInfo{
		cachedRow("foo", "1.0.0", testRegistry, testLocalDigest),
		remoteRow("foo", "1.0.0", "default", testRegistry),
		builtRow("foo", "1.0.0"),
	}
	once := DeduplicateArtifacts(in)
	twice := DeduplicateArtifacts(once)
	assert.Equal(t, once, twice)
}

func TestLatestPerOrigin(t *testing.T) {
	t.Parallel()

	staleLocal := cachedRow("foo", "1.3.0", testRegistry, testLocalDigest)
	staleLocal.Stale = true
	staleLocal.Locations = []ArtifactLocation{LocationLocal}
	staleRemote := remoteRow("foo", "1.3.0", "default", testRegistry)
	staleRemote.Locations = []ArtifactLocation{LocationRemote}

	noVersion := ArtifactInfo{Reference: Reference{Name: "nov", Kind: ArtifactKindSolution}, Tag: "stable", Catalog: "default"}
	noVersion2 := ArtifactInfo{Reference: Reference{Name: "nov", Kind: ArtifactKindSolution}, Tag: "edge", Catalog: "default"}

	tests := map[string]struct {
		in   []ArtifactInfo
		want []string // name@version/catalog
	}{
		"17 older cached version hidden behind newer remote": {
			in: []ArtifactInfo{
				remoteRow("foo", "1.3.0", "default", testRegistry),
				cachedRow("foo", "1.2.0", testRegistry, testLocalDigest),
			},
			want: []string{"foo@1.3.0/default"},
		},
		"18 every row at the top version is kept": {
			in: []ArtifactInfo{
				staleLocal,
				staleRemote,
				remoteRow("foo", "1.2.0", "default", testRegistry),
			},
			want: []string{"foo@1.3.0/local", "foo@1.3.0/default"},
		},
		"19 different origins each keep their latest": {
			in: []ArtifactInfo{
				builtRow("foo", "2.0.0"),
				builtRow("foo", "1.0.0"),
				remoteRow("foo", "1.3.0", "default", testRegistry),
			},
			want: []string{"foo@2.0.0/local", "foo@1.3.0/default"},
		},
		"different kinds are independent": {
			in: []ArtifactInfo{
				artifact("foo", "1.0.0", ArtifactKindProvider, "default"),
				artifact("foo", "2.0.0", ArtifactKindAuthHandler, "default"),
			},
			want: []string{"foo@1.0.0/default", "foo@2.0.0/default"},
		},
		"unversioned rows dropped when a versioned row exists": {
			in: []ArtifactInfo{
				{Reference: Reference{Name: "foo", Kind: ArtifactKindSolution}, Tag: "stable", Catalog: "default", Canonical: testRegistry},
				remoteRow("foo", "1.0.0", "default", testRegistry),
			},
			want: []string{"foo@1.0.0/default"},
		},
		"all unversioned rows kept": {
			in:   []ArtifactInfo{noVersion, noVersion2},
			want: []string{"nov@/default", "nov@/default"},
		},
		"empty": {in: nil, want: []string{}},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := LatestPerOrigin(tc.in)
			ids := make([]string, len(got))
			for i, a := range got {
				v := ""
				if a.Reference.Version != nil {
					v = a.Reference.Version.String()
				}
				ids[i] = a.Reference.Name + "@" + v + "/" + a.Catalog
			}
			assert.Equal(t, tc.want, ids)
		})
	}
}

func TestSortArtifacts(t *testing.T) {
	t.Parallel()

	noVersion := ArtifactInfo{Reference: Reference{Name: "foo", Kind: ArtifactKindSolution}, Catalog: "default"}
	in := []ArtifactInfo{
		noVersion,
		remoteRow("foo", "1.0.0", "default", testRegistry),
		builtRow("foo", "1.0.0"),
		remoteRow("foo", "2.0.0", "default", testRegistry),
		remoteRow("bar", "1.0.0", "default", testRegistry),
		artifact("foo", "3.0.0", ArtifactKindProvider, "default"),
	}

	SortArtifacts(in)

	ids := make([]string, len(in))
	for i, a := range in {
		v := "-"
		if a.Reference.Version != nil {
			v = a.Reference.Version.String()
		}
		ids[i] = a.Reference.Name + "/" + string(a.Reference.Kind) + "@" + v + "/" + a.Catalog
	}
	assert.Equal(t, []string{
		"bar/solution@1.0.0/default",
		"foo/provider@3.0.0/default",
		"foo/solution@2.0.0/default",
		"foo/solution@1.0.0/local",
		"foo/solution@1.0.0/default",
		"foo/solution@-/default",
	}, ids)
}

func benchArtifacts(n int) []ArtifactInfo {
	out := make([]ArtifactInfo, 0, n)
	for i := 0; i < n; i++ {
		v := semver.MustParse("1." + string(rune('0'+i%10)) + ".0")
		a := ArtifactInfo{
			Reference: Reference{Name: "plugin", Kind: ArtifactKindProvider, Version: v},
			Catalog:   "cat-" + string(rune('a'+i%3)),
		}
		if i%3 == 0 {
			a.Catalog = LocalCatalogName
		}
		a.Canonical = "ghcr.io/org/" + string(rune('a'+i%2))
		out = append(out, a)
	}
	return out
}

func BenchmarkFilterByVersionConstraint(b *testing.B) {
	in := benchArtifacts(100)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = FilterByVersionConstraint(in, ">=1.5.0")
	}
}

func BenchmarkDeduplicateArtifacts(b *testing.B) {
	in := benchArtifacts(100)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = DeduplicateArtifacts(in)
	}
}

func BenchmarkLatestPerOrigin(b *testing.B) {
	in := benchArtifacts(100)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = LatestPerOrigin(in)
	}
}
