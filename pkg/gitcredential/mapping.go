// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package gitcredential

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/auth"
	"github.com/oakwood-commons/scafctl/pkg/config"
)

const (
	// GitUsernameGitHub is the standard HTTP Basic username for GitHub OAuth and installation tokens.
	GitUsernameGitHub = "x-access-token"

	// GitUsernameGCP is the standard HTTP Basic username for Google Cloud Source Repositories OAuth tokens.
	GitUsernameGCP = "oauth2accesstoken"

	// GitUsernameEntra is the HTTP Basic username accepted by Azure DevOps for Entra bearer tokens.
	GitUsernameEntra = "oauth2"

	// GitUsernameDefault is the fallback HTTP Basic username for generic OAuth2 Git remotes.
	GitUsernameDefault = "oauth2"

	// DefaultEntraGitScope is the Azure DevOps resource scope used when acquiring Entra tokens for Git.
	DefaultEntraGitScope = "499b84ac-1321-427f-aa17-267ca6975798/.default"

	// DefaultGCPGitScope is the OAuth scope used when acquiring GCP tokens for Cloud Source Repositories.
	DefaultGCPGitScope = "https://www.googleapis.com/auth/cloud-platform"
)

// GitUsernameProvider is an optional interface implemented by auth handlers
// that declare a static custom Git HTTP Basic username.
type GitUsernameProvider interface {
	GitUsername() string
}

// ContextualGitUsernameProvider is an optional profile-aware interface
// implemented by auth handlers (such as oauth2.Handler) that resolve a Git
// HTTP Basic username for the active profile in ctx.
type ContextualGitUsernameProvider interface {
	GitUsernameForContext(ctx context.Context) string
}

// NormalizeRepoPath trims leading/trailing slashes and any trailing ".git"
// suffix for routing rule prefix matching so "/org/repo.git", "org/repo/", and
// "org/repo" match the same auth.git.rules[].pathPrefix.
func NormalizeRepoPath(raw string) string {
	s := CleanRepoPath(raw)
	if len(s) >= 4 && strings.EqualFold(s[len(s)-4:], ".git") {
		s = strings.Trim(s[:len(s)-4], "/")
	}
	return s
}

func effectiveMatchProtocol(protocol string) string {
	p := strings.ToLower(strings.TrimSpace(protocol))
	if p == "" {
		return "https"
	}
	return p
}

func hostsMatchForProtocol(protocol, candidateHost, targetHost string) bool {
	if candidateHost == "" || targetHost == "" {
		return false
	}
	proto := effectiveMatchProtocol(protocol)
	return NormalizeHostForProtocol(proto, candidateHost) == NormalizeHostForProtocol(proto, targetHost)
}

// MatchRule evaluates auth.git.rules against host and repoPath assuming HTTPS
// default-port semantics (:443). Use MatchRuleForProtocol when matching an
// explicit request protocol.
func MatchRule(host, repoPath string, rules []config.GitCredentialRule) (*config.GitCredentialRule, bool) {
	return MatchRuleForProtocol("https", host, repoPath, rules)
}

// MatchRuleForProtocol evaluates auth.git.rules against protocol, host, and
// repoPath. Host matching strips only protocol's default port (:443 for https,
// :80 for http); PathPrefix matching is enforced on "/" segment boundaries
// after validating against traversal segments and stripping ".git". Among all
// matching rules, the rule with the longest normalized PathPrefix wins.
func MatchRuleForProtocol(protocol, host, repoPath string, rules []config.GitCredentialRule) (*config.GitCredentialRule, bool) {
	rawHost := strings.ToLower(strings.TrimSpace(host))
	if rawHost == "" || len(rules) == 0 {
		return nil, false
	}
	if ValidateHostAttribute(rawHost) != nil || ValidateRepoPath(repoPath) != nil {
		return nil, false
	}
	proto := effectiveMatchProtocol(protocol)
	normHost := NormalizeHostForProtocol(proto, rawHost)
	normPath := NormalizeRepoPath(repoPath)

	bestIdx := -1
	bestPrefixLen := -1

	for i := range rules {
		ruleHost := strings.ToLower(strings.TrimSpace(rules[i].Host))
		if ruleHost == "" || !hostsMatchForProtocol(proto, ruleHost, normHost) {
			continue
		}
		rulePrefix := NormalizeRepoPath(rules[i].PathPrefix)
		if rulePrefix != "" {
			if normPath != rulePrefix && !strings.HasPrefix(normPath, rulePrefix+"/") {
				continue
			}
		}
		if len(rulePrefix) > bestPrefixLen {
			bestIdx = i
			bestPrefixLen = len(rulePrefix)
		}
	}

	if bestIdx < 0 {
		return nil, false
	}
	matched := rules[bestIdx]
	return &matched, true
}

// InferGitAuthHandler maps a Git HTTPS host to a built-in, GHES-configured, or
// custom OAuth2 auth handler without altering OCI registry inference in pkg/catalog.
func InferGitAuthHandler(host string, cfg *config.Config) string {
	return InferGitAuthHandlerForProtocol("https", host, cfg)
}

// InferGitAuthHandlerForProtocol maps a Git host under protocol to a built-in,
// GHES-configured, or custom OAuth2 auth handler, stripping only protocol's
// default port (:443 for https, :80 for http).
func InferGitAuthHandlerForProtocol(protocol, host string, cfg *config.Config) string {
	rawHost := strings.ToLower(strings.TrimSpace(host))
	if rawHost == "" || ValidateHostAttribute(rawHost) != nil {
		return ""
	}
	proto := effectiveMatchProtocol(protocol)
	normHost := NormalizeHostForProtocol(proto, rawHost)

	switch {
	case normHost == "github.com", normHost == "gist.github.com":
		return "github"
	case normHost == "dev.azure.com",
		strings.HasSuffix(normHost, ".visualstudio.com") && len(normHost) > len(".visualstudio.com"):
		return "entra"
	case normHost == "source.developers.google.com":
		return "gcp"
	}

	if matchesConfiguredGitHubHost(proto, normHost, cfg) {
		return "github"
	}

	if cfg != nil {
		for _, entry := range cfg.Auth.CustomOAuth2 {
			entryHost := strings.ToLower(strings.TrimSpace(entry.GitHost))
			if entryHost != "" && hostsMatchForProtocol(proto, entryHost, normHost) {
				return entry.Name
			}
		}
	}

	return ""
}

func matchesConfiguredGitHubHost(proto, normHost string, cfg *config.Config) bool {
	if cfg == nil || cfg.Auth.GitHub == nil {
		return false
	}
	if hostsMatchForProtocol(proto, normalizeConfiguredHostForProtocol(proto, cfg.Auth.GitHub.Hostname), normHost) {
		return true
	}
	for _, p := range cfg.Auth.GitHub.Profiles {
		if p != nil && hostsMatchForProtocol(proto, normalizeConfiguredHostForProtocol(proto, p.Hostname), normHost) {
			return true
		}
	}
	return false
}

// SingleMatchingGHESProfile returns the normalized profile name when exactly one
// configured auth.github.profiles entry matches host under HTTPS.
func SingleMatchingGHESProfile(host string, cfg *config.Config) string {
	return SingleMatchingGHESProfileForProtocol("https", host, cfg)
}

// SingleMatchingGHESProfileForProtocol returns the normalized profile name when
// exactly one configured auth.github.profiles entry matches host under protocol.
func SingleMatchingGHESProfileForProtocol(protocol, host string, cfg *config.Config) string {
	prof, _ := singleMatchingGHESProfileValidated(protocol, host, cfg)
	return prof
}

func singleMatchingGHESProfileValidated(protocol, host string, cfg *config.Config) (string, error) {
	rawHost := strings.ToLower(strings.TrimSpace(host))
	if rawHost == "" {
		return "", nil
	}
	if err := ValidateHostAttribute(rawHost); err != nil {
		return "", err
	}
	proto := effectiveMatchProtocol(protocol)
	normHost := NormalizeHostForProtocol(proto, rawHost)
	if normHost == "" || normHost == "github.com" || normHost == "gist.github.com" {
		return "", nil
	}
	if cfg == nil || cfg.Auth.GitHub == nil {
		return "", nil
	}
	if hostsMatchForProtocol(proto, normalizeConfiguredHostForProtocol(proto, cfg.Auth.GitHub.Hostname), normHost) {
		return "", nil
	}

	matched := make(map[string]struct{})
	for name, p := range cfg.Auth.GitHub.Profiles {
		if p == nil || !hostsMatchForProtocol(proto, normalizeConfiguredHostForProtocol(proto, p.Hostname), normHost) {
			continue
		}
		normProfile := auth.NormalizeProfileName(name)
		if normProfile == "" {
			return "", fmt.Errorf("invalid GitHub profile name %q for host %q: reserved profile name", name, normHost)
		}
		if err := auth.ValidateProfileName(normProfile); err != nil {
			return "", fmt.Errorf("invalid GitHub profile name %q for host %q: %w", name, normHost, err)
		}
		matched[normProfile] = struct{}{}
	}
	if len(matched) != 1 {
		return "", nil
	}
	names := make([]string, 0, 1)
	for k := range matched {
		names = append(names, k)
	}
	sort.Strings(names)
	return names[0], nil
}

func normalizeConfiguredHost(raw string) string {
	return normalizeConfiguredHostForProtocol("https", raw)
}

func normalizeConfiguredHostForProtocol(protocol, raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	proto := effectiveMatchProtocol(protocol)
	if strings.Contains(s, "://") {
		uScheme, _, ok := strings.Cut(s, "://")
		if !ok || strings.ToLower(strings.TrimSpace(uScheme)) != proto {
			return ""
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return ""
		}
		return NormalizeHostForProtocol(proto, u.Host)
	}
	if strings.ContainsRune(s, '/') {
		u, err := url.Parse(proto + "://" + s)
		if err != nil || u.Host == "" {
			return ""
		}
		return NormalizeHostForProtocol(proto, u.Host)
	}
	return NormalizeHostForProtocol(proto, s)
}

// InferGitDefaultScope returns the default OAuth scope needed for Git HTTPS
// operations against the given handler when no explicit rule scope is set.
func InferGitDefaultScope(handlerName string) string {
	switch handlerName {
	case "entra":
		return DefaultEntraGitScope
	case "gcp":
		return DefaultGCPGitScope
	default:
		return ""
	}
}

// ResolveGitUsername selects the Git HTTP Basic username using the precedence:
// ruleUsername > built-in handler convention (github/gcp/entra) or
// ContextualGitUsernameProvider / GitUsernameProvider > reqUsername > "oauth2".
func ResolveGitUsername(ctx context.Context, handler auth.Handler, ruleUsername, reqUsername string) string {
	if ruleUsername != "" {
		return ruleUsername
	}

	if handler != nil {
		switch handler.Name() {
		case "github":
			return GitUsernameGitHub
		case "gcp":
			return GitUsernameGCP
		case "entra":
			return GitUsernameEntra
		default:
			if cp, ok := handler.(ContextualGitUsernameProvider); ok {
				if u := cp.GitUsernameForContext(ctx); u != "" {
					return u
				}
			} else if p, ok := handler.(GitUsernameProvider); ok {
				if u := p.GitUsername(); u != "" {
					return u
				}
			}
		}
	}

	if reqUsername != "" {
		return reqUsername
	}
	return GitUsernameDefault
}
