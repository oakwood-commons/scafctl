// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/adrg/xdg"
	authofficial "github.com/oakwood-commons/scafctl/pkg/auth/official"
	"github.com/oakwood-commons/scafctl/pkg/plugin"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/solution"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandHandlersInstall_NoOfficialRegistry(t *testing.T) {
	ctx, buf := newTestContext(t)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := commandHandlersInstall(cliParams, ioStreams)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "official auth handler registry not available")
}

func TestCommandHandlersInstall_UnknownHandler(t *testing.T) {
	ctx, buf := newTestContext(t)

	officialReg := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github"},
	})
	ctx = authofficial.WithRegistry(ctx, officialReg)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := commandHandlersInstall(cliParams, ioStreams)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"nonexistent"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown auth handler")
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestCommandHandlersInstall_AlreadyCached(t *testing.T) {
	ctx, buf := newTestContext(t)

	officialReg := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github"},
	})
	ctx = authofficial.WithRegistry(ctx, officialReg)

	// Override xdg.CacheHome directly (t.Setenv doesn't work because
	// the xdg package reads the env var at init-time).
	xdgCache := t.TempDir()
	origCacheHome := xdg.CacheHome
	xdg.CacheHome = xdgCache
	t.Cleanup(func() { xdg.CacheHome = origCacheHome })

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "testcli"

	// Create the binary at the exact path the cache will look for:
	// $XDG_CACHE_HOME/testcli/plugins/<cacheKey>/<version>/<platformCacheKey>/<cacheKey>
	cacheKey := plugin.PluginCacheKey("github", solution.PluginKindAuthHandler)
	platform := plugin.CurrentPlatform()
	binDir := filepath.Join(xdgCache, "testcli", "plugins", cacheKey, "1.0.0", plugin.PlatformCacheKey(platform))
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, cacheKey), []byte("fake"), 0o755))

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := commandHandlersInstall(cliParams, ioStreams)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "already cached")
}

func TestCommandHandlersInstall_RequiresExactlyOneArg(t *testing.T) {
	ctx, buf := newTestContext(t)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := commandHandlersInstall(cliParams, ioStreams)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestCommandHandlersInstall_ForceBypassesCache(t *testing.T) {
	ctx, buf := newTestContext(t)

	officialReg := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github"},
	})
	ctx = authofficial.WithRegistry(ctx, officialReg)

	// Set up a cached binary.
	xdgCache := t.TempDir()
	origCacheHome := xdg.CacheHome
	xdg.CacheHome = xdgCache
	t.Cleanup(func() { xdg.CacheHome = origCacheHome })

	cliParams := settings.NewCliParams()
	cliParams.BinaryName = "testcli"

	cacheKey := plugin.PluginCacheKey("github", solution.PluginKindAuthHandler)
	platform := plugin.CurrentPlatform()
	binDir := filepath.Join(xdgCache, "testcli", "plugins", cacheKey, "1.0.0", plugin.PlatformCacheKey(platform))
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, cacheKey), []byte("fake"), 0o755))

	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := commandHandlersInstall(cliParams, ioStreams)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{"github", "--force"})

	// --force bypasses cache and attempts to download, which fails without
	// a real catalog, but we verify it doesn't short-circuit at cache check.
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloading auth handler")
	assert.Contains(t, buf.String(), "Fetching latest")
}

func TestCommandHandlersInstall_ValidArgsFunction(t *testing.T) {
	ctx, buf := newTestContext(t)

	officialReg := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github"},
		{Name: "entra", CatalogRef: "entra"},
	})
	ctx = authofficial.WithRegistry(ctx, officialReg)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := commandHandlersInstall(cliParams, ioStreams)
	cmd.SetContext(ctx)

	// ValidArgsFunction should return available handler names.
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	assert.Contains(t, completions, "github")
	assert.Contains(t, completions, "entra")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	// After arg is provided, no further completions should be offered.
	completions, directive = cmd.ValidArgsFunction(cmd, []string{"github"}, "")
	assert.Nil(t, completions)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestRemoveAuthHandler_NotCached(t *testing.T) {
	ctx, _ := newTestContext(t)

	cliParams := settings.NewCliParams()
	cache := plugin.NewCache(t.TempDir())

	err := removeAuthHandler(ctx, "github", cliParams, cache)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestRemoveAuthHandler_RemovesCachedHandler(t *testing.T) {
	ctx, buf := newTestContext(t)

	cacheDir := t.TempDir()
	cache := plugin.NewCache(cacheDir)

	// Create the expected directory structure for an auth handler.
	handlerDir := filepath.Join(cacheDir, "auth-handler-github", "1.0.0", "darwin-arm64")
	require.NoError(t, os.MkdirAll(handlerDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(handlerDir, "auth-handler-github"), []byte("fake-binary"), 0o755))

	cliParams := settings.NewCliParams()

	err := removeAuthHandler(ctx, "github", cliParams, cache)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "removed from cache")

	// Verify the directory was actually removed.
	_, err = os.Stat(filepath.Join(cacheDir, "auth-handler-github"))
	assert.True(t, os.IsNotExist(err))
}

func TestRemoveAuthHandler_RequiresExactlyOneArg(t *testing.T) {
	ctx, buf := newTestContext(t)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := commandHandlersRemove(cliParams, ioStreams)
	cmd.SetContext(ctx)
	cmd.SetArgs([]string{})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestRemoveAuthHandler_ValidArgsFunction(t *testing.T) {
	ctx, buf := newTestContext(t)

	officialReg := authofficial.NewRegistryFrom([]authofficial.AuthHandler{
		{Name: "github", CatalogRef: "github"},
	})
	ctx = authofficial.WithRegistry(ctx, officialReg)

	cliParams := settings.NewCliParams()
	ioStreams := terminal.NewIOStreams(nil, buf, buf, false)

	cmd := commandHandlersRemove(cliParams, ioStreams)
	cmd.SetContext(ctx)

	// No args yet — should offer completions.
	completions, directive := cmd.ValidArgsFunction(cmd, []string{}, "")
	assert.Contains(t, completions, "github")
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	// After arg provided — no further completions.
	completions, directive = cmd.ValidArgsFunction(cmd, []string{"github"}, "")
	assert.Nil(t, completions)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
}

func TestRemoveAuthHandler_ShowsLogoutHint(t *testing.T) {
	ctx, buf := newTestContext(t)

	cacheDir := t.TempDir()
	cache := plugin.NewCache(cacheDir)

	handlerDir := filepath.Join(cacheDir, "auth-handler-entra", "2.0.0", "darwin-arm64")
	require.NoError(t, os.MkdirAll(handlerDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(handlerDir, "auth-handler-entra"), []byte("fake"), 0o755))

	cliParams := settings.NewCliParams()

	err := removeAuthHandler(ctx, "entra", cliParams, cache)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "removed from cache")
	assert.Contains(t, output, "auth logout entra")
}

func TestRemoveAuthHandler_PathTraversal(t *testing.T) {
	ctx, _ := newTestContext(t)
	cliParams := settings.NewCliParams()
	cache := plugin.NewCache(t.TempDir())

	tests := []struct {
		name    string
		handler string
	}{
		{name: "dot-dot", handler: "../../../etc"},
		{name: "slash", handler: "foo/bar"},
		{name: "backslash", handler: `foo\bar`},
		{name: "single-dot", handler: "."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := removeAuthHandler(ctx, tt.handler, cliParams, cache)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "invalid auth handler name")
		})
	}
}
