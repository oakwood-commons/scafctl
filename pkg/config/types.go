// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package config provides application configuration management using Viper.
// It supports configuration files, environment variables, and CLI flag overrides.
package config

import "fmt"

// CurrentConfigVersion is the current config file version.
const CurrentConfigVersion = 1

// Config represents the application configuration.
type Config struct {
	Version    int              `json:"version,omitempty" yaml:"version,omitempty" mapstructure:"version" doc:"Config file version" example:"1" maximum:"100"`
	Catalogs   []CatalogConfig  `json:"catalogs" yaml:"catalogs" mapstructure:"catalogs" doc:"Configured catalogs" maxItems:"50"`
	Settings   Settings         `json:"settings" yaml:"settings" mapstructure:"settings" doc:"Application settings"`
	Logging    LoggingConfig    `json:"logging,omitempty" yaml:"logging,omitempty" mapstructure:"logging" doc:"Logging configuration"`
	Telemetry  TelemetryConfig  `json:"telemetry,omitempty" yaml:"telemetry,omitempty" mapstructure:"telemetry" doc:"OpenTelemetry configuration"`
	HTTPClient HTTPClientConfig `json:"httpClient,omitempty" yaml:"httpClient,omitempty" mapstructure:"httpClient" doc:"Global HTTP client configuration"`
	CEL        CELConfig        `json:"cel,omitempty" yaml:"cel,omitempty" mapstructure:"cel" doc:"CEL expression engine configuration"`
	Resolver   ResolverConfig   `json:"resolver,omitempty" yaml:"resolver,omitempty" mapstructure:"resolver" doc:"Resolver executor configuration"`
	Action     ActionConfig     `json:"action,omitempty" yaml:"action,omitempty" mapstructure:"action" doc:"Action executor configuration"`
	Auth       GlobalAuthConfig `json:"auth,omitempty" yaml:"auth,omitempty" mapstructure:"auth" doc:"Authentication handler configuration"`
	Build      BuildConfig      `json:"build,omitempty" yaml:"build,omitempty" mapstructure:"build" doc:"Build command configuration"`
}

// CatalogConfig represents a single catalog configuration.
type CatalogConfig struct {
	Name       string            `json:"name" yaml:"name" mapstructure:"name" doc:"Catalog name" example:"internal" maxLength:"255"`
	Type       string            `json:"type" yaml:"type" mapstructure:"type" doc:"Catalog type" example:"filesystem" maxLength:"50"`
	Path       string            `json:"path,omitempty" yaml:"path,omitempty" mapstructure:"path" doc:"Path for filesystem catalogs" maxLength:"4096"`
	URL        string            `json:"url,omitempty" yaml:"url,omitempty" mapstructure:"url" doc:"URL for remote catalogs" maxLength:"2048"`
	Auth       *AuthConfig       `json:"auth,omitempty" yaml:"auth,omitempty" mapstructure:"auth" doc:"Authentication configuration"`
	Metadata   map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty" mapstructure:"metadata" doc:"Additional metadata"`
	HTTPClient *HTTPClientConfig `json:"httpClient,omitempty" yaml:"httpClient,omitempty" mapstructure:"httpClient" doc:"Per-catalog HTTP client overrides (inherits from global)"`
}

// AuthConfig holds authentication settings for a catalog.
type AuthConfig struct {
	Type        string `json:"type" yaml:"type" mapstructure:"type" doc:"Auth type" example:"token" maxLength:"50"`
	TokenEnvVar string `json:"tokenEnvVar,omitempty" yaml:"tokenEnvVar,omitempty" mapstructure:"tokenEnvVar" doc:"Environment variable containing token" maxLength:"255"`
}

// Settings holds application-wide settings.
type Settings struct {
	DefaultCatalog string `json:"defaultCatalog,omitempty" yaml:"defaultCatalog,omitempty" mapstructure:"defaultCatalog" doc:"Default catalog name" example:"default" maxLength:"255"`
	NoColor        bool   `json:"noColor,omitempty" yaml:"noColor,omitempty" mapstructure:"noColor" doc:"Disable colored output"`
	Quiet          bool   `json:"quiet,omitempty" yaml:"quiet,omitempty" mapstructure:"quiet" doc:"Suppress non-essential output"`
}

// TelemetryConfig holds OpenTelemetry configuration.
type TelemetryConfig struct {
	// Endpoint is the OTLP gRPC exporter endpoint (e.g. localhost:4317).
	// Equivalent to the OTEL_EXPORTER_OTLP_ENDPOINT environment variable.
	// When empty, traces are written to stderr and no OTLP export occurs.
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty" mapstructure:"endpoint" doc:"OTLP gRPC exporter endpoint" example:"localhost:4317" maxLength:"2048"`

	// Insecure disables TLS for the OTLP gRPC connection. Useful for local
	// development setups where the collector has no TLS configured.
	Insecure bool `json:"insecure,omitempty" yaml:"insecure,omitempty" mapstructure:"insecure" doc:"Disable TLS for OTLP gRPC connection (development only)"`

	// ServiceName overrides the OTel resource service.name attribute.
	// Defaults to the binary name (scafctl).
	ServiceName string `json:"serviceName,omitempty" yaml:"serviceName,omitempty" mapstructure:"serviceName" doc:"OTel resource service.name override" example:"scafctl-ci" maxLength:"255"`

	// SamplerType controls the trace sampler. Supported values: always_on, always_off, traceidratio.
	// Defaults to always_on.
	SamplerType string `json:"samplerType,omitempty" yaml:"samplerType,omitempty" mapstructure:"samplerType" doc:"Trace sampler type (always_on, always_off, traceidratio)" example:"always_on" maxLength:"50"`

	// SamplerArg is the argument passed to the sampler (e.g. ratio for traceidratio).
	SamplerArg float64 `json:"samplerArg,omitempty" yaml:"samplerArg,omitempty" mapstructure:"samplerArg" doc:"Sampler argument (e.g. 0.1 for 10% sampling ratio)" example:"1.0"`
}

// LoggingConfig holds logging configuration.
type LoggingConfig struct {
	Level           string `json:"level,omitempty" yaml:"level,omitempty" mapstructure:"level" doc:"Log level (none, error, warn, info, debug, trace, or a numeric V-level)" example:"none" maxLength:"10"`
	Format          string `json:"format,omitempty" yaml:"format,omitempty" mapstructure:"format" doc:"Output format (console, json, text)" example:"console" maxLength:"10"`
	Timestamps      bool   `json:"timestamps,omitempty" yaml:"timestamps,omitempty" mapstructure:"timestamps" doc:"Include timestamps in logs"`
	EnableProfiling bool   `json:"enableProfiling,omitempty" yaml:"enableProfiling,omitempty" mapstructure:"enableProfiling" doc:"Enable profiling (unhides --pprof flag)"`
}

// LoggingFormatJSON is the JSON log format.
const LoggingFormatJSON = "json"

// LoggingFormatText is the text log format (alias for console).
const LoggingFormatText = "text"

// LoggingFormatConsole is the human-readable console log format.
const LoggingFormatConsole = "console"

// CatalogType constants define the supported catalog types.
const (
	CatalogTypeFilesystem = "filesystem"
	CatalogTypeOCI        = "oci"
	CatalogTypeHTTP       = "http"
)

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

// SetDefaultCatalog sets the default catalog by name.
// Returns an error if the catalog doesn't exist.
func (c *Config) SetDefaultCatalog(name string) error {
	if name == "" {
		c.Settings.DefaultCatalog = ""
		return nil
	}
	if _, exists := c.GetCatalog(name); !exists {
		return fmt.Errorf("catalog %q not found", name)
	}
	c.Settings.DefaultCatalog = name
	return nil
}

// ValidCatalogTypes returns the list of valid catalog types.
func ValidCatalogTypes() []string {
	return []string{CatalogTypeFilesystem, CatalogTypeOCI, CatalogTypeHTTP}
}

// IsValidCatalogType returns true if the given type is valid.
func IsValidCatalogType(t string) bool {
	for _, valid := range ValidCatalogTypes() {
		if t == valid {
			return true
		}
	}
	return false
}

// HTTPClientConfig holds HTTP client configuration settings.
// All duration fields use string format (e.g., "30s", "5m", "1h").
type HTTPClientConfig struct {
	// Timeout is the maximum time to wait for a request to complete
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty" mapstructure:"timeout" doc:"HTTP request timeout" example:"30s" maxLength:"20"`

	// Retry settings
	RetryMax     int    `json:"retryMax,omitempty" yaml:"retryMax,omitempty" mapstructure:"retryMax" doc:"Maximum number of retries" example:"3" maximum:"10"`
	RetryWaitMin string `json:"retryWaitMin,omitempty" yaml:"retryWaitMin,omitempty" mapstructure:"retryWaitMin" doc:"Minimum wait time between retries" example:"1s" maxLength:"20"`
	RetryWaitMax string `json:"retryWaitMax,omitempty" yaml:"retryWaitMax,omitempty" mapstructure:"retryWaitMax" doc:"Maximum wait time between retries" example:"30s" maxLength:"20"`

	// Cache settings
	EnableCache      *bool  `json:"enableCache,omitempty" yaml:"enableCache,omitempty" mapstructure:"enableCache" doc:"Enable HTTP response caching"`
	CacheType        string `json:"cacheType,omitempty" yaml:"cacheType,omitempty" mapstructure:"cacheType" doc:"Cache type: memory or filesystem" example:"filesystem" maxLength:"20"`
	CacheDir         string `json:"cacheDir,omitempty" yaml:"cacheDir,omitempty" mapstructure:"cacheDir" doc:"Directory for filesystem cache" example:"~/.scafctl/http-cache" maxLength:"4096"`
	CacheTTL         string `json:"cacheTTL,omitempty" yaml:"cacheTTL,omitempty" mapstructure:"cacheTTL" doc:"Time-to-live for cached responses" example:"10m" maxLength:"20"`
	CacheKeyPrefix   string `json:"cacheKeyPrefix,omitempty" yaml:"cacheKeyPrefix,omitempty" mapstructure:"cacheKeyPrefix" doc:"Prefix for cache keys" example:"scafctl:" maxLength:"50"`
	MaxCacheFileSize int64  `json:"maxCacheFileSize,omitempty" yaml:"maxCacheFileSize,omitempty" mapstructure:"maxCacheFileSize" doc:"Maximum size for a single cached file in bytes" example:"10485760"`
	MemoryCacheSize  int    `json:"memoryCacheSize,omitempty" yaml:"memoryCacheSize,omitempty" mapstructure:"memoryCacheSize" doc:"Maximum number of entries in memory cache" example:"1000" maximum:"100000"`

	// Circuit breaker settings
	EnableCircuitBreaker              *bool  `json:"enableCircuitBreaker,omitempty" yaml:"enableCircuitBreaker,omitempty" mapstructure:"enableCircuitBreaker" doc:"Enable circuit breaker pattern"`
	CircuitBreakerMaxFailures         int    `json:"circuitBreakerMaxFailures,omitempty" yaml:"circuitBreakerMaxFailures,omitempty" mapstructure:"circuitBreakerMaxFailures" doc:"Failures before opening circuit" example:"5" maximum:"100"`
	CircuitBreakerOpenTimeout         string `json:"circuitBreakerOpenTimeout,omitempty" yaml:"circuitBreakerOpenTimeout,omitempty" mapstructure:"circuitBreakerOpenTimeout" doc:"Wait time before half-open state" example:"30s" maxLength:"20"`
	CircuitBreakerHalfOpenMaxRequests int    `json:"circuitBreakerHalfOpenMaxRequests,omitempty" yaml:"circuitBreakerHalfOpenMaxRequests,omitempty" mapstructure:"circuitBreakerHalfOpenMaxRequests" doc:"Successful requests in half-open before closing" example:"1" maximum:"10"`

	// Compression
	EnableCompression *bool `json:"enableCompression,omitempty" yaml:"enableCompression,omitempty" mapstructure:"enableCompression" doc:"Enable automatic gzip compression"`
}

// HTTPClientCacheType constants define the supported HTTP cache types.
const (
	HTTPClientCacheTypeMemory     = "memory"
	HTTPClientCacheTypeFilesystem = "filesystem"
)

// ValidHTTPClientCacheTypes returns the list of valid HTTP cache types.
func ValidHTTPClientCacheTypes() []string {
	return []string{HTTPClientCacheTypeMemory, HTTPClientCacheTypeFilesystem}
}

// IsValidHTTPClientCacheType returns true if the given cache type is valid.
func IsValidHTTPClientCacheType(t string) bool {
	for _, valid := range ValidHTTPClientCacheTypes() {
		if t == valid {
			return true
		}
	}
	return false
}

// CELConfig holds CEL expression engine configuration.
type CELConfig struct {
	// CacheSize is the maximum number of compiled programs to cache
	CacheSize int `json:"cacheSize,omitempty" yaml:"cacheSize,omitempty" mapstructure:"cacheSize" doc:"CEL program cache size" example:"10000" maximum:"1000000"`

	// CostLimit is the cost limit for expression evaluation (0 = disabled)
	// Prevents runaway expressions from consuming resources
	CostLimit int64 `json:"costLimit,omitempty" yaml:"costLimit,omitempty" mapstructure:"costLimit" doc:"CEL cost limit (0=disabled)" example:"1000000"`

	// UseASTBasedCaching enables AST-based cache key generation for better hit rates
	// Expressions with same structure share cache entries
	UseASTBasedCaching bool `json:"useASTBasedCaching,omitempty" yaml:"useASTBasedCaching,omitempty" mapstructure:"useASTBasedCaching" doc:"Enable AST-based cache keys"`

	// EnableMetrics enables expression metrics collection
	EnableMetrics *bool `json:"enableMetrics,omitempty" yaml:"enableMetrics,omitempty" mapstructure:"enableMetrics" doc:"Enable expression metrics"`
}

// ResolverConfig holds resolver executor configuration.
type ResolverConfig struct {
	// Timeout is the default timeout per resolver execution
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty" mapstructure:"timeout" doc:"Default resolver timeout" example:"30s" maxLength:"20"`

	// PhaseTimeout is the maximum time for each resolution phase
	PhaseTimeout string `json:"phaseTimeout,omitempty" yaml:"phaseTimeout,omitempty" mapstructure:"phaseTimeout" doc:"Maximum phase duration" example:"5m" maxLength:"20"`

	// MaxConcurrency is the maximum concurrent resolvers per phase (0 = unlimited)
	MaxConcurrency int `json:"maxConcurrency,omitempty" yaml:"maxConcurrency,omitempty" mapstructure:"maxConcurrency" doc:"Max concurrent resolvers (0=unlimited)" example:"10" maximum:"1000"`

	// WarnValueSize is the warn threshold in bytes (0 = disabled)
	WarnValueSize int64 `json:"warnValueSize,omitempty" yaml:"warnValueSize,omitempty" mapstructure:"warnValueSize" doc:"Warn threshold in bytes" example:"1048576"`

	// MaxValueSize is the max value size in bytes (0 = disabled)
	MaxValueSize int64 `json:"maxValueSize,omitempty" yaml:"maxValueSize,omitempty" mapstructure:"maxValueSize" doc:"Max value size in bytes" example:"10485760"`

	// ValidateAll enables collecting all errors instead of stopping at first
	ValidateAll bool `json:"validateAll,omitempty" yaml:"validateAll,omitempty" mapstructure:"validateAll" doc:"Collect all errors vs stop at first"`
}

// ActionConfig holds action executor configuration.
type ActionConfig struct {
	// DefaultTimeout is the default timeout per action execution
	DefaultTimeout string `json:"defaultTimeout,omitempty" yaml:"defaultTimeout,omitempty" mapstructure:"defaultTimeout" doc:"Default action timeout" example:"5m" maxLength:"20"`

	// GracePeriod is the cancellation grace period
	GracePeriod string `json:"gracePeriod,omitempty" yaml:"gracePeriod,omitempty" mapstructure:"gracePeriod" doc:"Cancellation grace period" example:"30s" maxLength:"20"`

	// MaxConcurrency is the max concurrent actions (0 = unlimited)
	MaxConcurrency int `json:"maxConcurrency,omitempty" yaml:"maxConcurrency,omitempty" mapstructure:"maxConcurrency" doc:"Max concurrent actions (0=unlimited)" example:"5" maximum:"100"`
}

// GlobalAuthConfig holds authentication handler configuration.
type GlobalAuthConfig struct {
	// Entra contains Microsoft Entra ID configuration.
	Entra *EntraAuthConfig `json:"entra,omitempty" yaml:"entra,omitempty" mapstructure:"entra" doc:"Microsoft Entra ID configuration"`

	// GitHub contains GitHub authentication configuration.
	GitHub *GitHubAuthConfig `json:"github,omitempty" yaml:"github,omitempty" mapstructure:"github" doc:"GitHub authentication configuration"`

	// GCP contains Google Cloud Platform authentication configuration.
	GCP *GCPAuthConfig `json:"gcp,omitempty" yaml:"gcp,omitempty" mapstructure:"gcp" doc:"Google Cloud Platform authentication configuration"`
}

// EntraAuthConfig contains Entra-specific configuration.
type EntraAuthConfig struct {
	// ClientID overrides the default application ID.
	// If not set, uses the default scafctl public client ID.
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" mapstructure:"clientId" doc:"Azure application ID" maxLength:"36"`

	// TenantID sets the default tenant for authentication.
	// Use "common" for multi-tenant, "organizations" for work/school only,
	// or a specific tenant GUID.
	TenantID string `json:"tenantId,omitempty" yaml:"tenantId,omitempty" mapstructure:"tenantId" doc:"Default Azure tenant ID" example:"common" maxLength:"36"`

	// DefaultScopes are requested during login if not specified on command line.
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" mapstructure:"defaultScopes" doc:"Default OAuth scopes" maxItems:"20"`
}

// GitHubAuthConfig contains GitHub-specific configuration.
type GitHubAuthConfig struct {
	// ClientID overrides the default GitHub OAuth App client ID.
	// If not set, uses the default scafctl OAuth App client ID.
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" mapstructure:"clientId" doc:"GitHub OAuth App client ID" maxLength:"40"`

	// Hostname sets the GitHub hostname for enterprise server (GHES).
	// Defaults to "github.com".
	Hostname string `json:"hostname,omitempty" yaml:"hostname,omitempty" mapstructure:"hostname" doc:"GitHub hostname" example:"github.com" maxLength:"253"`

	// DefaultScopes are requested during login if not specified on command line.
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" mapstructure:"defaultScopes" doc:"Default OAuth scopes" maxItems:"20"`
}

// GCPAuthConfig contains GCP-specific configuration.
type GCPAuthConfig struct {
	// ClientID overrides the default OAuth 2.0 client ID.
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" mapstructure:"clientId" doc:"OAuth 2.0 client ID for interactive authentication" maxLength:"255"`

	// ClientSecret overrides the default OAuth 2.0 client secret.
	ClientSecret string `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty" mapstructure:"clientSecret" doc:"OAuth 2.0 client secret (not confidential for desktop apps)" maxLength:"255"` //nolint:gosec // G117: not a hardcoded credential, it's a config field

	// DefaultScopes are requested during login if not specified on command line.
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" mapstructure:"defaultScopes" doc:"Default OAuth scopes for GCP authentication" maxItems:"20"`

	// ImpersonateServiceAccount is the service account email to impersonate.
	ImpersonateServiceAccount string `json:"impersonateServiceAccount,omitempty" yaml:"impersonateServiceAccount,omitempty" mapstructure:"impersonateServiceAccount" doc:"Service account email to impersonate" example:"deploy@my-project.iam.gserviceaccount.com" maxLength:"255"`

	// Project is the default GCP project ID.
	Project string `json:"project,omitempty" yaml:"project,omitempty" mapstructure:"project" doc:"Default GCP project ID" example:"my-project-123" maxLength:"64"`
}

// BuildConfig holds build command configuration.
type BuildConfig struct {
	// EnableCache enables build-level caching to skip redundant builds.
	EnableCache *bool `json:"enableCache,omitempty" yaml:"enableCache,omitempty" mapstructure:"enableCache" doc:"Enable build-level caching"`

	// CacheDir is the directory for storing build cache entries.
	CacheDir string `json:"cacheDir,omitempty" yaml:"cacheDir,omitempty" mapstructure:"cacheDir" doc:"Build cache directory" maxLength:"4096"`

	// AutoCacheRemoteArtifacts automatically caches remote catalog fetches into the local catalog.
	AutoCacheRemoteArtifacts *bool `json:"autoCacheRemoteArtifacts,omitempty" yaml:"autoCacheRemoteArtifacts,omitempty" mapstructure:"autoCacheRemoteArtifacts" doc:"Auto-cache remote catalog fetches locally"`

	// PluginCacheDir is the directory for cached plugin binaries.
	PluginCacheDir string `json:"pluginCacheDir,omitempty" yaml:"pluginCacheDir,omitempty" mapstructure:"pluginCacheDir" doc:"Plugin binary cache directory" maxLength:"4096"`
}

// IsCacheEnabled returns whether build caching is enabled (defaults to true).
func (b *BuildConfig) IsCacheEnabled() bool {
	if b.EnableCache == nil {
		return true
	}
	return *b.EnableCache
}

// IsAutoCacheRemoteArtifacts returns whether remote artifacts are auto-cached (defaults to true).
func (b *BuildConfig) IsAutoCacheRemoteArtifacts() bool {
	if b.AutoCacheRemoteArtifacts == nil {
		return true
	}
	return *b.AutoCacheRemoteArtifacts
}
