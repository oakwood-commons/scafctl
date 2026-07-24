// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
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

func TestDeduplicateArtifacts_MergesAcrossCatalogs(t *testing.T) {
	a1 := artifact("gh", "1.0.0", ArtifactKindProvider, "cat-a")
	a2 := artifact("gh", "1.0.0", ArtifactKindProvider, "cat-b")
	a2.Digest = "sha256:deadbeef"
	a2.CreatedAt = time.Unix(1000, 0)
	a2.Annotations = map[string]string{"k": "v"}

	got := DeduplicateArtifacts([]ArtifactInfo{a1, a2})
	require.Len(t, got, 1, "same name+tag+kind must collapse to one row")
	assert.Equal(t, "cat-a, cat-b", got[0].Catalog, "catalog names combined")
	assert.Equal(t, "sha256:deadbeef", got[0].Digest, "richer digest preferred")
	assert.Equal(t, "v", got[0].Annotations["k"], "annotations merged")
}

func TestDeduplicateArtifacts_DistinctKindsKept(t *testing.T) {
	// Same name+version but different kinds must NOT be merged.
	p := artifact("shared", "1.0.0", ArtifactKindProvider, "c")
	h := artifact("shared", "1.0.0", ArtifactKindAuthHandler, "c")
	got := DeduplicateArtifacts([]ArtifactInfo{p, h})
	assert.Len(t, got, 2)
}

func TestDeduplicateArtifacts_PreservesOrderAndNoDuplicateCatalog(t *testing.T) {
	a1 := artifact("gh", "1.0.0", ArtifactKindProvider, "cat-a")
	a2 := artifact("gh", "1.0.0", ArtifactKindProvider, "cat-a") // same catalog twice
	got := DeduplicateArtifacts([]ArtifactInfo{a1, a2})
	require.Len(t, got, 1)
	assert.Equal(t, "cat-a", got[0].Catalog, "duplicate catalog name must not be repeated")
}

func benchArtifacts(n int) []ArtifactInfo {
	out := make([]ArtifactInfo, 0, n)
	for i := 0; i < n; i++ {
		v := semver.MustParse("1." + string(rune('0'+i%10)) + ".0")
		out = append(out, ArtifactInfo{
			Reference: Reference{Name: "plugin", Kind: ArtifactKindProvider, Version: v},
			Catalog:   "cat-" + string(rune('a'+i%3)),
		})
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
