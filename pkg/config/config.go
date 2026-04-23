// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/spf13/viper"
)

const (
	// DefaultConfigFileName is the default config file name (without extension).
	DefaultConfigFileName = "config"

	// DefaultConfigFileType is the default config file type.
	DefaultConfigFileType = "yaml"

	// EnvPrefix is the environment variable prefix.
	EnvPrefix = "SCAFCTL"
)

var (
	globalConfig            *Config
	globalConfigMu          sync.Mutex
	globalConfigInitialized bool
	globalConfigErr         error
)

// ManagerOption configures a Manager.
type ManagerOption func(*Manager)

// WithBaseConfig sets an embedded configuration layer that is merged after
// built-in defaults but before the user's config file. This allows embedders
// to ship organization-specific defaults without overwriting user config.
// The bytes must be valid YAML.
func WithBaseConfig(data []byte) ManagerOption {
	return func(m *Manager) {
		m.baseConfig = data
	}
}

// WithEnvPrefix overrides the default environment variable prefix ("SCAFCTL").
// The prefix is normalized to a safe env var format (upper-cased, hyphens/dots replaced with underscores).
func WithEnvPrefix(prefix string) ManagerOption {
	return func(m *Manager) {
		m.envPrefix = settings.SafeEnvPrefix(prefix)
	}
}

// Manager handles configuration loading and access.
type Manager struct {
	v          *viper.Viper
	configPath string
	baseConfig []byte
	envPrefix  string
	config     *Config
}

// NewManager creates a new configuration manager.
// If configPath is empty, the XDG-compliant default path will be used.
func NewManager(configPath string, opts ...ManagerOption) *Manager {
	m := &Manager{
		configPath: configPath,
		envPrefix:  EnvPrefix,
	}
	for _, opt := range opts {
		opt(m)
	}

	v := viper.New()
	v.SetConfigType(DefaultConfigFileType)
	v.SetEnvPrefix(m.envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	m.v = v

	return m
}

// Load loads the configuration from file and environment.
func (m *Manager) Load() (*Config, error) {
	// Determine config path
	configPath := m.configPath
	if configPath == "" {
		var err error
		configPath, err = paths.ConfigFile()
		if err != nil {
			return nil, fmt.Errorf("failed to determine config path: %w", err)
		}
	}

	// Set config file
	m.v.SetConfigFile(configPath)

	// Set defaults
	m.setDefaults()

	// Merge embedder base config (after defaults, before user file)
	if len(m.baseConfig) > 0 {
		if err := m.v.MergeConfig(bytes.NewReader(m.baseConfig)); err != nil {
			return nil, fmt.Errorf("failed to merge base config: %w", err)
		}
	}

	// Read user config file (not an error if it doesn't exist).
	// MergeInConfig is used unconditionally so that when a base config layer
	// is present the user file merges on top rather than replacing it.
	// When no base config exists MergeInConfig behaves identically to
	// ReadInConfig.
	if err := m.v.MergeInConfig(); err != nil {
		var configFileNotFoundError viper.ConfigFileNotFoundError
		if !errors.As(err, &configFileNotFoundError) && !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Unmarshal into struct
	var cfg Config
	if err := m.v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Viper replaces arrays entirely when merging config files, so default
	// catalog entries may be lost when a base config or user config supplies
	// its own catalogs list. Re-add any missing default catalogs by name.
	mergeDefaultCatalogEntries(&cfg)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	m.config = &cfg
	return &cfg, nil
}

// setDefaults sets default configuration values.
func (m *Manager) setDefaults() {
	// Settings defaults
	m.v.SetDefault("settings.defaultCatalog", "official")
	m.v.SetDefault("settings.noColor", false)
	m.v.SetDefault("settings.quiet", false)
	m.v.SetDefault("catalogs", defaultCatalogs())

	// Logging defaults
	m.v.SetDefault("logging.level", "none")
	m.v.SetDefault("logging.format", LoggingFormatConsole)
	m.v.SetDefault("logging.timestamps", true)
	m.v.SetDefault("logging.enableProfiling", false)

	// Telemetry defaults
	m.v.SetDefault("telemetry.endpoint", "")
	m.v.SetDefault("telemetry.insecure", false)
	m.v.SetDefault("telemetry.serviceName", "")
	m.v.SetDefault("telemetry.samplerType", settings.DefaultOTelSamplerType)
	m.v.SetDefault("telemetry.samplerArg", settings.DefaultOTelSamplerArg)

	// HTTP client defaults - all values from settings package
	m.v.SetDefault("httpClient.timeout", settings.DefaultHTTPTimeout.String())
	m.v.SetDefault("httpClient.retryMax", settings.DefaultHTTPRetryMax)
	m.v.SetDefault("httpClient.retryWaitMin", settings.DefaultHTTPRetryWaitMinimum.String())
	m.v.SetDefault("httpClient.retryWaitMax", settings.DefaultHTTPRetryWaitMaximum.String())
	m.v.SetDefault("httpClient.enableCache", true)
	m.v.SetDefault("httpClient.cacheType", HTTPClientCacheTypeFilesystem)
	m.v.SetDefault("httpClient.cacheDir", settings.DefaultHTTPCacheDir())
	m.v.SetDefault("httpClient.cacheTTL", settings.DefaultHTTPCacheTTL.String())
	m.v.SetDefault("httpClient.cacheKeyPrefix", settings.DefaultHTTPCacheKeyPrefix)
	m.v.SetDefault("httpClient.maxCacheFileSize", settings.DefaultMaxCacheFileSize)
	m.v.SetDefault("httpClient.memoryCacheSize", settings.DefaultMemoryCacheSize)
	m.v.SetDefault("httpClient.enableCircuitBreaker", false)
	m.v.SetDefault("httpClient.circuitBreakerMaxFailures", settings.DefaultCircuitBreakerMaxFailures)
	m.v.SetDefault("httpClient.circuitBreakerOpenTimeout", settings.DefaultCircuitBreakerOpenTimeout.String())
	m.v.SetDefault("httpClient.circuitBreakerHalfOpenMaxRequests", settings.DefaultCircuitBreakerHalfOpenRequests)
	m.v.SetDefault("httpClient.enableCompression", true)

	// CEL defaults - all values from settings package
	m.v.SetDefault("cel.cacheSize", settings.DefaultCELCacheSize)
	m.v.SetDefault("cel.costLimit", settings.DefaultCELCostLimit)
	m.v.SetDefault("cel.useASTBasedCaching", false)
	m.v.SetDefault("cel.enableMetrics", true)

	// Go template defaults - all values from settings package
	m.v.SetDefault("goTemplate.cacheSize", settings.DefaultGoTemplateCacheSize)
	m.v.SetDefault("goTemplate.enableMetrics", true)

	// Resolver defaults - all values from settings package
	m.v.SetDefault("resolver.timeout", settings.DefaultResolverTimeout.String())
	m.v.SetDefault("resolver.phaseTimeout", settings.DefaultPhaseTimeout.String())
	m.v.SetDefault("resolver.maxConcurrency", 0)
	m.v.SetDefault("resolver.warnValueSize", settings.DefaultWarnValueSize)
	m.v.SetDefault("resolver.maxValueSize", settings.DefaultMaxValueSize)
	m.v.SetDefault("resolver.validateAll", false)

	// Action defaults - all values from settings package
	m.v.SetDefault("action.defaultTimeout", settings.DefaultActionTimeout.String())
	m.v.SetDefault("action.gracePeriod", settings.DefaultGracePeriod.String())
	m.v.SetDefault("action.maxConcurrency", 0)
	m.v.SetDefault("action.outputDir", "")

	// Build defaults - all values from settings package
	m.v.SetDefault("build.enableCache", true)
	m.v.SetDefault("build.cacheDir", settings.DefaultBuildCacheDir())
	m.v.SetDefault("build.autoCacheRemoteArtifacts", true)
	m.v.SetDefault("build.pluginCacheDir", settings.DefaultPluginCacheDir())
}

// defaultCatalogs returns the built-in default catalog entries.
// Values are read from the embedded defaults.yaml; hardcoded constants serve
// as fallbacks when the embedded config is missing or unparseable.
func defaultCatalogs() []CatalogConfig {
	if embedded := EmbeddedCatalogDefaults(); len(embedded) > 0 {
		// Backfill the local catalog path which cannot be expressed in YAML
		// (it depends on XDG runtime resolution).
		for i := range embedded {
			if embedded[i].Type == CatalogTypeFilesystem && embedded[i].Path == "" {
				embedded[i].Path = paths.CatalogDir()
			}
		}
		return embedded
	}

	// Fallback when embedded defaults are absent.
	return []CatalogConfig{
		{
			Name: "local",
			Type: CatalogTypeFilesystem,
			Path: paths.CatalogDir(),
		},
		{
			Name:         "official",
			Type:         CatalogTypeOCI,
			URL:          "oci://ghcr.io/oakwood-commons",
			AuthProvider: "github",
		},
	}
}

// mergeDefaultCatalogEntries ensures built-in default catalog entries are
// present in cfg.Catalogs. Viper replaces arrays entirely when merging config
// layers, so defaults may be lost.
//
// Reserved catalog names ("local", "official") are fully enforced: all
// fields are overwritten with the default values so that users cannot redirect
// them to arbitrary registries. Non-reserved entries that match a default by
// name get missing fields backfilled without overwriting user values.
//
// The "official" catalog is skipped when DisableOfficialCatalog is set.
func mergeDefaultCatalogEntries(cfg *Config) {
	defaults := defaultCatalogs()
	defaultsByName := make(map[string]CatalogConfig, len(defaults))
	for _, dc := range defaults {
		defaultsByName[dc.Name] = dc
	}

	// Enforce reserved entries and backfill non-reserved entries.
	seen := make(map[string]struct{}, len(cfg.Catalogs))
	for i := range cfg.Catalogs {
		seen[cfg.Catalogs[i].Name] = struct{}{}
		dc, ok := defaultsByName[cfg.Catalogs[i].Name]
		if !ok {
			continue
		}
		if IsReservedCatalogName(cfg.Catalogs[i].Name) {
			// Reserved: overwrite all fields from defaults.
			cfg.Catalogs[i] = dc
			continue
		}
		// Non-reserved: backfill missing fields only.
		if cfg.Catalogs[i].Type == "" {
			cfg.Catalogs[i].Type = dc.Type
		}
		if cfg.Catalogs[i].Path == "" && dc.Path != "" {
			cfg.Catalogs[i].Path = dc.Path
		}
		if cfg.Catalogs[i].URL == "" && dc.URL != "" {
			cfg.Catalogs[i].URL = dc.URL
		}
		if cfg.Catalogs[i].AuthProvider == "" && dc.AuthProvider != "" {
			cfg.Catalogs[i].AuthProvider = dc.AuthProvider
		}
	}

	// Append entirely missing defaults.
	for _, dc := range defaults {
		if _, ok := seen[dc.Name]; ok {
			continue
		}
		if dc.Name == CatalogNameOfficial && cfg.Settings.DisableOfficialCatalog {
			continue
		}
		cfg.Catalogs = append(cfg.Catalogs, dc)
	}
}

// Save saves the current configuration to file.
// It syncs m.config to viper before writing, then uses viper's WriteConfig.
// This allows both direct config modification AND Set() calls to be persisted.
func (m *Manager) Save() error {
	if m.config == nil {
		return fmt.Errorf("no configuration loaded")
	}

	configPath := m.v.ConfigFileUsed()
	if configPath == "" {
		var err error
		configPath, err = paths.ConfigFile()
		if err != nil {
			return fmt.Errorf("failed to determine config path: %w", err)
		}
		m.v.SetConfigFile(configPath)
	}

	// Ensure directory exists
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Sync m.config to viper. This allows direct modifications to cfg.Settings
	// or cfg.Catalogs to be persisted. For individual key changes, use Set() first.
	m.v.Set("version", m.config.Version)
	m.v.Set("catalogs", m.config.Catalogs)
	m.v.Set("settings", m.config.Settings)
	m.v.Set("logging", m.config.Logging)
	m.v.Set("telemetry", m.config.Telemetry)
	m.v.Set("httpClient", m.config.HTTPClient)
	m.v.Set("cel", m.config.CEL)
	m.v.Set("resolver", m.config.Resolver)
	m.v.Set("action", m.config.Action)
	m.v.Set("auth", m.config.Auth)
	m.v.Set("build", m.config.Build)

	return m.v.WriteConfig()
}

// SaveAs saves the configuration to a specific path.
func (m *Manager) SaveAs(path string) error {
	if m.config == nil {
		return fmt.Errorf("no configuration loaded")
	}

	// Ensure directory exists
	configDir := filepath.Dir(path)
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Update viper with current config
	m.v.Set("version", m.config.Version)
	m.v.Set("catalogs", m.config.Catalogs)
	m.v.Set("settings", m.config.Settings)
	m.v.Set("logging", m.config.Logging)
	m.v.Set("telemetry", m.config.Telemetry)
	m.v.Set("httpClient", m.config.HTTPClient)
	m.v.Set("cel", m.config.CEL)
	m.v.Set("resolver", m.config.Resolver)
	m.v.Set("action", m.config.Action)
	m.v.Set("auth", m.config.Auth)
	m.v.Set("build", m.config.Build)

	return m.v.WriteConfigAs(path)
}

// Get returns a configuration value by key.
func (m *Manager) Get(key string) any {
	return m.v.Get(key)
}

// Set sets a configuration value.
// For individual settings fields (e.g., "logging.level"), this also updates
// m.config to keep it in sync. For top-level struct values like "settings" or
// "catalogs", only viper is updated (the caller should modify cfg directly instead).
func (m *Manager) Set(key string, value any) {
	m.v.Set(key, value)

	// Keep m.config in sync for individual settings fields
	if m.config != nil {
		switch key {
		case "logging.level":
			switch v := value.(type) {
			case string:
				m.config.Logging.Level = v
			case int:
				m.config.Logging.Level = strconv.Itoa(v)
			}
		case "logging.format":
			if v, ok := value.(string); ok {
				m.config.Logging.Format = v
			}
		case "logging.timestamps":
			if v, ok := value.(bool); ok {
				m.config.Logging.Timestamps = v
			}
		case "logging.enableProfiling":
			if v, ok := value.(bool); ok {
				m.config.Logging.EnableProfiling = v
			}
		case "settings.quiet":
			if v, ok := value.(bool); ok {
				m.config.Settings.Quiet = v
			}
		case "settings.noColor":
			if v, ok := value.(bool); ok {
				m.config.Settings.NoColor = v
			}
		case "settings.defaultCatalog":
			if v, ok := value.(string); ok {
				m.config.Settings.DefaultCatalog = v
			}
		}
	}
}

// Config returns the loaded configuration.
func (m *Manager) Config() *Config {
	return m.config
}

// ConfigPath returns the path to the config file.
func (m *Manager) ConfigPath() string {
	used := m.v.ConfigFileUsed()
	if used != "" {
		return used
	}
	// Return the configured path if no file has been loaded yet
	if m.configPath != "" {
		return m.configPath
	}
	// Return default XDG path
	defaultPath, err := paths.ConfigFile()
	if err != nil {
		// Fallback should not happen in practice
		return ""
	}
	return defaultPath
}

// IsSet checks if a configuration key is set.
func (m *Manager) IsSet(key string) bool {
	return m.v.IsSet(key)
}

// AllSettings returns all settings as a map.
func (m *Manager) AllSettings() map[string]any {
	return m.v.AllSettings()
}

// Global returns the global configuration (loads once).
func Global() (*Config, error) {
	globalConfigMu.Lock()
	defer globalConfigMu.Unlock()
	if !globalConfigInitialized {
		mgr := NewManager("")
		globalConfig, globalConfigErr = mgr.Load()
		globalConfigInitialized = true
	}
	return globalConfig, globalConfigErr
}

// ResetGlobal resets the global configuration (primarily for testing).
func ResetGlobal() {
	globalConfigMu.Lock()
	defer globalConfigMu.Unlock()
	globalConfigInitialized = false
	globalConfig = nil
	globalConfigErr = nil
}

// DefaultConfigPath returns the default configuration file path.
// Uses XDG Base Directory Specification.
func DefaultConfigPath() (string, error) {
	return paths.ConfigFile()
}
