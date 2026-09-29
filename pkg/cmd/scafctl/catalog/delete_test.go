// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"testing"

	"github.com/Masterminds/semver/v3"
	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandDelete(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "delete <name@version>", cmd.Use)
	assert.Contains(t, cmd.Aliases, "rm")
	assert.Contains(t, cmd.Aliases, "remove")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandDelete_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name string
	}{
		{"catalog"},
		{"kind"},
		{"origin"},
		{"insecure"},
		{"force"},
		{"dry-run"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			assert.NotNil(t, f, "flag %q should exist", tt.name)
		})
	}
}

func TestCommandDelete_CatalogFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("c")
	require.NotNil(t, f, "shorthand -c should exist")
	assert.Equal(t, "catalog", f.Name)
}

func TestCommandDelete_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires exactly 1 argument")
}

func TestCommandDelete_VersionRequired(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version required")
}

func TestCommandDelete_InvalidKind(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "--kind", "bogus"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
}

func TestCommandDelete_InvalidOrigin(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv
	useTempDataHome(t)

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "--origin", "not a valid origin"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --origin")
}

func TestCommandDelete_NotFound(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv
	useTempDataHome(t)

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"definitely-does-not-exist@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestLooksLikeRemoteReference(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		ref      string
		expected bool
	}{
		{
			name:     "simple name is local",
			ref:      "my-solution@1.0.0",
			expected: false,
		},
		{
			name:     "localhost prefix is remote",
			ref:      "localhost:5000/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "host with dot is remote",
			ref:      "ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "host with port is remote",
			ref:      "registry:5000/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "oci scheme prefix is remote",
			ref:      "oci://ghcr.io/myorg/solutions/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "docker registry is remote",
			ref:      "docker.io/myorg/my-solution@1.0.0",
			expected: true,
		},
		{
			name:     "no slash is local",
			ref:      "my-solution",
			expected: false,
		},
		{
			name:     "path without dot host is local",
			ref:      "myorg/my-solution",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := looksLikeRemoteReference(tt.ref)
			assert.Equal(t, tt.expected, result, "looksLikeRemoteReference(%q)", tt.ref)
		})
	}
}

func TestCommandDelete_AllFlag(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv
	useTempDataHome(t)

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--all", "--force"})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestCommandDelete_AllWithArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--all", "my-solution@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all cannot be used with positional arguments")
}

func TestCommandDelete_AllWithCatalog(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--all", "--catalog", "myregistry"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all only applies to the local catalog; cannot be combined with --catalog")
}

func TestCommandDelete_AllWithOrigin(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"--all", "--origin", "ghcr.io/myorg", "--force"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--origin only applies to a single local delete; cannot be combined with --all")
}

func TestCommandDelete_RemoteWithOrigin(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "--catalog", "myregistry", "--origin", "ghcr.io/myorg"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--origin not supported for remote delete")
}

func TestCommandDelete_RemoteReferenceWithOrigin(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0", "--origin", "ghcr.io/myorg"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--origin not supported for remote delete")
}

func TestRunDeleteAll_EmptyCatalog(t *testing.T) {
	// Set XDG_DATA_HOME to a temp dir so NewLocalCatalog creates an empty catalog
	useTempDataHome(t)
	ctx := newCatalogTestCtx(t)

	err := runDeleteAll(ctx, &DeleteOptions{Force: true, CliParams: settings.NewCliParams()})
	require.NoError(t, err)
}

func TestRunDeleteAll_DryRun(t *testing.T) {
	useTempDataHome(t)
	ctx := newCatalogTestCtx(t)

	err := runDeleteAll(ctx, &DeleteOptions{DryRun: true, Force: true, CliParams: settings.NewCliParams()})
	require.NoError(t, err)
}

func TestRunDeleteAll_RemovesEveryStoredTag(t *testing.T) {
	cat := newSeededLocalCatalog(t)
	ctx := context.Background()
	ref := catalogpkg.Reference{Kind: catalogpkg.ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}
	pulled := ref
	pulled.Origin = testPulledOrigin
	for _, r := range []catalogpkg.Reference{ref, pulled} {
		_, err := cat.Tag(ctx, r, "stable")
		require.NoError(t, err)
	}

	ctx, out := newBufferedCatalogTestCtx(t)
	err := runDeleteAll(ctx, &DeleteOptions{Force: true, CliParams: settings.NewCliParams()})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "Deleted 2 artifact(s)", "aliases are part of their identity, not separate artifacts")
	assert.NotContains(t, out.String(), "Failed to delete")

	infos, err := reopenLocalCatalog(t).List(ctx, catalogpkg.ArtifactKindSolution, "")
	require.NoError(t, err)
	assert.Empty(t, infos)
}

func TestCommandDelete_RemovesAliasesOfSelectedCopy(t *testing.T) {
	cat := newSeededLocalCatalog(t)
	ctx := context.Background()
	ref := catalogpkg.Reference{Kind: catalogpkg.ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}
	pulled := ref
	pulled.Origin = testPulledOrigin
	for _, r := range []catalogpkg.Reference{ref, pulled} {
		_, err := cat.Tag(ctx, r, "stable")
		require.NoError(t, err)
	}

	cmdCtx, out := newBufferedCatalogTestCtx(t)
	cmd := CommandDelete(settings.NewCliParams(), nil, "scafctl/catalog")
	cmd.SetContext(cmdCtx)
	cmd.SetArgs([]string{"app@1.0.0", "--origin", catalogpkg.OriginBuilt})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "(and aliases: stable)")
	assert.NotContains(t, out.String(), "still resolves")

	infos, err := reopenLocalCatalog(t).List(ctx, catalogpkg.ArtifactKindSolution, "app")
	require.NoError(t, err)
	require.Len(t, infos, 2, "the pulled copy and its alias must remain")
	for _, info := range infos {
		assert.Equal(t, testPulledOrigin, info.Canonical)
	}
}

func TestCommandDelete_WarnsWhenStaleAliasStillResolves(t *testing.T) {
	cat := newSeededLocalCatalog(t)
	ctx := context.Background()
	ref := catalogpkg.Reference{Kind: catalogpkg.ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}
	_, err := cat.Tag(ctx, ref, "stable")
	require.NoError(t, err)
	_, err = cat.Store(ctx, ref, []byte("rebuilt"), nil, nil, true)
	require.NoError(t, err)

	cmdCtx, out := newBufferedCatalogTestCtx(t)
	cmd := CommandDelete(settings.NewCliParams(), nil, "scafctl/catalog")
	cmd.SetContext(cmdCtx)
	cmd.SetArgs([]string{"app@1.0.0", "--origin", catalogpkg.OriginBuilt})
	require.NoError(t, cmd.Execute(), "the stale alias must not make the rebuilt version ambiguous")
	assert.Contains(t, out.String(), "Deleted app@")
	assert.Contains(t, out.String(), "still resolves to another local copy")
}

func TestCommandDelete_ByOriginLeavesOtherCopy(t *testing.T) {
	newSeededLocalCatalog(t)

	cmd := CommandDelete(settings.NewCliParams(), nil, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"app@1.0.0", "--origin", testPulledOrigin})
	require.NoError(t, cmd.Execute())

	infos, err := reopenLocalCatalog(t).List(context.Background(), catalogpkg.ArtifactKindSolution, "app")
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Empty(t, infos[0].Canonical, "only the built copy must remain")
}

func TestCommandDelete_AmbiguousLocalCopiesPrintHints(t *testing.T) {
	newSeededLocalCatalog(t)

	ctx, out := newBufferedCatalogTestCtx(t)
	cmd := CommandDelete(settings.NewCliParams(), nil, "scafctl/catalog")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"app@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.True(t, catalogpkg.IsAmbiguous(err))
	assert.Contains(t, out.String(), "catalog delete app@1.0.0 --kind solution --origin "+catalogpkg.OriginBuilt)
	assert.Contains(t, out.String(), "catalog delete app@1.0.0 --kind solution --origin "+testPulledOrigin)
}

func BenchmarkLooksLikeRemoteReference(b *testing.B) {
	refs := []string{
		"my-solution@1.0.0",
		"ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
		"localhost:5000/solutions/my-solution",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for _, ref := range refs {
			looksLikeRemoteReference(ref)
		}
	}
}

func BenchmarkCommandDelete(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	}
}
