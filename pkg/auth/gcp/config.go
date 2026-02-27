// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package gcp provides Google Cloud Platform authentication for scafctl.
package gcp

import "fmt"

const (
	// DefaultADCClientID is Google's well-known OAuth client ID for Application Default Credentials.
	// This is the same client ID used by gcloud for `gcloud auth application-default login`.
	// It is public and safe to embed: https://cloud.google.com/sdk/docs/authorizing
	DefaultADCClientID = "764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com" //nolint:gosec // Google's public ADC client ID, not a credential

	// DefaultADCClientSecret is the corresponding client secret for the default ADC OAuth client.
	// Despite the name, this is not confidential — it is required by the OAuth protocol for
	// installed/desktop applications but provides no security benefit.
	DefaultADCClientSecret = "d-FL95Q19q7MQmFpd7hHD0Ty" //nolint:gosec // Google's public ADC client secret, not a credential
)

// Config holds GCP-specific configuration.
type Config struct {
	// ClientID is the OAuth 2.0 client ID for the ADC browser flow.
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty" doc:"OAuth 2.0 client ID for the ADC browser flow" example:"764086051850-6qr4p6gpi6hn506pt8ejuq83di341hur.apps.googleusercontent.com" maxLength:"256"`

	// ClientSecret is the OAuth 2.0 client secret (not confidential for desktop apps).
	ClientSecret string `json:"clientSecret,omitempty" yaml:"clientSecret,omitempty" doc:"OAuth 2.0 client secret (not confidential for desktop apps)" maxLength:"256"` //nolint:gosec // G117: not a hardcoded credential, it's a config field

	// DefaultScopes are the default OAuth scopes requested during login.
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes requested during login" maxItems:"20"`

	// ImpersonateServiceAccount is the target service account email for impersonation.
	// When set, all token requests will impersonate this service account.
	ImpersonateServiceAccount string `json:"impersonateServiceAccount,omitempty" yaml:"impersonateServiceAccount,omitempty" doc:"Target service account email for impersonation" example:"sa@project.iam.gserviceaccount.com" maxLength:"320"`

	// Project is the default GCP project (informational, not used for auth).
	Project string `json:"project,omitempty" yaml:"project,omitempty" doc:"Default GCP project (informational)" example:"my-project-id" maxLength:"128"`
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
