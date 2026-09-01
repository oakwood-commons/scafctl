// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package catalog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/adrg/xdg"
	"github.com/go-logr/logr"
	"github.com/oakwood-commons/scafctl/pkg/catalog"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCommandPrune(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "prune", cmd.Use)
	assert.Contains(t, cmd.Aliases, "gc")
	assert.Contains(t, cmd.Aliases, "clean")
	assert.NotEmpty(t, cmd.Short)
	assert.NotNil(t, cmd.RunE)
}

func TestCommandPrune_KvxOutputFlags(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")

	flagTests := []struct {
		name      string
		shorthand string
	}{
		{"output", "o"},
		{"interactive", "i"},
	}

	for _, tt := range flagTests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := cmd.Flags().Lookup(tt.name)
			require.NotNil(t, f, "flag %q should exist", tt.name)
			if tt.shorthand != "" {
				assert.Equal(t, tt.shorthand, f.Shorthand)
			}
		})
	}
}

func TestCommandPrune_EmbedderBinaryName(t *testing.T) {
	t.Parallel()

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "mycli/catalog")

	require.NotNil(t, cmd)
	assert.Equal(t, "prune", cmd.Use)
}

func TestCommandPrune_JSONOutput(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	var output PruneOutput
	err = json.Unmarshal(outBuf.Bytes(), &output)
	require.NoError(t, err, "output should be valid JSON: %s", outBuf.String())
	// Empty catalog returns zeros
	assert.Equal(t, 0, output.RemovedManifests)
	assert.Equal(t, 0, output.RemovedBlobs)
	assert.Equal(t, int64(0), output.ReclaimedBytes)
	assert.NotEmpty(t, output.ReclaimedHuman)
}

func TestCommandPrune_YAMLOutput(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"-o", "yaml"})

	err := cmd.Execute()
	require.NoError(t, err)

	var output PruneOutput
	err = yaml.Unmarshal(outBuf.Bytes(), &output)
	require.NoError(t, err, "output should be valid YAML: %s", outBuf.String())
	assert.Equal(t, 0, output.RemovedManifests)
	assert.Equal(t, 0, output.RemovedBlobs)
}

func TestCommandPrune_TableOutput(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"-o", "table"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Explicit -o table routes through kvx
	out := outBuf.String()
	assert.NotEmpty(t, out)
	// Assert on values and display names (resilient to kvx key rendering changes)
	assert.Contains(t, out, "0")
	assert.Contains(t, out, "0 B")
}

func TestCommandPrune_ListOutput(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"-o", "list"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Explicit -o list routes through kvx
	out := outBuf.String()
	assert.NotEmpty(t, out)
	// Assert on values and display names (resilient to kvx key rendering changes)
	assert.Contains(t, out, "0")
	assert.Contains(t, out, "0 B")
}

func TestCommandPrune_QuietOutput(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"-o", "quiet"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(outBuf.String()))
}

func TestCommandPrune_AutoOutput(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(ctx)

	err := cmd.Execute()
	require.NoError(t, err)

	// Default (no -o flag) uses styled human-readable message
	assert.Contains(t, outBuf.String(), "No orphaned content found")
}

func TestCommandPrune_JSONIncludesReclaimedBytes(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"-o", "json"})

	err := cmd.Execute()
	require.NoError(t, err)

	// reclaimedBytes is present in structured output for scripting
	assert.Contains(t, outBuf.String(), "reclaimedBytes")
	assert.Contains(t, outBuf.String(), "reclaimedHuman")
}

func TestPruneOutput_Fields(t *testing.T) {
	t.Parallel()

	output := PruneOutput{
		RemovedManifests: 3,
		RemovedBlobs:     5,
		ReclaimedBytes:   1048576,
		ReclaimedHuman:   "1.0 MB",
	}

	assert.Equal(t, 3, output.RemovedManifests)
	assert.Equal(t, 5, output.RemovedBlobs)
	assert.Equal(t, int64(1048576), output.ReclaimedBytes)
	assert.Equal(t, "1.0 MB", output.ReclaimedHuman)
}

func TestPruneColumnHints(t *testing.T) {
	t.Parallel()

	// Verify column hints cover all expected fields
	assert.Contains(t, pruneColumnHints, "removedManifests")
	assert.Contains(t, pruneColumnHints, "removedBlobs")
	assert.Contains(t, pruneColumnHints, "reclaimedBytes")
	assert.Contains(t, pruneColumnHints, "reclaimedHuman")

	// Display names are human-friendly
	assert.Equal(t, "removed manifests", pruneColumnHints["removedManifests"].DisplayName)
	assert.Equal(t, "removed blobs", pruneColumnHints["removedBlobs"].DisplayName)
	assert.Equal(t, "reclaimed", pruneColumnHints["reclaimedHuman"].DisplayName)
	assert.Equal(t, "reclaimed bytes", pruneColumnHints["reclaimedBytes"].DisplayName)
}

func TestPruneColumnOrder(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"removedManifests", "removedBlobs", "reclaimedHuman", "reclaimedBytes"}, pruneColumnOrder)
}

func TestCommandPrune_DefaultOutputWithRemovedContent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	catalogPath := filepath.Join(tmpDir, "scafctl", "catalog")
	cat, err := catalog.NewLocalCatalogAt(catalogPath, logr.Discard())
	require.NoError(t, err)

	ref := catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    "test-sol",
		Version: semver.MustParse("1.0.0"),
	}
	_, err = cat.Store(context.Background(), ref, []byte("content"), nil, nil, false)
	require.NoError(t, err)

	// Plant an orphaned blob so prune has something to remove.
	orphanPath := filepath.Join(catalogPath, "blobs", "sha256",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	require.NoError(t, os.WriteFile(orphanPath, []byte("orphan"), 0o600))

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(ctx)

	err = cmd.Execute()
	require.NoError(t, err)

	out := outBuf.String()
	assert.Contains(t, out, "Pruned catalog")
	assert.Contains(t, out, "Removed blobs: 1")
	assert.Contains(t, out, "Reclaimed:")
}

func TestCommandPrune_DefaultOutputWithRemovedManifests(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", tmpDir)
	xdg.Reload()
	t.Cleanup(xdg.Reload)

	catalogPath := filepath.Join(tmpDir, "scafctl", "catalog")
	cat, err := catalog.NewLocalCatalogAt(catalogPath, logr.Discard())
	require.NoError(t, err)

	// Store two artifacts, then delete one's tag to create an orphaned manifest.
	for _, name := range []string{"keep", "orphan"} {
		ref := catalog.Reference{
			Kind:    catalog.ArtifactKindSolution,
			Name:    name,
			Version: semver.MustParse("1.0.0"),
		}
		_, err = cat.Store(context.Background(), ref, []byte("content-"+name), nil, nil, false)
		require.NoError(t, err)
	}

	err = cat.Delete(context.Background(), catalog.Reference{
		Kind:    catalog.ArtifactKindSolution,
		Name:    "orphan",
		Version: semver.MustParse("1.0.0"),
	})
	require.NoError(t, err)

	cliParams := settings.NewCliParams()
	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(context.Background(), w)
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(ctx)

	err = cmd.Execute()
	require.NoError(t, err)

	out := outBuf.String()
	assert.Contains(t, out, "Pruned catalog")
	assert.Contains(t, out, "Removed manifests:")
	assert.Contains(t, out, "Reclaimed:")
}

func TestCommandPrune_InteractiveRequiresTTY(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	cmd.SetContext(newCatalogTestCtx(t))
	cmd.SetArgs([]string{"-i"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "interactive mode requires a terminal")
}

func BenchmarkCommandPrune(b *testing.B) {
	cliParams := settings.NewCliParams()
	ioStreams, _, _ := terminal.NewTestIOStreams()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		CommandPrune(cliParams, ioStreams, "scafctl/catalog")
	}
}
