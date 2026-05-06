// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/oakwood-commons/scafctl/pkg/config"
	"github.com/oakwood-commons/scafctl/pkg/logger"
	"github.com/oakwood-commons/scafctl/pkg/secrets"
)

// DefaultClientID is the OAuth App client ID shipped with scafctl.
const DefaultClientID = "Ov23li6xn492GhPmt4YG"

// DefaultHostname is the default GitHub hostname.
const DefaultHostname = "github.com"

// Config holds configuration for the GitHub auth handler.
type Config struct {
	// ClientID is the GitHub OAuth App client ID.
	ClientID string `json:"clientId" yaml:"clientId" doc:"GitHub OAuth App client ID" example:"Ov23li6xn492GhPmt4YG"`

	// ClientSecret is the GitHub OAuth App client secret.
	// Required for the interactive (authorization code) flow. When not set,
	// the interactive flow falls back to device code with browser auto-open —
	// the same behaviour as 'gh auth login'. Not required for device code or PAT flows.
	ClientSecret string `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty" doc:"GitHub OAuth App client secret (required for browser auth code flow)" maxLength:"64"` //nolint:gosec // G117: config field, not a hardcoded credential

	// Hostname is the GitHub hostname (e.g. github.com or github.example.com for GHES).
	Hostname string `json:"hostname" yaml:"hostname" doc:"GitHub hostname" example:"github.com"`

	// DefaultScopes is the list of OAuth scopes to request by default.
	DefaultScopes []string `json:"defaultScopes" yaml:"defaultScopes" doc:"Default OAuth scopes to request" maxItems:"20"`

	// MinPollInterval is the minimum polling interval for device code flow.
	MinPollInterval time.Duration `json:"-" yaml:"-"`

	// SlowDownIncrement is the amount to add to polling interval on slow_down.
	SlowDownIncrement time.Duration `json:"-" yaml:"-"`

	// AppID is the GitHub App ID for the installation token flow.
	AppID int64 `json:"appId,omitempty" yaml:"appId,omitempty" doc:"GitHub App ID for installation token flow" example:"123456"`

	// InstallationID is the GitHub App installation ID.
	InstallationID int64 `json:"installationId,omitempty" yaml:"installationId,omitempty" doc:"GitHub App installation ID" example:"78901234"`

	// PrivateKey is the inline PEM-encoded private key for the GitHub App.
	// Can also be provided via the GITHUB_APP_PRIVATE_KEY environment variable.
	PrivateKey string `json:"privateKey,omitempty" yaml:"privateKey,omitempty" doc:"Inline PEM-encoded private key for the GitHub App" maxLength:"8192"` //nolint:gosec // Field name, not a credential

	// PrivateKeyPath is the file path to the PEM-encoded private key for the GitHub App.
	// Can also be provided via the GITHUB_APP_PRIVATE_KEY_PATH environment variable.
	PrivateKeyPath string `json:"privateKeyPath,omitempty" yaml:"privateKeyPath,omitempty" doc:"File path to PEM-encoded private key for the GitHub App" example:"/path/to/private-key.pem" maxLength:"1024"`

	// PrivateKeySecretName is the name of the secret in the secret store that
	// contains the PEM-encoded private key for the GitHub App.
	PrivateKeySecretName string `json:"privateKeySecretName,omitempty" yaml:"privateKeySecretName,omitempty" doc:"Secret store key for the GitHub App private key" maxLength:"255"`
}

// DefaultConfig returns the default GitHub auth configuration.
// Values for ClientID, Hostname, and DefaultScopes are read from the embedded
// defaults.yaml (pkg/config) so that the YAML file is the single source of
// truth. Non-serialised fields (poll intervals) are set here.
func DefaultConfig() *Config {
	cfg := &Config{
		ClientID:          DefaultClientID,
		Hostname:          DefaultHostname,
		MinPollInterval:   5 * time.Second,
		SlowDownIncrement: 5 * time.Second,
	}

	// Overlay values from the embedded defaults.yaml.
	if embedded := config.EmbeddedGitHubDefaults(); embedded != nil {
		if embedded.ClientID != "" {
			cfg.ClientID = embedded.ClientID
		}
		if embedded.Hostname != "" {
			cfg.Hostname = embedded.Hostname
		}
		if len(embedded.DefaultScopes) > 0 {
			cfg.DefaultScopes = embedded.DefaultScopes
		}
	}

	return cfg
}

// Validate checks the configuration for required fields.
func (c *Config) Validate() error {
	if c.ClientID == "" {
		return fmt.Errorf("github auth: client ID is required")
	}
	if c.Hostname == "" {
		return fmt.Errorf("github auth: hostname is required")
	}
	return nil
}

// GetOAuthBaseURL returns the base URL for GitHub OAuth endpoints.
func (c *Config) GetOAuthBaseURL() string {
	if c.Hostname == DefaultHostname {
		return "https://github.com"
	}
	return fmt.Sprintf("https://%s", c.Hostname)
}

// GetAPIBaseURL returns the base URL for GitHub API endpoints.
func (c *Config) GetAPIBaseURL() string {
	if c.Hostname == DefaultHostname {
		return "https://api.github.com"
	}
	return fmt.Sprintf("https://%s/api/v3", c.Hostname)
}

// GitHub App environment variable names.
const (
	// EnvGitHubAppID is the environment variable for the GitHub App ID.
	EnvGitHubAppID = "GITHUB_APP_ID"

	// EnvGitHubAppInstallationID is the environment variable for the GitHub App installation ID.
	EnvGitHubAppInstallationID = "GITHUB_APP_INSTALLATION_ID"

	// EnvGitHubAppPrivateKey is the environment variable for the inline PEM-encoded private key.
	EnvGitHubAppPrivateKey = "GITHUB_APP_PRIVATE_KEY" //nolint:gosec // env var name, not a credential

	// EnvGitHubAppPrivateKeyPath is the environment variable for the private key file path.
	EnvGitHubAppPrivateKeyPath = "GITHUB_APP_PRIVATE_KEY_PATH" //nolint:gosec // env var name, not a credential
)

// GetPrivateKey resolves the GitHub App private key from (in priority order):
// 1. Inline PrivateKey field / GITHUB_APP_PRIVATE_KEY env var
// 2. PrivateKeyPath field / GITHUB_APP_PRIVATE_KEY_PATH env var (read from file)
// 3. PrivateKeySecretName in the provided secret store
//
// Returns the PEM-encoded key bytes, or an error if no source provides a key.
func (c *Config) GetPrivateKey(ctx context.Context, store secrets.Store) ([]byte, error) {
	lgr := logger.FromContext(ctx)

	// 1. Inline PEM (field or env var)
	key := c.PrivateKey
	if key == "" {
		key = os.Getenv(EnvGitHubAppPrivateKey)
	}
	if key != "" {
		lgr.Info("WARNING: GitHub App private key loaded from inline config or environment variable; " +
			"prefer privateKeySecretName (secret store) or privateKeyPath (file) for better protection",
		)
		return []byte(key), nil
	}

	// 2. File path (field or env var)
	path := c.PrivateKeyPath
	if path == "" {
		path = os.Getenv(EnvGitHubAppPrivateKeyPath)
	}
	if path != "" {
		data, err := os.ReadFile(path) //nolint:gosec // user-provided path to their own private key
		if err != nil {
			return nil, fmt.Errorf("reading private key from %s: %w", path, err)
		}
		return data, nil
	}

	// 3. Secret store
	if c.PrivateKeySecretName != "" && store != nil {
		data, err := store.Get(ctx, c.PrivateKeySecretName)
		if err != nil {
			return nil, fmt.Errorf("reading private key from secret store (%s): %w", c.PrivateKeySecretName, err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("no private key configured: set %s env var, provide --private-key flag, or configure privateKeyPath/privateKeySecretName in config", EnvGitHubAppPrivateKey)
}

// GetAppID returns the App ID from config or the GITHUB_APP_ID environment variable.
func (c *Config) GetAppID() int64 {
	if c.AppID != 0 {
		return c.AppID
	}
	if v := os.Getenv(EnvGitHubAppID); v != "" {
		var id int64
		if _, err := fmt.Sscanf(v, "%d", &id); err == nil {
			return id
		}
	}
	return 0
}

// GetInstallationID returns the Installation ID from config or the GITHUB_APP_INSTALLATION_ID environment variable.
func (c *Config) GetInstallationID() int64 {
	if c.InstallationID != 0 {
		return c.InstallationID
	}
	if v := os.Getenv(EnvGitHubAppInstallationID); v != "" {
		var id int64
		if _, err := fmt.Sscanf(v, "%d", &id); err == nil {
			return id
		}
	}
	return 0
}

// ValidateAppConfig checks that all required GitHub App fields are present.
func (c *Config) ValidateAppConfig(ctx context.Context, store secrets.Store) error {
	if c.GetAppID() == 0 {
		return fmt.Errorf("github app: app ID is required (set %s or configure appId)", EnvGitHubAppID)
	}
	if c.GetInstallationID() == 0 {
		return fmt.Errorf("github app: installation ID is required (set %s or configure installationId)", EnvGitHubAppInstallationID)
	}
	if _, err := c.GetPrivateKey(ctx, store); err != nil {
		return fmt.Errorf("github app: %w", err)
	}
	return nil
}
