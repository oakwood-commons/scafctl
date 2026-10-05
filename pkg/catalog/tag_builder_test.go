// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingTagger struct {
	desc ocispec.Descriptor
	tag  string
	err  error
}

func (r *recordingTagger) Tag(_ context.Context, desc ocispec.Descriptor, tag string) error {
	r.desc, r.tag = desc, tag
	return r.err
}

func TestTagBuilder_Build(t *testing.T) {
	base := ocispec.Descriptor{
		MediaType:   ocispec.MediaTypeImageManifest,
		Digest:      digest.FromString("m"),
		Size:        1,
		Annotations: map[string]string{"existing": "keep", "override": "old"},
	}

	t.Run("tags with merged annotations", func(t *testing.T) {
		tg := &recordingTagger{}
		b := NewTagBuilder("solution/demo:1.0.0").
			Annotation(AnnotationTitle, "demo").
			Annotation("empty", "").
			Annotations(map[string]string{"override": "new", AnnotationSourceCanonical: "example.com/demo"})
		got, err := b.Build(context.Background(), base, tg)
		require.NoError(t, err)

		assert.Equal(t, "solution/demo:1.0.0", b.Tag())
		assert.Equal(t, "solution/demo:1.0.0", tg.tag)
		assert.Equal(t, base.Digest, tg.desc.Digest)
		assert.Equal(t, "keep", tg.desc.Annotations["existing"])
		assert.Equal(t, "new", tg.desc.Annotations["override"])
		assert.Equal(t, "demo", tg.desc.Annotations[AnnotationTitle])
		assert.Equal(t, "example.com/demo", tg.desc.Annotations[AnnotationSourceCanonical])
		assert.NotContains(t, tg.desc.Annotations, "empty")
		assert.Equal(t, "old", base.Annotations["override"])
		assert.Equal(t, tg.desc, got, "Build returns the descriptor as tagged")
	})

	t.Run("transformer drops existing keys before merge", func(t *testing.T) {
		tg := &recordingTagger{}
		got, err := NewTagBuilder("t").
			AnnotationTransformer(DropAnnotations("existing", "override")).
			Annotation("override", "new").
			Build(context.Background(), base, tg)
		require.NoError(t, err)

		assert.NotContains(t, got.Annotations, "existing")
		assert.Equal(t, "new", got.Annotations["override"], "builder annotations are merged after the transformer")
	})

	t.Run("nil transformer restores copy-all", func(t *testing.T) {
		got, err := NewTagBuilder("t").
			AnnotationTransformer(dropAllAnnotations).
			AnnotationTransformer(nil).
			Build(context.Background(), base, &recordingTagger{})
		require.NoError(t, err)
		assert.Equal(t, "keep", got.Annotations["existing"])
	})

	t.Run("transformer returning nil yields an empty map", func(t *testing.T) {
		got, err := NewTagBuilder("t").
			AnnotationTransformer(func(map[string]string) map[string]string { return nil }).
			Annotation(AnnotationTitle, "demo").
			Build(context.Background(), base, &recordingTagger{})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{AnnotationTitle: "demo"}, got.Annotations)
	})

	t.Run("empty tag", func(t *testing.T) {
		_, err := NewTagBuilder("").Build(context.Background(), base, &recordingTagger{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tag is empty")
	})

	t.Run("nil tagger", func(t *testing.T) {
		_, err := NewTagBuilder("t").Build(context.Background(), base, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no tagger")
	})

	t.Run("tagger error is wrapped", func(t *testing.T) {
		boom := errors.New("boom")
		got, err := NewTagBuilder("t").Build(context.Background(), base, &recordingTagger{err: boom})
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), `tagging artifact "t"`)
		assert.Equal(t, ocispec.Descriptor{}, got)
	})
}

func TestNewLocalTagBuilder(t *testing.T) {
	base := ocispec.Descriptor{
		MediaType: ocispec.MediaTypeImageManifest,
		Digest:    digest.FromString("m"),
		Size:      1,
		Annotations: map[string]string{
			AnnotationSourceCanonical: "example.com/solutions/old",
			AnnotationArtifactName:    "old",
			AnnotationTitle:           "keep me",
		},
	}

	t.Run("a remote ref sets canonical and identity", func(t *testing.T) {
		ref := Reference{
			Kind:    ArtifactKindSolution,
			Name:    "demo",
			Version: semver.MustParse("1.0.0"),
			Remote:  "example.com/solutions/demo",
		}
		b, err := NewLocalTagBuilder(ref)
		require.NoError(t, err)
		wantTag, err := tagForLocalRef(ref)
		require.NoError(t, err)
		assert.Equal(t, wantTag, b.Tag())

		tg := &recordingTagger{}
		got, err := b.Build(context.Background(), base, tg)
		require.NoError(t, err)
		assert.Equal(t, wantTag, tg.tag)
		assert.Equal(t, "example.com/solutions/demo", got.Annotations[AnnotationSourceCanonical])
		assert.Equal(t, "demo", got.Annotations[AnnotationArtifactName])
		assert.Equal(t, "1.0.0", got.Annotations[AnnotationVersion])
		assert.Equal(t, "keep me", got.Annotations[AnnotationTitle])
	})

	t.Run("a built ref removes inherited canonical", func(t *testing.T) {
		b, err := NewLocalTagBuilder(solutionRef("demo", "1.0.0"))
		require.NoError(t, err)
		assert.Equal(t, "solution/demo:1.0.0", b.Tag())

		got, err := b.Build(context.Background(), base, &recordingTagger{})
		require.NoError(t, err)
		assert.NotContains(t, got.Annotations, AnnotationSourceCanonical)
	})

	t.Run("caller annotations cannot override provenance", func(t *testing.T) {
		b, err := NewLocalTagBuilder(solutionRef("demo", "1.0.0"))
		require.NoError(t, err)

		got, err := b.
			Annotation(AnnotationSourceCanonical, "example.com/spoofed").
			Annotation(AnnotationArtifactName, "spoofed").
			Build(context.Background(), base, &recordingTagger{})
		require.NoError(t, err)
		assert.NotContains(t, got.Annotations, AnnotationSourceCanonical)
		assert.Equal(t, "demo", got.Annotations[AnnotationArtifactName])
	})

	t.Run("invalid ref is rejected", func(t *testing.T) {
		_, err := NewLocalTagBuilder(Reference{Kind: ArtifactKindSolution, Name: "demo"})
		require.Error(t, err)
	})
}
