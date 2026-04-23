// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"encoding/json"
	"testing"

	catalogpkg "github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandIndex(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "scafctl/catalog")

	assert.Equal(t, "index", cmd.Use)
	assert.True(t, cmd.HasSubCommands(), "index command should have subcommands")

	subNames := make([]string, 0, len(cmd.Commands()))
	for _, sub := range cmd.Commands() {
		subNames = append(subNames, sub.Name())
	}
	assert.Contains(t, subNames, "push")
	assert.Contains(t, subNames, "show")
}

func TestCommandIndex_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := &settings.Run{BinaryName: "mycli"}
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "mycli/catalog")

	push := cmd.Commands()[0]
	assert.Contains(t, push.Long, "mycli")
}

func TestWriteIndexList_AllKinds(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	artifacts := []catalogpkg.DiscoveredArtifact{
		{Kind: catalogpkg.ArtifactKindSolution, Name: "hello-world"},
		{Kind: catalogpkg.ArtifactKindProvider, Name: "terraform"},
		{Kind: catalogpkg.ArtifactKindAuthHandler, Name: "gcp-auth"},
	}

	err := writeIndexList(artifacts, outputOpts)
	require.NoError(t, err)

	var items []IndexListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 3)

	assert.Equal(t, "solution", items[0].Kind)
	assert.Equal(t, "hello-world", items[0].Name)
	assert.Equal(t, "provider", items[1].Kind)
	assert.Equal(t, "terraform", items[1].Name)
	assert.Equal(t, "auth-handler", items[2].Kind)
	assert.Equal(t, "gcp-auth", items[2].Name)
}

func TestWriteIndexList_Empty(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	err := writeIndexList([]catalogpkg.DiscoveredArtifact{}, outputOpts)
	require.NoError(t, err)

	var items []IndexListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	assert.Empty(t, items)
}

func TestCommandIndexPush_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "scafctl/catalog")

	push, _, err := cmd.Find([]string{"push"})
	require.NoError(t, err)

	assert.NotNil(t, push.Flags().Lookup("catalog"))
	assert.NotNil(t, push.Flags().Lookup("insecure"))
	assert.NotNil(t, push.Flags().Lookup("dry-run"))
	assert.NotNil(t, push.Flags().Lookup("output"))
}

func TestCommandIndexShow_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "scafctl/catalog")

	show, _, err := cmd.Find([]string{"show"})
	require.NoError(t, err)

	assert.NotNil(t, show.Flags().Lookup("catalog"))
	assert.NotNil(t, show.Flags().Lookup("insecure"))
	assert.NotNil(t, show.Flags().Lookup("output"))
	assert.Nil(t, show.Flags().Lookup("dry-run"), "show should not have --dry-run")
}

func TestRunIndexPush_NoCatalogConfig(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"push", "--catalog", "nonexistent-registry"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestRunIndexShow_NoCatalogConfig(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"show", "--catalog", "nonexistent-registry"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestRunIndexPush_DryRunFlag(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"push", "--catalog", "nonexistent-registry", "--dry-run"})

	err := cmd.Execute()
	require.Error(t, err)
}

func BenchmarkWriteIndexList(b *testing.B) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	artifacts := make([]catalogpkg.DiscoveredArtifact, 100)
	for i := range artifacts {
		kind := catalogpkg.ArtifactKindSolution
		switch i % 3 {
		case 1:
			kind = catalogpkg.ArtifactKindProvider
		case 2:
			kind = catalogpkg.ArtifactKindAuthHandler
		}
		artifacts[i] = catalogpkg.DiscoveredArtifact{Kind: kind, Name: "artifact"}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = writeIndexList(artifacts, outputOpts)
	}
}
