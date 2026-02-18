// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
package gcp

import "fmt"

// Config holds GCP-specific configuration.
type Config struct {
	// ClientID is the OAuth 2.0 client ID for the ADC browser flow.
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty"`

	// ClientSecret is the OAuth 2.0 client secret (not confidential for desktop apps).
	ClientSecret string `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty"` //nolint:gosec // G117: not a hardcoded credential, it's a config field

	// DefaultScopes are the default OAuth scopes requested during login.
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty"`

	// ImpersonateServiceAccount is the target service account email for impersonation.
	// When set, all token requests will impersonate this service account.
	ImpersonateServiceAccount string `json:"impersonateServiceAccount,omitempty" yaml:"impersonateServiceAccount,omitempty"`

	// Project is the default GCP project (informational, not used for auth).
	Project string `json:"project,omitempty" yaml:"project,omitempty"`
}

// DefaultConfig returns the default GCP configuration.
func DefaultConfig() *Config {
	return &Config{
		ClientID:     "",
		ClientSecret: "",
		DefaultScopes: []string{
			"openid",
			"email",
			"profile",
			"https://www.googleapis.com/auth/cloud-platform",
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.ImpersonateServiceAccount != "" {
		if len(c.ImpersonateServiceAccount) < 6 {
			return fmt.Errorf("impersonateServiceAccount must be a valid service account email")
		}
	}
	return nil
}
