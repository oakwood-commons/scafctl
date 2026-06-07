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

func seedTestCache(t *testing.T, cacheDir, name, version string) {
	t.Helper()
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	platformKey := goos + "-" + goarch
	dir := filepath.Join(cacheDir, name, version, platformKey)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	binName := name
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, binName), []byte("bin-"+version), 0o755))
}

func TestRunPrune_EmptyCache(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	opts := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  t.TempDir(),
		Keep:      1,
	}

	err := runPrune(ctx, opts, nil, kvxOpts)
	require.NoError(t, err)
}

func TestRunPrune_RemovesOldVersions(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "github", "1.0.0")
	seedTestCache(t, cacheDir, "github", "2.0.0")

	opts := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Keep:      1,
	}

	err := runPrune(ctx, opts, nil, kvxOpts)
	require.NoError(t, err)

	// Should report the removed version in output.
	assert.Contains(t, outBuf.String(), "1.0.0")
}

func TestRunPrune_DryRun(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "exec", "0.1.0")
	seedTestCache(t, cacheDir, "exec", "0.2.0")

	opts := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Keep:      1,
		DryRun:    true,
	}

	err := runPrune(ctx, opts, nil, kvxOpts)
	require.NoError(t, err)

	// Verify file still exists (dry run).
	platform := runtime.GOOS + "-" + runtime.GOARCH
	binName := "exec"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	_, statErr := os.Stat(filepath.Join(cacheDir, "exec", "0.1.0", platform, binName))
	assert.NoError(t, statErr, "file should still exist after dry run")
}

func TestRunPrune_FilterByName(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "github", "1.0.0")
	seedTestCache(t, cacheDir, "github", "2.0.0")
	seedTestCache(t, cacheDir, "exec", "0.1.0")
	seedTestCache(t, cacheDir, "exec", "0.2.0")

	opts := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Keep:      1,
	}

	err := runPrune(ctx, opts, []string{"github"}, kvxOpts)
	require.NoError(t, err)

	assert.Contains(t, outBuf.String(), "github")
	assert.NotContains(t, outBuf.String(), "exec")
}

func TestRunPrune_AllRequiresForce(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)

	opts := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  t.TempDir(),
		All:       true,
		Force:     false,
	}

	err := runPrune(ctx, opts, nil, kvxOpts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all requires --force")
}

func TestRunPrune_AllWithForce(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "github", "1.0.0")
	seedTestCache(t, cacheDir, "exec", "0.5.0")

	opts := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		All:       true,
		Force:     true,
	}

	err := runPrune(ctx, opts, nil, kvxOpts)
	require.NoError(t, err)

	assert.Contains(t, outBuf.String(), "github")
	assert.Contains(t, outBuf.String(), "exec")
}

func TestRunPrune_NoWriter_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	kvxOpts := kvx.NewOutputOptions(nil)

	opts := &PruneOptions{
		CliParams: settings.NewCliParams(),
		CacheDir:  t.TempDir(),
		Keep:      1,
	}

	err := runPrune(ctx, opts, nil, kvxOpts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writer not initialized")
}

func TestRunPrune_KeepMultiple(t *testing.T) {
	t.Parallel()

	ioStreams, outBuf, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "github", "1.0.0")
	seedTestCache(t, cacheDir, "github", "2.0.0")
	seedTestCache(t, cacheDir, "github", "3.0.0")

	opts := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  cacheDir,
		Keep:      2,
	}

	err := runPrune(ctx, opts, nil, kvxOpts)
	require.NoError(t, err)

	// Should only remove 1.0.0.
	assert.Contains(t, outBuf.String(), "1.0.0")
	assert.NotContains(t, outBuf.String(), "2.0.0")
	assert.NotContains(t, outBuf.String(), "3.0.0")
}

func TestCommandPrune_FlagsRegistered(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandPrune(cliParams, ioStreams, "scafctl/plugins")

	assert.NotNil(t, cmd.Flags().Lookup("keep"))
	assert.NotNil(t, cmd.Flags().Lookup("all"))
	assert.NotNil(t, cmd.Flags().Lookup("force"))
	assert.NotNil(t, cmd.Flags().Lookup("dry-run"))
	assert.NotNil(t, cmd.Flags().Lookup("cache-dir"))
}

func TestCommandPrune_ExecuteContext_EmptyCache(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandPrune(cliParams, ioStreams, "scafctl/plugins")
	cmd.SetArgs([]string{"--cache-dir", t.TempDir()})

	err := cmd.ExecuteContext(t.Context())
	require.NoError(t, err)
}

func TestCommandPrune_ExecuteContext_AllNoForce(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()

	cmd := CommandPrune(cliParams, ioStreams, "scafctl/plugins")
	cmd.SetArgs([]string{"--all", "--cache-dir", t.TempDir()})

	err := cmd.ExecuteContext(t.Context())
	require.Error(t, err)
}

func TestRunPrune_DefaultCacheDir(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	w := writer.New(ioStreams, cliParams)
	ctx := writer.WithWriter(t.Context(), w)

	kvxOpts := kvx.NewOutputOptions(ioStreams)
	kvxOpts.Format = "json"

	// Empty CacheDir → uses default. Should not panic.
	opts := &PruneOptions{
		CliParams: cliParams,
		IOStreams: ioStreams,
		CacheDir:  "",
		Keep:      1,
	}

	err := runPrune(ctx, opts, nil, kvxOpts)
	require.NoError(t, err)
}
