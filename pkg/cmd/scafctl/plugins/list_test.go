// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name string
		b    int64
		want string
	}{
		{"zero", 0, "0 B"},
		{"bytes", 512, "512 B"},
		{"kilobytes", 1536, "1.5 KB"},
		{"megabytes", 16792576, "16.0 MB"},
		{"gigabytes", 2147483648, "2.0 GB"},
		{"exact_1MB", 1048576, "1.0 MB"},
		{"exact_1KB", 1024, "1.0 KB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatBytes(tt.b)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPluginListColumnHints_PathHidden(t *testing.T) {
	hint, ok := pluginListColumnHints["path"]
	assert.True(t, ok, "path column hint should exist")
	assert.True(t, hint.Hidden, "path should be hidden in table view")
}

func TestPluginListColumnHints_SizeHidden(t *testing.T) {
	hint, ok := pluginListColumnHints["size"]
	assert.True(t, ok, "size column hint should exist")
	assert.True(t, hint.Hidden, "size (raw bytes) should be hidden in table view")
}

func TestPluginListColumnHints_NameVisible(t *testing.T) {
	hint, ok := pluginListColumnHints["name"]
	assert.True(t, ok, "name column hint should exist")
	assert.False(t, hint.Hidden, "name should be visible")
}

func TestRunList_EmptyCache_EmitsEmptyJSON(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	opts := &ListOptions{
		BinaryName: "scafctl",
		CacheDir:   t.TempDir(),
	}

	err := runList(ctx, opts, outputOpts)
	require.NoError(t, err)
	// The empty-cache message goes to stderr via PlainStderrf;
	// structured output on stdout must include a valid empty JSON array.
	assert.Contains(t, outBuf.String(), "[]")
}

func TestRunList_EmptyCache_DefaultsBinaryName(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	// CacheDir is set to a known-empty temp dir to keep the test hermetic.
	// BinaryName is left empty to exercise the defaulting logic.
	opts := &ListOptions{
		BinaryName: "",
		CacheDir:   t.TempDir(),
	}

	err := runList(ctx, opts, outputOpts)
	require.NoError(t, err)
	// BinaryName should have defaulted to settings.CliBinaryName.
	assert.Equal(t, settings.CliBinaryName, opts.BinaryName)
}

func TestRunList_PopulatedCache_ReturnsItems(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	// Seed the cache directory with a plugin binary.
	cacheDir := t.TempDir()
	platform := runtime.GOOS + "-" + runtime.GOARCH
	pluginDir := filepath.Join(cacheDir, "test-plugin", "1.2.3", platform)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	binName := "test-plugin"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, binName), []byte("binary"), 0o755))

	opts := &ListOptions{
		BinaryName: "scafctl",
		CacheDir:   cacheDir,
	}

	err := runList(ctx, opts, outputOpts)
	require.NoError(t, err)

	out := outBuf.String()
	assert.Contains(t, out, "test-plugin")
	assert.Contains(t, out, "1.2.3")
	assert.Contains(t, out, "sizeHuman")
}

func TestRunList_NoWriterInContext_ReturnsError(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	outputOpts := kvx.NewOutputOptions(ioStreams)
	ctx := t.Context() // no writer

	opts := &ListOptions{
		BinaryName: "scafctl",
		CacheDir:   t.TempDir(),
	}

	err := runList(ctx, opts, outputOpts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writer not initialized")
}

func TestRunList_InvalidCacheDir_ReturnsError(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	outputOpts := kvx.NewOutputOptions(ioStreams)
	outputOpts.Format = "json"

	// Use a file (not directory) as the cache dir to trigger an error from List().
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(tmpFile, []byte("x"), 0o644))

	opts := &ListOptions{
		BinaryName: "scafctl",
		CacheDir:   tmpFile,
	}

	err := runList(ctx, opts, outputOpts)
	require.Error(t, err)
}
