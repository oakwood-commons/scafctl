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
		assert.Contains(t, path, AppName())
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
		assert.Equal(t, filepath.Join(tmpDir, AppName(), ConfigFileName), path)

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
		t.Setenv("XDG_CONFIG_DIRS", tmpDir)
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
		configDir := filepath.Join(tmpDir, AppName())
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

	t.Run("skips directory named like the config file", func(t *testing.T) {
		homeDir, sysDir := t.TempDir(), t.TempDir()
		t.Setenv(EnvConfigHome, homeDir)
		t.Setenv(EnvConfigDirs, sysDir)

		require.NoError(t, os.MkdirAll(filepath.Join(homeDir, AppName(), ConfigFileName), 0o700))

		_, err := SearchConfigFile()
		require.Error(t, err, "a directory must not be returned as the config file")

		sysConfig := filepath.Join(sysDir, AppName(), ConfigFileName)
		require.NoError(t, os.MkdirAll(filepath.Dir(sysConfig), 0o700))
		require.NoError(t, os.WriteFile(sysConfig, []byte("a: b"), 0o600))

		found, err := SearchConfigFile()
		require.NoError(t, err)
		assert.Equal(t, sysConfig, found, "search continues past the directory to XDG_CONFIG_DIRS")
	})
}

func TestConfigDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := ConfigDir()
		assert.Contains(t, path, AppName())
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_CONFIG_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := ConfigDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName()), path)
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
		assert.Contains(t, path, AppName())
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
		assert.Equal(t, filepath.Join(tmpDir, AppName(), SecretsDirName), path)
	})
}

func TestSecretsDirPath(t *testing.T) {
	t.Run("returns path without creating directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := SecretsDirPath()
		assert.Equal(t, filepath.Join(tmpDir, AppName(), SecretsDirName), path)

		// Directory should NOT exist (not created)
		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err))
	})
}

func TestDataDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := DataDir()
		assert.Contains(t, path, AppName())
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_DATA_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := DataDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName()), path)
	})
}

func TestCacheDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := CacheDir()
		assert.Contains(t, path, AppName())
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_CACHE_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := CacheDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName()), path)
	})
}

func TestHTTPCacheDir(t *testing.T) {
	t.Run("returns path containing app name and http-cache", func(t *testing.T) {
		path := HTTPCacheDir()
		assert.Contains(t, path, AppName())
		assert.Contains(t, path, HTTPCacheDirName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_CACHE_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_CACHE_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := HTTPCacheDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName(), HTTPCacheDirName), path)
	})
}

func TestCatalogDir(t *testing.T) {
	t.Run("returns path containing app name and catalog", func(t *testing.T) {
		path := CatalogDir()
		assert.Contains(t, path, AppName())
		assert.Contains(t, path, CatalogDirName)
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_DATA_HOME", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := CatalogDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName(), CatalogDirName), path)
	})
}

// TestEnvOverrideWithoutReload guards against resolving to the user's real
// directories when a test (or caller) changes XDG_* without calling
// xdg.Reload(). adrg/xdg caches values at init, so the env must win.
func TestEnvOverrideWithoutReload(t *testing.T) {
	tests := []struct {
		name string
		env  string
		got  func() string
		want func(dir string) string
	}{
		{"CatalogDir", EnvDataHome, CatalogDir, func(d string) string { return filepath.Join(d, AppName(), CatalogDirName) }},
		{"DataDir", EnvDataHome, DataDir, func(d string) string { return filepath.Join(d, AppName()) }},
		{"SecretsDirPath", EnvDataHome, SecretsDirPath, func(d string) string { return filepath.Join(d, AppName(), SecretsDirName) }},
		{"CacheDir", EnvCacheHome, CacheDir, func(d string) string { return filepath.Join(d, AppName()) }},
		{"PluginCacheDir", EnvCacheHome, PluginCacheDir, func(d string) string { return filepath.Join(d, AppName(), PluginCacheDirName) }},
		{"ConfigDir", EnvConfigHome, ConfigDir, func(d string) string { return filepath.Join(d, AppName()) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv(tt.env, dir)
			assert.Equal(t, tt.want(dir), tt.got())
		})
	}
}

func TestEnvOverrideWithoutReload_CreatingFuncs(t *testing.T) {
	cfgDir := t.TempDir()
	dataDir := t.TempDir()
	t.Setenv(EnvConfigHome, cfgDir)
	t.Setenv(EnvConfigDirs, t.TempDir())
	t.Setenv(EnvDataHome, dataDir)

	cfg, err := ConfigFile()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(cfgDir, AppName(), ConfigFileName), cfg)
	assert.DirExists(t, filepath.Dir(cfg))

	secrets, err := SecretsDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dataDir, AppName(), SecretsDirName), secrets)
	assert.DirExists(t, secrets)

	_, err = SearchConfigFile()
	require.Error(t, err, "config file was not written, search must not fall back to the real home")

	require.NoError(t, os.WriteFile(cfg, []byte("a: b"), 0o600))
	found, err := SearchConfigFile()
	require.NoError(t, err)
	assert.Equal(t, cfg, found)
}

func TestEnvDirOr(t *testing.T) {
	const name = "SCAFCTL_TEST_ENV_DIR_OR"
	abs := t.TempDir()

	t.Setenv(name, "")
	assert.Equal(t, "fallback", envDirOr(name, "fallback"), "empty value uses fallback")

	t.Setenv(name, "relative/path")
	assert.Equal(t, "fallback", envDirOr(name, "fallback"), "relative value is ignored")

	t.Setenv(name, abs)
	assert.Equal(t, abs, envDirOr(name, "fallback"))
}

func TestConfigDirs(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()

	t.Setenv(EnvConfigDirs, strings.Join([]string{a, "relative", b}, string(os.PathListSeparator)))
	assert.Equal(t, []string{a, b}, configDirs(), "relative entries are dropped")

	t.Setenv(EnvConfigDirs, "")
	assert.Equal(t, xdg.ConfigDirs, configDirs(), "unset falls back to xdg")
}

func TestCreateFilePath_Error(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))

	_, err := createFilePath(blocker, filepath.Join("sub", "x.yaml"))
	require.Error(t, err)
}

func TestRuntimeDir(t *testing.T) {
	t.Run("returns path containing app name", func(t *testing.T) {
		path := RuntimeDir()
		assert.Contains(t, path, AppName())
		assert.True(t, filepath.IsAbs(path), "path should be absolute")
	})

	t.Run("respects XDG_RUNTIME_DIR", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("XDG_RUNTIME_DIR", tmpDir)
		xdg.Reload()
		defer xdg.Reload()

		path := RuntimeDir()
		assert.Equal(t, filepath.Join(tmpDir, AppName()), path)
	})
}

func TestPlatformDefaults(t *testing.T) {
	// Clear all XDG env vars to test platform defaults
	envVars := []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR"}
	for _, env := range envVars {
		t.Setenv(env, "")
	}
	xdg.Reload()
	applyDefaults() // Reapply CLI conventions after xdg.Reload() resets to library defaults
	defer xdg.Reload()

	t.Run("config uses platform-appropriate path", func(t *testing.T) {
		path := ConfigDir()
		switch runtime.GOOS {
		case "darwin":
			assert.Contains(t, path, ".config")
		case "linux":
			assert.Contains(t, path, ".config")
		case "windows":
			// Windows uses LOCALAPPDATA
			assert.Contains(t, path, AppName())
		}
	})

	t.Run("cache uses platform-appropriate path", func(t *testing.T) {
		path := CacheDir()
		switch runtime.GOOS {
		case "darwin":
			assert.Contains(t, path, ".cache")
		case "linux":
			assert.Contains(t, path, ".cache")
		case "windows":
			assert.Contains(t, path, AppName())
		}
	})
}

func TestHomeDir(t *testing.T) {
	t.Run("returns non-empty absolute path", func(t *testing.T) {
		home, err := HomeDir()
		require.NoError(t, err)
		assert.NotEmpty(t, home)
		assert.True(t, filepath.IsAbs(home), "home dir should be absolute")
	})

	t.Run("matches os.UserHomeDir", func(t *testing.T) {
		expected, err := os.UserHomeDir()
		require.NoError(t, err)
		got, err := HomeDir()
		require.NoError(t, err)
		assert.Equal(t, expected, got)
	})
}

func TestExpandHome(t *testing.T) {
	t.Run("expands tilde to home directory", func(t *testing.T) {
		home, err := HomeDir()
		require.NoError(t, err)

		expanded, err := ExpandHome("~/some/path")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(home, "some/path"), expanded)
	})

	t.Run("returns unchanged path without tilde", func(t *testing.T) {
		expanded, err := ExpandHome("/absolute/path")
		require.NoError(t, err)
		assert.Equal(t, "/absolute/path", expanded)
	})

	t.Run("returns unchanged empty string", func(t *testing.T) {
		expanded, err := ExpandHome("")
		require.NoError(t, err)
		assert.Equal(t, "", expanded)
	})

	t.Run("expands bare tilde to home directory", func(t *testing.T) {
		home, err := HomeDir()
		require.NoError(t, err)

		expanded, err := ExpandHome("~")
		require.NoError(t, err)
		assert.Equal(t, home, expanded)
	})

	t.Run("returns relative path unchanged", func(t *testing.T) {
		expanded, err := ExpandHome("relative/path")
		require.NoError(t, err)
		assert.Equal(t, "relative/path", expanded)
	})
}

func BenchmarkHomeDir(b *testing.B) {
	for b.Loop() {
		_, _ = HomeDir()
	}
}

func BenchmarkExpandHome(b *testing.B) {
	for b.Loop() {
		_, _ = ExpandHome("~/some/path")
	}
}

func TestAllPaths(t *testing.T) {
	paths := AllPaths()
	assert.NotEmpty(t, paths)
	// Should include at minimum Config, Secrets, Data, Catalog, Cache, HTTP Cache, State
	assert.GreaterOrEqual(t, len(paths), 5)
	for _, p := range paths {
		assert.NotEmpty(t, p.Name)
		assert.NotEmpty(t, p.Description)
	}
}

func TestIllustrativePaths_Linux(t *testing.T) {
	paths := IllustrativePaths("linux")
	assert.NotEmpty(t, paths)
	for _, p := range paths {
		assert.NotEmpty(t, p.Path)
	}
}

func TestIllustrativePaths_Darwin(t *testing.T) {
	paths := IllustrativePaths("darwin")
	assert.NotEmpty(t, paths)
}

func TestIllustrativePaths_Windows(t *testing.T) {
	paths := IllustrativePaths("windows")
	assert.NotEmpty(t, paths)
}

func TestIllustrativePaths_Macos(t *testing.T) {
	paths := IllustrativePaths("macos")
	assert.NotEmpty(t, paths)
}

func TestIllustrativePaths_Unknown(t *testing.T) {
	paths := IllustrativePaths("unknown-os")
	assert.NotEmpty(t, paths)
}

func TestBuildCacheDir(t *testing.T) {
	dir := BuildCacheDir()
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, BuildCacheDirName)
}

func TestPluginCacheDir(t *testing.T) {
	dir := PluginCacheDir()
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, PluginCacheDirName)
}

func TestArtifactCacheDir(t *testing.T) {
	dir := ArtifactCacheDir()
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, ArtifactCacheDirName)
}

func TestProviderSchemaCacheDir(t *testing.T) {
	dir := ProviderSchemaCacheDir()
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, ProviderSchemaCacheDirName)
}
