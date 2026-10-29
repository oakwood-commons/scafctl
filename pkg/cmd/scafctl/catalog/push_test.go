// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandPush(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "push <reference>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandPush_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"origin", ""},
		{"as", ""},
		{"force", "false"},
		{"dry-run", "false"},
		{"insecure", "false"},
		{"latest", "false"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.PersistentFlags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			assert.Equal(t, tt.defValue, f.DefValue, "flag %q default value", tt.name)
		})
	}

	noSBOM := cmd.Flags().Lookup("no-sbom")
	require.NotNil(t, noSBOM, "flag %q should exist", "no-sbom")
	assert.Equal(t, "false", noSBOM.DefValue, "flag %q default value", "no-sbom")
}

func TestCommandPush_NoCatalogOrKindFlags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	assert.Nil(t, cmd.Flags().Lookup("catalog"), "the destination comes from the reference")
	assert.Nil(t, cmd.Flags().ShorthandLookup("c"))
	assert.Nil(t, cmd.Flags().Lookup("kind"), "the kind comes from the reference path")
}

func TestCommandPush_PersistentFlags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	for _, name := range []string{"origin", "as", "force", "dry-run", "insecure", "latest"} {
		assert.NotNil(t, cmd.PersistentFlags().Lookup(name), "persistent flag %q should exist", name)
	}
	assert.NotNil(t, cmd.Flags().Lookup("no-sbom"), "local flag --no-sbom should exist")
}

func TestCommandPush_ForceFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.PersistentFlags().ShorthandLookup("f")
	require.NotNil(t, f, "shorthand -f should exist")
	assert.Equal(t, "force", f.Name)
}

func TestCommandPush_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument: <reference>")
}

func TestCommandPush_ShortReferenceRejected(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0"})

	err := cmd.Execute()

	require.ErrorIs(t, err, ErrRemoteReferenceRequired)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, err.Error(), "mycli catalog push <kind>")
}

func TestValidatePushRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		reference  string
		targetName string
		wantErr    string
		wantIs     error
	}{
		{name: "full reference", reference: "ghcr.io/org/scafctl/solutions/my-sol@1.0.0"},
		{name: "oci scheme", reference: "oci://ghcr.io/org/scafctl/providers/my-plugin@1.0.0"},
		{name: "no version", reference: "ghcr.io/org/scafctl/solutions/my-sol"},
		{name: "valid rename", reference: "ghcr.io/org/scafctl/solutions/my-sol@1.0.0", targetName: "renamed@2.0.0"},
		{name: "short name", reference: "my-sol@1.0.0", wantIs: ErrRemoteReferenceRequired},
		{name: "no kind in path", reference: "ghcr.io/org/my-sol@1.0.0", wantErr: "must include the artifact kind"},
		{name: "invalid version", reference: "ghcr.io/org/scafctl/solutions/my-sol@not-a-version", wantErr: "invalid reference"},
		{name: "rename without version", reference: "ghcr.io/org/scafctl/solutions/my-sol@1.0.0", targetName: "renamed", wantErr: "bare name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			opts := pushOpts("", tt.reference, "")
			opts.TargetName = tt.targetName

			err := validatePushRoot(opts)

			switch {
			case tt.wantIs != nil:
				require.ErrorIs(t, err, tt.wantIs)
				assert.Contains(t, err.Error(), "mycli catalog push <kind>")
			case tt.wantErr != "":
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			default:
				require.NoError(t, err)
			}
		})
	}
}

func TestPushReferenceBuilder_ParseRemoteReference(t *testing.T) {
	t.Parallel()

	t.Run("splits identity and destination", func(t *testing.T) {
		t.Parallel()

		opts := pushOpts("", "ghcr.io/org/scafctl/solutions/my-sol@1.0.0", "")

		build, err := newPushReferenceBuilder(t.Context(), opts).parseRemoteReference().Build()

		require.NoError(t, err)
		assert.Equal(t, catalog.ArtifactKindSolution, build.Reference.Kind)
		assert.Equal(t, "my-sol", build.Reference.Name)
		assert.Equal(t, "1.0.0", build.Reference.Version.String())
		assert.Equal(t, "ghcr.io", build.Registry)
		assert.Equal(t, "org/scafctl", build.Repository)
		assert.Empty(t, build.Reference.Remote, "the destination must not leak into the local origin filter")
	})

	t.Run("oci scheme and open version", func(t *testing.T) {
		t.Parallel()

		opts := pushOpts("", "oci://ghcr.io/org/scafctl/providers/my-plugin", "")

		build, err := newPushReferenceBuilder(t.Context(), opts).parseRemoteReference().Build()

		require.NoError(t, err)
		assert.Equal(t, catalog.ArtifactKindProvider, build.Reference.Kind)
		assert.Nil(t, build.Reference.Version)
		assert.Equal(t, "ghcr.io", build.Registry)
	})

	t.Run("digest is rejected", func(t *testing.T) {
		t.Parallel()

		opts := pushOpts("", "ghcr.io/org/scafctl/solutions/my-sol@"+testPushDigest, "")

		_, err := newPushReferenceBuilder(t.Context(), opts).parseRemoteReference().rejectDigest().Build()

		require.Error(t, err)
		assert.Contains(t, err.Error(), pushDigestUnsupportedMsg)
	})

	t.Run("invalid reference", func(t *testing.T) {
		t.Parallel()

		opts := pushOpts("", "ghcr.io/org/scafctl/solutions/my-sol@not-a-version", "")

		_, err := newPushReferenceBuilder(t.Context(), opts).parseRemoteReference().Build()

		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid reference")
	})
}

// The command tests below go through the real XDG-resolved catalog, so they
// set XDG_DATA_HOME and must not run in parallel.

const testPushRootRef = "ghcr.io/org/scafctl/solutions/my-sol@1.0.0"

func TestCommandPush_RootDryRun(t *testing.T) {
	t.Run("built artifact", func(t *testing.T) {
		cat := newXDGLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		ctx, out := newCapturingCtx(t)
		cmd := newPushTestCommand(ctx)
		cmd.SetArgs([]string{testPushRootRef, "--dry-run"})

		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "Would push my-sol@1.0.0 (origin: built) to ghcr.io/org/scafctl/solutions/my-sol")
	})

	t.Run("open version resolves to the latest local version", func(t *testing.T) {
		cat := newXDGLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.1.0", "")
		ctx, out := newCapturingCtx(t)
		cmd := newPushTestCommand(ctx)
		cmd.SetArgs([]string{"ghcr.io/org/scafctl/solutions/my-sol", "--dry-run"})

		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "Would push my-sol@1.1.0")
	})

	t.Run("rename with force", func(t *testing.T) {
		cat := newXDGLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		ctx, out := newCapturingCtx(t)
		cmd := newPushTestCommand(ctx)
		cmd.SetArgs([]string{testPushRootRef, "--as", "renamed@2.0.0", "--force", "--dry-run"})

		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "ghcr.io/org/scafctl/solutions/renamed")
		assert.Contains(t, out.String(), "--as: would publish as renamed@2.0.0")
		assert.Contains(t, out.String(), "--force: would overwrite if exists")
	})

	t.Run("--origin picks the pulled copy", func(t *testing.T) {
		cat := newXDGLocalCatalog(t)
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
		seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
		ctx, out := newCapturingCtx(t)
		cmd := newPushTestCommand(ctx)
		cmd.SetArgs([]string{testPushRootRef, "--origin", testPluginsOrigin, "--dry-run"})

		require.NoError(t, cmd.Execute())

		assert.Contains(t, out.String(), "(origin: "+testPluginsOrigin+")")
	})
}

func TestCommandPush_RootLocalNotFound(t *testing.T) {
	newXDGLocalCatalog(t)
	cmd := newPushTestCommand(newCatalogTestCtx(t))
	cmd.SetArgs([]string{testPushRootRef, "--dry-run"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Equal(t, exitcode.FileNotFound, exitcode.GetCode(err))
}

func TestCommandPush_RootAmbiguousOrigin(t *testing.T) {
	cat := newXDGLocalCatalog(t)
	seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", "")
	seedLocal(t, cat, catalog.ArtifactKindSolution, "my-sol", "1.0.0", testPluginsOrigin)
	ctx, out := newCapturingCtx(t)
	cmd := newPushTestCommand(ctx)
	cmd.SetArgs([]string{testPushRootRef, "--dry-run"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, out.String(), "--origin built")
	assert.Contains(t, out.String(), "--origin "+testPluginsOrigin)
}

func TestCommandPush_RootInvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "digest reference", args: []string{"ghcr.io/org/scafctl/solutions/my-sol@" + testPushDigest}, wantErr: pushDigestUnsupportedMsg},
		{name: "empty origin registry", args: []string{testPushRootRef, "--origin", "oci://"}, wantErr: "invalid --origin"},
		{name: "rename without version", args: []string{testPushRootRef, "--as", "renamed"}, wantErr: "bare name"},
		{name: "no kind in path", args: []string{"ghcr.io/org/my-sol@1.0.0"}, wantErr: "must include the artifact kind"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newXDGLocalCatalog(t)
			cmd := newPushTestCommand(newCatalogTestCtx(t))
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
		})
	}
}

func TestCommandPush_NoSBOMFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	// --sbom flag should no longer exist
	assert.Nil(t, cmd.Flags().Lookup("sbom"), "sbom flag should be removed")

	noSBOM := cmd.Flags().Lookup("no-sbom")
	require.NotNil(t, noSBOM, "no-sbom flag should exist")
	assert.Equal(t, "false", noSBOM.DefValue)
}

func TestShouldAttachSBOM(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		kind   catalog.ArtifactKind
		noSBOM bool
		want   bool
	}{
		{
			name:   "solution kind attaches SBOM",
			kind:   catalog.ArtifactKindSolution,
			noSBOM: false,
			want:   true,
		},
		{
			name:   "empty kind defaults to SBOM",
			kind:   "",
			noSBOM: false,
			want:   true,
		},
		{
			name:   "provider kind skips SBOM",
			kind:   catalog.ArtifactKindProvider,
			noSBOM: false,
			want:   false,
		},
		{
			name:   "auth-handler kind skips SBOM",
			kind:   catalog.ArtifactKindAuthHandler,
			noSBOM: false,
			want:   false,
		},
		{
			name:   "solution with --no-sbom skips SBOM",
			kind:   catalog.ArtifactKindSolution,
			noSBOM: true,
			want:   false,
		},
		{
			name:   "empty kind with --no-sbom skips SBOM",
			kind:   "",
			noSBOM: true,
			want:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := shouldAttachSBOM(tc.kind, tc.noSBOM)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCommandPush_LatestFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPush(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.PersistentFlags().Lookup("latest")
	require.NotNil(t, f, "latest flag should exist")
	assert.Equal(t, "false", f.DefValue)
}

func BenchmarkCommandPush(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandPush(cliParams, ioStreams, "scafctl/catalog")
	}
}
