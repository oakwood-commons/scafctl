// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A ref that pins an origin is a tag lookup, so Delete must never remove
// another origin's artifact.
func TestLocalCatalog_Delete_ExplicitOriginDoesNotCrossOrigins(t *testing.T) {
	t.Parallel()

	const origin = "ghcr.io/org/plugins"
	version := semver.MustParse("1.0.0")
	builtRef := Reference{Kind: ArtifactKindSolution, Name: "x", Version: version, Built: true}
	remoteRef := Reference{Kind: ArtifactKindSolution, Name: "x", Version: version, Remote: origin}

	tests := []struct {
		name       string
		seedBuilt  bool
		seedRemote bool
		target     Reference
		wantErr    bool
		wantBuilt  bool
		wantRemote bool
	}{
		{name: "remote target, only built stored", seedBuilt: true, target: remoteRef, wantErr: true, wantBuilt: true},
		{name: "built target, only remote stored", seedRemote: true, target: builtRef, wantErr: true, wantRemote: true},
		{name: "remote target, both stored", seedBuilt: true, seedRemote: true, target: remoteRef, wantBuilt: true},
		{name: "built target, both stored", seedBuilt: true, seedRemote: true, target: builtRef, wantRemote: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			cat := newTestCatalog(t)
			if tt.seedBuilt {
				_, err := cat.Store(ctx, builtRef, []byte("built"), nil, nil, false)
				require.NoError(t, err)
			}
			if tt.seedRemote {
				_, err := cat.Store(ctx, remoteRef, []byte("remote"), nil, map[string]string{AnnotationSourceCanonical: origin}, false)
				require.NoError(t, err)
			}

			err := cat.Delete(ctx, tt.target)

			if tt.wantErr {
				assert.True(t, IsNotFound(err), "got %v", err)
			} else {
				require.NoError(t, err)
			}
			builtExists, err := cat.Exists(ctx, builtRef)
			require.NoError(t, err)
			remoteExists, err := cat.Exists(ctx, remoteRef)
			require.NoError(t, err)
			assert.Equal(t, tt.wantBuilt, builtExists, "built copy")
			assert.Equal(t, tt.wantRemote, remoteExists, "remote copy")
		})
	}
}

// A ref that pins no origin matches by annotation identity, so a version held
// by two origins is ambiguous and nothing is deleted.
func TestLocalCatalog_Delete_UnpinnedOriginAmbiguous(t *testing.T) {
	t.Parallel()

	const origin = "ghcr.io/org/plugins"
	version := semver.MustParse("1.0.0")
	builtRef := Reference{Kind: ArtifactKindSolution, Name: "x", Version: version, Built: true}
	remoteRef := Reference{Kind: ArtifactKindSolution, Name: "x", Version: version, Remote: origin}
	unpinned := Reference{Kind: ArtifactKindSolution, Name: "x", Version: version}

	ctx := t.Context()
	cat := newTestCatalog(t)
	_, err := cat.Store(ctx, builtRef, []byte("built"), nil, nil, false)
	require.NoError(t, err)
	_, err = cat.Store(ctx, remoteRef, []byte("remote"), nil, map[string]string{AnnotationSourceCanonical: origin}, false)
	require.NoError(t, err)

	err = cat.Delete(ctx, unpinned)

	var ambiguous *AmbiguousArtifactError
	require.ErrorAs(t, err, &ambiguous)
	assert.Len(t, ambiguous.Matches, 2)
	for _, ref := range []Reference{builtRef, remoteRef} {
		exists, err := cat.Exists(ctx, ref)
		require.NoError(t, err)
		assert.True(t, exists, "nothing may be deleted on ambiguity")
	}
}

// A ref that cannot form a valid query (no kind) is rejected rather than
// matched against every artifact, so nothing is deleted.
func TestLocalCatalog_Delete_InvalidQueryRejected(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cat := newTestCatalog(t)
	stored := Reference{Kind: ArtifactKindSolution, Name: "x", Version: semver.MustParse("1.0.0"), Built: true}
	_, err := cat.Store(ctx, stored, []byte("built"), nil, nil, false)
	require.NoError(t, err)

	err = cat.Delete(ctx, Reference{Name: "x", Version: semver.MustParse("1.0.0")})

	var invalid *InvalidQueryError
	require.ErrorAs(t, err, &invalid)
	exists, err := cat.Exists(ctx, stored)
	require.NoError(t, err)
	assert.True(t, exists)
}

// A digest-only ref has no version; Delete must not dereference it after the
// tag is already removed.
func TestLocalCatalog_Delete_DigestOnly(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cat := newTestCatalog(t)
	stored := Reference{Kind: ArtifactKindSolution, Name: "x", Version: semver.MustParse("1.0.0"), Built: true}
	info, err := cat.Store(ctx, stored, []byte("built"), nil, nil, false)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		err = cat.Delete(ctx, Reference{Kind: ArtifactKindSolution, Name: "x", Digest: info.Digest})
	})
	require.NoError(t, err)

	exists, err := cat.Exists(ctx, stored)
	require.NoError(t, err)
	assert.False(t, exists)
}
