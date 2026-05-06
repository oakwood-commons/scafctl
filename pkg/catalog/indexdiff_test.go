// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffIndex_AllChangeTypes(t *testing.T) {
	t.Parallel()

	current := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "hello-world", LatestVersion: "1.0.0"},
		{Kind: ArtifactKindSolution, Name: "starter-kit", LatestVersion: "2.0.0"},
		{Kind: ArtifactKindProvider, Name: "terraform", LatestVersion: "0.5.0"},
	}

	next := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "hello-world", LatestVersion: "1.1.0"}, // version changed
		{Kind: ArtifactKindSolution, Name: "starter-kit", LatestVersion: "2.0.0"}, // unchanged
		{Kind: ArtifactKindSolution, Name: "new-app", LatestVersion: "0.1.0"},     // added
		{Kind: ArtifactKindAuthHandler, Name: "gcp-auth", LatestVersion: "1.0.0"}, // added
		// terraform removed
	}

	diff := DiffIndex(current, next)

	assert.Equal(t, 2, diff.Added)
	assert.Equal(t, 1, diff.Removed)
	assert.Equal(t, 1, diff.Changed)
	assert.Equal(t, 4, diff.Total)
	require.Len(t, diff.Entries, 5)

	// Sorted: added first, then version-changed, then removed, then unchanged.
	assert.Equal(t, IndexDiffAdded, diff.Entries[0].Change)
	assert.Equal(t, "gcp-auth", diff.Entries[0].Name)

	assert.Equal(t, IndexDiffAdded, diff.Entries[1].Change)
	assert.Equal(t, "new-app", diff.Entries[1].Name)
	assert.Equal(t, "0.1.0", diff.Entries[1].LatestVersion)

	assert.Equal(t, IndexDiffVersionChanged, diff.Entries[2].Change)
	assert.Equal(t, "hello-world", diff.Entries[2].Name)
	assert.Equal(t, "1.1.0", diff.Entries[2].LatestVersion)
	assert.Equal(t, "1.0.0", diff.Entries[2].PrevVersion)

	assert.Equal(t, IndexDiffRemoved, diff.Entries[3].Change)
	assert.Equal(t, "terraform", diff.Entries[3].Name)

	assert.Equal(t, IndexDiffUnchanged, diff.Entries[4].Change)
	assert.Equal(t, "starter-kit", diff.Entries[4].Name)
}

func TestDiffIndex_EmptyCurrent(t *testing.T) {
	t.Parallel()

	next := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "app", LatestVersion: "1.0.0"},
	}

	diff := DiffIndex(nil, next)

	assert.Equal(t, 1, diff.Added)
	assert.Equal(t, 0, diff.Removed)
	assert.Equal(t, 0, diff.Changed)
	require.Len(t, diff.Entries, 1)
	assert.Equal(t, IndexDiffAdded, diff.Entries[0].Change)
}

func TestDiffIndex_EmptyNext(t *testing.T) {
	t.Parallel()

	current := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "app", LatestVersion: "1.0.0"},
	}

	diff := DiffIndex(current, nil)

	assert.Equal(t, 0, diff.Added)
	assert.Equal(t, 1, diff.Removed)
	assert.Equal(t, 0, diff.Changed)
	assert.Equal(t, 0, diff.Total)
	require.Len(t, diff.Entries, 1)
	assert.Equal(t, IndexDiffRemoved, diff.Entries[0].Change)
}

func TestDiffIndex_BothEmpty(t *testing.T) {
	t.Parallel()

	diff := DiffIndex(nil, nil)

	assert.Equal(t, 0, diff.Added)
	assert.Equal(t, 0, diff.Removed)
	assert.Equal(t, 0, diff.Changed)
	assert.Equal(t, 0, diff.Total)
	assert.Empty(t, diff.Entries)
}

func TestDiffIndex_Identical(t *testing.T) {
	t.Parallel()

	artifacts := []DiscoveredArtifact{
		{Kind: ArtifactKindSolution, Name: "app", LatestVersion: "1.0.0"},
		{Kind: ArtifactKindProvider, Name: "tf", LatestVersion: "2.0.0"},
	}

	diff := DiffIndex(artifacts, artifacts)

	assert.Equal(t, 0, diff.Added)
	assert.Equal(t, 0, diff.Removed)
	assert.Equal(t, 0, diff.Changed)
	assert.Equal(t, 2, diff.Total)
	require.Len(t, diff.Entries, 2)
	for _, e := range diff.Entries {
		assert.Equal(t, IndexDiffUnchanged, e.Change)
	}
}

func BenchmarkDiffIndex(b *testing.B) {
	current := make([]DiscoveredArtifact, 100)
	next := make([]DiscoveredArtifact, 110)
	for i := range current {
		current[i] = DiscoveredArtifact{Kind: ArtifactKindSolution, Name: "app-" + string(rune('a'+i%26)), LatestVersion: "1.0.0"}
	}
	for i := range next {
		next[i] = DiscoveredArtifact{Kind: ArtifactKindSolution, Name: "app-" + string(rune('a'+i%26)), LatestVersion: "1.1.0"}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		DiffIndex(current, next)
	}
}
