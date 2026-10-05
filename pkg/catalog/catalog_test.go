// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
)

func TestIsPreRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version *semver.Version
		want    bool
	}{
		{"nil version", nil, false},
		{"stable", semver.MustParse("1.0.0"), false},
		{"beta", semver.MustParse("1.0.0-beta.1"), true},
		{"alpha", semver.MustParse("2.0.0-alpha"), true},
		{"rc", semver.MustParse("3.0.0-rc.1"), true},
		{"dev build", semver.MustParse("0.0.99-beta.1"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsPreRelease(tt.version))
		})
	}
}

func TestWantsLatest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query ArtifactQuery
		want  bool
	}{
		{"empty version", ArtifactQuery{}, true},
		{"latest alias", ArtifactQuery{Version: "latest"}, true},
		{"exact version", ArtifactQuery{Version: "1.0.0"}, false},
		{"exact pre-release", ArtifactQuery{Version: "1.0.0-rc.1"}, false},
		{"constraint", ArtifactQuery{Version: ">=1.0.0"}, false},
		{"digest pinned", ArtifactQuery{Digest: "sha256:abc"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, wantsLatest(tt.query))
		})
	}
}

func TestLatestCandidates(t *testing.T) {
	t.Parallel()

	info := func(v string) ArtifactInfo {
		return ArtifactInfo{Reference: Reference{Version: semver.MustParse(v)}}
	}
	versions := func(in []ArtifactInfo) []string {
		out := make([]string, 0, len(in))
		for _, a := range in {
			out = append(out, a.Reference.Version.String())
		}
		return out
	}

	t.Run("drops pre-releases when stable exists", func(t *testing.T) {
		t.Parallel()
		got := latestCandidates(context.Background(), []ArtifactInfo{info("1.0.0"), info("2.0.0-rc.1"), info("1.1.0")})
		assert.Equal(t, []string{"1.0.0", "1.1.0"}, versions(got))
	})

	t.Run("falls back to pre-releases when none stable", func(t *testing.T) {
		t.Parallel()
		got := latestCandidates(context.Background(), []ArtifactInfo{info("2.0.0-rc.1"), info("2.0.0-rc.2")})
		assert.Equal(t, []string{"2.0.0-rc.1", "2.0.0-rc.2"}, versions(got))
	})

	t.Run("keeps everything when pre-releases are included", func(t *testing.T) {
		t.Parallel()
		ctx := WithIncludePreRelease(context.Background())
		got := latestCandidates(ctx, []ArtifactInfo{info("1.0.0"), info("2.0.0-rc.1")})
		assert.Equal(t, []string{"1.0.0", "2.0.0-rc.1"}, versions(got))
	})

	t.Run("empty input", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, latestCandidates(context.Background(), nil))
	})
}

func TestIncludePreReleaseContext(t *testing.T) {
	t.Parallel()

	t.Run("false by default", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		assert.False(t, IncludePreReleaseFromContext(ctx))
	})

	t.Run("true when set", func(t *testing.T) {
		t.Parallel()
		ctx := WithIncludePreRelease(context.Background())
		assert.True(t, IncludePreReleaseFromContext(ctx))
	})
}

func TestSearchPatternContext(t *testing.T) {
	t.Parallel()

	t.Run("empty by default", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		assert.Empty(t, SearchPatternFromContext(ctx))
	})

	t.Run("returns pattern when set", func(t *testing.T) {
		t.Parallel()
		ctx := WithSearchPattern(context.Background(), "starter*")
		assert.Equal(t, "starter*", SearchPatternFromContext(ctx))
	})
}

func TestMatchesSearchPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		input string
		want  bool
	}{
		{"empty query matches everything", "", "anything", true},
		{"exact match", "starter-kit", "starter-kit", true},
		{"substring match", "starter", "starter-kit", true},
		{"suffix match", "kit", "starter-kit", true},
		{"middle match", "arter", "starter-kit", true},
		{"no match", "terraform", "hello-world", false},
		{"case insensitive query", "Starter", "starter-kit", true},
		{"case insensitive name", "starter", "Starter-Kit", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, matchesSearchPattern(tc.query, tc.input))
		})
	}
}
