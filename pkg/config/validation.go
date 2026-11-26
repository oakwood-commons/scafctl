// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/oakwood-commons/scafctl/pkg/api/middleware"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/plugin/identity"
	"github.com/oakwood-commons/scafctl/pkg/settings"
	"github.com/oakwood-commons/scafctl/pkg/terminal/output"
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
		ip, network, err := net.ParseCIDR(trimmed)
		if err != nil {
			return "", fmt.Errorf("not a valid CIDR block: %w", err)
		}
		// IPv4-mapped IPv6 notation is ambiguous in CIDR form, so refuse it
		// rather than guess. ParseCIDR masks the 16-byte form, so
		// "::ffff:127.0.0.1/32" does not name that one host -- it is the
		// network ::/32, silently exempting a vast range including ::1. The
		// plain-address branch narrows mapped literals to /32 (see below);
		// narrowing a masked network instead would silently do the opposite
		// of what its text says. The equivalent IPv4 CIDR says exactly what
		// the operator means, so require it.
		if strings.Contains(trimmed, ":") && ip.To4() != nil {
			return "", fmt.Errorf(
				"IPv4-mapped IPv6 CIDR notation %q covers %s once masked, not the mapped host it names; write the equivalent IPv4 CIDR instead",
				trimmed, network.String(),
			)
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

	// Validate auth config
	if err := c.Auth.Validate(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// Validate plugins config
	if err := c.Plugins.Validate(); err != nil {
		return fmt.Errorf("plugins: %w", err)
	}

	return nil
}

// Validate validates the plugin management configuration.
func (p *PluginsConfig) Validate() error {
	if p == nil {
		return nil
	}
	if err := p.Signatures.Validate(); err != nil {
		return fmt.Errorf("signatures: %w", err)
	}
	return nil
}

// Validate validates the plugin signature verification configuration.
// Bounds and trusted-identity pattern syntax are checked even when mode is
// "off" so configuration typos fail fast on config load or validation.
func (s *PluginSignaturesConfig) Validate() error {
	if s == nil {
		return nil
	}
	switch s.Mode {
	case "", "off", "warn", "enforce":
	default:
		return fmt.Errorf("mode: unknown signature mode %q: must be off, warn, or enforce", s.Mode)
	}
	if len(s.TrustedIssuers) > MaxTrustedIssuers {
		return fmt.Errorf("trustedIssuers: %d entries exceed the maximum of %d", len(s.TrustedIssuers), MaxTrustedIssuers)
	}
	if len(s.TrustedIdentities) > MaxTrustedIdentities {
		return fmt.Errorf("trustedIdentities: %d entries exceed the maximum of %d", len(s.TrustedIdentities), MaxTrustedIdentities)
	}
	if s.Mode == "warn" || s.Mode == "enforce" {
		if len(s.TrustedIssuers) == 0 || len(s.TrustedIdentities) == 0 {
			return fmt.Errorf("mode %q requires at least one trustedIssuers and one trustedIdentities entry", s.Mode)
		}
	}
	for i, issuer := range s.TrustedIssuers {
		if strings.TrimSpace(issuer) == "" {
			return fmt.Errorf("trustedIssuers[%d]: must not be empty", i)
		}
	}
	for i, pat := range s.TrustedIdentities {
		if err := identity.ValidateTrustedIdentityPattern(pat); err != nil {
			return fmt.Errorf("trustedIdentities[%d]: %w", i, err)
		}
	}
	return nil
}

// Validate validates global authentication configuration.
func (a *GlobalAuthConfig) Validate() error {
	if err := ValidateBrowser(a.Browser); err != nil {
		return fmt.Errorf("browser: %w", err)
	}
	if a.Git != nil {
		if err := a.Git.Validate(); err != nil {
			return fmt.Errorf("git: %w", err)
		}
	}
	for i := range a.CustomOAuth2 {
		if err := a.CustomOAuth2[i].ValidateGitFields(); err != nil {
			return fmt.Errorf("customOAuth2[%d]: %w", i, err)
		}
	}
	return nil
}

// Validate validates Git HTTPS credential helper configuration.
func (g *GitAuthConfig) Validate() error {
	if g == nil {
		return nil
	}
	if len(g.Rules) > MaxGitCredentialRules {
		return fmt.Errorf("rules: %d entries exceed the maximum of %d", len(g.Rules), MaxGitCredentialRules)
	}
	for i := range g.Rules {
		if err := g.Rules[i].Validate(); err != nil {
			return fmt.Errorf("rules[%d]: %w", i, err)
		}
	}
	return nil
}

// Validate validates a single GitCredentialRule entry.
func (r *GitCredentialRule) Validate() error {
	if r == nil {
		return nil
	}
	if strings.TrimSpace(r.Host) == "" {
		return errors.New("host: must not be empty")
	}
	if err := ValidateGitHost(r.Host); err != nil {
		return fmt.Errorf("host: %w", err)
	}
	if err := ValidateGitPathPrefix(r.PathPrefix); err != nil {
		return fmt.Errorf("pathPrefix: %w", err)
	}
	if err := validateGitRuleHandlerName(r.Handler); err != nil {
		return fmt.Errorf("handler: %w", err)
	}
	if err := validateGitRuleProfileName(r.Profile); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if err := validateGitAttrString(r.Scope, MaxGitRuleScopeLength); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	if err := validateGitAttrString(r.Username, MaxGitRuleUsernameLength); err != nil {
		return fmt.Errorf("username: %w", err)
	}
	return nil
}

// ValidateGitFields validates optional Git HTTPS fields on a CustomOAuth2Config.
func (oc *CustomOAuth2Config) ValidateGitFields() error {
	if oc == nil {
		return nil
	}
	if oc.GitHost != "" {
		if strings.TrimSpace(oc.GitHost) == "" {
			return errors.New("gitHost: must not be whitespace-only")
		}
		if err := ValidateGitHost(oc.GitHost); err != nil {
			return fmt.Errorf("gitHost: %w", err)
		}
	}
	if oc.GitUsername != "" {
		if err := validateGitAttrString(oc.GitUsername, MaxGitRuleUsernameLength); err != nil {
			return fmt.Errorf("gitUsername: %w", err)
		}
	}
	return nil
}

// ValidateGitHost validates a bare Git host or host:port selector (no scheme, path, or userinfo).
func ValidateGitHost(raw string) error {
	if len(raw) > MaxGitRuleHostLength {
		return fmt.Errorf("exceeds maximum length of %d characters", MaxGitRuleHostLength)
	}
	if !utf8.ValidString(raw) {
		return errors.New("contains invalid UTF-8")
	}
	if raw != strings.TrimSpace(raw) {
		return errors.New("must not have leading or trailing whitespace")
	}
	if raw == "" {
		return errors.New("must not be empty")
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) || unicode.IsSpace(r) {
			return errors.New("must not contain whitespace or control characters")
		}
	}
	if strings.Contains(raw, "://") {
		return fmt.Errorf("must be a bare host without scheme (got %q)", raw)
	}
	if strings.ContainsAny(raw, "/?#@\\") {
		return fmt.Errorf("must not contain path, query, fragment, or userinfo characters (got %q)", raw)
	}

	hostPart := raw
	if strings.HasPrefix(raw, "[") {
		h, portStr, err := net.SplitHostPort(raw)
		if err != nil {
			if !strings.HasSuffix(raw, "]") {
				return fmt.Errorf("invalid bracketed host %q: %w", raw, err)
			}
			h = strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
		} else if err := validatePortNumber(portStr); err != nil {
			return err
		}
		if net.ParseIP(h) == nil {
			return fmt.Errorf("invalid IPv6 literal %q", h)
		}
		return nil
	}

	if strings.Count(raw, ":") == 1 {
		h, portStr, err := net.SplitHostPort(raw)
		if err != nil || h == "" {
			return fmt.Errorf("invalid host:port %q", raw)
		}
		if err := validatePortNumber(portStr); err != nil {
			return err
		}
		hostPart = h
	} else if strings.Count(raw, ":") > 1 {
		return fmt.Errorf("IPv6 host %q must be enclosed in brackets", raw)
	}

	for i := range len(hostPart) {
		c := hostPart[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' {
			continue
		}
		return fmt.Errorf("invalid character %q in host %q", c, raw)
	}
	return nil
}

func validatePortNumber(portStr string) error {
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port %q must be between 1 and 65535", portStr)
	}
	return nil
}

// ValidateGitPathPrefix validates an optional repository path prefix.
func ValidateGitPathPrefix(prefix string) error {
	if prefix == "" {
		return nil
	}
	if len(prefix) > MaxGitRulePathPrefixLength {
		return fmt.Errorf("exceeds maximum length of %d characters", MaxGitRulePathPrefixLength)
	}
	if !utf8.ValidString(prefix) {
		return errors.New("contains invalid UTF-8")
	}
	for _, r := range prefix {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
			return errors.New("must not contain control characters")
		}
	}
	if strings.Contains(prefix, "://") || strings.ContainsAny(prefix, "?#\\") {
		return fmt.Errorf("must be a repository path prefix without scheme, query, fragment, or backslashes (got %q)", prefix)
	}
	trimmed := strings.Trim(prefix, "/")
	if trimmed == "" {
		return errors.New("must contain at least one path segment")
	}
	for _, seg := range strings.Split(trimmed, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("contains empty or traversal path segment in %q", prefix)
		}
	}
	return nil
}

func validateGitRuleHandlerName(name string) error {
	if name == "" {
		return errors.New("must not be empty")
	}
	if len(name) > MaxCustomOAuth2NameLength {
		return fmt.Errorf("exceeds maximum length of %d characters", MaxCustomOAuth2NameLength)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("must not contain '..' (got %q)", name)
	}
	first := name[0]
	if (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') && (first < '0' || first > '9') {
		return fmt.Errorf("must start with an ASCII letter or digit (got %q)", name)
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			continue
		}
		return fmt.Errorf("contains invalid character %q (allowed: a-z, A-Z, 0-9, -, _, .)", c)
	}
	return nil
}

func validateGitRuleProfileName(profile string) error {
	if profile == "" {
		return nil
	}
	if len(profile) > 64 {
		return fmt.Errorf("exceeds maximum length of 64 characters")
	}
	if strings.EqualFold(profile, "default") || strings.EqualFold(profile, "built-in") {
		return nil
	}
	first := profile[0]
	if (first < 'a' || first > 'z') && (first < 'A' || first > 'Z') && (first < '0' || first > '9') {
		return fmt.Errorf("must start with an ASCII letter or digit (got %q)", profile)
	}
	for i := 1; i < len(profile); i++ {
		c := profile[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		return fmt.Errorf("contains invalid character %q (allowed: a-z, A-Z, 0-9, -, _)", c)
	}
	return nil
}

func validateGitAttrString(val string, maxLen int) error {
	if val == "" {
		return nil
	}
	if len(val) > maxLen {
		return fmt.Errorf("exceeds maximum length of %d characters", maxLen)
	}
	if !utf8.ValidString(val) {
		return errors.New("contains invalid UTF-8")
	}
	for _, r := range val {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
			return errors.New("must not contain control characters")
		}
	}
	return nil
}

// NormalizeBrowser returns the canonical browser preference token after
// trimming surrounding whitespace and unwrapping a single pair of matching
// outer double or single quotes. Callers must validate raw inputs with
// ValidateBrowser first so control characters, whitespace-only values, and
// overlong strings are rejected before normalization.
func NormalizeBrowser(browser string) string {
	if browser == "" {
		return ""
	}
	s := strings.TrimSpace(browser)
	switch {
	case len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"':
		s = strings.TrimSpace(s[1 : len(s)-1])
	case len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'':
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// ValidateBrowser validates a raw browser preference string (such as
// auth.browser or --browser). Only the exact empty string is valid as unset.
// Non-empty values must fit within MaxAuthBrowserLength before trimming,
// contain valid UTF-8 with no control characters, not be whitespace-only, have
// balanced outer quotes if quoted, and not start with '-' so config or flag
// values cannot inject command-line options into browser launchers.
func ValidateBrowser(browser string) error {
	if browser == "" {
		return nil
	}
	if len(browser) > MaxAuthBrowserLength {
		return fmt.Errorf("exceeds maximum length of %d characters", MaxAuthBrowserLength)
	}
	if !utf8.ValidString(browser) {
		return errors.New("contains invalid UTF-8")
	}
	for _, r := range browser {
		if r < 0x20 || r == 0x7f || unicode.IsControl(r) {
			return errors.New("must not contain control characters")
		}
	}
	trimmed := strings.TrimSpace(browser)
	if trimmed == "" {
		return errors.New("must not be whitespace-only")
	}
	unquoted := trimmed
	switch {
	case trimmed[0] == '"' || trimmed[len(trimmed)-1] == '"':
		if len(trimmed) < 2 || trimmed[0] != '"' || trimmed[len(trimmed)-1] != '"' {
			return errors.New("must not contain double quotes")
		}
		unquoted = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	case trimmed[0] == '\'' || trimmed[len(trimmed)-1] == '\'':
		if len(trimmed) < 2 || trimmed[0] != '\'' || trimmed[len(trimmed)-1] != '\'' {
			return errors.New("must not have unmatched single quotes")
		}
		unquoted = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	}
	if unquoted == "" {
		return errors.New("must not be empty")
	}
	if strings.ContainsRune(unquoted, '"') {
		return errors.New("must not contain double quotes")
	}
	if strings.HasPrefix(unquoted, "'") || strings.HasSuffix(unquoted, "'") {
		return errors.New("must not contain nested or unmatched single quotes")
	}
	if strings.HasPrefix(trimmed, "-") || strings.HasPrefix(unquoted, "-") {
		return fmt.Errorf("must not start with '-' (got %q)", browser)
	}
	return nil
}

// ValidateCredentials validates SecretRef syntax, reserved secret prefixes, and
// workload-identity field constraints on a CustomOAuth2Config.
func (oc *CustomOAuth2Config) ValidateCredentials(binaryNames ...string) error {
	if oc == nil {
		return nil
	}
	if oc.FederatedToken != "" && oc.FederatedTokenFile != "" {
		return fmt.Errorf("federatedToken and federatedTokenFile are mutually exclusive")
	}
	if oc.FederatedTokenFile != "" {
		if strings.TrimSpace(oc.FederatedTokenFile) == "" {
			return fmt.Errorf("invalid federatedTokenFile: path must not be whitespace-only")
		}
		if strings.ContainsRune(oc.FederatedTokenFile, 0) {
			return fmt.Errorf("invalid federatedTokenFile: path must not contain NUL bytes")
		}
	}
	if oc.FederatedToken != "" {
		if err := oc.FederatedToken.ValidateFor(binaryNames...); err != nil {
			return fmt.Errorf("invalid federatedToken: %w", err)
		}
	}
	if oc.Token != "" {
		if err := oc.Token.ValidateFor(binaryNames...); err != nil {
			return fmt.Errorf("invalid token: %w", err)
		}
	}
	if oc.ClientSecret != "" {
		if err := ValidateCredentialValue(oc.ClientSecret, binaryNames...); err != nil {
			return fmt.Errorf("invalid clientSecret: %w", err)
		}
	}
	if oc.Username != "" {
		if err := ValidateCredentialValue(oc.Username, binaryNames...); err != nil {
			return fmt.Errorf("invalid username: %w", err)
		}
	}
	if oc.DynamicClientRegistration != nil && oc.DynamicClientRegistration.InitialAccessToken != "" {
		if err := ValidateCredentialValue(oc.DynamicClientRegistration.InitialAccessToken, binaryNames...); err != nil {
			return fmt.Errorf("invalid dynamicClientRegistration.initialAccessToken: %w", err)
		}
	}
	switch oc.WorkloadIdentityGrantType {
	case "", WorkloadIdentityGrantClientCredentials, WorkloadIdentityGrantTokenExchange:
	default:
		return fmt.Errorf("unknown workloadIdentityGrantType %q (valid: %s, %s)",
			oc.WorkloadIdentityGrantType, WorkloadIdentityGrantClientCredentials, WorkloadIdentityGrantTokenExchange)
	}
	if err := oc.ValidateGitFields(); err != nil {
		return err
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

	// Validate artifactKinds if specified (enforce maxItems and allowed values at load time)
	if len(c.ArtifactKinds) > MaxCatalogArtifactKinds {
		return fmt.Errorf("artifactKinds: %d entries exceed the maximum of %d", len(c.ArtifactKinds), MaxCatalogArtifactKinds)
	}
	if len(c.ArtifactKinds) > 0 {
		seen := make(map[string]bool, len(c.ArtifactKinds))
		for i, kind := range c.ArtifactKinds {
			if !IsValidCatalogArtifactKind(kind) {
				return fmt.Errorf("artifactKinds[%d]: invalid value %q, must be one of: %v", i, kind, ValidCatalogArtifactKinds())
			}
			if seen[kind] {
				return fmt.Errorf("artifactKinds[%d]: duplicate value %q", i, kind)
			}
			seen[kind] = true
		}
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

	if a.CIOutput != "" {
		if _, err := output.ParseCIMode(a.CIOutput); err != nil {
			return fmt.Errorf("ciOutput: %w", err)
		}
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
