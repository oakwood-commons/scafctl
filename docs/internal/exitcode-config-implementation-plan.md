# Exit Codes and Configuration Implementation Plan

This document outlines the implementation plan for:
1. Centralizing exit codes into a single package
2. Implementing Viper-based application configuration

---

## Table of Contents

1. [Exit Codes Centralization](#exit-codes-centralization)
2. [Viper Configuration Implementation](#viper-configuration-implementation)
3. [Implementation Order](#implementation-order)

---

## Exit Codes Centralization

### Problem

Exit codes are currently duplicated across multiple command files:
- `pkg/cmd/scafctl/run/solution.go`
- `pkg/cmd/scafctl/render/solution.go`

This leads to:
- Potential inconsistencies if codes diverge
- No single source of truth
- Difficulty customizing exit behavior

### Solution

Create a dedicated `pkg/exitcode` package that all commands import.

### Implementation

#### Step 1: Create Exit Code Package

```go
// pkg/exitcode/exitcode.go
package exitcode

// Standard exit codes for CLI commands.
// These follow common Unix conventions where possible.
const (
    // Success indicates successful execution.
    Success = 0

    // GeneralError indicates an unspecified error occurred.
    GeneralError = 1

    // ValidationFailed indicates input validation failed.
    ValidationFailed = 2

    // InvalidInput indicates invalid solution structure (e.g., circular dependency).
    InvalidInput = 3

    // FileNotFound indicates a file was not found or could not be parsed.
    FileNotFound = 4

    // RenderFailed indicates rendering/transformation failed.
    RenderFailed = 5

    // ActionFailed indicates action/workflow execution failed.
    ActionFailed = 6

    // ConfigError indicates a configuration error.
    ConfigError = 7

    // CatalogError indicates a catalog operation failed.
    CatalogError = 8

    // TimeoutError indicates an operation timed out.
    TimeoutError = 9

    // PermissionDenied indicates insufficient permissions.
    PermissionDenied = 10
)

// Description returns a human-readable description of an exit code.
func Description(code int) string {
    switch code {
    case Success:
        return "success"
    case GeneralError:
        return "general error"
    case ValidationFailed:
        return "validation failed"
    case InvalidInput:
        return "invalid input"
    case FileNotFound:
        return "file not found"
    case RenderFailed:
        return "render failed"
    case ActionFailed:
        return "action failed"
    case ConfigError:
        return "configuration error"
    case CatalogError:
        return "catalog error"
    case TimeoutError:
        return "timeout"
    case PermissionDenied:
        return "permission denied"
    default:
        return "unknown error"
    }
}
```

#### Step 2: Create Exit Code Tests

```go
// pkg/exitcode/exitcode_test.go
package exitcode

import (
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestExitCodes(t *testing.T) {
    t.Parallel()

    // Verify exit codes have expected values (document contract)
    assert.Equal(t, 0, Success)
    assert.Equal(t, 1, GeneralError)
    assert.Equal(t, 2, ValidationFailed)
    assert.Equal(t, 3, InvalidInput)
    assert.Equal(t, 4, FileNotFound)
    assert.Equal(t, 5, RenderFailed)
    assert.Equal(t, 6, ActionFailed)
}

func TestDescription(t *testing.T) {
    t.Parallel()

    tests := []struct {
        code     int
        expected string
    }{
        {Success, "success"},
        {GeneralError, "general error"},
        {ValidationFailed, "validation failed"},
        {999, "unknown error"},
    }

    for _, tt := range tests {
        assert.Equal(t, tt.expected, Description(tt.code))
    }
}
```

#### Step 3: Update Commands to Use Central Package

```go
// pkg/cmd/scafctl/run/solution.go
package run

import (
    "github.com/oakwood-commons/scafctl/pkg/exitcode"
    // ... other imports
)

// Remove local exit code constants, use exitcode package instead

func (o *SolutionOptions) Run(ctx context.Context) error {
    // ...
    if err != nil {
        return o.exitWithCode(err, exitcode.FileNotFound)
    }
    // ...
}
```

#### Step 4: Update Tests

Update tests that verify exit code values:

```go
// pkg/cmd/scafctl/run/solution_test.go
import "github.com/oakwood-commons/scafctl/pkg/exitcode"

func TestExitCodes(t *testing.T) {
    // Use central package
    assert.Equal(t, 0, exitcode.Success)
    assert.Equal(t, 2, exitcode.ValidationFailed)
    assert.Equal(t, 4, exitcode.FileNotFound)
}
```

### Files to Modify

| File | Action |
|------|--------|
| `pkg/exitcode/exitcode.go` | **Create** - Central exit codes |
| `pkg/exitcode/exitcode_test.go` | **Create** - Tests |
| `pkg/cmd/scafctl/run/solution.go` | **Modify** - Remove local constants, import `exitcode` |
| `pkg/cmd/scafctl/run/solution_test.go` | **Modify** - Use `exitcode` package |
| `pkg/cmd/scafctl/render/solution.go` | **Modify** - Remove local constants, import `exitcode` |
| `pkg/cmd/scafctl/render/solution_test.go` | **Modify** - Use `exitcode` package |

---

## Viper Configuration Implementation

### Overview

Implement Viper-based configuration to support:
- Configuration file (`~/.scafctl/config.yaml`)
- Environment variable overrides (`SCAFCTL_*`)
- CLI flag overrides
- Catalog management

### Package Structure

```
pkg/
├── config/
│   ├── config.go          # Main configuration logic
│   ├── config_test.go     # Tests
│   ├── types.go           # Configuration types
│   └── defaults.go        # Default values
```

### Implementation

#### Step 1: Add Viper Dependency

```bash
go get github.com/spf13/viper
```

#### Step 2: Create Configuration Types

```go
// pkg/config/types.go
package config

// Config represents the application configuration.
type Config struct {
    Catalogs []CatalogConfig `json:"catalogs" yaml:"catalogs" mapstructure:"catalogs" doc:"Configured catalogs" maxItems:"50"`
    Settings Settings        `json:"settings" yaml:"settings" mapstructure:"settings" doc:"Application settings"`
}

// CatalogConfig represents a single catalog configuration.
type CatalogConfig struct {
    Name     string            `json:"name" yaml:"name" mapstructure:"name" doc:"Catalog name" example:"internal" maxLength:"255"`
    Type     string            `json:"type" yaml:"type" mapstructure:"type" doc:"Catalog type" example:"filesystem" maxLength:"50"`
    Path     string            `json:"path,omitempty" yaml:"path,omitempty" mapstructure:"path" doc:"Path for filesystem catalogs" maxLength:"4096"`
    URL      string            `json:"url,omitempty" yaml:"url,omitempty" mapstructure:"url" doc:"URL for remote catalogs" maxLength:"2048"`
    Auth     *AuthConfig       `json:"auth,omitempty" yaml:"auth,omitempty" mapstructure:"auth" doc:"Authentication configuration"`
    Metadata map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty" mapstructure:"metadata" doc:"Additional metadata"`
}

// AuthConfig holds authentication settings for a catalog.
type AuthConfig struct {
    Type        string `json:"type" yaml:"type" mapstructure:"type" doc:"Auth type" example:"token" maxLength:"50"`
    TokenEnvVar string `json:"tokenEnvVar,omitempty" yaml:"tokenEnvVar,omitempty" mapstructure:"tokenEnvVar" doc:"Environment variable containing token" maxLength:"255"`
}

// Settings holds application-wide settings.
type Settings struct {
    DefaultCatalog string `json:"defaultCatalog" yaml:"defaultCatalog" mapstructure:"defaultCatalog" doc:"Default catalog name" example:"default" maxLength:"255"`
    NoColor        bool   `json:"noColor" yaml:"noColor" mapstructure:"noColor" doc:"Disable colored output"`
    Quiet          bool   `json:"quiet" yaml:"quiet" mapstructure:"quiet" doc:"Suppress non-essential output"`
    LogLevel       int    `json:"logLevel" yaml:"logLevel" mapstructure:"logLevel" doc:"Log level (-1=Debug, 0=Info, 1=Warn, 2=Error)" example:"0" maximum:"3"`
}

// CatalogType constants
const (
    CatalogTypeFilesystem = "filesystem"
    CatalogTypeOCI        = "oci"
    CatalogTypeHTTP       = "http"
)
```

#### Step 3: Create Configuration Manager

```go
// pkg/config/config.go
package config

import (
    "fmt"
    "os"
    "path/filepath"
    "sync"

    "github.com/spf13/viper"
)

const (
    // DefaultConfigFileName is the default config file name.
    DefaultConfigFileName = "config"

    // DefaultConfigFileType is the default config file type.
    DefaultConfigFileType = "yaml"

    // EnvPrefix is the environment variable prefix.
    EnvPrefix = "SCAFCTL"
)

var (
    globalConfig     *Config
    globalConfigOnce sync.Once
    globalConfigErr  error
)

// Manager handles configuration loading and access.
type Manager struct {
    v          *viper.Viper
    configPath string
    config     *Config
}

// NewManager creates a new configuration manager.
func NewManager(configPath string) *Manager {
    v := viper.New()
    v.SetConfigType(DefaultConfigFileType)
    v.SetEnvPrefix(EnvPrefix)
    v.AutomaticEnv()

    return &Manager{
        v:          v,
        configPath: configPath,
    }
}

// Load loads the configuration from file and environment.
func (m *Manager) Load() (*Config, error) {
    // Determine config path
    configPath := m.configPath
    if configPath == "" {
        home, err := os.UserHomeDir()
        if err != nil {
            return nil, fmt.Errorf("failed to get home directory: %w", err)
        }
        configPath = filepath.Join(home, ".scafctl", "config.yaml")
    }

    // Set config file
    m.v.SetConfigFile(configPath)

    // Set defaults
    m.setDefaults()

    // Try to read config file (not an error if it doesn't exist)
    if err := m.v.ReadInConfig(); err != nil {
        if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
            // Only return error if it's not a "file not found" error
            if !os.IsNotExist(err) {
                return nil, fmt.Errorf("failed to read config file: %w", err)
            }
        }
    }

    // Unmarshal into struct
    var cfg Config
    if err := m.v.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }

    m.config = &cfg
    return &cfg, nil
}

// setDefaults sets default configuration values.
func (m *Manager) setDefaults() {
    m.v.SetDefault("settings.defaultCatalog", "")
    m.v.SetDefault("settings.noColor", false)
    m.v.SetDefault("settings.quiet", false)
    m.v.SetDefault("settings.logLevel", 0)
    m.v.SetDefault("catalogs", []CatalogConfig{})
}

// Save saves the current configuration to file.
func (m *Manager) Save() error {
    if m.config == nil {
        return fmt.Errorf("no configuration loaded")
    }

    // Ensure directory exists
    configDir := filepath.Dir(m.v.ConfigFileUsed())
    if err := os.MkdirAll(configDir, 0o700); err != nil {
        return fmt.Errorf("failed to create config directory: %w", err)
    }

    return m.v.WriteConfig()
}

// SaveAs saves the configuration to a specific path.
func (m *Manager) SaveAs(path string) error {
    return m.v.WriteConfigAs(path)
}

// Get returns a configuration value by key.
func (m *Manager) Get(key string) any {
    return m.v.Get(key)
}

// Set sets a configuration value.
func (m *Manager) Set(key string, value any) {
    m.v.Set(key, value)
}

// Config returns the loaded configuration.
func (m *Manager) Config() *Config {
    return m.config
}

// ConfigPath returns the path to the config file.
func (m *Manager) ConfigPath() string {
    return m.v.ConfigFileUsed()
}

// Global returns the global configuration (loads once).
func Global() (*Config, error) {
    globalConfigOnce.Do(func() {
        mgr := NewManager("")
        globalConfig, globalConfigErr = mgr.Load()
    })
    return globalConfig, globalConfigErr
}

// GetCatalog returns a catalog configuration by name.
func (c *Config) GetCatalog(name string) (*CatalogConfig, bool) {
    for i := range c.Catalogs {
        if c.Catalogs[i].Name == name {
            return &c.Catalogs[i], true
        }
    }
    return nil, false
}

// GetDefaultCatalog returns the default catalog configuration.
func (c *Config) GetDefaultCatalog() (*CatalogConfig, bool) {
    if c.Settings.DefaultCatalog == "" {
        return nil, false
    }
    return c.GetCatalog(c.Settings.DefaultCatalog)
}

// AddCatalog adds a new catalog configuration.
func (c *Config) AddCatalog(catalog CatalogConfig) error {
    if _, exists := c.GetCatalog(catalog.Name); exists {
        return fmt.Errorf("catalog %q already exists", catalog.Name)
    }
    c.Catalogs = append(c.Catalogs, catalog)
    return nil
}

// RemoveCatalog removes a catalog by name.
func (c *Config) RemoveCatalog(name string) error {
    for i, cat := range c.Catalogs {
        if cat.Name == name {
            c.Catalogs = append(c.Catalogs[:i], c.Catalogs[i+1:]...)
            return nil
        }
    }
    return fmt.Errorf("catalog %q not found", name)
}
```

#### Step 4: Create Configuration Tests

```go
// pkg/config/config_test.go
package config

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestManager_Load_NoFile(t *testing.T) {
    t.Parallel()

    tmpDir := t.TempDir()
    configPath := filepath.Join(tmpDir, "nonexistent", "config.yaml")

    mgr := NewManager(configPath)
    cfg, err := mgr.Load()

    require.NoError(t, err)
    assert.NotNil(t, cfg)
    assert.Empty(t, cfg.Catalogs)
    assert.Equal(t, 0, cfg.Settings.LogLevel)
}

func TestManager_Load_WithFile(t *testing.T) {
    t.Parallel()

    tmpDir := t.TempDir()
    configPath := filepath.Join(tmpDir, "config.yaml")

    configContent := `
catalogs:
  - name: test
    type: filesystem
    path: ./test
settings:
  defaultCatalog: test
  logLevel: 1
`
    err := os.WriteFile(configPath, []byte(configContent), 0o600)
    require.NoError(t, err)

    mgr := NewManager(configPath)
    cfg, err := mgr.Load()

    require.NoError(t, err)
    assert.Len(t, cfg.Catalogs, 1)
    assert.Equal(t, "test", cfg.Catalogs[0].Name)
    assert.Equal(t, "filesystem", cfg.Catalogs[0].Type)
    assert.Equal(t, "test", cfg.Settings.DefaultCatalog)
    assert.Equal(t, 1, cfg.Settings.LogLevel)
}

func TestConfig_GetCatalog(t *testing.T) {
    t.Parallel()

    cfg := &Config{
        Catalogs: []CatalogConfig{
            {Name: "foo", Type: "filesystem", Path: "./foo"},
            {Name: "bar", Type: "oci", URL: "oci://example.com/bar"},
        },
    }

    cat, ok := cfg.GetCatalog("foo")
    assert.True(t, ok)
    assert.Equal(t, "foo", cat.Name)

    _, ok = cfg.GetCatalog("nonexistent")
    assert.False(t, ok)
}

func TestConfig_AddCatalog(t *testing.T) {
    t.Parallel()

    cfg := &Config{}

    err := cfg.AddCatalog(CatalogConfig{Name: "new", Type: "filesystem"})
    assert.NoError(t, err)
    assert.Len(t, cfg.Catalogs, 1)

    // Duplicate should error
    err = cfg.AddCatalog(CatalogConfig{Name: "new", Type: "filesystem"})
    assert.Error(t, err)
}

func TestConfig_RemoveCatalog(t *testing.T) {
    t.Parallel()

    cfg := &Config{
        Catalogs: []CatalogConfig{
            {Name: "foo", Type: "filesystem"},
            {Name: "bar", Type: "oci"},
        },
    }

    err := cfg.RemoveCatalog("foo")
    assert.NoError(t, err)
    assert.Len(t, cfg.Catalogs, 1)
    assert.Equal(t, "bar", cfg.Catalogs[0].Name)

    err = cfg.RemoveCatalog("nonexistent")
    assert.Error(t, err)
}
```

#### Step 5: Integrate with Root Command

```go
// pkg/cmd/scafctl/root.go
package scafctl

import (
    // ... existing imports
    "github.com/oakwood-commons/scafctl/pkg/config"
)

var (
    cliParams  = settings.NewCliParams()
    configPath string
    appConfig  *config.Config
)

func Root() *cobra.Command {
    cCmd := &cobra.Command{
        Use:   "scafctl",
        Short: "A configuration discovery and scaffolding tool",
        PersistentPreRunE: func(cCmd *cobra.Command, args []string) error {
            // Load configuration
            mgr := config.NewManager(configPath)
            cfg, err := mgr.Load()
            if err != nil {
                return fmt.Errorf("failed to load config: %w", err)
            }
            appConfig = cfg

            // Apply config settings to cliParams (CLI flags take precedence)
            if !cCmd.Flags().Changed("no-color") {
                cliParams.NoColor = cfg.Settings.NoColor
            }
            if !cCmd.Flags().Changed("quiet") {
                cliParams.IsQuiet = cfg.Settings.Quiet
            }
            if !cCmd.Flags().Changed("log-level") {
                cliParams.MinLogLevel = int8(cfg.Settings.LogLevel)
            }

            // ... rest of existing setup
            return nil
        },
        // ...
    }

    // Add --config flag
    cCmd.PersistentFlags().StringVar(&configPath, "config", "",
        "Path to config file (default: ~/.scafctl/config.yaml)")

    // ... existing flags and subcommands

    return cCmd
}
```

#### Step 6: Implement Config Commands

```go
// pkg/cmd/scafctl/config/config.go
package config

import (
    "fmt"

    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/spf13/cobra"
)

// CommandConfig creates the 'config' command.
func CommandConfig(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    cCmd := &cobra.Command{
        Use:   "config",
        Short: "Manage scafctl configuration",
        Long: `View and manage scafctl configuration.

Configuration is stored at ~/.scafctl/config.yaml by default.
Use --config flag to specify an alternate location.

Environment variables with SCAFCTL_ prefix override config file values.`,
        SilenceUsage: true,
    }

    cCmd.AddCommand(CommandView(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
    cCmd.AddCommand(CommandGet(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
    cCmd.AddCommand(CommandSet(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
    cCmd.AddCommand(CommandUnset(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
    cCmd.AddCommand(CommandAddCatalog(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
    cCmd.AddCommand(CommandRemoveCatalog(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))
    cCmd.AddCommand(CommandUseCatalog(cliParams, ioStreams, fmt.Sprintf("%s/%s", path, cCmd.Use)))

    return cCmd
}
```

```go
// pkg/cmd/scafctl/config/view.go
package config

import (
    "context"
    "path/filepath"

    "github.com/oakwood-commons/scafctl/pkg/cmd/flags"
    appconfig "github.com/oakwood-commons/scafctl/pkg/config"
    "github.com/oakwood-commons/scafctl/pkg/logger"
    "github.com/oakwood-commons/scafctl/pkg/settings"
    "github.com/oakwood-commons/scafctl/pkg/terminal"
    "github.com/oakwood-commons/scafctl/pkg/terminal/kvx"
    "github.com/oakwood-commons/scafctl/pkg/terminal/writer"
    "github.com/spf13/cobra"
)

type ViewOptions struct {
    IOStreams  *terminal.IOStreams
    CliParams  *settings.Run
    ConfigPath string

    flags.KvxOutputFlags
}

func CommandView(cliParams *settings.Run, ioStreams *terminal.IOStreams, path string) *cobra.Command {
    opts := &ViewOptions{}

    cCmd := &cobra.Command{
        Use:   "view",
        Short: "View current configuration",
        Long: `Display the current configuration.

Shows all settings from the config file merged with environment overrides.

Examples:
  # View config as YAML
  scafctl config view

  # View config as JSON
  scafctl config view -o json

  # View specific section
  scafctl config view -e '_.catalogs'`,
        RunE: func(cCmd *cobra.Command, args []string) error {
            cliParams.EntryPointSettings.Path = filepath.Join(path, cCmd.Use)
            ctx := settings.IntoContext(context.Background(), cliParams)

            if lgr := logger.FromContext(cCmd.Context()); lgr != nil {
                ctx = logger.WithLogger(ctx, lgr)
            }

            w := writer.FromContext(cCmd.Context())
            if w == nil {
                w = writer.New(ioStreams, cliParams)
            }
            ctx = writer.WithWriter(ctx, w)

            opts.IOStreams = ioStreams
            opts.CliParams = cliParams

            return opts.Run(ctx)
        },
        SilenceUsage: true,
    }

    flags.AddKvxOutputFlagsToStruct(cCmd, &opts.KvxOutputFlags)
    // Default to yaml for config view
    if err := cCmd.Flags().Set("output", "yaml"); err != nil {
        return nil
    }

    return cCmd
}

func (o *ViewOptions) Run(ctx context.Context) error {
    mgr := appconfig.NewManager(o.ConfigPath)
    cfg, err := mgr.Load()
    if err != nil {
        return err
    }

    // Include config file path in output
    output := map[string]any{
        "configFile": mgr.ConfigPath(),
        "catalogs":   cfg.Catalogs,
        "settings":   cfg.Settings,
    }

    return o.writeOutput(ctx, output)
}

func (o *ViewOptions) writeOutput(ctx context.Context, data any) error {
    kvxOpts := flags.NewKvxOutputOptionsFromFlags(
        o.Output,
        o.Interactive,
        o.Expression,
        kvx.WithOutputContext(ctx),
        kvx.WithOutputNoColor(o.CliParams.NoColor),
        kvx.WithOutputAppName("scafctl config view"),
    )
    kvxOpts.IOStreams = o.IOStreams

    return kvxOpts.Write(data)
}
```

### Environment Variable Mapping

| Config Path | Environment Variable |
|-------------|---------------------|
| `settings.defaultCatalog` | `SCAFCTL_SETTINGS_DEFAULTCATALOG` |
| `settings.noColor` | `SCAFCTL_SETTINGS_NOCOLOR` |
| `settings.quiet` | `SCAFCTL_SETTINGS_QUIET` |
| `settings.logLevel` | `SCAFCTL_SETTINGS_LOGLEVEL` |

---

## Implementation Order

### Phase 1: Exit Codes (1-2 hours)

1. Create `pkg/exitcode/exitcode.go`
2. Create `pkg/exitcode/exitcode_test.go`
3. Update `pkg/cmd/scafctl/run/solution.go` to use `exitcode` package
4. Update `pkg/cmd/scafctl/run/solution_test.go`
5. Update `pkg/cmd/scafctl/render/solution.go` to use `exitcode` package
6. Update `pkg/cmd/scafctl/render/solution_test.go`
7. Run `golangci-lint run --fix` and `go test ./...`

### Phase 2: Config Package (4-6 hours)

1. Add Viper dependency: `go get github.com/spf13/viper`
2. Create `pkg/config/types.go`
3. Create `pkg/config/config.go`
4. Create `pkg/config/config_test.go`
5. Run tests

### Phase 3: Root Integration (2-3 hours)

1. Update `pkg/cmd/scafctl/root.go` to load config
2. Add `--config` global flag
3. Apply config settings to `cliParams`
4. Test config loading

### Phase 4: Config Commands (4-6 hours)

1. Create `pkg/cmd/scafctl/config/config.go` (parent command)
2. Implement `config view`
3. Implement `config get`
4. Implement `config set`
5. Implement `config unset`
6. Implement `config add-catalog`
7. Implement `config remove-catalog`
8. Implement `config use-catalog`
9. Add tests for each command
10. Register in root command

### Estimated Total: 11-17 hours

---

## Files Summary

### New Files

| File | Purpose |
|------|---------|
| `pkg/exitcode/exitcode.go` | Central exit code definitions |
| `pkg/exitcode/exitcode_test.go` | Exit code tests |
| `pkg/config/types.go` | Configuration type definitions |
| `pkg/config/config.go` | Configuration manager |
| `pkg/config/config_test.go` | Configuration tests |
| `pkg/cmd/scafctl/config/config.go` | Config parent command |
| `pkg/cmd/scafctl/config/view.go` | `config view` command |
| `pkg/cmd/scafctl/config/get.go` | `config get` command |
| `pkg/cmd/scafctl/config/set.go` | `config set` command |
| `pkg/cmd/scafctl/config/unset.go` | `config unset` command |
| `pkg/cmd/scafctl/config/add_catalog.go` | `config add-catalog` command |
| `pkg/cmd/scafctl/config/remove_catalog.go` | `config remove-catalog` command |
| `pkg/cmd/scafctl/config/use_catalog.go` | `config use-catalog` command |

### Modified Files

| File | Changes |
|------|---------|
| `go.mod` | Add Viper dependency |
| `pkg/cmd/scafctl/root.go` | Load config, add `--config` flag |
| `pkg/cmd/scafctl/run/solution.go` | Use `exitcode` package |
| `pkg/cmd/scafctl/run/solution_test.go` | Use `exitcode` package |
| `pkg/cmd/scafctl/render/solution.go` | Use `exitcode` package |
| `pkg/cmd/scafctl/render/solution_test.go` | Use `exitcode` package |
