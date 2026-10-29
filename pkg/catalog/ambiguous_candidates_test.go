// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAmbiguousArtifactError_Candidates(t *testing.T) {
	t.Parallel()

	err := &AmbiguousArtifactError{
		Matches: []ArtifactInfo{
			{Reference: Reference{Kind: ArtifactKindSolution, Name: "a", Built: true}, Digest: "sha256:built"},
			{
				Reference:   Reference{Kind: ArtifactKindSolution, Name: "a", Remote: "ghcr.io/org/plugins"},
				Annotations: map[string]string{AnnotationSourceCanonical: "ghcr.io/org/plugins"},
				Digest:      "sha256:remote",
			},
		},
	}

	got := err.Candidates()
	require.Len(t, got, 2)
	assert.Equal(t, SourceBuiltKeyword, got[0].Origin)
	assert.Equal(t, "sha256:built", got[0].Digest)
	assert.Equal(t, "ghcr.io/org/plugins", got[1].Origin)
	assert.Equal(t, "sha256:remote", got[1].Digest)

	// Every origin must round-trip through the --origin parser to the same selector.
	for _, c := range got {
		sel, parseErr := ParseSourceSelector(c.Origin)
		require.NoError(t, parseErr)
		assert.True(t, sel.matches(map[string]string{AnnotationSourceCanonical: remoteOf(c)}))
	}
}

// remoteOf returns the canonical remote an origin value stands for ("" for built).
func remoteOf(c AmbiguousCandidate) string {
	if c.Origin == SourceBuiltKeyword {
		return ""
	}
	return c.Origin
}

func TestAmbiguousArtifactError_Candidates_Empty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, (&AmbiguousArtifactError{}).Candidates())
}
