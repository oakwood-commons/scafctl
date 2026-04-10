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

func TestCommandTags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "tags <registry/repository[/kind]/name>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandTags_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name     string
		defValue string
	}{
		{"kind", ""},
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

func TestCommandTags_RequiresExactlyOneArg(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts 1 arg(s)")
}

func TestCommandTags_InvalidReference(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{":::invalid"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandTags_InvalidKind(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandTags(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"ghcr.io/myorg/my-solution", "--kind", "not-a-valid-kind"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid kind")
}

func TestTagsListItem_Fields(t *testing.T) {
	t.Parallel()

	item := TagsListItem{
		Tag:      "1.0.0",
		IsSemver: true,
		Version:  "1.0.0",
	}

	assert.Equal(t, "1.0.0", item.Tag)
	assert.True(t, item.IsSemver)
	assert.Equal(t, "1.0.0", item.Version)
}

func TestTagsListItem_AliasTag(t *testing.T) {
	t.Parallel()

	item := TagsListItem{
		Tag:      "stable",
		IsSemver: false,
	}

	assert.Equal(t, "stable", item.Tag)
	assert.False(t, item.IsSemver)
	assert.Empty(t, item.Version)
}

func BenchmarkCommandTags(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandTags(cliParams, ioStreams, "scafctl/catalog")
	}
}
