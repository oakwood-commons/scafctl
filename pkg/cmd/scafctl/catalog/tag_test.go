// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsValidTagChar(t *testing.T) {
	valid := []rune{'a', 'z', 'A', 'Z', '0', '9', '_', '.', '-'}
	for _, ch := range valid {
		assert.True(t, catalogpkg.IsValidTagChar(ch), "expected %q to be valid", string(ch))
	}

	invalid := []rune{'/', ':', ' ', '@', '#', '!', '(', ')'}
	for _, ch := range invalid {
		assert.False(t, catalogpkg.IsValidTagChar(ch), "expected %q to be invalid", string(ch))
	}
}

func TestCommandTag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "tag <name@version> <alias>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandTag_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"catalog", ""},
		{"kind", ""},
		{"origin", ""},
		{"insecure", "false"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			assert.Equal(t, tt.defValue, f.DefValue, "flag %q default value", tt.name)
		})
	}
}

func TestCommandTag_CatalogFlagShorthand(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")

	f := cmd.Flags().ShorthandLookup("c")
	require.NotNil(t, f, "shorthand -c should exist")
	assert.Equal(t, "catalog", f.Name)
}

func TestCommandTag_RequiresTwoArgs(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()

	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{}},
		{"one arg", []string{"my-solution@1.0.0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := CommandTag(settings.NewCliParams(), ioStreams, "scafctl/catalog")
			c.SetArgs(tt.args)
			err := c.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "accepts 2 arg(s)")
		})
	}
}

func TestCommandTag_InvalidAlias_SemverVersion(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	// Semver versions are not valid aliases
	cmd.SetArgs([]string{"my-solution@1.0.0", "2.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandTag_MissingVersion(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	// No version in reference
	cmd.SetArgs([]string{"my-solution", "stable"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version required")
}

func TestCommandTag_InvalidKind(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "stable", "--kind", "not-a-valid-kind"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
}

func TestCommandTag_RemoteSkipsKindInference(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	// With --catalog set, kind inference from local catalog is skipped.
	// This will fail at catalog URL resolution (no config), not at kind inference.
	cmd.SetArgs([]string{"nonexistent@1.0.0", "stable", "--catalog", "my-registry"})

	err := cmd.Execute()
	require.Error(t, err)
	// Should NOT contain "failed to infer artifact kind" — kind inference is skipped for remote
	assert.NotContains(t, err.Error(), "failed to infer artifact kind")
}

func TestCommandTag_ReservedLatestAlias(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "latest"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandTag_NumericAlias(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "123"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "purely numeric")
}

func TestCommandTag_InvalidCharAlias(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "bad/alias"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid character")
}

func TestCommandTag_RemoteWithOrigin(t *testing.T) {
	t.Parallel()

	cmd := CommandTag(settings.NewCliParams(), nil, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "stable", "--catalog", "my-registry", "--origin", "ghcr.io/myorg"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--origin not supported for remote tag")
}

func TestCommandTag_AmbiguousLocalCopiesPrintHints(t *testing.T) {
	newSeededLocalCatalog(t)

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ctx, out := newBufferedCatalogTestCtx(t)
	cmd := CommandTag(cliParams, nil, "mycli/catalog")
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"app@1.0.0", "stable"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.True(t, catalogpkg.IsAmbiguous(err))
	assert.Contains(t, out.String(), "mycli catalog tag app@1.0.0 stable --kind solution --origin "+catalogpkg.OriginBuilt)
	assert.Contains(t, out.String(), "mycli catalog tag app@1.0.0 stable --kind solution --origin "+testPulledOrigin)
}

func TestCommandTag_LocalByOrigin(t *testing.T) {
	newSeededLocalCatalog(t)

	for _, origin := range []string{testPulledOrigin, catalogpkg.OriginBuilt} {
		cmd := CommandTag(settings.NewCliParams(), nil, "scafctl/catalog")
		cmd.SetContext(newCatalogTestCtx(t))
		cmd.SetArgs([]string{"app@1.0.0", "stable", "--origin", origin})
		require.NoError(t, cmd.Execute(), "origin %s", origin)
	}

	infos, err := reopenLocalCatalog(t).List(context.Background(), catalogpkg.ArtifactKindSolution, "app")
	require.NoError(t, err)
	var aliasOrigins []string
	for _, info := range infos {
		if info.Tag == "stable" {
			aliasOrigins = append(aliasOrigins, info.Canonical)
		}
	}
	assert.ElementsMatch(t, []string{testPulledOrigin, ""}, aliasOrigins, "each copy gets its own alias")
}

func TestCommandTag_LocalByDigest(t *testing.T) {
	cat := newSeededLocalCatalog(t)
	ctx := context.Background()
	ref := catalogpkg.Reference{Kind: catalogpkg.ArtifactKindSolution, Name: "app", Version: semver.MustParse("1.0.0")}
	pulled := ref
	pulled.Origin = testPulledOrigin
	info, err := cat.Resolve(ctx, pulled)
	require.NoError(t, err)

	cmdCtx, out := newBufferedCatalogTestCtx(t)
	cmd := CommandTag(settings.NewCliParams(), nil, "scafctl/catalog")
	cmd.SetContext(cmdCtx)
	cmd.SetArgs([]string{"app@" + info.Digest, "stable", "--origin", testPulledOrigin})
	require.NoError(t, cmd.Execute(), "a digest hint printed by tag must be runnable")
	assert.Contains(t, out.String(), `Tagged app@1.0.0 as "stable"`)
}

func TestCommandTag_RemoteRejectsDigest(t *testing.T) {
	t.Parallel()

	cmd := CommandTag(settings.NewCliParams(), nil, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"app@sha256:" + strings.Repeat("a", 64), "stable", "--catalog", "my-registry"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest not supported for remote tagging")
}

func BenchmarkCommandTag(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandTag(cliParams, ioStreams, "scafctl/catalog")
	}
}
