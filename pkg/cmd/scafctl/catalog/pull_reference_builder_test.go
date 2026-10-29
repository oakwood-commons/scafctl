// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullReferenceBuilder_Build(t *testing.T) {
	t.Parallel()

	t.Run("builds a versioned short reference from explicit catalog url", func(t *testing.T) {
		t.Parallel()

		build, err := newPullReferenceBuilder(newCatalogTestCtx(t), &PullOptions{
			Reference: "demo@1.2.3",
			Kind:      "solution",
			Catalog:   "ghcr.io/myorg/scafctl",
		}).
			parseShortReference().
			resolveCatalogLocation().
			Build()

		require.NoError(t, err)
		assert.Equal(t, catalog.ArtifactKindSolution, build.Reference.Kind)
		assert.Equal(t, "demo", build.Reference.Name)
		require.NotNil(t, build.Reference.Version)
		assert.Equal(t, "1.2.3", build.Reference.Version.String())
		assert.Equal(t, "ghcr.io", build.Registry)
		assert.Equal(t, "myorg/scafctl", build.Repository)
		assert.Equal(t, "ghcr.io/myorg/scafctl", build.Reference.Remote)
	})

	t.Run("builds an unversioned short reference", func(t *testing.T) {
		t.Parallel()

		build, err := newPullReferenceBuilder(newCatalogTestCtx(t), &PullOptions{
			Reference: "demo",
			Kind:      "provider",
			Catalog:   "ghcr.io/myorg/scafctl",
		}).
			parseShortReference().
			resolveCatalogLocation().
			Build()

		require.NoError(t, err)
		assert.Equal(t, catalog.ArtifactKindProvider, build.Reference.Kind)
		assert.Equal(t, "demo", build.Reference.Name)
		assert.Nil(t, build.Reference.Version)
		assert.Empty(t, build.Reference.Digest)
		assert.Equal(t, "ghcr.io/myorg/scafctl", build.Reference.Remote)
	})

	t.Run("invalid versioned reference returns an invalid reference error", func(t *testing.T) {
		t.Parallel()

		_, err := newPullReferenceBuilder(newCatalogTestCtx(t), &PullOptions{
			Reference: "demo@not-a-version",
			Kind:      "solution",
			Catalog:   "ghcr.io/myorg/scafctl",
		}).
			parseShortReference().
			resolveCatalogLocation().
			Build()

		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrInvalidReference)
	})

	t.Run("builds a full remote reference", func(t *testing.T) {
		t.Parallel()

		build, err := newPullReferenceBuilder(newCatalogTestCtx(t), &PullOptions{
			Reference: "ghcr.io/myorg/scafctl/solutions/demo@1.2.3",
		}).
			parseRemoteReference().
			Build()

		require.NoError(t, err)
		assert.Equal(t, catalog.ArtifactKindSolution, build.Reference.Kind)
		assert.Equal(t, "demo", build.Reference.Name)
		require.NotNil(t, build.Reference.Version)
		assert.Equal(t, "1.2.3", build.Reference.Version.String())
		assert.Equal(t, "ghcr.io", build.Registry)
		assert.Equal(t, "myorg/scafctl", build.Repository)
		assert.Equal(t, "ghcr.io/myorg/scafctl", build.Reference.Remote)
		assert.Equal(t, "1.2.3", build.Tag)
	})

	t.Run("full remote reference rejects catalog override", func(t *testing.T) {
		t.Parallel()

		_, err := newPullReferenceBuilder(newCatalogTestCtx(t), &PullOptions{
			Reference: "ghcr.io/myorg/scafctl/solutions/demo@1.2.3",
			Catalog:   "ghcr.io/other/catalog",
		}).
			parseRemoteReference().
			Build()

		require.Error(t, err)
		assert.ErrorIs(t, err, ErrCatalogWithRemoteReference)
	})
}
