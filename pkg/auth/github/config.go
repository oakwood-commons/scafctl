// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"fmt"
	"time"
)

// DefaultClientID is the OAuth App client ID shipped with scafctl.
const DefaultClientID = "Ov23li6xn492GhPmt4YG"

// DefaultHostname is the default GitHub hostname.
const DefaultHostname = "github.com"

// Config holds configuration for the GitHub auth handler.
type Config struct {
	// ClientID is the GitHub OAuth App client ID.
	ClientID string `json:"clientId" yaml:"clientId" doc:"GitHub OAuth App client ID" example:"Ov23li6xn492GhPmt4YG"`

	// Hostname is the GitHub hostname (e.g. github.com or github.example.com for GHES).
	Hostname string `json:"hostname" yaml:"hostname" doc:"GitHub hostname" example:"github.com"`

	// DefaultScopes is the list of OAuth scopes to request by default.
	DefaultScopes []string `json:"defaultScopes" yaml:"defaultScopes" doc:"Default OAuth scopes to request" maxItems:"20"`

	// MinPollInterval is the minimum polling interval for device code flow.
	MinPollInterval time.Duration `json:"-" yaml:"-"`

	// SlowDownIncrement is the amount to add to polling interval on slow_down.
	SlowDownIncrement time.Duration `json:"-" yaml:"-"`
}

// DefaultConfig returns the default GitHub auth configuration.
func DefaultConfig() *Config {
	return &Config{
		ClientID:          DefaultClientID,
		Hostname:          DefaultHostname,
		DefaultScopes:     []string{"repo", "read:user"},
		MinPollInterval:   5 * time.Second,
		SlowDownIncrement: 5 * time.Second,
	}
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
