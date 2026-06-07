// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"encoding/json"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunUpdate_NoArgsNoAll_ReturnsError(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)

	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  t.TempDir(),
		Target:    "latest",
	}

	err := runUpdate(ctx, opts, nil, kvxOpts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no plugin names specified")
}

func TestRunUpdate_InvalidTarget(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)

	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  t.TempDir(),
		Target:    "invalid",
		All:       true,
	}

	err := runUpdate(ctx, opts, nil, kvxOpts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid --target")
}

func TestRunUpdate_EmptyCache_AllUpToDate(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  t.TempDir(),
		Target:    "latest",
		All:       true,
	}

	// With empty cache + --all, no plugins to update.
	err := runUpdate(ctx, opts, nil, kvxOpts)
	require.NoError(t, err)
}

func TestRunUpdate_PluginNotInCache(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  t.TempDir(),
		Target:    "latest",
	}

	// Plugin not in cache — should return an error suggesting 'plugins install'.
	err := runUpdate(ctx, opts, []string{"nonexistent"}, kvxOpts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in cache")
}

func TestRunUpdate_DryRun_AllUpToDate(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	// Empty cache → --all reports all up to date.
	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Target:    "latest",
		All:       true,
		DryRun:    true,
	}

	err := runUpdate(ctx, opts, nil, kvxOpts)
	require.NoError(t, err)

	// Should emit empty JSON array.
	var items []updateResultItem
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
	assert.Empty(t, items)
}

func TestRunUpdate_PinnedVersion_NotInCache(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, stderrBuf := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Target:    "latest",
		All:       true,
	}

	// Pass pinned version for something not in cache — should warn on stderr.
	err := runUpdate(ctx, opts, []string{"ghost@1.0.0"}, kvxOpts)
	// With --all mode and pinned name not in cache, PlanUpdates succeeds
	// (all path is empty), then pinned version lookup fails → warning + empty result.
	require.NoError(t, err)
	assert.Contains(t, stderrBuf.String(), "not found in cache")

	var items []updateResultItem
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
	assert.Empty(t, items)
}

func TestRunUpdate_PinnedVersion_AlreadyCurrent(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "exec", "1.0.0")

	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Target:    "latest",
		All:       true,
	}

	// Pin to the same version that's already cached → nothing to do.
	err := runUpdate(ctx, opts, []string{"exec@1.0.0"}, kvxOpts)
	require.NoError(t, err)

	var items []updateResultItem
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
	assert.Empty(t, items)
}

func TestRunUpdate_PinnedVersion_DifferentVersion_DryRun(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, stderrBuf := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "exec", "1.0.0")

	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Target:    "latest",
		All:       true,
		DryRun:    true,
	}

	// Pin to different version → should show as pending update in dry-run.
	err := runUpdate(ctx, opts, []string{"exec@2.0.0"}, kvxOpts)
	require.NoError(t, err)

	assert.Contains(t, stderrBuf.String(), "Dry run")

	var items []updateResultItem
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
	require.Len(t, items, 1)
	assert.Equal(t, "exec", items[0].Name)
	assert.Equal(t, "1.0.0", items[0].OldVersion)
	assert.Equal(t, "2.0.0", items[0].NewVersion)
	assert.Equal(t, "pending", items[0].Status)
}

func TestRunUpdate_NoWriter_ReturnsError(t *testing.T) {
	t.Parallel()

	// Context without a writer.
	ctx := t.Context()
	kvxOpts := kvx.NewOutputOptions(nil)

	opts := &UpdateOptions{
		CliParams: settings.NewCliParams(),
		CacheDir:  t.TempDir(),
		Target:    "latest",
	}

	err := runUpdate(ctx, opts, []string{"foo"}, kvxOpts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writer not initialized")
}

func TestRunUpdate_DryRun_WithCachedPlugin(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, stderrBuf := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "github", "1.0.0")

	opts := &UpdateOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Target:    "latest",
		DryRun:    true,
	}

	// Plugin in cache but no catalog available → PlanUpdates will fail
	// catalog resolution (nil fetcher) after getting version from cache.
	// This actually triggers the catalog chain build which needs config.
	// Instead, test with --all which doesn't need catalog if we seed + pin.
	opts.All = true
	// Pinned version (different) → bypasses catalog.
	err := runUpdate(ctx, opts, []string{"github@2.0.0"}, kvxOpts)
	require.NoError(t, err)
	assert.Contains(t, stderrBuf.String(), "Dry run")

	var items []updateResultItem
	require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
	require.Len(t, items, 1)
	assert.Equal(t, "github", items[0].Name)
}

func TestCommandUpdate_FlagsRegistered(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/plugins")

	assert.NotNil(t, cmd.Flags().Lookup("all"))
	assert.NotNil(t, cmd.Flags().Lookup("target"))
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("platform"))
	assert.NotNil(t, cmd.Flags().Lookup("no-cache"))
	assert.NotNil(t, cmd.Flags().Lookup("cache-dir"))
}

func TestCommandUpdate_ExecuteContext_AllDryRun(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/plugins")
	cmd.SetArgs([]string{"--all", "--dry-run", "--cache-dir", t.TempDir()})

	err := cmd.ExecuteContext(t.Context())
	require.NoError(t, err)
}

func TestCommandUpdate_ExecuteContext_NoArgs(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandUpdate(cliParams, ioStreams, "scafctl/plugins")
	cmd.SetArgs([]string{})

	err := cmd.ExecuteContext(t.Context())
	require.Error(t, err)
}
