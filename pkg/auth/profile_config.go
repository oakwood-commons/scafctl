// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"fmt"
	"sort"

	"github.com/oakwood-commons/scafctl/pkg/config"
)

// ResolveActiveProfile determines which profile should be active for a handler
// given the current context and configuration. Precedence (highest first):
//  1. Profile from context (set by solution YAML or --profile flag)
//  2. Profile from --auth-profile global flag / SCAFCTL_AUTH_PROFILE env var
//  3. ActiveProfile from handler config
//  4. Empty string (default profile)
func ResolveActiveProfile(ctx context.Context, handlerName string) string {
	// 1. Per-command/solution profile (already set in context by CLI flag or HTTP provider)
	if p := ProfileFromContext(ctx); p != "" {
		return p
	}

	// 2. Global auth profile from context (set by root --auth-profile flag)
	// Check raw value first: if the user explicitly set --auth-profile (even
	// to "built-in" or "default"), honor it and skip config-level fallback.
	if gp := GlobalProfileFromContext(ctx); gp != "" {
		return NormalizeProfileName(gp)
	}

	// 3. Config-level active profile
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return ""
	}

	var active string
	switch handlerName {
	case "entra":
		if cfg.Auth.Entra != nil {
			active = cfg.Auth.Entra.ActiveProfile
		}
	case "github":
		if cfg.Auth.GitHub != nil {
			active = cfg.Auth.GitHub.ActiveProfile
		}
	case "gcp":
		if cfg.Auth.GCP != nil {
			active = cfg.Auth.GCP.ActiveProfile
		}
	}

	// Normalize and validate config-level profile before returning.
	// Invalid profile names from config are silently treated as empty (default).
	normalized := NormalizeProfileName(active)
	if normalized != "" {
		if err := ValidateProfileName(normalized); err != nil {
			return ""
		}
	}
	return normalized
}

// ConfigActiveProfile returns the active profile for a handler from the config
// file only, ignoring context and flags. Use this for display purposes (e.g.
// marking which profile is active in status output).
func ConfigActiveProfile(ctx context.Context, handlerName string) string {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return ""
	}

	switch handlerName {
	case "entra":
		if cfg.Auth.Entra != nil {
			return cfg.Auth.Entra.ActiveProfile
		}
	case "github":
		if cfg.Auth.GitHub != nil {
			return cfg.Auth.GitHub.ActiveProfile
		}
	case "gcp":
		if cfg.Auth.GCP != nil {
			return cfg.Auth.GCP.ActiveProfile
		}
	}

	return ""
}

// ListConfiguredProfiles returns the profile names configured for a handler.
func ListConfiguredProfiles(ctx context.Context, handlerName string) []string {
	cfg := config.FromContext(ctx)
	if cfg == nil {
		return nil
	}

	switch handlerName {
	case "entra":
		if cfg.Auth.Entra != nil {
			return mapKeys(cfg.Auth.Entra.Profiles)
		}
	case "github":
		if cfg.Auth.GitHub != nil {
			return mapKeys(cfg.Auth.GitHub.Profiles)
		}
	case "gcp":
		if cfg.Auth.GCP != nil {
			return mapKeys(cfg.Auth.GCP.Profiles)
		}
	}

	return nil
}

// ValidateAuthProfiles validates all profile names in the auth configuration.
// Returns the first validation error encountered, or nil if all names are valid.
func ValidateAuthProfiles(cfg *config.Config) error {
	if cfg == nil {
		return nil
	}

	if cfg.Auth.Entra != nil {
		if err := validateProfileMap("entra", cfg.Auth.Entra.ActiveProfile, cfg.Auth.Entra.Profiles); err != nil {
			return err
		}
	}
	if cfg.Auth.GitHub != nil {
		if err := validateProfileMap("github", cfg.Auth.GitHub.ActiveProfile, cfg.Auth.GitHub.Profiles); err != nil {
			return err
		}
	}
	if cfg.Auth.GCP != nil {
		if err := validateProfileMap("gcp", cfg.Auth.GCP.ActiveProfile, cfg.Auth.GCP.Profiles); err != nil {
			return err
		}
	}

	return nil
}

// validateProfileMap validates a handler's activeProfile and profile map keys.
func validateProfileMap[T any](handlerName, activeProfile string, profiles map[string]*T) error {
	if activeProfile != "" {
		if err := ValidateProfileName(activeProfile); err != nil {
			return fmt.Errorf("auth.%s.activeProfile: %w", handlerName, err)
		}
	}
	for name := range profiles {
		if err := ValidateProfileName(name); err != nil {
			return fmt.Errorf("auth.%s.profiles[%q]: %w", handlerName, name, err)
		}
	}
	return nil
}

// EnsureProfileRegistered adds a named profile entry to the in-memory config
// if it doesn't already exist. This makes the profile discoverable by
// ListConfiguredProfiles and auth switch without requiring manual config edits.
// Does nothing for the default (empty) profile.
func EnsureProfileRegistered(cfg *config.Config, handlerName, profile string) {
	if cfg == nil || profile == "" {
		return
	}

	switch handlerName {
	case "entra":
		if cfg.Auth.Entra == nil {
			cfg.Auth.Entra = &config.EntraAuthConfig{}
		}
		if cfg.Auth.Entra.Profiles == nil {
			cfg.Auth.Entra.Profiles = make(map[string]*config.EntraProfileConfig)
		}
		if _, exists := cfg.Auth.Entra.Profiles[profile]; !exists {
			cfg.Auth.Entra.Profiles[profile] = &config.EntraProfileConfig{}
		}
	case "github":
		if cfg.Auth.GitHub == nil {
			cfg.Auth.GitHub = &config.GitHubAuthConfig{}
		}
		if cfg.Auth.GitHub.Profiles == nil {
			cfg.Auth.GitHub.Profiles = make(map[string]*config.GitHubProfileConfig)
		}
		if _, exists := cfg.Auth.GitHub.Profiles[profile]; !exists {
			cfg.Auth.GitHub.Profiles[profile] = &config.GitHubProfileConfig{}
		}
	case "gcp":
		if cfg.Auth.GCP == nil {
			cfg.Auth.GCP = &config.GCPAuthConfig{}
		}
		if cfg.Auth.GCP.Profiles == nil {
			cfg.Auth.GCP.Profiles = make(map[string]*config.GCPProfileConfig)
		}
		if _, exists := cfg.Auth.GCP.Profiles[profile]; !exists {
			cfg.Auth.GCP.Profiles[profile] = &config.GCPProfileConfig{}
		}
	}
}

// DeleteProfile removes a named profile from the in-memory config.
// If the handler's ActiveProfile points to the deleted profile, it is reset
// to the unnamed built-in profile. Returns true if the profile existed and was removed.
func DeleteProfile(cfg *config.Config, handlerName, profile string) (bool, error) {
	if cfg == nil {
		return false, fmt.Errorf("config is nil")
	}
	if profile == "" {
		return false, fmt.Errorf("cannot delete the unnamed built-in profile")
	}

	var found bool
	switch handlerName {
	case "entra":
		if cfg.Auth.Entra != nil {
			if _, exists := cfg.Auth.Entra.Profiles[profile]; exists {
				delete(cfg.Auth.Entra.Profiles, profile)
				if cfg.Auth.Entra.ActiveProfile == profile {
					cfg.Auth.Entra.ActiveProfile = ""
				}
				found = true
			}
		}
	case "github":
		if cfg.Auth.GitHub != nil {
			if _, exists := cfg.Auth.GitHub.Profiles[profile]; exists {
				delete(cfg.Auth.GitHub.Profiles, profile)
				if cfg.Auth.GitHub.ActiveProfile == profile {
					cfg.Auth.GitHub.ActiveProfile = ""
				}
				found = true
			}
		}
	case "gcp":
		if cfg.Auth.GCP != nil {
			if _, exists := cfg.Auth.GCP.Profiles[profile]; exists {
				delete(cfg.Auth.GCP.Profiles, profile)
				if cfg.Auth.GCP.ActiveProfile == profile {
					cfg.Auth.GCP.ActiveProfile = ""
				}
				found = true
			}
		}
	default:
		return false, fmt.Errorf("unknown handler: %s", handlerName)
	}

	return found, nil
}

// ResolveEntraConfig returns a copy of the Entra config with the active profile's
// fields merged on top. Non-empty profile fields override top-level values.
// If no profile is active or the profile doesn't exist, the top-level config is
// returned unchanged. The returned config has ActiveProfile/Profiles cleared
// since it represents the fully-resolved effective config.
func ResolveEntraConfig(cfg *config.EntraAuthConfig, profile string) *config.EntraAuthConfig {
	if cfg == nil {
		return nil
	}

	// Copy top-level config (shallow copy is sufficient for string/slice fields).
	resolved := *cfg
	resolved.ActiveProfile = ""
	resolved.Profiles = nil

	if profile == "" {
		return &resolved
	}

	p, ok := cfg.Profiles[profile]
	if !ok || p == nil {
		return &resolved
	}

	// Profile fields override top-level when non-empty.
	if p.HTTPClient != nil {
		resolved.HTTPClient = p.HTTPClient
	}
	if p.ClientID != "" {
		resolved.ClientID = p.ClientID
	}
	if p.TenantID != "" {
		resolved.TenantID = p.TenantID
	}
	if p.Authority != "" {
		resolved.Authority = p.Authority
	}
	if len(p.DefaultScopes) > 0 {
		resolved.DefaultScopes = p.DefaultScopes
	}
	if p.DefaultFlow != "" {
		resolved.DefaultFlow = p.DefaultFlow
	}
	if p.FederatedTokenFile != "" {
		resolved.FederatedTokenFile = p.FederatedTokenFile
	}
	if p.FederatedToken != "" {
		resolved.FederatedToken = p.FederatedToken
	}
	if p.ClientSecret != "" {
		resolved.ClientSecret = p.ClientSecret
	}

	return &resolved
}

// ResolveGitHubConfig returns a copy of the GitHub config with the active profile's
// fields merged on top. Non-empty profile fields override top-level values.
func ResolveGitHubConfig(cfg *config.GitHubAuthConfig, profile string) *config.GitHubAuthConfig {
	if cfg == nil {
		return nil
	}

	resolved := *cfg
	resolved.ActiveProfile = ""
	resolved.Profiles = nil

	if profile == "" {
		return &resolved
	}

	p, ok := cfg.Profiles[profile]
	if !ok || p == nil {
		return &resolved
	}

	if p.HTTPClient != nil {
		resolved.HTTPClient = p.HTTPClient
	}
	if p.ClientID != "" {
		resolved.ClientID = p.ClientID
	}
	if p.ClientSecret != "" {
		resolved.ClientSecret = p.ClientSecret
	}
	if p.Hostname != "" {
		resolved.Hostname = p.Hostname
	}
	if len(p.DefaultScopes) > 0 {
		resolved.DefaultScopes = p.DefaultScopes
	}
	if p.AppID != 0 {
		resolved.AppID = p.AppID
	}
	if p.InstallationID != 0 {
		resolved.InstallationID = p.InstallationID
	}
	if p.PrivateKeyPath != "" {
		resolved.PrivateKeyPath = p.PrivateKeyPath
	}
	if p.PrivateKey != "" {
		resolved.PrivateKey = p.PrivateKey
	}
	if p.PrivateKeySecretName != "" {
		resolved.PrivateKeySecretName = p.PrivateKeySecretName
	}

	return &resolved
}

// ResolveGCPConfig returns a copy of the GCP config with the active profile's
// fields merged on top. Non-empty profile fields override top-level values.
func ResolveGCPConfig(cfg *config.GCPAuthConfig, profile string) *config.GCPAuthConfig {
	if cfg == nil {
		return nil
	}

	resolved := *cfg
	resolved.ActiveProfile = ""
	resolved.Profiles = nil

	if profile == "" {
		return &resolved
	}

	p, ok := cfg.Profiles[profile]
	if !ok || p == nil {
		return &resolved
	}

	if p.HTTPClient != nil {
		resolved.HTTPClient = p.HTTPClient
	}
	if p.ClientID != "" {
		resolved.ClientID = p.ClientID
	}
	if p.ClientSecret != "" {
		resolved.ClientSecret = p.ClientSecret
	}
	if len(p.DefaultScopes) > 0 {
		resolved.DefaultScopes = p.DefaultScopes
	}
	if p.ImpersonateServiceAccount != "" {
		resolved.ImpersonateServiceAccount = p.ImpersonateServiceAccount
	}
	if p.Project != "" {
		resolved.Project = p.Project
	}
	if p.ServiceAccountKeyFile != "" {
		resolved.ServiceAccountKeyFile = p.ServiceAccountKeyFile
	}
	if p.ExternalAccountFile != "" {
		resolved.ExternalAccountFile = p.ExternalAccountFile
	}

	return &resolved
}

func mapKeys[T any](m map[string]*T) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
