# XDG Base Directory Specification Implementation Plan

## Overview

This document outlines the plan to migrate scafctl from custom platform-specific paths to the [XDG Base Directory Specification](https://specifications.freedesktop.org/basedir/latest/). The `adrg/xdg` Go library will be used to provide cross-platform XDG compliance.

**Note:** This is a **breaking change**. Existing configurations in old locations will not be migrated—users must manually move or recreate their configuration. Old directories can be deleted.

---

## Current State

### Configuration File

| Item | Current Path |
|------|--------------|
| Config | `~/.scafctl/config.yaml` |

**Code Location:** [pkg/config/config.go](../../pkg/config/config.go)

```go
const (
    DefaultConfigDir = ".scafctl"
)
```

### Secrets Storage

| Platform | Current Path |
|----------|--------------|
| macOS | `~/Library/Application Support/scafctl/secrets/` |
| Linux | `~/.config/scafctl/secrets/` (respects `XDG_CONFIG_HOME`) |
| Windows | `%APPDATA%\scafctl\secrets\` |

**Code Location:** [pkg/secrets/storage.go](../../pkg/secrets/storage.go)

### HTTP Cache

| Item | Current Path |
|------|--------------|
| HTTP Cache | `~/.scafctl/http-cache/` |

**Code Location:** [pkg/settings/settings.go](../../pkg/settings/settings.go)

```go
const (
    DefaultHTTPCacheDir = "~/.scafctl/http-cache"
)
```

### Default Catalog

| Item | Current Path |
|------|--------------|
| Default Catalog | `~/.scafctl/catalog/` |

**Code Location:** Example configs in `examples/config/`

---

## Target State (XDG Compliant)

Using the `adrg/xdg` library, paths will be determined as follows:

### XDG Directory Mapping

| Data Type | XDG Variable | scafctl Subdirectory | Purpose |
|-----------|--------------|---------------------|---------|
| Config | `XDG_CONFIG_HOME` | `scafctl/config.yaml` | User configuration |
| Secrets | `XDG_DATA_HOME` | `scafctl/secrets/` | Encrypted secrets (persistent user data) |
| HTTP Cache | `XDG_CACHE_HOME` | `scafctl/http-cache/` | HTTP response cache (deletable) |
| Catalog (local) | `XDG_DATA_HOME` | `scafctl/catalog/` | Local solution catalog |
| State/Logs | `XDG_STATE_HOME` | `scafctl/` | Logs, history, session state |

### Platform-Specific Default Paths

The `adrg/xdg` library provides platform-appropriate defaults:

#### Unix/Linux

| Variable | Default Path |
|----------|--------------|
| `XDG_CONFIG_HOME` | `~/.config` |
| `XDG_DATA_HOME` | `~/.local/share` |
| `XDG_CACHE_HOME` | `~/.cache` |
| `XDG_STATE_HOME` | `~/.local/state` |

**Resulting paths:**
- Config: `~/.config/scafctl/config.yaml`
- Secrets: `~/.local/share/scafctl/secrets/`
- HTTP Cache: `~/.cache/scafctl/http-cache/`
- Catalog: `~/.local/share/scafctl/catalog/`

#### macOS

| Variable | Default Path |
|----------|--------------|
| `XDG_CONFIG_HOME` | `~/Library/Application Support` |
| `XDG_DATA_HOME` | `~/Library/Application Support` |
| `XDG_CACHE_HOME` | `~/Library/Caches` |
| `XDG_STATE_HOME` | `~/Library/Application Support` |

**Resulting paths:**
- Config: `~/Library/Application Support/scafctl/config.yaml`
- Secrets: `~/Library/Application Support/scafctl/secrets/`
- HTTP Cache: `~/Library/Caches/scafctl/http-cache/`
- Catalog: `~/Library/Application Support/scafctl/catalog/`

#### Windows

| Variable | Default Path |
|----------|--------------|
| `XDG_CONFIG_HOME` | `%LOCALAPPDATA%` |
| `XDG_DATA_HOME` | `%LOCALAPPDATA%` |
| `XDG_CACHE_HOME` | `%LOCALAPPDATA%\cache` |
| `XDG_STATE_HOME` | `%LOCALAPPDATA%` |

**Resulting paths:**
- Config: `%LOCALAPPDATA%\scafctl\config.yaml`
- Secrets: `%LOCALAPPDATA%\scafctl\secrets\`
- HTTP Cache: `%LOCALAPPDATA%\cache\scafctl\http-cache\`
- Catalog: `%LOCALAPPDATA%\scafctl\catalog\`

---

## Breaking Changes

### What Will Break

1. **Configuration file location changes**
   - Old: `~/.scafctl/config.yaml`
   - New: Platform-specific XDG path (see above)
   - **Impact:** Users must manually copy config to new location or recreate it

2. **Secrets location changes (macOS only)**
   - Old: `~/Library/Application Support/scafctl/secrets/`
   - New: `~/Library/Application Support/scafctl/secrets/` (same for macOS via XDG)
   - **Note:** macOS secrets path is unchanged because `adrg/xdg` uses `~/Library/Application Support` for `XDG_DATA_HOME`

3. **Secrets location changes (Linux)**
   - Old: `~/.config/scafctl/secrets/`
   - New: `~/.local/share/scafctl/secrets/`
   - **Impact:** Users must manually move secrets (or re-create them)

4. **Secrets location changes (Windows)**
   - Old: `%APPDATA%\scafctl\secrets\`
   - New: `%LOCALAPPDATA%\scafctl\secrets\`
   - **Impact:** Users must manually move secrets

5. **HTTP cache location changes**
   - Old: `~/.scafctl/http-cache/`
   - New: XDG cache path (see above)
   - **Impact:** Cache will be rebuilt on first use (no user action needed)

6. **Default catalog path in examples/documentation**
   - Old: `~/.scafctl/catalog/`
   - New: XDG data path (see above)
   - **Impact:** Documentation and examples must be updated

### Migration Notes for Users

Users should be informed to:
1. Delete old directories after migration: `rm -rf ~/.scafctl`
2. Re-run `scafctl secrets set` for any stored secrets, OR manually move secrets directory
3. Copy config file to new location, OR use `scafctl config init` to create a fresh config

---

## Implementation Plan

### Phase 1: Add XDG Package Dependency

**Files:** `go.mod`

**Tasks:**
- [ ] Add `github.com/adrg/xdg` dependency
- [ ] Run `go mod tidy`

### Phase 2: Create Centralized Paths Package

**Files:** `pkg/paths/paths.go` (new)

Create a new package to centralize all path resolution using XDG:

```go
package paths

import (
    "path/filepath"

    "github.com/adrg/xdg"
)

const (
    // AppName is the application name used in XDG paths
    AppName = "scafctl"
)

// ConfigFile returns the path to the config file.
// Returns: $XDG_CONFIG_HOME/scafctl/config.yaml
func ConfigFile() (string, error) {
    return xdg.ConfigFile(filepath.Join(AppName, "config.yaml"))
}

// SearchConfigFile searches for config file in XDG config paths.
func SearchConfigFile() (string, error) {
    return xdg.SearchConfigFile(filepath.Join(AppName, "config.yaml"))
}

// SecretsDir returns the path to the secrets directory.
// Returns: $XDG_DATA_HOME/scafctl/secrets/
func SecretsDir() (string, error) {
    return xdg.DataFile(filepath.Join(AppName, "secrets", ".keep"))
}

// CacheDir returns the path to the cache directory.
// Returns: $XDG_CACHE_HOME/scafctl/
func CacheDir() string {
    return filepath.Join(xdg.CacheHome, AppName)
}

// HTTPCacheDir returns the path to the HTTP cache directory.
// Returns: $XDG_CACHE_HOME/scafctl/http-cache/
func HTTPCacheDir() string {
    return filepath.Join(xdg.CacheHome, AppName, "http-cache")
}

// DataDir returns the path to the data directory.
// Returns: $XDG_DATA_HOME/scafctl/
func DataDir() string {
    return filepath.Join(xdg.DataHome, AppName)
}

// CatalogDir returns the default path to the local catalog directory.
// Returns: $XDG_DATA_HOME/scafctl/catalog/
func CatalogDir() string {
    return filepath.Join(xdg.DataHome, AppName, "catalog")
}

// StateDir returns the path to the state directory.
// Returns: $XDG_STATE_HOME/scafctl/
func StateDir() string {
    return filepath.Join(xdg.StateHome, AppName)
}
```

**Tasks:**
- [ ] Create `pkg/paths/paths.go`
- [ ] Create `pkg/paths/paths_test.go` with tests for all functions
- [ ] Add `pkg/paths/doc.go` with package documentation

### Phase 3: Update Config Package

**Files:** `pkg/config/config.go`

**Tasks:**
- [ ] Remove `DefaultConfigDir` constant (`".scafctl"`)
- [ ] Update `NewManager` to use `paths.ConfigFile()` or `paths.SearchConfigFile()`
- [ ] Update `Load()` to use new path resolution
- [ ] Update `Save()` to use new path resolution
- [ ] Update tests in `pkg/config/config_test.go`

**Before:**
```go
const (
    DefaultConfigDir = ".scafctl"
)

// In Load()
configPath = filepath.Join(home, DefaultConfigDir, DefaultConfigFileName+"."+DefaultConfigFileType)
```

**After:**
```go
import "github.com/oakwood-commons/scafctl/pkg/paths"

// In Load()
configPath, err = paths.ConfigFile()
if err != nil {
    return nil, fmt.Errorf("determining config path: %w", err)
}
```

### Phase 4: Update Secrets Package

**Files:** `pkg/secrets/storage.go`, `pkg/secrets/options.go`

**Tasks:**
- [ ] Remove platform-specific path logic from `getSecretsDir()`
- [ ] Use `paths.SecretsDir()` instead
- [ ] Update documentation in `pkg/secrets/README.md`
- [ ] Update tests in `pkg/secrets/storage_test.go`

**Before:**
```go
func getSecretsDir() (string, error) {
    if envDir := os.Getenv(secretsDirEnvVar); envDir != "" {
        return envDir, nil
    }

    switch runtime.GOOS {
    case "darwin":
        home, err := os.UserHomeDir()
        if err != nil {
            return "", fmt.Errorf("getting user home directory: %w", err)
        }
        return filepath.Join(home, "Library", "Application Support", "scafctl", secretsDirName), nil
    case "linux":
        // ...
    case "windows":
        // ...
    }
}
```

**After:**
```go
func getSecretsDir() (string, error) {
    if envDir := os.Getenv(secretsDirEnvVar); envDir != "" {
        return envDir, nil
    }

    return paths.SecretsDir()
}
```

### Phase 5: Update Settings Package (HTTP Cache)

**Files:** `pkg/settings/settings.go`

**Tasks:**
- [ ] Remove hardcoded `DefaultHTTPCacheDir` constant
- [ ] Create a function to get the default HTTP cache dir using `paths.HTTPCacheDir()`
- [ ] Update any code that references `DefaultHTTPCacheDir`

**Before:**
```go
const (
    DefaultHTTPCacheDir = "~/.scafctl/http-cache"
)
```

**After:**
```go
import "github.com/oakwood-commons/scafctl/pkg/paths"

// DefaultHTTPCacheDir returns the default HTTP cache directory.
func DefaultHTTPCacheDir() string {
    return paths.HTTPCacheDir()
}
```

### Phase 6: Update Documentation and Examples

**Files:**
- `examples/config/minimal-config.yaml`
- `examples/config/full-config.yaml`
- `examples/config/README.md`
- `docs/internal/secrets-implementation.md`
- `docs/design/misc.md`
- `pkg/cmd/scafctl/secrets/secrets.go` (help text)
- `pkg/secrets/README.md`
- `pkg/secrets/options.go` (doc comments)

**Tasks:**
- [ ] Update all references to `~/.scafctl/` with XDG-aware paths
- [ ] Add note about XDG compliance in documentation
- [ ] Update CLI help text to reflect new paths
- [ ] Add migration instructions to README or CHANGELOG

### Phase 7: Add Path Info Command (Optional Enhancement)

**Files:** `pkg/cmd/scafctl/config/paths.go` (new)

Add a command to display all resolved paths:

```bash
$ scafctl config paths
Config:     /Users/user/Library/Application Support/scafctl/config.yaml
Secrets:    /Users/user/Library/Application Support/scafctl/secrets/
HTTP Cache: /Users/user/Library/Caches/scafctl/http-cache/
Data:       /Users/user/Library/Application Support/scafctl/
State:      /Users/user/Library/Application Support/scafctl/
Catalog:    /Users/user/Library/Application Support/scafctl/catalog/
```

**Tasks:**
- [ ] Create `scafctl config paths` command
- [ ] Support `--json` output format

### Phase 8: Cleanup and Testing

**Tasks:**
- [ ] Run all existing tests
- [ ] Add integration tests for path resolution
- [ ] Test on all platforms (macOS, Linux, Windows)
- [ ] Verify XDG environment variable overrides work correctly
- [ ] Test with `SCAFCTL_SECRETS_DIR` override (backward compat)

---

## Environment Variable Overrides

The following environment variables will continue to work:

| Variable | Purpose | Priority |
|----------|---------|----------|
| `SCAFCTL_SECRETS_DIR` | Override secrets directory | Highest (existing behavior) |
| `XDG_CONFIG_HOME` | Override config home | Standard XDG |
| `XDG_DATA_HOME` | Override data home | Standard XDG |
| `XDG_CACHE_HOME` | Override cache home | Standard XDG |
| `XDG_STATE_HOME` | Override state home | Standard XDG |

---

## Testing Checklist

- [ ] Unit tests for `pkg/paths/` package
- [ ] Config loading from new XDG path
- [ ] Config saving to new XDG path
- [ ] Secrets storage in new XDG data path
- [ ] HTTP cache in new XDG cache path
- [ ] `SCAFCTL_SECRETS_DIR` override still works
- [ ] XDG environment variable overrides work
- [ ] Integration tests pass
- [ ] Manual testing on macOS
- [ ] Manual testing on Linux
- [ ] Manual testing on Windows

---

## Rollout Communication

Add to CHANGELOG:

```markdown
## BREAKING CHANGES

### XDG Base Directory Compliance

scafctl now follows the XDG Base Directory Specification for all file storage:

| Data Type | Old Path | New Path (Linux) | New Path (macOS) |
|-----------|----------|------------------|------------------|
| Config | `~/.scafctl/config.yaml` | `~/.config/scafctl/config.yaml` | `~/Library/Application Support/scafctl/config.yaml` |
| Secrets | `~/.config/scafctl/secrets/` | `~/.local/share/scafctl/secrets/` | `~/Library/Application Support/scafctl/secrets/` |
| HTTP Cache | `~/.scafctl/http-cache/` | `~/.cache/scafctl/http-cache/` | `~/Library/Caches/scafctl/http-cache/` |

**Migration:**
1. Move your config file to the new location, or let scafctl create a new one
2. Re-create secrets with `scafctl secrets set`, or move the secrets directory
3. Delete old directory: `rm -rf ~/.scafctl`

The HTTP cache will be automatically rebuilt and requires no action.
```

---

## Files to Modify Summary

| File | Action | Description |
|------|--------|-------------|
| `go.mod` | Modify | Add `github.com/adrg/xdg` dependency |
| `pkg/paths/paths.go` | Create | Centralized path resolution |
| `pkg/paths/paths_test.go` | Create | Tests for paths package |
| `pkg/paths/doc.go` | Create | Package documentation |
| `pkg/config/config.go` | Modify | Use paths package for config location |
| `pkg/config/config_test.go` | Modify | Update tests |
| `pkg/secrets/storage.go` | Modify | Use paths package for secrets location |
| `pkg/secrets/storage_test.go` | Modify | Update tests |
| `pkg/secrets/options.go` | Modify | Update doc comments |
| `pkg/secrets/README.md` | Modify | Update documentation |
| `pkg/settings/settings.go` | Modify | Update HTTP cache dir constant |
| `pkg/cmd/scafctl/secrets/secrets.go` | Modify | Update help text |
| `examples/config/*.yaml` | Modify | Update path references |
| `examples/config/README.md` | Modify | Update path references |
| `docs/design/misc.md` | Modify | Update path references |
| `docs/internal/secrets-implementation.md` | Modify | Update path references |

---

## Appendix: adrg/xdg Library Reference

### Key Functions

```go
// Get path for creating a file (creates parent dirs)
xdg.ConfigFile("scafctl/config.yaml")  // Returns full path, creates dirs
xdg.DataFile("scafctl/secrets/.keep")  // Returns full path, creates dirs
xdg.CacheFile("scafctl/cache.db")      // Returns full path, creates dirs
xdg.StateFile("scafctl/state.json")    // Returns full path, creates dirs

// Search for existing file across XDG paths
xdg.SearchConfigFile("scafctl/config.yaml")
xdg.SearchDataFile("scafctl/data.json")

// Direct access to base directories
xdg.ConfigHome  // e.g., ~/.config or ~/Library/Application Support
xdg.DataHome    // e.g., ~/.local/share or ~/Library/Application Support  
xdg.CacheHome   // e.g., ~/.cache or ~/Library/Caches
xdg.StateHome   // e.g., ~/.local/state or ~/Library/Application Support
xdg.RuntimeDir  // e.g., /run/user/1000 or ~/Library/Application Support
```

### Key Properties

- Thread-safe for concurrent use
- Respects XDG environment variables
- Provides sensible platform-specific defaults
- `*File()` functions create parent directories automatically
- `Search*File()` functions search across all relevant XDG paths
