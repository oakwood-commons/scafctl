// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package paths

import (
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

// init overrides the adrg/xdg library's macOS defaults to follow CLI tool
// conventions instead of GUI application conventions.
//
// The adrg/xdg library defaults macOS to ~/Library/Application Support and
// ~/Library/Caches, which is appropriate for GUI applications but not for
// CLI tools. Most CLI tools (gh, git, packer, stripe, kubectl, docker,
// terraform) use ~/.config, ~/.local/share, ~/.cache, and ~/.local/state
// on macOS.
//
// This override only applies when the corresponding XDG environment variable
// is not set, so user overrides are always respected.
//
// We set the environment variables rather than just the exported xdg package
// variables because xdg.DataFile()/xdg.ConfigFile() use internal state that
// is only updated via xdg.Reload().
//
// See: https://atmos.tools/changelog/macos-xdg-cli-conventions
//
//nolint:gochecknoinits // init is required to override xdg defaults before any path function is called.
func init() {
	applyDefaults()
}

// applyDefaults sets XDG environment variables to CLI tool conventions on
// macOS when they are not already set, then reloads the xdg library so both
// exported variables (xdg.DataHome) and internal state (used by xdg.DataFile)
// reflect the overrides.
//
// This is called from init() and can be called again after xdg.Reload()
// to reapply the overrides.
func applyDefaults() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}

	changed := false

	if os.Getenv("XDG_CONFIG_HOME") == "" {
		os.Setenv("XDG_CONFIG_HOME", filepath.Join(homeDir, ".config"))
		changed = true
	}

	if os.Getenv("XDG_DATA_HOME") == "" {
		os.Setenv("XDG_DATA_HOME", filepath.Join(homeDir, ".local", "share"))
		changed = true
	}

	if os.Getenv("XDG_CACHE_HOME") == "" {
		os.Setenv("XDG_CACHE_HOME", filepath.Join(homeDir, ".cache"))
		changed = true
	}

	if os.Getenv("XDG_STATE_HOME") == "" {
		os.Setenv("XDG_STATE_HOME", filepath.Join(homeDir, ".local", "state"))
		changed = true
	}

	if changed {
		xdg.Reload()
	}
}
