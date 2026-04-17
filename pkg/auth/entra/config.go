// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package entra provides Microsoft Entra ID (formerly Azure AD) authentication
// for scafctl using the OAuth 2.0 device authorization flow.
package entra

import (
	"fmt"
	"strings"
	"time"
)

// DefaultMinPollInterval is the minimum poll interval for device code flow.
// OAuth 2.0 device authorization grant recommends at least 5 seconds.
const DefaultMinPollInterval = 5 * time.Second

// Config holds Entra-specific configuration.
type Config struct {
	// ClientID is the Azure application/client ID.
	// This should be a public client application registered in Azure.
	ClientID string `json:"clientId" yaml:"clientId" doc:"Azure application ID" example:"04b07795-8ddb-461a-bbee-02f9e1bf7b46"`

	// TenantID is the default Azure tenant ID.
	// Use "common" for multi-tenant, "organizations" for work/school accounts only,
	// or a specific tenant GUID.
	TenantID string `json:"tenantId" yaml:"tenantId" doc:"Azure tenant ID" example:"common"`

	// Authority is the Azure AD authority URL.
	// Defaults to https://login.microsoftonline.com
	Authority string `json:"authority,omitempty" yaml:"authority,omitempty" doc:"Azure AD authority URL"`

	// DefaultScopes are requested during initial login if not specified.
	DefaultScopes []string `json:"defaultScopes,omitempty" yaml:"defaultScopes,omitempty" doc:"Default OAuth scopes"`

	// MinPollInterval is the minimum interval between device code poll requests.
	// Defaults to 5 seconds per OAuth 2.0 spec. Only set lower for testing.
	MinPollInterval time.Duration `json:"-" yaml:"-"`

	// SlowDownIncrement is added to poll interval when server returns slow_down.
	// Defaults to 5 seconds per OAuth 2.0 spec. Only set lower for testing.
	SlowDownIncrement time.Duration `json:"-" yaml:"-"`
}

// DefaultConfig returns the default Entra configuration.
func DefaultConfig() *Config {
	return &Config{
		// Using Azure CLI's public client ID as default.
		// This is a well-known public client that supports device code flow.
		ClientID:          "04b07795-8ddb-461a-bbee-02f9e1bf7b46",
		TenantID:          "common",
		Authority:         "https://login.microsoftonline.com",
		MinPollInterval:   DefaultMinPollInterval,
		SlowDownIncrement: 5 * time.Second,
		DefaultScopes: []string{
			"openid",
			"profile",
			"offline_access",
		},
	}
}

// Validate validates the configuration.
func (c *Config) Validate() error {
	if c.ClientID == "" {
		return fmt.Errorf("clientId is required")
	}
	if c.TenantID == "" {
		return fmt.Errorf("tenantId is required")
	}
	return nil
}

// GetAuthority returns the full authority URL for the configured tenant.
func (c *Config) GetAuthority() string {
	authority := c.Authority
	if authority == "" {
		authority = "https://login.microsoftonline.com"
	}
	return authority
}

// GetAuthorityWithTenant returns the full authority URL for a specific tenant.
func (c *Config) GetAuthorityWithTenant(tenantID string) string {
	return fmt.Sprintf("%s/%s", c.GetAuthority(), tenantID)
}

// DefaultGraphResourceURI is the base URI for Microsoft Graph API scopes.
const DefaultGraphResourceURI = "https://graph.microsoft.com/"

// QualifyScope returns a fully-qualified scope string.  Bare permission names
// like "Group.Read.All" are prefixed with the Microsoft Graph resource URI;
// scopes that already contain a scheme (e.g. "https://...") or are well-known
// OIDC scopes ("openid", "profile", "offline_access", "email") are returned
// unchanged.
func QualifyScope(scope string) string {
	// Already qualified or well-known OIDC scope
	if strings.Contains(scope, "://") || strings.Contains(scope, "/") {
		return scope
	}
	switch scope {
	case "openid", "profile", "offline_access", "email":
		return scope
	}
	return DefaultGraphResourceURI + scope
}
