// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// TestManager_Load_ConfigDir_MergesFragments verifies that fragments in the
// config.d directory are merged beneath the user's config file.
func TestManager_Load_ConfigDir_MergesFragments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	writeFile(t, configPath, "settings:\n  defaultCatalog: user-catalog\n")
	writeFile(t, filepath.Join(dir, ConfigDirName, "10-telemetry.yaml"),
		"telemetry:\n  serviceName: from-fragment\n")

	cfg, err := NewManager(configPath).Load()
	require.NoError(t, err)

	// User config wins for keys it defines.
	assert.Equal(t, "user-catalog", cfg.Settings.DefaultCatalog)
	// Fragment supplies keys the user config does not define.
	assert.Equal(t, "from-fragment", cfg.Telemetry.ServiceName)
}

// TestManager_Load_ConfigDir_UserFileOverridesFragment verifies precedence:
// config.yaml overrides config.d fragments.
func TestManager_Load_ConfigDir_UserFileOverridesFragment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	writeFile(t, configPath, "telemetry:\n  serviceName: from-user\n")
	writeFile(t, filepath.Join(dir, ConfigDirName, "10-telemetry.yaml"),
		"telemetry:\n  serviceName: from-fragment\n")

	cfg, err := NewManager(configPath).Load()
	require.NoError(t, err)
	assert.Equal(t, "from-user", cfg.Telemetry.ServiceName)
}

// TestManager_Load_ConfigDir_LexicalOrder verifies later fragments override
// earlier ones in lexical filename order.
func TestManager_Load_ConfigDir_LexicalOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	writeFile(t, configPath, "settings:\n  defaultCatalog: user-catalog\n")
	writeFile(t, filepath.Join(dir, ConfigDirName, "10-a.yaml"),
		"telemetry:\n  serviceName: first\n")
	writeFile(t, filepath.Join(dir, ConfigDirName, "20-b.yml"),
		"telemetry:\n  serviceName: second\n")

	cfg, err := NewManager(configPath).Load()
	require.NoError(t, err)
	assert.Equal(t, "second", cfg.Telemetry.ServiceName)
}

// TestManager_Load_ConfigDir_MissingIsNotError verifies a missing config.d
// directory is not an error.
func TestManager_Load_ConfigDir_MissingIsNotError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	writeFile(t, configPath, "settings:\n  defaultCatalog: user-catalog\n")

	cfg, err := NewManager(configPath).Load()
	require.NoError(t, err)
	assert.Equal(t, "user-catalog", cfg.Settings.DefaultCatalog)
}

// TestManager_Load_ConfigDir_InvalidFragmentErrors verifies a malformed
// fragment surfaces an error.
func TestManager_Load_ConfigDir_InvalidFragmentErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	writeFile(t, configPath, "settings:\n  defaultCatalog: user-catalog\n")
	writeFile(t, filepath.Join(dir, ConfigDirName, "bad.yaml"), "telemetry: [unterminated\n")

	_, err := NewManager(configPath).Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad.yaml")
}

// TestManager_ConfigDir verifies ConfigDir returns the directory of the config
// file, for both an explicit path and after loading the default.
func TestManager_ConfigDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	mgr := NewManager(configPath)
	assert.Equal(t, dir, mgr.ConfigDir())

	writeFile(t, configPath, "settings:\n  defaultCatalog: user-catalog\n")
	_, err := mgr.Load()
	require.NoError(t, err)
	assert.Equal(t, dir, mgr.ConfigDir())
}

// TestManager_Save_DoesNotBakeConfigDirValues verifies that values supplied
// only by a config.d fragment are not written into the user's config file on
// Save, so drop-in layering stays overridable.
func TestManager_Save_DoesNotBakeConfigDirValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	writeFile(t, configPath, "settings:\n  defaultCatalog: user-catalog\n")
	writeFile(t, filepath.Join(dir, ConfigDirName, "10-telemetry.yaml"),
		"telemetry:\n  serviceName: from-fragment\n")

	mgr := NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)
	require.Equal(t, "from-fragment", cfg.Telemetry.ServiceName)

	// Mutate an unrelated user-owned value and save.
	cfg.Logging.Level = "error"
	require.NoError(t, mgr.Save())

	// The fragment value must not be baked into the user's config file.
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "from-fragment",
		"config.d fragment value must not be persisted to the user config file")

	// Changing the fragment afterward must still take effect (proves the value
	// was not made sticky by the save).
	writeFile(t, filepath.Join(dir, ConfigDirName, "10-telemetry.yaml"),
		"telemetry:\n  serviceName: changed-fragment\n")
	cfg2, err := NewManager(configPath).Load()
	require.NoError(t, err)
	assert.Equal(t, "changed-fragment", cfg2.Telemetry.ServiceName)
	assert.Equal(t, "error", cfg2.Logging.Level)
}

// TestManager_Save_KeepsUserOverrideOfFragmentKey verifies that when the user
// config sets the same key a fragment provides, Save persists the user's value.
func TestManager_Save_KeepsUserOverrideOfFragmentKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")

	writeFile(t, configPath, "telemetry:\n  serviceName: from-user\n")
	writeFile(t, filepath.Join(dir, ConfigDirName, "10-telemetry.yaml"),
		"telemetry:\n  serviceName: from-fragment\n")

	mgr := NewManager(configPath)
	cfg, err := mgr.Load()
	require.NoError(t, err)
	require.Equal(t, "from-user", cfg.Telemetry.ServiceName)

	cfg.Logging.Level = "error"
	require.NoError(t, mgr.Save())

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "from-user",
		"user-owned value that shadows a fragment key must be persisted")
}
