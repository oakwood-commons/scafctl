// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDigestResolver returns a fixed digest (or error) per artifact name and
// records call counts and peak concurrency.
type fakeDigestResolver struct {
	digests  map[string]string
	errs     map[string]error
	delay    time.Duration
	calls    atomic.Int32
	inFlight atomic.Int32
	peak     atomic.Int32
	mu       sync.Mutex
	refs     []Reference
}

func (f *fakeDigestResolver) Resolve(_ context.Context, ref Reference) (ArtifactInfo, error) {
	f.calls.Add(1)
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	f.refs = append(f.refs, ref)
	f.mu.Unlock()
	if err := f.errs[ref.Name]; err != nil {
		return ArtifactInfo{}, err
	}
	return ArtifactInfo{Reference: ref, Digest: f.digests[ref.Name], Size: 42}, nil
}

// mergedRow is a cached copy merged with its remote row by DeduplicateArtifacts.
func mergedRow(name, digest string) ArtifactInfo {
	rows := DeduplicateArtifacts([]ArtifactInfo{
		cachedRow(name, "1.0.0", testRegistry, digest),
		remoteRow(name, "1.0.0", "default", testRegistry),
	})
	return rows[0]
}

func TestVerifyCachedDigests(t *testing.T) {
	t.Parallel()

	t.Run("matching digest keeps merged row", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{digests: map[string]string{"foo": testLocalDigest}}
		in := []ArtifactInfo{mergedRow("foo", testLocalDigest)}

		got, err := VerifyCachedDigests(t.Context(), in, map[string]DigestResolver{testRegistry: r}, 2)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, []ArtifactLocation{LocationLocal, LocationRemote}, got[0].Locations)
		assert.False(t, got[0].Stale)
		assert.Equal(t, int32(1), r.calls.Load())
		require.Len(t, r.refs, 1)
		assert.Equal(t, "foo", r.refs[0].Name)
		assert.Equal(t, ArtifactKindSolution, r.refs[0].Kind)
		assert.Equal(t, "1.0.0", r.refs[0].Version.String())
	})

	t.Run("differing digest splits into stale local and remote rows", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{digests: map[string]string{"foo": testRemoteDigest}}
		in := []ArtifactInfo{mergedRow("foo", testLocalDigest)}

		got, err := VerifyCachedDigests(t.Context(), in, map[string]DigestResolver{testRegistry: r}, 2)
		require.NoError(t, err)
		require.Len(t, got, 2)

		local, remote := got[0], got[1]
		assert.Equal(t, []ArtifactLocation{LocationLocal}, local.Locations)
		assert.True(t, local.Stale)
		assert.Equal(t, testLocalDigest, local.Digest)
		assert.Equal(t, LocalCatalogName, local.Catalog)
		assert.Equal(t, testRegistry, local.Canonical)

		assert.Equal(t, []ArtifactLocation{LocationRemote}, remote.Locations)
		assert.False(t, remote.Stale)
		assert.Equal(t, testRemoteDigest, remote.Digest)
		assert.Equal(t, "default", remote.Catalog)
		assert.Equal(t, testRegistry, remote.Canonical)
		assert.True(t, remote.CreatedAt.IsZero(), "remote createdAt is unknown")
		assert.Equal(t, int64(42), remote.Size)

		// The split result is stable under a second dedup pass.
		again := DeduplicateArtifacts(got)
		require.Len(t, again, 2)
		assert.True(t, again[0].Stale)
	})

	t.Run("resolve error leaves row merged and unverified", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		r := &fakeDigestResolver{errs: map[string]error{"foo": boom}}
		in := []ArtifactInfo{mergedRow("foo", testLocalDigest)}

		got, err := VerifyCachedDigests(t.Context(), in, map[string]DigestResolver{testRegistry: r}, 2)
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "foo")
		require.Len(t, got, 1)
		assert.Equal(t, []ArtifactLocation{LocationLocal, LocationRemote}, got[0].Locations)
		assert.False(t, got[0].Stale)
	})

	t.Run("canonical lookup is case-insensitive", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{digests: map[string]string{"foo": testLocalDigest}}
		in := []ArtifactInfo{mergedRow("foo", testLocalDigest)}

		_, err := VerifyCachedDigests(t.Context(), in, map[string]DigestResolver{"GHCR.io/Org/Solutions": r}, 2)
		require.NoError(t, err)
		assert.Equal(t, int32(1), r.calls.Load())
	})

	t.Run("rows that need no verification are untouched", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{}
		noDigest := mergedRow("nodigest", "")
		noVersion := mergedRow("noversion", testLocalDigest)
		noVersion.Reference.Version = nil
		otherOrigin := DeduplicateArtifacts([]ArtifactInfo{
			cachedRow("other", "1.0.0", testOtherRegistry, testLocalDigest),
			remoteRow("other", "1.0.0", "other", testOtherRegistry),
		})[0]
		in := []ArtifactInfo{
			builtRow("built", "1.0.0"),
			cachedRow("cached", "1.0.0", testRegistry, testLocalDigest),
			remoteRow("remote", "1.0.0", "default", testRegistry),
			noDigest,
			noVersion,
			otherOrigin,
		}

		got, err := VerifyCachedDigests(t.Context(), in, map[string]DigestResolver{testRegistry: r}, 2)
		require.NoError(t, err)
		assert.Equal(t, in, got)
		assert.Equal(t, int32(0), r.calls.Load())
	})

	t.Run("no resolvers is a no-op", func(t *testing.T) {
		t.Parallel()
		in := []ArtifactInfo{mergedRow("foo", testLocalDigest)}
		got, err := VerifyCachedDigests(t.Context(), in, nil, 2)
		require.NoError(t, err)
		assert.Equal(t, in, got)
	})

	t.Run("input is not mutated", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{digests: map[string]string{"foo": testRemoteDigest}}
		in := []ArtifactInfo{mergedRow("foo", testLocalDigest)}
		before := in[0]
		before.Locations = append([]ArtifactLocation(nil), in[0].Locations...)

		_, err := VerifyCachedDigests(t.Context(), in, map[string]DigestResolver{testRegistry: r}, 2)
		require.NoError(t, err)
		assert.Equal(t, before, in[0])
	})

	t.Run("concurrency is bounded", func(t *testing.T) {
		t.Parallel()
		const rows, limit = 12, 3
		r := &fakeDigestResolver{digests: map[string]string{}, delay: 5 * time.Millisecond}
		in := make([]ArtifactInfo, 0, rows)
		for i := range rows {
			name := "foo" + string(rune('a'+i))
			r.digests[name] = testLocalDigest
			in = append(in, mergedRow(name, testLocalDigest))
		}

		got, err := VerifyCachedDigests(t.Context(), in, map[string]DigestResolver{testRegistry: r}, limit)
		require.NoError(t, err)
		assert.Len(t, got, rows)
		assert.Equal(t, int32(rows), r.calls.Load())
		assert.LessOrEqual(t, r.peak.Load(), int32(limit))
	})

	t.Run("cancelled context skips resolves and keeps rows merged", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{digests: map[string]string{"foo": testRemoteDigest, "bar": testRemoteDigest}}
		in := []ArtifactInfo{mergedRow("foo", testLocalDigest), mergedRow("bar", testLocalDigest)}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		got, err := VerifyCachedDigests(ctx, in, map[string]DigestResolver{testRegistry: r}, 1)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, in, got)
		assert.Equal(t, int32(0), r.calls.Load())
	})

	t.Run("non-positive concurrency still verifies", func(t *testing.T) {
		t.Parallel()
		r := &fakeDigestResolver{digests: map[string]string{"foo": testLocalDigest}}
		in := []ArtifactInfo{mergedRow("foo", testLocalDigest)}

		_, err := VerifyCachedDigests(t.Context(), in, map[string]DigestResolver{testRegistry: r}, 0)
		require.NoError(t, err)
		assert.Equal(t, int32(1), r.calls.Load())
		assert.Equal(t, int32(1), r.peak.Load())
	})
}

func BenchmarkVerifyCachedDigests(b *testing.B) {
	r := &fakeDigestResolver{digests: map[string]string{}}
	in := make([]ArtifactInfo, 0, 50)
	for i := range 50 {
		name := "foo" + string(rune('a'+i%26)) + string(rune('a'+i/26))
		r.digests[name] = testLocalDigest
		in = append(in, mergedRow(name, testLocalDigest))
	}
	resolvers := map[string]DigestResolver{testRegistry: r}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = VerifyCachedDigests(ctx, in, resolvers, 5)
	}
}
