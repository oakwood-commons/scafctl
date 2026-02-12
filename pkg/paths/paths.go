// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package paths

import (
	"path/filepath"

	"github.com/adrg/xdg"
)

const (
	// AppName is the application name used in XDG paths.
	AppName = "scafctl"

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
)

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
	return xdg.ConfigFile(filepath.Join(AppName, ConfigFileName))
}

// SearchConfigFile searches for the config file in XDG config paths.
// Does not create any directories.
//
// Search order:
//  1. $XDG_CONFIG_HOME/scafctl/config.yaml
//  2. $XDG_CONFIG_DIRS/scafctl/config.yaml (each directory in order)
func SearchConfigFile() (string, error) {
	return xdg.SearchConfigFile(filepath.Join(AppName, ConfigFileName))
}

// ConfigDir returns the path to the config directory.
//
// Returns: $XDG_CONFIG_HOME/scafctl/
func ConfigDir() string {
	return filepath.Join(xdg.ConfigHome, AppName)
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
	// xdg.DataFile creates parent directories and returns the full path
	// We use a placeholder file path to ensure the directory is created
	path, err := xdg.DataFile(filepath.Join(AppName, SecretsDirName, ".keep"))
	if err != nil {
		return "", err
	}
	// Return the directory, not the file path
	return filepath.Dir(path), nil
}

// SecretsDirPath returns the secrets directory path without creating it.
//
// Returns: $XDG_DATA_HOME/scafctl/secrets/
func SecretsDirPath() string {
	return filepath.Join(xdg.DataHome, AppName, SecretsDirName)
}

// DataDir returns the path to the data directory.
//
// Returns: $XDG_DATA_HOME/scafctl/
func DataDir() string {
	return filepath.Join(xdg.DataHome, AppName)
}

// CacheDir returns the path to the cache directory.
//
// Returns: $XDG_CACHE_HOME/scafctl/
func CacheDir() string {
	return filepath.Join(xdg.CacheHome, AppName)
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
	return filepath.Join(xdg.CacheHome, AppName, HTTPCacheDirName)
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
	return filepath.Join(xdg.DataHome, AppName, CatalogDirName)
}

// StateDir returns the path to the state directory.
// Used for logs, history, and session state.
//
// Returns: $XDG_STATE_HOME/scafctl/
//
// Platform defaults:
//   - Linux: ~/.local/state/scafctl/
//   - macOS: ~/.local/state/scafctl/
//   - Windows: %LOCALAPPDATA%\scafctl\
func StateDir() string {
	return filepath.Join(xdg.StateHome, AppName)
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
	return filepath.Join(xdg.CacheHome, AppName, BuildCacheDirName)
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
	return filepath.Join(xdg.CacheHome, AppName, PluginCacheDirName)
}

// RuntimeDir returns the path to the runtime directory.
// Used for sockets, pipes, and other runtime files.
//
// Returns: $XDG_RUNTIME_DIR/scafctl/
func RuntimeDir() string {
	return filepath.Join(xdg.RuntimeDir, AppName)
}
