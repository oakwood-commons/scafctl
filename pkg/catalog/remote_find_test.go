// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoteCatalog_Find(t *testing.T) {
	t.Parallel()

	const name = "find-prov"
	stored := []string{"1.0.0", "1.2.0", "1.5.0", "2.0.0-beta.1"}

	cat, ts := newTestRemoteCatalog(t)
	// Cleanup (not defer) so the server outlives the parallel subtests.
	t.Cleanup(ts.Close)

	ctx := t.Context()
	digests := make(map[string]string, len(stored))
	for _, v := range stored {
		info, err := cat.Store(ctx, testRef(name, v), []byte("v"+v), nil, nil, false)
		require.NoError(t, err)
		digests[v] = info.Digest
	}

	tests := []struct {
		name        string
		query       ArtifactQuery
		preRelease  bool
		wantVersion string
		wantErr     error
	}{
		{
			name:        "open version prefers newest stable",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider},
			wantVersion: "1.5.0",
		},
		{
			name:        "latest keyword prefers newest stable",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "latest"},
			wantVersion: "1.5.0",
		},
		{
			name:        "open version with pre-release opt-in",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider},
			preRelease:  true,
			wantVersion: "2.0.0-beta.1",
		},
		{
			name:        "exact version",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "1.2.0"},
			wantVersion: "1.2.0",
		},
		{
			name:        "constraint picks highest match",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "~1.2"},
			wantVersion: "1.2.0",
		},
		{
			name:        "caret constraint",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "^1.0.0"},
			wantVersion: "1.5.0",
		},
		{
			name:        "range constraint excludes pre-release without opt-in",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: ">=1.0.0"},
			wantVersion: "1.5.0",
		},
		{
			name:        "range constraint includes pre-release with opt-in",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: ">=1.0.0"},
			preRelease:  true,
			wantVersion: "2.0.0-beta.1",
		},
		{
			name:        "exact pre-release version resolves without opt-in",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "2.0.0-beta.1"},
			wantVersion: "2.0.0-beta.1",
		},
		{
			name:    "constraint with no match",
			query:   ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: ">=3.0.0"},
			wantErr: ErrNoMatchingVersion,
		},
		{
			name:    "exact version absent",
			query:   ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "9.9.9"},
			wantErr: ErrNoMatchingVersion,
		},
		{
			name:    "unknown repository",
			query:   ArtifactQuery{Name: "no-such-prov", Kind: ArtifactKindProvider},
			wantErr: ErrArtifactNotFound,
		},
		{
			name:    "wrong kind",
			query:   ArtifactQuery{Name: name, Kind: ArtifactKindSolution},
			wantErr: ErrArtifactNotFound,
		},
		{
			name:        "digest without version",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Digest: digests["1.0.0"]},
			wantVersion: "1.0.0",
		},
		{
			name:        "digest with matching constraint",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "^1.0.0", Digest: digests["1.2.0"]},
			wantVersion: "1.2.0",
		},
		{
			name:    "digest outside constraint",
			query:   ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "~1.5", Digest: digests["1.0.0"]},
			wantErr: ErrArtifactNotFound,
		},
		{
			name:        "remote source matching this catalog",
			query:       ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Source: SourceSelector{Kind: SourceRemote, Remote: cat.canonicalID()}},
			wantVersion: "1.5.0",
		},
		{
			name:    "remote source naming another catalog",
			query:   ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Source: SourceSelector{Kind: SourceRemote, Remote: "other.example.com/repo"}},
			wantErr: ErrArtifactNotFound,
		},
		{
			name:    "built source never matches a remote",
			query:   ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Source: SourceSelector{Kind: SourceBuilt}},
			wantErr: ErrArtifactNotFound,
		},
		{
			name:    "missing name",
			query:   ArtifactQuery{Kind: ArtifactKindProvider},
			wantErr: ErrInvalidQuery,
		},
		{
			name:    "missing kind",
			query:   ArtifactQuery{Name: name},
			wantErr: ErrInvalidQuery,
		},
		{
			name:    "invalid version",
			query:   ArtifactQuery{Name: name, Kind: ArtifactKindProvider, Version: "not-a-version!"},
			wantErr: ErrInvalidQuery,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			findCtx := t.Context()
			if tt.preRelease {
				findCtx = WithIncludePreRelease(findCtx)
			}

			info, err := cat.Find(findCtx, tt.query)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				if errors.Is(tt.wantErr, ErrNoMatchingVersion) {
					assert.ErrorIs(t, err, ErrArtifactNotFound, "NoMatchingVersionError must unwrap to ErrArtifactNotFound for chain fallthrough")
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, name, info.Reference.Name)
			assert.Equal(t, ArtifactKindProvider, info.Reference.Kind)
			require.NotNil(t, info.Reference.Version)
			assert.Equal(t, tt.wantVersion, info.Reference.Version.String())
			assert.Equal(t, digests[tt.wantVersion], info.Digest)
			assert.Equal(t, cat.canonicalID(), info.Reference.Remote)
			assert.False(t, info.Reference.Built)
			assert.Equal(t, cat.Name(), info.Catalog)
		})
	}
}

func TestRemoteCatalog_Find_OnlyPreReleases(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	defer ts.Close()

	ctx := t.Context()
	for _, v := range []string{"1.0.0-alpha.1", "1.0.0-beta.1"} {
		_, err := cat.Store(ctx, testRef("pre-only", v), []byte(v), nil, nil, false)
		require.NoError(t, err)
	}

	info, err := cat.Find(ctx, ArtifactQuery{Name: "pre-only", Kind: ArtifactKindProvider})
	require.NoError(t, err)
	assert.Equal(t, "1.0.0-beta.1", info.Reference.Version.String())
}

func TestRemoteCatalog_Find_Unreachable(t *testing.T) {
	t.Parallel()
	cat, ts := newTestRemoteCatalog(t)
	ts.Close()

	_, err := cat.Find(t.Context(), ArtifactQuery{Name: "any", Kind: ArtifactKindProvider})
	require.Error(t, err)
	assert.False(t, IsNotFound(err), "unreachable registry must not look like not-found")
	_, ok := IsCatalogUnreachable(err)
	assert.True(t, ok)
}

func TestVersionCandidates(t *testing.T) {
	t.Parallel()

	versions := func(vs ...string) []*semver.Version {
		out := make([]*semver.Version, 0, len(vs))
		for _, v := range vs {
			out = append(out, semver.MustParse(v))
		}
		return out
	}
	strs := func(vs []*semver.Version) []string {
		out := make([]string, 0, len(vs))
		for _, v := range vs {
			out = append(out, v.String())
		}
		return out
	}

	all := versions("1.0.0", "2.0.0-rc.1", "1.5.0", "1.2.0")

	tests := []struct {
		name       string
		query      ArtifactQuery
		preRelease bool
		in         []*semver.Version
		want       []string
	}{
		{name: "open sorts stable desc", in: all, want: []string{"1.5.0", "1.2.0", "1.0.0"}},
		{name: "latest alias sorts stable desc", query: ArtifactQuery{Version: "latest"}, in: all, want: []string{"1.5.0", "1.2.0", "1.0.0"}},
		{name: "unnormalized LATEST matches nothing", query: ArtifactQuery{Version: "  LATEST  "}, in: all, want: []string{}},
		{name: "open with pre-release opt-in", in: all, preRelease: true, want: []string{"2.0.0-rc.1", "1.5.0", "1.2.0", "1.0.0"}},
		{name: "open falls back to pre-releases", in: versions("1.0.0-a", "1.0.0-b"), want: []string{"1.0.0-b", "1.0.0-a"}},
		{name: "exact", query: ArtifactQuery{Version: "1.2.0"}, in: all, want: []string{"1.2.0"}},
		{name: "constraint", query: ArtifactQuery{Version: ">=1.2.0"}, in: all, want: []string{"1.5.0", "1.2.0"}},
		{name: "constraint with pre-release context", query: ArtifactQuery{Version: ">=1.2.0"}, preRelease: true, in: all, want: []string{"2.0.0-rc.1", "1.5.0", "1.2.0"}},
		{name: "no match", query: ArtifactQuery{Version: "3.x"}, in: all, want: []string{}},
		{name: "invalid selector matches nothing", query: ArtifactQuery{Version: "not-a-version!"}, in: all, want: []string{}},
		{name: "empty input", in: nil, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			if tt.preRelease {
				ctx = WithIncludePreRelease(ctx)
			}
			assert.Equal(t, tt.want, strs(versionCandidates(ctx, tt.query, tt.in)))
		})
	}
}

func TestIsRegistryNotFound(t *testing.T) {
	t.Parallel()
	assert.False(t, isRegistryNotFound(nil))
	assert.False(t, isRegistryNotFound(assert.AnError))
}

func TestLocalCatalog_Find_NoMatchingVersionAndChainFallthrough(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	local := newTestCatalog(t)
	_, err := local.Store(ctx, testRef("prov-a", "0.4.0"), []byte("local-040"), nil, nil, false)
	require.NoError(t, err)

	// LocalCatalog.Find returns NoMatchingVersionError when versions exist but none match.
	_, err = local.Find(ctx, ArtifactQuery{Name: "prov-a", Kind: ArtifactKindProvider, Version: ">=0.6.0"})
	require.ErrorIs(t, err, ErrNoMatchingVersion)
	require.ErrorIs(t, err, ErrArtifactNotFound)
	noMatch, ok := IsNoMatchingVersion(err)
	require.True(t, ok)
	assert.Equal(t, []string{"0.4.0"}, noMatch.Available)
	assert.Equal(t, LocalCatalogName, noMatch.Catalog)

	// Absent artifact returns plain ArtifactNotFoundError, not ErrNoMatchingVersion.
	_, err = local.Find(ctx, ArtifactQuery{Name: "absent-prov", Kind: ArtifactKindProvider, Version: ">=0.6.0"})
	require.ErrorIs(t, err, ErrArtifactNotFound)
	assert.NotErrorIs(t, err, ErrNoMatchingVersion)

	// In a multi-catalog chain, local 0.4.0 misses ">=0.6.0" with NoMatchingVersionError
	// and falls through cleanly to remote 0.6.0!
	remote, ts := newTestRemoteCatalog(t)
	t.Cleanup(ts.Close)
	_, err = remote.Store(ctx, testRef("prov-a", "0.6.0"), []byte("remote-060"), nil, nil, false)
	require.NoError(t, err)

	chain := newPluginSourceChain(local.logger, newLocalPlugin(local), remote)
	info, err := chain.Find(ctx, ArtifactQuery{Name: "prov-a", Kind: ArtifactKindProvider, Version: ">=0.6.0"})
	require.NoError(t, err)
	assert.Equal(t, "0.6.0", info.Reference.Version.String())
	assert.Equal(t, remote.Name(), info.Catalog)

	// When no catalog in the chain matches ">=1.0.0", aggregateChainError surfaces *NoMatchingVersionError.
	_, err = chain.Find(ctx, ArtifactQuery{Name: "prov-a", Kind: ArtifactKindProvider, Version: ">=1.0.0"})
	require.ErrorIs(t, err, ErrNoMatchingVersion)
	require.ErrorIs(t, err, ErrArtifactNotFound)
	merged, ok := IsNoMatchingVersion(err)
	require.True(t, ok)
	assert.Contains(t, merged.Available, "0.4.0")
	assert.Contains(t, merged.Available, "0.6.0")
}

func BenchmarkVersionCandidates(b *testing.B) {
	vs := make([]*semver.Version, 0, 100)
	for i := range 100 {
		vs = append(vs, semver.New(uint64(i/10), uint64(i%10), 0, "", ""))
	}
	query := ArtifactQuery{Version: "^3.0.0"}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = versionCandidates(ctx, query, vs)
	}
}
