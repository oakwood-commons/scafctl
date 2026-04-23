// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package config provides application configuration management using Viper.
// It supports configuration files, environment variables, and CLI flag overrides.
package config

import (
	"fmt"

	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
)

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
	GoTemplate GoTemplateConfig `json:"goTemplate,omitempty" yaml:"goTemplate,omitempty" mapstructure:"goTemplate" doc:"Go template engine configuration"`
	Resolver   ResolverConfig   `json:"resolver,omitempty" yaml:"resolver,omitempty" mapstructure:"resolver" doc:"Resolver executor configuration"`
	Action     ActionConfig     `json:"action,omitempty" yaml:"action,omitempty" mapstructure:"action" doc:"Action executor configuration"`
	Auth       GlobalAuthConfig `json:"auth,omitempty" yaml:"auth,omitempty" mapstructure:"auth" doc:"Authentication handler configuration"`
	Build      BuildConfig      `json:"build,omitempty" yaml:"build,omitempty" mapstructure:"build" doc:"Build command configuration"`
	APIServer  APIServerConfig  `json:"apiServer,omitempty" yaml:"apiServer,omitempty" mapstructure:"apiServer" doc:"REST API server configuration"`
	Discovery  DiscoveryConfig  `json:"discovery,omitempty" yaml:"discovery,omitempty" mapstructure:"discovery" doc:"Auto-discovery configuration"`
}

// DiscoveryStrategy controls how a remote catalog discovers available artifacts.
type DiscoveryStrategy string

// Discovery strategy constants.
const (
	// DiscoveryStrategyAuto tries API enumeration first, falls back to the
	// catalog-index artifact on ErrEnumerationNotSupported. This is the default.
	DiscoveryStrategyAuto DiscoveryStrategy = "auto"

	// DiscoveryStrategyIndex skips API enumeration and fetches the catalog-index
	// artifact directly. Fastest path; works without authentication.
	DiscoveryStrategyIndex DiscoveryStrategy = "index"

	// DiscoveryStrategyAPI always uses API enumeration and never falls back to
	// the catalog-index artifact.
	DiscoveryStrategyAPI DiscoveryStrategy = "api"
)

// ValidDiscoveryStrategies returns the list of valid discovery strategies.
func ValidDiscoveryStrategies() []string {
	return []string{string(DiscoveryStrategyAuto), string(DiscoveryStrategyIndex), string(DiscoveryStrategyAPI)}
}

// IsValidDiscoveryStrategy returns true if the given strategy is valid.
func IsValidDiscoveryStrategy(s string) bool {
	for _, valid := range ValidDiscoveryStrategies() {
		if s == valid {
			return true
		}
	}
	return false
}

// CatalogConfig represents a single catalog configuration.
type CatalogConfig struct {
	Name              string            `json:"name" yaml:"name" mapstructure:"name" doc:"Catalog name" example:"internal" maxLength:"255"`
	Type              string            `json:"type" yaml:"type" mapstructure:"type" doc:"Catalog type" example:"filesystem" maxLength:"50"`
	Path              string            `json:"path,omitempty" yaml:"path,omitempty" mapstructure:"path" doc:"Path for filesystem catalogs" maxLength:"4096" example:"~/.config/scafctl/catalog"`
	URL               string            `json:"url,omitempty" yaml:"url,omitempty" mapstructure:"url" doc:"URL for remote catalogs" maxLength:"2048" example:"https://catalog.example.com"`
	Auth              *AuthConfig       `json:"auth,omitempty" yaml:"auth,omitempty" mapstructure:"auth" doc:"Authentication configuration"`
	AuthProvider      string            `json:"authProvider,omitempty" yaml:"authProvider,omitempty" mapstructure:"authProvider" doc:"Auth handler name for automatic token injection (e.g. github, gcp, entra)" maxLength:"64" example:"github"`
	AuthScope         string            `json:"authScope,omitempty" yaml:"authScope,omitempty" mapstructure:"authScope" doc:"OAuth scope for auth provider token requests" maxLength:"1024" example:"https://management.azure.com/.default"`
	DiscoveryStrategy DiscoveryStrategy `json:"discoveryStrategy,omitempty" yaml:"discoveryStrategy,omitempty" mapstructure:"discoveryStrategy" doc:"How artifacts are discovered: auto (API then index fallback), index (index only), api (API only)" example:"auto" maxLength:"10"`
	Metadata          map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty" mapstructure:"metadata" doc:"Additional metadata"`
	HTTPClient        *HTTPClientConfig `json:"httpClient,omitempty" yaml:"httpClient,omitempty" mapstructure:"httpClient" doc:"Per-catalog HTTP client overrides (inherits from global)"`
}

// AuthConfig holds authentication settings for a catalog.
type AuthConfig struct {
	Type        string `json:"type" yaml:"type" mapstructure:"type" doc:"Auth type" example:"token" maxLength:"50"`
	TokenEnvVar string `json:"tokenEnvVar,omitempty" yaml:"tokenEnvVar,omitempty" mapstructure:"tokenEnvVar" doc:"Environment variable containing token" maxLength:"255" example:"SCAFCTL_TOKEN"`
}

// Settings holds application-wide settings.
type Settings struct {
	DefaultCatalog string              `json:"defaultCatalog,omitempty" yaml:"defaultCatalog,omitempty" mapstructure:"defaultCatalog" doc:"Default catalog name" example:"default" maxLength:"255"`
	NoColor        bool                `json:"noColor,omitempty" yaml:"noColor,omitempty" mapstructure:"noColor" doc:"Disable colored output"`
	Quiet          bool                `json:"quiet,omitempty" yaml:"quiet,omitempty" mapstructure:"quiet" doc:"Suppress non-essential output"`
	VersionCheck   *VersionCheckConfig `json:"versionCheck,omitempty" yaml:"versionCheck,omitempty" mapstructure:"versionCheck" doc:"Version check configuration"`

	// RequireSecureKeyring when true causes scafctl to fail with an error if the
	// OS keyring is unavailable and the secret store would fall back to an insecure
	// file-based or environment-variable-based master key. Enable this in
	// production or shared environments to prevent silent degradation of secret
	// protection.
	RequireSecureKeyring bool `json:"requireSecureKeyring,omitempty" yaml:"requireSecureKeyring,omitempty" mapstructure:"requireSecureKeyring" doc:"Fail if OS keyring is unavailable instead of falling back to insecure storage"`

	// DisableOfficialCatalog prevents the built-in official catalog from being
	// added to the catalog chain. Embedders can set this when their CLI should
	// not fall back to the scafctl community catalog.
	DisableOfficialCatalog bool `json:"disableOfficialCatalog,omitempty" yaml:"disableOfficialCatalog,omitempty" mapstructure:"disableOfficialCatalog" doc:"Disable the built-in official catalog"`
}

// VersionCheckConfig holds version check configuration.
type VersionCheckConfig struct {
	// Timeout overrides the version check HTTP timeout (default: 5s).
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty" mapstructure:"timeout" doc:"Version check timeout" example:"5s" maxLength:"20"`
	// Enabled can disable the automatic version check.
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty" mapstructure:"enabled" doc:"Enable version check"`
}

// TelemetryConfig holds OpenTelemetry configuration.
type TelemetryConfig struct {
	// Endpoint is the OTLP gRPC exporter endpoint (e.g. localhost:4317).
	// Equivalent to the OTEL_EXPORTER_OTLP_ENDPOINT environment variable.
	// When empty, tracing and OTel log export are disabled (noop providers).
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

// Reserved catalog names. These names are owned by the embedded defaults and
// their configuration values are always enforced at load time. Users cannot
// override fields on reserved catalogs -- if they need custom settings they
// must use a different name.
const (
	// CatalogNameLocal is the local filesystem catalog, always first in the chain.
	CatalogNameLocal = "local"

	// CatalogNameOfficial is the official OCI catalog, always last in the chain.
	CatalogNameOfficial = "official"
)

// IsReservedCatalogName reports whether name is a reserved catalog name whose
// configuration is enforced by the embedded defaults.
func IsReservedCatalogName(name string) bool {
	return name == CatalogNameLocal || name == CatalogNameOfficial
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
	MaxCacheFileSize int64  `json:"maxCacheFileSize,omitempty" yaml:"maxCacheFileSize,omitempty" mapstructure:"maxCacheFileSize" doc:"Maximum size for a single cached file in bytes" maximum:"1073741824" example:"10485760"`
	MemoryCacheSize  int    `json:"memoryCacheSize,omitempty" yaml:"memoryCacheSize,omitempty" mapstructure:"memoryCacheSize" doc:"Maximum number of entries in memory cache" example:"1000" maximum:"100000"`

	// Circuit breaker settings
	EnableCircuitBreaker              *bool  `json:"enableCircuitBreaker,omitempty" yaml:"enableCircuitBreaker,omitempty" mapstructure:"enableCircuitBreaker" doc:"Enable circuit breaker pattern"`
	CircuitBreakerMaxFailures         int    `json:"circuitBreakerMaxFailures,omitempty" yaml:"circuitBreakerMaxFailures,omitempty" mapstructure:"circuitBreakerMaxFailures" doc:"Failures before opening circuit" example:"5" maximum:"100"`
	CircuitBreakerOpenTimeout         string `json:"circuitBreakerOpenTimeout,omitempty" yaml:"circuitBreakerOpenTimeout,omitempty" mapstructure:"circuitBreakerOpenTimeout" doc:"Wait time before half-open state" example:"30s" maxLength:"20"`
	CircuitBreakerHalfOpenMaxRequests int    `json:"circuitBreakerHalfOpenMaxRequests,omitempty" yaml:"circuitBreakerHalfOpenMaxRequests,omitempty" mapstructure:"circuitBreakerHalfOpenMaxRequests" doc:"Successful requests in half-open before closing" example:"1" maximum:"10"`

	// Compression
	EnableCompression *bool `json:"enableCompression,omitempty" yaml:"enableCompression,omitempty" mapstructure:"enableCompression" doc:"Enable automatic gzip compression"`

	// AllowPrivateIPs controls whether HTTP requests to private, loopback, and
	// link-local IP addresses are permitted. Checked against IP literals only
	// (hostnames are not pre-resolved). When false (default), requests to RFC 1918
	// ranges (10.x, 172.16.x, 192.168.x), loopback (127.x, ::1), link-local
	// (169.254.x), and CGNAT (100.64.x) are blocked. Set to true to allow private
	// network access (e.g., for on-premises endpoints or local development).
	AllowPrivateIPs *bool `json:"allowPrivateIPs,omitempty" yaml:"allowPrivateIPs,omitempty" mapstructure:"allowPrivateIPs" doc:"Allow HTTP requests to private/loopback/link-local IP literals (default: false). Set true to allow private network access." example:"false"`

	// MaxResponseBodySize is the maximum number of bytes the HTTP provider will
	// read from a single response body. Prevents denial-of-service via unbounded
	// responses from malicious or misconfigured servers. Defaults to 100 MB.
	MaxResponseBodySize int64 `json:"maxResponseBodySize,omitempty" yaml:"maxResponseBodySize,omitempty" mapstructure:"maxResponseBodySize" doc:"Maximum HTTP response body size in bytes (default: 104857600)" maximum:"1073741824" example:"104857600"`
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
	CostLimit int64 `json:"costLimit,omitempty" yaml:"costLimit,omitempty" mapstructure:"costLimit" doc:"CEL cost limit (0=disabled)" maximum:"100000000" example:"1000000"`

	// UseASTBasedCaching enables AST-based cache key generation for better hit rates
	// Expressions with same structure share cache entries
	UseASTBasedCaching bool `json:"useASTBasedCaching,omitempty" yaml:"useASTBasedCaching,omitempty" mapstructure:"useASTBasedCaching" doc:"Enable AST-based cache keys"`

	// EnableMetrics enables expression metrics collection
	EnableMetrics *bool `json:"enableMetrics,omitempty" yaml:"enableMetrics,omitempty" mapstructure:"enableMetrics" doc:"Enable expression metrics"`
}

// GoTemplateConfig holds Go template engine configuration.
type GoTemplateConfig struct {
	// CacheSize is the maximum number of compiled templates to cache
	CacheSize int `json:"cacheSize,omitempty" yaml:"cacheSize,omitempty" mapstructure:"cacheSize" doc:"Go template cache size" example:"10000" maximum:"1000000"`

	// EnableMetrics enables template metrics collection
	EnableMetrics *bool `json:"enableMetrics,omitempty" yaml:"enableMetrics,omitempty" mapstructure:"enableMetrics" doc:"Enable template metrics"`

	// AllowEnvFunctions enables the sprig 'env' and 'expandenv' template functions.
	// When false (the default), these functions are removed from the template
	// function map to prevent solution files from exfiltrating process secrets
	// (e.g. GITHUB_TOKEN, AWS_SECRET_ACCESS_KEY) via {{ env "SECRET" }}.
	// Set to true only if your solutions explicitly require reading env vars.
	AllowEnvFunctions bool `json:"allowEnvFunctions,omitempty" yaml:"allowEnvFunctions,omitempty" mapstructure:"allowEnvFunctions" doc:"Allow sprig env/expandenv functions in Go templates (default: false). Enable only if solutions require reading env vars."`
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
	WarnValueSize int64 `json:"warnValueSize,omitempty" yaml:"warnValueSize,omitempty" mapstructure:"warnValueSize" doc:"Warn threshold in bytes" maximum:"1073741824" example:"1048576"`

	// MaxValueSize is the max value size in bytes (0 = disabled)
	MaxValueSize int64 `json:"maxValueSize,omitempty" yaml:"maxValueSize,omitempty" mapstructure:"maxValueSize" doc:"Max value size in bytes" maximum:"1073741824" example:"10485760"`

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

	// OutputDir is the default target directory for action file operations.
	// When set, actions resolve relative paths against this directory instead of CWD.
	// Can be overridden by the --output-dir CLI flag.
	OutputDir string `json:"outputDir,omitempty" yaml:"outputDir,omitempty" mapstructure:"outputDir" doc:"Default target directory for action file operations" maxLength:"4096" example:"/tmp/output"`
}

// GlobalAuthConfig holds authentication handler configuration.
type GlobalAuthConfig struct {
	// HTTPClient optionally overrides the global HTTP client settings for all auth handlers.
	// Individual handler configs (Entra, GitHub, GCP) can further override these.
	HTTPClient *HTTPClientConfig `json:"httpClient,omitempty" yaml:"httpClient,omitempty" mapstructure:"httpClient" doc:"HTTP client overrides for auth handlers"`

	// Entra contains Microsoft Entra ID configuration.
	Entra *EntraAuthConfig `json:"entra,omitempty" yaml:"entra,omitempty" mapstructure:"entra" doc:"Microsoft Entra ID configuration"`

	// GitHub contains GitHub authentication configuration.
	GitHub *GitHubAuthConfig `json:"github,omitempty" yaml:"github,omitempty" mapstructure:"github" doc:"GitHub authentication configuration"`

	// GCP contains Google Cloud Platform authentication configuration.
	GCP *GCPAuthConfig `json:"gcp,omitempty" yaml:"gcp,omitempty" mapstructure:"gcp" doc:"Google Cloud Platform authentication configuration"`

	// CustomOAuth2 contains user-defined OAuth2 auth handlers.
	CustomOAuth2 []CustomOAuth2Config `json:"customOAuth2,omitempty" yaml:"customOAuth2,omitempty" mapstructure:"customOAuth2" doc:"User-defined OAuth2 auth handlers for any OAuth2 service" maxItems:"20"`
}

// EntraAuthConfig contains Entra-specific configuration.
type EntraAuthConfig struct {
	// HTTPClient optionally overrides HTTP settings for Entra auth requests.
	HTTPClient *HTTPClientConfig `json:"httpClient,omitempty" yaml:"httpClient,omitempty" mapstructure:"httpClient" doc:"HTTP client overrides for Entra"`

	// ClientID overrides the default application ID.
	// If not set, uses the default scafctl public client ID.
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" mapstructure:"clientId" doc:"Azure application ID" maxLength:"36" example:"00000000-0000-0000-0000-000000000000"`

	// TenantID sets the default tenant for authentication.
	// Use "common" for multi-tenant, "organizations" for work/school only,
	// or a specific tenant GUID.
	TenantID string `json:"tenantId,omitempty" yaml:"tenantId,omitempty" mapstructure:"tenantId" doc:"Default Azure tenant ID" example:"common" maxLength:"36"`

	// Authority is the Azure AD authority URL.
	// Defaults to https://login.microsoftonline.com
	Authority string `json:"authority,omitempty" yaml:"authority,omitempty" mapstructure:"authority" doc:"Azure AD authority URL" maxLength:"256" example:"https://login.microsoftonline.com"`

	// DefaultScopes are requested during login if not specified on command line.
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" mapstructure:"defaultScopes" doc:"Default OAuth scopes" maxItems:"20"`
}

// GitHubAuthConfig contains GitHub-specific configuration.
type GitHubAuthConfig struct {
	// HTTPClient optionally overrides HTTP settings for GitHub auth requests.
	HTTPClient *HTTPClientConfig `json:"httpClient,omitempty" yaml:"httpClient,omitempty" mapstructure:"httpClient" doc:"HTTP client overrides for GitHub"`

	// ClientID overrides the default GitHub OAuth App client ID.
	// If not set, uses the default scafctl OAuth App client ID.
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" mapstructure:"clientId" doc:"GitHub OAuth App client ID" maxLength:"40" example:"Iv1.abc123def456"`

	// ClientSecret is the GitHub OAuth App client secret.
	// Required for the interactive (browser authorization code + PKCE) flow.
	// When not set, the interactive flow automatically uses device code with
	// browser auto-open — the same behaviour as 'gh auth login'.
	ClientSecret string `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty" mapstructure:"clientSecret" doc:"GitHub OAuth App client secret (required for browser auth code flow)" maxLength:"64"` //nolint:gosec // G117: config field, not a hardcoded credential

	// Hostname sets the GitHub hostname for enterprise server (GHES).
	// Defaults to "github.com".
	Hostname string `json:"hostname,omitempty" yaml:"hostname,omitempty" mapstructure:"hostname" doc:"GitHub hostname" example:"github.com" maxLength:"253"`

	// DefaultScopes are requested during login if not specified on command line.
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" mapstructure:"defaultScopes" doc:"Default OAuth scopes" maxItems:"20"`

	// AppID is the GitHub App ID for the installation token flow.
	AppID int64 `json:"appId,omitempty" yaml:"appId,omitempty" mapstructure:"appId" doc:"GitHub App ID for installation token flow" maximum:"1000000000" example:"123456"`

	// InstallationID is the GitHub App installation ID.
	InstallationID int64 `json:"installationId,omitempty" yaml:"installationId,omitempty" mapstructure:"installationId" doc:"GitHub App installation ID" maximum:"1000000000" example:"78901234"`

	// PrivateKeyPath is the file path to the PEM-encoded private key for the GitHub App.
	PrivateKeyPath string `json:"privateKeyPath,omitempty" yaml:"privateKeyPath,omitempty" mapstructure:"privateKeyPath" doc:"File path to PEM-encoded private key for the GitHub App" example:"/path/to/private-key.pem" maxLength:"1024"`

	// PrivateKey is the inline PEM-encoded private key for the GitHub App.
	PrivateKey string `json:"privateKey,omitempty" yaml:"privateKey,omitempty" mapstructure:"privateKey" doc:"Inline PEM-encoded private key for the GitHub App" maxLength:"8192"` //nolint:gosec // Field name, not a credential

	// PrivateKeySecretName is the name of the secret store entry containing the private key.
	PrivateKeySecretName string `json:"privateKeySecretName,omitempty" yaml:"privateKeySecretName,omitempty" mapstructure:"privateKeySecretName" doc:"Secret store key for the GitHub App private key" maxLength:"255" example:"github-app-key"`
}

// GCPAuthConfig contains GCP-specific configuration.
type GCPAuthConfig struct {
	// HTTPClient optionally overrides HTTP settings for GCP auth requests.
	HTTPClient *HTTPClientConfig `json:"httpClient,omitempty" yaml:"httpClient,omitempty" mapstructure:"httpClient" doc:"HTTP client overrides for GCP"`

	// ClientID overrides the default OAuth 2.0 client ID.
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" mapstructure:"clientId" doc:"OAuth 2.0 client ID for interactive authentication" maxLength:"255" example:"123456789.apps.googleusercontent.com"`

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
	CacheDir string `json:"cacheDir,omitempty" yaml:"cacheDir,omitempty" mapstructure:"cacheDir" doc:"Build cache directory" maxLength:"4096" example:"~/.cache/scafctl/build-cache"`

	// AutoCacheRemoteArtifacts automatically caches remote catalog fetches into the local catalog.
	AutoCacheRemoteArtifacts *bool `json:"autoCacheRemoteArtifacts,omitempty" yaml:"autoCacheRemoteArtifacts,omitempty" mapstructure:"autoCacheRemoteArtifacts" doc:"Auto-cache remote catalog fetches locally"`

	// PluginCacheDir is the directory for cached plugin binaries.
	PluginCacheDir string `json:"pluginCacheDir,omitempty" yaml:"pluginCacheDir,omitempty" mapstructure:"pluginCacheDir" doc:"Plugin binary cache directory" maxLength:"4096" example:"~/.cache/scafctl/plugins"`
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

// DiscoveryConfig holds auto-discovery preferences.
type DiscoveryConfig struct {
	// ActionFiles overrides the file names searched during "run action"
	// auto-discovery. When empty, the built-in defaults are used.
	ActionFiles []string `json:"actionFiles,omitempty" yaml:"actionFiles,omitempty" mapstructure:"actionFiles" doc:"File names for action auto-discovery" maxItems:"20"`
}

// APIServerConfig holds REST API server configuration.
type APIServerConfig struct {
	Host            string               `json:"host,omitempty" yaml:"host,omitempty" mapstructure:"host" doc:"Host to bind to (defaults to 127.0.0.1; use 0.0.0.0 to expose publicly)" example:"127.0.0.1" maxLength:"253"`
	Port            int                  `json:"port,omitempty" yaml:"port,omitempty" mapstructure:"port" doc:"Port to listen on" example:"8080" minimum:"1" maximum:"65535"`
	APIVersion      string               `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty" mapstructure:"apiVersion" doc:"API version prefix (e.g. v1, v2)" example:"v1" maxLength:"10" pattern:"^v[0-9]+$" patternDescription:"must be 'v' followed by one or more digits (e.g. v1, v2)"`
	ShutdownTimeout string               `json:"shutdownTimeout,omitempty" yaml:"shutdownTimeout,omitempty" mapstructure:"shutdownTimeout" doc:"Graceful shutdown timeout" example:"30s" maxLength:"20"`
	RequestTimeout  string               `json:"requestTimeout,omitempty" yaml:"requestTimeout,omitempty" mapstructure:"requestTimeout" doc:"Default request timeout" example:"60s" maxLength:"20"`
	BodyReadTimeout string               `json:"bodyReadTimeout,omitempty" yaml:"bodyReadTimeout,omitempty" mapstructure:"bodyReadTimeout" doc:"Default body read timeout for Huma operations" example:"15s" maxLength:"20"`
	MaxRequestSize  int64                `json:"maxRequestSize,omitempty" yaml:"maxRequestSize,omitempty" mapstructure:"maxRequestSize" doc:"Maximum request body size in bytes" maximum:"1073741824" example:"10485760"`
	TLS             APITLSConfig         `json:"tls,omitempty" yaml:"tls,omitempty" mapstructure:"tls" doc:"TLS configuration"`
	CORS            APICORSConfig        `json:"cors,omitempty" yaml:"cors,omitempty" mapstructure:"cors" doc:"CORS configuration"`
	RateLimit       APIRateLimitConfig   `json:"rateLimit,omitempty" yaml:"rateLimit,omitempty" mapstructure:"rateLimit" doc:"Rate limiting configuration"`
	Auth            APIAuthConfig        `json:"auth,omitempty" yaml:"auth,omitempty" mapstructure:"auth" doc:"Authentication configuration"`
	Compression     APICompressionConfig `json:"compression,omitempty" yaml:"compression,omitempty" mapstructure:"compression" doc:"Response compression configuration"`
	OpenAPI         APIOpenAPIConfig     `json:"openAPI,omitempty" yaml:"openAPI,omitempty" mapstructure:"openAPI" doc:"OpenAPI specification configuration (Servers field is wired; Title, Description, and other fields are reserved for future use)"`
	Profiler        APIProfilerConfig    `json:"profiler,omitempty" yaml:"profiler,omitempty" mapstructure:"profiler" doc:"Profiler configuration (reserved for future use — not yet wired into server setup)"`
	Audit           APIAuditConfig       `json:"audit,omitempty" yaml:"audit,omitempty" mapstructure:"audit" doc:"Audit logging configuration"`
	Tracing         APITracingConfig     `json:"tracing,omitempty" yaml:"tracing,omitempty" mapstructure:"tracing" doc:"OpenTelemetry tracing configuration"`
	MaxConcurrent   int                  `json:"maxConcurrent,omitempty" yaml:"maxConcurrent,omitempty" mapstructure:"maxConcurrent" doc:"Maximum concurrent in-flight requests (chi Throttle, not TCP connections)" maximum:"100000" example:"1000"`
}

// APITLSConfig holds TLS configuration for the API server.
type APITLSConfig struct {
	Enabled bool   `json:"enabled,omitempty" yaml:"enabled,omitempty" mapstructure:"enabled" doc:"Enable TLS"`
	Cert    string `json:"cert,omitempty" yaml:"cert,omitempty" mapstructure:"cert" doc:"Path to TLS certificate file" maxLength:"4096" example:"/etc/ssl/cert.pem"`
	Key     string `json:"key,omitempty" yaml:"key,omitempty" mapstructure:"key" doc:"Path to TLS private key file" maxLength:"4096" example:"/etc/ssl/key.pem"`
}

// APICORSConfig holds CORS configuration for the API server.
type APICORSConfig struct {
	Enabled        bool     `json:"enabled,omitempty" yaml:"enabled,omitempty" mapstructure:"enabled" doc:"Enable CORS"`
	AllowedOrigins []string `json:"allowedOrigins,omitempty" yaml:"allowedOrigins,omitempty" mapstructure:"allowedOrigins" doc:"Allowed origins" maxItems:"50"`
	AllowedMethods []string `json:"allowedMethods,omitempty" yaml:"allowedMethods,omitempty" mapstructure:"allowedMethods" doc:"Allowed HTTP methods" maxItems:"10"`
	AllowedHeaders []string `json:"allowedHeaders,omitempty" yaml:"allowedHeaders,omitempty" mapstructure:"allowedHeaders" doc:"Allowed headers" maxItems:"50"`
	MaxAge         int      `json:"maxAge,omitempty" yaml:"maxAge,omitempty" mapstructure:"maxAge" doc:"Max age for CORS preflight in seconds" maximum:"86400" example:"3600"`
}

// APIRateLimitConfig holds rate limiting configuration.
type APIRateLimitConfig struct {
	Global    *APIRateLimitEntry            `json:"global,omitempty" yaml:"global,omitempty" mapstructure:"global" doc:"Global rate limit"`
	Endpoints map[string]*APIRateLimitEntry `json:"endpoints,omitempty" yaml:"endpoints,omitempty" mapstructure:"endpoints" doc:"Per-endpoint rate limits (reserved for future use — not yet applied by the middleware stack)"`
}

// APIRateLimitEntry defines a rate limit rule.
type APIRateLimitEntry struct {
	MaxRequests int    `json:"maxRequests" yaml:"maxRequests" mapstructure:"maxRequests" doc:"Maximum requests in window" maximum:"100000" example:"100"`
	Window      string `json:"window" yaml:"window" mapstructure:"window" doc:"Time window for rate limit" example:"1m" maxLength:"20"`
	TrustProxy  bool   `json:"trustProxy,omitempty" yaml:"trustProxy,omitempty" mapstructure:"trustProxy" doc:"Trust X-Forwarded-For and X-Real-IP proxy headers for client IP identification. Only enable when a trusted reverse proxy sanitizes these headers; otherwise clients can spoof their IP to bypass rate limiting."`
}

// APIAuthConfig holds API authentication configuration.
type APIAuthConfig struct {
	AzureOIDC APIAzureOIDCConfig `json:"azureOIDC,omitempty" yaml:"azureOIDC,omitempty" mapstructure:"azureOIDC" doc:"Azure AD OIDC configuration"`
}

// APIAzureOIDCConfig holds Azure AD OIDC configuration.
type APIAzureOIDCConfig struct {
	Enabled  bool   `json:"enabled,omitempty" yaml:"enabled,omitempty" mapstructure:"enabled" doc:"Enable Entra OIDC authentication"`
	TenantID string `json:"tenantId,omitempty" yaml:"tenantId,omitempty" mapstructure:"tenantId" doc:"Azure AD tenant ID" maxLength:"36" example:"00000000-0000-0000-0000-000000000000"`
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" mapstructure:"clientId" doc:"Azure AD client ID" maxLength:"36" example:"00000000-0000-0000-0000-000000000000"`
}

// APICompressionConfig holds response compression configuration.
type APICompressionConfig struct {
	Level int `json:"level,omitempty" yaml:"level,omitempty" mapstructure:"level" doc:"Compression level (0-9, 0=disabled)" maximum:"9" example:"6"`
}

// APIOpenAPIConfig holds OpenAPI specification configuration.
type APIOpenAPIConfig struct {
	Servers     []APIOpenAPIServerConfig `json:"servers,omitempty" yaml:"servers,omitempty" mapstructure:"servers" doc:"OpenAPI server entries" maxItems:"10"`
	Title       string                   `json:"title,omitempty" yaml:"title,omitempty" mapstructure:"title" doc:"API title" maxLength:"200" example:"scafctl API"`
	Description string                   `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description" doc:"API description" maxLength:"2000"`
}

// APIOpenAPIServerConfig holds a single OpenAPI server entry.
type APIOpenAPIServerConfig struct {
	URL         string `json:"url" yaml:"url" mapstructure:"url" doc:"Server URL" maxLength:"2048" example:"https://api.example.com"`
	Description string `json:"description,omitempty" yaml:"description,omitempty" mapstructure:"description" doc:"Server description" maxLength:"500"`
}

// APIProfilerConfig holds profiler configuration for the API server.
type APIProfilerConfig struct {
	Enabled         bool `json:"enabled,omitempty" yaml:"enabled,omitempty" mapstructure:"enabled" doc:"Enable pprof profiler endpoints"`
	AllowUnauthProd bool `json:"allowUnauthProd,omitempty" yaml:"allowUnauthProd,omitempty" mapstructure:"allowUnauthProd" doc:"Allow unauthenticated profiler access in production"`
}

// APIAuditConfig holds audit logging configuration.
type APIAuditConfig struct {
	Enabled    bool `json:"enabled,omitempty" yaml:"enabled,omitempty" mapstructure:"enabled" doc:"Enable audit logging"`
	TrustProxy bool `json:"trustProxy,omitempty" yaml:"trustProxy,omitempty" mapstructure:"trustProxy" doc:"Trust X-Forwarded-For and X-Real-IP proxy headers for client IP in audit logs. Only enable when a trusted reverse proxy sanitizes these headers; otherwise clients can spoof their source IP in audit records."`
}

// APITracingConfig holds OpenTelemetry tracing configuration for the API server.
type APITracingConfig struct {
	Enabled bool `json:"enabled,omitempty" yaml:"enabled,omitempty" mapstructure:"enabled" doc:"Enable OpenTelemetry tracing"`
}

// CustomOAuth2Config defines a user-configurable OAuth2 auth handler.
// Each entry registers as its own named auth.Handler, usable for any OAuth2 service
// (OCI registries, APIs, providers, etc.).
type CustomOAuth2Config struct {
	// Name is the handler identifier, used as: scafctl auth login <name>
	// Must not conflict with built-in handler names (github, gcp, entra).
	Name        string `json:"name" yaml:"name" mapstructure:"name" doc:"Handler name (used as CLI argument)" maxLength:"64" example:"quay"`
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty" mapstructure:"displayName" doc:"Human-readable display name" maxLength:"128" example:"Quay.io"`

	// OAuth2 endpoints
	AuthorizeURL  string `json:"authorizeURL,omitempty" yaml:"authorizeURL,omitempty" mapstructure:"authorizeURL" doc:"OAuth2 authorization endpoint (required for interactive flow)" maxLength:"2048" example:"https://quay.io/oauth/authorize"`
	TokenURL      string `json:"tokenURL" yaml:"tokenURL" mapstructure:"tokenURL" doc:"OAuth2 token endpoint" maxLength:"2048" example:"https://quay.io/oauth/access_token"`
	DeviceAuthURL string `json:"deviceAuthURL,omitempty" yaml:"deviceAuthURL,omitempty" mapstructure:"deviceAuthURL" doc:"OAuth2 device authorization endpoint (required for device_code flow)" maxLength:"2048" example:"https://sso.example.com/device/code"`

	// Client credentials
	ClientID     string `json:"clientID" yaml:"clientID" mapstructure:"clientID" doc:"OAuth2 client ID" maxLength:"256"`
	ClientSecret string `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty" mapstructure:"clientSecret" doc:"OAuth2 client secret (required for client_credentials flow)" maxLength:"256"` //nolint:gosec // G117: config field, not a hardcoded credential

	// Flow configuration
	Scopes                 []string `json:"scopes,omitempty" yaml:"scopes,omitempty" mapstructure:"scopes" doc:"Default OAuth scopes" maxItems:"20"`
	DefaultFlow            string   `json:"defaultFlow,omitempty" yaml:"defaultFlow,omitempty" mapstructure:"defaultFlow" doc:"Default OAuth2 flow (interactive, device_code, client_credentials)" enum:"interactive,device_code,client_credentials" maxLength:"32" example:"interactive"`
	CallbackPort           int      `json:"callbackPort,omitempty" yaml:"callbackPort,omitempty" mapstructure:"callbackPort" doc:"Local callback port for interactive flow (0 = random)" minimum:"0" maximum:"65535" example:"8080"`
	DeviceCodePollInterval int      `json:"deviceCodePollInterval,omitempty" yaml:"deviceCodePollInterval,omitempty" mapstructure:"deviceCodePollInterval" doc:"Polling interval in seconds for device_code flow (0 = server default)" minimum:"0" maximum:"30" example:"5"`
	DisablePKCE            bool     `json:"disablePKCE,omitempty" yaml:"disablePKCE,omitempty" mapstructure:"disablePKCE" doc:"Disable PKCE for servers that reject code_challenge parameters"`
	ResponseType           string   `json:"responseType,omitempty" yaml:"responseType,omitempty" mapstructure:"responseType" doc:"OAuth2 response type: code (default) or token (implicit grant)" enum:"code,token" maxLength:"16" example:"token"`

	// Token verification
	VerifyURL      string                `json:"verifyURL,omitempty" yaml:"verifyURL,omitempty" mapstructure:"verifyURL" doc:"Token verification endpoint (optional)" maxLength:"2048" example:"https://quay.io/api/v1/user/"`
	IdentityFields *IdentityFieldMapping `json:"identityFields,omitempty" yaml:"identityFields,omitempty" mapstructure:"identityFields" doc:"Field mapping from verify response to identity claims"`

	// Registry association (optional, only for OCI registry auth)
	Registry         string `json:"registry,omitempty" yaml:"registry,omitempty" mapstructure:"registry" doc:"OCI registry host for auto-detection (optional, registry-only)" maxLength:"253" example:"quay.io"`
	RegistryUsername string `json:"registryUsername,omitempty" yaml:"registryUsername,omitempty" mapstructure:"registryUsername" doc:"Username for registry auth (optional, default: oauth2accesstoken)" maxLength:"256"`

	// Token exchange (optional secondary credential derivation)
	TokenExchange *TokenExchangeConfig `json:"tokenExchange,omitempty" yaml:"tokenExchange,omitempty" mapstructure:"tokenExchange" doc:"Optional secondary API call to derive a service-specific credential from the OAuth2 token"`
}

// TokenExchangeConfig defines a secondary API call that the OAuth2 handler executes after
// initial authentication. The primary OAuth2 token is injected as a Bearer token in the
// Authorization header. The response is parsed to extract a derived credential.
//
// This is fully generic — not coupled to registries or any specific service type:
//   - Quay.io: OAuth2 token → POST /api/v1/user/apptoken → app token
//   - API gateway: OAuth2 token → POST /v1/keys/generate → scoped API key
//   - Vault-like: OAuth2 token → POST /v1/auth/jwt/login → dynamic secret
//
// Note: This is a configurable credential derivation step, not an implementation
// of RFC 8693 (OAuth 2.0 Token Exchange).
type TokenExchangeConfig struct {
	// URL is the API endpoint to call to derive a secondary token.
	URL string `json:"url" yaml:"url" mapstructure:"url" doc:"API endpoint to derive a secondary credential" maxLength:"2048" example:"https://quay.io/api/v1/user/apptoken"`
	// Method is the HTTP method (default: POST).
	Method string `json:"method,omitempty" yaml:"method,omitempty" mapstructure:"method" doc:"HTTP method (default: POST)" maxLength:"10" example:"POST"`
	// RequestBody is the JSON body to send. Supports Go template variables: {{.Hostname}}, {{.Username}}.
	RequestBody gotmpl.GoTemplatingContent `json:"requestBody,omitempty" yaml:"requestBody,omitempty" mapstructure:"requestBody" doc:"JSON request body (Go template, optional)" maxLength:"4096"`
	// TokenJSONPath is the dot-notation path to extract the derived token from the JSON response.
	TokenJSONPath string `json:"tokenJSONPath" yaml:"tokenJSONPath" mapstructure:"tokenJSONPath" doc:"JSON path to the derived token in the response" maxLength:"256" example:"token.token"`
	// UsernameJSONPath optionally extracts a username from the response.
	UsernameJSONPath string `json:"usernameJSONPath,omitempty" yaml:"usernameJSONPath,omitempty" mapstructure:"usernameJSONPath" doc:"JSON path to username in response (optional)" maxLength:"256"`
}

// IdentityFieldMapping maps fields in a token verification response to identity claims.
type IdentityFieldMapping struct {
	Username string `json:"username,omitempty" yaml:"username,omitempty" mapstructure:"username" doc:"JSON field for username" maxLength:"128" example:"username"`
	Email    string `json:"email,omitempty" yaml:"email,omitempty" mapstructure:"email" doc:"JSON field for email" maxLength:"128" example:"email"`
	Name     string `json:"name,omitempty" yaml:"name,omitempty" mapstructure:"name" doc:"JSON field for display name" maxLength:"128"`
}
