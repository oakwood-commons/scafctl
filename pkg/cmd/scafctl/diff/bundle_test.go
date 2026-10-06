// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package diff

import (
	"bytes"
	"context"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/diffreport"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution/bundler"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportFromBundleDiff(t *testing.T) {
	result := &bundler.DiffResult{
		RefA: "x@1",
		RefB: "x@2",
		Solution: &bundler.SolutionDiff{
			Resolvers: bundler.DiffSets{Added: []string{"ra"}, Modified: []string{"rm"}, Removed: []string{"rr"}},
			Actions:   bundler.DiffSets{Added: []string{"aa"}},
		},
		Files: &bundler.FilesDiff{
			Added:    []bundler.FileDiffEntry{{Path: "f.txt", Size: 1024}},
			Modified: []bundler.FileDiffEntry{{Path: "m.txt"}},
			Removed:  []bundler.FileDiffEntry{{Path: "g.txt"}},
		},
		Vendored: &bundler.VendoredDiff{
			Added:    []bundler.VendoredEntry{{Name: "va", Version: "1.0.0"}},
			Upgraded: []bundler.VendoredUpgrade{{Name: "dep", From: "1.0.0", To: "2.0.0"}},
			Removed:  []bundler.VendoredEntry{{Name: "vr", Version: "0.9.0"}},
		},
		Plugins: &bundler.PluginsDiff{
			Added:    []bundler.PluginDiffEntry{{Name: "pa", VersionTo: "1.0.0"}},
			Modified: []bundler.PluginDiffEntry{{Name: "p", VersionFrom: "1.0.0", VersionTo: "2.0.0"}},
			Removed:  []bundler.PluginDiffEntry{{Name: "pr", VersionFrom: "0.9.0"}},
		},
	}

	r := reportFromBundleDiff(result)

	assert.Equal(t, "bundle", r.Kind)
	byGroup := map[string]int{}
	for _, e := range r.Entries {
		byGroup[e.Group]++
	}
	// resolvers/actions "modified" names are present-in-both, not changes, so
	// only added/removed are reported.
	assert.Equal(t, 2, byGroup["resolvers"])
	assert.Equal(t, 1, byGroup["actions"])
	assert.Equal(t, 3, byGroup["files"])
	assert.Equal(t, 3, byGroup["vendored"])
	assert.Equal(t, 3, byGroup["plugins"])

	// Upgraded vendored dependency carries a from->to detail.
	var found bool
	for _, e := range r.Entries {
		if e.Group == "vendored" && e.Path == "dep" {
			found = true
			assert.Equal(t, diffreport.ChangeModified, e.Kind)
			assert.Equal(t, "1.0.0 -> 2.0.0", e.Detail)
		}
	}
	assert.True(t, found, "expected an upgraded vendored entry")
}

func TestRunBundleDiff_FetchError(t *testing.T) {
	// Cannot use t.Parallel with t.Setenv; isolate the catalog to a temp dir.
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var stdout, stderr bytes.Buffer
	ioStreams := &terminal.IOStreams{Out: &stdout, ErrOut: &stderr}
	cliParams := &settings.Run{NoColor: true}
	ctx := logger.WithLogger(context.Background(), logger.Get(-1))
	ctx = writer.WithWriter(ctx, writer.New(ioStreams, cliParams))

	opts := &BundleDiffOptions{
		RefA:      "nonexistent-solution@1.0.0",
		RefB:      "nonexistent-solution@2.0.0",
		CliParams: cliParams,
		IOStreams: ioStreams,
	}

	err := runBundleDiff(ctx, opts)
	require.Error(t, err, "fetching a nonexistent bundle must fail")
}

func TestCommandDiffBundle(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandDiffBundle(cliParams, ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "bundle <ref-a> <ref-b>", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.True(t, cmd.SilenceUsage)
	assert.NotNil(t, cmd.RunE)
	assert.Contains(t, cmd.Long, "scafctl diff bundle")
}

func TestCommandDiffBundle_Flags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	cmd := CommandDiffBundle(cliParams, ioStreams, "scafctl")

	filesOnlyFlag := cmd.Flags().Lookup("files-only")
	require.NotNil(t, filesOnlyFlag, "files-only flag should exist")
	assert.Equal(t, "false", filesOnlyFlag.DefValue)

	solutionOnlyFlag := cmd.Flags().Lookup("solution-only")
	require.NotNil(t, solutionOnlyFlag, "solution-only flag should exist")
	assert.Equal(t, "false", solutionOnlyFlag.DefValue)

	ignoreFlag := cmd.Flags().Lookup("ignore")
	require.NotNil(t, ignoreFlag, "ignore flag should exist")
	assert.Equal(t, "[]", ignoreFlag.DefValue)

	outputFlag := cmd.Flags().Lookup("output")
	require.NotNil(t, outputFlag, "output flag should exist")

	interactiveFlag := cmd.Flags().Lookup("interactive")
	require.NotNil(t, interactiveFlag, "interactive flag should exist")

	expressionFlag := cmd.Flags().Lookup("expression")
	require.NotNil(t, expressionFlag, "expression flag should exist")
}

func TestCommandDiffBundle_RequiresExactlyTwoArgs(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	// No args should fail
	cmd := CommandDiffBundle(cliParams, ioStreams, "scafctl")
	cmd.SilenceErrors = true
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)

	// One arg should fail
	cmd2 := CommandDiffBundle(cliParams, ioStreams, "scafctl")
	cmd2.SilenceErrors = true
	cmd2.SetArgs([]string{"ref1"})
	err = cmd2.Execute()
	assert.Error(t, err)

	// Three args should fail
	cmd3 := CommandDiffBundle(cliParams, ioStreams, "scafctl")
	cmd3.SilenceErrors = true
	cmd3.SetArgs([]string{"ref1", "ref2", "ref3"})
	err = cmd3.Execute()
	assert.Error(t, err)
}

func BenchmarkCommandDiffBundle(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CommandDiffBundle(cliParams, ioStreams, "scafctl")
	}
}
