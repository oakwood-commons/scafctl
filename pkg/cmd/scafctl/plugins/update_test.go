// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugins

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/exitcode"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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

	// Pass pinned version for something not in cache -- warns on stderr, emits empty JSON, and exits nonzero.
	err := runUpdate(ctx, opts, []string{"ghost@1.0.0"}, kvxOpts)
	require.Error(t, err)
	assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
	assert.Contains(t, stderrBuf.String(), "not found in cache")
	assert.NotContains(t, stderrBuf.String(), "All plugins are up to date.")

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

func TestRunUpdate_PinnedVersion_NoCatalog_ReturnsError(t *testing.T) {
	t.Parallel()

	ioStreams, _, _ := terminal.NewTestIOStreams()
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

	// Pinned to different version with no catalog → should error (not panic).
	err := runUpdate(ctx, opts, []string{"exec@2.0.0"}, kvxOpts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no remote catalogs available")
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

func TestCommandUpdate_AllUpToDate_OutputFormats(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		extraArgs      []string
		wantStdout     string
		wantStderrSub  string
		wantEmptyJSON  bool
		wantEmptyYAML  bool
		wantQuietEmpty bool
	}{
		{
			name:          "default auto omits empty kvx table",
			extraArgs:     nil,
			wantStdout:    "",
			wantStderrSub: "All plugins are up to date.",
		},
		{
			name:          "table format omits empty kvx table",
			extraArgs:     []string{"-o", "table"},
			wantStdout:    "",
			wantStderrSub: "All plugins are up to date.",
		},
		{
			name:          "list format omits empty kvx table",
			extraArgs:     []string{"-o", "list"},
			wantStdout:    "",
			wantStderrSub: "All plugins are up to date.",
		},
		{
			name:          "tree format omits empty kvx table",
			extraArgs:     []string{"-o", "tree"},
			wantStdout:    "",
			wantStderrSub: "All plugins are up to date.",
		},
		{
			name:          "text format omits empty kvx table",
			extraArgs:     []string{"-o", "text"},
			wantStdout:    "",
			wantStderrSub: "All plugins are up to date.",
		},
		{
			name:           "quiet format suppresses stdout and stderr",
			extraArgs:      []string{"-o", "quiet"},
			wantQuietEmpty: true,
		},
		{
			name:          "json format emits empty array on stdout",
			extraArgs:     []string{"-o", "json"},
			wantStderrSub: "All plugins are up to date.",
			wantEmptyJSON: true,
		},
		{
			name:          "yaml format emits empty array on stdout",
			extraArgs:     []string{"-o", "yaml"},
			wantStderrSub: "All plugins are up to date.",
			wantEmptyYAML: true,
		},
		{
			name:          "expression evaluates against empty result slice",
			extraArgs:     []string{"-e", "size(_)"},
			wantStdout:    "0",
			wantStderrSub: "All plugins are up to date.",
		},
		{
			name:          "valid where filter in human mode omits empty kvx table",
			extraArgs:     []string{"-w", "_.name == 'exec'"},
			wantStdout:    "",
			wantStderrSub: "All plugins are up to date.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cacheDir := t.TempDir()

			ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
			cliParams := settings.NewCliParams()

			cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
			args := append([]string{"--all", "--cache-dir", cacheDir}, tt.extraArgs...)
			cmd.SetArgs(args)

			err := cmd.ExecuteContext(t.Context())
			require.NoError(t, err)

			stdout := outBuf.String()
			stderr := errBuf.String()

			if tt.wantQuietEmpty {
				assert.Empty(t, stdout)
				assert.Empty(t, stderr)
				return
			}

			assert.Contains(t, stderr, tt.wantStderrSub)

			switch {
			case tt.wantEmptyJSON:
				assert.Equal(t, "[]", strings.TrimSpace(stdout))
				var items []updateResultItem
				require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
				assert.Empty(t, items)
			case tt.wantEmptyYAML:
				assert.Equal(t, "[]", strings.TrimSpace(stdout))
				var items []updateResultItem
				require.NoError(t, yaml.Unmarshal(outBuf.Bytes(), &items))
				assert.Empty(t, items)
			case tt.wantStdout != "":
				assert.Equal(t, tt.wantStdout, strings.TrimSpace(stdout))
			default:
				assert.Empty(t, stdout)
				assert.NotContains(t, stdout, "(value)")
				assert.NotContains(t, stdout, "[]")
			}
		})
	}
}

func TestCommandUpdate_EmbedderBinaryName_AllUpToDate(t *testing.T) {
	t.Parallel()

	t.Run("custom binary name", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		cliParams.BinaryName = "mycli"

		cmd := CommandUpdate(cliParams, ioStreams, "mycli")
		assert.Contains(t, cmd.Long, "mycli plugins update --all")
		assert.NotContains(t, cmd.Long, "scafctl plugins update")

		cmd.SetArgs([]string{"--all", "--cache-dir", t.TempDir()})
		require.NoError(t, cmd.ExecuteContext(t.Context()))
		assert.Empty(t, outBuf.String())
		assert.Contains(t, errBuf.String(), "All plugins are up to date.")
	})

	t.Run("empty binary name falls back to default", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := &settings.Run{}

		cmd := CommandUpdate(cliParams, ioStreams, settings.CliBinaryName)
		assert.Contains(t, cmd.Long, settings.CliBinaryName+" plugins update --all")

		cmd.SetArgs([]string{"--all", "--cache-dir", t.TempDir(), "-o", "json"})
		require.NoError(t, cmd.ExecuteContext(t.Context()))
		assert.Equal(t, "[]", strings.TrimSpace(outBuf.String()))
		assert.Contains(t, errBuf.String(), "All plugins are up to date.")
	})
}

func TestRunUpdate_PinnedVersion_AlreadyCurrent_DefaultTableOmitsEmptyBox(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	seedTestCache(t, cacheDir, "exec", "1.0.0")

	t.Run("default table omits empty kvx box", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", cacheDir, "exec@1.0.0"})
		require.NoError(t, cmd.ExecuteContext(t.Context()))

		assert.Empty(t, outBuf.String())
		assert.NotContains(t, outBuf.String(), "(value)")
		assert.NotContains(t, outBuf.String(), "[]")
		assert.Contains(t, errBuf.String(), "All plugins are up to date.")
	})

	t.Run("quiet suppresses status when pinned plugin is current", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", cacheDir, "exec@1.0.0", "-o", "quiet"})
		require.NoError(t, cmd.ExecuteContext(t.Context()))

		assert.Empty(t, outBuf.String())
		assert.Empty(t, errBuf.String())
	})

	t.Run("quiet suppresses warnings while returning CatalogError on unresolved plugin", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", cacheDir, "ghost@1.0.0", "-o", "quiet"})
		err := cmd.ExecuteContext(t.Context())
		require.Error(t, err)
		assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
		assert.Empty(t, outBuf.String())
		assert.Empty(t, errBuf.String())
	})

	t.Run("unsupported test output format fails even when no updates are found", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", cacheDir, "exec@1.0.0", "-o", "test"})
		err := cmd.ExecuteContext(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not supported by this command")
		assert.Empty(t, outBuf.String())
		assert.NotContains(t, errBuf.String(), "All plugins are up to date.")
	})

	t.Run("malformed where expression fails in human mode even when no updates are found", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", cacheDir, "exec@1.0.0", "-w", "invalid((("})
		err := cmd.ExecuteContext(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "where filter failed")
		assert.Empty(t, outBuf.String())
		assert.NotContains(t, errBuf.String(), "All plugins are up to date.")
	})

	t.Run("interactive flag routes through kvx terminal check", func(t *testing.T) {
		t.Parallel()

		ioStreams, _, _ := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", cacheDir, "exec@1.0.0", "-i"})
		err := cmd.ExecuteContext(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "interactive mode requires a terminal")
	})

	t.Run("global quiet with json output suppresses stderr while emitting empty array", func(t *testing.T) {
		t.Parallel()

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		cliParams.IsQuiet = true

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", cacheDir, "exec@1.0.0", "-o", "json"})
		require.NoError(t, cmd.ExecuteContext(t.Context()))

		assert.Equal(t, "[]", strings.TrimSpace(outBuf.String()))
		assert.Empty(t, errBuf.String())
	})

	t.Run("quiet format suppresses catalog fallback warning and returns CatalogError when all catalogs fail", func(t *testing.T) {
		t.Parallel()

		unreachSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		t.Cleanup(unreachSrv.Close)
		host := strings.TrimPrefix(unreachSrv.URL, "http://")

		appCfg := &config.Config{
			Settings: config.Settings{DisableOfficialCatalog: true},
			Catalogs: []config.CatalogConfig{
				{Name: "primary-down", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/one", Insecure: true},
				{Name: "secondary-down", Type: config.CatalogTypeOCI, URL: "oci://" + host + "/two", Insecure: true},
			},
		}

		seededDir := t.TempDir()
		seedTestCache(t, seededDir, "exec", "1.0.0")
		seedTestCache(t, seededDir, "github", "1.0.0")

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		ctx := config.WithConfig(t.Context(), appCfg)

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", seededDir, "-o", "quiet"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
		assert.Contains(t, err.Error(), "failed to resolve 2 plugins")
		assert.Empty(t, outBuf.String())
		assert.Empty(t, errBuf.String())
	})

	t.Run("pinned plugin supersedes its catalog failure while unpinned failure still returns CatalogError", func(t *testing.T) {
		t.Parallel()

		seededDir := t.TempDir()
		seedTestCache(t, seededDir, "exec", "1.0.0")
		seedTestCache(t, seededDir, "github", "1.0.0")

		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--cache-dir", seededDir, "exec@1.0.0"})
		err := cmd.ExecuteContext(t.Context())
		require.Error(t, err)
		assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))
		assert.Empty(t, outBuf.String())
		assert.NotContains(t, errBuf.String(), "exec:")
		assert.Contains(t, errBuf.String(), "github:")
		assert.NotContains(t, errBuf.String(), "All plugins are up to date.")
	})
}

func TestCommandUpdate_DefaultCacheDir_UsesConfiguredBinaryName(t *testing.T) {
	xdgCache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", xdgCache)

	mycliCacheDir := settings.PluginCacheDirFor("mycli")
	seedTestCache(t, mycliCacheDir, "exec", "1.0.0")

	ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "mycli"

	cmd := CommandUpdate(cliParams, ioStreams, "mycli")
	cmd.SetArgs([]string{"--all", "exec@1.0.0"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	assert.Empty(t, outBuf.String())
	assert.NotContains(t, errBuf.String(), "not found in cache")
	assert.Contains(t, errBuf.String(), "All plugins are up to date.")

	// Nil/empty CliParams falls back to settings.CliBinaryName cache directory.
	defaultCacheDir := settings.PluginCacheDirFor("")
	seedTestCache(t, defaultCacheDir, "exec", "1.0.0")

	ioStreamsDefault, outBufDefault, errBufDefault := terminal.NewTestIOStreams()
	w := writer.New(ioStreamsDefault, &settings.Run{})
	ctx := writer.WithWriter(t.Context(), w)
	kvxOpts := kvx.NewOutputOptions(ioStreamsDefault)
	err := runUpdate(ctx, &UpdateOptions{
		Target: "latest",
		All:    true,
	}, []string{"exec@1.0.0"}, kvxOpts)
	require.NoError(t, err)
	assert.Empty(t, outBufDefault.String())
	assert.Contains(t, errBufDefault.String(), "All plugins are up to date.")
}

func startSingleProviderOCIRegistry(t *testing.T, name, version string, linuxBin []byte) string {
	t.Helper()

	sha256Digest := func(b []byte) string {
		sum := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(sum[:])
	}

	const (
		configMedia = "application/vnd.scafctl.provider.config.v1+json"
		layerMedia  = "application/vnd.scafctl.provider.v1+binary"
		indexMedia  = "application/vnd.oci.image.index.v1+json"
		manifMedia  = "application/vnd.oci.image.manifest.v1+json"
	)

	cfgBytes := []byte(`{}`)
	cfgDigest := sha256Digest(cfgBytes)
	layerDigest := sha256Digest(linuxBin)

	manifestDoc := fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":%q,"config":{"mediaType":%q,"digest":%q,"size":%d},"layers":[{"mediaType":%q,"digest":%q,"size":%d}],"annotations":{"dev.scafctl.artifact.name":%q,"dev.scafctl.artifact.type":"provider","org.opencontainers.image.version":%q,"dev.scafctl.artifact.platform":"linux/amd64"}}`,
		manifMedia, configMedia, cfgDigest, len(cfgBytes), layerMedia, layerDigest, len(linuxBin), name, version,
	)
	manifestBytes := []byte(manifestDoc)
	manifestDigest := sha256Digest(manifestBytes)

	indexDoc := fmt.Sprintf(
		`{"schemaVersion":2,"mediaType":%q,"manifests":[{"mediaType":%q,"digest":%q,"size":%d,"platform":{"os":"linux","architecture":"amd64"}}],"annotations":{"dev.scafctl.artifact.name":%q,"dev.scafctl.artifact.type":"provider","org.opencontainers.image.version":%q}}`,
		indexMedia, manifMedia, manifestDigest, len(manifestBytes), name, version,
	)
	indexBytes := []byte(indexDoc)
	indexDigest := sha256Digest(indexBytes)

	repoPath := "scafctl/providers/" + name
	blobs := map[string][]byte{
		cfgDigest:   cfgBytes,
		layerDigest: linuxBin,
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/v2/" || path == "/v2" {
			w.WriteHeader(http.StatusOK)
			return
		}
		prefix := "/v2/" + repoPath + "/"
		if !strings.HasPrefix(path, prefix) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		rest := strings.TrimPrefix(path, prefix)
		switch {
		case rest == "tags/list":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": repoPath,
				"tags": []string{version},
			})
		case strings.HasPrefix(rest, "manifests/"):
			ref := strings.TrimPrefix(rest, "manifests/")
			var media, dgst string
			var body []byte
			switch ref {
			case version, indexDigest:
				media, dgst, body = indexMedia, indexDigest, indexBytes
			case manifestDigest:
				media, dgst, body = manifMedia, manifestDigest, manifestBytes
			default:
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", media)
			w.Header().Set("Docker-Content-Digest", dgst)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			if r.Method == http.MethodGet {
				_, _ = w.Write(body)
			}
		case strings.HasPrefix(rest, "blobs/"):
			dgst := strings.TrimPrefix(rest, "blobs/")
			data, ok := blobs[dgst]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Docker-Content-Digest", dgst)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			if r.Method == http.MethodGet {
				_, _ = w.Write(data)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return strings.TrimPrefix(srv.URL, "https://")
}

func TestCommandUpdate_MixedSuccess_PartialFailure(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	host := startSingleProviderOCIRegistry(t, "acme-prov", "2.0.0", []byte("fake-acme-prov-2.0.0"))
	appCfg := &config.Config{
		Settings: config.Settings{DisableOfficialCatalog: true},
		Catalogs: []config.CatalogConfig{
			{
				Name:          "test-oci",
				Type:          config.CatalogTypeOCI,
				URL:           "oci://" + host + "/scafctl",
				Insecure:      true,
				ArtifactKinds: []string{"provider"},
			},
		},
	}

	seedMixedCache := func(t *testing.T) (*plugin.Cache, string) {
		t.Helper()
		dir := t.TempDir()
		c := plugin.NewCache(dir)
		_, err := c.Put("acme-prov", "1.0.0", "linux/amd64", []byte("fake-acme-prov-1.0.0"))
		require.NoError(t, err)
		_, err = c.Put("missing-prov", "1.0.0", "linux/amd64", []byte("fake-missing-prov-1.0.0"))
		require.NoError(t, err)
		return c, dir
	}

	t.Run("dry-run renders planned update and returns CatalogError for unresolved plugin", func(t *testing.T) {
		cache, cacheDir := seedMixedCache(t)
		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		ctx := config.WithConfig(t.Context(), appCfg)

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--dry-run", "--platform", "linux/amd64", "--cache-dir", cacheDir, "-o", "json"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))

		var items []updateResultItem
		require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
		require.Len(t, items, 1)
		assert.Equal(t, updateResultItem{
			Name:       "acme-prov",
			OldVersion: "1.0.0",
			NewVersion: "2.0.0",
			Status:     "pending",
		}, items[0])
		assert.Contains(t, errBuf.String(), "missing-prov:")
		assert.Contains(t, errBuf.String(), "Dry run: 1 plugin(s) would be updated:")

		_, latestVer, ok := cache.GetLatestCached("acme-prov", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "1.0.0", latestVer)
	})

	t.Run("actual update writes new version to cache, emits output, and returns CatalogError for unresolved plugin", func(t *testing.T) {
		cache, cacheDir := seedMixedCache(t)
		ioStreams, outBuf, errBuf := terminal.NewTestIOStreams()
		cliParams := settings.NewCliParams()
		ctx := config.WithConfig(t.Context(), appCfg)

		cmd := CommandUpdate(cliParams, ioStreams, "scafctl")
		cmd.SetArgs([]string{"--all", "--platform", "linux/amd64", "--cache-dir", cacheDir, "-o", "json"})
		err := cmd.ExecuteContext(ctx)
		require.Error(t, err)
		assert.Equal(t, exitcode.CatalogError, exitcode.GetCode(err))

		var items []updateResultItem
		require.NoError(t, json.Unmarshal(outBuf.Bytes(), &items))
		require.Len(t, items, 1)
		assert.Equal(t, updateResultItem{
			Name:       "acme-prov",
			OldVersion: "1.0.0",
			NewVersion: "2.0.0",
			Status:     "updated",
		}, items[0])
		assert.Contains(t, errBuf.String(), "missing-prov:")
		assert.Contains(t, errBuf.String(), "Updated 1 plugin(s).")

		_, latestVer, ok := cache.GetLatestCached("acme-prov", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "2.0.0", latestVer)
	})

	t.Run("quiet live and dry-run updates suppress all stdout and stderr", func(t *testing.T) {
		dryDir := t.TempDir()
		dryCache := plugin.NewCache(dryDir)
		_, err := dryCache.Put("acme-prov", "1.0.0", "linux/amd64", []byte("fake-acme-prov-1.0.0"))
		require.NoError(t, err)

		dryStreams, dryOut, dryErr := terminal.NewTestIOStreams()
		ctx := config.WithConfig(t.Context(), appCfg)
		dryCmd := CommandUpdate(settings.NewCliParams(), dryStreams, "scafctl")
		dryCmd.SetArgs([]string{"--all", "--dry-run", "--platform", "linux/amd64", "--cache-dir", dryDir, "-o", "quiet"})
		require.NoError(t, dryCmd.ExecuteContext(ctx))
		assert.Empty(t, dryOut.String())
		assert.Empty(t, dryErr.String())

		liveDir := t.TempDir()
		liveCache := plugin.NewCache(liveDir)
		_, err = liveCache.Put("acme-prov", "1.0.0", "linux/amd64", []byte("fake-acme-prov-1.0.0"))
		require.NoError(t, err)

		liveStreams, liveOut, liveErr := terminal.NewTestIOStreams()
		liveCmd := CommandUpdate(settings.NewCliParams(), liveStreams, "scafctl")
		liveCmd.SetArgs([]string{"--all", "--platform", "linux/amd64", "--cache-dir", liveDir, "-o", "quiet"})
		require.NoError(t, liveCmd.ExecuteContext(ctx))
		assert.Empty(t, liveOut.String())
		assert.Empty(t, liveErr.String())

		_, latestVer, ok := liveCache.GetLatestCached("acme-prov", "linux/amd64")
		require.True(t, ok)
		assert.Equal(t, "2.0.0", latestVer)
	})
}
