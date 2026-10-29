// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/assert"
)

// dropAllAnnotations is a test transformer that discards every caller key.
func dropAllAnnotations(map[string]string) map[string]string {
	return map[string]string{}
}

func TestDropAnnotations(t *testing.T) {
	tests := []struct {
		name  string
		keys  []string
		input map[string]string
		want  map[string]string
	}{
		{
			name:  "drops listed keys",
			keys:  []string{"b"},
			input: map[string]string{"a": "1", "b": "2"},
			want:  map[string]string{"a": "1"},
		},
		{
			name:  "no keys keeps everything",
			input: map[string]string{"a": "1"},
			want:  map[string]string{"a": "1"},
		},
		{
			name: "nil input yields empty map",
			keys: []string{"a"},
			want: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, DropAnnotations(tt.keys...)(tt.input))
		})
	}
}

func TestDropAnnotations_DoesNotMutateInput(t *testing.T) {
	input := map[string]string{"a": "1", "b": "2"}
	_ = DropAnnotations("a")(input)
	assert.Equal(t, map[string]string{"a": "1", "b": "2"}, input)
}

func TestContentIdentity(t *testing.T) {
	createdAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("x", 3600))

	t.Run("ref wins over supplied identity", func(t *testing.T) {
		got := contentIdentity(solutionRef("new", "2.0.0"), createdAt, map[string]string{
			AnnotationArtifactName: "old",
			AnnotationVersion:      "1.0.0",
			AnnotationArtifactType: "provider",
			AnnotationCreated:      "yesterday",
			AnnotationTitle:        "keep me",
		})

		assert.Equal(t, ArtifactKindSolution.String(), got[AnnotationArtifactType])
		assert.Equal(t, "new", got[AnnotationArtifactName])
		assert.Equal(t, "2.0.0", got[AnnotationVersion])
		assert.Equal(t, "2025-01-02T02:04:05Z", got[AnnotationCreated])
		assert.Equal(t, "keep me", got[AnnotationTitle])
	})

	t.Run("version is removed when ref has none", func(t *testing.T) {
		ref := Reference{Kind: ArtifactKindSolution, Name: "demo"}
		got := contentIdentity(ref, createdAt, map[string]string{AnnotationVersion: "1.0.0"})
		assert.NotContains(t, got, AnnotationVersion)
	})

	t.Run("does not mutate input", func(t *testing.T) {
		input := map[string]string{AnnotationArtifactName: "old"}
		_ = contentIdentity(solutionRef("new", "1.0.0"), createdAt, input)
		assert.Equal(t, "old", input[AnnotationArtifactName])
	})
}

func TestStripDescriptorOnly(t *testing.T) {
	got := stripDescriptorOnly(map[string]string{
		AnnotationSourceCanonical: "example.com/solutions/demo",
		AnnotationOrigin:          "example.com",
		AnnotationTitle:           "keep me",
	})

	assert.Equal(t, map[string]string{AnnotationTitle: "keep me"}, got)
}

func TestLocalProvenance(t *testing.T) {
	t.Run("a remote reference sets canonical", func(t *testing.T) {
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "demo",
			Version: semver.MustParse("1.0.0"),
			Remote:  "example.com/solutions/demo",
		}
		got := localProvenance(ref, nil)

		assert.Equal(t, "example.com/solutions/demo", got[AnnotationSourceCanonical])
		assert.Equal(t, ArtifactKindSolution.String(), got[AnnotationArtifactType])
		assert.Equal(t, "demo", got[AnnotationArtifactName])
		assert.Equal(t, "1.0.0", got[AnnotationVersion])
	})

	t.Run("a built reference clears the inherited canonical", func(t *testing.T) {
		got := localProvenance(
			solutionRef("demo", "1.0.0"),
			map[string]string{AnnotationSourceCanonical: "example.com/solutions/old"},
		)

		assert.NotContains(t, got, AnnotationSourceCanonical,
			"a rebuilt artifact must not claim an origin the remote never published")
	})

	t.Run("identity is rewritten and unrelated annotations are kept", func(t *testing.T) {
		got := localProvenance(
			solutionRef("new", "2.0.0"),
			map[string]string{
				AnnotationArtifactName: "old",
				AnnotationVersion:      "1.0.0",
				AnnotationTitle:        "keep me",
			},
		)

		assert.Equal(t, "new", got[AnnotationArtifactName])
		assert.Equal(t, "2.0.0", got[AnnotationVersion])
		assert.Equal(t, "keep me", got[AnnotationTitle])
	})

	t.Run("a versionless reference keeps the existing version", func(t *testing.T) {
		got := localProvenance(
			Reference{Kind: ArtifactKindSolution, Name: "demo"},
			map[string]string{AnnotationVersion: "1.0.0"},
		)
		assert.Equal(t, "1.0.0", got[AnnotationVersion])
	})
}

func TestFormatCreated(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.FixedZone("x", -7200))
	assert.Equal(t, "2025-06-01T14:00:00Z", formatCreated(ts))
}
