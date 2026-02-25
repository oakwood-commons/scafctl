// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package paths

import (
	"fmt"
	"strings"
)

// SupportedPlatforms lists the platforms accepted by IllustrativePaths.
var SupportedPlatforms = []string{"linux", "darwin", "macos", "windows"}

// PathInfo represents information about a path used by scafctl.
type PathInfo struct {
	Name        string `json:"name" yaml:"name"`
	Path        string `json:"path" yaml:"path"`
	Description string `json:"description" yaml:"description"`
	XDGVariable string `json:"xdgVariable,omitempty" yaml:"xdgVariable,omitempty"`
}

// AllPaths returns the actual resolved paths for the current platform.
func AllPaths() []PathInfo {
	configPath, err := ConfigFile()
	if err != nil {
		configPath = fmt.Sprintf("(error: %v)", err)
	}

	secretsPath, err := SecretsDir()
	if err != nil {
		secretsPath = fmt.Sprintf("(error: %v)", err)
	}

	return []PathInfo{
		{
			Name:        "Config",
			Path:        configPath,
			Description: "Configuration file",
			XDGVariable: "XDG_CONFIG_HOME",
		},
		{
			Name:        "Secrets",
			Path:        secretsPath,
			Description: "Encrypted secrets storage",
			XDGVariable: "XDG_DATA_HOME",
		},
		{
			Name:        "Data",
			Path:        DataDir(),
			Description: "User data directory",
			XDGVariable: "XDG_DATA_HOME",
		},
		{
			Name:        "Catalog",
			Path:        CatalogDir(),
			Description: "Default local catalog",
			XDGVariable: "XDG_DATA_HOME",
		},
		{
			Name:        "Cache",
			Path:        CacheDir(),
			Description: "Cache directory",
			XDGVariable: "XDG_CACHE_HOME",
		},
		{
			Name:        "HTTP Cache",
			Path:        HTTPCacheDir(),
			Description: "HTTP response cache",
			XDGVariable: "XDG_CACHE_HOME",
		},
		{
			Name:        "State",
			Path:        StateDir(),
			Description: "State data (logs, history)",
			XDGVariable: "XDG_STATE_HOME",
		},
	}
}

// IllustrativePaths returns illustrative default paths for a given platform.
// These are the XDG defaults when no environment variables are set.
func IllustrativePaths(platform string) []PathInfo {
	var configHome, dataHome, cacheHome, stateHome string

	switch platform {
	case "linux":
		configHome = "~/.config"
		dataHome = "~/.local/share"
		cacheHome = "~/.cache"
		stateHome = "~/.local/state"
	case "darwin":
		configHome = "~/.config"
		dataHome = "~/.local/share"
		cacheHome = "~/.cache"
		stateHome = "~/.local/state"
	case "windows":
		configHome = "%LOCALAPPDATA%"
		dataHome = "%LOCALAPPDATA%"
		cacheHome = "%LOCALAPPDATA%\\cache"
		stateHome = "%LOCALAPPDATA%"
	default:
		// Fallback to Linux-style paths
		configHome = "~/.config"
		dataHome = "~/.local/share"
		cacheHome = "~/.cache"
		stateHome = "~/.local/state"
	}

	// Use appropriate path separator
	sep := "/"
	if platform == "windows" {
		sep = "\\"
	}

	join := func(parts ...string) string {
		return strings.Join(parts, sep)
	}

	return []PathInfo{
		{
			Name:        "Config",
			Path:        join(configHome, "scafctl", "config.yaml"),
			Description: "Configuration file",
			XDGVariable: "XDG_CONFIG_HOME",
		},
		{
			Name:        "Secrets",
			Path:        join(dataHome, "scafctl", "secrets"),
			Description: "Encrypted secrets storage",
			XDGVariable: "XDG_DATA_HOME",
		},
		{
			Name:        "Data",
			Path:        join(dataHome, "scafctl"),
			Description: "User data directory",
			XDGVariable: "XDG_DATA_HOME",
		},
		{
			Name:        "Catalog",
			Path:        join(dataHome, "scafctl", "catalog"),
			Description: "Default local catalog",
			XDGVariable: "XDG_DATA_HOME",
		},
		{
			Name:        "Cache",
			Path:        join(cacheHome, "scafctl"),
			Description: "Cache directory",
			XDGVariable: "XDG_CACHE_HOME",
		},
		{
			Name:        "HTTP Cache",
			Path:        join(cacheHome, "scafctl", "http-cache"),
			Description: "HTTP response cache",
			XDGVariable: "XDG_CACHE_HOME",
		},
		{
			Name:        "State",
			Path:        join(stateHome, "scafctl"),
			Description: "State data (logs, history)",
			XDGVariable: "XDG_STATE_HOME",
		},
	}
}
