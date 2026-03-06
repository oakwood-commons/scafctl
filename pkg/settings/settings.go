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
	FromAPI bool
	FromCli bool
	Path    string
}

// VersionInfo holds metadata about the build, including the commit hash,
// build version, and build timestamp.
type VersionInfo struct {
	Commit       string
	BuildVersion string
	BuildTime    string
}

// Run holds configuration settings for a single execution of the application.
// It includes options for logging, entry point configuration, output formatting,
// and error handling behavior.
type Run struct {
	MinLogLevel        string
	EntryPointSettings EntryPointSettings
	IsQuiet            bool
	NoColor            bool
	ExitOnError        bool
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
