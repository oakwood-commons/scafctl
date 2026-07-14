// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/oakwood-commons/scafctl/pkg/paths"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

const (
	// DefaultConfigFileName is the default config file name (without extension).
	DefaultConfigFileName = "config"

	// DefaultConfigFileType is the default config file type.
	DefaultConfigFileType = "yaml"

	// ConfigDirName is the drop-in directory (relative to the config file's
	// directory) whose *.yaml/*.yml fragments are merged in lexical order
	// between the embedder base layer and the user's config file.
	ConfigDirName = "config.d"

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

	// dropIn holds the merged config.d fragment values (lowercased keys), and
	// userSettings holds the values read from the user's config file alone. Save
	// uses them to avoid baking tooling-supplied drop-in values into the user's
	// config file, which would defeat config.d layering.
	dropIn       map[string]any
	userSettings map[string]any
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

	// Merge drop-in config fragments from the config.d directory (after the
	// embedder base layer, before the user's config file). Fragments are
	// merged in lexical filename order, so later files override earlier ones.
	// Precedence: defaults -> base -> config.d -> config.yaml -> env -> flags.
	if err := m.mergeConfigDir(filepath.Dir(configPath)); err != nil {
		return nil, err
	}

	// Capture the user config file's own values (no defaults, base, config.d, or
	// env) so Save can distinguish user-owned keys from drop-in values.
	m.userSettings = readUserSettings(configPath)

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

	// Viper/mapstructure drops struct pointers when all fields are zero-valued.
	// This means `auth.github: { profiles: { work: {} } }` round-trips as
	// Auth.GitHub == nil. Fix up by checking the raw viper data for auth
	// handler entries and profile keys that were lost during unmarshal.
	restoreAuthProfileEntries(m.v, &cfg)

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

// DirFragments returns the config.d fragment file paths located next to
// the main config file, in the same lexical order the loader merges them.
// baseDir is the directory containing the config file (i.e. the parent of the
// config.d directory). Both *.yaml and *.yml fragments are included. A missing
// or empty config.d directory yields a nil slice and no error.
//
// This is the single source of truth for config.d discovery: mergeConfigDir
// consumes it during load, and the `config paths` command uses it to list the
// fragments that are actually read. Keeping one implementation prevents the
// listing from drifting from the merge behavior.
func DirFragments(baseDir string) ([]string, error) {
	if baseDir == "" {
		return nil, nil
	}
	dir := filepath.Join(baseDir, ConfigDirName)

	var fragments []string
	for _, pattern := range []string{"*.yaml", "*.yml"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, fmt.Errorf("failed to scan %s: %w", dir, err)
		}
		fragments = append(fragments, matches...)
	}
	sort.Strings(fragments)
	return fragments, nil
}

// mergeConfigDir merges every *.yaml/*.yml fragment in the config.d directory
// located next to the main config file. Fragments are merged in lexical
// filename order (later files override earlier ones). A missing config.d
// directory is not an error; individual fragments that fail to parse are.
func (m *Manager) mergeConfigDir(baseDir string) error {
	fragments, err := DirFragments(baseDir)
	if err != nil {
		return err
	}
	if len(fragments) == 0 {
		return nil
	}

	// Track drop-in values (parsed with documented yaml-tag casing) so Save can
	// identify and skip them, keeping config.d fragments out of the persisted
	// user config file. Deep-merged in lexical order to mirror the viper merge.
	dropMap := map[string]any{}

	for _, fragment := range fragments {
		data, err := os.ReadFile(fragment) //nolint:gosec // path derived from trusted config dir
		if err != nil {
			return fmt.Errorf("failed to read config fragment %s: %w", fragment, err)
		}
		if err := m.v.MergeConfig(bytes.NewReader(data)); err != nil {
			return fmt.Errorf("failed to merge config fragment %s: %w", fragment, err)
		}
		var fragMap map[string]any
		if err := yaml.Unmarshal(data, &fragMap); err != nil {
			return fmt.Errorf("failed to parse config fragment %s: %w", fragment, err)
		}
		deepMergeMap(dropMap, fragMap)
	}
	m.dropIn = dropMap
	return nil
}

// deepMergeMap recursively merges src into dst, with src winning on conflicts.
// Nested maps are merged; all other values (including slices) are replaced.
func deepMergeMap(dst, src map[string]any) {
	for k, v := range src {
		if sv, ok := v.(map[string]any); ok {
			if dv, ok := dst[k].(map[string]any); ok {
				deepMergeMap(dv, sv)
				continue
			}
		}
		dst[k] = v
	}
}

// readUserSettings reads the user's config file alone (excluding defaults, the
// embedder base layer, config.d, and env vars) and returns its settings map
// keyed by documented (yaml-tag) names. A missing or unreadable file yields nil.
func readUserSettings(configPath string) map[string]any {
	if configPath == "" {
		return nil
	}
	data, err := os.ReadFile(configPath) //nolint:gosec // path is the configured config file
	if err != nil {
		return nil
	}
	var settings map[string]any
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return nil
	}
	return settings
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

// restoreAuthProfileEntries fixes up auth handler configs and profile entries
// that viper/mapstructure dropped during Unmarshal. When all fields in a struct
// pointer are zero-valued, mapstructure sets the pointer to nil. This means
// `auth.github: { profiles: { work: {} } }` round-trips as Auth.GitHub == nil.
// We check the raw viper data and restore the empty entries.
func restoreAuthProfileEntries(v *viper.Viper, cfg *Config) {
	type handlerProfiles struct {
		viperKey string
		initFn   func()
		profiles func() map[string]bool
		addFn    func(string)
	}

	handlers := []handlerProfiles{
		{
			viperKey: "auth.entra.profiles",
			initFn: func() {
				if cfg.Auth.Entra == nil {
					cfg.Auth.Entra = &EntraAuthConfig{}
				}
			},
			profiles: func() map[string]bool {
				if cfg.Auth.Entra == nil {
					return nil
				}
				m := make(map[string]bool, len(cfg.Auth.Entra.Profiles))
				for k := range cfg.Auth.Entra.Profiles {
					m[k] = true
				}
				return m
			},
			addFn: func(name string) {
				if cfg.Auth.Entra.Profiles == nil {
					cfg.Auth.Entra.Profiles = make(map[string]*EntraProfileConfig)
				}
				if _, ok := cfg.Auth.Entra.Profiles[name]; !ok {
					cfg.Auth.Entra.Profiles[name] = &EntraProfileConfig{}
				}
			},
		},
		{
			viperKey: "auth.github.profiles",
			initFn: func() {
				if cfg.Auth.GitHub == nil {
					cfg.Auth.GitHub = &GitHubAuthConfig{}
				}
			},
			profiles: func() map[string]bool {
				if cfg.Auth.GitHub == nil {
					return nil
				}
				m := make(map[string]bool, len(cfg.Auth.GitHub.Profiles))
				for k := range cfg.Auth.GitHub.Profiles {
					m[k] = true
				}
				return m
			},
			addFn: func(name string) {
				if cfg.Auth.GitHub.Profiles == nil {
					cfg.Auth.GitHub.Profiles = make(map[string]*GitHubProfileConfig)
				}
				if _, ok := cfg.Auth.GitHub.Profiles[name]; !ok {
					cfg.Auth.GitHub.Profiles[name] = &GitHubProfileConfig{}
				}
			},
		},
		{
			viperKey: "auth.gcp.profiles",
			initFn: func() {
				if cfg.Auth.GCP == nil {
					cfg.Auth.GCP = &GCPAuthConfig{}
				}
			},
			profiles: func() map[string]bool {
				if cfg.Auth.GCP == nil {
					return nil
				}
				m := make(map[string]bool, len(cfg.Auth.GCP.Profiles))
				for k := range cfg.Auth.GCP.Profiles {
					m[k] = true
				}
				return m
			},
			addFn: func(name string) {
				if cfg.Auth.GCP.Profiles == nil {
					cfg.Auth.GCP.Profiles = make(map[string]*GCPProfileConfig)
				}
				if _, ok := cfg.Auth.GCP.Profiles[name]; !ok {
					cfg.Auth.GCP.Profiles[name] = &GCPProfileConfig{}
				}
			},
		},
	}

	for _, h := range handlers {
		raw := v.Get(h.viperKey)
		rawMap, ok := raw.(map[string]any)
		if !ok || len(rawMap) == 0 {
			continue
		}
		// Raw data has profile keys — ensure the struct exists.
		h.initFn()
		existing := h.profiles()
		for name := range rawMap {
			if !existing[name] {
				h.addFn(name)
			}
		}
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
	m.v.Set("kube", m.config.Kube)
	m.v.Set("build", m.config.Build)

	return m.writeConfig(m.v, "")
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
	m.v.Set("kube", m.config.Kube)
	m.v.Set("build", m.config.Build)

	return m.writeConfig(m.v, path)
}

// writeConfig persists the configuration, pruning any values that came solely
// from the config.d drop-in layer so they are not baked into the user's config
// file (which would defeat config.d layering). When path is empty the manager's
// current config file is used. With no drop-in layer present it writes v
// directly, preserving the original output byte-for-byte.
func (m *Manager) writeConfig(v *viper.Viper, path string) error {
	if len(m.dropIn) == 0 {
		if path == "" {
			return v.WriteConfig()
		}
		return v.WriteConfigAs(path)
	}

	// Normalize the effective config to documented (yaml-tag) casing so it can
	// be compared against the fragment/user maps, then drop pure drop-in values.
	effBytes, err := yaml.Marshal(m.config)
	if err != nil {
		return fmt.Errorf("failed to marshal config for save: %w", err)
	}
	var effMap map[string]any
	if err := yaml.Unmarshal(effBytes, &effMap); err != nil {
		return fmt.Errorf("failed to normalize config for save: %w", err)
	}

	pruned := pruneDropIn(effMap, m.dropIn, m.userSettings)
	outBytes, err := yaml.Marshal(pruned)
	if err != nil {
		return fmt.Errorf("failed to marshal config for save: %w", err)
	}

	target := path
	if target == "" {
		target = v.ConfigFileUsed()
	}
	if target == "" {
		return fmt.Errorf("no config file path available for save")
	}
	if err := os.WriteFile(target, outBytes, 0o600); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", target, err)
	}
	return nil
}

// pruneDropIn returns a copy of effective with keys whose values came solely
// from the config.d drop-in layer removed. A key is dropped only when its value
// equals the drop-in value and the user's own config file did not set it; keys
// the user set, or values that differ from the drop-in, are always kept. Nested
// maps are pruned recursively.
func pruneDropIn(effective, dropIn, user map[string]any) map[string]any {
	out := make(map[string]any, len(effective))
	for k, v := range effective {
		dv, inDrop := dropIn[k]
		if !inDrop {
			out[k] = v
			continue
		}

		uv, inUser := user[k]

		// Recurse into nested maps so partially user-owned subtrees survive.
		if vm, ok := v.(map[string]any); ok {
			if dm, ok := dv.(map[string]any); ok {
				um, _ := uv.(map[string]any)
				if pruned := pruneDropIn(vm, dm, um); len(pruned) > 0 {
					out[k] = pruned
				}
				continue
			}
		}

		if inUser {
			out[k] = v
			continue
		}
		if reflect.DeepEqual(v, dv) {
			continue // pure drop-in value; the fragment re-supplies it on load
		}
		out[k] = v
	}
	return out
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

// Delete removes a configuration key from the user's config file on disk.
// Unlike Set(key, nil), this physically removes the key from the YAML so that
// embedded defaults or Viper defaults take effect on next Load().
// It operates directly on the YAML file to avoid Viper's inability to truly
// delete keys.
//
// Returns true if the key was found and removed, false if it was not present.
// A missing config file is treated as a no-op (returns false, nil).
func (m *Manager) Delete(key string) (bool, error) {
	configPath, err := m.configPathOrError()
	if err != nil {
		return false, err
	}

	data, err := os.ReadFile(configPath) //nolint:gosec // config path from trusted source
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // no config file means nothing to delete
		}
		return false, fmt.Errorf("reading config file: %w", err)
	}

	var rawCfg map[string]any
	if err := yaml.Unmarshal(data, &rawCfg); err != nil {
		return false, fmt.Errorf("parsing config file: %w", err)
	}
	if rawCfg == nil {
		return false, nil // nothing to delete
	}

	if !deleteNestedKey(rawCfg, strings.Split(key, ".")) {
		return false, nil // key not present, nothing to do
	}

	out, err := yaml.Marshal(rawCfg)
	if err != nil {
		return false, fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(configPath, out, 0o600); err != nil { //nolint:gosec // config file permissions
		return false, fmt.Errorf("writing config file: %w", err)
	}
	return true, nil
}

// deleteNestedKey removes a key from a nested map structure given a path of
// dot-separated segments. Returns true if the key was found and removed.
func deleteNestedKey(m map[string]any, parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	if len(parts) == 1 {
		if _, exists := m[parts[0]]; exists {
			delete(m, parts[0])
			return true
		}
		return false
	}
	// Traverse into the next level.
	next, ok := m[parts[0]].(map[string]any)
	if !ok {
		return false
	}
	deleted := deleteNestedKey(next, parts[1:])
	// Clean up empty parent maps.
	if deleted && len(next) == 0 {
		delete(m, parts[0])
	}
	return deleted
}

// Config returns the loaded configuration.
func (m *Manager) Config() *Config {
	return m.config
}

// ConfigPath returns the path to the config file.
func (m *Manager) ConfigPath() string {
	p, _ := m.configPathOrError()
	return p
}

// ConfigDir returns the directory containing the config file. This is the
// anchor for the config.d drop-in directory and for resolving relative paths
// referenced from configuration. Returns an empty string only when the config
// path cannot be determined.
func (m *Manager) ConfigDir() string {
	p, err := m.configPathOrError()
	if err != nil || p == "" {
		return ""
	}
	return filepath.Dir(p)
}

// configPathOrError resolves the config file path, returning an error with the
// root cause when the path cannot be determined.
func (m *Manager) configPathOrError() (string, error) {
	used := m.v.ConfigFileUsed()
	if used != "" {
		return used, nil
	}
	// Return the configured path if no file has been loaded yet
	if m.configPath != "" {
		return m.configPath, nil
	}
	// Return default XDG path
	defaultPath, err := paths.ConfigFile()
	if err != nil {
		return "", fmt.Errorf("failed to determine config path: %w", err)
	}
	if defaultPath == "" {
		return "", fmt.Errorf("failed to determine config path: resolved to empty string")
	}
	return defaultPath, nil
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
