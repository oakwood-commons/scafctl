// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package paths provides centralized path resolution for scafctl using the
// XDG Base Directory Specification (https://specifications.freedesktop.org/basedir/latest/).
//
// This package uses the github.com/adrg/xdg library to provide cross-platform
// XDG-compliant paths for configuration, data, cache, and state files.
//
// # Directory Types
//
// The XDG specification defines several directory types:
//
//   - Config: User-specific configuration files (XDG_CONFIG_HOME)
//   - Data: User-specific data files (XDG_DATA_HOME)
//   - Cache: User-specific non-essential cached data (XDG_CACHE_HOME)
//   - State: User-specific state data like logs and history (XDG_STATE_HOME)
//
// # Platform Defaults
//
// When XDG environment variables are not set, platform-specific defaults are used:
//
// Linux:
//   - Config: ~/.config/scafctl/
//   - Data: ~/.local/share/scafctl/
//   - Cache: ~/.cache/scafctl/
//   - State: ~/.local/state/scafctl/
//
// macOS:
//   - Config: ~/.config/scafctl/
//   - Data: ~/.local/share/scafctl/
//   - Cache: ~/.cache/scafctl/
//   - State: ~/.local/state/scafctl/
//
// Windows:
//   - Config: %LOCALAPPDATA%\scafctl\
//   - Data: %LOCALAPPDATA%\scafctl\
//   - Cache: %LOCALAPPDATA%\cache\scafctl\
//   - State: %LOCALAPPDATA%\scafctl\
//
// # Environment Variable Overrides
//
// All XDG environment variables are respected:
//   - XDG_CONFIG_HOME
//   - XDG_DATA_HOME
//   - XDG_CACHE_HOME
//   - XDG_STATE_HOME
//
// Additionally, SCAFCTL_SECRETS_DIR can override the secrets directory location.
package paths
