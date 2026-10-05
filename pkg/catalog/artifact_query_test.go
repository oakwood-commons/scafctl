// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArtifactQuery_Validate(t *testing.T) {
	tests := []struct {
		name    string
		query   ArtifactQuery
		wantErr string
	}{
		{
			name:  "name and kind are sufficient",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution},
		},
		{
			name: "remote source with origin",
			query: ArtifactQuery{
				Name:   "demo",
				Kind:   ArtifactKindSolution,
				Source: SourceSelector{Kind: SourceRemote, Remote: "example.com"},
			},
		},
		{
			name:    "missing name",
			query:   ArtifactQuery{Kind: ArtifactKindSolution},
			wantErr: "name is required",
		},
		{
			name:    "missing kind",
			query:   ArtifactQuery{Name: "demo"},
			wantErr: "kind is required",
		},
		{
			name: "remote source without origin",
			query: ArtifactQuery{
				Name:   "demo",
				Kind:   ArtifactKindSolution,
				Source: SourceSelector{Kind: SourceRemote},
			},
			wantErr: "requires a remote origin",
		},
		{
			name: "origin without remote source kind",
			query: ArtifactQuery{
				Name:   "demo",
				Kind:   ArtifactKindSolution,
				Source: SourceSelector{Kind: SourceBuilt, Remote: "example.com"},
			},
			wantErr: "only valid with source kind remote",
		},
		{
			name: "digest is accepted",
			query: ArtifactQuery{
				Name:   "demo",
				Kind:   ArtifactKindSolution,
				Digest: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		{
			name: "invalid digest is rejected",
			query: ArtifactQuery{
				Name:   "demo",
				Kind:   ArtifactKindSolution,
				Digest: "not-a-digest",
			},
			wantErr: "is not a valid OCI digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.query.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, ErrInvalidQuery)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestArtifactQuery_Matches(t *testing.T) {
	builtInfo := ArtifactInfo{
		Reference: Reference{
			Kind:    ArtifactKindSolution,
			Name:    "demo",
			Version: semver.MustParse("1.2.3"),
			Built:   true,
		},
		Digest: "sha256:built",
		Annotations: map[string]string{
			AnnotationArtifactName: "demo",
			AnnotationArtifactType: "solution",
			AnnotationVersion:      "1.2.3",
		},
	}

	remoteInfo := ArtifactInfo{
		Reference: Reference{
			Kind:   ArtifactKindSolution,
			Name:   "demo",
			Remote: "example.com",
		},
		Digest: "sha256:remote",
		Annotations: map[string]string{
			AnnotationArtifactName:    "demo",
			AnnotationArtifactType:    "solution",
			AnnotationVersion:         "2.0.0",
			AnnotationSourceCanonical: "example.com",
		},
	}

	tests := []struct {
		name  string
		query ArtifactQuery
		info  ArtifactInfo
		want  bool
	}{
		{
			name:  "matches built artifact by exact version",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "1.2.3"},
			info:  builtInfo,
			want:  true,
		},
		{
			name:  "rejects kind mismatch",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindProvider},
			info:  builtInfo,
			want:  false,
		},
		{
			name:  "matches built provenance",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Source: SourceSelector{Kind: SourceBuilt}},
			info:  builtInfo,
			want:  true,
		},
		{
			name:  "rejects remote provenance for built selector",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Source: SourceSelector{Kind: SourceBuilt}},
			info:  remoteInfo,
			want:  false,
		},
		{
			name:  "matches remote origin",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Source: SourceSelector{Kind: SourceRemote, Remote: "example.com"}},
			info:  remoteInfo,
			want:  true,
		},
		{
			name:  "matches digest pin",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Digest: "sha256:built"},
			info:  builtInfo,
			want:  true,
		},
		{
			name:  "rejects digest mismatch",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Digest: "sha256:other"},
			info:  builtInfo,
			want:  false,
		},
		{
			name:  "matches digest and version together",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "1.2.3", Digest: "sha256:built"},
			info:  builtInfo,
			want:  true,
		},
		{
			name:  "rejects version mismatch even when digest matches",
			query: ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution, Version: "2.0.0", Digest: "sha256:built"},
			info:  builtInfo,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.query.matches(tt.info))
		})
	}
}

func TestSourceSelector_Matches(t *testing.T) {
	built := map[string]string{AnnotationArtifactName: "demo"}
	remote := map[string]string{AnnotationSourceCanonical: "example.com"}
	bare := map[string]string{}

	tests := []struct {
		name        string
		selector    SourceSelector
		annotations map[string]string
		want        bool
	}{
		{"any matches built", SourceSelector{Kind: SourceAny}, built, true},
		{"any matches remote", SourceSelector{Kind: SourceAny}, remote, true},
		{"any matches unannotated", SourceSelector{Kind: SourceAny}, bare, true},
		{"built matches built", SourceSelector{Kind: SourceBuilt}, built, true},
		{"built matches unannotated", SourceSelector{Kind: SourceBuilt}, bare, true},
		{"built rejects remote", SourceSelector{Kind: SourceBuilt}, remote, false},
		{"remote matches same origin", SourceSelector{Kind: SourceRemote, Remote: "example.com"}, remote, true},
		{"remote rejects other origin", SourceSelector{Kind: SourceRemote, Remote: "other.com"}, remote, false},
		{"remote rejects built", SourceSelector{Kind: SourceRemote, Remote: "example.com"}, built, false},
		{"remote rejects unannotated", SourceSelector{Kind: SourceRemote, Remote: "example.com"}, bare, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.selector.matches(tt.annotations))
		})
	}
}

func TestSortByVersionDesc(t *testing.T) {
	t.Run("orders semantically descending", func(t *testing.T) {
		artifacts := []ArtifactInfo{
			{Reference: Reference{Version: semver.MustParse("2.0.0")}},
			{Reference: Reference{Version: semver.MustParse("10.0.0")}},
			{Reference: Reference{Version: semver.MustParse("1.0.0")}},
		}

		sortByVersionDesc(artifacts)

		assert.Equal(t, "10.0.0", artifacts[0].Reference.Version.String())
		assert.Equal(t, "2.0.0", artifacts[1].Reference.Version.String())
		assert.Equal(t, "1.0.0", artifacts[2].Reference.Version.String())
	})

	t.Run("sorts versionless artifacts last", func(t *testing.T) {
		artifacts := []ArtifactInfo{
			{Reference: Reference{Version: nil}},
			{Reference: Reference{Version: semver.MustParse("1.0.0")}},
		}

		sortByVersionDesc(artifacts)

		require.NotNil(t, artifacts[0].Reference.Version)
		assert.Equal(t, "1.0.0", artifacts[0].Reference.Version.String())
		assert.Nil(t, artifacts[1].Reference.Version)
	})
}

func TestCheckAmbiguity(t *testing.T) {
	query := ArtifactQuery{Name: "demo", Kind: ArtifactKindSolution}

	tests := []struct {
		name      string
		artifacts []ArtifactInfo
		wantErr   bool
	}{
		{
			name: "single origin is unambiguous",
			artifacts: []ArtifactInfo{
				{Reference: Reference{Remote: "example.com"}},
				{Reference: Reference{Remote: "example.com"}},
			},
		},
		{
			name: "all built is unambiguous",
			artifacts: []ArtifactInfo{
				{Reference: Reference{Built: true}},
				{Reference: Reference{Built: true}},
			},
		},
		{
			name: "built mixed with remote is ambiguous",
			artifacts: []ArtifactInfo{
				{Reference: Reference{Built: true}},
				{Reference: Reference{Remote: "example.com"}},
			},
			wantErr: true,
		},
		{
			name: "two remote origins are ambiguous",
			artifacts: []ArtifactInfo{
				{Reference: Reference{Remote: "example.com"}},
				{Reference: Reference{Remote: "other.com"}},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkAmbiguity(query, tt.artifacts)
			if tt.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, ErrAmbiguousArtifact)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestVersionOrConstraint(t *testing.T) {
	t.Run("zero value matches any version", func(t *testing.T) {
		var vc VersionOrConstraint
		assert.Equal(t, LatestVersionConstraint, vc.semVarType)
		assert.True(t, vc.Satisfies("1.0.0"))
		assert.True(t, vc.Satisfies("random-tag"))
		assert.True(t, vc.Satisfies(""))
	})

	t.Run("empty string and latest match any version", func(t *testing.T) {
		for _, input := range []string{"", "latest"} {
			var vc VersionOrConstraint
			vc.Set(input)
			assert.Equal(t, LatestVersionConstraint, vc.semVarType)
			assert.True(t, vc.Satisfies("1.0.0"))
			assert.True(t, vc.Satisfies("2.3.4-beta.1"))
			assert.True(t, vc.Satisfies(""))
		}
	})

	t.Run("exact version matches only equal semver", func(t *testing.T) {
		var vc VersionOrConstraint
		vc.Set("1.2.3")
		assert.Equal(t, IsVersion, vc.semVarType)
		require.NotNil(t, vc.version)
		assert.Equal(t, "1.2.3", vc.version.String())

		assert.True(t, vc.Satisfies("1.2.3"))
		assert.True(t, vc.Satisfies("v1.2.3"), "v-prefix should be semver-equal")
		assert.False(t, vc.Satisfies("1.2.4"))
		assert.False(t, vc.Satisfies("2.0.0"))
		assert.False(t, vc.Satisfies("not-a-semver"))
		assert.False(t, vc.Satisfies(""))
	})

	t.Run("semver constraint matches satisfying versions", func(t *testing.T) {
		var vc VersionOrConstraint
		vc.Set(">=1.2.0, <2.0.0")
		assert.Equal(t, IsConstraint, vc.semVarType)
		require.NotNil(t, vc.constraint)

		assert.True(t, vc.Satisfies("1.2.0"))
		assert.True(t, vc.Satisfies("1.5.3"))
		assert.True(t, vc.Satisfies("1.9.9"))
		assert.False(t, vc.Satisfies("1.1.9"))
		assert.False(t, vc.Satisfies("2.0.0"))
		assert.False(t, vc.Satisfies("0.9.0"))
		assert.False(t, vc.Satisfies("invalid"))
	})

	t.Run("caret constraint", func(t *testing.T) {
		var vc VersionOrConstraint
		vc.Set("^1.2.3")
		assert.Equal(t, IsConstraint, vc.semVarType)

		assert.True(t, vc.Satisfies("1.2.3"))
		assert.True(t, vc.Satisfies("1.9.0"))
		assert.False(t, vc.Satisfies("2.0.0"))
		assert.False(t, vc.Satisfies("1.2.2"))
	})

	t.Run("tilde constraint", func(t *testing.T) {
		var vc VersionOrConstraint
		vc.Set("~1.2.3")
		assert.Equal(t, IsConstraint, vc.semVarType)

		assert.True(t, vc.Satisfies("1.2.3"))
		assert.True(t, vc.Satisfies("1.2.9"))
		assert.False(t, vc.Satisfies("1.3.0"))
		assert.False(t, vc.Satisfies("1.2.2"))
	})

	t.Run("wildcard constraint", func(t *testing.T) {
		var vc VersionOrConstraint
		vc.Set("1.x")
		assert.Equal(t, IsConstraint, vc.semVarType)

		assert.True(t, vc.Satisfies("1.0.0"))
		assert.True(t, vc.Satisfies("1.9.9"))
		assert.False(t, vc.Satisfies("2.0.0"))
	})

	t.Run("invalid version or constraint does not panic", func(t *testing.T) {
		var vc VersionOrConstraint
		vc.Set("!!!not-a-valid-version-or-constraint&&&")
		assert.Equal(t, InvalidVersionOrConstraint, vc.semVarType)
		assert.False(t, vc.Satisfies("1.0.0"))
		assert.False(t, vc.Satisfies(""))
	})
}
