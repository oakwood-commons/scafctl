// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/adrg/xdg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigFile(t *testing.T) {
	t.Run("returns path containing app name and config file", func(t *testing.T) {
		path, err := ConfigFile()
		require.NoError(t, err)
		assert.Contains(t, path, AppName)
		assert.Contains(t, path, ConfigFileName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_CONFIG_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path, err := ConfigFile()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(path, tmpDir), "path should start with XDG_CONFIG_HOME")
		assert.Equal(t, filepath.Join(tmpDir, AppName, ConfigFileName), path)

		// Verify directory was created
		dirPath := filepath.Dir(path)
		info, err := os.Stat(dirPath)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})
}

func TestSearchConfigFile(t *testing.T) {
	t.Run("returns error when file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		_, err := SearchConfigFile()
		assert.Error(t, err)
	})

	t.Run("finds existing config file", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		// Create the config file
		configDir := filepath.Join(tmpDir, AppName)
		err := os.MkdirAll(configDir, 0o700)
		require.NoError(t, err)

		configPath := filepath.Join(configDir, ConfigFileName)
		err = os.WriteFile(configPath, []byte("test: value"), 0o600)
		require.NoError(t, err)

		// Search should find it
		foundPath, err := SearchConfigFile()
		require.NoError(t, err)
		assert.Equal(t, configPath, foundPath)
	})
}

func TestConfigDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := ConfigDir()
		assert.Contains(t, path, AppName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_CONFIG_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := ConfigDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName), path)
	})
}

func TestSecretsDir(t *testing.T) {
	t.Run("returns path containing app name and secrets dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path, err := SecretsDir()
		require.NoError(t, err)
		assert.Contains(t, path, AppName)
		assert.Contains(t, path, SecretsDirName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")

		// Verify directory was created
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})

	t.Run("respects XDG_DATA_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path, err := SecretsDir()
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(path, tmpDir), "path should start with XDG_DATA_HOME")
		assert.Equal(t, filepath.Join(tmpDir, AppName, SecretsDirName), path)
	})
}

func TestSecretsDirPath(t *testing.T) {
	t.Run("returns path without creating directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := SecretsDirPath()
		assert.Equal(t, filepath.Join(tmpDir, AppName, SecretsDirName), path)

		// Directory should NOT exist (not created)
		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err))
	})
}

func TestDataDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := DataDir()
		assert.Contains(t, path, AppName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_DATA_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := DataDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName), path)
	})
}

func TestCacheDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := CacheDir()
		assert.Contains(t, path, AppName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_CACHE_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := CacheDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName), path)
	})
}

func TestHTTPCacheDir(t *testing.T) {
	t.Run("returns path containing app name and http-cache", func(t *testing.T) {
		path := HTTPCacheDir()
		assert.Contains(t, path, AppName)
		assert.Contains(t, path, HTTPCacheDirName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_CACHE_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := HTTPCacheDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName, HTTPCacheDirName), path)
	})
}

func TestCatalogDir(t *testing.T) {
	t.Run("returns path containing app name and catalog", func(t *testing.T) {
		path := CatalogDir()
		assert.Contains(t, path, AppName)
		assert.Contains(t, path, CatalogDirName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_DATA_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := CatalogDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName, CatalogDirName), path)
	})
}

func TestStateDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := StateDir()
		assert.Contains(t, path, AppName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_STATE_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_STATE_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := StateDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName), path)
	})
}

func TestRuntimeDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := RuntimeDir()
		assert.Contains(t, path, AppName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_RUNTIME_DIR", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_RUNTIME_DIR", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := RuntimeDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName), path)
	})
}

func TestPlatformDefaults(t *testing.T) {
	// Clear all XDG env vars to test platform defaults
	envVars := []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"}
	for _, env := range envVars {
		t.Setenv(env, "")
	}
	xdg.Reload()
	defer xdg.Reload()

	t.Run("config uses platform-appropriate path", func(t *testing.T) {
		path := ConfigDir()
		switch runtime.GOOS {
		case "darwin":
			assert.Contains(t, path, "Library/Application Support")
		case "linux":
			assert.Contains(t, path, ".config")
		case "windows":
			// Windows uses LOCALAPPDATA
			assert.Contains(t, path, AppName)
		}
	})

	t.Run("cache uses platform-appropriate path", func(t *testing.T) {
		path := CacheDir()
		switch runtime.GOOS {
		case "darwin":
			assert.Contains(t, path, "Library/Caches")
		case "linux":
			assert.Contains(t, path, ".cache")
		case "windows":
			assert.Contains(t, path, AppName)
		}
	})
}
