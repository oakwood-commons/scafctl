// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testPullRegistry   = "ghcr.io"
	testPullRepository = "org/scafctl"
	testPullCatalogURL = testPullRegistry + "/" + testPullRepository
)

// newPullTestCommand wires the pull command (and its kind subcommands) with a
// non-default binary name so rendered output proves it honors the embedder.
func newPullTestCommand(t *testing.T) *cobra.Command {
	t.Helper()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPull(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	return cmd
}

func TestCommandPull_KindSubcommandsRegistered(t *testing.T) {
	t.Parallel()

	cmd := newPullTestCommand(t)

	tests := []struct {
		name  string
		alias string
	}{
		{name: "solution", alias: "sol"},
		{name: "solution", alias: "solutions"},
		{name: "provider", alias: "providers"},
		{name: "auth-handler", alias: "auth-handlers"},
	}
	for _, tt := range tests {
		sub, _, err := cmd.Find([]string{tt.name})
		require.NoError(t, err)
		assert.Equal(t, tt.name, sub.Name())

		viaAlias, _, err := cmd.Find([]string{tt.alias})
		require.NoError(t, err)
		assert.Equal(t, tt.name, viaAlias.Name(), "alias %q", tt.alias)
	}
}

func TestCommandPullKind_Flags(t *testing.T) {
	t.Parallel()

	for _, pk := range pullKinds {
		t.Run(string(pk.Kind), func(t *testing.T) {
			t.Parallel()

			sub, _, err := newPullTestCommand(t).Find([]string{string(pk.Kind)})
			require.NoError(t, err)

			for _, name := range []string{"catalog", "as", "version", "force", "dry-run", "insecure", "no-cache"} {
				assert.NotNil(t, sub.Flag(name), "flag --%s", name)
			}
			require.NotNil(t, sub.Flags().Lookup("catalog"))
			assert.Equal(t, "c", sub.Flags().Lookup("catalog").Shorthand)

			// Bundle verification only applies to solutions, the only kind with bundles.
			isSolution := pk.Kind == catalogpkg.ArtifactKindSolution
			assert.Equal(t, isSolution, sub.Flags().Lookup("strict") != nil, "--strict")
			assert.Equal(t, isSolution, sub.Flags().Lookup("no-verify") != nil, "--no-verify")

			// The kind comes from the subcommand, never from a flag.
			assert.Nil(t, sub.Flag("kind"))
		})
	}
}

func TestCommandPullKind_RequiresOneArg(t *testing.T) {
	t.Parallel()

	cmd := newPullTestCommand(t)
	cmd.SetArgs([]string{"solution"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestCommandPullKind_SetsKindFromSubcommand(t *testing.T) {
	t.Parallel()

	for _, pk := range pullKinds {
		t.Run(string(pk.Kind), func(t *testing.T) {
			t.Parallel()

			// An unsatisfiable --version constraint fails validation, which
			// only proves the kind was set if it got past the kind check.
			cmd := newPullTestCommand(t)
			cmd.SetArgs([]string{string(pk.Kind), "demo@1.0.0", "--version", "^2.0.0"})

			err := cmd.Execute()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrVersionConstraintConflict)
			assert.NotContains(t, err.Error(), "artifact kind is required")
		})
	}
}

func TestRunPullKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		opts     *PullOptions
		wantErr  string
		wantIs   error
		wantCode int
	}{
		{
			name:     "empty reference fails validation",
			opts:     &PullOptions{Kind: "solution"},
			wantErr:  "reference cannot be empty",
			wantIs:   catalogpkg.ErrInvalidReference,
			wantCode: exitcode.InvalidInput,
		},
		{
			name:     "full remote reference is rejected",
			opts:     &PullOptions{Reference: "ghcr.io/org/scafctl/solutions/demo@1.0.0", Kind: "solution"},
			wantErr:  "full remote references are not allowed",
			wantIs:   catalogpkg.ErrInvalidReference,
			wantCode: exitcode.InvalidInput,
		},
		{
			name:     "unknown kind fails validation",
			opts:     &PullOptions{Reference: "demo@1.0.0", Kind: "widget"},
			wantErr:  "artifact kind is required",
			wantIs:   catalogpkg.ErrInvalidReference,
			wantCode: exitcode.InvalidInput,
		},
		{
			name:     "conflicting version constraint fails validation",
			opts:     &PullOptions{Reference: "demo@1.0.0", Kind: "solution", VersionConstraint: "^2.0.0"},
			wantIs:   ErrVersionConstraintConflict,
			wantCode: exitcode.InvalidInput,
		},
		{
			name:     "bare --as is rejected",
			opts:     &PullOptions{Reference: "demo@1.0.0", Kind: "solution", TargetName: "renamed"},
			wantIs:   catalogpkg.ErrInvalidReference,
			wantCode: exitcode.InvalidInput,
		},
		{
			name:     "invalid version fails the build",
			opts:     &PullOptions{Reference: "demo@not-a-version", Kind: "solution", Catalog: testPullCatalogURL},
			wantErr:  "invalid reference",
			wantCode: exitcode.InvalidInput,
		},
		{
			name:     "unresolvable catalog fails the build",
			opts:     &PullOptions{Reference: "demo@1.0.0", Kind: "solution"},
			wantErr:  "no configuration loaded",
			wantCode: exitcode.InvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := runPullKind(newCatalogTestCtx(t), tt.opts)

			require.Error(t, err)
			if tt.wantErr != "" {
				assert.Contains(t, err.Error(), tt.wantErr)
			}
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
			assert.Equal(t, tt.wantCode, exitcode.GetCode(err))
		})
	}
}

func TestCommandPullKind_RejectsRemoteReference(t *testing.T) {
	t.Parallel()

	cmd := newPullTestCommand(t)
	cmd.SetArgs([]string{"solution", "ghcr.io/org/scafctl/solutions/demo@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "full remote references are not allowed")
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestPullReferenceBuilder_ParseShortReference(t *testing.T) {
	t.Parallel()

	t.Run("name only leaves the version open", func(t *testing.T) {
		t.Parallel()

		build, err := newPullReferenceBuilder(t.Context(), &PullOptions{Reference: "my-plugin", Kind: "provider"}).
			parseShortReference().
			Build()

		require.NoError(t, err)
		assert.Equal(t, catalogpkg.ArtifactKindProvider, build.Reference.Kind)
		assert.Equal(t, "my-plugin", build.Reference.Name)
		assert.False(t, build.Reference.HasVersion())
		assert.Empty(t, build.Tag)
	})

	t.Run("name and version", func(t *testing.T) {
		t.Parallel()

		build, err := newPullReferenceBuilder(t.Context(), &PullOptions{Reference: "my-sol@1.2.3", Kind: "solution"}).
			parseShortReference().
			Build()

		require.NoError(t, err)
		assert.Equal(t, "my-sol", build.Reference.Name)
		assert.Equal(t, "1.2.3", build.Tag)
		require.True(t, build.Reference.HasVersion())
		assert.Equal(t, "1.2.3", build.Reference.Version.String())
	})

	t.Run("invalid version is rejected", func(t *testing.T) {
		t.Parallel()

		_, err := newPullReferenceBuilder(t.Context(), &PullOptions{Reference: "my-sol@not-a-version", Kind: "solution"}).
			parseShortReference().
			Build()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid reference")
	})
}

func TestPullReferenceBuilder_ResolveCatalogLocation(t *testing.T) {
	t.Parallel()

	t.Run("explicit catalog URL", func(t *testing.T) {
		t.Parallel()

		build, err := newPullReferenceBuilder(t.Context(), &PullOptions{
			Reference: "my-sol@1.0.0",
			Kind:      "solution",
			Catalog:   testPullCatalogURL,
		}).
			parseShortReference().
			resolveCatalogLocation().
			Build()

		require.NoError(t, err)
		assert.Equal(t, testPullRegistry, build.Registry)
		assert.Equal(t, testPullRepository, build.Repository)
		assert.Equal(t, catalogpkg.CanonicalRemoteCatalogID(testPullRegistry, testPullRepository), build.Reference.Remote)
	})

	t.Run("no catalog and no config fails", func(t *testing.T) {
		t.Parallel()

		_, err := newPullReferenceBuilder(t.Context(), &PullOptions{Reference: "my-sol@1.0.0", Kind: "solution"}).
			parseShortReference().
			resolveCatalogLocation().
			Build()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no configuration loaded")
	})
}

func TestPullReferenceBuilder_FirstErrorShortCircuits(t *testing.T) {
	t.Parallel()

	_, err := newPullReferenceBuilder(t.Context(), &PullOptions{
		Reference: "my-sol@not-a-version",
		Kind:      "solution",
		Catalog:   testPullCatalogURL,
	}).
		parseShortReference().
		resolveCatalogLocation().
		Build()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid reference", "later steps must not replace the first error")
}
