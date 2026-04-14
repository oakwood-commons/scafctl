// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"testing"

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
	assert.Equal(t, "tag <name@version> <target>", cmd.Use)
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
		{"force", "false"},
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

func TestCommandTag_RetagToNewName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	// Target is name@version — this is a re-tag operation.
	// Will fail because source doesn't exist in catalog, but validates the re-tag path.
	cmd.SetArgs([]string{"my-solution@1.0.0", "new-name@2.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	// Should NOT contain "alias" errors — it should go through the re-tag path
	assert.NotContains(t, err.Error(), "alias")
}

func TestCommandTag_RetagInvalidTargetName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "INVALID@1.0.0"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid target name")
}

func TestCommandTag_RetagRemoteNotSupported(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTag(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"my-solution@1.0.0", "new-name@1.0.0", "--catalog", "ghcr.io/myorg"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote re-tag not supported")
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
