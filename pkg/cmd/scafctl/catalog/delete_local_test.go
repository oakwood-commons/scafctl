// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"bytes"
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPluginsOrigin = "ghcr.io/org/plugins"

// newDeleteLocalCatalog returns an isolated local catalog. It does not touch
// XDG paths, so tests using it can run in parallel.
func newDeleteLocalCatalog(t *testing.T) *catalog.LocalCatalog {
	t.Helper()
	cat, err := catalog.NewLocalCatalogAt(t.TempDir(), logr.Discard())
	require.NoError(t, err)
	return cat
}

// newCapturingCtx returns a context whose writer output is captured in the
// returned buffer.
func newCapturingCtx(t *testing.T) (context.Context, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	return writer.WithWriter(context.Background(), w), &buf
}

// seedLocal stores one artifact. An empty origin stores it as built; otherwise
// it is stored as pulled from that registry, mirroring what a pull records.
func seedLocal(t *testing.T, cat *catalog.LocalCatalog, kind catalog.ArtifactKind, name, version, origin string) catalog.Reference {
	t.Helper()

	ref := catalog.Reference{Kind: kind, Name: name, Version: semver.MustParse(version)}
	annotations := map[string]string{}
	if origin == "" {
		ref.Built = true
	} else {
		ref.Remote = origin
		annotations[catalog.AnnotationSourceCanonical] = origin
	}

	_, err := cat.Store(t.Context(), ref, []byte("name: "+name+"\norigin: "+origin+"\nversion: "+version), nil, annotations, false)
	require.NoError(t, err)
	return ref
}

func requireLocalExists(t *testing.T, cat *catalog.LocalCatalog, ref catalog.Reference, want bool) {
	t.Helper()
	exists, err := cat.Exists(t.Context(), ref)
	require.NoError(t, err)
	assert.Equal(t, want, exists, "Exists(%s)", ref.String())
}

// buildDelete parses opts the same way the command pipeline does.
func buildDelete(t *testing.T, opts *DeleteOptions) deleteReferenceBuild {
	t.Helper()
	build, err := newDeleteReferenceBuilder(t.Context(), opts).
		parseReference().
		parseOrigin().
		Build()
	require.NoError(t, err)
	return build
}

func deleteOpts(kind catalog.ArtifactKind, reference, origin string) *DeleteOptions {
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	return &DeleteOptions{
		Reference: reference,
		Kind:      string(kind),
		Origin:    origin,
		CliParams: cliParams,
	}
}

func TestDeleteFromLocal_NoOrigin(t *testing.T) {
	t.Parallel()

	t.Run("single match is deleted", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		ref := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		ctx, out := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		requireLocalExists(t, cat, ref, false)
		assert.Contains(t, out.String(), "Deleted")
	})

	t.Run("single remote-origin match is deleted without --origin", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		ref := seedLocal(t, cat, catalog.ArtifactKindProvider, "my-plugin", "1.0.0", testPluginsOrigin)
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindProvider, "my-plugin@1.0.0", "")

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		requireLocalExists(t, cat, ref, false)
	})

	t.Run("missing artifact is not found", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		ctx, out := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "ghost@1.0.0", "")

		err := deleteFromLocal(ctx, opts, cat, buildDelete(t, opts))

		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
		assert.Contains(t, out.String(), "not found")
	})

	t.Run("ambiguous across origins deletes nothing and prints commands", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		built := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		pulled := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
		ctx, out := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")

		err := deleteFromLocal(ctx, opts, cat, buildDelete(t, opts))

		require.Error(t, err)
		assert.ErrorIs(t, err, catalog.ErrAmbiguousArtifact)
		assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		assert.Contains(t, out.String(), "re-run the same command with one of:")
		assert.Contains(t, out.String(), "--origin built")
		assert.Contains(t, out.String(), "--origin "+testPluginsOrigin)
		requireLocalExists(t, cat, built, true)
		requireLocalExists(t, cat, pulled, true)
	})
}

func TestDeleteFromLocal_ExplicitOrigin(t *testing.T) {
	t.Parallel()

	t.Run("built deletes only the built copy", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		built := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		pulled := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "built")

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		requireLocalExists(t, cat, built, false)
		requireLocalExists(t, cat, pulled, true)
	})

	t.Run("registry deletes only that registry's copy", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		built := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		pulled := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", testPluginsOrigin)

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		requireLocalExists(t, cat, pulled, false)
		requireLocalExists(t, cat, built, true)
	})

	t.Run("one registry among several", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		a := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
		b := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "example.com/other")
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "example.com/other")

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		requireLocalExists(t, cat, a, true)
		requireLocalExists(t, cat, b, false)
	})

	t.Run("origin that does not hold the artifact is not found", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		built := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "example.com/elsewhere")

		err := deleteFromLocal(ctx, opts, cat, buildDelete(t, opts))

		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
		requireLocalExists(t, cat, built, true)
	})
}

func TestDeleteFromLocal_Isolation(t *testing.T) {
	t.Parallel()

	t.Run("other kinds with the same name and version are untouched", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		sol := seedLocal(t, cat, catalog.ArtifactKindSolution, "shared", "1.0.0", "")
		prov := seedLocal(t, cat, catalog.ArtifactKindProvider, "shared", "1.0.0", "")
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindProvider, "shared@1.0.0", "")

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		requireLocalExists(t, cat, prov, false)
		requireLocalExists(t, cat, sol, true)
	})

	t.Run("other versions of the same name are untouched", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		v1 := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		v2 := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "2.0.0", "")
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		requireLocalExists(t, cat, v1, false)
		requireLocalExists(t, cat, v2, true)
	})

	t.Run("same version under different origins is not ambiguous when versions differ", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		built := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		pulled := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "2.0.0", testPluginsOrigin)
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		requireLocalExists(t, cat, built, false)
		requireLocalExists(t, cat, pulled, true)
	})
}

func TestDeleteFromLocal_DryRun(t *testing.T) {
	t.Parallel()

	t.Run("reports and keeps the artifact", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		ref := seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		ctx, out := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")
		opts.DryRun = true

		require.NoError(t, deleteFromLocal(ctx, opts, cat, buildDelete(t, opts)))

		assert.Contains(t, out.String(), "Would delete")
		requireLocalExists(t, cat, ref, true)
	})

	t.Run("explicit origin that is missing is not found", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "built")
		opts.DryRun = true

		err := deleteFromLocal(ctx, opts, cat, buildDelete(t, opts))

		require.Error(t, err)
		assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
	})

	t.Run("ambiguity is still reported", func(t *testing.T) {
		t.Parallel()

		cat := newDeleteLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
		ctx, _ := newCapturingCtx(t)
		opts := deleteOpts(catalog.ArtifactKindSolution, "my-sol@1.0.0", "")
		opts.DryRun = true

		err := deleteFromLocal(ctx, opts, cat, buildDelete(t, opts))

		assert.ErrorIs(t, err, catalog.ErrAmbiguousArtifact)
	})
}

// The command tests below go through the real XDG-resolved catalog, so they
// set XDG_DATA_HOME and must not run in parallel.

func TestCommandDeleteKind_LocalNotFound(t *testing.T) {
	for _, dk := range deleteKinds {
		t.Run(string(dk.Kind), func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", t.TempDir())

			cliParams := settings.NewCliParams()
			ioStreams, _, _ := terminal.NewTestIOStreams()
			cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
			cmd.SetContext(newCatalogTestCtx(t))
			cmd.SetArgs([]string{string(dk.Kind), "my-artifact@1.0.0"})

			err := cmd.Execute()

			require.Error(t, err)
			assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
		})
	}
}

func TestCommandDeleteKind_OriginFlag(t *testing.T) {
	tests := []struct {
		name        string
		origin      string
		wantErr     string
		wantErrCode int
	}{
		{name: "built reaches the local catalog", origin: "built", wantErrCode: exitcode.FileNotFound},
		{name: "registry reaches the local catalog", origin: "oci://ghcr.io/org/plugins/", wantErrCode: exitcode.FileNotFound},
		{name: "empty registry rejected", origin: "oci://", wantErr: "invalid --origin", wantErrCode: exitcode.InvalidInput},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_DATA_HOME", t.TempDir())

			cliParams := settings.NewCliParams()
			ioStreams, _, _ := terminal.NewTestIOStreams()
			cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
			cmd.SetContext(newCatalogTestCtx(t))
			cmd.SetArgs([]string{"solution", "my-artifact@1.0.0", "--origin", tt.origin})

			err := cmd.Execute()

			require.Error(t, err)
			assert.Equal(t, tt.wantErrCode, exitcode.GetCode(err))
			if tt.wantErr != "" {
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}
