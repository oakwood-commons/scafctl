// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExactSelector(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		kindFlag   string
		originFlag string
		want       ExactSelector
		wantVer    string
		wantErr    string
	}{
		{
			name:    "name with version",
			input:   "my-solution@1.0.0",
			want:    ExactSelector{Name: "my-solution"},
			wantVer: "1.0.0",
		},
		{
			name:  "name with digest",
			input: "my-solution@" + testPushDigest,
			want:  ExactSelector{Name: "my-solution", Digest: testPushDigest},
		},
		{
			name:     "with kind",
			input:    "exec@0.6.0",
			kindFlag: "provider",
			want:     ExactSelector{Kind: ArtifactKindProvider, Name: "exec"},
			wantVer:  "0.6.0",
		},
		{
			name:       "with origin built",
			input:      "my-solution@1.0.0",
			originFlag: OriginBuilt,
			want:       ExactSelector{Name: "my-solution", Built: true},
			wantVer:    "1.0.0",
		},
		{
			name:       "with canonical origin",
			input:      "exec@1.0.0",
			originFlag: "ghcr.io/oakwood-commons",
			want:       ExactSelector{Name: "exec", Origin: "ghcr.io/oakwood-commons"},
			wantVer:    "1.0.0",
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: "reference cannot be empty",
		},
		{
			name:    "no version or digest",
			input:   "my-solution",
			wantErr: "version or digest required",
		},
		{
			name:     "invalid kind",
			input:    "my-solution@1.0.0",
			kindFlag: "bogus",
			wantErr:  "invalid kind",
		},
		{
			name:       "invalid origin",
			input:      "my-solution@1.0.0",
			originFlag: "not a valid origin",
			wantErr:    "invalid --origin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseExactSelector(tt.input, tt.kindFlag, tt.originFlag)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			if tt.wantVer != "" {
				require.NotNil(t, got.Version)
				assert.Equal(t, tt.wantVer, got.Version.String())
				got.Version = nil
			} else {
				assert.Nil(t, got.Version)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLocalCatalog_ResolveExact(t *testing.T) {
	f := newPushFixture(t)

	t.Run("unambiguous built artifact", func(t *testing.T) {
		sel, err := ParseExactSelector("solo@1.0.0", "", "")
		require.NoError(t, err)
		info, err := f.cat.ResolveExact(context.Background(), sel)
		require.NoError(t, err)
		assert.Equal(t, f.soloBuilt100, info.Digest)
		assert.Equal(t, "", info.Reference.Origin)
	})

	t.Run("built and pulled same version is ambiguous", func(t *testing.T) {
		sel, err := ParseExactSelector("app@1.0.0", "", "")
		require.NoError(t, err)
		_, err = f.cat.ResolveExact(context.Background(), sel)
		require.Error(t, err)
		assert.True(t, IsAmbiguous(err))
		var amb *AmbiguousArtifactError
		require.ErrorAs(t, err, &amb)
		require.Len(t, amb.Candidates, 2)
		hints := amb.DeleteHints("scafctl")
		require.Len(t, hints, 2)
		for _, h := range hints {
			assert.Contains(t, h, "catalog delete")
		}
	})

	t.Run("origin flag disambiguates built vs pulled", func(t *testing.T) {
		sel, err := ParseExactSelector("app@1.0.0", "", OriginBuilt)
		require.NoError(t, err)
		info, err := f.cat.ResolveExact(context.Background(), sel)
		require.NoError(t, err)
		assert.Equal(t, f.appBuilt100, info.Digest)
		assert.Equal(t, "", info.Reference.Origin)

		sel, err = ParseExactSelector("app@1.0.0", "", testPushOrigin)
		require.NoError(t, err)
		info, err = f.cat.ResolveExact(context.Background(), sel)
		require.NoError(t, err)
		assert.Equal(t, f.appPulled100, info.Digest)
		assert.Equal(t, testPushOrigin, info.Reference.Origin)
	})

	t.Run("digest pin bypasses ambiguity", func(t *testing.T) {
		sel, err := ParseExactSelector("app@"+f.appPulled100, "", "")
		require.NoError(t, err)
		info, err := f.cat.ResolveExact(context.Background(), sel)
		require.NoError(t, err)
		assert.Equal(t, f.appPulled100, info.Digest)
		assert.Equal(t, testPushOrigin, info.Reference.Origin)
	})

	t.Run("not found reports available origins", func(t *testing.T) {
		sel, err := ParseExactSelector("app@1.0.0", "", testPushOtherOrigin)
		require.NoError(t, err)
		_, err = f.cat.ResolveExact(context.Background(), sel)
		require.Error(t, err)
		assert.True(t, IsNotFound(err))
		assert.Contains(t, err.Error(), "no copy from")
	})

	t.Run("name existing as multiple kinds is ambiguous without --kind", func(t *testing.T) {
		sel, err := ParseExactSelector("exec@1.0.0", "", "")
		require.NoError(t, err)
		_, err = f.cat.ResolveExact(context.Background(), sel)
		require.Error(t, err)
		var ambKind *AmbiguousKindError
		require.ErrorAs(t, err, &ambKind)
		assert.Equal(t, "@1.0.0", ambKind.Selector, "the version must be preserved so hints are directly runnable")
		hints := ambKind.DeleteHints("scafctl")
		require.NotEmpty(t, hints)
		for _, hint := range hints {
			assert.Contains(t, hint, "exec@1.0.0", "hint must include the version/digest, not just the bare name")
		}
	})

	t.Run("kind flag resolves kind ambiguity", func(t *testing.T) {
		sel, err := ParseExactSelector("exec@0.6.0", "provider", "")
		require.NoError(t, err)
		info, err := f.cat.ResolveExact(context.Background(), sel)
		require.NoError(t, err)
		assert.Equal(t, f.execProvider, info.Digest)
	})

	t.Run("resolved reference addresses the exact tag", func(t *testing.T) {
		sel, err := ParseExactSelector("app@1.0.0", "", testPushOrigin)
		require.NoError(t, err)
		info, err := f.cat.ResolveExact(context.Background(), sel)
		require.NoError(t, err)
		require.NoError(t, f.cat.Delete(context.Background(), info.Reference))

		exists, err := f.cat.Exists(context.Background(), Reference{
			Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0"), Origin: testPushOrigin,
		})
		require.NoError(t, err)
		assert.False(t, exists)

		// The built copy at the same name/version must be untouched.
		existsBuilt, err := f.cat.Exists(context.Background(), Reference{
			Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0"),
		})
		require.NoError(t, err)
		assert.True(t, existsBuilt)
	})
}
