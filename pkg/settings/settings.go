// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package settings

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/adrg/xdg"
)

const (
	CliBinaryName = "scafctl"
)

// Timeout defaults
const (
	// DefaultResolverTimeout is the default timeout for individual resolver execution.
	DefaultResolverTimeout = 30 * time.Second

	// DefaultPhaseTimeout is the default timeout for resolver phase execution.
	DefaultPhaseTimeout = 5 * time.Minute

	// DefaultActionTimeout is the default timeout for action execution.
	DefaultActionTimeout = 5 * time.Minute

	// DefaultGracePeriod is the default grace period for action cancellation.
	DefaultGracePeriod = 30 * time.Second
)

// File conflict defaults
const (
	// DefaultConflictStrategy is the default conflict resolution strategy for file writes.
	DefaultConflictStrategy = "skip-unchanged"

	// DefaultMaxBackups is the maximum number of backup files (.bak, .bak.1, etc.) per source file.
	DefaultMaxBackups = 5
)

// HTTP client defaults
const (
	// DefaultHTTPTimeout is the default timeout for HTTP requests.
	DefaultHTTPTimeout = 30 * time.Second

	// DefaultHTTPRetryMax is the default maximum number of HTTP retries.
	DefaultHTTPRetryMax = 3

	// DefaultHTTPRetryWaitMinimum is the minimum wait time between HTTP retries.
	DefaultHTTPRetryWaitMinimum = 1 * time.Second

	// DefaultHTTPRetryWaitMaximum is the maximum wait time between HTTP retries.
	DefaultHTTPRetryWaitMaximum = 30 * time.Second

	// DefaultHTTPCacheTTL is the default TTL for HTTP cache entries.
	DefaultHTTPCacheTTL = 10 * time.Minute

	// DefaultHTTPCacheKeyPrefix is the default prefix for HTTP cache keys.
	DefaultHTTPCacheKeyPrefix = "scafctl:"

	// DefaultArtifactCacheTTL is the default TTL for the artifact cache.
	// Catalog artifacts are cached for 24 hours by default.
	DefaultArtifactCacheTTL = 24 * time.Hour
)

// DefaultHTTPCacheDir returns the default directory for HTTP cache.
// Uses XDG Base Directory Specification:
//   - Linux: ~/.cache/scafctl/http-cache/
//   - macOS: ~/.cache/scafctl/http-cache/
//   - Windows: %LOCALAPPDATA%\cache\scafctl\http-cache\
func DefaultHTTPCacheDir() string {
	return filepath.Join(xdg.CacheHome, "scafctl", "http-cache")
}

// Size limits
const (
	// DefaultMaxResponseBodySize is the maximum HTTP response body size the
	// HTTP provider will read into memory (100 MB). This prevents denial of
	// service via unbounded response bodies from malicious or misconfigured
	// servers. Configurable per-deployment via httpClient.maxResponseBodySize.
	DefaultMaxResponseBodySize int64 = 100 * 1024 * 1024

	// DefaultMaxCacheFileSize is the maximum size for a single cached file (10MB).
	DefaultMaxCacheFileSize = 10 * 1024 * 1024

	// DefaultMemoryCacheSize is the maximum number of entries in memory caches.
	DefaultMemoryCacheSize = 1000

	// DefaultWarnValueSize is the threshold for warning about large resolver values (1MB).
	DefaultWarnValueSize = 1024 * 1024

	// DefaultMaxValueSize is the maximum allowed resolver value size (10MB).
	DefaultMaxValueSize = 10 * 1024 * 1024
)

// OTel / telemetry defaults
const (
	// DefaultOTelSamplerType is the default trace sampler. always_on means every
	// span is recorded (appropriate for a CLI tool with low call volume).
	DefaultOTelSamplerType = "always_on"

	// DefaultOTelSamplerArg is the default argument for the sampler.
	// For traceidratio this is the sampling probability (0.0–1.0).
	DefaultOTelSamplerArg = 1.0
)

// CEL defaults
const (
	// DefaultCELCacheSize is the default size for the CEL program cache.
	DefaultCELCacheSize = 10000

	// DefaultCELCostLimit is the default cost limit for CEL expression evaluation.
	// Set to 0 to disable cost limiting.
	DefaultCELCostLimit = 1000000
)

// Go template defaults
const (
	// DefaultGoTemplateCacheSize is the default size for the Go template compilation cache.
	DefaultGoTemplateCacheSize = 10000
)

// API server defaults
const (
	// DefaultAPIPort is the default port for the API server.
	DefaultAPIPort = 8080

	// DefaultAPIHost is the default host for the API server.
	DefaultAPIHost = "0.0.0.0"

	// DefaultAPIVersion is the default API version prefix.
	DefaultAPIVersion = "v1"

	// DefaultAPIShutdownTimeout is the default graceful shutdown timeout.
	DefaultAPIShutdownTimeout = "30s"

	// DefaultAPIRequestTimeout is the default request timeout.
	DefaultAPIRequestTimeout = "60s"

	// DefaultAPIMaxRequestSize is the default maximum request body size in bytes (10MB).
	DefaultAPIMaxRequestSize int64 = 10 * 1024 * 1024

	// DefaultAPICompressionLevel is the default gzip compression level.
	DefaultAPICompressionLevel = 6

	// DefaultAPICORSMaxAge is the default CORS max age in seconds.
	DefaultAPICORSMaxAge = 3600

	// DefaultAPIRateLimitMaxRequests is the default rate limit max requests.
	DefaultAPIRateLimitMaxRequests = 100

	// DefaultAPIRateLimitWindow is the default rate limit window.
	DefaultAPIRateLimitWindow = "1m"

	// DefaultAPIMaxConcurrentConns is the default max concurrent connections.
	DefaultAPIMaxConcurrentConns = 1000

	// DefaultAPIFilterCostLimit is the CEL cost limit for API filter expressions.
	// Lower than the CLI default (1,000,000) to prevent DoS via user-supplied expressions.
	DefaultAPIFilterCostLimit uint64 = 10000

	// DefaultAPIBodyReadTimeout is the default timeout for reading request bodies.
	DefaultAPIBodyReadTimeout = "15s"

	// DefaultAPIOperationMaxBodyBytes is the default per-operation max body size (1 MiB).
	// This is the Huma-level limit; the chi middleware limit (MaxRequestSize) is the outer bound.
	DefaultAPIOperationMaxBodyBytes int64 = 1 << 20

	// DefaultAPIAdminMaxBodyBytes is the max body size for admin endpoints (1 KiB).
	// Admin POST endpoints have empty or tiny bodies.
	DefaultAPIAdminMaxBodyBytes int64 = 1024

	// DefaultAPIEvalTimeout is the maximum wall-clock time allowed for a single
	// CEL or Go-template evaluation request. Prevents CPU exhaustion from
	// expensive user-supplied expressions.
	DefaultAPIEvalTimeout = 30 * time.Second
)

// Circuit breaker defaults
const (
	// DefaultCircuitBreakerMaxFailures is the number of consecutive failures before opening the circuit.
	DefaultCircuitBreakerMaxFailures = 5

	// DefaultCircuitBreakerOpenTimeout is how long to wait before transitioning from Open to HalfOpen.
	DefaultCircuitBreakerOpenTimeout = 30 * time.Second

	// DefaultCircuitBreakerHalfOpenRequests is the number of successful requests in HalfOpen before closing.
	DefaultCircuitBreakerHalfOpenRequests = 1
)

// DefaultBuildCacheDir returns the default directory for build cache.
// Uses XDG Base Directory Specification:
//   - Linux: ~/.cache/scafctl/build-cache/
//   - macOS: ~/.cache/scafctl/build-cache/
//   - Windows: %LOCALAPPDATA%\cache\scafctl\build-cache\
func DefaultBuildCacheDir() string {
	return filepath.Join(xdg.CacheHome, "scafctl", "build-cache")
}

// DefaultPluginCacheDir returns the default directory for cached plugin binaries.
// Uses XDG Base Directory Specification:
//   - Linux: ~/.cache/scafctl/plugins/
//   - macOS: ~/.cache/scafctl/plugins/
//   - Windows: %LOCALAPPDATA%\cache\scafctl\plugins\
func DefaultPluginCacheDir() string {
	return filepath.Join(xdg.CacheHome, "scafctl", "plugins")
}

var (
	RootSolutionFolders = []string{
		CliBinaryName,
		fmt.Sprintf(".%s", CliBinaryName),
		"",
	}
	SolutionFileNames = []string{
		"solution.yaml",
		"solution.yml",
		fmt.Sprintf("%s.yaml", CliBinaryName),
		fmt.Sprintf("%s.yml", CliBinaryName),
		"solution.json",
		fmt.Sprintf("%s.json", CliBinaryName),
	}
)

var VersionInformation = VersionInfo{
	Commit:       "unknown",
	BuildVersion: "v0.0.0-nightly",
	BuildTime:    "unknown",
}

// EntryPointSettings holds configuration options for determining the entry point source.
// It specifies whether the entry point is provided via an API or CLI, and the path to the entry point.
type EntryPointSettings struct {
	FromAPI bool   `json:"fromAPI" yaml:"fromAPI" doc:"Whether the entry point is provided via API"`
	FromCli bool   `json:"fromCli" yaml:"fromCli" doc:"Whether the entry point is provided via CLI"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty" doc:"Path to the entry point" maxLength:"512" example:"./solution.yaml"`
}

// VersionInfo holds metadata about the build, including the commit hash,
// build version, and build timestamp.
type VersionInfo struct {
	Commit       string `json:"commit" yaml:"commit" doc:"Git commit hash" maxLength:"64" example:"abc1234"`
	BuildVersion string `json:"buildVersion" yaml:"buildVersion" doc:"Build version string" maxLength:"64" example:"v1.2.3"`
	BuildTime    string `json:"buildTime" yaml:"buildTime" doc:"Build timestamp" maxLength:"64" example:"2025-01-01T00:00:00Z"`
}

// Run holds configuration settings for a single execution of the application.
// It includes options for logging, entry point configuration, output formatting,
// and error handling behavior.
type Run struct {
	MinLogLevel        string             `json:"minLogLevel" yaml:"minLogLevel" doc:"Minimum log level" maxLength:"16" example:"info"`
	EntryPointSettings EntryPointSettings `json:"entryPointSettings" yaml:"entryPointSettings" doc:"Entry point configuration"`
	IsQuiet            bool               `json:"isQuiet" yaml:"isQuiet" doc:"Whether to suppress non-essential output"`
	NoColor            bool               `json:"noColor" yaml:"noColor" doc:"Whether to disable colored output"`
	ExitOnError        bool               `json:"exitOnError" yaml:"exitOnError" doc:"Whether to exit on error"`
}

// NewCliParams initializes and returns a pointer to a Run struct with default CLI parameters.
// It sets logging level to "none" (no structured logs by default), configures entry point
// settings for CLI usage, and sets default flags for quiet mode, color output, and error handling.
func NewCliParams() *Run {
	return &Run{
		MinLogLevel: "none",
		EntryPointSettings: EntryPointSettings{
			FromAPI: false,
			FromCli: true,
		},
		IsQuiet:     false,
		NoColor:     false,
		ExitOnError: true,
	}
}
