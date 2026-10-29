// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		opts    DeleteOptions
		wantErr string
		wantIs  error
	}{
		{name: "empty reference", opts: DeleteOptions{}, wantErr: "version required"},
		{name: "missing version", opts: DeleteOptions{Reference: "my-artifact"}, wantErr: "version required"},
		{name: "origin with catalog", opts: DeleteOptions{Reference: "a@1.0.0", Origin: "built", Catalog: "ghcr.io/org"}, wantIs: ErrOriginWithCatalog},
		{name: "valid", opts: DeleteOptions{Reference: "a@1.0.0", Origin: "built"}},
		{name: "catalog alone is valid", opts: DeleteOptions{Reference: "a@1.0.0", Catalog: "ghcr.io/org"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateDelete(&tt.opts)
			switch {
			case tt.wantIs != nil:
				assert.ErrorIs(t, err, tt.wantIs)
			case tt.wantErr != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			default:
				assert.NoError(t, err)
			}
		})
	}
}

func TestDeleteReferenceBuilder(t *testing.T) {
	t.Parallel()

	t.Run("builds reference and source", func(t *testing.T) {
		t.Parallel()

		opts := &DeleteOptions{Reference: "my-artifact@1.2.3", Kind: string(catalog.ArtifactKindProvider), Origin: "oci://ghcr.io/org/plugins/"}
		build, err := newDeleteReferenceBuilder(t.Context(), opts).
			parseReference().
			parseOrigin().
			Build()
		require.NoError(t, err)
		assert.Equal(t, catalog.ArtifactKindProvider, build.Reference.Kind)
		assert.Equal(t, "my-artifact", build.Reference.Name)
		require.NotNil(t, build.Reference.Version)
		assert.Equal(t, "1.2.3", build.Reference.Version.String())
		assert.Equal(t, catalog.SourceSelector{Kind: catalog.SourceRemote, Remote: "ghcr.io/org/plugins"}, build.Source)
		assert.Equal(t, "ghcr.io/org/plugins", build.Reference.Remote)
		assert.False(t, build.Reference.Built)
	})

	t.Run("built origin marks reference built", func(t *testing.T) {
		t.Parallel()

		build, err := newDeleteReferenceBuilder(t.Context(), &DeleteOptions{Reference: "a@1.0.0", Kind: string(catalog.ArtifactKindSolution), Origin: "built"}).
			parseReference().
			parseOrigin().
			Build()
		require.NoError(t, err)
		assert.True(t, build.Reference.Built)
		assert.Empty(t, build.Reference.Remote)
	})

	t.Run("no origin selects any", func(t *testing.T) {
		t.Parallel()

		build, err := newDeleteReferenceBuilder(t.Context(), &DeleteOptions{Reference: "a@1.0.0", Kind: string(catalog.ArtifactKindSolution)}).
			parseReference().
			parseOrigin().
			Build()
		require.NoError(t, err)
		assert.Equal(t, catalog.SourceAny, build.Source.Kind)
		assert.False(t, build.Reference.Built)
		assert.Empty(t, build.Reference.Remote)
	})

	t.Run("invalid reference short-circuits", func(t *testing.T) {
		t.Parallel()

		_, err := newDeleteReferenceBuilder(t.Context(), &DeleteOptions{Reference: "a@", Origin: "oci://"}).
			parseReference().
			parseOrigin().
			Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid reference")
	})

	t.Run("invalid origin", func(t *testing.T) {
		t.Parallel()

		_, err := newDeleteReferenceBuilder(t.Context(), &DeleteOptions{Reference: "a@1.0.0", Origin: "oci://"}).
			parseReference().
			parseOrigin().
			Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid --origin")
	})
}
