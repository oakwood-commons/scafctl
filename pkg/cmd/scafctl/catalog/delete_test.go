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

func TestCommandDelete(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "delete <reference>", cmd.Use)
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
		{"all"},
		{"force"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			assert.NotNil(t, f, "flag %q should exist", tt.name)
		})
	}
}

func TestCommandDelete_PersistentFlags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")

	for _, name := range []string{"dry-run", "insecure"} {
		flag := cmd.PersistentFlags().Lookup(name)
		require.NotNil(t, flag, "persistent flag %q should exist", name)
	}
}

// The root command is remote-only (like pull), so --catalog and --kind live on
// the kind subcommands; --force only guards --all and is not on them.
func TestCommandDelete_RootAndKindFlagSplit(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")

	assert.Nil(t, cmd.Flags().Lookup("catalog"))
	assert.Nil(t, cmd.Flags().Lookup("kind"))
	assert.NotNil(t, cmd.PersistentFlags().Lookup("dry-run"))
	assert.NotNil(t, cmd.PersistentFlags().Lookup("insecure"))

	sub, _, err := cmd.Find([]string{"solution"})
	require.NoError(t, err)
	assert.NotNil(t, sub.Flags().Lookup("catalog"))
	assert.NotNil(t, sub.Flags().Lookup("origin"))
	assert.NotNil(t, sub.InheritedFlags().Lookup("dry-run"))
	assert.NotNil(t, sub.InheritedFlags().Lookup("insecure"))
	assert.Nil(t, sub.Flags().Lookup("force"))
	assert.Nil(t, sub.Flags().Lookup("all"))
	c := sub.Flags().ShorthandLookup("c")
	require.NotNil(t, c, "shorthand -c should exist on kind commands")
	assert.Equal(t, "catalog", c.Name)
}

func TestCommandDelete_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	ctx, out := newCapturingCtx(t)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires exactly 1 argument")
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	// The entry point does not print errors carrying an exit code.
	assert.Contains(t, out.String(), "requires exactly 1 argument")
}

func TestCommandDelete_ShortReferenceRejected(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRemoteReferenceRequired)
	assert.Contains(t, err.Error(), cliParams.BinaryName+" catalog delete <kind> <name@version>")
}

func TestCommandDelete_RemoteReferenceVersionRequired(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"ghcr.io/myorg/scafctl/solutions/my-solution"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version required")
}

func TestCommandDelete_RemoteReferenceDryRun(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0", "--dry-run"})

	require.NoError(t, cmd.Execute())
}

func TestValidateDeleteRoot_CustomBinaryName(t *testing.T) {
	t.Parallel()

	custom := settings.NewCliParams()
	custom.BinaryName = "mycli"

	err := validateDeleteRoot(&DeleteOptions{Reference: "my-solution@1.0.0", CliParams: custom})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mycli catalog delete <kind> <name@version>")
	assert.NotContains(t, err.Error(), "scafctl catalog delete")
}

func TestBuildDeleteRemoteReference(t *testing.T) {
	t.Parallel()

	build, err := buildDeleteRemoteReference(newCatalogTestCtx(t), &DeleteOptions{
		Reference: "ghcr.io/myorg/scafctl/solutions/my-solution@1.0.0",
	})

	require.NoError(t, err)
	assert.Equal(t, "my-solution", build.Reference.Name)
	assert.Equal(t, "ghcr.io", build.Registry)
	assert.NotEmpty(t, build.Repository)
	assert.Equal(t, catalog.SourceAny, build.Source.Kind)
}

func TestBuildDeleteRemoteReference_Invalid(t *testing.T) {
	t.Parallel()

	_, err := buildDeleteRemoteReference(newCatalogTestCtx(t), &DeleteOptions{Reference: "not a reference"})

	require.Error(t, err)
}

func TestCommandDelete_ValidationFailureExitCode(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandDelete(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0"})

	err := cmd.Execute()

	require.Error(t, err)
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
}

func TestCommandDelete_AllFlag(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv
	t.Setenv("XDG_DATA_HOME", t.TempDir())

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
	ctx, out := newCapturingCtx(t)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"--all", "my-solution@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all cannot be used with positional arguments")
	assert.Equal(t, exitcode.InvalidInput, exitcode.GetCode(err))
	assert.Contains(t, out.String(), "--all cannot be used with positional arguments")
}

func TestRunDeleteAll_EmptyCatalog(t *testing.T) {
	// Set XDG_DATA_HOME to a temp dir so NewLocalCatalog creates an empty catalog
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := newCatalogTestCtx(t)

	err := runDeleteAll(ctx, &DeleteOptions{Force: true, CliParams: settings.NewCliParams()})
	require.NoError(t, err)
}

func TestRunDeleteAll_DryRun(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ctx := newCatalogTestCtx(t)

	err := runDeleteAll(ctx, &DeleteOptions{DryRun: true, Force: true, CliParams: settings.NewCliParams()})
	require.NoError(t, err)
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
