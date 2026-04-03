// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal"
	"github.com/oakwood-commons/scafctl/pkg/terminal/writer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newInstallTestCtx creates a context with a writer for credentialhelper command tests.
func newInstallTestCtx() context.Context {
	var buf bytes.Buffer
	ioStreams := terminal.NewIOStreams(nil, &buf, &buf, false)
	w := writer.New(ioStreams, settings.NewCliParams())
	return writer.WithWriter(context.Background(), w)
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "tilde prefix", path: "~/bin", want: filepath.Join(home, "bin")},
		{name: "tilde only", path: "~", want: home},
		{name: "absolute path", path: "/usr/local/bin", want: "/usr/local/bin"},
		{name: "relative path", path: "relative/path", want: "relative/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, expandHome(tt.path))
		})
	}
}

func TestCreateSymlink(t *testing.T) {
	// Use the test binary as a valid executable target
	exe, err := os.Executable()
	require.NoError(t, err)

	t.Run("creates symlink", func(t *testing.T) {
		dir := t.TempDir()
		linkPath := filepath.Join(dir, "docker-credential-test")

		err := createSymlink(exe, linkPath)
		require.NoError(t, err)

		target, err := os.Readlink(linkPath)
		require.NoError(t, err)
		assert.Equal(t, exe, target)
	})

	t.Run("replaces existing symlink", func(t *testing.T) {
		dir := t.TempDir()
		linkPath := filepath.Join(dir, "docker-credential-test")

		// Create initial symlink
		require.NoError(t, os.Symlink("/nonexistent", linkPath))

		// Should replace it
		err := createSymlink(exe, linkPath)
		require.NoError(t, err)

		target, err := os.Readlink(linkPath)
		require.NoError(t, err)
		assert.Equal(t, exe, target)
	})

	t.Run("refuses to overwrite non-symlink", func(t *testing.T) {
		dir := t.TempDir()
		linkPath := filepath.Join(dir, "docker-credential-test")

		// Create a regular file
		require.NoError(t, os.WriteFile(linkPath, []byte("data"), 0o644))

		err := createSymlink(exe, linkPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a symlink")
	})
}

func TestReadWriteContainerConfig(t *testing.T) {
	t.Run("read nonexistent returns empty map", func(t *testing.T) {
		cfg, err := readContainerConfig(filepath.Join(t.TempDir(), "config.json"))
		require.NoError(t, err)
		assert.Empty(t, cfg)
	})

	t.Run("round trip", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		cfg := map[string]interface{}{"credsStore": "desktop", "auths": map[string]interface{}{}}

		require.NoError(t, writeContainerConfig(path, cfg))

		got, err := readContainerConfig(path)
		require.NoError(t, err)
		assert.Equal(t, "desktop", got["credsStore"])
	})

	t.Run("preserves existing keys", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		initial := map[string]interface{}{
			"auths":      map[string]interface{}{"ghcr.io": map[string]interface{}{"auth": "xyz"}},
			"credsStore": "desktop",
		}
		require.NoError(t, writeContainerConfig(path, initial))

		// Update with scafctl credsStore
		cfg, err := readContainerConfig(path)
		require.NoError(t, err)
		cfg["credsStore"] = settings.CliBinaryName
		require.NoError(t, writeContainerConfig(path, cfg))

		// Verify auths preserved
		got, err := readContainerConfig(path)
		require.NoError(t, err)
		assert.Equal(t, settings.CliBinaryName, got["credsStore"])
		assert.NotNil(t, got["auths"])
	})
}

func TestUpdateContainerConfig(t *testing.T) {
	t.Run("global credsStore", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, updateContainerConfig(path, "", nil))

		data, err := os.ReadFile(path)
		require.NoError(t, err)

		var cfg map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &cfg))
		assert.Equal(t, settings.CliBinaryName, cfg["credsStore"])
	})

	t.Run("per-registry credHelper", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, updateContainerConfig(path, "ghcr.io", nil))

		data, err := os.ReadFile(path)
		require.NoError(t, err)

		var cfg map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &cfg))

		credHelpers, ok := cfg["credHelpers"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, settings.CliBinaryName, credHelpers["ghcr.io"])
	})
}

func TestRemoveFromContainerConfig(t *testing.T) {
	t.Run("remove global credsStore", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		cfg := map[string]interface{}{"credsStore": settings.CliBinaryName}
		require.NoError(t, writeContainerConfig(path, cfg))

		require.NoError(t, removeFromContainerConfig(path, "", nil))

		got, err := readContainerConfig(path)
		require.NoError(t, err)
		_, hasCredsStore := got["credsStore"]
		assert.False(t, hasCredsStore)
	})

	t.Run("does not remove other credsStore", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		cfg := map[string]interface{}{"credsStore": "desktop"}
		require.NoError(t, writeContainerConfig(path, cfg))

		require.NoError(t, removeFromContainerConfig(path, "", nil))

		got, err := readContainerConfig(path)
		require.NoError(t, err)
		assert.Equal(t, "desktop", got["credsStore"])
	})

	t.Run("remove per-registry credHelper", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		cfg := map[string]interface{}{
			"credHelpers": map[string]interface{}{"ghcr.io": settings.CliBinaryName, "docker.io": "desktop"},
		}
		require.NoError(t, writeContainerConfig(path, cfg))

		require.NoError(t, removeFromContainerConfig(path, "ghcr.io", nil))

		got, err := readContainerConfig(path)
		require.NoError(t, err)
		credHelpers := got["credHelpers"].(map[string]interface{})
		assert.NotContains(t, credHelpers, "ghcr.io")
		assert.Contains(t, credHelpers, "docker.io")
	})

	t.Run("remove last credHelper removes key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		cfg := map[string]interface{}{
			"credHelpers": map[string]interface{}{"ghcr.io": settings.CliBinaryName},
		}
		require.NoError(t, writeContainerConfig(path, cfg))

		require.NoError(t, removeFromContainerConfig(path, "ghcr.io", nil))

		got, err := readContainerConfig(path)
		require.NoError(t, err)
		_, hasCredHelpers := got["credHelpers"]
		assert.False(t, hasCredHelpers)
	})

	t.Run("nonexistent file is no-op", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nonexistent", "config.json")
		err := removeFromContainerConfig(path, "", nil)
		assert.NoError(t, err)
	})
}

func TestDockerConfigPath(t *testing.T) {
	t.Run("uses DOCKER_CONFIG env", func(t *testing.T) {
		t.Setenv("DOCKER_CONFIG", "/custom/docker")
		assert.Equal(t, "/custom/docker/config.json", dockerConfigPath())
	})

	t.Run("defaults to ~/.docker", func(t *testing.T) {
		t.Setenv("DOCKER_CONFIG", "")
		home, _ := os.UserHomeDir()
		assert.Equal(t, filepath.Join(home, ".docker", "config.json"), dockerConfigPath())
	})
}

func TestPodmanConfigPath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	t.Run("defaults to ~/.config/containers/auth.json", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "")
		got := podmanConfigPath()
		assert.Equal(t, filepath.Join(home, ".config", "containers", "auth.json"), got)
	})
}

func TestCommandInstall_Structure(t *testing.T) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandInstall(ioStreams)

	require.NotNil(t, cmd)
	assert.Equal(t, "install", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Verify flags exist
	assert.NotNil(t, cmd.Flags().Lookup("docker"), "flag 'docker' should exist")
	assert.NotNil(t, cmd.Flags().Lookup("podman"), "flag 'podman' should exist")
	assert.NotNil(t, cmd.Flags().Lookup("registry"), "flag 'registry' should exist")
}

func TestCommandUninstall_Structure(t *testing.T) {
	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandUninstall(ioStreams)

	require.NotNil(t, cmd)
	assert.Equal(t, "uninstall", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	assert.NotNil(t, cmd.Flags().Lookup("docker"), "flag 'docker' should exist")
	assert.NotNil(t, cmd.Flags().Lookup("podman"), "flag 'podman' should exist")
}

func TestCommandUninstall_RefusesNonSymlink(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, symlinkName)

	// Create a regular file at the symlink path
	require.NoError(t, os.WriteFile(linkPath, []byte("not a symlink"), 0o644))

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandUninstall(ioStreams)
	cmd.SetContext(newInstallTestCtx())
	cmd.SetArgs([]string{"--bin-dir", dir})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to remove non-symlink")

	// Regular file must not be deleted
	_, statErr := os.Stat(linkPath)
	assert.NoError(t, statErr, "regular file should not have been deleted")
}

func TestCommandUninstall_NonExistentSymlink(t *testing.T) {
	dir := t.TempDir()

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandUninstall(ioStreams)
	cmd.SetContext(newInstallTestCtx())
	cmd.SetArgs([]string{"--bin-dir", dir})

	// Should succeed gracefully when symlink doesn't exist
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestFindScafctlBinary(t *testing.T) {
	path, err := findScafctlBinary()
	require.NoError(t, err)
	assert.NotEmpty(t, path)
	// Should be an absolute path
	assert.True(t, filepath.IsAbs(path), "binary path should be absolute, got %s", path)
}
