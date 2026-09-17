// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/settings"
)

// NormalizeCIDR converts a single address-allowlist entry into CIDR form,
// returning an error describing why an entry is unusable.
//
// A CIDR block is returned in canonical form, not the text supplied:
// net.ParseCIDR accepts host bits (for example "10.42.7.9/24"), and the
// masked network -- what the entry actually covers -- is what is stored, so
// that example comes back as "10.42.7.0/24". A bare address is widened to
// the single-address range covering it (/32 for IPv4, /128 for IPv6), since
// naming one host is the obvious way to express "just this host" and
// rejecting it would be a needless papercut.
//
// This is the single definition shared by configuration validation and by the
// code that builds the runtime policy, so an entry accepted at startup can
// never be one the policy later rejects.
func NormalizeCIDR(entry string) (string, error) {
	trimmed := strings.TrimSpace(entry)
	if trimmed == "" {
		return "", errors.New("empty entry")
	}

	if strings.Contains(trimmed, "/") {
		_, network, err := net.ParseCIDR(trimmed)
		if err != nil {
			return "", fmt.Errorf("not a valid CIDR block: %w", err)
		}
		// Return the masked network rather than the text supplied. "10.42.7.9/24"
		// is accepted by ParseCIDR but covers 10.42.7.0/24, and an operator who
		// wrote it probably meant one host. Storing the effective range makes the
		// widening visible wherever the entry is echoed back.
		return network.String(), nil
	}

	ip := net.ParseIP(trimmed)
	if ip == nil {
		return "", errors.New("not a valid IP address or CIDR block")
	}
	// Build the CIDR from the canonical address rather than the text supplied.
	// An IPv4-mapped IPv6 literal such as "::ffff:192.168.1.1" has a non-nil
	// To4(), but "::ffff:192.168.1.1/32" parses as the IPv6 network ::/32 --
	// silently exempting a vast range including ::1, rather than the single
	// host the operator asked for.
	if v4 := ip.To4(); v4 != nil {
		return v4.String() + "/32", nil
	}
	return ip.String() + "/128", nil
}

// Validate validates the entire configuration.
// Returns an error if any configuration value is invalid.
func (c *Config) Validate() error {
	// Warn about missing or outdated version (but don't fail)
	// This is handled by the caller if needed

	// Validate global HTTP client config
	if err := c.HTTPClient.Validate(); err != nil {
		return fmt.Errorf("httpClient: %w", err)
	}

	// Validate CEL config
	if err := c.CEL.Validate(); err != nil {
		return fmt.Errorf("cel: %w", err)
	}

	// Validate resolver config
	if err := c.Resolver.Validate(); err != nil {
		return fmt.Errorf("resolver: %w", err)
	}

	// Validate action config
	if err := c.Action.Validate(); err != nil {
		return fmt.Errorf("action: %w", err)
	}

	// Validate each catalog
	for i, catalog := range c.Catalogs {
		if err := catalog.Validate(); err != nil {
			return fmt.Errorf("catalogs[%d]: %w", i, err)
		}
	}

	// Validate build config
	if err := c.Build.Validate(); err != nil {
		return fmt.Errorf("build: %w", err)
	}

	// Validate API server config
	if err := c.APIServer.Validate(); err != nil {
		return fmt.Errorf("apiServer: %w", err)
	}

	return nil
}

// Validate validates a catalog configuration.
func (c *CatalogConfig) Validate() error {
	// Validate catalog type if specified
	if c.Type != "" && !IsValidCatalogType(c.Type) {
		return fmt.Errorf("type: invalid value %q, must be one of: %v", c.Type, ValidCatalogTypes())
	}

	// Validate discovery strategy if specified
	if c.DiscoveryStrategy != "" && !IsValidDiscoveryStrategy(string(c.DiscoveryStrategy)) {
		return fmt.Errorf("discoveryStrategy: invalid value %q, must be one of: %v", c.DiscoveryStrategy, ValidDiscoveryStrategies())
	}

	// Validate per-catalog HTTP client config if present
	if c.HTTPClient != nil {
		if err := c.HTTPClient.Validate(); err != nil {
			return fmt.Errorf("httpClient: %w", err)
		}
	}

	return nil
}

// Validate validates the HTTP client configuration.
// Returns an error if any value is invalid.
func (h *HTTPClientConfig) Validate() error {
	// Validate duration fields
	durationFields := map[string]string{
		"timeout":                   h.Timeout,
		"retryWaitMin":              h.RetryWaitMin,
		"retryWaitMax":              h.RetryWaitMax,
		"cacheTTL":                  h.CacheTTL,
		"circuitBreakerOpenTimeout": h.CircuitBreakerOpenTimeout,
	}

	for field, value := range durationFields {
		if value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
			}
		}
	}

	// Validate cache type
	if h.CacheType != "" && !IsValidHTTPClientCacheType(h.CacheType) {
		return fmt.Errorf("cacheType: invalid value %q, must be one of: %v", h.CacheType, ValidHTTPClientCacheTypes())
	}

	// Validate numeric ranges
	if h.RetryMax < 0 {
		return fmt.Errorf("retryMax: must be non-negative, got %d", h.RetryMax)
	}
	if h.MemoryCacheSize < 0 {
		return fmt.Errorf("memoryCacheSize: must be non-negative, got %d", h.MemoryCacheSize)
	}
	if h.MaxCacheFileSize < 0 {
		return fmt.Errorf("maxCacheFileSize: must be non-negative, got %d", h.MaxCacheFileSize)
	}
	if h.CircuitBreakerMaxFailures < 0 {
		return fmt.Errorf("circuitBreakerMaxFailures: must be non-negative, got %d", h.CircuitBreakerMaxFailures)
	}
	if h.CircuitBreakerHalfOpenMaxRequests < 0 {
		return fmt.Errorf("circuitBreakerHalfOpenMaxRequests: must be non-negative, got %d", h.CircuitBreakerHalfOpenMaxRequests)
	}

	// The `maxItems` struct tag documents this bound for schema consumers, but
	// the config loader never applies struct tags, so an advertised cap that is
	// not checked here is not a cap. Same reasoning as allowedHosts below.
	//
	// An absent list is nothing to validate; a present but empty one is a
	// deliberate "no exceptions" and is valid.
	if entries, set := h.PrivateCIDRs(); set {
		if len(entries) > settings.MaxAllowedPrivateCIDRs {
			return fmt.Errorf("allowedPrivateCIDRs: %d entries exceed the maximum of %d",
				len(entries), settings.MaxAllowedPrivateCIDRs)
		}

		// Reject a malformed address allowlist at startup. A bad entry is
		// refused loudly here rather than dropped, because an operator who
		// mistypes a range would otherwise believe they had granted access they
		// had not.
		for i, entry := range entries {
			if _, err := NormalizeCIDR(entry); err != nil {
				return fmt.Errorf("allowedPrivateCIDRs[%d]: %q: %w", i, entry, err)
			}
		}
	}

	return nil
}

// Validate validates the CEL configuration.
// Returns an error if any value is invalid.
func (c *CELConfig) Validate() error {
	// Validate cache size
	if c.CacheSize < 0 {
		return fmt.Errorf("cacheSize: must be non-negative, got %d", c.CacheSize)
	}

	// Validate cost limit
	if c.CostLimit < 0 {
		return fmt.Errorf("costLimit: must be non-negative, got %d", c.CostLimit)
	}

	return nil
}

// Validate validates the resolver configuration.
// Returns an error if any value is invalid.
func (r *ResolverConfig) Validate() error {
	// Validate duration fields
	durationFields := map[string]string{
		"timeout":      r.Timeout,
		"phaseTimeout": r.PhaseTimeout,
	}

	for field, value := range durationFields {
		if value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
			}
		}
	}

	// Validate numeric ranges
	if r.MaxConcurrency < 0 {
		return fmt.Errorf("maxConcurrency: must be non-negative, got %d", r.MaxConcurrency)
	}
	if r.WarnValueSize < 0 {
		return fmt.Errorf("warnValueSize: must be non-negative, got %d", r.WarnValueSize)
	}
	if r.MaxValueSize < 0 {
		return fmt.Errorf("maxValueSize: must be non-negative, got %d", r.MaxValueSize)
	}

	return nil
}

// Validate validates the action configuration.
// Returns an error if any value is invalid.
func (a *ActionConfig) Validate() error {
	// Validate duration fields
	durationFields := map[string]string{
		"defaultTimeout": a.DefaultTimeout,
		"gracePeriod":    a.GracePeriod,
	}

	for field, value := range durationFields {
		if value != "" {
			if _, err := time.ParseDuration(value); err != nil {
				return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
			}
		}
	}

	// Validate numeric ranges
	if a.MaxConcurrency < 0 {
		return fmt.Errorf("maxConcurrency: must be non-negative, got %d", a.MaxConcurrency)
	}

	return nil
}

// Validate validates the logging configuration.
// Returns an error if any value is invalid.
func (l *LoggingConfig) Validate() error {
	// Validate log level (must be a recognized named level or numeric V-level)
	if l.Level != "" {
		if _, err := logger.ParseLogLevel(l.Level); err != nil {
			return fmt.Errorf("level: %w", err)
		}
	}

	// Validate format
	if l.Format != "" && l.Format != LoggingFormatJSON && l.Format != LoggingFormatText && l.Format != LoggingFormatConsole {
		return fmt.Errorf("format: must be %q, %q, or %q, got %q", LoggingFormatConsole, LoggingFormatJSON, LoggingFormatText, l.Format)
	}

	return nil
}

// CheckVersion checks if the config version is current and returns a warning message if not.
// Returns an empty string if the version is current or if version checking should be skipped.
func (c *Config) CheckVersion() string {
	if c.Version == 0 {
		return "config file has no version specified, consider adding 'version: 1'"
	}
	if c.Version < CurrentConfigVersion {
		return fmt.Sprintf("config file version %d is outdated, current version is %d", c.Version, CurrentConfigVersion)
	}
	if c.Version > CurrentConfigVersion {
		return fmt.Sprintf("config file version %d is newer than supported version %d, some features may not work", c.Version, CurrentConfigVersion)
	}
	return ""
}

// Validate validates the build configuration.
func (b *BuildConfig) Validate() error {
	// CacheDir and PluginCacheDir are validated for length by struct tags;
	// no additional semantic validation needed at this time.
	return nil
}

// Validate validates the API server configuration.
func (c *APIServerConfig) Validate() error {
	// Struct tags document the ceiling for schema consumers, but the config
	// loader does not enforce them, so the bound has to be checked here or an
	// operator can configure an arbitrarily large per-connection header buffer.
	if c.MaxHeaderBytes > settings.MaxAPIMaxHeaderBytes {
		return fmt.Errorf("maxHeaderBytes: %d exceeds the maximum of %d bytes", c.MaxHeaderBytes, settings.MaxAPIMaxHeaderBytes)
	}
	if c.MaxHeaderBytes < 0 {
		return fmt.Errorf("maxHeaderBytes: must not be negative, got %d", c.MaxHeaderBytes)
	}

	// Same reason as maxHeaderBytes above: the `maxItems` struct tag documents
	// the bound for schema consumers, but the config loader never applies
	// struct tags, so an advertised cap that is not checked here is not a cap.
	if len(c.AllowedHosts) > settings.MaxAPIAllowedHosts {
		return fmt.Errorf("allowedHosts: %d entries exceed the maximum of %d", len(c.AllowedHosts), settings.MaxAPIAllowedHosts)
	}

	// An allowlist made entirely of blank or malformed entries would leave this
	// security control configured but inert; refuse to start instead.
	if err := middleware.ValidateAllowedHosts(c.AllowedHosts); err != nil {
		return fmt.Errorf("allowedHosts: %w", err)
	}

	if c.TokenPassThrough != nil {
		if err := c.TokenPassThrough.Validate(); err != nil {
			return fmt.Errorf("tokenPassThrough: %w", err)
		}
	}
	return nil
}

// Validate validates token pass-through configuration.
func (c *TokenPassThroughConfig) Validate() error {
	if c == nil {
		return nil
	}
	for i, suffix := range c.AllowedHeaders {
		suffix = strings.TrimSpace(suffix)
		if suffix == "" {
			return fmt.Errorf("allowedHeaders[%d]: must not be empty", i)
		}
		if strings.HasPrefix(http.CanonicalHeaderKey(suffix), middleware.TokenHeaderPrefix) {
			return fmt.Errorf("allowedHeaders[%d]: must not include %s prefix", i, middleware.TokenHeaderPrefix)
		}
		header := http.CanonicalHeaderKey(middleware.TokenHeaderPrefix + suffix)
		if !validHTTPHeaderFieldName(header) {
			return fmt.Errorf("allowedHeaders[%d]: invalid HTTP header suffix %q", i, c.AllowedHeaders[i])
		}
	}
	return nil
}

func validHTTPHeaderFieldName(name string) bool {
	if name == "" {
		return false
	}
	for i := range len(name) {
		c := name[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		switch c {
		case '!', '#', '$', '%', '&', '\'', '*', '+', '-', '.', '^', '_', '`', '|', '~':
			continue
		default:
			return false
		}
	}
	return true
}
