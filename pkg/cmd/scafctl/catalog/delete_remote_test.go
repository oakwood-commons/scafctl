// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"errors"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRemoteDeleter struct {
	deleteErr error
	deleted   []catalog.Reference
}

func (f *fakeRemoteDeleter) Delete(_ context.Context, ref catalog.Reference) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, ref)
	return nil
}

func (f *fakeRemoteDeleter) RepositoryPath(ref catalog.Reference) string {
	return "ghcr.io/org/scafctl/" + ref.Kind.Plural() + "/" + ref.Name
}

func remoteDeleteBuild() deleteReferenceBuild {
	return deleteReferenceBuild{
		Reference: catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    "my-sol",
			Version: semver.MustParse("1.0.0"),
		},
		Registry:   "ghcr.io",
		Repository: "org/scafctl",
	}
}

func TestDeleteFromRemote(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dryRun      bool
		deleteErr   error
		wantCode    int
		wantDeleted int
	}{
		{name: "deletes", wantDeleted: 1},
		{name: "dry run does not delete", dryRun: true},
		{
			name:      "not found",
			deleteErr: &catalog.ArtifactNotFoundError{Reference: catalog.Reference{Name: "my-sol"}, Catalog: "ghcr.io"},
			wantCode:  exitcode.FileNotFound,
		},
		{name: "unsupported registry", deleteErr: errors.New("405 method not allowed"), wantCode: exitcode.CatalogError},
		{name: "other failure", deleteErr: errors.New("boom"), wantCode: exitcode.CatalogError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &fakeRemoteDeleter{deleteErr: tt.deleteErr}
			build := remoteDeleteBuild()
			opts := &DeleteOptions{Reference: "my-sol@1.0.0", Catalog: "ghcr.io/org/scafctl", DryRun: tt.dryRun}

			err := deleteFromRemote(newCatalogTestCtx(t), opts, fake, build, build.Registry, build.Repository)

			if tt.wantCode == 0 {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Equal(t, tt.wantCode, exitcode.GetCode(err))
			}
			assert.Len(t, fake.deleted, tt.wantDeleted)
		})
	}
}

func TestDeleteReferenceBuilder_ResolveCatalogLocation(t *testing.T) {
	t.Parallel()

	t.Run("no catalog is a no-op", func(t *testing.T) {
		t.Parallel()

		build, err := newDeleteReferenceBuilder(t.Context(), &DeleteOptions{Reference: "a@1.0.0", Kind: string(catalog.ArtifactKindSolution)}).
			parseReference().
			parseOrigin().
			resolveCatalogLocation().
			Build()
		require.NoError(t, err)
		assert.Empty(t, build.Registry)
		assert.Empty(t, build.Repository)
	})

	t.Run("url catalog resolves registry and repository", func(t *testing.T) {
		t.Parallel()

		build, err := newDeleteReferenceBuilder(t.Context(), &DeleteOptions{
			Reference: "a@1.0.0",
			Kind:      string(catalog.ArtifactKindSolution),
			Catalog:   "ghcr.io/org/scafctl",
		}).
			parseReference().
			parseOrigin().
			resolveCatalogLocation().
			Build()
		require.NoError(t, err)
		assert.Equal(t, "ghcr.io", build.Registry)
		assert.Equal(t, "org/scafctl", build.Repository)
	})
}

func TestCommandDeleteKind_RemoteDryRun(t *testing.T) {
	t.Parallel()

	for _, dk := range deleteKinds {
		t.Run(string(dk.Kind), func(t *testing.T) {
			t.Parallel()

			ioStreams, _, _ := terminal.NewTestIOStreams()
			cmd := CommandDelete(settings.NewCliParams(), ioStreams, "scafctl/catalog")
			cmd.SetContext(newCatalogTestCtx(t))
			cmd.SetArgs([]string{string(dk.Kind), "my-artifact@1.0.0", "--catalog", "ghcr.io/org/scafctl", "--dry-run"})

			require.NoError(t, cmd.Execute())
		})
	}
}
