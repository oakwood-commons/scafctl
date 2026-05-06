// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

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
