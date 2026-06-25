// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package credentialhelper

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation may require elevated privilege on Windows")
		}

		dir := t.TempDir()
		linkPath := filepath.Join(dir, "docker-credential-test")

		err := createSymlink(exe, linkPath)
		require.NoError(t, err)

		target, err := os.Readlink(linkPath)
		require.NoError(t, err)
		assert.Equal(t, exe, target)
	})

	t.Run("replaces existing symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlink creation may require elevated privilege on Windows")
		}

		dir := t.TempDir()
		linkPath := filepath.Join(dir, "docker-credential-test")

		// Create initial symlink
		require.NoError(t, os.Symlink(filepath.FromSlash("/nonexistent"), linkPath))

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
		require.NoError(t, updateContainerConfig(path, "", settings.CliBinaryName))

		data, err := os.ReadFile(path)
		require.NoError(t, err)

		var cfg map[string]interface{}
		require.NoError(t, json.Unmarshal(data, &cfg))
		assert.Equal(t, settings.CliBinaryName, cfg["credsStore"])
	})

	t.Run("per-registry credHelper", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		require.NoError(t, updateContainerConfig(path, "ghcr.io", settings.CliBinaryName))

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

		require.NoError(t, removeFromContainerConfig(path, "", settings.CliBinaryName))

		got, err := readContainerConfig(path)
		require.NoError(t, err)
		_, hasCredsStore := got["credsStore"]
		assert.False(t, hasCredsStore)
	})

	t.Run("does not remove other credsStore", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		cfg := map[string]interface{}{"credsStore": "desktop"}
		require.NoError(t, writeContainerConfig(path, cfg))

		require.NoError(t, removeFromContainerConfig(path, "", settings.CliBinaryName))

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

		require.NoError(t, removeFromContainerConfig(path, "ghcr.io", settings.CliBinaryName))

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

		require.NoError(t, removeFromContainerConfig(path, "ghcr.io", settings.CliBinaryName))

		got, err := readContainerConfig(path)
		require.NoError(t, err)
		_, hasCredHelpers := got["credHelpers"]
		assert.False(t, hasCredHelpers)
	})

	t.Run("nonexistent file is no-op", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nonexistent", "config.json")
		err := removeFromContainerConfig(path, "", settings.CliBinaryName)
		assert.NoError(t, err)
	})
}

func TestDockerConfigPath(t *testing.T) {
	t.Run("uses DOCKER_CONFIG env", func(t *testing.T) {
		customDir := filepath.FromSlash("/custom/docker")
		t.Setenv("DOCKER_CONFIG", customDir)
		assert.Equal(t, filepath.Join(customDir, "config.json"), dockerConfigPath())
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
	cmd := commandInstall(ioStreams, "scafctl")

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
	cmd := commandUninstall(ioStreams, "scafctl")

	require.NotNil(t, cmd)
	assert.Equal(t, "uninstall", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	assert.NotNil(t, cmd.Flags().Lookup("docker"), "flag 'docker' should exist")
	assert.NotNil(t, cmd.Flags().Lookup("podman"), "flag 'podman' should exist")
}

func TestCommandUninstall_RefusesNonSymlink(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, credHelperName("scafctl"))

	// Create a regular file at the symlink path
	require.NoError(t, os.WriteFile(linkPath, []byte("not a symlink"), 0o644))

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandUninstall(ioStreams, "scafctl")
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
	cmd := commandUninstall(ioStreams, "scafctl")
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

func TestCredHelperFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		binaryName string
		goos       string
		want       string
	}{
		{name: "linux default", binaryName: "scafctl", goos: "linux", want: "docker-credential-scafctl"},
		{name: "darwin default", binaryName: "scafctl", goos: "darwin", want: "docker-credential-scafctl"},
		{name: "windows default", binaryName: "scafctl", goos: "windows", want: "docker-credential-scafctl.cmd"},
		{name: "embedder unix", binaryName: "mycli", goos: "linux", want: "docker-credential-mycli"},
		{name: "embedder windows", binaryName: "mycli", goos: "windows", want: "docker-credential-mycli.cmd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, credHelperFileName(tt.binaryName, tt.goos))
		})
	}
}

func TestShimContent(t *testing.T) {
	t.Parallel()

	const target = "/usr/local/bin/scafctl"

	unix := shimContent(target, "linux")
	assert.Contains(t, unix, "#!/bin/sh")
	assert.Contains(t, unix, shimSignature)
	assert.Contains(t, unix, `exec "`+target+`" credential-helper "$@"`)

	win := shimContent(target, "windows")
	assert.Contains(t, win, "@echo off")
	assert.Contains(t, win, shimSignature)
	assert.Contains(t, win, `"`+target+`" credential-helper %*`)
	assert.Contains(t, win, "\r\n", "windows shim should use CRLF line endings")
}

func TestWriteShimAndIsManagedShim(t *testing.T) {
	target, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	shimPath := filepath.Join(dir, "shim.sh")

	require.NoError(t, writeShim(target, shimPath, "linux"))

	data, err := os.ReadFile(shimPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), shimSignature)
	assert.Contains(t, string(data), target)

	managed, err := isManagedShim(shimPath)
	require.NoError(t, err)
	assert.True(t, managed, "generated shim should be recognized as managed")

	// A foreign regular file must not be recognized as a managed shim.
	foreign := filepath.Join(dir, "foreign")
	require.NoError(t, os.WriteFile(foreign, []byte("not a shim"), 0o644))
	managed, err = isManagedShim(foreign)
	require.NoError(t, err)
	assert.False(t, managed, "unrelated file should not be a managed shim")
}

func TestWriteShim_ReplacesManagedShim(t *testing.T) {
	target, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	shimPath := filepath.Join(dir, "shim.sh")

	// A previously generated managed shim may be replaced.
	require.NoError(t, writeShim(target, shimPath, "linux"))
	require.NoError(t, writeShim(target, shimPath, "linux"))

	data, err := os.ReadFile(shimPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), shimSignature)
}

func TestWriteShim_RefusesForeignFile(t *testing.T) {
	target, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	shimPath := filepath.Join(dir, "shim.sh")
	require.NoError(t, os.WriteFile(shimPath, []byte("user data"), 0o644))

	err = writeShim(target, shimPath, "linux")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a managed shim")

	// The unrelated file must be left untouched.
	data, err := os.ReadFile(shimPath)
	require.NoError(t, err)
	assert.Equal(t, "user data", string(data))
}

func TestWriteShim_ReplacesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privilege on Windows")
	}
	target, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	shimPath := filepath.Join(dir, "shim.sh")
	require.NoError(t, os.Symlink(filepath.FromSlash("/nonexistent"), shimPath))

	require.NoError(t, writeShim(target, shimPath, "linux"))

	data, err := os.ReadFile(shimPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), shimSignature)
}

func TestWriteShim_NonExecutableTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "does-not-exist")
	shimPath := filepath.Join(dir, "shim.sh")

	err := writeShim(target, shimPath, "linux")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not executable")
}

func TestIsManagedShim_MatchesWindowsSignatureLine(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "shim.cmd")
	require.NoError(t, os.WriteFile(shimPath, []byte(shimContent("target.exe", "windows")), 0o644))

	managed, err := isManagedShim(shimPath)
	require.NoError(t, err)
	assert.True(t, managed, "generated windows shim should be recognized as managed")
}

func TestIsManagedShim_RejectsSignatureBeyondHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large")

	// Place the signature far past the bounded header so it must not match.
	content := strings.Repeat("x", shimHeaderScanBytes+64) + "\n" + shimSignatureLinePOSIX + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	managed, err := isManagedShim(path)
	require.NoError(t, err)
	assert.False(t, managed, "signature outside the scanned header must not match")
}

func TestIsManagedShim_RejectsSubstringWithoutExactLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "incidental")

	// The signature text appears, but never as a standalone comment line.
	content := "prefix " + shimSignature + " suffix on the same line\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	managed, err := isManagedShim(path)
	require.NoError(t, err)
	assert.False(t, managed, "incidental substring must not be treated as a managed shim")
}

func TestIsManagedShim_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty")
	require.NoError(t, os.WriteFile(path, nil, 0o644))

	managed, err := isManagedShim(path)
	require.NoError(t, err)
	assert.False(t, managed, "empty file is not a managed shim")
}

func TestIsManagedShim_MissingFile(t *testing.T) {
	managed, err := isManagedShim(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
	assert.False(t, managed)
}

func TestInstallHelper(t *testing.T) {
	target, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	linkPath := filepath.Join(dir, credHelperFileName("scafctl", runtime.GOOS))

	method, err := installHelper(target, linkPath, runtime.GOOS)
	require.NoError(t, err)

	if runtime.GOOS == "windows" {
		assert.Equal(t, "shim", method)
	} else {
		assert.Equal(t, "symlink", method)
	}

	_, err = os.Lstat(linkPath)
	require.NoError(t, err, "installed helper should exist at %s", linkPath)
}

func TestInstallHelper_Windows(t *testing.T) {
	target, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	linkPath := filepath.Join(dir, credHelperFileName("scafctl", "windows"))

	method, err := installHelper(target, linkPath, "windows")
	require.NoError(t, err)
	assert.Equal(t, "shim", method)

	data, err := os.ReadFile(linkPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), shimSignature)
	assert.True(t, strings.HasSuffix(linkPath, ".cmd"))
}

func TestCommandUninstall_RemovesManagedShim(t *testing.T) {
	target, err := os.Executable()
	require.NoError(t, err)

	dir := t.TempDir()
	shimPath := filepath.Join(dir, credHelperFileName("scafctl", runtime.GOOS))
	require.NoError(t, writeShim(target, shimPath, runtime.GOOS))

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandUninstall(ioStreams, "scafctl")
	cmd.SetContext(newInstallTestCtx())
	cmd.SetArgs([]string{"--bin-dir", dir})

	require.NoError(t, cmd.Execute())

	_, statErr := os.Stat(shimPath)
	assert.True(t, os.IsNotExist(statErr), "managed shim should be removed")
}

func TestCommandInstall_RunE(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privilege on Windows")
	}
	binDir := t.TempDir()
	dockerDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerDir)

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandInstall(ioStreams, "scafctl")
	cmd.SetContext(newInstallTestCtx())
	cmd.SetArgs([]string{"--bin-dir", binDir, "--docker"})

	require.NoError(t, cmd.Execute())

	// Helper installed.
	linkPath := filepath.Join(binDir, credHelperFileName("scafctl", runtime.GOOS))
	_, err := os.Lstat(linkPath)
	require.NoError(t, err, "credential helper should be installed at %s", linkPath)

	// Docker config updated with global credsStore.
	cfg, err := readContainerConfig(filepath.Join(dockerDir, "config.json"))
	require.NoError(t, err)
	assert.Equal(t, "scafctl", cfg["credsStore"])
}

func TestCommandInstall_RunE_Registry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privilege on Windows")
	}
	binDir := t.TempDir()
	dockerDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerDir)

	ioStreams, _, _ := terminal.NewTestIOStreams()
	cmd := commandInstall(ioStreams, "scafctl")
	cmd.SetContext(newInstallTestCtx())
	cmd.SetArgs([]string{"--bin-dir", binDir, "--docker", "--registry", "ghcr.io"})

	require.NoError(t, cmd.Execute())

	cfg, err := readContainerConfig(filepath.Join(dockerDir, "config.json"))
	require.NoError(t, err)
	credHelpers, ok := cfg["credHelpers"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "scafctl", credHelpers["ghcr.io"])
}

func TestCommandInstallUninstall_DockerRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation may require elevated privilege on Windows")
	}
	binDir := t.TempDir()
	dockerDir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dockerDir)

	ioStreams, _, _ := terminal.NewTestIOStreams()

	install := commandInstall(ioStreams, "scafctl")
	install.SetContext(newInstallTestCtx())
	install.SetArgs([]string{"--bin-dir", binDir, "--docker"})
	require.NoError(t, install.Execute())

	uninstall := commandUninstall(ioStreams, "scafctl")
	uninstall.SetContext(newInstallTestCtx())
	uninstall.SetArgs([]string{"--bin-dir", binDir, "--docker"})
	require.NoError(t, uninstall.Execute())

	// Helper removed.
	linkPath := filepath.Join(binDir, credHelperFileName("scafctl", runtime.GOOS))
	_, statErr := os.Lstat(linkPath)
	assert.True(t, os.IsNotExist(statErr), "credential helper should be removed")

	// Docker credsStore cleaned.
	cfg, err := readContainerConfig(filepath.Join(dockerDir, "config.json"))
	require.NoError(t, err)
	_, hasCredsStore := cfg["credsStore"]
	assert.False(t, hasCredsStore, "credsStore should be removed from docker config")
}
