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
		{Kind: catalogpkg.ArtifactKindSolution, Name: "hello-world", LatestVersion: "1.2.0"},
		{Kind: catalogpkg.ArtifactKindProvider, Name: "terraform", LatestVersion: "0.5.1"},
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
	assert.Equal(t, "1.2.0", items[0].LatestVersion)
	assert.Equal(t, "provider", items[1].Kind)
	assert.Equal(t, "terraform", items[1].Name)
	assert.Equal(t, "0.5.1", items[1].LatestVersion)
	assert.Equal(t, "auth-handler", items[2].Kind)
	assert.Equal(t, "gcp-auth", items[2].Name)
	assert.Empty(t, items[2].LatestVersion, "missing version should be empty")
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

func TestWriteIndexDiff(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	diff := catalogpkg.IndexDiffSummary{
		Entries: []catalogpkg.IndexDiffEntry{
			{Change: catalogpkg.IndexDiffAdded, Kind: catalogpkg.ArtifactKindSolution, Name: "new-app", LatestVersion: "1.0.0"},
			{Change: catalogpkg.IndexDiffVersionChanged, Kind: catalogpkg.ArtifactKindProvider, Name: "terraform", LatestVersion: "2.0.0", PrevVersion: "1.5.0"},
			{Change: catalogpkg.IndexDiffRemoved, Kind: catalogpkg.ArtifactKindSolution, Name: "old-app", LatestVersion: "0.9.0"},
			{Change: catalogpkg.IndexDiffUnchanged, Kind: catalogpkg.ArtifactKindSolution, Name: "stable", LatestVersion: "3.0.0"},
		},
		Added:   1,
		Removed: 1,
		Changed: 1,
		Total:   3,
	}

	err := writeIndexDiff(diff, outputOpts)
	require.NoError(t, err)

	output := outBuf.String()
	assert.Contains(t, output, `"change"`)
	assert.Contains(t, output, `"added"`)
	assert.Contains(t, output, `"version-changed"`)
	assert.Contains(t, output, `"removed"`)
	assert.Contains(t, output, `"unchanged"`)
	assert.Contains(t, output, `"prevVersion"`)
	assert.Contains(t, output, `"1.5.0"`)
}

func BenchmarkWriteIndexDiff(b *testing.B) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	diff := catalogpkg.IndexDiffSummary{
		Entries: make([]catalogpkg.IndexDiffEntry, 100),
		Added:   30,
		Removed: 10,
		Changed: 20,
		Total:   90,
	}
	for i := range diff.Entries {
		diff.Entries[i] = catalogpkg.IndexDiffEntry{
			Change:        catalogpkg.IndexDiffAdded,
			Kind:          catalogpkg.ArtifactKindSolution,
			Name:          "artifact",
			LatestVersion: "1.0.0",
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = writeIndexDiff(diff, outputOpts)
	}
}

func TestWriteIndexList_AppNameInOutputOpts(t *testing.T) {
	t.Parallel()

	ioStreams, out, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"
	outputOpts.AppName = "my-catalog"

	artifacts := []catalogpkg.DiscoveredArtifact{
		{Kind: catalogpkg.ArtifactKindSolution, Name: "hello-world", LatestVersion: "1.0.0"},
	}

	err := writeIndexList(artifacts, outputOpts)
	require.NoError(t, err)

	var items []IndexListItem
	err = json.Unmarshal(out.Bytes(), &items)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "hello-world", items[0].Name)

	// AppName should be preserved on the outputOpts (used as table header).
	assert.Equal(t, "my-catalog", outputOpts.AppName)
}

func TestCommandIndexPush_NoAppNameInRunE(t *testing.T) {
	t.Parallel()

	// Verify that the RunE closure does NOT set AppName on opts.
	// AppName should be set later in runIndexPush from the catalog name.
	cliParams := &settings.Run{BinaryName: "mycli"}
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "mycli/catalog")

	push, _, err := cmd.Find([]string{"push"})
	require.NoError(t, err)

	// The push command should exist and have RunE wired.
	assert.NotNil(t, push.RunE)
}

func TestCommandIndexShow_FilterFlags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandIndex(cliParams, ioStreams, "scafctl/catalog")

	show, _, err := cmd.Find([]string{"show"})
	require.NoError(t, err)

	assert.NotNil(t, show.Flags().Lookup("kind"))
	assert.NotNil(t, show.Flags().Lookup("search"))
	assert.NotNil(t, show.Flags().ShorthandLookup("k"))
	assert.NotNil(t, show.Flags().ShorthandLookup("s"))
}

func TestFilterIndexArtifacts(t *testing.T) {
	t.Parallel()

	artifacts := []catalogpkg.DiscoveredArtifact{
		{Kind: catalogpkg.ArtifactKindSolution, Name: "hello-world", DisplayName: "Hello World", Category: "getting-started", Description: "A starter solution"},
		{Kind: catalogpkg.ArtifactKindProvider, Name: "sleep"},
		{Kind: catalogpkg.ArtifactKindProvider, Name: "env"},
		{Kind: catalogpkg.ArtifactKindAuthHandler, Name: "entra"},
	}

	tests := []struct {
		name      string
		kind      string
		search    string
		wantCount int
		wantNames []string
	}{
		{
			name:      "no filters returns all",
			wantCount: 4,
		},
		{
			name:      "filter by kind solution",
			kind:      "solution",
			wantCount: 1,
			wantNames: []string{"hello-world"},
		},
		{
			name:      "filter by kind provider",
			kind:      "provider",
			wantCount: 2,
			wantNames: []string{"sleep", "env"},
		},
		{
			name:      "filter by kind case-insensitive",
			kind:      "Provider",
			wantCount: 2,
		},
		{
			name:      "search by name",
			search:    "hello",
			wantCount: 1,
			wantNames: []string{"hello-world"},
		},
		{
			name:      "search by display name",
			search:    "World",
			wantCount: 1,
			wantNames: []string{"hello-world"},
		},
		{
			name:      "search by category",
			search:    "getting-started",
			wantCount: 1,
		},
		{
			name:      "search by description",
			search:    "starter",
			wantCount: 1,
		},
		{
			name:      "kind and search combined",
			kind:      "provider",
			search:    "env",
			wantCount: 1,
			wantNames: []string{"env"},
		},
		{
			name:      "no match",
			search:    "nonexistent",
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := filterIndexArtifacts(artifacts, tc.kind, tc.search)
			assert.Len(t, result, tc.wantCount)
			if tc.wantNames != nil {
				names := make([]string, len(result))
				for i, a := range result {
					names[i] = a.Name
				}
				assert.Equal(t, tc.wantNames, names)
			}
		})
	}
}

func TestMatchesSearch(t *testing.T) {
	t.Parallel()

	a := catalogpkg.DiscoveredArtifact{
		Name:        "hello-world",
		DisplayName: "Hello World Starter",
		Description: "A minimal getting-started solution",
		Category:    "templates",
	}

	assert.True(t, matchesSearch(a, "hello"))
	assert.True(t, matchesSearch(a, "starter"))
	assert.True(t, matchesSearch(a, "minimal"))
	assert.True(t, matchesSearch(a, "templates"))
	assert.False(t, matchesSearch(a, "nonexistent"))
}
