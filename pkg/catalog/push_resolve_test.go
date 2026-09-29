// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

const testPushDigest = "sha256:3f2a000000000000000000000000000000000000000000000000000000000000"

func TestParsePushSelector(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		kindFlag   string
		originFlag string
		want       PushSelector
		wantVer    string
		wantErr    string
	}{
		// Short form
		{
			name:  "name only",
			input: "my-solution",
			want:  PushSelector{Name: "my-solution"},
		},
		{
			name:    "name with version",
			input:   "my-solution@1.0.0",
			want:    PushSelector{Name: "my-solution"},
			wantVer: "1.0.0",
		},
		{
			name:  "name with latest is no version",
			input: "my-solution@latest",
			want:  PushSelector{Name: "my-solution"},
		},
		{
			name:  "name with digest",
			input: "my-solution@" + testPushDigest,
			want:  PushSelector{Name: "my-solution", Digest: testPushDigest},
		},
		{
			name:     "short form with kind",
			input:    "exec@0.6.0",
			kindFlag: "provider",
			want:     PushSelector{Kind: ArtifactKindProvider, Name: "exec"},
			wantVer:  "0.6.0",
		},
		{
			name:       "short form with origin built",
			input:      "my-solution@1.0.0",
			originFlag: OriginBuilt,
			want:       PushSelector{Name: "my-solution", Built: true},
			wantVer:    "1.0.0",
		},
		{
			name:       "short form with canonical origin",
			input:      "exec",
			originFlag: "ghcr.io/oakwood-commons",
			want:       PushSelector{Name: "exec", Origin: "ghcr.io/oakwood-commons"},
		},
		{
			name:       "origin is normalized",
			input:      "exec",
			originFlag: " oci://ghcr.io/oakwood-commons/ ",
			want:       PushSelector{Name: "exec", Origin: "ghcr.io/oakwood-commons"},
		},
		{
			name:       "registry-only origin",
			input:      "exec",
			originFlag: "localhost:5000",
			want:       PushSelector{Name: "exec", Origin: "localhost:5000"},
		},
		{
			name:    "input is trimmed",
			input:   "  my-solution@1.0.0  ",
			want:    PushSelector{Name: "my-solution"},
			wantVer: "1.0.0",
		},

		// FQN form
		{
			name:    "fqn with kinds segment and version",
			input:   "ghcr.io/oakwood-commons/providers/exec@0.6.0",
			want:    PushSelector{Kind: ArtifactKindProvider, Name: "exec", Origin: "ghcr.io/oakwood-commons", MatchRemoteName: true},
			wantVer: "0.6.0",
		},
		{
			name:    "fqn with colon tag",
			input:   "ghcr.io/myorg/solutions/my-solution:1.2.3",
			want:    PushSelector{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "ghcr.io/myorg", MatchRemoteName: true},
			wantVer: "1.2.3",
		},
		{
			name:  "fqn without version",
			input: "ghcr.io/myorg/solutions/my-solution",
			want:  PushSelector{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "ghcr.io/myorg", MatchRemoteName: true},
		},
		{
			name:  "fqn with digest",
			input: "ghcr.io/myorg/solutions/my-solution@" + testPushDigest,
			want:  PushSelector{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "ghcr.io/myorg", Digest: testPushDigest, MatchRemoteName: true},
		},
		{
			name:    "fqn with oci scheme and port",
			input:   "oci://localhost:5000/team/auth-handlers/entra@1.0.0",
			want:    PushSelector{Kind: ArtifactKindAuthHandler, Name: "entra", Origin: "localhost:5000/team", MatchRemoteName: true},
			wantVer: "1.0.0",
		},
		{
			name:    "fqn nested repository",
			input:   "ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
			want:    PushSelector{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "ghcr.io/myorg/scafctl", MatchRemoteName: true},
			wantVer: "1.0.0",
		},
		{
			name:    "fqn registry-only origin",
			input:   "localhost:5000/solutions/my-solution@1.0.0",
			want:    PushSelector{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "localhost:5000", MatchRemoteName: true},
			wantVer: "1.0.0",
		},
		{
			name:     "fqn with matching kind flag",
			input:    "ghcr.io/myorg/providers/exec@0.6.0",
			kindFlag: "provider",
			want:     PushSelector{Kind: ArtifactKindProvider, Name: "exec", Origin: "ghcr.io/myorg", MatchRemoteName: true},
			wantVer:  "0.6.0",
		},
		{
			name:     "fqn without kinds segment uses kind flag",
			input:    "ghcr.io/myorg/my-solution@1.0.0",
			kindFlag: "solution",
			want:     PushSelector{Kind: ArtifactKindSolution, Name: "my-solution", Origin: "ghcr.io/myorg", MatchRemoteName: true},
			wantVer:  "1.0.0",
		},

		// Errors
		{name: "empty input", input: "   ", wantErr: "reference cannot be empty"},
		{name: "invalid kind flag", input: "exec", kindFlag: "widget", wantErr: "invalid kind"},
		{name: "invalid short name", input: "My_Solution", wantErr: "name must be lowercase"},
		{name: "invalid short version", input: "my-solution@not-a-version", wantErr: "invalid version"},
		{name: "invalid short digest", input: "my-solution@sha256:abc", wantErr: "invalid digest"},
		{name: "local tag form rejected", input: "solution/my-solution:1.0.0", wantErr: "expected an artifact name"},
		{name: "origin with fqn", input: "ghcr.io/myorg/solutions/x@1.0.0", originFlag: "ghcr.io/myorg", wantErr: "--origin cannot be combined"},
		{name: "kind conflicts with fqn", input: "ghcr.io/myorg/solutions/x@1.0.0", kindFlag: "provider", wantErr: "conflicts with kind solution"},
		{name: "fqn without kind", input: "ghcr.io/myorg/x@1.0.0", wantErr: "specify --kind"},
		{name: "fqn trailing segments after name", input: "ghcr.io/myorg/solutions/x/extra@1.0.0", wantErr: "must end in /<kinds>/<name>"},
		{
			name:    "fqn repository path contains a kind word (uses last kinds segment)",
			input:   "ghcr.io/solutions/providers/exec@1.0.0",
			want:    PushSelector{Kind: ArtifactKindProvider, Name: "exec", Origin: "ghcr.io/solutions", MatchRemoteName: true},
			wantVer: "1.0.0",
		},
		{name: "fqn invalid name", input: "ghcr.io/myorg/solutions/My_Sol@1.0.0", wantErr: "name must be lowercase"},
		{name: "fqn invalid version", input: "ghcr.io/myorg/solutions/x@nope", wantErr: "invalid version"},
		{name: "fqn invalid digest", input: "ghcr.io/myorg/solutions/x@sha256:abc", wantErr: "invalid digest"},
		{name: "origin with at sign", input: "x", originFlag: "ghcr.io/myorg@1", wantErr: "invalid --origin"},
		{name: "origin with other scheme", input: "x", originFlag: "https://ghcr.io/myorg", wantErr: "invalid --origin"},
		{name: "origin with whitespace", input: "x", originFlag: "ghcr.io/my org", wantErr: "invalid --origin"},
		{name: "origin only slashes", input: "x", originFlag: "oci:///", wantErr: "invalid --origin"},
		{name: "origin with empty segment", input: "x", originFlag: "ghcr.io//myorg", wantErr: "invalid --origin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParsePushSelector(tt.input, tt.kindFlag, tt.originFlag)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.True(t, errors.Is(err, ErrInvalidReference), "error should wrap ErrInvalidReference")
				return
			}
			require.NoError(t, err)

			if tt.wantVer == "" {
				assert.Nil(t, got.Version)
			} else {
				require.NotNil(t, got.Version)
				assert.Equal(t, tt.wantVer, got.Version.String())
			}
			got.Version = nil
			assert.Equal(t, tt.want, got)
		})
	}
}

func BenchmarkParsePushSelector(b *testing.B) {
	inputs := []string{
		"my-solution@1.0.0",
		"ghcr.io/oakwood-commons/providers/exec@0.6.0",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, in := range inputs {
			_, _ = ParsePushSelector(in, "", "")
		}
	}
}

const (
	testPushOrigin      = "ghcr.io/org"
	testPushOtherOrigin = "ghcr.io/oc"
)

// pushFixture is a local catalog seeded with every push-resolution variance.
type pushFixture struct {
	cat              *LocalCatalog
	appBuilt100      string // digest
	appPulled100     string
	appBuilt110      string
	appPulled120     string
	appPulledRC      string
	soloBuilt100     string
	execSolution     string
	execProvider     string
	renamedApp       string
	mirrorSharedDgst string
}

func storeForPush(t testing.TB, cat *LocalCatalog, kind ArtifactKind, name, version, canonical string, anns map[string]string) string {
	t.Helper()
	ref := Reference{Kind: kind, Name: name, Version: semver.MustParse(version), Origin: canonical}
	if anns == nil {
		anns = map[string]string{}
	}
	if canonical != "" {
		anns[AnnotationSourceCanonical] = canonical
		anns[AnnotationOrigin] = "pulled from " + canonical
	}
	content := []byte(kind.String() + "/" + name + "@" + version + " from " + canonical + " " + anns[AnnotationSourceName])
	// Each origin is a distinct local identity with its own tag, so no force
	// is needed to store the same name/version from several origins.
	info, err := cat.Store(context.Background(), ref, content, nil, anns, false)
	require.NoError(t, err)
	return info.Digest
}

func newPushFixture(t testing.TB) *pushFixture {
	t.Helper()
	cat, err := NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	ctx := context.Background()

	f := &pushFixture{cat: cat}
	f.appBuilt100 = storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", "", nil)
	f.appBuilt110 = storeForPush(t, cat, ArtifactKindSolution, "app", "1.1.0", "", nil)
	f.appPulled100 = storeForPush(t, cat, ArtifactKindSolution, "app", "1.0.0", testPushOrigin, nil)
	f.appPulled120 = storeForPush(t, cat, ArtifactKindSolution, "app", "1.2.0", testPushOrigin, nil)
	f.appPulledRC = storeForPush(t, cat, ArtifactKindSolution, "app", "2.0.0-rc.1", testPushOrigin, nil)

	f.soloBuilt100 = storeForPush(t, cat, ArtifactKindSolution, "solo", "1.0.0", "", nil)
	_, err = cat.Tag(ctx, Reference{Kind: ArtifactKindSolution, Name: "solo", Version: semver.MustParse("1.0.0")}, "stable")
	require.NoError(t, err)

	f.execSolution = storeForPush(t, cat, ArtifactKindSolution, "exec", "1.0.0", "", nil)
	f.execProvider = storeForPush(t, cat, ArtifactKindProvider, "exec", "0.6.0", testPushOtherOrigin, nil)

	// Renamed pull (contract 8 shape): local name renamed-app, remote name
	// upstream-app, tagged in the origin-qualified namespace like any pull.
	f.renamedApp = storeForPush(t, cat, ArtifactKindSolution, "renamed-app", "1.0.0", testPushOrigin, map[string]string{
		AnnotationSourceName: "upstream-app",
	})

	// Same digest tagged as built and as pulled from testPushOrigin.
	f.mirrorSharedDgst = storeForPush(t, cat, ArtifactKindSolution, "mirror", "1.0.0", "", nil)
	builtRef := Reference{Kind: ArtifactKindSolution, Name: "mirror", Version: semver.MustParse("1.0.0")}
	desc, err := cat.store.Resolve(ctx, builtRef.LocalTag())
	require.NoError(t, err)
	anns := make(map[string]string, len(desc.Annotations)+1)
	for k, v := range desc.Annotations {
		anns[k] = v
	}
	anns[AnnotationSourceCanonical] = testPushOrigin
	desc.Annotations = anns
	pulledRef := builtRef
	pulledRef.Origin = testPushOrigin
	require.NoError(t, cat.store.Tag(ctx, desc, pulledRef.LocalTag()))

	return f
}

func TestLocalCatalog_ResolveForPush(t *testing.T) {
	f := newPushFixture(t)

	type want struct {
		digest    string
		canonical string
		name      string
		tag       string
	}
	tests := []struct {
		name       string
		input      string
		kindFlag   string
		originFlag string
		prerelease bool
		want       *want
		wantErr    func(t *testing.T, err error)
	}{
		{
			name:  "single built artifact latest prefers version tag over alias",
			input: "solo",
			want:  &want{digest: f.soloBuilt100, name: "solo", tag: "1.0.0"},
		},
		{
			name:  "single built artifact exact version",
			input: "solo@1.0.0",
			want:  &want{digest: f.soloBuilt100, name: "solo", tag: "1.0.0"},
		},
		{
			name:  "same version built and pulled is ambiguous (C2)",
			input: "app@1.0.0",
			wantErr: func(t *testing.T, err error) {
				var amb *AmbiguousArtifactError
				require.ErrorAs(t, err, &amb)
				require.Len(t, amb.Candidates, 2)
				assert.Equal(t, "", amb.Candidates[0].Origin)
				assert.Equal(t, f.appBuilt100, amb.Candidates[0].Digest)
				assert.Equal(t, testPushOrigin, amb.Candidates[1].Origin)
				assert.Equal(t, f.appPulled100, amb.Candidates[1].Digest)
				assert.Equal(t, "1.0.0", amb.Version)
			},
		},
		{
			name:  "latest differs per origin is ambiguous, not global max (C4)",
			input: "app",
			wantErr: func(t *testing.T, err error) {
				var amb *AmbiguousArtifactError
				require.ErrorAs(t, err, &amb)
				require.Len(t, amb.Candidates, 2)
				assert.Equal(t, "1.1.0", amb.Candidates[0].Version)
				assert.Equal(t, "1.2.0", amb.Candidates[1].Version)
				assert.Empty(t, amb.Version)
			},
		},
		{
			name:       "origin built picks built latest",
			input:      "app",
			originFlag: OriginBuilt,
			want:       &want{digest: f.appBuilt110, name: "app", tag: "1.1.0"},
		},
		{
			name:       "origin canonical picks latest stable from that origin (C6)",
			input:      "app",
			originFlag: testPushOrigin,
			want:       &want{digest: f.appPulled120, canonical: testPushOrigin, name: "app", tag: "1.2.0"},
		},
		{
			name:       "pre-release included when opted in",
			input:      "app",
			originFlag: testPushOrigin,
			prerelease: true,
			want:       &want{digest: f.appPulledRC, canonical: testPushOrigin, name: "app", tag: "2.0.0-rc.1"},
		},
		{
			name:       "exact version with origin",
			input:      "app@1.0.0",
			originFlag: testPushOrigin,
			want:       &want{digest: f.appPulled100, canonical: testPushOrigin, name: "app", tag: "1.0.0"},
		},
		{
			name:  "digest bypasses ambiguity",
			input: "app@" + f.appBuilt100,
			want:  &want{digest: f.appBuilt100, name: "app", tag: "1.0.0"},
		},
		{
			name:       "digest with non-matching origin is not found",
			input:      "app@" + f.appBuilt100,
			originFlag: testPushOrigin,
			wantErr: func(t *testing.T, err error) {
				assert.True(t, IsNotFound(err))
				assert.Contains(t, err.Error(), "available from: built locally")
			},
		},
		{
			name:  "unknown version is not found",
			input: "app@9.9.9",
			wantErr: func(t *testing.T, err error) {
				assert.True(t, IsNotFound(err))
				assert.NotContains(t, err.Error(), "available from")
			},
		},
		{
			name:       "origin filter lists other origins with that version",
			input:      "app@1.1.0",
			originFlag: testPushOrigin,
			wantErr: func(t *testing.T, err error) {
				assert.True(t, IsNotFound(err))
				assert.Contains(t, err.Error(), "no copy from "+testPushOrigin)
				assert.Contains(t, err.Error(), "available from: built locally")
			},
		},
		{
			name:       "origin filter with no alternative",
			input:      "app@9.9.9",
			originFlag: OriginBuilt,
			wantErr: func(t *testing.T, err error) {
				assert.True(t, IsNotFound(err))
				assert.Contains(t, err.Error(), "no copy from built locally")
				assert.NotContains(t, err.Error(), "available from")
			},
		},
		{
			name:    "unknown name is not found",
			input:   "missing",
			wantErr: func(t *testing.T, err error) { assert.True(t, IsNotFound(err)) },
		},
		{
			name:  "name across kinds is ambiguous (C3)",
			input: "exec",
			wantErr: func(t *testing.T, err error) {
				var ak *AmbiguousKindError
				require.ErrorAs(t, err, &ak)
				assert.Equal(t, []ArtifactKind{ArtifactKindProvider, ArtifactKindSolution}, ak.Kinds)
				assert.True(t, IsAmbiguous(err))
			},
		},
		{
			name:     "kind flag disambiguates kinds",
			input:    "exec",
			kindFlag: "provider",
			want:     &want{digest: f.execProvider, canonical: testPushOtherOrigin, name: "exec", tag: "0.6.0"},
		},
		{
			name:       "origin flag disambiguates kinds",
			input:      "exec",
			originFlag: OriginBuilt,
			want:       &want{digest: f.execSolution, name: "exec", tag: "1.0.0"},
		},
		{
			name:  "same digest under two origins is one artifact (C1)",
			input: "mirror@1.0.0",
			want:  &want{digest: f.mirrorSharedDgst, name: "mirror", tag: "1.0.0"},
		},
		{
			name:  "fqn selects the pulled mirror",
			input: testPushOrigin + "/solutions/app@1.0.0",
			want:  &want{digest: f.appPulled100, canonical: testPushOrigin, name: "app", tag: "1.0.0"},
		},
		{
			name:  "fqn latest from that origin",
			input: testPushOrigin + "/solutions/app",
			want:  &want{digest: f.appPulled120, canonical: testPushOrigin, name: "app", tag: "1.2.0"},
		},
		{
			name:  "fqn matches renamed copy by its remote name",
			input: testPushOrigin + "/solutions/upstream-app@1.0.0",
			want:  &want{digest: f.renamedApp, canonical: testPushOrigin, name: "renamed-app", tag: "1.0.0"},
		},
		{
			name:  "fqn never matches renamed copy by its local name",
			input: testPushOrigin + "/solutions/renamed-app@1.0.0",
			wantErr: func(t *testing.T, err error) {
				assert.True(t, IsNotFound(err))
			},
		},
		{
			name:  "fqn from another origin reports available origins",
			input: "ghcr.io/other/solutions/app@1.0.0",
			wantErr: func(t *testing.T, err error) {
				assert.True(t, IsNotFound(err))
				assert.Contains(t, err.Error(), "available from: built locally, "+testPushOrigin)
			},
		},
		{
			name:  "fqn does not fall back to a built copy",
			input: testPushOrigin + "/solutions/solo@1.0.0",
			wantErr: func(t *testing.T, err error) {
				assert.True(t, IsNotFound(err))
				assert.Contains(t, err.Error(), "available from: built locally")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.prerelease {
				ctx = WithIncludePreRelease(ctx)
			}
			sel, err := ParsePushSelector(tt.input, tt.kindFlag, tt.originFlag)
			require.NoError(t, err)

			info, err := f.cat.ResolveForPush(ctx, sel)
			if tt.wantErr != nil {
				require.Error(t, err)
				tt.wantErr(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.digest, info.Digest)
			assert.Equal(t, tt.want.canonical, info.Canonical)
			assert.Equal(t, tt.want.name, info.Reference.Name)
			assert.Equal(t, tt.want.tag, info.Tag)
		})
	}
}

func TestLocalCatalog_ResolveForPush_EmptyName(t *testing.T) {
	cat := newTestCatalog(t)
	_, err := cat.ResolveForPush(context.Background(), PushSelector{})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidReference)
}

func TestLocalCatalog_ResolveForPush_AliasCollapsesIntoOneCandidate(t *testing.T) {
	f := newPushFixture(t)
	ctx := context.Background()

	// Tag the built app@1.0.0 with an alias; it must stay one candidate with
	// both tag labels.
	_, err := f.cat.Tag(ctx, Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}, "stable")
	require.NoError(t, err)

	sel, err := ParsePushSelector("app@1.0.0", "", "")
	require.NoError(t, err)
	_, err = f.cat.ResolveForPush(ctx, sel)

	var amb *AmbiguousArtifactError
	require.ErrorAs(t, err, &amb)
	require.Len(t, amb.Candidates, 2)
	assert.Equal(t, []string{"1.0.0", "stable"}, amb.Candidates[0].Tags)
	assert.Equal(t, []string{"1.0.0"}, amb.Candidates[1].Tags)
}

func TestAmbiguousArtifactError_MessageAndHints(t *testing.T) {
	digestA := "sha256:" + strings.Repeat("a", 64)
	digestB := "sha256:" + strings.Repeat("b", 64)
	digestC := "sha256:" + strings.Repeat("c", 64)

	e := &AmbiguousArtifactError{
		Kind:    ArtifactKindSolution,
		Name:    "app",
		Version: "1.0.0",
		Candidates: []PushCandidate{
			{Name: "app", Origin: "", Version: "1.0.0", Digest: digestA, Tags: []string{"1.0.0", "stable"}},
			{Name: "app", Origin: testPushOrigin, Version: "1.0.0", Digest: digestB, Tags: []string{"1.0.0"}},
			{Name: "app", Origin: testPushOrigin, Version: "1.0.0", Digest: digestC, Tags: []string{"1.0.0"}},
		},
	}

	msg := e.Error()
	assert.Contains(t, msg, `solution "app@1.0.0" matches 3 different local artifacts`)
	assert.Contains(t, msg, "built locally  1.0.0  sha256:aaaaaaaaaaaa  [1.0.0, stable]")
	assert.Contains(t, msg, testPushOrigin+"  1.0.0  sha256:bbbbbbbbbbbb  [1.0.0]")
	assert.True(t, IsAmbiguous(e))

	hints := e.Hints("mycli")
	assert.Equal(t, []string{
		"mycli catalog push app@1.0.0 --kind solution --origin built --catalog <destination>",
		"mycli catalog push app@" + digestB + " --kind solution --catalog <destination>",
		"mycli catalog push app@" + digestC + " --kind solution --catalog <destination>",
	}, hints)
}

func TestAmbiguousArtifactError_UnversionedUsesDigestHint(t *testing.T) {
	e := &AmbiguousArtifactError{
		Kind:       ArtifactKindProvider,
		Name:       "exec",
		Candidates: []PushCandidate{{Name: "exec", Digest: testPushDigest}},
	}
	assert.Contains(t, e.Error(), `provider "exec" matches 1 different local artifacts`)
	assert.Equal(t, []string{"mycli catalog push exec@" + testPushDigest + " --kind provider --catalog <destination>"}, e.Hints("mycli"))
}

func TestAmbiguityHints_EmptyBinaryNameFallsBack(t *testing.T) {
	artifact := &AmbiguousArtifactError{
		Kind:       ArtifactKindProvider,
		Name:       "exec",
		Candidates: []PushCandidate{{Name: "exec", Version: "1.0.0", Digest: testPushDigest}},
	}
	kind := &AmbiguousKindError{Name: "exec", Kinds: []ArtifactKind{ArtifactKindProvider}}
	prefix := settings.CliBinaryName + " catalog "
	for _, hints := range [][]string{
		artifact.Hints(""), artifact.DeleteHints(""), artifact.TagHints("", "stable"),
		kind.Hints(""), kind.DeleteHints(""), kind.TagHints("", "stable"),
	} {
		require.Len(t, hints, 1)
		assert.True(t, strings.HasPrefix(hints[0], prefix), "hint %q must start with %q", hints[0], prefix)
	}
}

func TestAmbiguousKindError(t *testing.T) {
	e := &AmbiguousKindError{Name: "exec", Kinds: []ArtifactKind{ArtifactKindProvider, ArtifactKindSolution}}
	assert.Equal(t, `"exec" exists as multiple kinds (provider, solution); specify --kind`, e.Error())
	assert.True(t, IsAmbiguous(e))
	assert.False(t, IsAmbiguous(errors.New("other")))
	assert.Equal(t, []string{
		"mycli catalog push exec --kind provider --catalog <destination>",
		"mycli catalog push exec --kind solution --catalog <destination>",
	}, e.Hints("mycli"))
}

func TestAmbiguousKindError_PreservesSelector(t *testing.T) {
	e := &AmbiguousKindError{Name: "exec", Kinds: []ArtifactKind{ArtifactKindProvider, ArtifactKindSolution}, Selector: "@1.0.0"}
	assert.Equal(t, `"exec@1.0.0" exists as multiple kinds (provider, solution); specify --kind`, e.Error())
	assert.Equal(t, []string{
		"mycli catalog push exec@1.0.0 --kind provider --catalog <destination>",
		"mycli catalog push exec@1.0.0 --kind solution --catalog <destination>",
	}, e.Hints("mycli"))
	assert.Equal(t, []string{
		"mycli catalog delete exec@1.0.0 --kind provider",
		"mycli catalog delete exec@1.0.0 --kind solution",
	}, e.DeleteHints("mycli"))
}

func TestSelectorSuffix(t *testing.T) {
	assert.Empty(t, selectorSuffix(nil, ""))
	assert.Equal(t, "@1.0.0", selectorSuffix(semver.MustParse("1.0.0"), ""))
	assert.Equal(t, "@"+testPushDigest, selectorSuffix(nil, testPushDigest))
	assert.Equal(t, "@"+testPushDigest, selectorSuffix(semver.MustParse("1.0.0"), testPushDigest), "digest takes precedence over version")
}

func TestResolveForPush_AmbiguousKindPreservesSelector(t *testing.T) {
	f := newPushFixture(t)
	_, err := f.cat.ResolveForPush(context.Background(), PushSelector{Name: "exec", Version: semver.MustParse("1.0.0")})
	var ambKind *AmbiguousKindError
	require.ErrorAs(t, err, &ambKind)
	assert.Equal(t, "@1.0.0", ambKind.Selector)
}

func TestLatestPerOrigin_Unversioned(t *testing.T) {
	infos := []ArtifactInfo{
		{Digest: "d1", Canonical: ""},
		{Digest: "d2", Canonical: "", Reference: Reference{Version: semver.MustParse("1.0.0")}},
		{Digest: "d3", Canonical: testPushOrigin},
	}
	got := latestPerOrigin(infos, false)
	digests := make([]string, 0, len(got))
	for _, a := range got {
		digests = append(digests, a.Digest)
	}
	assert.ElementsMatch(t, []string{"d2", "d3"}, digests, "versioned wins within an origin; unversioned kept when nothing else")
}

func TestShortDigest(t *testing.T) {
	assert.Equal(t, "sha256:3f2a00000000", shortDigest(testPushDigest))
	assert.Equal(t, "sha256:abc", shortDigest("sha256:abc"))
	assert.Equal(t, "other", shortDigest("other"))
}

func TestManifestAnnotations_StripsDescriptorOnlyKeys(t *testing.T) {
	got := manifestAnnotations(map[string]string{
		AnnotationSourceCanonical: testPushOrigin,
		AnnotationSourceName:      "upstream",
		AnnotationOrigin:          "pulled from " + testPushOrigin,
		AnnotationArtifactName:    "local",
	})
	assert.Equal(t, map[string]string{AnnotationArtifactName: "local"}, got)
}

func TestNewPushDestinationRequiredError(t *testing.T) {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	tests := []struct {
		name       string
		cfg        *config.Config
		wantSubstr []string
		notSubstr  []string
	}{
		{
			name:       "no config",
			wantSubstr: []string{"--catalog", "destination", "mycli catalog push <artifact> --catalog"},
			notSubstr:  []string{"Configured catalogs"},
		},
		{
			name: "lists configured OCI catalogs only",
			cfg: &config.Config{Catalogs: []config.CatalogConfig{
				{Name: "local", Type: config.CatalogTypeFilesystem, Path: "/tmp/cat"},
				{Name: "team", Type: config.CatalogTypeOCI, URL: "ghcr.io/team"},
			}},
			wantSubstr: []string{"Configured catalogs:", "team  ghcr.io/team"},
			notSubstr:  []string{"/tmp/cat"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := settings.IntoContext(context.Background(), cliParams)
			if tt.cfg != nil {
				ctx = config.WithConfig(ctx, tt.cfg)
			}

			err := NewPushDestinationRequiredError(ctx)

			require.ErrorIs(t, err, ErrPushDestinationRequired)
			for _, s := range tt.wantSubstr {
				assert.Contains(t, err.Error(), s)
			}
			for _, s := range tt.notSubstr {
				assert.NotContains(t, err.Error(), s)
			}
		})
	}
}

func TestValidatePushDestination(t *testing.T) {
	pulled := ArtifactInfo{
		Reference: Reference{Kind: ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")},
		Canonical: testPushOrigin,
	}
	built := pulled
	built.Canonical = ""

	tests := []struct {
		name    string
		info    ArtifactInfo
		dest    string
		wantErr bool
	}{
		{name: "built to any destination", info: built, dest: testPushOrigin},
		{name: "pulled to another catalog", info: pulled, dest: testPushOtherOrigin},
		{name: "pulled to a sub-repository of its origin", info: pulled, dest: testPushOrigin + "/mirror"},
		{name: "pulled back to its origin", info: pulled, dest: testPushOrigin, wantErr: true},
		{name: "origin compared case-insensitively", info: pulled, dest: strings.ToUpper(testPushOrigin), wantErr: true},
		{name: "trailing slash ignored", info: pulled, dest: testPushOrigin + "/", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePushDestination(tt.info, tt.dest)
			if !tt.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrPushToOrigin)
			assert.Contains(t, err.Error(), "app@1.0.0")
			assert.Contains(t, err.Error(), testPushOrigin)
		})
	}
}

func BenchmarkResolveForPush(b *testing.B) {
	f := newPushFixture(b)
	ctx := context.Background()
	sel, err := ParsePushSelector("app", "", OriginBuilt)
	require.NoError(b, err)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, _ = f.cat.ResolveForPush(ctx, sel)
	}
}
