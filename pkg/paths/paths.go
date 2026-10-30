// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/adrg/xdg"
)

// appName is the application name used in XDG paths.
// Defaults to "scafctl" but can be overridden by embedders via SetAppName.
var (
	appName     = "scafctl"
	appNameOnce sync.Once
)

// AppName returns the current application name used in XDG paths.
func AppName() string {
	return appName
}

// SetAppName overrides the application name used in all XDG path functions.
// It must be called once, before any path functions are used (typically during
// CLI initialization). Subsequent calls are no-ops. An empty name is ignored.
func SetAppName(name string) {
	if name == "" {
		return
	}
	appNameOnce.Do(func() {
		appName = name
	})
}

const (
	// ConfigFileName is the default config file name.
	ConfigFileName = "config.yaml"

	// SecretsDirName is the name of the secrets subdirectory.
	SecretsDirName = "secrets"

	// HTTPCacheDirName is the name of the HTTP cache subdirectory.
	HTTPCacheDirName = "http-cache"

	// CatalogDirName is the name of the catalog subdirectory.
	CatalogDirName = "catalog"

	// BuildCacheDirName is the name of the build cache subdirectory.
	BuildCacheDirName = "build-cache"

	// PluginCacheDirName is the name of the plugin cache subdirectory.
	PluginCacheDirName = "plugins"

	// ArtifactCacheDirName is the name of the artifact cache subdirectory.
	ArtifactCacheDirName = "artifact"

	// ProviderSchemaCacheDirName is the name of the provider schema cache subdirectory.
	ProviderSchemaCacheDirName = "provider-schemas"
)

// XDG base directory environment variable names.
const (
	EnvConfigHome = "XDG_CONFIG_HOME"
	EnvConfigDirs = "XDG_CONFIG_DIRS"
	EnvDataHome   = "XDG_DATA_HOME"
	EnvCacheHome  = "XDG_CACHE_HOME"
)

// baseDirPerm matches the permissions adrg/xdg uses for directories it creates.
const baseDirPerm = os.ModeDir | 0o700

// ConfigHome returns the XDG config home directory.
//
// The environment variable is read on every call so that overrides made after
// process start (e.g. t.Setenv in tests) take effect. adrg/xdg caches its
// values at init and only refreshes them on xdg.Reload(), so relying on it
// alone silently resolves to the user's real directories.
func ConfigHome() string {
	return envDirOr(EnvConfigHome, xdg.ConfigHome)
}

// DataHome returns the XDG data home directory. See ConfigHome for why the
// environment is consulted on every call.
func DataHome() string {
	return envDirOr(EnvDataHome, xdg.DataHome)
}

// CacheHome returns the XDG cache home directory. See ConfigHome for why the
// environment is consulted on every call.
func CacheHome() string {
	return envDirOr(EnvCacheHome, xdg.CacheHome)
}

// envDirOr returns the value of the named environment variable when it is an
// absolute path (relative values are invalid per the XDG spec and ignored, as
// adrg/xdg does), otherwise fallback.
func envDirOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" && filepath.IsAbs(v) {
		return v
	}
	return fallback
}

// configDirs returns the XDG config search directories, reading
// XDG_CONFIG_DIRS on every call (see ConfigHome). Relative entries are ignored.
func configDirs() []string {
	var dirs []string
	for _, d := range filepath.SplitList(os.Getenv(EnvConfigDirs)) {
		if filepath.IsAbs(d) {
			dirs = append(dirs, d)
		}
	}
	if len(dirs) > 0 {
		return dirs
	}
	return xdg.ConfigDirs
}

// createFilePath joins base and relPath and ensures the parent directory exists.
func createFilePath(base, relPath string) (string, error) {
	p := filepath.Join(base, relPath)
	if err := os.MkdirAll(filepath.Dir(p), baseDirPerm); err != nil {
		return "", fmt.Errorf("create directory for %s: %w", p, err)
	}
	return p, nil
}

// ConfigFile returns the path to the config file.
// Creates parent directories if they don't exist.
//
// Returns: $XDG_CONFIG_HOME/scafctl/config.yaml
//
// Platform defaults:
//   - Linux: ~/.config/scafctl/config.yaml
//   - macOS: ~/.config/scafctl/config.yaml
//   - Windows: %LOCALAPPDATA%\scafctl\config.yaml
func ConfigFile() (string, error) {
	return createFilePath(ConfigHome(), filepath.Join(appName, ConfigFileName))
}

// SearchConfigFile searches for the config file in XDG config paths.
// Does not create any directories.
//
// Search order:
//  1. $XDG_CONFIG_HOME/scafctl/config.yaml
//  2. $XDG_CONFIG_DIRS/scafctl/config.yaml (each directory in order)
func SearchConfigFile() (string, error) {
	relPath := filepath.Join(appName, ConfigFileName)
	dirs := append([]string{ConfigHome()}, configDirs()...)
	for _, dir := range dirs {
		p := filepath.Join(dir, relPath)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("could not locate %s in any of the following paths: %v", relPath, dirs)
}

// ConfigDir returns the path to the config directory.
//
// Returns: $XDG_CONFIG_HOME/scafctl/
func ConfigDir() string {
	return filepath.Join(ConfigHome(), appName)
}

// SecretsDir returns the path to the secrets directory.
// Creates parent directories if they don't exist.
//
// Returns: $XDG_DATA_HOME/scafctl/secrets/
//
// Platform defaults:
//   - Linux: ~/.local/share/scafctl/secrets/
//   - macOS: ~/.local/share/scafctl/secrets/
//   - Windows: %LOCALAPPDATA%\scafctl\secrets\
//
// Note: Secrets are stored in DATA_HOME (not CONFIG_HOME) because they are
// user-specific data that should persist, not configuration settings.
func SecretsDir() (string, error) {
	dir := SecretsDirPath()
	if err := os.MkdirAll(dir, baseDirPerm); err != nil {
		return "", fmt.Errorf("create secrets directory %s: %w", dir, err)
	}
	return dir, nil
}

// SecretsDirPath returns the secrets directory path without creating it.
//
// Returns: $XDG_DATA_HOME/scafctl/secrets/
func SecretsDirPath() string {
	return filepath.Join(DataHome(), appName, SecretsDirName)
}

// DataDir returns the path to the data directory.
//
// Returns: $XDG_DATA_HOME/scafctl/
func DataDir() string {
	return filepath.Join(DataHome(), appName)
}

// CacheDir returns the path to the cache directory.
//
// Returns: $XDG_CACHE_HOME/scafctl/
func CacheDir() string {
	return filepath.Join(CacheHome(), appName)
}

// HTTPCacheDir returns the path to the HTTP cache directory.
//
// Returns: $XDG_CACHE_HOME/scafctl/http-cache/
//
// Platform defaults:
//   - Linux: ~/.cache/scafctl/http-cache/
//   - macOS: ~/.cache/scafctl/http-cache/
//   - Windows: %LOCALAPPDATA%\cache\scafctl\http-cache\
func HTTPCacheDir() string {
	return filepath.Join(CacheHome(), appName, HTTPCacheDirName)
}

// CatalogDir returns the default path to the local catalog directory.
//
// Returns: $XDG_DATA_HOME/scafctl/catalog/
//
// Platform defaults:
//   - Linux: ~/.local/share/scafctl/catalog/
//   - macOS: ~/.local/share/scafctl/catalog/
//   - Windows: %LOCALAPPDATA%\scafctl\catalog\
func CatalogDir() string {
	return filepath.Join(DataHome(), appName, CatalogDirName)
}

// BuildCacheDir returns the default path to the build cache directory.
//
// Returns: $XDG_CACHE_HOME/scafctl/build-cache/
//
// Platform defaults:
//   - Linux: ~/.cache/scafctl/build-cache/
//   - macOS: ~/.cache/scafctl/build-cache/
//   - Windows: %LOCALAPPDATA%\cache\scafctl\build-cache\
func BuildCacheDir() string {
	return filepath.Join(CacheHome(), appName, BuildCacheDirName)
}

// PluginCacheDir returns the default path to the plugin cache directory.
//
// Returns: $XDG_CACHE_HOME/scafctl/plugins/
//
// Platform defaults:
//   - Linux: ~/.cache/scafctl/plugins/
//   - macOS: ~/.cache/scafctl/plugins/
//   - Windows: %LOCALAPPDATA%\cache\scafctl\plugins\
func PluginCacheDir() string {
	return filepath.Join(CacheHome(), appName, PluginCacheDirName)
}

// ArtifactCacheDir returns the default path to the artifact cache directory.
// Used for caching downloaded catalog artifacts (solutions, providers, auth-handlers)
// with configurable TTL-based expiration.
//
// Returns: $XDG_CACHE_HOME/scafctl/artifact/
//
// Platform defaults:
//   - Linux: ~/.cache/scafctl/artifact/
//   - macOS: ~/.cache/scafctl/artifact/
//   - Windows: %LOCALAPPDATA%\cache\scafctl\artifact\
func ArtifactCacheDir() string {
	return filepath.Join(CacheHome(), appName, ArtifactCacheDirName)
}

// ProviderSchemaCacheDir returns the path to the provider schema cache directory.
// Used for caching provider descriptor JSON so schemas are available without
// spawning plugin processes.
//
// Returns: $XDG_CACHE_HOME/scafctl/provider-schemas/
//
// Platform defaults:
//   - Linux: ~/.cache/scafctl/provider-schemas/
//   - macOS: ~/.cache/scafctl/provider-schemas/
//   - Windows: %LOCALAPPDATA%\cache\scafctl\provider-schemas\
func ProviderSchemaCacheDir() string {
	return filepath.Join(CacheHome(), appName, ProviderSchemaCacheDirName)
}

// RuntimeDir returns the path to the runtime directory.
// Used for sockets, pipes, and other runtime files.
//
// Returns: $XDG_RUNTIME_DIR/scafctl/
func RuntimeDir() string {
	return filepath.Join(xdg.RuntimeDir, appName)
}

// HomeDir returns the user's home directory path.
// This centralizes home directory resolution so callers outside pkg/paths
// do not call os.UserHomeDir directly.
func HomeDir() (string, error) {
	return os.UserHomeDir()
}

// ExpandHome expands a leading ~ in the given path to the user's home directory.
// If the path does not start with ~, it is returned unchanged.
func ExpandHome(path string) (string, error) {
	if len(path) == 0 || path[0] != '~' {
		return path, nil
	}
	home, err := HomeDir()
	if err != nil {
		return "", fmt.Errorf("expand home directory: %w", err)
	}
	return filepath.Join(home, path[1:]), nil
}
