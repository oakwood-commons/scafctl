// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	hclog "github.com/hashicorp/go-hclog"
	hplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortTempDir returns a temp directory whose path is short enough that the
// socket directory newSocketDir makes inside it passes the length check. The
// default t.TempDir path includes the test name and can be too long on macOS.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sd-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestNewSocketDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Run("is not used on Windows, where plugins talk over TCP", func(t *testing.T) {
			assert.Empty(t, newSocketDir(true))
		})
		return
	}

	t.Run("creates a private directory under the temp directory", func(t *testing.T) {
		tmp := shortTempDir(t)
		t.Setenv("TMPDIR", tmp)

		dir := newSocketDir(true)

		require.NotEmpty(t, dir)
		assert.Equal(t, tmp, filepath.Dir(dir))
		assert.True(t, strings.HasPrefix(filepath.Base(dir), socketDirPrefix))
		info, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
		assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "only the current user may reach the plugin's socket")
	})

	t.Run("gives each plugin its own directory", func(t *testing.T) {
		t.Setenv("TMPDIR", shortTempDir(t))
		first, second := newSocketDir(true), newSocketDir(true)
		require.NotEmpty(t, first)
		require.NotEmpty(t, second)
		assert.NotEqual(t, first, second)
	})

	t.Run("declines when a socket path inside it would be too long, and leaves nothing behind", func(t *testing.T) {
		tmp := shortTempDir(t)
		// Long enough that tmp/plugin-dirNNNNNNNNNN/pluginNNNNNNNNNN passes the limit.
		long := filepath.Join(tmp, strings.Repeat("x", maxSocketPathLen))
		require.NoError(t, os.Mkdir(long, 0o750))
		t.Setenv("TMPDIR", long)

		assert.Empty(t, newSocketDir(true))

		entries, err := os.ReadDir(long)
		require.NoError(t, err)
		assert.Empty(t, entries, "the directory made to measure the path was removed")
	})

	t.Run("declines when the operator has chosen a socket directory", func(t *testing.T) {
		operatorDir := shortTempDir(t) // made before TMPDIR is redirected below
		tmp := shortTempDir(t)
		t.Setenv("TMPDIR", tmp)
		t.Setenv(hplugin.EnvUnixSocketDir, operatorDir)

		assert.Empty(t, newSocketDir(true), "their value would win over one set by the host, so none is made")

		entries, err := os.ReadDir(tmp)
		require.NoError(t, err)
		assert.Empty(t, entries, "nothing was created in the temp directory")
	})

	t.Run("makes one anyway when the environment is sanitized, because the operator's variable never reaches the plugin", func(t *testing.T) {
		operatorDir := shortTempDir(t) // made before TMPDIR is redirected below
		tmp := shortTempDir(t)
		t.Setenv("TMPDIR", tmp)
		t.Setenv(hplugin.EnvUnixSocketDir, operatorDir)

		dir := newSocketDir(false)

		require.NotEmpty(t, dir)
		assert.Equal(t, tmp, filepath.Dir(dir))
	})

	t.Run("declines when the temp directory cannot be written", func(t *testing.T) {
		t.Setenv("TMPDIR", filepath.Join(shortTempDir(t), "does-not-exist"))
		assert.Empty(t, newSocketDir(true), "the plugin falls back to creating its socket itself")
	})

	t.Run("accepts the longest path that still fits", func(t *testing.T) {
		// A temp directory sized so the worst-case socket path is exactly the limit.
		tmp := shortTempDir(t)
		worstSuffix := 1 + len(socketDirPrefix) + 10 + 1 + socketFileNameLen
		pad := maxSocketPathLen - worstSuffix - len(tmp) - 1
		if pad < 1 {
			t.Skip("the temp directory is already too long to build this case")
		}
		fit := filepath.Join(tmp, strings.Repeat("f", pad))
		require.NoError(t, os.Mkdir(fit, 0o750))
		t.Setenv("TMPDIR", fit)

		dir := newSocketDir(true)

		assert.NotEmpty(t, dir)
		if dir != "" {
			assert.LessOrEqual(t, len(dir)+1+socketFileNameLen, maxSocketPathLen)
		}
	})
}

func TestRemoveSocketDir(t *testing.T) {
	t.Run("removes the directory and what is in it", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "plugin-dir1")
		require.NoError(t, os.Mkdir(dir, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "plugin2"), nil, 0o600))

		removeSocketDir(dir)

		assert.NoDirExists(t, dir)
	})

	t.Run("an empty path means none was made and removes nothing", func(t *testing.T) {
		cwd, err := os.Getwd()
		require.NoError(t, err)
		removeSocketDir("")
		assert.DirExists(t, cwd)
	})

	t.Run("a directory that is already gone is not an error", func(t *testing.T) {
		removeSocketDir(filepath.Join(t.TempDir(), "never-existed"))
	})
}

func TestClientKill_RemovesTheSocketDirectory(t *testing.T) {
	newDir := func(t *testing.T) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "plugin-dir1")
		require.NoError(t, os.Mkdir(dir, 0o750))
		return dir
	}

	t.Run("provider client", func(t *testing.T) {
		dir := newDir(t)
		c := &Client{pluginClient: &hplugin.Client{}, socketDir: dir}

		c.Kill()
		assert.NoDirExists(t, dir)

		assert.NotPanics(t, c.Kill, "killing twice is safe")
	})

	t.Run("provider client without a process still removes its directory", func(t *testing.T) {
		dir := newDir(t)
		c := &Client{socketDir: dir}
		c.Kill()
		assert.NoDirExists(t, dir)
	})

	t.Run("a nil client is safe", func(t *testing.T) {
		var c *Client
		assert.NotPanics(t, c.Kill)
	})

	t.Run("auth handler client", func(t *testing.T) {
		dir := newDir(t)
		c := &AuthHandlerClient{pluginClient: &hplugin.Client{}, socketDir: dir}

		c.Kill()
		assert.NoDirExists(t, dir)

		assert.NotPanics(t, c.Kill, "killing twice is safe")
	})
}

func TestBuildPluginClient_SocketDirectoryOwnership(t *testing.T) {
	makeCfg := func(clientOptions, hclog.Logger, func(string) *exec.Cmd) pluginConfig { return pluginConfig{} }
	accept := func(any) (ProviderPlugin, error) { return &MockProviderPlugin{}, nil }
	reject := func(any) (ProviderPlugin, error) { return nil, assert.AnError }

	// newDir stands in for the directory the real connector creates.
	newDir := func(t *testing.T) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "plugin-dir1")
		require.NoError(t, os.Mkdir(dir, 0o750))
		return dir
	}

	t.Run("the connector is asked for a directory and the caller gets its path", func(t *testing.T) {
		dir := newDir(t)
		var asked bool
		_, _, got, err := buildPluginClient("p", nil,
			func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
				asked = cfg.socketDir != nil
				*cfg.socketDir = dir
				return &MockProviderPlugin{}, &hplugin.Client{}, nil
			}, makeCfg, accept)

		require.NoError(t, err)
		assert.True(t, asked)
		assert.Equal(t, dir, got)
		assert.DirExists(t, dir, "on success the caller owns the directory and removes it when the plugin is killed")
	})

	t.Run("a failed connection removes the directory", func(t *testing.T) {
		dir := newDir(t)
		_, _, got, err := buildPluginClient("p", nil,
			func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
				*cfg.socketDir = dir
				return nil, nil, assert.AnError
			}, makeCfg, accept)

		require.Error(t, err)
		assert.Empty(t, got)
		assert.NoDirExists(t, dir)
	})

	t.Run("a plugin of the wrong type is killed and its directory removed", func(t *testing.T) {
		dir := newDir(t)
		_, _, got, err := buildPluginClient("p", nil,
			func(_ string, cfg pluginConfig) (any, *hplugin.Client, error) {
				*cfg.socketDir = dir
				return struct{}{}, &hplugin.Client{}, nil
			}, makeCfg, reject)

		require.Error(t, err)
		assert.Empty(t, got)
		assert.NoDirExists(t, dir)
	})

	t.Run("a connector that makes no directory is fine", func(t *testing.T) {
		_, _, got, err := buildPluginClient("p", nil,
			func(string, pluginConfig) (any, *hplugin.Client, error) {
				return &MockProviderPlugin{}, &hplugin.Client{}, nil
			}, makeCfg, accept)

		require.NoError(t, err)
		assert.Empty(t, got)
	})
}
