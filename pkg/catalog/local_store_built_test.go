// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func builtSolutionBuilder(ref Reference, content string) *ManifestBuilder {
	return NewManifestBuilder(ref).
		Layers(Layer{MediaType: MediaTypeSolutionContent, Data: []byte(content)})
}

func TestLocalCatalog_StoreBuilt(t *testing.T) {
	t.Parallel()

	t.Run("tags as built and records origin", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		cat := newTestCatalog(t)

		info, err := cat.StoreBuilt(ctx, builtSolutionBuilder(solutionRef("demo", "1.0.0"), "name: demo"), false)
		require.NoError(t, err)
		assert.Equal(t, "solution/demo:1.0.0", info.Tag)
		assert.True(t, info.Reference.Built)
		assert.Empty(t, info.Reference.Remote)
		assert.Equal(t, originBuilt, info.Annotations[AnnotationOrigin])
		assert.NotContains(t, info.Annotations, AnnotationSourceCanonical)

		// Origin is descriptor-only: it must never enter the manifest blob.
		desc, err := cat.store.Resolve(ctx, info.Tag)
		require.NoError(t, err)
		manifestAnns, err := cat.getManifestAnnotations(ctx, desc)
		require.NoError(t, err)
		assert.NotContains(t, manifestAnns, AnnotationOrigin)
	})

	t.Run("ignores a pulled copy of the same version", func(t *testing.T) {
		t.Parallel()
		ctx := t.Context()
		cat := newTestCatalog(t)

		const origin = "ghcr.io/org/solutions"
		pulled := solutionRef("demo", "1.0.0")
		pulled.Remote = origin
		_, err := cat.Store(ctx, pulled, []byte("pulled"), nil,
			map[string]string{AnnotationSourceCanonical: origin}, false)
		require.NoError(t, err)

		_, err = cat.StoreBuilt(ctx, builtSolutionBuilder(solutionRef("demo", "1.0.0"), "built"), false)
		require.NoError(t, err)

		_, err = cat.StoreBuilt(ctx, builtSolutionBuilder(solutionRef("demo", "1.0.0"), "built again"), false)
		var existsErr *ArtifactExistsError
		require.ErrorAs(t, err, &existsErr, "the built copy blocks a second build")

		_, err = cat.StoreBuilt(ctx, builtSolutionBuilder(solutionRef("demo", "1.0.0"), "forced"), true)
		require.NoError(t, err)

		content, _, err := cat.Fetch(ctx, pulled)
		require.NoError(t, err)
		assert.Equal(t, "pulled", string(content), "force never overwrites the pulled copy")
	})

	t.Run("rejects a remote origin", func(t *testing.T) {
		t.Parallel()
		cat := newTestCatalog(t)
		ref := solutionRef("demo", "1.0.0")
		ref.Remote = "ghcr.io/org/solutions"

		_, err := cat.StoreBuilt(t.Context(), builtSolutionBuilder(ref, "x"), false)
		var invalid *InvalidReferenceError
		require.ErrorAs(t, err, &invalid)
	})

	t.Run("rejects a nil builder", func(t *testing.T) {
		t.Parallel()
		cat := newTestCatalog(t)

		_, err := cat.StoreBuilt(t.Context(), nil, false)
		var builderErr *BuilderError
		require.ErrorAs(t, err, &builderErr)
	})
}

// Every descriptor-only key must stay out of the manifest blob for both
// legacy store paths, not just AnnotationSourceCanonical.
func TestLocalCatalog_Store_ManifestExcludesDescriptorOnly(t *testing.T) {
	t.Parallel()

	// Store and StoreDedup stamp into the caller's map, so each call gets a
	// fresh one.
	annotations := func() map[string]string {
		return map[string]string{
			AnnotationSourceCanonical: "ghcr.io/org/demo",
			AnnotationOrigin:          originBuilt,
			"keep.me":                 "yes",
		}
	}

	tests := map[string]func(cat *LocalCatalog, ref Reference) (ArtifactInfo, error){
		"Store": func(cat *LocalCatalog, ref Reference) (ArtifactInfo, error) {
			return cat.Store(t.Context(), ref, []byte("name: demo"), nil, annotations(), false)
		},
		"StoreDedup": func(cat *LocalCatalog, ref Reference) (ArtifactInfo, error) {
			return cat.StoreDedup(t.Context(), ref, []byte("name: demo"), []byte(`{"version":2,"root":".","files":[]}`), nil, nil, annotations(), false)
		},
	}

	for name, store := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cat := newTestCatalog(t)

			info, err := store(cat, solutionRef("demo", "1.0.0"))
			require.NoError(t, err)

			desc, err := cat.store.Resolve(t.Context(), cat.tagForRef(info.Reference))
			require.NoError(t, err)
			manifestAnns, err := cat.getManifestAnnotations(t.Context(), desc)
			require.NoError(t, err)
			for _, key := range descriptorOnlyAnnotations {
				assert.NotContains(t, manifestAnns, key)
			}
			assert.Equal(t, "yes", manifestAnns["keep.me"])
		})
	}
}
