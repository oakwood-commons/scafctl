// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listingInput mixes built, cached, and remote rows across two versions.
func listingInput() []ArtifactInfo {
	return []ArtifactInfo{
		remoteRow("zeta", "2.0.0", "default", testRegistry),
		remoteRow("zeta", "1.0.0", "default", testRegistry),
		cachedRow("zeta", "2.0.0", testRegistry, testLocalDigest),
		builtRow("alpha", "1.0.0"),
		builtRow("alpha", "0.9.0"),
	}
}

func TestBuildListing(t *testing.T) {
	t.Parallel()

	t.Run("latest per origin, sorted", func(t *testing.T) {
		t.Parallel()
		got, err := BuildListing(context.Background(), listingInput(), ListingOptions{})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "alpha", got[0].Reference.Name)
		assert.Equal(t, "1.0.0", got[0].Reference.Version.String())
		assert.True(t, got[0].IsBuilt())
		assert.Equal(t, "zeta", got[1].Reference.Name)
		assert.Equal(t, "2.0.0", got[1].Reference.Version.String())
		assert.Equal(t, []ArtifactLocation{LocationLocal, LocationRemote}, got[1].ResolvedLocations())
	})

	t.Run("all versions keeps every row", func(t *testing.T) {
		t.Parallel()
		got, err := BuildListing(context.Background(), listingInput(), ListingOptions{AllVersions: true})
		require.NoError(t, err)
		require.Len(t, got, 4)
		assert.Equal(t, "0.9.0", got[1].Reference.Version.String())
		assert.Equal(t, "1.0.0", got[3].Reference.Version.String())
	})

	t.Run("stale cached copy is split", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{digests: map[string]string{"zeta": testRemoteDigest}}
		got, err := BuildListing(context.Background(), listingInput(), ListingOptions{
			Resolvers:   map[string]DigestResolver{testRegistry: r},
			Concurrency: 2,
		})
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, int32(1), r.calls.Load())

		var stale, remote int
		for _, a := range got {
			if a.Reference.Name != "zeta" {
				continue
			}
			if a.Stale {
				stale++
				assert.Equal(t, []ArtifactLocation{LocationLocal}, a.ResolvedLocations())
			} else {
				remote++
				assert.Equal(t, testRemoteDigest, a.Digest)
			}
		}
		assert.Equal(t, 1, stale)
		assert.Equal(t, 1, remote)
	})

	t.Run("only latest rows are verified", func(t *testing.T) {
		t.Parallel()
		in := append(listingInput(), cachedRow("zeta", "1.0.0", testRegistry, testLocalDigest))
		r := &fakeDigestResolver{digests: map[string]string{"zeta": testLocalDigest}}
		_, err := BuildListing(context.Background(), in, ListingOptions{
			Resolvers: map[string]DigestResolver{testRegistry: r},
		})
		require.NoError(t, err)
		assert.Equal(t, int32(1), r.calls.Load())
	})

	t.Run("include filter runs after verification", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{digests: map[string]string{"zeta": testRemoteDigest}}
		got, err := BuildListing(context.Background(), listingInput(), ListingOptions{
			Resolvers: map[string]DigestResolver{testRegistry: r},
			Include:   func(a ArtifactInfo) bool { return a.HasLocation(LocationLocal) },
		})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.True(t, got[0].IsBuilt())
		assert.True(t, got[1].Stale)
	})

	t.Run("verification error is returned with rows", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		r := &fakeDigestResolver{errs: map[string]error{"zeta": boom}}
		got, err := BuildListing(context.Background(), listingInput(), ListingOptions{
			Resolvers: map[string]DigestResolver{testRegistry: r},
		})
		require.ErrorIs(t, err, boom)
		require.Len(t, got, 2)
		assert.False(t, got[1].Stale)
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		got, err := BuildListing(context.Background(), nil, ListingOptions{})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("input is not mutated", func(t *testing.T) {
		t.Parallel()
		in := listingInput()
		want := listingInput()
		_, err := BuildListing(context.Background(), in, ListingOptions{})
		require.NoError(t, err)
		assert.Equal(t, want, in)
	})
}

func BenchmarkBuildListing(b *testing.B) {
	in := listingInput()
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = BuildListing(ctx, in, ListingOptions{})
	}
}
